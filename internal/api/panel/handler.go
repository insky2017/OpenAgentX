package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/auth/web"
	"openagentx/internal/controlplane"
	"openagentx/internal/domain"
)

type State interface {
	ListAgents(context.Context, int) ([]domain.AgentIdentity, error)
	ListWorkers(context.Context, int) ([]domain.WorkerInstance, error)
	ListTasks(context.Context, string, int) ([]domain.Task, error)
	ListJournal(context.Context, int64, int) ([]domain.JournalEvent, error)
	GetTask(context.Context, string) (*domain.Task, error)
	ListMessages(context.Context, string) ([]domain.Message, error)
	ListMailbox(context.Context, string, int64, int) ([]domain.MailboxItem, error)
	GetRunAttempt(context.Context, string) (*domain.RunAttempt, error)
	ListPendingApprovals(context.Context, int) ([]domain.ApprovalRequest, error)
}

type Handler struct {
	state    State
	commands *controlplane.CommandService
	auth     *web.Manager
	mux      *http.ServeMux
}

func NewHandler(state State, commands *controlplane.CommandService, auth *web.Manager) (*Handler, error) {
	if state == nil || commands == nil || auth == nil {
		return nil, fmt.Errorf("panel state, commands and auth are required")
	}
	h := &Handler{state: state, commands: commands, auth: auth, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /api/observe/v1/overview", h.overview)
	h.mux.HandleFunc("GET /api/observe/v1/agents", h.agents)
	h.mux.HandleFunc("GET /api/observe/v1/tasks", h.tasks)
	h.mux.HandleFunc("GET /api/observe/v1/tasks/{taskID}", h.task)
	h.mux.HandleFunc("GET /api/observe/v1/mailboxes", h.mailboxes)
	h.mux.HandleFunc("GET /api/observe/v1/run-attempts/{runID}", h.run)
	h.mux.HandleFunc("GET /api/observe/v1/events/stream", h.events)
	h.mux.HandleFunc("POST /api/control/v1/tasks", h.createTask)
	h.mux.HandleFunc("POST /api/control/v1/tasks/{taskID}/messages", h.createMessage)
	h.mux.HandleFunc("POST /api/control/v1/tasks/{taskID}/cancel", h.cancelTask)
	h.mux.HandleFunc("POST /api/control/v1/approvals/{approvalID}/decisions", h.decideApproval)
	return h, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	h.mux.ServeHTTP(w, r)
}
func (h *Handler) session(w http.ResponseWriter, r *http.Request, write bool) (*web.Session, bool) {
	s, err := h.auth.Authenticate(r)
	if err != nil {
		http.Error(w, "unauthorized", 401)
		return nil, false
	}
	if write {
		if err := web.RequireRole(s, web.RoleOperator); err != nil {
			http.Error(w, "forbidden", 403)
			return nil, false
		}
		if err := web.ValidateCSRF(s, r.Header.Get("X-CSRF-Token")); err != nil {
			http.Error(w, "invalid csrf token", 403)
			return nil, false
		}
	}
	return s, true
}
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	agents, _ := h.state.ListAgents(r.Context(), 100)
	workers, _ := h.state.ListWorkers(r.Context(), 100)
	tasks, _ := h.state.ListTasks(r.Context(), "", 100)
	approvals, _ := h.state.ListPendingApprovals(r.Context(), 100)
	writeJSON(w, map[string]any{"agents": agents, "workers": workers, "tasks": tasks, "approvals": approvals, "server_time": time.Now().UTC()})
}
func (h *Handler) agents(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	a, e := h.state.ListAgents(r.Context(), 100)
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	writeJSON(w, a)
}
func (h *Handler) tasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	agent := r.URL.Query().Get("agent_id")
	t, e := h.state.ListTasks(r.Context(), agent, 100)
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	writeJSON(w, t)
}
func (h *Handler) task(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	t, e := h.state.GetTask(r.Context(), r.PathValue("taskID"))
	if e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	m, _ := h.state.ListMessages(r.Context(), t.ID)
	writeJSON(w, map[string]any{"task": t, "messages": m})
}
func (h *Handler) mailboxes(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	items, e := h.state.ListMailbox(r.Context(), r.URL.Query().Get("agent_id"), 0, 100)
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	writeJSON(w, items)
}
func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	v, e := h.state.GetRunAttempt(r.Context(), r.PathValue("runID"))
	if e != nil {
		http.Error(w, e.Error(), 404)
		return
	}
	writeJSON(w, v)
}
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.session(w, r, false); !ok {
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	for {
		events, e := h.state.ListJournal(r.Context(), after, 100)
		if e != nil {
			return
		}
		for _, ev := range events {
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.Sequence, b)
			after = ev.Sequence
		}
		fl.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-time.After(time.Second):
		}
	}
}
func (h *Handler) createTask(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	var req openapi.CreateTaskRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	req.SenderPrincipalID = s.User.ID
	v, e := h.commands.CreateTask(r.Context(), s.User.ID, req)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	writeJSON(w, v)
}
func (h *Handler) createMessage(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	var req openapi.CreateMessageRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	v, e := h.commands.CreateMessage(r.Context(), s.User.ID, r.PathValue("taskID"), req)
	if e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	writeJSON(w, v)
}
func (h *Handler) cancelTask(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	var req openapi.CancelTaskRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	req.RequestedBy = s.User.ID
	v, e := h.commands.CancelTask(r.Context(), s.User.ID, r.PathValue("taskID"), req)
	if e != nil {
		status := 400
		if errors.Is(e, domain.ErrTerminalState) {
			status = 409
		}
		http.Error(w, e.Error(), status)
		return
	}
	writeJSON(w, v)
}

func (h *Handler) decideApproval(w http.ResponseWriter, r *http.Request) {
	s, ok := h.session(w, r, true)
	if !ok {
		return
	}
	var req openapi.DecideApprovalRequest
	if openapi.DecodeStrictJSON(r.Body, &req) != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	result, err := h.commands.DecideApproval(r.Context(), s.User.ID, r.PathValue("approvalID"), req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, result)
}
