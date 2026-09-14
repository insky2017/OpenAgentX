package console

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	openapi "openagentx/internal/api"
	consoleapi "openagentx/internal/api/console"
	consoleclient "openagentx/internal/client/console"
	"openagentx/internal/consolemodel"
	"openagentx/internal/domain"
)

const (
	replPrompt            = "OAX> "
	foregroundUnavailable = "Foreground Takeover（规划中，暂不可用）"
)

type attachUpdate struct {
	attached *consoleapi.AttachResponse
	event    *openapi.JournalEventReadModel
}

type inputResult struct {
	line string
	err  error
}

func runInteractiveAttach(ctx context.Context, cancel context.CancelFunc, client Client, agentID, organizationID, mode string, deps Dependencies) error {
	updates := make(chan attachUpdate)
	followDone := make(chan error, 1)
	go func() {
		followDone <- client.Follow(ctx, agentID, mode,
			func(attached consoleapi.AttachResponse) error {
				select {
				case updates <- attachUpdate{attached: &attached}:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			},
			func(event openapi.JournalEventReadModel) error {
				select {
				case updates <- attachUpdate{event: &event}:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			})
	}()

	var reducer *consolemodel.Reducer
	for reducer == nil {
		select {
		case update := <-updates:
			if update.attached != nil {
				var err error
				reducer, err = consolemodel.New(*update.attached)
				if err != nil {
					cancel()
					return err
				}
				if err := writeJSON(deps.Out, reducer.Snapshot()); err != nil {
					cancel()
					return err
				}
			}
		case err := <-followDone:
			if err == nil && ctx.Err() != nil {
				return nil
			}
			return err
		case <-ctx.Done():
			return nil
		}
	}

	inputs := make(chan inputResult, 1)
	go scanInput(ctx, deps.In, inputs)
	fmt.Fprint(deps.Out, replPrompt)
	for {
		select {
		case update := <-updates:
			if update.attached != nil {
				if err := reducer.ApplySnapshot(*update.attached); err != nil {
					cancel()
					return err
				}
				fmt.Fprintln(deps.Out)
				if err := writeJSON(deps.Out, reducer.Snapshot()); err != nil {
					cancel()
					return err
				}
			} else if update.event != nil {
				result, err := reducer.Apply(*update.event)
				if err != nil {
					cancel()
					return err
				}
				if result.Timeline == nil {
					continue
				}
				fmt.Fprintln(deps.Out)
				if err := writeJSON(deps.Out, *result.Timeline); err != nil {
					cancel()
					return err
				}
			}
			fmt.Fprint(deps.Out, replPrompt)
		case input := <-inputs:
			if input.err != nil {
				cancel()
				if input.err == io.EOF {
					return nil
				}
				return fmt.Errorf("read Console command: %w", input.err)
			}
			result, plain, quit, err := executeREPLCommand(ctx, client, reducer.Snapshot(), agentID, organizationID, input.line)
			if err != nil {
				fmt.Fprintf(deps.Err, "Console command failed: %v\n", err)
			} else if plain != "" {
				fmt.Fprintln(deps.Out, plain)
			} else if result != nil {
				if err := writeJSON(deps.Out, result); err != nil {
					cancel()
					return err
				}
			}
			if quit {
				cancel()
				return nil
			}
			fmt.Fprint(deps.Out, replPrompt)
		case err := <-followDone:
			if err == nil && ctx.Err() != nil {
				return nil
			}
			return err
		case <-ctx.Done():
			return nil
		}
	}
}

func scanInput(ctx context.Context, reader io.Reader, results chan<- inputResult) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		select {
		case results <- inputResult{line: scanner.Text()}:
		case <-ctx.Done():
			return
		}
	}
	result := inputResult{err: io.EOF}
	if err := scanner.Err(); err != nil {
		result.err = err
	}
	select {
	case results <- result:
	case <-ctx.Done():
	}
}

func executeREPLCommand(ctx context.Context, client Client, attached consoleapi.AttachResponse, agentID, organizationID, line string) (result any, plain string, quit bool, err error) {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil, "", false, nil
	}
	command, remainder, _ := strings.Cut(line, " ")
	switch command {
	case "/quit", "/exit":
		return nil, "", true, nil
	case "/help":
		return nil, "/dispatch <content> | /steer <task> <version> <content> | /cancel <task> <version> | /approve <approval> <version> | /reject <approval> <version> | /down | /foreground | /quit", false, nil
	case "/foreground":
		return nil, foregroundUnavailable, false, nil
	case "/dispatch":
		content := strings.TrimSpace(remainder)
		if content == "" {
			return nil, "", false, fmt.Errorf("usage: /dispatch <content>")
		}
		response, callErr := client.Dispatch(ctx, openapi.CreateTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: consoleclient.IdempotencyKey("console-dispatch")}, TargetAgentID: agentID, OrganizationID: organizationID, DispatchMode: domain.DispatchModeDirect, Content: content})
		return response, "", false, callErr
	case "/steer":
		values := strings.SplitN(strings.TrimSpace(remainder), " ", 3)
		if len(values) != 3 || strings.TrimSpace(values[2]) == "" {
			return nil, "", false, fmt.Errorf("usage: /steer <task> <version> <content>")
		}
		version, parseErr := positiveVersion(values[1])
		if parseErr != nil {
			return nil, "", false, parseErr
		}
		response, callErr := client.Steer(ctx, values[0], openapi.CreateMessageRequest{Meta: openapi.CommandMeta{IdempotencyKey: consoleclient.IdempotencyKey("console-steer"), ExpectedVersion: version}, Content: strings.TrimSpace(values[2])})
		return response, "", false, callErr
	case "/cancel":
		values := strings.Fields(remainder)
		if len(values) != 2 {
			return nil, "", false, fmt.Errorf("usage: /cancel <task> <version>")
		}
		version, parseErr := positiveVersion(values[1])
		if parseErr != nil {
			return nil, "", false, parseErr
		}
		response, callErr := client.Cancel(ctx, values[0], openapi.CancelTaskRequest{Meta: openapi.CommandMeta{IdempotencyKey: consoleclient.IdempotencyKey("console-cancel"), ExpectedVersion: version}})
		return response, "", false, callErr
	case "/approve", "/reject":
		values := strings.Fields(remainder)
		if len(values) != 2 {
			return nil, "", false, fmt.Errorf("usage: %s <approval> <version>", command)
		}
		version, parseErr := positiveVersion(values[1])
		if parseErr != nil {
			return nil, "", false, parseErr
		}
		decision := domain.ApprovalDecisionApprove
		if command == "/reject" {
			decision = domain.ApprovalDecisionReject
		}
		response, callErr := client.DecideApproval(ctx, values[0], openapi.DecideApprovalRequest{Meta: openapi.CommandMeta{IdempotencyKey: consoleclient.IdempotencyKey("console-approval"), ExpectedVersion: version}, Decision: decision})
		return response, "", false, callErr
	case "/down":
		if attached.WorkerInstanceID == "" || attached.WorkerStatus == domain.WorkerStatusOffline {
			return attached, "", false, nil
		}
		response, callErr := client.WorkerCommand(ctx, attached.WorkerInstanceID, attached.Generation, domain.WorkerCommandStop, consoleclient.IdempotencyKey("console-down"), false)
		return response, "", false, callErr
	default:
		return nil, "", false, fmt.Errorf("unknown command %q; use /help", command)
	}
}

func positiveVersion(value string) (int64, error) {
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("version must be a positive integer")
	}
	return version, nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
