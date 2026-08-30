package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	openapi "openagentx/internal/api"
	"openagentx/internal/domain"
)

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Worker API error (%d %s): %s", e.StatusCode, e.Code, e.Message)
}

type UnixHTTPWorkerClient struct {
	httpClient *http.Client
	transport  *http.Transport
	baseURL    string

	mu           sync.RWMutex
	sessionToken string
}

func NewUnixHTTPWorkerClient(socketPath string) (*UnixHTTPWorkerClient, error) {
	if strings.TrimSpace(socketPath) == "" {
		return nil, fmt.Errorf("Unix socket path is required")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	return &UnixHTTPWorkerClient{
		httpClient: &http.Client{Transport: transport, Timeout: 40 * time.Second}, transport: transport, baseURL: "http://unix",
	}, nil
}

// NewHTTPWorkerClient reuses the Worker API contract over HTTPS (typically
// configured with mTLS by the remotehttps transport package).
func NewHTTPWorkerClient(baseURL string, httpClient *http.Client) (*UnixHTTPWorkerClient, error) {
	if strings.TrimSpace(baseURL) == "" || httpClient == nil {
		return nil, fmt.Errorf("Worker base URL and HTTP client are required")
	}
	return &UnixHTTPWorkerClient{httpClient: httpClient, baseURL: strings.TrimRight(baseURL, "/")}, nil
}

func (c *UnixHTTPWorkerClient) Close() error {
	if c.transport != nil {
		c.transport.CloseIdleConnections()
	}
	return nil
}

func (c *UnixHTTPWorkerClient) RegisterWorker(ctx context.Context, request openapi.RegisterRequest) (*openapi.WorkerSession, error) {
	var session openapi.WorkerSession
	if err := c.do(ctx, http.MethodPost, openapi.WorkerRegisterPath, nil, request, &session, false); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.sessionToken = session.SessionToken
	c.mu.Unlock()
	return &session, nil
}

func (c *UnixHTTPWorkerClient) Heartbeat(ctx context.Context, request openapi.HeartbeatRequest) error {
	path := replacePath(openapi.WorkerHeartbeatPath, "{worker-id}", request.WorkerInstanceID)
	return c.do(ctx, http.MethodPost, path, nil, request, nil, true)
}

func (c *UnixHTTPWorkerClient) ClaimMailbox(ctx context.Context, request openapi.ClaimRequest) (*domain.MailboxItem, error) {
	path := replacePath(openapi.WorkerMailboxClaimPath, "{worker-id}", request.WorkerInstanceID)
	query := url.Values{"wait": []string{strconv.Itoa(request.WaitSeconds) + "s"}}
	var response openapi.ClaimResponse
	if err := c.do(ctx, http.MethodPost, path, query, request, &response, true); err != nil {
		return nil, err
	}
	return response.Item, nil
}

func (c *UnixHTTPWorkerClient) BeginAttempt(ctx context.Context, itemID string, request openapi.BeginAttemptRequest) (*openapi.BeginAttemptResponse, error) {
	path := replacePath(openapi.MailboxBeginAttemptPath, "{item-id}", itemID)
	var response openapi.BeginAttemptResponse
	if err := c.do(ctx, http.MethodPost, path, nil, request, &response, true); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *UnixHTTPWorkerClient) AcceptMailboxItem(ctx context.Context, itemID string, request openapi.AcceptRequest) error {
	path := replacePath(openapi.MailboxAcceptPath, "{item-id}", itemID)
	return c.do(ctx, http.MethodPost, path, nil, request, nil, true)
}

func (c *UnixHTTPWorkerClient) ClaimWorkerCommand(ctx context.Context, request openapi.ControlClaimRequest) (*domain.WorkerCommand, error) {
	path := replacePath(openapi.WorkerControlClaimPath, "{worker-id}", request.WorkerInstanceID)
	query := url.Values{"wait": []string{strconv.Itoa(request.WaitSeconds) + "s"}}
	var response openapi.ControlClaimResponse
	if err := c.do(ctx, http.MethodPost, path, query, request, &response, true); err != nil {
		return nil, err
	}
	return response.Command, nil
}

func (c *UnixHTTPWorkerClient) AcknowledgeWorkerCommand(ctx context.Context, commandID string, request openapi.ControlAckRequest) error {
	path := replacePath(openapi.WorkerCommandAckPath, "{command-id}", commandID)
	return c.do(ctx, http.MethodPost, path, nil, request, nil, true)
}

func (c *UnixHTTPWorkerClient) AppendRunEvents(ctx context.Context, runID string, batch openapi.EventBatch) error {
	path := replacePath(openapi.RunEventsPath, "{run-id}", runID)
	return c.do(ctx, http.MethodPost, path, nil, batch, nil, true)
}

func (c *UnixHTTPWorkerClient) FinishRun(ctx context.Context, runID string, request openapi.FinishRunRequest) error {
	path := replacePath(openapi.RunFinishPath, "{run-id}", runID)
	return c.do(ctx, http.MethodPost, path, nil, request, nil, true)
}

func (c *UnixHTTPWorkerClient) do(ctx context.Context, method string, path string, query url.Values, requestBody any, responseBody any, authenticated bool) error {
	requestURL := c.baseURL + path
	if len(query) != 0 {
		requestURL += "?" + query.Encode()
	}
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("encode Worker API request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return fmt.Errorf("create Worker API request: %w", err)
	}
	if requestBody != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		c.mu.RLock()
		token := c.sessionToken
		c.mu.RUnlock()
		if token == "" {
			return fmt.Errorf("Worker is not registered")
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("call Worker API: %w", err)
	}
	defer response.Body.Close()
	responseBytes, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return fmt.Errorf("read Worker API response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiError openapi.ErrorResponse
		if err := json.Unmarshal(responseBytes, &apiError); err != nil {
			return &APIError{StatusCode: response.StatusCode, Code: openapi.ErrorInternal, Message: string(responseBytes)}
		}
		return &APIError{StatusCode: response.StatusCode, Code: apiError.Code, Message: apiError.Message}
	}
	if responseBody != nil && len(responseBytes) != 0 {
		if err := json.Unmarshal(responseBytes, responseBody); err != nil {
			return fmt.Errorf("decode Worker API response: %w", err)
		}
	}
	return nil
}

func replacePath(path string, placeholder string, value string) string {
	return strings.Replace(path, placeholder, url.PathEscape(value), 1)
}

var _ openapi.WorkerControlClient = (*UnixHTTPWorkerClient)(nil)
