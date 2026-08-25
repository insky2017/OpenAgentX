package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agentbus/internal/domain"
	"agentbus/internal/service"
)

type Client struct {
	socketPath string
	httpClient *http.Client
}

type TaskDetail struct {
	Task     *domain.Task      `json:"task"`
	Messages []*domain.Message `json:"messages"`
}

type APIError struct {
	StatusCode int    `json:"status_code"`
	Code       string `json:"code"`
	ErrMessage string `json:"error"`
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("API error (%d %s): %s", e.StatusCode, e.Code, e.ErrMessage)
	}
	return fmt.Sprintf("API error (%d): %s", e.StatusCode, e.ErrMessage)
}

func DefaultAgentBusDir() string {
	execPath, err := os.Executable()
	if err == nil {
		if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
			execPath = resolved
		}
		dir := filepath.Dir(execPath)
		if filepath.Base(dir) == "bin" {
			return filepath.Dir(dir)
		}
		// If running from cmd/agentbus or similar
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}

	// Fallback to checking cwd and parents
	if cwd, err := os.Getwd(); err == nil {
		curr := cwd
		for i := 0; i < 5; i++ {
			if _, err := os.Stat(filepath.Join(curr, "go.mod")); err == nil {
				return curr
			}
			if _, err := os.Stat(filepath.Join(curr, "OpenAgentX", "go.mod")); err == nil {
				return filepath.Join(curr, "OpenAgentX")
			}
			if _, err := os.Stat(filepath.Join(curr, "AgentBus", "go.mod")); err == nil {
				return filepath.Join(curr, "AgentBus")
			}
			parent := filepath.Dir(curr)
			if parent == curr {
				break
			}
			curr = parent
		}
		return cwd
	}

	return "."
}

func DefaultSocketPath() string {
	return filepath.Join(DefaultAgentBusDir(), "run", "agentbus.sock")
}

func DefaultDBPath() string {
	return filepath.Join(DefaultAgentBusDir(), "data", "agentbus.db")
}

func ResolveSocketPath(socketPath string) string {
	if strings.TrimSpace(socketPath) != "" {
		abs, err := filepath.Abs(socketPath)
		if err == nil {
			return abs
		}
		return socketPath
	}
	if env := strings.TrimSpace(os.Getenv("AGENTBUS_SOCKET")); env != "" {
		abs, err := filepath.Abs(env)
		if err == nil {
			return abs
		}
		return env
	}
	return DefaultSocketPath()
}

func NewClient(socketPath string) *Client {
	resolved := ResolveSocketPath(socketPath)
	return &Client{
		socketPath: resolved,
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", resolved)
				},
			},
			Timeout: 60 * time.Second,
		},
	}
}

func (c *Client) do(ctx context.Context, method string, path string, query url.Values, reqBody any, respBody any) error {
	reqURL := "http://unix" + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if reqBody != nil {
		buf, err := json.Marshal(reqBody)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to agentbus daemon at '%s': %w", c.socketPath, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiErr struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(bodyBytes, &apiErr); err == nil && apiErr.Error != "" {
			return &APIError{
				StatusCode: resp.StatusCode,
				Code:       apiErr.Code,
				ErrMessage: apiErr.Error,
			}
		}
		return &APIError{
			StatusCode: resp.StatusCode,
			ErrMessage: string(bodyBytes),
		}
	}

	if respBody != nil && len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, respBody); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w (body: %s)", err, string(bodyBytes))
		}
	}

	return nil
}

func (c *Client) RegisterAgent(ctx context.Context, agent *domain.Agent) (*domain.Agent, error) {
	var resp struct {
		Agent *domain.Agent `json:"agent"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/agents", nil, agent, &resp); err != nil {
		return nil, err
	}
	return resp.Agent, nil
}

func (c *Client) AttachAgent(ctx context.Context, req service.AttachAgentRequest) (*service.AttachAgentResponse, error) {
	var resp service.AttachAgentResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/agents/attach", nil, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) BootstrapAgent(ctx context.Context, id string) (*service.BootstrapAgentResponse, error) {
	var resp service.BootstrapAgentResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/agents/"+url.PathEscape(id)+"/bootstrap", nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetSession(ctx context.Context, id string) (*service.GetSessionResponse, error) {
	var resp service.GetSessionResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/agents/"+url.PathEscape(id)+"/session", nil, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ReadySession(ctx context.Context, agentID string, generation int64) (*domain.AgentSession, error) {
	reqBody := map[string]int64{"generation": generation}
	var resp struct {
		Session *domain.AgentSession `json:"session"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/sessions/"+url.PathEscape(agentID)+"/ready", nil, reqBody, &resp); err != nil {
		return nil, err
	}
	return resp.Session, nil
}

func (c *Client) ListAgents(ctx context.Context) ([]*domain.Agent, error) {
	var resp struct {
		Agents []*domain.Agent `json:"agents"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/agents", nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Agents, nil
}

func (c *Client) GetAgent(ctx context.Context, id string) (*domain.Agent, error) {
	var resp struct {
		Agent *domain.Agent `json:"agent"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/agents/"+url.PathEscape(id), nil, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Agent, nil
}

func (c *Client) SubmitTask(ctx context.Context, req service.SubmitTaskRequest) (*service.SubmitTaskResponse, error) {
	var resp service.SubmitTaskResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/tasks", nil, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListTasks(ctx context.Context, agentID string, status string) ([]*domain.Task, error) {
	q := url.Values{}
	if agentID != "" {
		q.Set("agent", agentID)
	}
	if status != "" {
		q.Set("status", status)
	}
	var resp struct {
		Tasks []*domain.Task `json:"tasks"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/tasks", q, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Tasks, nil
}

func (c *Client) GetTask(ctx context.Context, id string, agentID string) (*TaskDetail, error) {
	q := url.Values{}
	if agentID != "" {
		q.Set("agent", agentID)
	}
	var resp TaskDetail
	if err := c.do(ctx, http.MethodGet, "/api/v1/tasks/"+url.PathEscape(id), q, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) AckTask(ctx context.Context, id string, agentID string) (*domain.Task, error) {
	reqBody := map[string]string{"agent": agentID}
	var resp struct {
		Task *domain.Task `json:"task"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(id)+"/ack", nil, reqBody, &resp); err != nil {
		return nil, err
	}
	return resp.Task, nil
}

func (c *Client) UpdateTaskStatus(ctx context.Context, id string, agentID string, message string) error {
	reqBody := map[string]string{
		"agent":   agentID,
		"message": message,
	}
	return c.do(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(id)+"/status", nil, reqBody, nil)
}

func (c *Client) SendMessage(ctx context.Context, id string, fromAgentID string, content string) error {
	reqBody := map[string]string{
		"from":    fromAgentID,
		"content": content,
	}
	return c.do(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(id)+"/send", nil, reqBody, nil)
}

func (c *Client) CompleteTask(ctx context.Context, id string, agentID string, result string) (*domain.Task, error) {
	reqBody := map[string]string{
		"agent":  agentID,
		"result": result,
	}
	var resp struct {
		Task *domain.Task `json:"task"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(id)+"/complete", nil, reqBody, &resp); err != nil {
		return nil, err
	}
	return resp.Task, nil
}

func (c *Client) FailTask(ctx context.Context, id string, agentID string, errStr string) (*domain.Task, error) {
	reqBody := map[string]string{
		"agent": agentID,
		"error": errStr,
	}
	var resp struct {
		Task *domain.Task `json:"task"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(id)+"/fail", nil, reqBody, &resp); err != nil {
		return nil, err
	}
	return resp.Task, nil
}

func (c *Client) CancelTask(ctx context.Context, id string, agentID string) (*domain.Task, error) {
	reqBody := map[string]string{"agent": agentID}
	var resp struct {
		Task *domain.Task `json:"task"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(id)+"/cancel", nil, reqBody, &resp); err != nil {
		return nil, err
	}
	return resp.Task, nil
}

func (c *Client) GetEvents(ctx context.Context, id string, callerAgentID string, afterSeq int64, timeout time.Duration) ([]*domain.Event, error) {
	q := url.Values{}
	if callerAgentID != "" {
		q.Set("agent", callerAgentID)
	}
	if afterSeq >= 0 {
		q.Set("after", strconv.FormatInt(afterSeq, 10))
	}
	if timeout > 0 {
		q.Set("timeout", timeout.String())
	}
	var resp struct {
		Events []*domain.Event `json:"events"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v1/tasks/"+url.PathEscape(id)+"/events", q, nil, &resp); err != nil {
		return nil, err
	}
	return resp.Events, nil
}

func (c *Client) RecordRuntimeEvent(ctx context.Context, id string, req service.RecordRuntimeEventRequest) (*service.RecordRuntimeEventResponse, error) {
	var resp service.RecordRuntimeEventResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/tasks/"+url.PathEscape(id)+"/runtime-events", nil, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
