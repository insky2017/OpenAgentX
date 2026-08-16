package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"agentbus/internal/domain"
	"agentbus/internal/service"
)

type Server struct {
	svc            *service.Service
	socketPath     string
	logger         *slog.Logger
	httpServer     *http.Server
	listener       net.Listener
	socketFileInfo os.FileInfo
}

func NewServer(svc *service.Service, socketPath string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	s := &Server{
		svc:        svc,
		socketPath: socketPath,
		logger:     logger,
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Handler:      s.requestMiddleware(mux),
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	return s
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/agents", s.handleRegisterAgent)
	mux.HandleFunc("GET /api/v1/agents", s.handleListAgents)
	mux.HandleFunc("GET /api/v1/agents/{id}", s.handleGetAgent)

	// V0.1 Attach / Bootstrap / Session
	mux.HandleFunc("POST /api/v1/agents/attach", s.handleAttachAgent)
	mux.HandleFunc("POST /api/v1/agents/{id}/bootstrap", s.handleBootstrapAgent)
	mux.HandleFunc("GET /api/v1/agents/{id}/session", s.handleGetSession)
	mux.HandleFunc("POST /api/v1/sessions/{agent_id}/ready", s.handleReadySession)

	mux.HandleFunc("POST /api/v1/tasks", s.handleSubmitTask)
	mux.HandleFunc("GET /api/v1/tasks", s.handleListTasks)
	mux.HandleFunc("GET /api/v1/tasks/{id}", s.handleGetTask)

	mux.HandleFunc("POST /api/v1/tasks/{id}/ack", s.handleAckTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/status", s.handleUpdateTaskStatus)
	mux.HandleFunc("POST /api/v1/tasks/{id}/send", s.handleSendMessage)
	mux.HandleFunc("POST /api/v1/tasks/{id}/complete", s.handleCompleteTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/fail", s.handleFailTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/cancel", s.handleCancelTask)
	mux.HandleFunc("POST /api/v1/tasks/{id}/runtime-events", s.handleRecordRuntimeEvent)
	mux.HandleFunc("GET /api/v1/tasks/{id}/events", s.handleGetEvents)
}

func (s *Server) requestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// Limit request body size to 1 MiB
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		next.ServeHTTP(w, r)
		s.logger.Debug("http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

func (s *Server) Start(ctx context.Context) error {
	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create socket directory: %w", err)
	}

	if fi, err := os.Lstat(s.socketPath); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("socket path '%s' already exists and is not a socket file", s.socketPath)
		}

		// Try connecting to verify if a daemon is actively listening
		conn, dialErr := net.DialTimeout("unix", s.socketPath, 500*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			return fmt.Errorf("another agentbus daemon is already running and listening on socket '%s'", s.socketPath)
		}

		// Check if it is a stale socket (connection refused)
		if errors.Is(dialErr, syscall.ECONNREFUSED) || strings.Contains(dialErr.Error(), "connection refused") {
			s.logger.Info("removing stale socket file", slog.String("socket", s.socketPath))
			if err := os.Remove(s.socketPath); err != nil {
				return fmt.Errorf("failed to remove stale socket: %w", err)
			}
		} else {
			return fmt.Errorf("failed to probe existing socket '%s': %w", s.socketPath, dialErr)
		}
	}

	l, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on unix socket '%s': %w", s.socketPath, err)
	}
	s.listener = l

	if ul, ok := l.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}

	if err := os.Chmod(s.socketPath, 0600); err != nil {
		s.logger.Warn("failed to chmod socket file", slog.String("error", err.Error()))
	}

	fi, err := os.Lstat(s.socketPath)
	if err != nil {
		l.Close()
		return fmt.Errorf("failed to stat created socket '%s': %w", s.socketPath, err)
	}
	s.socketFileInfo = fi

	s.logger.Info("agentbus daemon server starting", slog.String("socket", s.socketPath))

	errCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("agentbus daemon shutting down gracefully")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.httpServer.Shutdown(shutdownCtx)
		s.cleanupSocket()
		return nil
	case err := <-errCh:
		s.cleanupSocket()
		return err
	}
}

func (s *Server) cleanupSocket() {
	if s.socketFileInfo == nil {
		return
	}
	currFi, err := os.Lstat(s.socketPath)
	if err != nil {
		return
	}
	if currFi.Mode()&os.ModeSocket == 0 {
		return
	}
	if !isSameSocket(s.socketFileInfo, currFi) {
		s.logger.Warn("socket file on disk was replaced by another entity, skipping cleanup",
			slog.String("socket", s.socketPath),
		)
		return
	}
	_ = os.Remove(s.socketPath)
}

func isSameSocket(fi1, fi2 os.FileInfo) bool {
	if fi1 == nil || fi2 == nil {
		return false
	}
	if !os.SameFile(fi1, fi2) {
		return false
	}
	st1, ok1 := fi1.Sys().(*syscall.Stat_t)
	st2, ok2 := fi2.Sys().(*syscall.Stat_t)
	if ok1 && ok2 {
		if st1.Ino != st2.Ino || st1.Dev != st2.Dev || st1.Ctim != st2.Ctim || st1.Mtim != st2.Mtim {
			return false
		}
	}
	return true
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	code := "INTERNAL_ERROR"

	var domainErr *domain.DomainError
	if errors.As(err, &domainErr) {
		code = domainErr.Code
		switch domainErr.Code {
		case "INVALID_INPUT":
			status = http.StatusBadRequest
		case "FORBIDDEN":
			status = http.StatusForbidden
		case "CONFLICT":
			status = http.StatusConflict
		}
	} else if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrAgentNotFound) || errors.Is(err, domain.ErrTaskNotFound) {
		status = http.StatusNotFound
		code = "NOT_FOUND"
	} else if errors.Is(err, domain.ErrAgentSessionNotFound) {
		status = http.StatusNotFound
		code = "AGENT_SESSION_NOT_FOUND"
	} else if errors.Is(err, domain.ErrAgentProfileNotFound) {
		status = http.StatusNotFound
		code = "AGENT_PROFILE_NOT_FOUND"
	} else if errors.Is(err, domain.ErrUnauthorized) {
		status = http.StatusForbidden
		code = "UNAUTHORIZED"
	} else if errors.Is(err, domain.ErrWorkerBusy) {
		status = http.StatusConflict
		code = "WORKER_BUSY"
	} else if errors.Is(err, domain.ErrAgentNotReady) {
		status = http.StatusConflict
		code = "AGENT_NOT_READY"
	} else if errors.Is(err, domain.ErrSessionGenerationConflict) {
		status = http.StatusConflict
		code = "SESSION_GENERATION_CONFLICT"
	} else if errors.Is(err, domain.ErrIdempotencyConflict) {
		status = http.StatusConflict
		code = "IDEMPOTENCY_CONFLICT"
	} else if errors.Is(err, domain.ErrInvalidTransition) || errors.Is(err, domain.ErrInvalidState) || errors.Is(err, domain.ErrTerminalState) {
		status = http.StatusBadRequest
		code = "INVALID_STATE_TRANSITION"
	} else if errors.Is(err, domain.ErrInvalidAddress) {
		status = http.StatusBadRequest
		code = "INVALID_ADDRESS"
	} else if errors.Is(err, domain.ErrInvalidManifest) {
		status = http.StatusBadRequest
		code = "INVALID_MANIFEST"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"code":  code,
		"error": err.Error(),
	})
}

// Handlers

func (s *Server) handleRegisterAgent(w http.ResponseWriter, r *http.Request) {
	var a domain.Agent
	if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
		s.writeError(w, domain.ErrInvalidInput("invalid json request body"))
		return
	}
	if err := s.svc.RegisterAgent(r.Context(), &a); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"agent": a})
}

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.svc.ListAgents(r.Context())
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"agents": agents})
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, domain.ErrInvalidInput("agent id required"))
		return
	}
	agent, err := s.svc.GetAgent(r.Context(), id)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"agent": agent})
}

func (s *Server) handleAttachAgent(w http.ResponseWriter, r *http.Request) {
	var req service.AttachAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, domain.ErrInvalidInput("invalid json request body"))
		return
	}
	resp, err := s.svc.AttachAgent(r.Context(), req)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleBootstrapAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, domain.ErrInvalidInput("agent id required"))
		return
	}
	resp, err := s.svc.BootstrapAgent(r.Context(), id)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, domain.ErrInvalidInput("agent id required"))
		return
	}
	resp, err := s.svc.GetSession(r.Context(), id)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, resp)
}

type readySessionReq struct {
	Generation int64 `json:"generation"`
}

func (s *Server) handleReadySession(w http.ResponseWriter, r *http.Request) {
	agentID := r.PathValue("agent_id")
	if agentID == "" {
		s.writeError(w, domain.ErrInvalidInput("agent_id required"))
		return
	}
	var req readySessionReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, domain.ErrInvalidInput("invalid json request body"))
		return
	}
	if req.Generation <= 0 {
		s.writeError(w, domain.ErrInvalidInput("generation must be > 0"))
		return
	}
	session, err := s.svc.ReadySession(r.Context(), agentID, req.Generation)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"session": session})
}

func (s *Server) handleSubmitTask(w http.ResponseWriter, r *http.Request) {
	var req service.SubmitTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, domain.ErrInvalidInput("invalid json request body"))
		return
	}
	resp, err := s.svc.SubmitTask(r.Context(), req)
	if err != nil {
		s.writeError(w, err)
		return
	}
	status := http.StatusCreated
	if resp.IsDuplicate {
		status = http.StatusOK
	}
	s.writeJSON(w, status, resp)
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent")
	if strings.TrimSpace(agentID) == "" {
		s.writeError(w, domain.ErrInvalidInput("query parameter 'agent' is required"))
		return
	}
	status := r.URL.Query().Get("status")
	tasks, err := s.svc.ListTasks(r.Context(), agentID, status)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	callerAgent := r.URL.Query().Get("agent")
	if strings.TrimSpace(callerAgent) == "" {
		s.writeError(w, domain.ErrInvalidInput("query parameter 'agent' is required"))
		return
	}

	task, err := s.svc.GetTask(r.Context(), id, callerAgent)
	if err != nil {
		s.writeError(w, err)
		return
	}

	msgs, err := s.svc.GetTaskMessages(r.Context(), id, callerAgent)
	if err != nil {
		s.writeError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"task":     task,
		"messages": msgs,
	})
}

type agentActorReq struct {
	Agent string `json:"agent"`
}

func (s *Server) handleAckTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req agentActorReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, domain.ErrInvalidInput("invalid json request body"))
		return
	}
	task, err := s.svc.AckTask(r.Context(), id, req.Agent)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

type statusReq struct {
	Agent   string `json:"agent"`
	Message string `json:"message"`
}

func (s *Server) handleUpdateTaskStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req statusReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, domain.ErrInvalidInput("invalid json request body"))
		return
	}
	if err := s.svc.UpdateTaskStatus(r.Context(), id, req.Agent, req.Message); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type sendReq struct {
	From    string `json:"from"`
	Content string `json:"content"`
}

func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req sendReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, domain.ErrInvalidInput("invalid json request body"))
		return
	}
	if err := s.svc.SendMessage(r.Context(), id, req.From, req.Content); err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

type completeReq struct {
	Agent  string `json:"agent"`
	Result string `json:"result"`
}

func (s *Server) handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req completeReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, domain.ErrInvalidInput("invalid json request body"))
		return
	}
	task, err := s.svc.CompleteTask(r.Context(), id, req.Agent, req.Result)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

type failReq struct {
	Agent string `json:"agent"`
	Error string `json:"error"`
}

func (s *Server) handleFailTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req failReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, domain.ErrInvalidInput("invalid json request body"))
		return
	}
	task, err := s.svc.FailTask(r.Context(), id, req.Agent, req.Error)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req agentActorReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, domain.ErrInvalidInput("invalid json request body"))
		return
	}
	task, err := s.svc.CancelTask(r.Context(), id, req.Agent)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"task": task})
}

func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	callerAgent := r.URL.Query().Get("agent")
	if strings.TrimSpace(callerAgent) == "" {
		s.writeError(w, domain.ErrInvalidInput("query parameter 'agent' is required"))
		return
	}

	afterStr := r.URL.Query().Get("after")
	timeoutStr := r.URL.Query().Get("timeout")

	var afterSeq int64
	if afterStr != "" {
		val, err := strconv.ParseInt(afterStr, 10, 64)
		if err != nil {
			s.writeError(w, domain.ErrInvalidInput("invalid 'after' sequence parameter: must be an integer"))
			return
		}
		afterSeq = val
	}

	var timeout time.Duration
	if timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr); err == nil {
			timeout = d
		} else if sec, err := strconv.Atoi(timeoutStr); err == nil {
			timeout = time.Duration(sec) * time.Second
		} else {
			s.writeError(w, domain.ErrInvalidInput("invalid 'timeout' parameter: must be a duration (e.g. 30s) or seconds"))
			return
		}
	}

	events, err := s.svc.GetEvents(r.Context(), id, callerAgent, afterSeq, timeout)
	if err != nil {
		s.writeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (s *Server) handleRecordRuntimeEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if strings.TrimSpace(id) == "" {
		s.writeError(w, domain.ErrInvalidInput("task ID is required in URL path"))
		return
	}

	var req service.RecordRuntimeEventRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&req); err != nil {
		s.writeError(w, domain.ErrInvalidInput(fmt.Sprintf("invalid json body: %v", err)))
		return
	}
	if dec.More() {
		s.writeError(w, domain.ErrInvalidInput("request body contains multiple JSON values"))
		return
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		s.writeError(w, domain.ErrInvalidInput("request body contains trailing characters"))
		return
	}

	resp, err := s.svc.RecordRuntimeEvent(r.Context(), id, req)
	if err != nil {
		s.writeError(w, err)
		return
	}

	s.writeJSON(w, http.StatusOK, resp)
}
