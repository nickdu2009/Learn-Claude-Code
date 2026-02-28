// Package loop implements the core agent loop.
package loop

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

// Run executes the agent loop until the model stops requesting tool calls.
// messages is the conversation history (modified in place).
func Run(
	ctx context.Context,
	client *openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
	registry *tools.Registry,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	for {
		resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    shared.ChatModel(model),
			Messages: messages,
			Tools:    registry.Definitions(),
		})
		if err != nil {
			return messages, fmt.Errorf("API call failed: %w", err)
		}

		choice := resp.Choices[0]
		messages = append(messages, choice.Message.ToParam())

		// 没有工具调用时，模型返回最终文本，循环结束
		if choice.FinishReason != "tool_calls" {
			return messages, nil
		}

		// 执行所有工具调用，收集结果
		for _, tc := range choice.Message.ToolCalls {
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return messages, fmt.Errorf("failed to parse tool args for %s: %w", tc.Function.Name, err)
			}

			output, err := registry.Dispatch(tc.Function.Name, args)
			if err != nil {
				output = fmt.Sprintf("error: %s", err.Error())
			}

			messages = append(messages, openai.ToolMessage(output, tc.ID))
		}
	}
}
