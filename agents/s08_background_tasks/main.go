package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/background"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
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

type backgroundToolService interface {
	Run(ctx context.Context, command string) (background.Task, error)
	Check(taskID string) (background.Task, error)
	List() ([]background.Task, error)
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

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	backgroundManager, err := background.NewManager(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	registry := newS08Registry(backgroundManager)

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	rec := devtools.NewRecorderFromEnv()
	_ = rec.BeginRun(context.Background(), devtools.RunMeta{
		Kind:  "main",
		Title: "s08 background tasks agent",
	})
	runResult := devtools.RunResult{
		Status:           "completed",
		CompletionReason: "normal",
	}
	defer func() {
		_ = rec.FinishRun(context.Background(), runResult)
	}()

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(buildS08SystemPrompt(cwd)),
	}
	inputCh := make(chan inputEvent)
	go readInputLoop(os.Stdin, inputCh)

	runner := loop.RunWithBackgroundNotifications(backgroundManager)

	for {
		fmt.Printf("%ss08 >> %s", colorCyan, colorReset)

		select {
		case <-signalCtx.Done():
			runResult.CompletionReason = "signal"
			fmt.Fprintln(os.Stderr, "\nreceived shutdown signal, exiting safely...")
			return
		case <-backgroundManager.Wakeups():
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

func buildS08SystemPrompt(cwd string) string {
	return "You are a coding agent at " + cwd + ".\n" +
		"Prefer tools over prose.\n" +
		"For work that may take time, prefer the background task tools and keep making progress instead of waiting.\n" +
		"Treat background task updates as new information and use tool results to decide when to inspect a task, review all running tasks, or respond to the user.\n" +
		"Avoid unnecessary status checks when there is no new signal."
}

func newS08Registry(backgroundService backgroundToolService) *tools.Registry {
	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(tools.BackgroundRunToolDef(), tools.NewBackgroundRunHandler(backgroundService))
	registry.Register(tools.CheckBackgroundToolDef(), tools.NewCheckBackgroundHandler(backgroundService))
	return registry
}

func registerBaseTools(registry *tools.Registry) {
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.EditFileToolDef(), tools.EditFileHandler)
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
