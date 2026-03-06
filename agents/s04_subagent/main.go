// s04 follows the original tutorial strictly:
// parent agent = base tools + task, child agent = base tools only.
// The todo tool is intentionally not registered in this session.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
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

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "no .env file found, using system env")
	}

	client, err := newClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	model := getModel()
	cwd, _ := os.Getwd()
	parentSystem := fmt.Sprintf(
		"You are a coding agent at %s. Use the task tool to delegate exploration or subtasks.",
		cwd,
	)
	childSystem := fmt.Sprintf(
		"You are a coding subagent at %s. Complete the given task, then summarize your findings.",
		cwd,
	)

	childRegistry := tools.New()
	registerBaseTools(childRegistry)

	parentRegistry := tools.New()
	registerBaseTools(parentRegistry)
	parentRegistry.Register(
		tools.TaskToolDef(),
		tools.NewTaskHandler(func(ctx context.Context, prompt string, description string) (string, error) {
			desc := strings.TrimSpace(description)
			if desc == "" {
				desc = "subtask"
			}
			fmt.Printf("> task (%s): %s\n", desc, preview(prompt, 80))
			return loop.RunSubagent(ctx, client, model, childSystem, prompt, childRegistry)
		}),
	)

	rec := devtools.NewRecorderFromEnv()
	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(parentSystem),
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("%ss04 >> %s", colorCyan, colorReset)
		if !scanner.Scan() {
			break
		}

		query := strings.TrimSpace(scanner.Text())
		if query == "" || query == "q" || query == "exit" {
			break
		}

		history = append(history, openai.UserMessage(query))
		ctx := devtools.WithRecorder(context.Background(), rec)
		history, err = loop.Run(ctx, client, model, history, parentRegistry)
		if err != nil {
			fmt.Fprintln(os.Stderr, "loop error:", err)
			continue
		}

		printAssistantReply(history[len(history)-1])
		fmt.Println()
	}
}

func registerBaseTools(registry *tools.Registry) {
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.EditFileToolDef(), tools.EditFileHandler)
	registry.Register(tools.ListDirToolDef(), tools.ListDirHandler)
}

func preview(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
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
