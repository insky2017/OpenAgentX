package worker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"openagentx/internal/api"
	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
)

var errControlledStop = errors.New("controlled Worker stop")

type Runner struct {
	config   Config
	client   api.WorkerControlClient
	backends *BackendPool
	resolver ControlPayloadResolver

	draining atomic.Bool
	statusMu sync.RWMutex
	health   map[string]openruntime.BackendHealth
}

type networkBindingPullClient interface {
	PullNetworkBindings(context.Context, api.NetworkBindingPullRequest) ([]domain.NetworkBinding, error)
}

func NewRunner(config Config, client api.WorkerControlClient, backends *BackendPool, resolver ControlPayloadResolver) (*Runner, error) {
	config = config.withDefaults()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if client == nil || backends == nil {
		return nil, fmt.Errorf("Worker Control client and Runtime Backend pool are required")
	}
	return &Runner{config: config, client: client, backends: backends, resolver: resolver}, nil
}

func (r *Runner) Run(ctx context.Context) error {
	registrations, health, err := r.backends.Observe(ctx)
	if err != nil {
		return fmt.Errorf("Worker Runtime self-check: %w", err)
	}
	r.setHealth(health)
	session, err := r.client.RegisterWorker(ctx, api.RegisterRequest{
		ContractVersion: api.ContractVersion, AgentID: r.config.AgentID,
		WorkerInstanceID: r.config.WorkerInstanceID, Transport: r.config.Transport,
		Capabilities: append([]string(nil), r.config.Capabilities...), Backends: registrations,
	})
	if err != nil {
		return fmt.Errorf("register Worker: %w", err)
	}
	networkAcks := r.applyNetworkBindings(session.NetworkBindings)
	if err := r.heartbeat(ctx, session, domain.WorkerStatusOnline, networkAcks); err != nil {
		return fmt.Errorf("initial Worker heartbeat: %w", err)
	}
	// The acknowledgement is a one-shot state transition. Subsequent
	// heartbeats omit it; a still-pending binding will be returned by pull and
	// retried explicitly.
	networkAcks = nil
	manager, err := NewActiveRunManager(r.client, r.backends, r.resolver, *session,
		&r.draining, r.config.ShutdownTimeout)
	if err != nil {
		return err
	}

	runContext, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	group, groupContext := errgroup.WithContext(runContext)
	stopCommands := make(chan domain.WorkerCommand, 1)
	group.Go(func() error { return r.heartbeatLoop(groupContext, session, networkAcks) })
	group.Go(func() error { return r.mailboxPump(groupContext, session, manager) })
	group.Go(func() error { return manager.Run(groupContext) })
	group.Go(func() error { return r.workerControlLoop(groupContext, session, stopCommands) })

	err = group.Wait()
	if errors.Is(err, errControlledStop) {
		select {
		case command := <-stopCommands:
			ackContext, cancel := context.WithTimeout(context.Background(), r.config.ShutdownTimeout)
			defer cancel()
			if releaseErr := r.releaseWorker(ackContext, session); releaseErr != nil {
				return fmt.Errorf("release Worker lease before stop acknowledgement: %w", releaseErr)
			}
			if ackErr := r.client.AcknowledgeReleasedWorkerCommand(ackContext, command.ID, api.ControlAckRequest{
				WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
				FencingToken: session.Worker.FencingToken,
				State:        domain.WorkerCommandApplied, Result: "Worker stopped after controlled reconciliation",
			}); ackErr != nil {
				return fmt.Errorf("acknowledge controlled Worker stop: %w", ackErr)
			}
			return nil
		default:
			return fmt.Errorf("controlled Worker stop lost command context")
		}
	}
	if err == nil && ctx.Err() != nil {
		releaseContext, cancel := context.WithTimeout(context.Background(), r.config.ShutdownTimeout)
		defer cancel()
		if releaseErr := r.releaseWorker(releaseContext, session); releaseErr != nil {
			return fmt.Errorf("release Worker lease during shutdown: %w", releaseErr)
		}
	}
	if err != nil {
		return err
	}
	return nil
}

func (r *Runner) releaseWorker(ctx context.Context, session *api.WorkerSession) error {
	if err := r.client.ReleaseWorker(ctx, api.WorkerReleaseRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken,
	}); err != nil {
		return err
	}
	// ReleaseWorker advances the fencing token exactly once in its transaction;
	// carry that value for the narrow released stop acknowledgement.
	session.Worker.FencingToken++
	return nil
}

func (r *Runner) heartbeatLoop(ctx context.Context, session *api.WorkerSession, networkAcks map[string]api.NetworkBindingAck) error {
	ticker := time.NewTicker(r.config.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if puller, ok := r.client.(networkBindingPullClient); ok {
				bindings, pullErr := puller.PullNetworkBindings(ctx, api.NetworkBindingPullRequest{WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken})
				if pullErr != nil {
					if ctx.Err() != nil {
						return nil
					}
					return fmt.Errorf("pull Worker network bindings: %w", pullErr)
				}
				if len(bindings) > 0 {
					networkAcks = r.applyNetworkBindings(bindings)
					if err := r.heartbeat(ctx, session, domain.WorkerStatusOnline, networkAcks); err != nil {
						if ctx.Err() != nil {
							return nil
						}
						return fmt.Errorf("acknowledge Worker network bindings: %w", err)
					}
					networkAcks = nil
				}
			}
			_, health, observeErr := r.backends.Observe(ctx)
			status := domain.WorkerStatusOnline
			if observeErr != nil {
				status = domain.WorkerStatusDegraded
			}
			if len(health) != 0 {
				r.setHealth(health)
			}
			if r.draining.Load() {
				status = domain.WorkerStatusDraining
			}
			if err := r.heartbeat(ctx, session, status, networkAcks); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("Worker heartbeat: %w", err)
			}
		}
	}
}

func (r *Runner) heartbeat(ctx context.Context, session *api.WorkerSession, status domain.WorkerStatus, networkAcks map[string]api.NetworkBindingAck) error {
	return r.client.Heartbeat(ctx, api.HeartbeatRequest{
		WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
		FencingToken: session.Worker.FencingToken, Status: status, BackendHealth: r.getHealth(), NetworkBindings: networkAcks,
	})
}

func (r *Runner) applyNetworkBindings(bindings []domain.NetworkBinding) map[string]api.NetworkBindingAck {
	acks := make(map[string]api.NetworkBindingAck, len(bindings))
	for _, binding := range bindings {
		ack := api.NetworkBindingAck{BackendID: binding.BackendID, ProfileID: binding.ProfileID, ProfileVersion: binding.ProfileVersion, State: "applied"}
		if err := r.backends.ApplyNetworkBinding(binding); err != nil {
			ack.State = "failed"
			ack.Diagnostic = sanitizeNetworkDiagnostic(err.Error())
		}
		acks[binding.BackendID] = ack
	}
	return acks
}

func sanitizeNetworkDiagnostic(value string) string {
	value = strings.ReplaceAll(value, "\r", " ")
	value = strings.ReplaceAll(value, "\n", " ")
	if len(value) > 4096 {
		value = value[:4096]
	}
	return value
}

func (r *Runner) mailboxPump(ctx context.Context, session *api.WorkerSession, manager *ActiveRunManager) error {
	for {
		workCapacity := manager.WorkCapacity()
		request := api.ClaimRequest{
			WorkerInstanceID: session.Worker.ID, AgentID: session.Worker.AgentID,
			Generation: session.Worker.Generation, FencingToken: session.Worker.FencingToken,
			WorkCapacity: workCapacity, WaitSeconds: int(r.config.MailboxWait / time.Second),
		}
		claimContext := ctx
		cancelClaim := func() {}
		claimFinished := make(chan struct{})
		if workCapacity == 0 {
			var cancel context.CancelFunc
			claimContext, cancel = context.WithCancel(ctx)
			cancelClaim = cancel
			go func() {
				select {
				case <-manager.CapacityAvailable():
					cancel()
				case <-claimFinished:
				case <-ctx.Done():
				}
			}()
		}
		item, err := r.client.ClaimMailbox(claimContext, request)
		close(claimFinished)
		cancelClaim()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if workCapacity == 0 && errors.Is(err, context.Canceled) {
				continue
			}
			return fmt.Errorf("claim Agent Mailbox: %w", err)
		}
		if item == nil {
			continue
		}
		if item.Lane == domain.MailboxLaneWork && manager.WorkCapacity() == 0 {
			wait := 50 * time.Millisecond
			if item.LeaseUntil != nil {
				wait = time.Until(*item.LeaseUntil)
				if wait < 0 {
					wait = 0
				}
			}
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return nil
			case <-timer.C:
			}
			continue
		}
		if err := manager.Submit(ctx, *item); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("deliver Mailbox item to Run Manager: %w", err)
		}
	}
}

func (r *Runner) workerControlLoop(ctx context.Context, session *api.WorkerSession, stopCommands chan<- domain.WorkerCommand) error {
	if !r.config.EnableControlLoop {
		<-ctx.Done()
		return nil
	}
	for {
		command, err := r.client.ClaimWorkerCommand(ctx, api.ControlClaimRequest{
			WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
			FencingToken: session.Worker.FencingToken,
			WaitSeconds:  int(r.config.ControlWait / time.Second),
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("claim Worker control command: %w", err)
		}
		if command == nil {
			continue
		}
		switch command.Kind {
		case domain.WorkerCommandDrain:
			r.draining.Store(true)
			if err := r.heartbeat(ctx, session, domain.WorkerStatusDraining, nil); err != nil {
				return fmt.Errorf("enter Worker draining state: %w", err)
			}
			if err := r.client.AcknowledgeWorkerCommand(ctx, command.ID, api.ControlAckRequest{
				WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
				FencingToken: session.Worker.FencingToken,
				State:        domain.WorkerCommandApplied, Result: "Worker is draining",
			}); err != nil {
				return fmt.Errorf("acknowledge Worker drain: %w", err)
			}
		case domain.WorkerCommandHealthCheck:
			if err := r.client.AcknowledgeWorkerCommand(ctx, command.ID, api.ControlAckRequest{
				WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
				FencingToken: session.Worker.FencingToken,
				State:        domain.WorkerCommandApplied, Result: "Worker heartbeat and Runtime Backend probes are active",
			}); err != nil {
				return fmt.Errorf("acknowledge Worker health check: %w", err)
			}
		case domain.WorkerCommandStop:
			r.draining.Store(true)
			select {
			case stopCommands <- *command:
				return errControlledStop
			case <-ctx.Done():
				return nil
			}
		default:
			if err := r.client.AcknowledgeWorkerCommand(ctx, command.ID, api.ControlAckRequest{
				WorkerInstanceID: session.Worker.ID, Generation: session.Worker.Generation,
				FencingToken: session.Worker.FencingToken,
				State:        domain.WorkerCommandFailed, Result: "Unsupported Worker command",
			}); err != nil {
				return fmt.Errorf("reject unsupported Worker command: %w", err)
			}
		}
	}
}

func (r *Runner) setHealth(health map[string]openruntime.BackendHealth) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	r.health = make(map[string]openruntime.BackendHealth, len(health))
	for backendID, status := range health {
		r.health[backendID] = status
	}
}

func (r *Runner) getHealth() map[string]openruntime.BackendHealth {
	r.statusMu.RLock()
	defer r.statusMu.RUnlock()
	health := make(map[string]openruntime.BackendHealth, len(r.health))
	for backendID, status := range r.health {
		health[backendID] = status
	}
	return health
}
