package console

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	"openagentx/internal/domain"
)

type testClient struct {
	mu              sync.Mutex
	attached        consoleapi.AttachResponse
	loginCount      int
	attachCount     int
	followCount     int
	dispatches      []openapi.CreateTaskRequest
	steers          []openapi.CreateMessageRequest
	cancels         []openapi.CancelTaskRequest
	approvals       []openapi.DecideApprovalRequest
	workerCommands  []domain.WorkerCommandKind
	attachedAgentID string
}

func (c *testClient) Login(context.Context, string, string) error {
	c.loginCount++
	return nil
}

func (c *testClient) Attach(_ context.Context, agentID, _ string) (consoleapi.AttachResponse, error) {
	c.attachCount++
	c.attachedAgentID = agentID
	response := c.attached
	response.AgentID = agentID
	return response, nil
}

func (c *testClient) Follow(ctx context.Context, agentID, _ string, _ int64, onAttach func(consoleapi.AttachResponse) error, onEvent func(openapi.JournalEventReadModel) error) error {
	c.followCount++
	response := c.attached
	response.AgentID = agentID
	if err := onAttach(response); err != nil {
		return err
	}
	if err := onEvent(openapi.JournalEventReadModel{Sequence: 1, ID: "event-1", AggregateType: "task", AggregateID: "task-1", EventType: "task.created"}); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func (c *testClient) Dispatch(_ context.Context, request openapi.CreateTaskRequest) (openapi.CreateTaskResponse, error) {
	c.dispatches = append(c.dispatches, request)
	return openapi.CreateTaskResponse{}, nil
}

func (c *testClient) Steer(_ context.Context, _ string, request openapi.CreateMessageRequest) (openapi.CreateMessageResponse, error) {
	c.steers = append(c.steers, request)
	return openapi.CreateMessageResponse{}, nil
}

func (c *testClient) Cancel(_ context.Context, _ string, request openapi.CancelTaskRequest) (openapi.CancelTaskResponse, error) {
	c.cancels = append(c.cancels, request)
	return openapi.CancelTaskResponse{}, nil
}

func (c *testClient) DecideApproval(_ context.Context, _ string, request openapi.DecideApprovalRequest) (openapi.DecideApprovalResponse, error) {
	c.approvals = append(c.approvals, request)
	return openapi.DecideApprovalResponse{}, nil
}

func (c *testClient) WorkerCommand(_ context.Context, _ string, _ int64, kind domain.WorkerCommandKind, _ string, _ bool) (openapi.WorkerCommandResponse, error) {
	c.workerCommands = append(c.workerCommands, kind)
	return openapi.WorkerCommandResponse{}, nil
}

type testTmux struct {
	current string
	windows string
	err     error
	calls   []string
}

func (t *testTmux) Run(_ context.Context, args ...string) (string, error) {
	t.calls = append(t.calls, strings.Join(args, " "))
	if t.err != nil {
		return "", t.err
	}
	if args[0] == "display-message" {
		return t.current, nil
	}
	if args[0] == "list-windows" {
		return t.windows, nil
	}
	return "", fmt.Errorf("unexpected tmux command")
}

func consoleDeps(client Client, input string, interactive bool) (Dependencies, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	return Dependencies{
		Out: out, Err: errOut, In: strings.NewReader(input), IsInteractive: func() bool { return interactive },
		ReadPassword: func(string) (string, error) { return "password", nil },
		NewClient:    func(string) (Client, error) { return client, nil },
	}, out, errOut
}

func TestInteractiveAttachUsesOfficialClientForCommandsWhileFollowing(t *testing.T) {
	client := &testClient{attached: consoleapi.AttachResponse{
		WorkerInstanceID: "worker-quote", Generation: 7, WorkerStatus: domain.WorkerStatusOnline,
	}}
	input := strings.Join([]string{
		"/dispatch inspect repository",
		"/steer task-1 2 continue carefully",
		"/cancel task-1 3",
		"/approve approval-1 4",
		"/reject approval-2 5",
		"/down",
		"/foreground",
		"/quit",
	}, "\n") + "\n"
	deps, out, stderr := consoleDeps(client, input, true)
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--agent", "quote", "--organization", "org-1"}, deps); code != 0 {
		t.Fatalf("attach code=%d stderr=%s", code, stderr.String())
	}
	if client.followCount != 1 || len(client.dispatches) != 1 || len(client.steers) != 1 || len(client.cancels) != 1 || len(client.approvals) != 2 ||
		!reflect.DeepEqual(client.workerCommands, []domain.WorkerCommandKind{domain.WorkerCommandStop}) {
		t.Fatalf("official client calls follow=%d dispatch=%d steer=%d cancel=%d approvals=%d worker=%v", client.followCount, len(client.dispatches), len(client.steers), len(client.cancels), len(client.approvals), client.workerCommands)
	}
	if client.dispatches[0].TargetAgentID != "quote" || client.dispatches[0].OrganizationID != "org-1" || client.steers[0].Meta.ExpectedVersion != 2 ||
		client.cancels[0].Meta.ExpectedVersion != 3 || client.approvals[0].Decision != domain.ApprovalDecisionApprove || client.approvals[1].Decision != domain.ApprovalDecisionReject {
		t.Fatalf("unexpected API requests dispatch=%+v steer=%+v cancel=%+v approvals=%+v", client.dispatches, client.steers, client.cancels, client.approvals)
	}
	if !strings.Contains(out.String(), foregroundUnavailable) || !strings.Contains(out.String(), replPrompt) || !strings.Contains(out.String(), "task.created") {
		t.Fatalf("interactive output missing command bar or Follow event: %s", out.String())
	}
}

func TestAttachOnceIsExplicitNonInteractiveAndDoesNotReadCommands(t *testing.T) {
	client := &testClient{attached: consoleapi.AttachResponse{WorkerStatus: domain.WorkerStatusOffline}}
	deps, _, stderr := consoleDeps(client, "/dispatch must-not-run\n", false)
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--agent", "quote", "--once"}, deps); code != 0 {
		t.Fatalf("once code=%d stderr=%s", code, stderr.String())
	}
	if client.attachCount != 1 || client.followCount != 0 || len(client.dispatches) != 0 {
		t.Fatalf("once behavior attach=%d follow=%d dispatch=%d", client.attachCount, client.followCount, len(client.dispatches))
	}

	client = &testClient{}
	deps, _, stderr = consoleDeps(client, "", false)
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--agent", "quote"}, deps); code != 2 {
		t.Fatalf("non-interactive continuous attach code=%d", code)
	}
	if client.loginCount != 0 || !strings.Contains(stderr.String(), "requires an interactive TTY") {
		t.Fatalf("non-interactive attach reached API or lacked guidance: login=%d stderr=%s", client.loginCount, stderr.String())
	}
}

func TestAttachDefaultsAgentFromExactManagedTmuxWindow(t *testing.T) {
	client := &testClient{attached: consoleapi.AttachResponse{WorkerStatus: domain.WorkerStatusOffline}}
	tmux := &testTmux{current: "agentx\tquote\t0\t1\n", windows: "overview\nquote\nrisk\n"}
	deps, _, stderr := consoleDeps(client, "", false)
	deps.Tmux = tmux
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--once"}, deps); code != 0 {
		t.Fatalf("tmux default attach code=%d stderr=%s", code, stderr.String())
	}
	if client.attachedAgentID != "quote" || len(tmux.calls) != 2 || !strings.HasPrefix(tmux.calls[0], "display-message -p -F") {
		t.Fatalf("resolved Agent=%q tmux calls=%v", client.attachedAgentID, tmux.calls)
	}

	explicit := &testTmux{err: fmt.Errorf("must not be called")}
	deps, _, stderr = consoleDeps(client, "", false)
	deps.Tmux = explicit
	if code := Execute([]string{"attach", "--socket", "/run/openagentx.sock", "--agent", "risk", "--once"}, deps); code != 0 {
		t.Fatalf("explicit Agent attach code=%d stderr=%s", code, stderr.String())
	}
	if len(explicit.calls) != 0 {
		t.Fatalf("explicit --agent unexpectedly queried tmux: %v", explicit.calls)
	}
}

func TestTmuxAgentResolutionFailsClosedForWrongOrAmbiguousWindow(t *testing.T) {
	tests := map[string]*testTmux{
		"wrong-session": {current: "other\tquote\t0\t1\n", windows: "quote\n"},
		"overview":      {current: "agentx\toverview\t0\t1\n", windows: "overview\n"},
		"wrong-pane":    {current: "agentx\tquote\t1\t1\n", windows: "quote\n"},
		"unmanaged":     {current: "agentx\tquote\t0\t\n", windows: "quote\n"},
		"duplicate":     {current: "agentx\tquote\t0\t1\n", windows: "quote\nquote\n"},
	}
	for name, tmux := range tests {
		t.Run(name, func(t *testing.T) {
			if agentID, err := resolveAgentFromTmux(context.Background(), tmux); err == nil || agentID != "" {
				t.Fatalf("resolved ambiguous Agent %q err=%v", agentID, err)
			}
		})
	}
}
