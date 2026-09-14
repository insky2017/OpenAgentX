package console

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
)

type Client struct {
	httpClient     *http.Client
	baseURL        string
	cookie         *http.Cookie
	csrf           string
	reconnectDelay time.Duration
}

type APIError struct {
	StatusCode int
	Message    string
}

type terminalFollowError struct{ err error }

func (e *terminalFollowError) Error() string { return e.err.Error() }
func (e *terminalFollowError) Unwrap() error { return e.err }

func (e *APIError) Error() string {
	return fmt.Sprintf("Console API status %d: %s", e.StatusCode, e.Message)
}

func NewUnixClient(socketPath string) (*Client, error) {
	if strings.TrimSpace(socketPath) == "" {
		return nil, fmt.Errorf("Console socket path is required")
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	return &Client{httpClient: &http.Client{Transport: transport}, baseURL: "http://unix", reconnectDelay: time.Second}, nil
}

func (c *Client) Login(ctx context.Context, username, password string) error {
	var session openapi.WebSessionResponse
	response, err := c.do(ctx, http.MethodPost, openapi.AuthLoginPath, nil, openapi.LoginRequest{Username: username, Password: password}, &session, false, "", false)
	if err != nil {
		return err
	}
	for _, cookie := range response.Cookies() {
		if cookie.Name == "openagentx_session" {
			c.cookie = cookie
			break
		}
	}
	if c.cookie == nil || session.CSRFToken == "" {
		return fmt.Errorf("Console login did not return a complete session")
	}
	c.csrf = session.CSRFToken
	return nil
}

func (c *Client) Attach(ctx context.Context, agentID, mode string) (consoleapi.AttachResponse, error) {
	query := url.Values{"agent_id": []string{agentID}}
	if mode != "" {
		query.Set("mode", mode)
	}
	var result consoleapi.AttachResponse
	_, err := c.do(ctx, http.MethodGet, consoleapi.AttachPath, query, nil, &result, true, "", false)
	return result, err
}

func (c *Client) Dispatch(ctx context.Context, request openapi.CreateTaskRequest) (openapi.CreateTaskResponse, error) {
	var result openapi.CreateTaskResponse
	_, err := c.do(ctx, http.MethodPost, openapi.ControlCreateTaskPath, nil, request, &result, true, request.Meta.IdempotencyKey, false)
	return result, err
}

func (c *Client) Steer(ctx context.Context, taskID string, request openapi.CreateMessageRequest) (openapi.CreateMessageResponse, error) {
	path := strings.Replace(openapi.ControlCreateMessagePath, "{task-id}", url.PathEscape(taskID), 1)
	var result openapi.CreateMessageResponse
	_, err := c.do(ctx, http.MethodPost, path, nil, request, &result, true, request.Meta.IdempotencyKey, false)
	return result, err
}

func (c *Client) Cancel(ctx context.Context, taskID string, request openapi.CancelTaskRequest) (openapi.CancelTaskResponse, error) {
	path := strings.Replace(openapi.ControlCancelTaskPath, "{task-id}", url.PathEscape(taskID), 1)
	var result openapi.CancelTaskResponse
	_, err := c.do(ctx, http.MethodPost, path, nil, request, &result, true, request.Meta.IdempotencyKey, false)
	return result, err
}

func (c *Client) DecideApproval(ctx context.Context, approvalID string, request openapi.DecideApprovalRequest) (openapi.DecideApprovalResponse, error) {
	path := strings.Replace(openapi.ControlApprovalDecisionPath, "{approval-request-id}", url.PathEscape(approvalID), 1)
	var result openapi.DecideApprovalResponse
	_, err := c.do(ctx, http.MethodPost, path, nil, request, &result, true, request.Meta.IdempotencyKey, false)
	return result, err
}

func (c *Client) WorkerCommand(ctx context.Context, workerID string, generation int64, kind domain.WorkerCommandKind, idempotencyKey string, confirmForce bool) (openapi.WorkerCommandResponse, error) {
	path := strings.Replace(openapi.AdminWorkerStopPath, "{worker-id}", url.PathEscape(workerID), 1)
	if kind == domain.WorkerCommandDrain {
		path = strings.Replace(openapi.AdminWorkerDrainPath, "{worker-id}", url.PathEscape(workerID), 1)
	} else if kind == domain.WorkerCommandForceStop {
		path = strings.Replace(openapi.AdminWorkerForceStopPath, "{worker-id}", url.PathEscape(workerID), 1)
	}
	meta := openapi.CommandMeta{IdempotencyKey: idempotencyKey, ExpectedVersion: generation}
	body := any(openapi.WorkerAdminRequest{Meta: meta, ExpectedGeneration: generation})
	if kind == domain.WorkerCommandForceStop {
		body = openapi.WorkerForceStopRequest{Meta: meta, ExpectedGeneration: generation, Confirm: confirmForce}
	}
	var result openapi.WorkerCommandResponse
	_, err := c.do(ctx, http.MethodPost, path, nil, body, &result, true, idempotencyKey, confirmForce)
	return result, err
}

func (c *Client) Events(ctx context.Context, agentID string, afterSequence int64) (io.ReadCloser, error) {
	query := url.Values{"after_sequence": []string{strconv.FormatInt(afterSequence, 10)}}
	query.Set("agent_id", agentID)
	response, err := c.do(ctx, http.MethodGet, openapi.ObserveEventsStreamPath, query, nil, nil, true, "", false)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

// Follow reconnects the authenticated Agent stream from the last delivered
// sequence. Attach is resolved again on every connection so a restarted Worker
// is observed at its current generation rather than a stale instance ID.
func (c *Client) Follow(
	ctx context.Context,
	agentID string,
	mode string,
	afterSequence int64,
	onAttach func(consoleapi.AttachResponse) error,
	onEvent func(openapi.JournalEventReadModel) error,
) error {
	if onAttach == nil || onEvent == nil {
		return fmt.Errorf("Console follow callbacks are required")
	}
	if afterSequence < 0 {
		return fmt.Errorf("after sequence cannot be negative")
	}
	delay := c.reconnectDelay
	if delay <= 0 {
		delay = time.Second
	}
	for {
		attached, err := c.Attach(ctx, agentID, mode)
		if err == nil {
			if err = onAttach(attached); err != nil {
				return err
			}
		}
		if err == nil {
			var stream io.ReadCloser
			stream, err = c.Events(ctx, agentID, afterSequence)
			if err == nil {
				afterSequence, err = readEventStream(stream, afterSequence, onEvent)
				_ = stream.Close()
			}
		}
		if ctx.Err() != nil {
			return nil
		}
		var terminalErr *terminalFollowError
		if errors.As(err, &terminalErr) {
			return terminalErr.err
		}
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func readEventStream(reader io.Reader, afterSequence int64, onEvent func(openapi.JournalEventReadModel) error) (int64, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	var eventID int64
	var data strings.Builder
	deliver := func() error {
		defer func() {
			eventID = 0
			data.Reset()
		}()
		if data.Len() == 0 {
			return nil
		}
		if eventID <= 0 {
			return &terminalFollowError{err: fmt.Errorf("Console SSE event is missing a positive id")}
		}
		if eventID <= afterSequence {
			return nil
		}
		var event openapi.JournalEventReadModel
		if err := json.Unmarshal([]byte(data.String()), &event); err != nil {
			return &terminalFollowError{err: fmt.Errorf("decode Console event: %w", err)}
		}
		if event.Sequence != eventID {
			return &terminalFollowError{err: fmt.Errorf("Console event sequence %d does not match SSE id %d", event.Sequence, eventID)}
		}
		if err := onEvent(event); err != nil {
			return &terminalFollowError{err: err}
		}
		afterSequence = eventID
		return nil
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := deliver(); err != nil {
				return afterSequence, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			parsed, err := strconv.ParseInt(value, 10, 64)
			if err != nil || parsed <= 0 {
				return afterSequence, &terminalFollowError{err: fmt.Errorf("invalid Console SSE event id")}
			}
			eventID = parsed
		case "data":
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return afterSequence, err
	}
	if err := deliver(); err != nil {
		return afterSequence, err
	}
	return afterSequence, io.EOF
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, destination any, authenticated bool, idempotencyKey string, dangerous bool) (*http.Response, error) {
	requestURL := c.baseURL + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, requestURL, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		if c.cookie == nil {
			return nil, fmt.Errorf("Console is not authenticated")
		}
		request.AddCookie(c.cookie)
		if method != http.MethodGet {
			request.Header.Set("X-CSRF-Token", c.csrf)
		}
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	if dangerous {
		request.Header.Set("X-Confirm-Dangerous", "force-stop")
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		message, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
		return nil, &APIError{StatusCode: response.StatusCode, Message: strings.TrimSpace(string(message))}
	}
	if destination != nil {
		defer response.Body.Close()
		if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
			return nil, err
		}
	}
	return response, nil
}

func IdempotencyKey(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UTC().UnixNano())
}
