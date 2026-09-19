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
	"os"
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
	bearerToken    string
	installationID string
	reconnectDelay time.Duration
}

type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

type terminalFollowError struct{ err error }

type ConnectionState string

const (
	ConnectionConnecting        ConnectionState = "connecting"
	ConnectionConnected         ConnectionState = "connected"
	ConnectionDisconnected      ConnectionState = "disconnected"
	ConnectionReconnecting      ConnectionState = "reconnecting"
	ConnectionRetentionReattach ConnectionState = "retention-reattach"
)

type FollowState struct {
	State  ConnectionState
	Cursor int64
}

func (e *terminalFollowError) Error() string { return e.err.Error() }
func (e *terminalFollowError) Unwrap() error { return e.err }

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("Console API status %d (%s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("Console API status %d: %s", e.StatusCode, e.Message)
}

func NewUnixClient(socketPath string) (*Client, error) {
	if strings.TrimSpace(socketPath) == "" {
		return nil, fmt.Errorf("Console socket path is required")
	}
	info, err := os.Stat(socketPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("Console socket does not exist: %s", socketPath)
	}
	if err != nil {
		return nil, fmt.Errorf("inspect Console socket: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return nil, fmt.Errorf("Console socket path is not a Unix socket: %s", socketPath)
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	return &Client{httpClient: &http.Client{Transport: transport}, baseURL: "http://unix", reconnectDelay: time.Second}, nil
}

func (c *Client) Login(context.Context, string, string) error {
	return fmt.Errorf("direct password login is disabled; run openagentx console login")
}

func (c *Client) ProbeInstallation(ctx context.Context) (openapi.CLIInstallationResponse, error) {
	var result openapi.CLIInstallationResponse
	_, err := c.do(ctx, http.MethodGet, openapi.CLIInstallationProbePath, nil, nil, &result, false, "", false)
	if err == nil && strings.TrimSpace(result.InstallationID) == "" {
		return openapi.CLIInstallationResponse{}, fmt.Errorf("Console installation probe returned an empty identity")
	}
	return result, err
}

func (c *Client) LoginCredential(ctx context.Context, username, password string) (openapi.CLILoginResponse, error) {
	probe, err := c.ProbeInstallation(ctx)
	if err != nil {
		return openapi.CLILoginResponse{}, err
	}
	var result openapi.CLILoginResponse
	_, err = c.do(ctx, http.MethodPost, openapi.CLIAuthLoginPath, nil, openapi.LoginRequest{Username: username, Password: password}, &result, false, "", false)
	if err != nil {
		return openapi.CLILoginResponse{}, err
	}
	if result.Token == "" || result.InstallationID != probe.InstallationID {
		return openapi.CLILoginResponse{}, fmt.Errorf("Console login returned an invalid installation-bound credential")
	}
	c.bearerToken = result.Token
	c.installationID = result.InstallationID
	return result, nil
}

func (c *Client) UseCredential(ctx context.Context, installationID, token string) error {
	probe, err := c.ProbeInstallation(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(installationID) == "" || probe.InstallationID != installationID {
		return fmt.Errorf("Console installation identity does not match the stored credential")
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("Console credential token is empty")
	}
	c.installationID = installationID
	c.bearerToken = token
	return nil
}

func (c *Client) Session(ctx context.Context) (openapi.CLISessionResponse, error) {
	var result openapi.CLISessionResponse
	_, err := c.do(ctx, http.MethodGet, openapi.CLIAuthSessionPath, nil, nil, &result, true, "", false)
	return result, err
}

func (c *Client) Logout(ctx context.Context) error {
	response, err := c.do(ctx, http.MethodPost, openapi.CLIAuthLogoutPath, nil, nil, nil, true, "", false)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err == nil {
		c.bearerToken = ""
	}
	return err
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

func (c *Client) ListAgentOptions(ctx context.Context) ([]domain.ConsoleAgentOption, error) {
	const maxAgentOptions = 10000
	result := make([]domain.ConsoleAgentOption, 0)
	cursor := ""
	for {
		query := url.Values{}
		if cursor != "" {
			query.Set("after_agent_id", cursor)
		}
		var page consoleapi.AgentOptionsPage
		if _, err := c.do(ctx, http.MethodGet, consoleapi.AgentsPath, query, nil, &page, true, "", false); err != nil {
			return nil, err
		}
		for _, option := range page.Agents {
			if err := option.Validate(); err != nil || (cursor != "" && option.AgentID <= cursor) {
				return nil, fmt.Errorf("Console Agent list returned an invalid projection")
			}
			cursor = option.AgentID
			result = append(result, option)
			if len(result) > maxAgentOptions {
				return nil, fmt.Errorf("Console Agent list exceeds the supported safe bound")
			}
		}
		if !page.HasMore {
			if page.NextCursor != "" {
				return nil, fmt.Errorf("Console Agent list returned an unexpected cursor")
			}
			return result, nil
		}
		if len(page.Agents) == 0 || page.NextCursor == "" || page.NextCursor != cursor {
			return nil, fmt.Errorf("Console Agent list pagination did not advance")
		}
	}
}

func (c *Client) ListTaskOptions(ctx context.Context, agentID string) ([]openapi.ConsoleTaskOption, error) {
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		return nil, err
	}
	const maxTaskOptions = 10000
	path := strings.Replace(consoleapi.AgentTasksPath, "{agentID}", url.PathEscape(agentID), 1)
	result := make([]openapi.ConsoleTaskOption, 0)
	cursor := ""
	var previous *openapi.ConsoleTaskOption
	for {
		query := url.Values{"limit": []string{"100"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		var page openapi.ConsoleTaskPage
		if _, err := c.do(ctx, http.MethodGet, path, query, nil, &page, true, "", false); err != nil {
			return nil, err
		}
		for index := range page.Tasks {
			option := page.Tasks[index]
			if err := validateTaskOption(option); err != nil || previous != nil && !taskOptionBefore(option, *previous) {
				return nil, fmt.Errorf("Console Task list returned an invalid projection")
			}
			copy := option
			previous = &copy
			result = append(result, option)
			if len(result) > maxTaskOptions {
				return nil, fmt.Errorf("Console Task list exceeds the supported safe bound")
			}
		}
		if !page.HasMore {
			if page.NextCursor != "" {
				return nil, fmt.Errorf("Console Task list returned an unexpected cursor")
			}
			return result, nil
		}
		if len(page.Tasks) == 0 || page.NextCursor == "" || page.NextCursor == cursor {
			return nil, fmt.Errorf("Console Task list pagination did not advance")
		}
		cursor = page.NextCursor
	}
}

func (c *Client) TaskSnapshot(ctx context.Context, agentID, taskID string) (openapi.ConsoleTaskSnapshot, error) {
	if err := domain.ValidateIdentifier("agent_id", agentID); err != nil {
		return openapi.ConsoleTaskSnapshot{}, err
	}
	if err := domain.ValidateOpaqueID("task_id", taskID); err != nil {
		return openapi.ConsoleTaskSnapshot{}, err
	}
	path := strings.Replace(consoleapi.AgentTaskPath, "{agentID}", url.PathEscape(agentID), 1)
	path = strings.Replace(path, "{taskID}", url.PathEscape(taskID), 1)
	var result openapi.ConsoleTaskSnapshot
	_, err := c.do(ctx, http.MethodGet, path, nil, nil, &result, true, "", false)
	if err != nil {
		return openapi.ConsoleTaskSnapshot{}, err
	}
	if result.Task.TaskID != taskID || result.Task.AgentID != agentID || result.SnapshotSequence < 0 {
		return openapi.ConsoleTaskSnapshot{}, fmt.Errorf("Console Task snapshot returned an invalid identity or cursor")
	}
	return result, nil
}

func validateTaskOption(option openapi.ConsoleTaskOption) error {
	if err := domain.ValidateOpaqueID("task_id", option.TaskID); err != nil {
		return err
	}
	if option.Version <= 0 || !option.Status.Valid() || strings.TrimSpace(option.Summary) == "" {
		return fmt.Errorf("invalid Task option status, version, or summary")
	}
	_, err := time.Parse(time.RFC3339Nano, option.UpdatedAt)
	return err
}

func taskOptionBefore(current, previous openapi.ConsoleTaskOption) bool {
	currentTime, currentErr := time.Parse(time.RFC3339Nano, current.UpdatedAt)
	previousTime, previousErr := time.Parse(time.RFC3339Nano, previous.UpdatedAt)
	if currentErr != nil || previousErr != nil {
		return false
	}
	return currentTime.Before(previousTime) || currentTime.Equal(previousTime) && current.TaskID < previous.TaskID
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

func (c *Client) Events(ctx context.Context, agentID, mode string, afterSequence int64) (io.ReadCloser, error) {
	if mode != consoleapi.ModeNormal && mode != consoleapi.ModeDiagnostic {
		return nil, fmt.Errorf("invalid Console event mode")
	}
	query := url.Values{"after_sequence": []string{strconv.FormatInt(afterSequence, 10)}}
	query.Set("agent_id", agentID)
	query.Set("mode", mode)
	response, err := c.do(ctx, http.MethodGet, openapi.ObserveEventsStreamPath, query, nil, nil, true, "", false)
	if err != nil {
		return nil, err
	}
	return response.Body, nil
}

// Follow starts at the transactional Attach cursor and reconnects from the
// last event successfully applied by the callback. It re-attaches only when
// the server explicitly reports that retention has expired the cursor.
func (c *Client) Follow(
	ctx context.Context,
	agentID string,
	mode string,
	onAttach func(consoleapi.AttachResponse) error,
	onEvent func(openapi.JournalEventReadModel) error,
	onState func(FollowState) error,
) error {
	if onAttach == nil || onEvent == nil || onState == nil {
		return fmt.Errorf("Console follow callbacks are required")
	}
	if mode != consoleapi.ModeNormal && mode != consoleapi.ModeDiagnostic {
		return fmt.Errorf("invalid Console event mode")
	}
	notify := func(state ConnectionState, cursor int64) error {
		if err := onState(FollowState{State: state, Cursor: cursor}); err != nil {
			return &terminalFollowError{err: err}
		}
		return nil
	}
	delay := c.reconnectDelay
	if delay <= 0 {
		delay = time.Second
	}
	var cursor int64
	needsAttach := true
	for {
		var err error
		if needsAttach {
			if err = notify(ConnectionConnecting, cursor); err != nil {
				return err
			}
			var attached consoleapi.AttachResponse
			attached, err = c.Attach(ctx, agentID, mode)
			if err == nil && attached.SnapshotSequence < 0 {
				return fmt.Errorf("Console Attach returned a negative snapshot sequence")
			}
			if err == nil && attached.Mode != mode {
				return fmt.Errorf("Console Attach mode does not match the requested event mode")
			}
			if err == nil {
				if err = onAttach(attached); err != nil {
					return err
				}
				cursor = attached.SnapshotSequence
				needsAttach = false
			}
		}
		if err == nil {
			var stream io.ReadCloser
			stream, err = c.Events(ctx, agentID, mode, cursor)
			if err == nil {
				if err = notify(ConnectionConnected, cursor); err != nil {
					_ = stream.Close()
					return err
				}
				cursor, err = readEventStream(stream, cursor, onEvent)
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
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusConflict && apiErr.Code == openapi.ErrorEventCursorExpired {
			if notifyErr := notify(ConnectionRetentionReattach, cursor); notifyErr != nil {
				return notifyErr
			}
			needsAttach = true
			continue
		}
		if errors.As(err, &apiErr) && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			return err
		}
		if notifyErr := notify(ConnectionDisconnected, cursor); notifyErr != nil {
			return notifyErr
		}
		if notifyErr := notify(ConnectionReconnecting, cursor); notifyErr != nil {
			return notifyErr
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
		if eventID < afterSequence {
			return &terminalFollowError{err: fmt.Errorf("Console SSE event sequence moved backward from %d to %d", afterSequence, eventID)}
		}
		if eventID == afterSequence {
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
		if c.bearerToken == "" {
			return nil, fmt.Errorf("Console is not authenticated")
		}
		request.Header.Set("Authorization", "Bearer "+c.bearerToken)
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
		var structured openapi.ErrorResponse
		if json.Unmarshal(message, &structured) == nil && structured.Code != "" {
			return nil, &APIError{StatusCode: response.StatusCode, Code: structured.Code, Message: structured.Message}
		}
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
