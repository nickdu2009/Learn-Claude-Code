// Package loop implements the core agent loop.
package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
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
	return RunWithRecorder(ctx, client, model, messages, registry, devtools.NewRunRecorderFromEnv())
}

// RunWithRecorder executes the agent loop and records DevTools steps into the given recorder (optional).
// When recorder is reused across calls, multiple rounds will be grouped into the same DevTools run.
func RunWithRecorder(
	ctx context.Context,
	client *openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
	registry *tools.Registry,
	rec *devtools.RunRecorder,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	provider := inferProviderFromEnv()

	for {
		stepID, start := "", time.Time{}
		params := openai.ChatCompletionNewParams{
			Model:    shared.ChatModel(model),
			Messages: messages,
			Tools:    registry.Definitions(),
		}
		if rec != nil {
			stepID, start = rec.StartStep(ctx, "generate", model, provider, messages, registry.Definitions(), map[string]any{
				"baseURL": os.Getenv("DASHSCOPE_BASE_URL"),
			})
		}

		resp, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			if rec != nil {
				rec.FinishStep(ctx, stepID, start, nil, nil, fmt.Errorf("API call failed: %w", err), params, nil, nil)
			}
			return messages, fmt.Errorf("API call failed: %w", err)
		}

		choice := resp.Choices[0]
		messages = append(messages, choice.Message.ToParam())

		if rec != nil {
			output := buildViewerOutput(choice.FinishReason, choice.Message)
			usage := buildViewerUsage(resp)
			rec.FinishStep(ctx, stepID, start, output, usage, nil, params, resp, nil)
		}

		// 没有工具调用时，模型返回最终文本，循环结束
		if choice.FinishReason != "tool_calls" {
			return messages, nil
		}

		// 执行所有工具调用，收集结果
		for _, tc := range choice.Message.ToolCalls {
			if rec != nil {
				rec.RegisterToolCall(tc.ID, tc.Function.Name)
			}
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

func inferProviderFromEnv() string {
	baseURL := os.Getenv("DASHSCOPE_BASE_URL")
	if baseURL == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Host)
	switch {
	case strings.Contains(host, "dashscope"):
		return "dashscope"
	case strings.Contains(host, "aliyun"):
		return "aliyun"
	default:
		if host != "" {
			return host
		}
		return ""
	}
}

func buildViewerUsage(resp *openai.ChatCompletion) any {
	if resp == nil || resp.Usage.PromptTokens == 0 && resp.Usage.CompletionTokens == 0 && resp.Usage.TotalTokens == 0 {
		// Keep null if provider doesn't return usage.
		return nil
	}
	return map[string]any{
		"inputTokens":  resp.Usage.PromptTokens,
		"outputTokens": resp.Usage.CompletionTokens,
		"totalTokens":  resp.Usage.TotalTokens,
		"raw":          resp.Usage,
	}
}

func buildViewerOutput(finishReason string, msg openai.ChatCompletionMessage) any {
	fr := finishReason
	if fr == "tool_calls" {
		fr = "tool-calls" // viewer expects this string
	}

	parts := make([]map[string]any, 0, 4)
	if strings.TrimSpace(msg.Content) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": msg.Content,
		})
	}

	toolCalls := make([]map[string]any, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		call := map[string]any{
			"type":       "tool-call",
			"toolName":   tc.Function.Name,
			"toolCallId": tc.ID,
			"args":       tc.Function.Arguments,
		}
		toolCalls = append(toolCalls, call)
		parts = append(parts, call)
	}

	out := map[string]any{
		"finishReason": fr,
		"content":      parts,
	}
	if len(toolCalls) > 0 {
		out["toolCalls"] = toolCalls
	}
	return out
}
