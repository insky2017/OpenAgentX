package codex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type backgroundTerminal struct {
	ProcessID string `json:"processId"`
	ItemID    string `json:"itemId"`
	OSPID     *int   `json:"osPid"`
}

// This namespace owns model-created unified-exec PTYs. command/exec/terminate
// and process/kill instead own client-created commands and are not substitutes.
func (h *turnHandle) terminateToolProcesses() (returnErr error) {
	var reports []map[string]any
	defer func() {
		failure := ""
		if returnErr != nil {
			failure = returnErr.Error()
		}
		report, _ := json.MarshalIndent(map[string]any{"thread_id": h.threadID, "turn_id": h.turnID, "checked_at": time.Now().UTC(), "method": "thread/backgroundTerminals/terminate", "commands": reports, "stopped": returnErr == nil, "error": failure}, "", "  ")
		_ = os.WriteFile(filepath.Join(h.adapter.config.StateDir, "cancel-background-terminals-"+h.turnID+".json"), report, 0600)
	}()
	h.mu.Lock()
	pending := map[string]providerItem{}
	for id, item := range h.commands {
		if item.ExitCode == nil {
			pending[id] = item
		}
	}
	h.mu.Unlock()
	if len(pending) == 0 {
		return nil
	}
	if h.processes == nil {
		return errors.New("cannot independently verify tool processes on an external Codex host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for len(pending) > 0 {
		cursor := ""
		matched := false
		for {
			var list struct {
				Data       []backgroundTerminal `json:"data"`
				NextCursor *string              `json:"nextCursor"`
			}
			params := map[string]any{"threadId": h.threadID, "limit": 100}
			if cursor != "" {
				params["cursor"] = cursor
			}
			if err := h.client.Call(ctx, "thread/backgroundTerminals/list", params, &list); err != nil {
				return err
			}
			reports = append(reports, map[string]any{"list": list})
			for _, terminal := range list.Data {
				item, ok := pending[terminal.ItemID]
				if !ok {
					continue
				}
				if item.ProcessID != "" && item.ProcessID != terminal.ProcessID {
					return errors.New("Codex background process identity mismatch")
				}
				if terminal.OSPID == nil {
					return errors.New("Codex background terminal omitted OS PID")
				}
				ref, _, err := readIdentity(*terminal.OSPID)
				if err != nil {
					return err
				}
				h.processes.capture()
				h.processes.mu.Lock()
				owned := h.processes.known[ref.PID] == ref
				h.processes.mu.Unlock()
				if !owned {
					return errors.New("Codex terminal PID is not in this owned host tree")
				}
				tree := newProcessTracker(ref)
				tree.capture()
				tree.mu.Lock()
				var refs []processIdentity
				for _, child := range tree.known {
					refs = append(refs, child)
				}
				tree.mu.Unlock()
				var response struct {
					Terminated bool `json:"terminated"`
				}
				if err = h.client.Call(ctx, "thread/backgroundTerminals/terminate", map[string]any{"threadId": h.threadID, "processId": terminal.ProcessID}, &response); err != nil {
					return err
				}
				deadline := time.Now().Add(2 * time.Second)
				for {
					alive := false
					for _, child := range refs {
						alive = alive || identityAlive(child)
					}
					if !alive {
						break
					}
					if time.Now().After(deadline) {
						return fmt.Errorf("Codex background terminal %s remained alive", terminal.ProcessID)
					}
					time.Sleep(25 * time.Millisecond)
				}
				reports = append(reports, map[string]any{"item_id": terminal.ItemID, "provider_process_id": terminal.ProcessID, "terminated": response.Terminated, "os_processes": refs, "all_exited": true})
				delete(pending, terminal.ItemID)
				matched = true
			}
			if list.NextCursor == nil || *list.NextCursor == "" {
				break
			}
			if *list.NextCursor == cursor {
				return errors.New("Codex terminal list cursor did not advance")
			}
			cursor = *list.NextCursor
		}
		if len(pending) == 0 {
			break
		}
		if !matched {
			select {
			case <-ctx.Done():
				return errors.New("Codex pending tool was not located in background terminals")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	report, _ := json.MarshalIndent(map[string]any{"thread_id": h.threadID, "turn_id": h.turnID, "checked_at": time.Now().UTC(), "method": "thread/backgroundTerminals/terminate", "commands": reports, "stopped": true}, "", "  ")
	return os.WriteFile(filepath.Join(h.adapter.config.StateDir, "cancel-processes-"+h.turnID+".json"), report, 0600)
}
