package overview

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
)

type row struct {
	Option    domain.ConsoleAgentOption
	Attached  consoleapi.AttachResponse
	Task      *openapi.ConsoleTaskSnapshot
	Error     string
	UpdatedAt time.Time
}

type refreshResult struct {
	Rows []row
	At   time.Time
	Err  error
}

type reader struct {
	socket, credentials string
	store               *credentialstore.Store
	client              *consoleclient.Client
	now                 func() time.Time
}

// All API requests below are GETs. Authentication reuses the existing Console
// credential without login, rotation, deletion, or a Worker lifecycle operation.
func (r *reader) authorize(ctx context.Context) error {
	if r.client == nil {
		c, err := consoleclient.NewUnixClient(r.socket)
		if err != nil {
			return fmt.Errorf("控制面未连接")
		}
		r.client = c
	}
	probe, err := r.client.ProbeInstallation(ctx)
	if err != nil {
		return publicReadError(err)
	}
	credential, err := r.store.Load(r.socket, probe.InstallationID, "")
	if err != nil {
		return fmt.Errorf("需要登录：%s", r.loginCommand())
	}
	if !r.now().Before(credential.AbsoluteExpires) {
		return fmt.Errorf("登录已过期：%s", r.loginCommand())
	}
	if err := r.client.UseCredential(ctx, credential.InstallationID, credential.Token); err != nil {
		return publicReadError(err)
	}
	session, err := r.client.Session(ctx)
	if err != nil {
		return publicReadError(err)
	}
	if session.InstallationID != credential.InstallationID || session.Principal.TokenID != credential.TokenID || session.Principal.Username != credential.Username || !session.AbsoluteExpiresAt.Equal(credential.AbsoluteExpires) || !r.now().Before(session.AbsoluteExpiresAt) {
		return fmt.Errorf("登录身份已改变，请重新登录")
	}
	if !has(session.Principal.Scopes, string(domain.CLIScopeConsoleRead)) || !(has(session.Principal.Roles, "viewer") || has(session.Principal.Roles, "operator") || has(session.Principal.Roles, "owner")) {
		return fmt.Errorf("当前登录缺少总览只读权限 console.read")
	}
	return nil
}

func (r *reader) loginCommand() string {
	return "openagentx console login --socket " + shellQuote(r.socket) + " --credentials " + shellQuote(r.credentials)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func has(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
func publicReadError(err error) error {
	var apiErr *consoleclient.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 401:
			return fmt.Errorf("登录失效，请运行 console login 后重试")
		case 403:
			return fmt.Errorf("只读权限不足")
		default:
			return fmt.Errorf("控制面读取失败（HTTP %d）", apiErr.StatusCode)
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("控制面读取超时")
	}
	return fmt.Errorf("控制面连接中断；保留上次状态")
}

func (r *reader) refresh(ctx context.Context) refreshResult {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := r.authorize(ctx); err != nil {
		return refreshResult{Err: err}
	}
	options, err := r.client.ListAgentOptions(ctx)
	if err != nil {
		return refreshResult{Err: publicReadError(err)}
	}
	rows := make([]row, len(options))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				entry := row{Option: options[i]}
				attached, readErr := r.client.Attach(ctx, entry.Option.AgentID, consoleapi.ModeNormal)
				if readErr == nil && attached.AgentID != entry.Option.AgentID {
					readErr = fmt.Errorf("invalid Agent projection")
				}
				if readErr == nil {
					entry.Attached = attached
					id := ""
					if attached.ActiveRun != nil {
						id = attached.ActiveRun.TaskID
					} else if attached.SuggestedTask != nil {
						id = attached.SuggestedTask.TaskID
					}
					if id != "" {
						task, taskErr := r.client.TaskSnapshot(ctx, entry.Option.AgentID, id)
						readErr = taskErr
						if taskErr == nil {
							entry.Task = &task
						}
					}
				}
				if readErr != nil {
					entry.Error = publicReadError(readErr).Error()
				} else {
					entry.UpdatedAt = r.now()
				}
				rows[i] = entry
			}
		}()
	}
	for i := range options {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rowPriority(rows[i]), rowPriority(rows[j])
		if a != b {
			return a < b
		}
		return rows[i].Option.AgentID < rows[j].Option.AgentID
	})
	return refreshResult{Rows: rows, At: r.now()}
}

func rowPriority(r row) int {
	if r.Error != "" {
		return 0
	}
	if r.Task != nil {
		switch r.Task.Task.Status {
		case domain.TaskStatusWaitingApproval, domain.TaskStatusWaitingInput, domain.TaskStatusUncertain, domain.TaskStatusFailed:
			return 0
		}
	}
	if r.Attached.Readiness == nil || !r.Attached.Readiness.Ready {
		return 1
	}
	if r.Attached.ActiveRun != nil || r.Attached.SuggestedTask != nil {
		return 2
	}
	return 3
}
