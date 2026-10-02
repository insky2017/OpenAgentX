// Package external provides explicit, durable messages for an existing agent
// session. Reading an inbox never starts a model turn or acknowledges a message.
package external

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	openapi "openagentx/internal/api"
	externalapi "openagentx/internal/api/external"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/credentialstore"
	"openagentx/internal/domain"
	"openagentx/internal/localprofile"
)

const usage = `OAX 原会话通信（保留当前 Codex/Agent 宿主，不启动 Worker）

管理员首次登记身份后绑定；通信凭据自动保存，不打印：
  openagentx external bind --agent rhythm --host codex-desktop --thread <原thread> --peers oneaxe-pay
  openagentx external status --agent rhythm

Agent 自己发信、读信和回复：
  openagentx external send --agent rhythm --to oneaxe-pay --key handoff-01 --content-file /path/request.md
  openagentx external inbox --agent oneaxe-pay
  openagentx external ack --agent oneaxe-pay --message <message_id>
  openagentx external reply --agent oneaxe-pay --message <message_id> --content-file /path/reply.md
  openagentx external status --agent rhythm --message <message_id>

send 默认 consultation；--kind request 表示明确行动请求。相同 --key 和内容可安全重试；改内容须新key。
reply 自动关联、推导接收者，默认key为 reply:<message_id>；不能回复一个结果。
inbox 默认只显示未确认消息；--all 显示历史，--after <sequence> 分页，--watch 持续观察。
ack 只代表读入，不代表业务完成；回复是Agent自述，不是独立验收。

当前没有 Desktop 自动唤醒连接器。绑定不证明在线或可唤醒；inbox --watch 仅观察，不会调度会话。
轮换/换绑须 --expected-generation <当前代次>；revoke 撤销通信身份，不停止原会话。
公共选项：--socket <绝对路径> --credentials <owner凭据文件> --session-file <专用凭据文件>
`

type options struct {
	agent, host, thread, peers, to, key, kind, contentFile, message, sessionFile string
	generation, after                                                            int64
	all, watch, owner                                                            bool
	socket, credentials                                                          localprofile.PathFlag
}

func Execute(args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(out, usage)
		return 0
	}
	command := args[0]
	switch command {
	case "bind", "status", "send", "reply", "inbox", "ack", "revoke":
	default:
		fmt.Fprint(errOut, usage)
		return 2
	}
	var o options
	fs := flag.NewFlagSet("external "+command, flag.ContinueOnError)
	fs.SetOutput(errOut)
	fs.StringVar(&o.agent, "agent", "", "自身领域Agent ID")
	fs.StringVar(&o.host, "host", "", "原宿主标识（登记信息，不是已连接证明）")
	fs.StringVar(&o.thread, "thread", "", "明确的原会话ID")
	fs.StringVar(&o.peers, "peers", "", "可互通的Agent ID，逗号分隔")
	fs.StringVar(&o.to, "to", "", "收信Agent ID")
	fs.StringVar(&o.key, "key", "", "稳定幂等键；同一消息重试复用")
	fs.StringVar(&o.kind, "kind", "consultation", "consultation 或 request")
	fs.StringVar(&o.contentFile, "content-file", "", "正文文件；最多64KiB，不放凭据")
	fs.StringVar(&o.message, "message", "", "消息ID")
	fs.StringVar(&o.sessionFile, "session-file", "", "Agent专用凭据文件绝对路径")
	fs.Int64Var(&o.generation, "expected-generation", 0, "已有绑定的当前代次；初次为0")
	fs.Int64Var(&o.after, "after", 0, "收件箱序号游标")
	fs.BoolVar(&o.all, "all", false, "包括已确认消息")
	fs.BoolVar(&o.watch, "watch", false, "持续观察收件箱，Ctrl-C结束；不会唤醒模型")
	fs.BoolVar(&o.owner, "owner", false, "用owner身份查看绑定，用于丢失凭据后恢复")
	fs.Var(&o.socket, "socket", localprofile.PathUsage(localprofile.SocketPath, "OAX socket"))
	fs.Var(&o.credentials, "credentials", localprofile.PathUsage(localprofile.CredentialsPath, "owner credentials"))
	fs.Usage = func() { fmt.Fprint(errOut, usage); fs.PrintDefaults() }
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(errOut, "unexpected positional arguments")
		return 2
	}
	if o.owner && o.message != "" {
		fmt.Fprintln(errOut, "--owner查看绑定；查看消息请使用参与者的专用身份，不同时指定--message")
		return 2
	}
	if err := domain.ValidateIdentifier("agent_id", o.agent); err != nil {
		fmt.Fprintln(errOut, "请指定 --agent <领域ID>")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, command, o, out); err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(errOut, "命令已中断；写入结果可能未知。发信用相同key/内容核对，绑定用status --owner查询代次。")
			return 130
		}
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

type client struct {
	http  *http.Client
	token string
}

func (c *client) call(ctx context.Context, method, path, key string, in, out any) error {
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	r, err := http.NewRequestWithContext(ctx, method, "http://unix"+externalapi.Prefix+path, body)
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+c.token)
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	response, err := c.http.Do(r)
	if err != nil {
		return fmt.Errorf("OAX通信失败；发信结果可能未知，请用同一key和内容重试: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		var failure openapi.ErrorResponse
		if json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&failure) != nil {
			return fmt.Errorf("OAX HTTP %d", response.StatusCode)
		}
		return fmt.Errorf("OAX HTTP %d (%s): %s", response.StatusCode, failure.Code, failure.Message)
	}
	if out == nil {
		return nil
	}
	// At most 100 messages, each 64 KiB before JSON escaping (up to x6).
	return json.NewDecoder(io.LimitReader(response.Body, 64<<20)).Decode(out)
}

func run(ctx context.Context, command string, o options, out io.Writer) error {
	resolver := localprofile.DefaultResolver()
	socket, err := resolver.Resolve(localprofile.SocketPath, o.socket.Override())
	if err != nil {
		return err
	}
	ownerPath, err := resolver.Resolve(localprofile.CredentialsPath, o.credentials.Override())
	if err != nil {
		return err
	}
	if o.sessionFile == "" {
		o.sessionFile = filepath.Join(filepath.Dir(ownerPath.Path), "external", o.agent, "credentials.json")
	}
	if !filepath.IsAbs(o.sessionFile) || filepath.Clean(o.sessionFile) == ownerPath.Path {
		return fmt.Errorf("session-file must be absolute and separate from owner credentials")
	}
	probe, err := consoleclient.NewUnixClient(socket.Path)
	if err != nil {
		return err
	}
	installation, err := probe.ProbeInstallation(ctx)
	if err != nil {
		return err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket.Path)
	}}
	defer transport.CloseIdleConnections()
	c := &client{http: &http.Client{Transport: transport, Timeout: 15 * time.Second}}
	store, err := credentialstore.New(o.sessionFile, credentialstore.Options{})
	if err != nil {
		return err
	}
	if command == "bind" || command == "revoke" || o.owner {
		ownerStore, err := credentialstore.New(ownerPath.Path, credentialstore.Options{})
		if err != nil {
			return err
		}
		credential, err := ownerStore.Load(socket.Path, installation.InstallationID, "")
		if err != nil {
			return fmt.Errorf("owner登录不可用，请运行 openagentx console login: %w", err)
		}
		c.token = credential.Token
	} else {
		credential, err := store.Load(socket.Path, installation.InstallationID, o.agent)
		if err != nil {
			return fmt.Errorf("未找到 %s 的通信凭据，请先 external bind: %w", o.agent, err)
		}
		c.token = credential.Token
	}
	if o.owner && command != "status" {
		return fmt.Errorf("--owner仅用于status；发信必须使用Agent专用身份")
	}
	switch command {
	case "bind":
		in := domain.BindExternalSessionInput{AgentID: o.agent, HostID: o.host, ThreadID: o.thread, ExpectedGeneration: o.generation}
		for _, peer := range strings.Split(o.peers, ",") {
			in.AllowedPeerAgentIDs = append(in.AllowedPeerAgentIDs, strings.TrimSpace(peer))
		}
		if err := in.Validate(); err != nil {
			return err
		}
		var result externalapi.BindResponse
		_, err := store.Replace(func() (credentialstore.Credential, error) {
			if err := c.call(ctx, "POST", "bindings", "", in, &result); err != nil {
				return credentialstore.Credential{}, err
			}
			if result.Binding == nil || result.Binding.AgentID != o.agent || result.Token == "" {
				return credentialstore.Credential{}, fmt.Errorf("invalid bind response")
			}
			return credentialstore.Credential{SocketPath: socket.Path, InstallationID: installation.InstallationID, Username: o.agent, TokenID: result.Binding.ID, Token: result.Token, AbsoluteExpires: result.Binding.TokenExpiresAt}, nil
		})
		if err != nil {
			return fmt.Errorf("绑定或凭据保存未完成；先用 status --owner 查询当前代次后再恢复: %w", err)
		}
		return emit(out, map[string]any{"binding": result.Binding, "credential_file": o.sessionFile, "automatic_delivery": false})
	case "revoke":
		if o.generation < 1 {
			return fmt.Errorf("revoke需要 --expected-generation")
		}
		var result map[string]bool
		if err := c.call(ctx, "POST", "bindings/"+url.PathEscape(o.agent)+"/revoke", "", map[string]int64{"expected_generation": o.generation}, &result); err != nil {
			return err
		}
		return emit(out, result)
	case "status":
		path := "status"
		if o.owner {
			path = "bindings/" + url.PathEscape(o.agent)
		} else if o.message != "" {
			path = "messages/" + url.PathEscape(o.message)
		}
		var result json.RawMessage
		if err := c.call(ctx, "GET", path, "", nil, &result); err != nil {
			return err
		}
		return emit(out, result)
	case "send", "reply":
		content, err := readContent(o.contentFile)
		if err != nil {
			return err
		}
		in := domain.SendExternalMessageInput{TargetAgentID: o.to, Kind: domain.ExternalMessageKind(o.kind), Content: string(content), IdempotencyKey: o.key}
		if command == "reply" {
			if o.to != "" {
				return fmt.Errorf("reply接收者由原请求推导，不使用--to")
			}
			in.Kind = domain.ExternalMessageResult
			in.ReplyToMessageID = o.message
			if in.IdempotencyKey == "" {
				in.IdempotencyKey = "reply:" + o.message
			}
		}
		if err := in.Validate(); err != nil {
			return err
		}
		var result domain.ExternalMessage
		if err := c.call(ctx, "POST", "messages", in.IdempotencyKey, in, &result); err != nil {
			return err
		}
		return emit(out, result)
	case "ack":
		if o.message == "" {
			return fmt.Errorf("ack需要--message")
		}
		var result domain.ExternalMessage
		if err := c.call(ctx, "POST", "messages/"+url.PathEscape(o.message)+"/ack", "", struct{}{}, &result); err != nil {
			return err
		}
		return emit(out, result)
	case "inbox":
		return inbox(ctx, c, o, out)
	}
	return nil
}

func readContent(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("请指定--content-file；正文不放凭据")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("content-file must be a regular file")
	}
	content, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil {
		return nil, err
	}
	if len(content) > 65536 {
		return nil, fmt.Errorf("正文超过64KiB")
	}
	return content, nil
}

func inbox(ctx context.Context, c *client, o options, out io.Writer) error {
	if o.after < 0 {
		return fmt.Errorf("after必须非负")
	}
	after := o.after
	for {
		var result struct {
			Messages []domain.ExternalMessage `json:"messages"`
		}
		if err := c.call(ctx, "GET", fmt.Sprintf("inbox?after=%d&limit=100", after), "", nil, &result); err != nil {
			return err
		}
		visible := make([]domain.ExternalMessage, 0, len(result.Messages))
		for _, m := range result.Messages {
			after = m.Sequence
			if o.all || m.AcknowledgedAt == nil {
				visible = append(visible, m)
			}
		}
		if !o.watch || len(visible) > 0 {
			if err := emit(out, map[string]any{"messages": visible, "next_after": after, "has_more": len(result.Messages) == 100}); err != nil {
				return err
			}
		}
		if !o.watch {
			return nil
		}
		if len(result.Messages) == 100 {
			continue
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

func emit(out io.Writer, v any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(v)
}
