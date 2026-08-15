package connector

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	paneIDRegex   = regexp.MustCompile(`^%[0-9]+$`)
	addressRegex  = regexp.MustCompile(`^[a-zA-Z0-9_\-\.\:\%]+$`)
	ErrBadAddress = errors.New("invalid tmux pane address format")
	ErrPaneDead   = errors.New("tmux pane is dead")
)

type Runner interface {
	Run(ctx context.Context, stdin string, name string, args ...string) (string, error)
}

type DefaultRunner struct{}

func (r *DefaultRunner) Run(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return stdout.String(), fmt.Errorf("%w: stderr: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

type TmuxConnector struct {
	runner Runner
}

func NewTmuxConnector(runner Runner) *TmuxConnector {
	if runner == nil {
		runner = &DefaultRunner{}
	}
	return &TmuxConnector{runner: runner}
}

func ValidateAddress(address string) error {
	if strings.ContainsAny(address, "\r\n\t") {
		return fmt.Errorf("%w: address cannot contain control characters", ErrBadAddress)
	}
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return fmt.Errorf("%w: address cannot be empty", ErrBadAddress)
	}
	if paneIDRegex.MatchString(trimmed) {
		return nil
	}
	if addressRegex.MatchString(trimmed) {
		return nil
	}
	return fmt.Errorf("%w: '%s'", ErrBadAddress, trimmed)
}

func (c *TmuxConnector) ProbePane(ctx context.Context, address string) (string, error) {
	if err := ValidateAddress(address); err != nil {
		return "", err
	}
	out, err := c.runner.Run(ctx, "", "tmux", "display-message", "-p", "-t", address, "#{pane_id} #{pane_dead}")
	if err != nil {
		return "", fmt.Errorf("tmux pane probe failed for '%s': %w", address, err)
	}
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty tmux probe output for '%s'", address)
	}
	paneID := fields[0]
	if len(fields) >= 2 && fields[1] == "1" {
		return "", fmt.Errorf("%w: pane '%s' (pane_dead=1)", ErrPaneDead, address)
	}
	return paneID, nil
}

type DeliveryDisposition string

const (
	DispositionNotified       DeliveryDisposition = "notified"
	DispositionDeliveryFailed DeliveryDisposition = "delivery_failed"
	DispositionSkipped        DeliveryDisposition = "skipped"
)

type DeliveryResult struct {
	Disposition DeliveryDisposition `json:"disposition"`
	PaneID      string              `json:"pane_id,omitempty"`
	Error       string              `json:"error,omitempty"`
}

func (c *TmuxConnector) deliverText(ctx context.Context, address string, text string) DeliveryResult {
	if err := ValidateAddress(address); err != nil {
		return DeliveryResult{
			Disposition: DispositionDeliveryFailed,
			Error:       err.Error(),
		}
	}

	paneID, err := c.ProbePane(ctx, address)
	if err != nil {
		return DeliveryResult{
			Disposition: DispositionDeliveryFailed,
			Error:       err.Error(),
		}
	}

	randBytes := make([]byte, 4)
	_, _ = rand.Read(randBytes)
	bufName := fmt.Sprintf("ab-%d-%s", time.Now().UnixNano(), hex.EncodeToString(randBytes))

	// 1. Load buffer via stdin
	if _, err := c.runner.Run(ctx, text, "tmux", "load-buffer", "-b", bufName, "-"); err != nil {
		return DeliveryResult{
			Disposition: DispositionDeliveryFailed,
			PaneID:      paneID,
			Error:       fmt.Sprintf("failed to load tmux buffer: %v", err),
		}
	}

	// 2. Paste buffer into target pane (-d deletes the buffer after paste)
	if _, err := c.runner.Run(ctx, "", "tmux", "paste-buffer", "-b", bufName, "-d", "-t", address); err != nil {
		// Try cleaning up buffer if paste failed
		_, _ = c.runner.Run(ctx, "", "tmux", "delete-buffer", "-b", bufName)
		return DeliveryResult{
			Disposition: DispositionDeliveryFailed,
			PaneID:      paneID,
			Error:       fmt.Sprintf("failed to paste tmux buffer into pane '%s': %v", address, err),
		}
	}

	// 3. Send Enter key to execute or display line
	if _, err := c.runner.Run(ctx, "", "tmux", "send-keys", "-t", address, "Enter"); err != nil {
		return DeliveryResult{
			Disposition: DispositionDeliveryFailed,
			PaneID:      paneID,
			Error:       fmt.Sprintf("failed to send Enter key to pane '%s': %v", address, err),
		}
	}

	return DeliveryResult{
		Disposition: DispositionNotified,
		PaneID:      paneID,
	}
}

func (c *TmuxConnector) NotifyBootstrap(ctx context.Context, address string, agentID string, role string, generation int64, instructionsPath string) DeliveryResult {
	if strings.ContainsAny(agentID, "\r\n\t") || strings.ContainsAny(role, "\r\n\t") || strings.ContainsAny(instructionsPath, "\r\n\t") {
		return DeliveryResult{
			Disposition: DispositionDeliveryFailed,
			Error:       "control characters in bootstrap parameters",
		}
	}
	text := fmt.Sprintf("[AgentBus Bootstrap] agent_id=%s role=%s generation=%d. Read %s, then use AgentBus CLI: session ready --agent %s --generation %d",
		agentID, role, generation, instructionsPath, agentID, generation)
	return c.deliverText(ctx, address, text)
}

func (c *TmuxConnector) NotifyTask(ctx context.Context, address string, agentID string, role string, instructionsPath string, taskID string, isNewTask bool) DeliveryResult {
	if strings.ContainsAny(agentID, "\r\n\t") || strings.ContainsAny(role, "\r\n\t") || strings.ContainsAny(instructionsPath, "\r\n\t") || strings.ContainsAny(taskID, "\r\n\t") {
		return DeliveryResult{
			Disposition: DispositionDeliveryFailed,
			Error:       "control characters in task notification parameters",
		}
	}

	var text string
	roleTag := ""
	if role != "" {
		roleTag = fmt.Sprintf(" role=%s", role)
	}
	readHint := ""
	if instructionsPath != "" {
		readHint = fmt.Sprintf(" If role context is uncertain, read %s.", instructionsPath)
	}

	if isNewTask {
		text = fmt.Sprintf("[AgentBus][agent=%s%s] New task %s.%s Use AgentBus CLI: task get %s --agent %s",
			agentID, roleTag, taskID, readHint, taskID, agentID)
	} else {
		text = fmt.Sprintf("[AgentBus][agent=%s%s] New message for task %s.%s Use AgentBus CLI: task get %s --agent %s",
			agentID, roleTag, taskID, readHint, taskID, agentID)
	}

	return c.deliverText(ctx, address, text)
}

func (c *TmuxConnector) Notify(ctx context.Context, address string, targetAgentID string, taskID string, isNewTask bool) DeliveryResult {
	return c.NotifyTask(ctx, address, targetAgentID, "", "", taskID, isNewTask)
}
