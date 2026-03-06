// s02: Tool Use
// Motto: "Adding a tool means adding one handler"
//
// TODO: 在 s01 基础上扩展工具集（read_file、write_file、list_dir），
// 演示 dispatch map 模式：新增工具只需 Register 一次，循环本身不变。
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
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorReset  = "\033[0m"
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
	system := fmt.Sprintf("You are a coding agent at %s. Use tools to solve tasks. Act, don't explain.", cwd)

	// 初始化 Registry 并注册工具
	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.ListDirToolDef(), tools.ListDirHandler)

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("%ss02 >> %s", colorCyan, colorReset)
		if !scanner.Scan() {
			break
		}
		query := strings.TrimSpace(scanner.Text())
		if query == "" || query == "q" || query == "exit" {
			break
		}

		history = append(history, openai.UserMessage(query))

		// 调用核心循环
		var err error
		history, err = loop.Run(
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
