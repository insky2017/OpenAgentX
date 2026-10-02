package codex

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// These identities are operating-system PIDs plus Linux start times. Provider
// item.processId is a logical PTY handle and MUST NOT be passed to kill(2).
type processIdentity struct {
	PID       int    `json:"pid"`
	StartTime uint64 `json:"start_time"`
}

func readIdentity(pid int) (processIdentity, string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return processIdentity{}, "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return processIdentity{}, "", errors.New("malformed process stat")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 20 {
		return processIdentity{}, "", errors.New("incomplete process stat")
	}
	start, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return processIdentity{}, "", err
	}
	return processIdentity{PID: pid, StartTime: start}, fields[0], nil
}
func identityAlive(ref processIdentity) bool {
	actual, state, err := readIdentity(ref.PID)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	return actual == ref && state != "Z" && state != "X"
}
func signalIdentity(ref processIdentity, sig syscall.Signal) error {
	actual, _, err := readIdentity(ref.PID)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if actual != ref {
		return nil
	}
	if err = syscall.Kill(ref.PID, sig); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

type processTracker struct {
	mu    sync.Mutex
	root  processIdentity
	known map[int]processIdentity
	err   error
}

func newProcessTracker(root processIdentity) *processTracker {
	return &processTracker{root: root, known: map[int]processIdentity{root.PID: root}}
}

// Read every thread's children: a child launched from a Rust worker thread need
// not appear under /proc/PID/task/PID/children. Following known identities also
// retains descendants that later change process group or are reparented.
func (p *processTracker) capture() { p.mu.Lock(); defer p.mu.Unlock(); p.captureLocked() }
func (p *processTracker) captureLocked() {
	queue := make([]processIdentity, 0, len(p.known))
	for _, ref := range p.known {
		queue = append(queue, ref)
	}
	seen := map[int]bool{}
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		if seen[ref.PID] || !identityAlive(ref) {
			continue
		}
		seen[ref.PID] = true
		tasks, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", ref.PID))
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				p.err = errors.Join(p.err, err)
			}
			continue
		}
		for _, task := range tasks {
			data, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%s/children", ref.PID, task.Name()))
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					p.err = errors.Join(p.err, err)
				}
				continue
			}
			for _, value := range strings.Fields(string(data)) {
				pid, err := strconv.Atoi(value)
				if err != nil {
					continue
				}
				child, _, err := readIdentity(pid)
				if err != nil {
					continue
				}
				p.known[pid] = child
				queue = append(queue, child)
			}
		}
	}
}
func (p *processTracker) watch(done <-chan struct{}) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			p.capture()
		}
	}
}

func (p *processTracker) stop() (refs []processIdentity, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Freeze the owned host and the observed tree before killing it so the
	// leader cannot keep launching replacements while cancellation is checked.
	for pass := 0; pass < 3; pass++ {
		p.captureLocked()
		for _, ref := range p.known {
			err = errors.Join(err, signalIdentity(ref, syscall.SIGSTOP))
		}
	}
	for _, ref := range p.known {
		refs = append(refs, ref)
		if ref != p.root {
			err = errors.Join(err, signalIdentity(ref, syscall.SIGKILL))
		}
	}
	err = errors.Join(err, signalIdentity(p.root, syscall.SIGKILL), p.err)
	deadline := time.Now().Add(5 * time.Second)
	for {
		alive := false
		for _, ref := range refs {
			alive = alive || identityAlive(ref)
		}
		if !alive {
			return refs, err
		}
		if time.Now().After(deadline) {
			return refs, errors.Join(err, errors.New("owned Codex process tree did not stop"))
		}
		time.Sleep(25 * time.Millisecond)
	}
}
