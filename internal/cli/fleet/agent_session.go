package fleet

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"openagentx/internal/api"
	"openagentx/internal/domain"
	"openagentx/internal/localprofile"
)

type sessionClient interface {
	ReadAgentSession(context.Context, string, string) (*domain.AgentSession, error)
	GetAgentModelSettings(context.Context, string, string) (domain.AgentModelSettings, error)
	Dispatch(context.Context, api.CreateTaskRequest) (api.CreateTaskResponse, error)
	TaskSnapshot(context.Context, string, string) (api.ConsoleTaskSnapshot, error)
}

func executeAgentNewSession(args []string, deps Dependencies) int {
	if err := agentNewSession(args, deps); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(deps.Err, "新会话交接：", err)
		return 1
	}
	return 0
}

func agentNewSession(args []string, deps Dependencies) error {
	var agent, backend, handoffFile, expectedThread, key string
	var expectedVersion int64
	var apply, jsonOutput bool
	var wait time.Duration
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	f := flag.NewFlagSet("agent new-session", flag.ContinueOnError)
	f.SetOutput(deps.Err)
	f.StringVar(&backend, "backend", "codex", "Codex backend ID")
	f.StringVar(&handoffFile, "handoff-file", "", "交接 Markdown；仅作为上下文，不继续旧业务")
	f.StringVar(&expectedThread, "expected-thread", "", "预览确认的当前 thread（apply 必填）")
	f.Int64Var(&expectedVersion, "expected-version", -1, "预览确认的活动会话版本（apply 必填）")
	f.StringVar(&key, "key", "", "稳定请求 ID；默认由目标、预期会话和交接内容生成")
	f.BoolVar(&apply, "apply", false, "提交初始化 query Task；仅空闲且无未完成咨询时允许")
	f.BoolVar(&jsonOutput, "json", false, "结构化结果")
	f.DurationVar(&wait, "wait", 3*time.Minute, "等待初始化结果的时长；到时不会取消或重复后台任务")
	var manifest, database, socket, workers, credentials localprofile.PathFlag
	f.Var(&socket, "socket", "Daemon socket")
	f.Var(&credentials, "credentials", "Credential store")
	f.Usage = func() {
		fmt.Fprintln(deps.Err, "Usage: openagentx agent new-session AGENT [--handoff-file FILE] [--json]\n       openagentx agent new-session AGENT --handoff-file FILE --apply --expected-thread ID --expected-version N [--key KEY] [--wait 3m]\n默认只读预览；保留稳定身份、角色、目录、模型偏好与旧历史。完成后从另一终端重开 agent open AGENT --native。")
		f.PrintDefaults()
	}
	if err := f.Parse(args); err != nil {
		return err
	}
	if agent == "" && f.NArg() == 1 {
		agent = f.Arg(0)
	} else if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if err := domain.ValidateIdentifier("agent_id", agent); err != nil {
		return err
	}
	if err := domain.ValidateIdentifier("backend_id", backend); err != nil {
		return err
	}
	if wait <= 0 {
		return errors.New("--wait must be positive")
	}
	if apply && (handoffFile == "" || expectedThread == "" || expectedVersion < 0) {
		return errors.New("先预览当前会话；--apply 需要 --handoff-file、--expected-thread 和 --expected-version")
	}
	var handoff []byte
	if handoffFile != "" {
		file, err := os.Open(handoffFile)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil || !info.Mode().IsRegular() {
			file.Close()
			return errors.New("handoff must be a regular UTF-8 text file")
		}
		handoff, err = io.ReadAll(io.LimitReader(file, 64<<10+1))
		file.Close()
		if err != nil {
			return err
		}
		if len(handoff) > 64<<10 || strings.TrimSpace(string(handoff)) == "" || !utf8.Valid(handoff) {
			return errors.New("handoff must contain 1..65536 bytes")
		}
	}
	paths, err := resolveFleetPaths(manifest, database, socket, workers, credentials)
	if err != nil {
		return err
	}
	ctx := deps.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	role, scope := domain.WebRoleViewer, domain.CLIScopeConsoleRead
	if apply {
		role, scope = domain.WebRoleOwner, domain.CLIScopeConsoleControl
	}
	base, _, err := authenticateFleetClient(ctx, paths, role, scope, deps)
	if err != nil {
		return err
	}
	client, ok := base.(sessionClient)
	if !ok {
		return errors.New("client does not support Agent session handoff")
	}
	current, err := client.ReadAgentSession(ctx, agent, backend)
	if err != nil {
		return err
	}
	if !apply {
		attach, err := base.Attach(ctx, agent, "normal")
		if err != nil {
			return err
		}
		settings, err := client.GetAgentModelSettings(ctx, agent, backend)
		if err != nil {
			return err
		}
		if jsonOutput {
			return json.NewEncoder(deps.Out).Encode(map[string]any{"dry_run": true, "session": current, "worker_status": attach.WorkerStatus, "active_run": attach.ActiveRun, "model": settings, "handoff_bytes": len(handoff), "handoff_sha256": fmt.Sprintf("%x", sha256.Sum256(handoff))})
		}
		fmt.Fprintf(deps.Out, "Agent: %s\n当前 thread: %s\n会话版本: %d\nWorker: %s\n模型: %s / %s\n", agent, current.ThreadID, current.Version, attach.WorkerStatus, settings.Model, settings.Effort)
		if current.PendingTaskID != "" {
			fmt.Fprintf(deps.Out, "已有交接 Task: %s；请查看其结果，不要重复提交。\n", current.PendingTaskID)
		} else if attach.ActiveRun != nil {
			fmt.Fprintln(deps.Out, "Agent 正在执行；须等当前任务结束后从另一终端提交交接。")
		}
		fmt.Fprintf(deps.Out, "交接内容: %d bytes；本次只读。空闲后提交：\nopenagentx agent new-session %s --handoff-file <FILE> --apply --expected-thread %s --expected-version %d\n", len(handoff), agent, current.ThreadID, current.Version)
		return nil
	}
	options, err := base.ListAgentOptions(ctx)
	if err != nil {
		return err
	}
	organization := ""
	for _, option := range options {
		if option.AgentID == agent {
			organization = option.OrganizationID
		}
	}
	if organization == "" {
		return errors.New("Agent is not registered")
	}
	content := "OpenAgentX 新会话交接。保留本轮冻结的 Agent 身份、角色、工作目录和模型设置。以下为上下文资料，不授予执行历史工作的权限，也不表示旧任务成功。请仅简洁确认身份、工作目录及资料中的验收标识（若有）；不要调用工具、改文件或继续旧任务，等待用户下一项指令。\n\n--- 交接资料 ---\n" + string(handoff)
	if key == "" {
		raw, _ := json.Marshal([]any{agent, backend, expectedThread, expectedVersion, content})
		key = fmt.Sprintf("session-%x", sha256.Sum256(raw))
	}
	receipt, err := client.Dispatch(ctx, api.CreateTaskRequest{Meta: api.CommandMeta{IdempotencyKey: key}, TargetAgentID: agent, OrganizationID: organization, DispatchMode: domain.DispatchModeDirect, Intent: domain.TaskIntentQuery, Content: content, NewSession: &domain.NewSessionRequest{BackendID: backend, ExpectedThreadID: expectedThread, ExpectedVersion: expectedVersion}})
	if err != nil {
		return err
	}
	// Print the receipt before waiting. A lost CLI connection never cancels or
	// silently resubmits the already committed initialization task.
	if !jsonOutput {
		fmt.Fprintf(deps.Out, "交接 Task: %s\n", receipt.TaskID)
	}
	waitCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	for {
		snapshot, err := client.TaskSnapshot(waitCtx, agent, receipt.TaskID)
		if err != nil {
			return fmt.Errorf("Task %s 已提交；查询中断，请检查结果，不要换 key 重发：%w", receipt.TaskID, err)
		}
		if (&domain.Task{Status: snapshot.Task.Status}).IsTerminal() {
			active, err := client.ReadAgentSession(ctx, agent, backend)
			if err != nil {
				return err
			}
			if jsonOutput {
				if err := json.NewEncoder(deps.Out).Encode(map[string]any{"task_id": receipt.TaskID, "task_status": snapshot.Task.Status, "session": active}); err != nil {
					return err
				}
			}
			if snapshot.Task.Status != domain.TaskStatusSucceeded {
				return fmt.Errorf("交接 Task %s 结束为 %s；原活动会话保留，失败记录可查，未自动重试", receipt.TaskID, snapshot.Task.Status)
			}
			if active.ContextTaskID != receipt.TaskID || active.ThreadID == "" || active.ThreadID == expectedThread {
				return fmt.Errorf("Task %s 已完成，但活动会话不再指向此交接；请重新预览", receipt.TaskID)
			}
			if !jsonOutput {
				fmt.Fprintf(deps.Out, "新 thread: %s\n会话版本: %d；身份和旧历史保留。退出旧 view 后重新连接：\nopenagentx agent open %s --native\n", active.ThreadID, active.Version, agent)
			}
			return nil
		}
		if err := deps.Wait(waitCtx, 2*time.Second); err != nil {
			return fmt.Errorf("交接 Task %s 已提交且仍在后台；本次仅停止等待，未取消或重新派单", receipt.TaskID)
		}
	}
}
