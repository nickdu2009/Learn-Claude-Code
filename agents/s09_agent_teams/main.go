package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/team"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const (
	colorCyan  = "\033[36m"
	colorReset = "\033[0m"
)

type inputEvent struct {
	line string
	err  error
	eof  bool
}

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "no .env file found, using system env")
	}

	client, err := newClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	teamService, err := newTeamService(signalCtx, client, getModel(), cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := teamService.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintln(os.Stderr, "team shutdown error:", err)
		}
	}()

	registry := newS09Registry(teamService)

	rec := devtools.NewRecorderFromEnv()
	_ = rec.BeginRun(context.Background(), devtools.RunMeta{
		Kind:  "main",
		Title: "s09 agent teams agent",
	})
	runResult := devtools.RunResult{
		Status:           "completed",
		CompletionReason: "normal",
	}
	defer func() {
		_ = rec.FinishRun(context.Background(), runResult)
	}()

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(buildS09SystemPrompt(cwd)),
	}
	inputCh := make(chan inputEvent)
	go readInputLoop(os.Stdin, inputCh)

	runner := loop.RunWithTeamInboxNotifications("lead", teamService)

	for {
		fmt.Printf("%ss09 >> %s", colorCyan, colorReset)

		select {
		case <-signalCtx.Done():
			runResult.CompletionReason = "signal"
			fmt.Fprintln(os.Stderr, "\nreceived shutdown signal, exiting safely...")
			return
		case <-teamService.Wakeups("lead"):
			ctx := devtools.WithRecorder(signalCtx, rec)
			history, err = runner(ctx, client, getModel(), history, registry)
			if err != nil {
				if signalCtx.Err() != nil || errors.Is(err, context.Canceled) {
					runResult.CompletionReason = "signal"
					fmt.Fprintln(os.Stderr, "\nreceived shutdown signal, exiting safely...")
					return
				}
				fmt.Fprintln(os.Stderr, "loop error:", err)
				continue
			}
			printAssistantReply(history[len(history)-1])
			fmt.Println()
		case event, ok := <-inputCh:
			if !ok || event.eof {
				runResult.CompletionReason = "eof"
				return
			}
			if event.err != nil {
				runResult.Status = "failed"
				runResult.CompletionReason = "input-error"
				fmt.Fprintln(os.Stderr, "input error:", event.err)
				return
			}

			query := strings.TrimSpace(event.line)
			if query == "" || query == "q" || query == "exit" {
				runResult.CompletionReason = "user-exit"
				return
			}
			if query == "/team" {
				output, err := formatTeamList(teamService)
				if err != nil {
					fmt.Fprintln(os.Stderr, "team error:", err)
					continue
				}
				fmt.Println(output)
				continue
			}
			if query == "/inbox" {
				output, err := teamService.DrainInboxJSON("lead")
				if err != nil {
					fmt.Fprintln(os.Stderr, "inbox error:", err)
					continue
				}
				fmt.Println(output)
				continue
			}

			history = append(history, openai.UserMessage(query))
			ctx := devtools.WithRecorder(signalCtx, rec)
			history, err = runner(ctx, client, getModel(), history, registry)
			if err != nil {
				if signalCtx.Err() != nil || errors.Is(err, context.Canceled) {
					runResult.CompletionReason = "signal"
					fmt.Fprintln(os.Stderr, "\nreceived shutdown signal, exiting safely...")
					return
				}
				fmt.Fprintln(os.Stderr, "loop error:", err)
				continue
			}

			printAssistantReply(history[len(history)-1])
			fmt.Println()
		}
	}
}

func buildS09SystemPrompt(cwd string) string {
	return "You are a coding agent at " + cwd + ".\n" +
		"Prefer tools over prose.\n" +
		"For larger tasks, delegate focused work to persistent teammates instead of doing everything alone.\n" +
		"Treat inbox updates from teammates as new information and react to them when they arrive.\n" +
		"Keep teammates coordinated with clear messages and avoid unnecessary inbox checks when there is no new signal."
}

func newTeamService(baseCtx context.Context, client *openai.Client, model string, cwd string) (*team.Service, error) {
	teamDir := filepath.Join(cwd, ".team")
	repo, err := team.NewFileRepository(teamDir)
	if err != nil {
		return nil, err
	}
	mailbox, err := team.NewFileMailbox(filepath.Join(teamDir, "inbox"))
	if err != nil {
		return nil, err
	}
	service, err := team.NewService(baseCtx, client, model, repo, mailbox)
	if err != nil {
		return nil, err
	}
	service.SetFactory(team.RuntimeFactory{
		Workdir: cwd,
		RegistryBuilder: func(member team.Member) (*tools.Registry, error) {
			return newTeammateRegistry(service, member.Name), nil
		},
	})
	return service, nil
}

func newS09Registry(teamService *team.Service) *tools.Registry {
	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(
		tools.SpawnTeammateToolDef(),
		tools.NewSpawnTeammateHandler(func(ctx context.Context, name, role, prompt string) (string, error) {
			member, err := teamService.Spawn(ctx, name, role, prompt)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Spawned teammate %q (role: %s)", member.Name, member.Role), nil
		}),
	)
	registry.Register(
		tools.ListTeammatesToolDef(),
		tools.NewListTeammatesHandler(func() (string, error) {
			return formatTeamList(teamService)
		}),
	)
	registry.Register(
		tools.SendMessageToolDef(),
		tools.NewSendMessageHandler(func(_ context.Context, to, content, msgType string) (string, error) {
			if err := teamService.Send("lead", to, content, msgType); err != nil {
				return "", err
			}
			return fmt.Sprintf("Sent %s to %s", strings.TrimSpace(msgType), to), nil
		}),
	)
	registry.Register(
		tools.ReadInboxToolDef(),
		tools.NewReadInboxHandler(func(_ context.Context) (string, error) {
			return teamService.DrainInboxJSON("lead")
		}),
	)
	registry.Register(
		tools.BroadcastToolDef(),
		tools.NewBroadcastHandler(func(_ context.Context, content string) (string, error) {
			count, err := teamService.Broadcast("lead", content)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Broadcast to %d teammates", count), nil
		}),
	)
	return registry
}

func newTeammateRegistry(teamService *team.Service, sender string) *tools.Registry {
	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(
		tools.SendMessageToolDef(),
		tools.NewSendMessageHandler(func(_ context.Context, to, content, msgType string) (string, error) {
			if err := teamService.Send(sender, to, content, msgType); err != nil {
				return "", err
			}
			return fmt.Sprintf("Sent %s to %s", strings.TrimSpace(msgType), to), nil
		}),
	)
	registry.Register(
		tools.ReadInboxToolDef(),
		tools.NewReadInboxHandler(func(_ context.Context) (string, error) {
			return teamService.DrainInboxJSON(sender)
		}),
	)
	return registry
}

func registerBaseTools(registry *tools.Registry) {
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.EditFileToolDef(), tools.EditFileHandler)
}

func formatTeamList(teamService *team.Service) (string, error) {
	memberList, err := teamService.List()
	if err != nil {
		return "", err
	}
	if len(memberList) == 0 {
		return "No teammates.", nil
	}

	lines := []string{"Team: default"}
	for _, member := range memberList {
		lines = append(lines, fmt.Sprintf("- %s (%s): %s", member.Name, member.Role, member.Status))
	}
	return strings.Join(lines, "\n"), nil
}

func printAssistantReply(message openai.ChatCompletionMessageParamUnion) {
	if message.OfAssistant == nil {
		return
	}

	content := message.OfAssistant.Content
	if content.OfString.Value != "" {
		fmt.Println(content.OfString.Value)
	}
	for _, part := range content.OfArrayOfContentParts {
		if part.OfText != nil {
			fmt.Println(part.OfText.Text)
		}
	}
}

func readInputLoop(r io.Reader, out chan<- inputEvent) {
	defer close(out)

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		out <- inputEvent{line: scanner.Text()}
	}
	if err := scanner.Err(); err != nil {
		out <- inputEvent{err: err}
		return
	}
	out <- inputEvent{eof: true}
}

func newClient() (*openai.Client, error) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("DASHSCOPE_API_KEY is not set")
	}
	baseURL := os.Getenv("DASHSCOPE_BASE_URL")
	if baseURL == "" {
		return nil, fmt.Errorf("DASHSCOPE_BASE_URL is not set")
	}

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	)
	return &client, nil
}

func getModel() string {
	if m := os.Getenv("DASHSCOPE_MODEL"); m != "" {
		return m
	}
	return "qwen-plus"
}
