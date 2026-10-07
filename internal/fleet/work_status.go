package fleet

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const WorkStateOption = "@oax_state"

// SpinnerFrame is deliberately outside tmux's format language. tmux calls the
// tiny CLI helper on its one-second status refresh; no model or daemon is used.
func SpinnerFrame(now time.Time) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return " " + frames[now.Unix()%int64(len(frames))]
}

// WorkStatus is a best-effort view of one Worker's authoritative activity.
// Observe never waits for tmux. All tmux work is bounded and serialized here.
// Reconciliation also discovers windows opened after Worker startup.
type WorkStatus struct {
	runner          CommandRunner
	agentID, binary string
	running         atomic.Bool
	wake            chan struct{}
	stop            chan struct{}
	done            chan struct{}
}

func StartWorkStatus(runner CommandRunner, agentID, binary string) *WorkStatus {
	s := &WorkStatus{runner: runner, agentID: agentID, binary: binary,
		wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{})}
	go s.loop()
	return s
}

func (s *WorkStatus) Observe(running bool) {
	s.running.Store(running)
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *WorkStatus) Close() {
	close(s.stop)
	<-s.done
}

func (s *WorkStatus) loop() {
	defer close(s.done)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		s.project(s.running.Load())
		select {
		case <-s.stop:
			s.project(false)
			return
		case <-s.wake:
		case <-ticker.C:
		}
	}
}

func (s *WorkStatus) project(running bool) {
	if s.runner == nil || s.agentID == "" || s.binary == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// Workers are background services with no TMUX_PANE. Discover only windows
	// already bound by Fleet (native binding uses the caller's exact TMUX_PANE),
	// then address their stable window IDs, never the user's active window.
	out, err := s.runner.Run(ctx, "list-windows", "-a", "-F", "#{window_id}\t#{@openagentx_managed}\t#{@openagentx_agent_id}\t#{session_id}")
	if err != nil {
		return
	}
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || !validWindowID(fields[0]) || fields[1] != "1" || fields[2] != s.agentID {
			continue
		}
		window := fields[0]
		if !seen[window] {
			seen[window] = true
			state := "idle"
			if running {
				state = "running"
			}
			// Best effort at every stage; no failure escapes into the Worker.
			if _, err := s.runner.Run(ctx, "set-option", "-w", "-t", window, WorkStateOption, state); err != nil {
				continue
			}
			s.installFormats(ctx, window)
		}
		if strings.HasPrefix(fields[3], "$") {
			if _, err := strconv.ParseUint(strings.TrimPrefix(fields[3], "$"), 10, 64); err == nil {
				_, _ = s.runner.Run(ctx, "set-option", "-t", fields[3], "status-interval", "1")
			}
		}
	}
}

func (s *WorkStatus) installFormats(ctx context.Context, window string) {
	// Only this suffix belongs to OAX. Keep all user style, index/name/flag
	// choices and separate current/non-current formats verbatim before it.
	command := "'" + strings.ReplaceAll(s.binary, "'", "'\\''") + "' tmux-spinner"
	suffix := "#{?#{==:#{@oax_state},running},#(" + command + "),}"
	for _, option := range []string{"window-status-format", "window-status-current-format"} {
		out, err := s.runner.Run(ctx, "show-options", "-w", "-A", "-v", "-t", window, option)
		if err != nil {
			continue
		}
		original := strings.TrimSuffix(out, "\n")
		if strings.HasSuffix(original, suffix) {
			continue
		}
		// Remember the last suffix, so binary relocation replaces only our own
		// decoration and never stacks it or restores over newer user edits.
		marker := "@oax_" + option + "_suffix"
		old, _ := s.runner.Run(ctx, "show-options", "-w", "-v", "-t", window, marker)
		old = strings.TrimSuffix(old, "\n")
		if old != "" {
			original = strings.TrimSuffix(original, old)
		}
		if _, err := s.runner.Run(ctx, "set-option", "-w", "-t", window, option, original+suffix); err == nil {
			_, _ = s.runner.Run(ctx, "set-option", "-w", "-t", window, marker, suffix)
		}
	}
}
