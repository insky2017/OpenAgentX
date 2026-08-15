package service

import (
	"context"
	"encoding/json"
	"fmt"
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

	// Trigger connector notification
	targetAgent, err := s.store.GetAgent(ctx, createdTask.TargetAgentID)
	if err == nil && targetAgent.Connector == domain.ConnectorTmux && s.connector != nil {
		deliv := s.connector.Notify(ctx, targetAgent.Address, targetAgent.ID, createdTask.ID, true)
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
	if _, err := s.store.GetAgent(ctx, caller); err != nil {
		return nil, fmt.Errorf("%w: caller agent '%s'", domain.ErrAgentNotFound, caller)
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
	if _, err := s.store.GetAgent(ctx, agent); err != nil {
		return nil, fmt.Errorf("%w: agent '%s'", domain.ErrAgentNotFound, agent)
	}
	return s.store.ListTasks(ctx, agent, status)
}

func (s *Service) AckTask(ctx context.Context, taskID string, actorAgentID string) (*domain.Task, error) {
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

	// Trigger supplemental notification to target agent
	task, err := s.store.GetTask(ctx, taskID)
	if err == nil {
		targetAgent, err := s.store.GetAgent(ctx, task.TargetAgentID)
		if err == nil && targetAgent.Connector == domain.ConnectorTmux && s.connector != nil {
			deliv := s.connector.Notify(ctx, targetAgent.Address, targetAgent.ID, task.ID, false)
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
	if _, err := s.store.GetAgent(ctx, caller); err != nil {
		return nil, fmt.Errorf("%w: caller agent '%s'", domain.ErrAgentNotFound, caller)
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
	if _, err := s.store.GetAgent(ctx, caller); err != nil {
		return nil, fmt.Errorf("%w: caller agent '%s'", domain.ErrAgentNotFound, caller)
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
