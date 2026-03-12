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
	"slices"
	"strings"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/tasks"
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

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	repoRoot, err := findRepoRoot(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	repo, err := tasks.NewFileRepository(filepath.Join(repoRoot, ".tasks"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	taskService := tasks.NewService(repo)

	system := fmt.Sprintf(
		"You are a coding agent at %s.\n"+
			"For multi-step work, create and maintain a persistent task graph using task_create, task_update, task_list, and task_get.\n"+
			"Use dependencies explicitly. Mark tasks in_progress before starting and completed when done.\n"+
			"Prefer tools over prose.",
		cwd,
	)

	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(tools.TaskCreateToolDef(), tools.NewTaskCreateHandler(taskService))
	registry.Register(tools.TaskUpdateToolDef(), tools.NewTaskUpdateHandler(taskService))
	registry.Register(tools.TaskListToolDef(), tools.NewTaskListHandler(taskService))
	registry.Register(tools.TaskGetToolDef(), tools.NewTaskGetHandler(taskService))

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	rec := devtools.NewRecorderFromEnv()
	_ = rec.BeginRun(context.Background(), devtools.RunMeta{
		Kind:  "main",
		Title: "s07 task system agent",
	})
	runResult := devtools.RunResult{
		Status:           "completed",
		CompletionReason: "normal",
	}
	defer func() {
		_ = rec.FinishRun(context.Background(), runResult)
	}()

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
	}

	inputCh := make(chan inputEvent)
	go readInputLoop(os.Stdin, inputCh)

	for {
		fmt.Printf("%ss07 >> %s", colorCyan, colorReset)

		select {
		case <-signalCtx.Done():
			runResult.CompletionReason = "signal"
			fmt.Fprintln(os.Stderr, "\nreceived shutdown signal, exiting safely...")
			warnInProgressTasks(os.Stderr, taskService)
			return
		case event, ok := <-inputCh:
			if !ok || event.eof {
				runResult.CompletionReason = "eof"
				warnInProgressTasks(os.Stderr, taskService)
				return
			}
			if event.err != nil {
				runResult.Status = "failed"
				runResult.CompletionReason = "input-error"
				fmt.Fprintln(os.Stderr, "input error:", event.err)
				warnInProgressTasks(os.Stderr, taskService)
				return
			}

			query := strings.TrimSpace(event.line)
			if query == "" || query == "q" || query == "exit" {
				runResult.CompletionReason = "user-exit"
				warnInProgressTasks(os.Stderr, taskService)
				return
			}

			history = append(history, openai.UserMessage(query))
			ctx := devtools.WithRecorder(signalCtx, rec)
			history, err = loop.Run(ctx, client, getModel(), history, registry)
			if err != nil {
				if signalCtx.Err() != nil || errors.Is(err, context.Canceled) {
					runResult.CompletionReason = "signal"
					fmt.Fprintln(os.Stderr, "\nreceived shutdown signal, exiting safely...")
					warnInProgressTasks(os.Stderr, taskService)
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

func registerBaseTools(registry *tools.Registry) {
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.EditFileToolDef(), tools.EditFileHandler)
	registry.Register(tools.ListDirToolDef(), tools.ListDirHandler)
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

func warnInProgressTasks(w io.Writer, taskService interface {
	ListTasks() ([]tasks.Task, error)
}) {
	taskList, err := taskService.ListTasks()
	if err != nil {
		fmt.Fprintf(w, "warning: failed to inspect task board during shutdown: %v\n", err)
		return
	}

	warning := formatInProgressTaskWarning(taskList)
	if warning == "" {
		return
	}
	fmt.Fprintln(w, warning)
}

func formatInProgressTaskWarning(taskList []tasks.Task) string {
	inProgress := make([]tasks.Task, 0)
	for _, task := range taskList {
		if task.Status == tasks.StatusInProgress {
			inProgress = append(inProgress, task)
		}
	}
	if len(inProgress) == 0 {
		return ""
	}

	slices.SortFunc(inProgress, func(a, b tasks.Task) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})

	lines := []string{"warning: the following tasks are still in_progress:"}
	for _, task := range inProgress {
		lines = append(lines, fmt.Sprintf("- #%d %s", task.ID, task.Subject))
	}
	return strings.Join(lines, "\n")
}

func findRepoRoot(start string) (string, error) {
	dir := filepath.Clean(start)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("failed to locate repository root from %s", start)
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
