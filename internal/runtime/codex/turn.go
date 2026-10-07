package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"openagentx/internal/domain"
	openruntime "openagentx/internal/runtime"
	"openagentx/internal/safeoutput"
)

type pendingApproval struct {
	RPCID     json.RawMessage
	Method    string
	ExpiresAt time.Time
}
type turnHandle struct {
	adapter      *Adapter
	client       *RPCClient
	ctx          context.Context
	cancel       context.CancelFunc
	threadID     string
	turnID       string
	sink         openruntime.EventSink
	events       <-chan RPCMessage
	unsubscribe  func()
	done         chan struct{}
	mu           sync.Mutex
	pending      map[string]pendingApproval
	result       openruntime.TurnResult
	err          error
	initialError error
	cancelSignal chan struct{}
	cancelOnce   sync.Once
	processes    *processTracker
	commands     map[string]providerItem
}

func (h *turnHandle) collect() {
	defer h.unsubscribe()
	defer h.cancel()
	result := openruntime.TurnResult{Status: openruntime.TurnResultUncertain, ProviderSessionID: h.threadID}
	var streamErr error = h.initialError
	deadlineReason := ""
	finalByID := map[string]string{}
	toolSeen := false
	pendingOtherTools := map[string]string{}
	var finalOrder []string
	emitted := 0
	ctxDone := h.ctx.Done()
	cancelSignal := h.cancelSignal
	var stopTimer *time.Timer
	var stopDeadline <-chan time.Time
	finish := func() {
		if stopTimer != nil {
			stopTimer.Stop()
		}
		if streamErr != nil {
			result.Status = openruntime.TurnResultUncertain
			result.FinalReply = false
			result.Error = "Codex Runtime ended without a fully verified protocol result: " + safeoutput.RedactText(streamErr.Error())
		}
		if deadlineReason != "" && result.Status != openruntime.TurnResultSucceeded {
			if result.Error != "" {
				result.Error = deadlineReason + "; " + result.Error
			} else {
				result.Error = deadlineReason
			}
		}
		result = safeoutput.SanitizeTurnResult(result)
		h.adapter.mu.Lock()
		if h.adapter.active == h {
			h.adapter.active = nil
			h.adapter.state.State = "idle"
			if result.Status == openruntime.TurnResultUncertain {
				h.adapter.state.State = "uncertain"
				h.adapter.unresolved = true
			}
			_ = h.adapter.writeStateLocked()
		}
		h.adapter.mu.Unlock()
		h.mu.Lock()
		h.result = result
		h.err = streamErr
		h.mu.Unlock()
		close(h.done)
	}
	defer finish()
	for {
		select {
		case <-ctxDone:
			ctxDone = nil
			if errors.Is(h.ctx.Err(), context.DeadlineExceeded) {
				deadline, _ := h.ctx.Deadline()
				deadlineReason = "OAX 执行时限已到，已请求停止本轮（deadline_exceeded；截止 " + deadline.UTC().Format(time.RFC3339) + "）；已完成的操作可能仍有效，任务未自动重试"
			}
			cancelCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := h.RequestCancel(cancelCtx)
			cancel()
			if err != nil {
				streamErr = errors.Join(streamErr, fmt.Errorf("deadline cancellation not acknowledged: %w", err))
			}
			h.cancelOnce.Do(func() { close(h.cancelSignal) })
		case <-cancelSignal:
			cancelSignal = nil
			stopTimer = time.NewTimer(10 * time.Second)
			stopDeadline = stopTimer.C
		case <-stopDeadline:
			streamErr = errors.Join(streamErr, errors.New("Codex cancellation terminal event missing"))
			// Only our owned process may be stopped. A remote host is never killed.
			h.adapter.mu.Lock()
			if h.adapter.config.Endpoint == "" {
				h.adapter.stopLocked()
			}
			h.adapter.mu.Unlock()
			return
		case message, ok := <-h.events:
			if !ok {
				streamErr = errors.Join(streamErr, h.client.Err())
				return
			}
			var p struct {
				ThreadID   string          `json:"threadId"`
				TurnID     string          `json:"turnId"`
				Turn       providerTurn    `json:"turn"`
				Item       providerItem    `json:"item"`
				Delta      string          `json:"delta"`
				TokenUsage json.RawMessage `json:"tokenUsage"`
			}
			if len(message.Params) > 0 {
				if err := json.Unmarshal(message.Params, &p); err != nil {
					streamErr = errors.Join(streamErr, err)
					continue
				}
			}
			if p.ThreadID != h.threadID {
				continue
			}
			id := p.TurnID
			if id == "" {
				id = p.Turn.ID
			}
			if id != "" && id != h.turnID {
				continue
			}
			if len(message.ID) > 0 {
				if err := h.serverRequest(message); err != nil {
					streamErr = errors.Join(streamErr, err)
				}
				continue
			}
			switch message.Method {
			case "item/started":
				if p.Item.Type == "fileChange" || p.Item.Type == "mcpToolCall" || p.Item.Type == "dynamicToolCall" {
					pendingOtherTools[p.Item.ID] = p.Item.Type
				}
				if p.Item.Type == "commandExecution" {
					h.mu.Lock()
					h.commands[p.Item.ID] = p.Item
					h.mu.Unlock()
				}
				if p.Item.Type == "commandExecution" || p.Item.Type == "fileChange" || p.Item.Type == "mcpToolCall" || p.Item.Type == "dynamicToolCall" {
					toolSeen = true
					if h.processes != nil {
						h.processes.capture()
					}
				}
			case "item/completed":
				if p.TurnID != h.turnID || p.Item.ID == "" {
					streamErr = errors.Join(streamErr, errors.New("Codex completed item omitted exact turn or item identity"))
					continue
				}
				delete(pendingOtherTools, p.Item.ID)
				if p.Item.Type == "commandExecution" {
					h.mu.Lock()
					h.commands[p.Item.ID] = p.Item
					h.mu.Unlock()
				}
				if p.Item.Type == "agentMessage" && p.Item.Phase == "final_answer" {
					if _, exists := finalByID[p.Item.ID]; !exists {
						finalOrder = append(finalOrder, p.Item.ID)
					}
					finalByID[p.Item.ID] = p.Item.Text
				}
				if emitted < openruntime.MaxPublicOutputEvents && h.sink != nil {
					text := p.Item.Text
					if text == "" {
						text = "Codex completed " + p.Item.Type
					}
					payload, _ := json.Marshal(map[string]any{"stage": "output", "status": "running", "text": safeoutput.ProjectText(text).Text, "has_output": true})
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					err := h.sink.Emit(ctx, openruntime.RuntimeEvent{Type: "turn.output", Payload: payload, OccurredAt: time.Now().UTC()})
					cancel()
					if err != nil {
						streamErr = errors.Join(streamErr, err)
					}
					emitted++
				}
			case "thread/tokenUsage/updated":
				result.UsageJSON = p.TokenUsage
			case "turn/completed":
				if p.Turn.ID != h.turnID {
					continue
				}
				// turn/completed may only contain a summary. Authoritative complete
				// answers come from item/completed with phase=final_answer.
				var parts []string
				for _, id := range finalOrder {
					parts = append(parts, finalByID[id])
				}
				result.Result = strings.Join(parts, "\n\n")
				switch p.Turn.Status {
				case "completed":
					if len(p.Turn.Error) > 0 && string(p.Turn.Error) != "null" {
						streamErr = errors.Join(streamErr, errors.New("Codex successful terminal contained an error"))
					}
					result.Status = openruntime.TurnResultSucceeded
					result.FinalReply = strings.TrimSpace(result.Result) != ""
					if !result.FinalReply {
						streamErr = errors.Join(streamErr, errors.New("Codex completed without an authoritative final reply"))
					}
				case "interrupted":
					if len(pendingOtherTools) > 0 {
						streamErr = errors.Join(streamErr, errors.New("interrupted non-command tool completion is unverified"))
					}
					stopErr := h.terminateToolProcesses()
					if stopErr == nil {
						result.Status = openruntime.TurnResultCanceled
					} else if h.processes != nil {
						refs, err := h.processes.stop()
						report, _ := json.MarshalIndent(map[string]any{"thread_id": h.threadID, "turn_id": h.turnID, "checked_at": time.Now().UTC(), "processes": refs, "stopped": err == nil}, "", "  ")
						_ = os.WriteFile(filepath.Join(h.adapter.config.StateDir, "cancel-processes-"+h.turnID+".json"), report, 0600)
						h.adapter.mu.Lock()
						h.adapter.stopLocked()
						h.adapter.mu.Unlock()
						if err != nil {
							streamErr = errors.Join(streamErr, err)
						} else {
							result.Status = openruntime.TurnResultCanceled
						}
					} else if toolSeen {
						streamErr = errors.Join(streamErr, errors.New("external Codex interrupted a turn but tool termination is unverified"))
					} else {
						result.Status = openruntime.TurnResultCanceled
					}
				case "failed":
					result.Status = openruntime.TurnResultFailed
					result.Error = "Codex provider reported a failed turn"
					if len(p.Turn.Error) > 0 && string(p.Turn.Error) != "null" {
						result.Error += "; " + safeoutput.RedactText(string(p.Turn.Error))
					}
				default:
					streamErr = errors.Join(streamErr, fmt.Errorf("unsupported Codex terminal status %q", p.Turn.Status))
				}
				return
			}
		}
	}
}

func (h *turnHandle) serverRequest(m RPCMessage) error {
	switch m.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		digest := sha256.Sum256(append(append([]byte(h.threadID+":"+h.turnID+":"), m.ID...), m.Params...))
		id := "codex-approval-" + hex.EncodeToString(digest[:16])
		expires := time.Now().Add(10 * time.Minute)
		h.mu.Lock()
		h.pending[id] = pendingApproval{RPCID: append(json.RawMessage(nil), m.ID...), Method: m.Method, ExpiresAt: expires}
		h.mu.Unlock()
		payload, _ := json.Marshal(map[string]any{"approval_request_id": id, "scope_digest": hex.EncodeToString(digest[:]), "expires_at": expires})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := h.sink.Emit(ctx, openruntime.RuntimeEvent{Type: "approval.requested", Payload: payload, OccurredAt: time.Now().UTC()}); err != nil {
			_ = h.client.Respond(ctx, m.ID, map[string]any{"decision": "decline"})
			return fmt.Errorf("persist Codex approval: %w", err)
		}
		return nil
	default:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = h.client.RespondError(ctx, m.ID, -32601, "OpenAgentX does not support this interactive request")
		return fmt.Errorf("unsupported Codex server request %s", m.Method)
	}
}

func (h *turnHandle) Wait(ctx context.Context) (openruntime.TurnResult, error) {
	select {
	case <-h.done:
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.result, h.err
	case <-ctx.Done():
		return openruntime.TurnResult{}, ctx.Err()
	}
}
func (h *turnHandle) Steer(ctx context.Context, m domain.Message) error {
	select {
	case <-h.done:
		return errors.New("Codex turn already ended")
	default:
	}
	return h.client.Call(ctx, "turn/steer", map[string]any{"threadId": h.threadID, "expectedTurnId": h.turnID, "input": []map[string]any{{"type": "text", "text": m.Content}}, "clientUserMessageId": m.ID}, nil)
}
func (h *turnHandle) RequestCancel(ctx context.Context) error {
	select {
	case <-h.done:
		return nil
	default:
	}
	if h.processes != nil {
		h.processes.capture()
	}
	err := h.client.Call(ctx, "turn/interrupt", map[string]any{"threadId": h.threadID, "turnId": h.turnID}, nil)
	h.cancelOnce.Do(func() { close(h.cancelSignal) })
	return err
}
func (h *turnHandle) DecideApproval(ctx context.Context, d domain.ApprovalDecision) error {
	h.mu.Lock()
	approval, ok := h.pending[d.ApprovalRequestID]
	if !ok || time.Now().After(approval.ExpiresAt) {
		h.mu.Unlock()
		return errors.New("Codex approval is stale or not owned by this turn")
	}
	delete(h.pending, d.ApprovalRequestID)
	h.mu.Unlock()
	decision := "decline"
	if d.Decision == domain.ApprovalDecisionApprove {
		decision = "accept"
	} else if d.Decision != domain.ApprovalDecisionReject {
		return errors.New("unsupported Codex approval decision")
	}
	return h.client.Respond(ctx, approval.RPCID, map[string]any{"decision": decision})
}

type finishedHandle struct {
	result openruntime.TurnResult
	err    error
}

func (h *finishedHandle) Wait(context.Context) (openruntime.TurnResult, error) {
	return h.result, h.err
}
func (h *finishedHandle) Steer(context.Context, domain.Message) error {
	return openruntime.ErrSteerUnsupported
}
func (h *finishedHandle) DecideApproval(context.Context, domain.ApprovalDecision) error {
	return openruntime.ErrApprovalUnsupported
}
func (h *finishedHandle) RequestCancel(context.Context) error {
	return openruntime.ErrCancelUnsupported
}
