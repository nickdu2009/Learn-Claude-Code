// s03: TodoWrite
// Motto: "An agent without a plan drifts"
//
// TODO: 引入 TodoManager，Agent 在执行前先调用 todo 列出步骤，
// 每完成一步更新状态，演示计划驱动的执行模式。
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// ANSI 颜色码
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
	system := fmt.Sprintf(
		"You are a coding agent at %s.\n"+
			"Use the todo tool to plan multi-step tasks. Mark in_progress before starting, completed when done.\n"+
			"Prefer tools over prose.",
		cwd,
	)

	todoManager := NewTodoManager()

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.ListDirToolDef(), tools.ListDirHandler)
	registry.Register(tools.EditFileToolDef(), tools.EditFileHandler)
	registry.Register(tools.TodoToolDef(), todoManager.HandleTodo)

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("%ss03 >> %s", colorCyan, colorReset)
		if !scanner.Scan() {
			break
		}
		query := strings.TrimSpace(scanner.Text())
		if query == "" || query == "q" || query == "exit" {
			break
		}

		history = append(history, openai.UserMessage(query))

		history, err = loop.RunWithTodoNag(
			context.Background(),
			client,
			model,
			history,
			registry,
		)
		if err != nil {
			fmt.Fprintln(os.Stderr, "loop error:", err)
			continue
		}

		// 打印最终回复
		last := history[len(history)-1]
		if last.OfAssistant != nil {
			content := last.OfAssistant.Content
			if content.OfString.Value != "" {
				fmt.Println(content.OfString.Value)
			}
			for _, part := range content.OfArrayOfContentParts {
				if part.OfText != nil {
					fmt.Println(part.OfText.Text)
				}
			}
		}
		fmt.Println()
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
	c := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	)
	return &c, nil
}

func getModel() string {
	if m := os.Getenv("DASHSCOPE_MODEL"); m != "" {
		return m
	}
	return "qwen-plus"
}
