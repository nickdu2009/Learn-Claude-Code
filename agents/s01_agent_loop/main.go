// s01: The Agent Loop
// Motto: "One loop & Bash is all you need"
//
// 最小 Agent：一个工具（bash）+ 一个循环。
// 演示核心 Agent 模式：调用 LLM → 检测 tool_calls → 执行工具 → 追加结果 → 循环。
package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/qwen"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using system env")
	}

	client, err := qwen.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	registry := tools.New()
	registry.Register(bashToolDef(), bashHandler)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("You are a helpful assistant. Use the bash tool to execute shell commands when needed."),
		openai.UserMessage("What is the current directory and list its files?"),
	}

	messages, err = loop.Run(context.Background(), client, qwen.Model(), messages, registry)
	if err != nil {
		log.Fatal(err)
	}

	// 打印最终回复
	last := messages[len(messages)-1]
	if last.OfAssistant != nil {
		content := last.OfAssistant.Content
		// 纯文本内容
		if content.OfString.Value != "" {
			fmt.Println(content.OfString.Value)
		}
		// 多部分内容
		for _, part := range content.OfArrayOfContentParts {
			if part.OfText != nil {
				fmt.Println(part.OfText.Text)
			}
		}
	}
}

func bashToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "bash",
			Description: openai.String("Execute a bash command and return its output."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The bash command to execute.",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

func bashHandler(args map[string]any) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("command must be a string")
	}

	out, err := exec.Command("bash", "-c", command).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("exit error: %s\n%s", err, out), nil
	}

	result := string(out)
	if len(result) > 4000 {
		result = result[:4000] + "\n...(truncated)"
	}
	return result, nil
}
