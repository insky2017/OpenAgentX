package console

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestFinalConsoleGrammar(t *testing.T) {
	accepted := [][]string{
		{}, {"login"}, {"logout"}, {"attach"}, {"attach", "--agent", "quote"}, {"attach", "--diagnostic"},
		{"--socket", "/tmp/oax.sock"}, {"login", "--credentials", "/tmp/credentials.json"},
	}
	for _, args := range accepted {
		if _, err := parseInvocation(args, io.Discard); err != nil {
			t.Fatalf("accepted args %v: %v", args, err)
		}
	}
	for _, args := range [][]string{
		{"status"}, {"dispatch"}, {"steer"}, {"cancel"}, {"approve"}, {"reject"}, {"down"}, {"force-stop"},
		{"attach", "--once"}, {"attach", "--username", "owner"}, {"attach", "--organization", "org"},
		{"login", "--password", "value"}, {"logout", "extra"},
	} {
		if _, err := parseInvocation(args, io.Discard); err == nil {
			t.Fatalf("legacy/invalid args accepted: %v", args)
		}
	}
}

func TestNonInteractiveMenuLoginAndAttachFailBeforeSideEffects(t *testing.T) {
	for _, args := range [][]string{{}, {"login"}, {"attach"}, {"attach", "--agent", "quote"}} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var out, errOut bytes.Buffer
			clientCalls, storeCalls, programCalls := 0, 0, 0
			code := Execute(args, Dependencies{Out: &out, Err: &errOut, IsInteractive: func() bool { return false },
				NewClient:          func(string) (Client, error) { clientCalls++; return nil, errors.New("unexpected") },
				NewCredentialStore: func(string) (CredentialStore, error) { storeCalls++; return nil, errors.New("unexpected") },
				RunProgram: func(context.Context, tea.Model, io.Reader, io.Writer) (tea.Model, error) {
					programCalls++
					return nil, errors.New("unexpected")
				}})
			if code != 2 || clientCalls != 0 || storeCalls != 0 || programCalls != 0 || !strings.Contains(errOut.String(), "Observe API") {
				t.Fatalf("args=%v code=%d client=%d store=%d program=%d stderr=%q", args, code, clientCalls, storeCalls, programCalls, errOut.String())
			}
		})
	}
}

func TestExecuteReturnsFailureForFatalDirectAttachAfterTerminalCleanup(t *testing.T) {
	var out, errOut bytes.Buffer
	store := &fakeCredentialStore{loadErr: errors.New("unused")}
	programCalls := 0
	code := Execute([]string{"attach", "--socket", "/tmp/oax.sock", "--credentials", "/tmp/credentials.json", "--agent", "quote"},
		Dependencies{Out: &out, Err: &errOut, IsInteractive: func() bool { return true },
			NewCredentialStore: func(string) (CredentialStore, error) { return store, nil },
			RunProgram: func(_ context.Context, model tea.Model, _ io.Reader, _ io.Writer) (tea.Model, error) {
				programCalls++
				m := model.(tuiModel)
				m.fatal = true
				m.notice = "Attach preparation failed: operation failed"
				return m, nil
			}})
	if code != 1 || programCalls != 1 || !strings.Contains(errOut.String(), "Attach preparation failed") {
		t.Fatalf("code=%d program=%d stderr=%q", code, programCalls, errOut.String())
	}
}

func TestExecuteTreatsInterruptedProgramAsCleanDetach(t *testing.T) {
	store := &fakeCredentialStore{}
	code := Execute([]string{"--socket", "/tmp/oax.sock", "--credentials", "/tmp/credentials.json"}, Dependencies{
		Out: io.Discard, Err: io.Discard, IsInteractive: func() bool { return true },
		NewCredentialStore: func(string) (CredentialStore, error) { return store, nil },
		RunProgram: func(context.Context, tea.Model, io.Reader, io.Writer) (tea.Model, error) {
			return nil, tea.ErrInterrupted
		},
	})
	if code != 0 {
		t.Fatalf("interrupted TUI code=%d", code)
	}
}
