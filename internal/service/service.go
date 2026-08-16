package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"agentbus/internal/connector"
	"agentbus/internal/domain"
	"agentbus/internal/store"
	"github.com/google/uuid"
)

type EventBroker struct {
	mu          sync.RWMutex
	subscribers map[string][]chan struct{}
}

func NewEventBroker() *EventBroker {
	return &EventBroker{
		subscribers: make(map[string][]chan struct{}),
	}
}

func (b *EventBroker) Subscribe(taskID string) (chan struct{}, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan struct{}, 1)
	b.subscribers[taskID] = append(b.subscribers[taskID], ch)
	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		subs := b.subscribers[taskID]
		for i, sub := range subs {
			if sub == ch {
				b.subscribers[taskID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		if len(b.subscribers[taskID]) == 0 {
			delete(b.subscribers, taskID)
		}
	}
	return ch, unsubscribe
}

func (b *EventBroker) Publish(taskID string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if subs, ok := b.subscribers[taskID]; ok {
		for _, ch := range subs {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}
}

type Service struct {
	store     store.Store
	connector *connector.TmuxConnector
	broker    *EventBroker
	logger    *slog.Logger
}

func NewService(s store.Store, conn *connector.TmuxConnector, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:     s,
		connector: conn,
		broker:    NewEventBroker(),
		logger:    logger,
	}
}

func (s *Service) RegisterAgent(ctx context.Context, agent *domain.Agent) error {
	if err := agent.Validate(); err != nil {
		return err
	}
	if agent.Connector == domain.ConnectorTmux {
		if err := connector.ValidateAddress(agent.Address); err != nil {
			return err
		}
	}
	err := s.store.RegisterAgent(ctx, agent)
	if err == nil {
		s.logger.Info("agent registered",
			slog.String("agent_id", agent.ID),
			slog.String("role", agent.Role),
			slog.String("connector", agent.Connector),
			slog.String("address", agent.Address),
		)
	}
	return err
}

func (s *Service) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	return s.store.GetAgent(ctx, id)
}

func (s *Service) ListAgents(ctx context.Context) ([]*domain.Agent, error) {
	return s.store.ListAgents(ctx)
}

// V0.1 Attach / Bootstrap / Session

type AttachAgentRequest struct {
	Agent    *domain.Agent        `json:"agent"`
	Profile  *domain.AgentProfile `json:"profile"`
	NoNotify bool                 `json:"no_notify"`
}

type AttachAgentResponse struct {
	Agent         *domain.Agent                 `json:"agent"`
	Profile       *domain.AgentProfile          `json:"profile"`
	Session       *domain.AgentSession          `json:"session"`
	Disposition   connector.DeliveryDisposition `json:"disposition"`
	DeliveryError string                        `json:"delivery_error,omitempty"`
}

func (s *Service) AttachAgent(ctx context.Context, req AttachAgentRequest) (*AttachAgentResponse, error) {
	if req.Agent == nil {
		return nil, domain.ErrInvalidInput("agent is required")
	}
	if req.Profile == nil {
		return nil, domain.ErrInvalidInput("profile is required")
	}
	if err := req.Agent.Validate(); err != nil {
		return nil, err
	}
	if err := req.Profile.Validate(); err != nil {
		return nil, err
	}
	if req.Agent.ID != req.Profile.AgentID {
		return nil, domain.ErrInvalidInput("agent ID must match profile agent ID")
	}

	sessionStatus := domain.SessionStatusBootstrapping
	resolvedPane := req.Agent.Address
	var delivErr *string
	disposition := connector.DispositionSkipped

	if req.Agent.Connector == domain.ConnectorTmux && s.connector != nil && !req.NoNotify {
		paneID, err := s.connector.ProbePane(ctx, req.Agent.Address)
		if err != nil {
			errStr := err.Error()
			delivErr = &errStr
			sessionStatus = domain.SessionStatusDeliveryFailed
			disposition = connector.DispositionDeliveryFailed
		} else {
			resolvedPane = paneID
		}
	}

	session, err := s.store.AttachAgent(ctx, req.Agent, req.Profile, sessionStatus, resolvedPane, delivErr)
	if err != nil {
		return nil, err
	}

	// Deliver bootstrap notification if tmux, probe succeeded, and not no-notify
	if req.Agent.Connector == domain.ConnectorTmux && s.connector != nil && !req.NoNotify && sessionStatus == domain.SessionStatusBootstrapping {
		deliv := s.connector.NotifyBootstrap(ctx, req.Agent.Address, req.Agent.ID, req.Agent.Role, session.Generation, req.Profile.InstructionsPath)
		if deliv.Disposition == connector.DispositionDeliveryFailed {
			delivErr = &deliv.Error
			disposition = connector.DispositionDeliveryFailed
			updatedSess, updateErr := s.store.UpdateSessionDelivery(ctx, req.Agent.ID, session.Generation, domain.SessionStatusDeliveryFailed, resolvedPane, &deliv.Error)
			if updateErr != nil {
				return nil, fmt.Errorf("bootstrap notification delivery failed (%s) and failed to persist delivery state: %w", deliv.Error, updateErr)
			}
			session = updatedSess
		} else {
			disposition = connector.DispositionNotified
			s.logger.Info("agent bootstrap notified",
				slog.String("agent_id", req.Agent.ID),
				slog.Int64("generation", session.Generation),
				slog.String("pane_id", deliv.PaneID),
			)
		}
	} else if req.NoNotify {
		s.logger.Info("agent attached with no-notify",
			slog.String("agent_id", req.Agent.ID),
			slog.Int64("generation", session.Generation),
		)
	}

	return &AttachAgentResponse{
		Agent:       req.Agent,
		Profile:     req.Profile,
		Session:     session,
		Disposition: disposition,
		DeliveryError: func() string {
			if delivErr != nil {
				return *delivErr
			}
			return ""
		}(),
	}, nil
}

type BootstrapAgentResponse struct {
	Agent         *domain.Agent                 `json:"agent"`
	Profile       *domain.AgentProfile          `json:"profile"`
	Session       *domain.AgentSession          `json:"session"`
	Disposition   connector.DeliveryDisposition `json:"disposition"`
	DeliveryError string                        `json:"delivery_error,omitempty"`
}

func (s *Service) BootstrapAgent(ctx context.Context, agentID string) (*BootstrapAgentResponse, error) {
	agent, profile, _, err := s.store.GetAgentSession(ctx, agentID)
	if err != nil {
		return nil, err
	}

	sessionStatus := domain.SessionStatusBootstrapping
	resolvedPane := agent.Address
	var delivErr *string
	disposition := connector.DispositionSkipped

	if agent.Connector == domain.ConnectorTmux && s.connector != nil {
		paneID, err := s.connector.ProbePane(ctx, agent.Address)
		if err != nil {
			errStr := err.Error()
			delivErr = &errStr
			sessionStatus = domain.SessionStatusDeliveryFailed
			disposition = connector.DispositionDeliveryFailed
		} else {
			resolvedPane = paneID
		}
	}

	a, p, session, err := s.store.BootstrapAgent(ctx, agentID, sessionStatus, resolvedPane, delivErr)
	if err != nil {
		return nil, err
	}

	if agent.Connector == domain.ConnectorTmux && s.connector != nil && sessionStatus == domain.SessionStatusBootstrapping {
		deliv := s.connector.NotifyBootstrap(ctx, agent.Address, agent.ID, agent.Role, session.Generation, profile.InstructionsPath)
		if deliv.Disposition == connector.DispositionDeliveryFailed {
			delivErr = &deliv.Error
			disposition = connector.DispositionDeliveryFailed
			updatedSess, updateErr := s.store.UpdateSessionDelivery(ctx, agentID, session.Generation, domain.SessionStatusDeliveryFailed, resolvedPane, &deliv.Error)
			if updateErr != nil {
				return nil, fmt.Errorf("bootstrap notification delivery failed (%s) and failed to persist delivery state: %w", deliv.Error, updateErr)
			}
			session = updatedSess
		} else {
			disposition = connector.DispositionNotified
			s.logger.Info("agent bootstrap re-notified",
				slog.String("agent_id", agent.ID),
				slog.Int64("generation", session.Generation),
				slog.String("pane_id", deliv.PaneID),
			)
		}
	}

	return &BootstrapAgentResponse{
		Agent:       a,
		Profile:     p,
		Session:     session,
		Disposition: disposition,
		DeliveryError: func() string {
			if delivErr != nil {
				return *delivErr
			}
			return ""
		}(),
	}, nil
}

func (s *Service) ReadySession(ctx context.Context, agentID string, generation int64) (*domain.AgentSession, error) {
	session, err := s.store.ReadySession(ctx, agentID, generation)
	if err != nil {
		return nil, err
	}
	s.logger.Info("agent session ready",
		slog.String("agent_id", agentID),
		slog.Int64("generation", generation),
	)
	return session, nil
}

type GetSessionResponse struct {
	Agent   *domain.Agent        `json:"agent"`
	Profile *domain.AgentProfile `json:"profile"`
	Session *domain.AgentSession `json:"session"`
}

func (s *Service) GetSession(ctx context.Context, agentID string) (*GetSessionResponse, error) {
	a, p, sess, err := s.store.GetAgentSession(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return &GetSessionResponse{
		Agent:   a,
		Profile: p,
		Session: sess,
	}, nil
}

// Ready Gate Helper

func (s *Service) verifyAgentReady(ctx context.Context, agentID string, roleName string) error {
	ready, err := s.store.IsAgentReady(ctx, agentID)
	if err != nil {
		return err
	}
	if !ready {
		return fmt.Errorf("%w: %s '%s' has not completed session bootstrap/ready", domain.ErrAgentNotReady, roleName, agentID)
	}
	return nil
}

type SubmitTaskRequest struct {
	SenderAgentID  string `json:"sender_agent_id"`
	TargetAgentID  string `json:"target_agent_id"`
	IdempotencyKey string `json:"idempotency_key"`
	Content        string `json:"content"`
}

type SubmitTaskResponse struct {
	Task        *domain.Task `json:"task"`
	IsDuplicate bool         `json:"is_duplicate"`
}

func (s *Service) SubmitTask(ctx context.Context, req SubmitTaskRequest) (*SubmitTaskResponse, error) {
	// Ready Gate: verify sender and target are both ready
	if err := s.verifyAgentReady(ctx, req.SenderAgentID, "sender agent"); err != nil {
		return nil, err
	}
	if err := s.verifyAgentReady(ctx, req.TargetAgentID, "target agent"); err != nil {
		return nil, err
	}

	taskID := fmt.Sprintf("task-%s", uuid.New().String())
	now := time.Now().UTC().Format(time.RFC3339Nano)

	task := &domain.Task{
		ID:             taskID,
		SenderAgentID:  strings.TrimSpace(req.SenderAgentID),
		TargetAgentID:  strings.TrimSpace(req.TargetAgentID),
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		Content:        strings.TrimSpace(req.Content),
		Status:         domain.TaskStatusQueued,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	initialMsg := &domain.Message{
		ID:            fmt.Sprintf("msg-%s", uuid.New().String()),
		TaskID:        taskID,
		SenderAgentID: task.SenderAgentID,
		Kind:          domain.MessageKindInstruction,
		Content:       task.Content,
		CreatedAt:     now,
	}

	submitPayload, _ := json.Marshal(map[string]any{
		"sender_agent_id": task.SenderAgentID,
		"target_agent_id": task.TargetAgentID,
		"status":          task.Status,
	})

	submitEvt := &domain.Event{
		ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
		TaskID:       taskID,
		ActorAgentID: task.SenderAgentID,
		Type:         domain.EventTaskSubmitted,
		Payload:      string(submitPayload),
		CreatedAt:    now,
	}

	createdTask, isDuplicate, err := s.store.SubmitTask(ctx, task, initialMsg, submitEvt)
	if err != nil {
		s.logger.Warn("task submit rejected",
			slog.String("sender_agent_id", req.SenderAgentID),
			slog.String("target_agent_id", req.TargetAgentID),
			slog.String("idempotency_key", req.IdempotencyKey),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	if isDuplicate {
		s.logger.Info("task submit idempotent hit",
			slog.String("task_id", createdTask.ID),
			slog.String("sender_agent_id", req.SenderAgentID),
			slog.String("idempotency_key", req.IdempotencyKey),
		)
		return &SubmitTaskResponse{
			Task:        createdTask,
			IsDuplicate: true,
		}, nil
	}

	s.logger.Info("task submitted",
		slog.String("task_id", createdTask.ID),
		slog.String("sender_agent_id", createdTask.SenderAgentID),
		slog.String("target_agent_id", createdTask.TargetAgentID),
		slog.String("status", string(createdTask.Status)),
	)
	s.broker.Publish(createdTask.ID)

	// Trigger connector notification with identity reminder
	targetAgent, targetProfile, _, err := s.store.GetAgentSession(ctx, createdTask.TargetAgentID)
	if err == nil && targetAgent.Connector == domain.ConnectorTmux && s.connector != nil {
		instrPath := ""
		if targetProfile != nil {
			instrPath = targetProfile.InstructionsPath
		}
		deliv := s.connector.NotifyTask(ctx, targetAgent.Address, targetAgent.ID, targetAgent.Role, instrPath, createdTask.ID, true)
		delivNow := time.Now().UTC().Format(time.RFC3339Nano)
		if deliv.Disposition == connector.DispositionNotified {
			p, _ := json.Marshal(map[string]any{"pane_id": deliv.PaneID, "disposition": deliv.Disposition})
			evt := &domain.Event{
				ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
				TaskID:       createdTask.ID,
				ActorAgentID: "agentbus",
				Type:         domain.EventTaskNotified,
				Payload:      string(p),
				CreatedAt:    delivNow,
			}
			if _, err := s.store.AddEvent(ctx, evt); err != nil {
				s.logger.Error("failed to record notification event",
					slog.String("task_id", createdTask.ID),
					slog.String("error", err.Error()),
				)
			} else {
				s.logger.Info("tmux notification delivered",
					slog.String("task_id", createdTask.ID),
					slog.String("target_agent_id", targetAgent.ID),
					slog.String("pane_id", deliv.PaneID),
				)
				s.broker.Publish(createdTask.ID)
			}
		} else {
			p, _ := json.Marshal(map[string]any{"error": deliv.Error, "pane_id": deliv.PaneID, "disposition": deliv.Disposition})
			evt := &domain.Event{
				ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
				TaskID:       createdTask.ID,
				ActorAgentID: "agentbus",
				Type:         domain.EventTaskDeliveryFailed,
				Payload:      string(p),
				CreatedAt:    delivNow,
			}
			if _, err := s.store.AddEvent(ctx, evt); err != nil {
				s.logger.Error("failed to record delivery failure event",
					slog.String("task_id", createdTask.ID),
					slog.String("error", err.Error()),
				)
			} else {
				s.logger.Warn("tmux notification delivery failed",
					slog.String("task_id", createdTask.ID),
					slog.String("target_agent_id", targetAgent.ID),
					slog.String("error", deliv.Error),
				)
				s.broker.Publish(createdTask.ID)
			}
		}
	}

	return &SubmitTaskResponse{
		Task:        createdTask,
		IsDuplicate: false,
	}, nil
}

func (s *Service) GetTask(ctx context.Context, taskID string, callerAgentID string) (*domain.Task, error) {
	caller := strings.TrimSpace(callerAgentID)
	if caller == "" {
		return nil, domain.ErrInvalidInput("caller --agent is required to get task")
	}
	if err := s.verifyAgentReady(ctx, caller, "caller agent"); err != nil {
		return nil, err
	}

	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	if task.SenderAgentID != caller && task.TargetAgentID != caller {
		return nil, fmt.Errorf("%w: agent '%s' is neither sender nor target for task '%s'", domain.ErrUnauthorized, caller, taskID)
	}

	return task, nil
}

func (s *Service) ListTasks(ctx context.Context, agentID string, status string) ([]*domain.Task, error) {
	agent := strings.TrimSpace(agentID)
	if agent == "" {
		return nil, domain.ErrInvalidInput("filter --agent is required to list tasks")
	}
	if err := s.verifyAgentReady(ctx, agent, "filter agent"); err != nil {
		return nil, err
	}
	return s.store.ListTasks(ctx, agent, status)
}

func (s *Service) AckTask(ctx context.Context, taskID string, actorAgentID string) (*domain.Task, error) {
	if err := s.verifyAgentReady(ctx, actorAgentID, "actor agent"); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload, _ := json.Marshal(map[string]any{"actor_agent_id": actorAgentID, "status": domain.TaskStatusRunning})
	evt := &domain.Event{
		ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
		TaskID:       taskID,
		ActorAgentID: actorAgentID,
		Type:         domain.EventTaskAcknowledged,
		Payload:      string(payload),
		CreatedAt:    now,
	}

	t, err := s.store.AckTask(ctx, taskID, actorAgentID, evt)
	if err != nil {
		s.logger.Warn("task ack failed",
			slog.String("task_id", taskID),
			slog.String("actor_agent_id", actorAgentID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("task acknowledged",
		slog.String("task_id", taskID),
		slog.String("actor_agent_id", actorAgentID),
		slog.String("status", string(t.Status)),
	)
	s.broker.Publish(taskID)
	return t, nil
}

func (s *Service) UpdateTaskStatus(ctx context.Context, taskID string, actorAgentID string, message string) error {
	if err := s.verifyAgentReady(ctx, actorAgentID, "actor agent"); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	msg := &domain.Message{
		ID:            fmt.Sprintf("msg-%s", uuid.New().String()),
		TaskID:        taskID,
		SenderAgentID: actorAgentID,
		Kind:          domain.MessageKindStatusUpdate,
		Content:       message,
		CreatedAt:     now,
	}
	payload, _ := json.Marshal(map[string]any{"actor_agent_id": actorAgentID, "message": message})
	evt := &domain.Event{
		ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
		TaskID:       taskID,
		ActorAgentID: actorAgentID,
		Type:         domain.EventTaskStatusUpdated,
		Payload:      string(payload),
		CreatedAt:    now,
	}

	err := s.store.UpdateTaskStatus(ctx, taskID, actorAgentID, msg, evt)
	if err != nil {
		s.logger.Warn("task status update failed",
			slog.String("task_id", taskID),
			slog.String("actor_agent_id", actorAgentID),
			slog.String("error", err.Error()),
		)
		return err
	}

	s.logger.Info("task status updated",
		slog.String("task_id", taskID),
		slog.String("actor_agent_id", actorAgentID),
		slog.String("message", message),
	)
	s.broker.Publish(taskID)
	return nil
}

func (s *Service) SendMessage(ctx context.Context, taskID string, senderAgentID string, content string) error {
	if err := s.verifyAgentReady(ctx, senderAgentID, "sender agent"); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	msg := &domain.Message{
		ID:            fmt.Sprintf("msg-%s", uuid.New().String()),
		TaskID:        taskID,
		SenderAgentID: senderAgentID,
		Kind:          domain.MessageKindSupplement,
		Content:       content,
		CreatedAt:     now,
	}
	payload, _ := json.Marshal(map[string]any{"sender_agent_id": senderAgentID, "content": content})
	evt := &domain.Event{
		ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
		TaskID:       taskID,
		ActorAgentID: senderAgentID,
		Type:         domain.EventTaskMessageSent,
		Payload:      string(payload),
		CreatedAt:    now,
	}

	err := s.store.SendMessage(ctx, taskID, senderAgentID, msg, evt)
	if err != nil {
		s.logger.Warn("send message failed",
			slog.String("task_id", taskID),
			slog.String("sender_agent_id", senderAgentID),
			slog.String("error", err.Error()),
		)
		return err
	}

	s.logger.Info("task message sent",
		slog.String("task_id", taskID),
		slog.String("sender_agent_id", senderAgentID),
	)
	s.broker.Publish(taskID)

	// Trigger supplemental notification to target agent with identity reminder
	task, err := s.store.GetTask(ctx, taskID)
	if err == nil {
		targetAgent, targetProfile, _, err := s.store.GetAgentSession(ctx, task.TargetAgentID)
		if err == nil && targetAgent.Connector == domain.ConnectorTmux && s.connector != nil {
			instrPath := ""
			if targetProfile != nil {
				instrPath = targetProfile.InstructionsPath
			}
			deliv := s.connector.NotifyTask(ctx, targetAgent.Address, targetAgent.ID, targetAgent.Role, instrPath, task.ID, false)
			delivNow := time.Now().UTC().Format(time.RFC3339Nano)
			if deliv.Disposition == connector.DispositionNotified {
				p, _ := json.Marshal(map[string]any{"pane_id": deliv.PaneID, "disposition": deliv.Disposition})
				evtNotif := &domain.Event{
					ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
					TaskID:       task.ID,
					ActorAgentID: "agentbus",
					Type:         domain.EventTaskNotified,
					Payload:      string(p),
					CreatedAt:    delivNow,
				}
				if _, err := s.store.AddEvent(ctx, evtNotif); err != nil {
					s.logger.Error("failed to record notification event",
						slog.String("task_id", task.ID),
						slog.String("error", err.Error()),
					)
				} else {
					s.broker.Publish(task.ID)
				}
			} else {
				p, _ := json.Marshal(map[string]any{"error": deliv.Error, "pane_id": deliv.PaneID, "disposition": deliv.Disposition})
				evtNotif := &domain.Event{
					ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
					TaskID:       task.ID,
					ActorAgentID: "agentbus",
					Type:         domain.EventTaskDeliveryFailed,
					Payload:      string(p),
					CreatedAt:    delivNow,
				}
				if _, err := s.store.AddEvent(ctx, evtNotif); err != nil {
					s.logger.Error("failed to record delivery failure event",
						slog.String("task_id", task.ID),
						slog.String("error", err.Error()),
					)
				} else {
					s.broker.Publish(task.ID)
				}
			}
		}
	}

	return nil
}

func (s *Service) CompleteTask(ctx context.Context, taskID string, actorAgentID string, result string) (*domain.Task, error) {
	if err := s.verifyAgentReady(ctx, actorAgentID, "actor agent"); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload, _ := json.Marshal(map[string]any{"actor_agent_id": actorAgentID, "result": result, "status": domain.TaskStatusSucceeded})
	evt := &domain.Event{
		ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
		TaskID:       taskID,
		ActorAgentID: actorAgentID,
		Type:         domain.EventTaskSucceeded,
		Payload:      string(payload),
		CreatedAt:    now,
	}

	t, err := s.store.CompleteTask(ctx, taskID, actorAgentID, result, evt)
	if err != nil {
		s.logger.Warn("task complete failed",
			slog.String("task_id", taskID),
			slog.String("actor_agent_id", actorAgentID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("task completed",
		slog.String("task_id", taskID),
		slog.String("actor_agent_id", actorAgentID),
		slog.String("status", string(t.Status)),
	)
	s.broker.Publish(taskID)
	return t, nil
}

func (s *Service) FailTask(ctx context.Context, taskID string, actorAgentID string, errStr string) (*domain.Task, error) {
	if err := s.verifyAgentReady(ctx, actorAgentID, "actor agent"); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload, _ := json.Marshal(map[string]any{"actor_agent_id": actorAgentID, "error": errStr, "status": domain.TaskStatusFailed})
	evt := &domain.Event{
		ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
		TaskID:       taskID,
		ActorAgentID: actorAgentID,
		Type:         domain.EventTaskFailed,
		Payload:      string(payload),
		CreatedAt:    now,
	}

	t, err := s.store.FailTask(ctx, taskID, actorAgentID, errStr, evt)
	if err != nil {
		s.logger.Warn("task fail failed",
			slog.String("task_id", taskID),
			slog.String("actor_agent_id", actorAgentID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("task failed",
		slog.String("task_id", taskID),
		slog.String("actor_agent_id", actorAgentID),
		slog.String("status", string(t.Status)),
		slog.String("task_error", errStr),
	)
	s.broker.Publish(taskID)
	return t, nil
}

func (s *Service) CancelTask(ctx context.Context, taskID string, actorAgentID string) (*domain.Task, error) {
	if err := s.verifyAgentReady(ctx, actorAgentID, "actor agent"); err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	payload, _ := json.Marshal(map[string]any{"actor_agent_id": actorAgentID, "status": domain.TaskStatusCanceled})
	evt := &domain.Event{
		ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
		TaskID:       taskID,
		ActorAgentID: actorAgentID,
		Type:         domain.EventTaskCanceled,
		Payload:      string(payload),
		CreatedAt:    now,
	}

	t, err := s.store.CancelTask(ctx, taskID, actorAgentID, evt)
	if err != nil {
		s.logger.Warn("task cancel failed",
			slog.String("task_id", taskID),
			slog.String("actor_agent_id", actorAgentID),
			slog.String("error", err.Error()),
		)
		return nil, err
	}

	s.logger.Info("task canceled",
		slog.String("task_id", taskID),
		slog.String("actor_agent_id", actorAgentID),
		slog.String("status", string(t.Status)),
	)
	s.broker.Publish(taskID)
	return t, nil
}

func (s *Service) GetEvents(ctx context.Context, taskID string, callerAgentID string, afterSeq int64, timeout time.Duration) ([]*domain.Event, error) {
	caller := strings.TrimSpace(callerAgentID)
	if caller == "" {
		return nil, domain.ErrInvalidInput("caller --agent is required to watch events")
	}
	if err := s.verifyAgentReady(ctx, caller, "caller agent"); err != nil {
		return nil, err
	}

	// Check task exists and authorization
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	if task.SenderAgentID != caller && task.TargetAgentID != caller {
		return nil, fmt.Errorf("%w: agent '%s' is neither sender nor target for task '%s'", domain.ErrUnauthorized, caller, taskID)
	}

	// Fetch current events
	events, err := s.store.GetEvents(ctx, taskID, afterSeq)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 || timeout <= 0 || task.IsTerminal() {
		return events, nil
	}

	// Subscribe for new events
	ch, unsubscribe := s.broker.Subscribe(taskID)
	defer unsubscribe()

	// Double check in case an event arrived just before subscribing
	events, err = s.store.GetEvents(ctx, taskID, afterSeq)
	if err != nil || len(events) > 0 {
		return events, err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return s.store.GetEvents(ctx, taskID, afterSeq)
	case <-ch:
		return s.store.GetEvents(ctx, taskID, afterSeq)
	}
}

func (s *Service) GetTaskMessages(ctx context.Context, taskID string, callerAgentID string) ([]*domain.Message, error) {
	caller := strings.TrimSpace(callerAgentID)
	if caller == "" {
		return nil, domain.ErrInvalidInput("caller --agent is required to get task messages")
	}
	if err := s.verifyAgentReady(ctx, caller, "caller agent"); err != nil {
		return nil, err
	}

	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	if task.SenderAgentID != caller && task.TargetAgentID != caller {
		return nil, fmt.Errorf("%w: agent '%s' is neither sender nor target for task '%s'", domain.ErrUnauthorized, caller, taskID)
	}

	return s.store.GetTaskMessages(ctx, taskID)
}

type RecordRuntimeEventRequest struct {
	Agent     string          `json:"agent"`
	Runtime   string          `json:"runtime"`
	Event     string          `json:"event"`
	SessionID string          `json:"session_id,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type RecordRuntimeEventResponse struct {
	TaskID        string `json:"task_id"`
	Status        string `json:"status"`
	EventSequence int64  `json:"event_sequence"`
}

func (s *Service) RecordRuntimeEvent(ctx context.Context, taskID string, req RecordRuntimeEventRequest) (*RecordRuntimeEventResponse, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, domain.ErrInvalidInput("task ID cannot be empty")
	}
	if strings.TrimSpace(req.Agent) == "" {
		return nil, domain.ErrInvalidInput("agent is required")
	}
	if req.Runtime != "agy" {
		return nil, domain.ErrInvalidInput(fmt.Sprintf("unsupported runtime '%s' (only 'agy' is supported in V0.2)", req.Runtime))
	}
	if !domain.IsValidAGYEvent(req.Event) {
		return nil, domain.ErrInvalidInput(fmt.Sprintf("invalid or unsupported event '%s'", req.Event))
	}

	// 1. Verify agent session is ready
	if err := s.verifyAgentReady(ctx, req.Agent, "actor agent"); err != nil {
		return nil, err
	}

	// 2. Verify agent profile runtime matches
	_, profile, _, err := s.store.GetAgentSession(ctx, req.Agent)
	if err != nil {
		return nil, err
	}
	if profile.Runtime != req.Runtime {
		return nil, domain.ErrInvalidInput(fmt.Sprintf("agent '%s' configured runtime '%s' does not match request runtime '%s'", req.Agent, profile.Runtime, req.Runtime))
	}

	// 3. Verify Task exists and target agent matches actor agent
	task, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task.TargetAgentID != req.Agent {
		return nil, fmt.Errorf("%w: actor '%s' is not target agent '%s' for task '%s'", domain.ErrUnauthorized, req.Agent, task.TargetAgentID, taskID)
	}

	// 4. Validate payload: must be a single non-null JSON object
	trimmedPayload := bytes.TrimSpace(req.Payload)
	if len(trimmedPayload) == 0 || string(trimmedPayload) == "null" {
		return nil, domain.ErrInvalidInput("payload is required and must be a valid JSON object")
	}
	var rawPayload map[string]any
	dec := json.NewDecoder(bytes.NewReader(trimmedPayload))
	if err := dec.Decode(&rawPayload); err != nil || rawPayload == nil {
		return nil, domain.ErrInvalidInput(fmt.Sprintf("payload must be a valid JSON object (cannot be array, primitive or null): %v", err))
	}
	if dec.More() {
		return nil, domain.ErrInvalidInput("payload contains multiple JSON values")
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return nil, domain.ErrInvalidInput("payload contains trailing characters")
	}

	normPayload := map[string]any{
		"runtime":      req.Runtime,
		"event":        req.Event,
		"session_id":   req.SessionID,
		"hook_payload": rawPayload,
	}
	normBytes, err := json.Marshal(normPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal normalized runtime event payload: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	evt := &domain.Event{
		ID:           fmt.Sprintf("evt-%s", uuid.New().String()),
		TaskID:       taskID,
		ActorAgentID: req.Agent,
		Type:         domain.EventRuntimeObserved,
		Payload:      string(normBytes),
		CreatedAt:    now,
	}

	seq, err := s.store.AddEvent(ctx, evt)
	if err != nil {
		s.logger.Warn("failed to record runtime event",
			slog.String("task_id", taskID),
			slog.String("agent_id", req.Agent),
			slog.String("event", req.Event),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to store runtime event: %w", err)
	}

	s.logger.Info("runtime event recorded",
		slog.String("task_id", taskID),
		slog.String("agent_id", req.Agent),
		slog.String("event", req.Event),
		slog.Int64("sequence", seq),
	)

	s.broker.Publish(taskID)

	// Re-fetch latest task after AddEvent to avoid returning stale status during concurrent transitions
	latestTask, err := s.store.GetTask(ctx, taskID)
	if err != nil {
		s.logger.Warn("failed to refetch task after recording runtime event",
			slog.String("task_id", taskID),
			slog.String("error", err.Error()),
		)
		return nil, fmt.Errorf("failed to fetch updated task status: %w", err)
	}

	return &RecordRuntimeEventResponse{
		TaskID:        taskID,
		Status:        string(latestTask.Status),
		EventSequence: seq,
	}, nil
}
