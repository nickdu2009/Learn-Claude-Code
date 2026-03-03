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
	"github.com/openai/openai-go/packages/ssestream"
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
//
// When the environment variable AI_SDK_DEVTOOLS_STREAM=1 is set, each LLM call is made via the
// streaming API so that raw_chunks are captured in the DevTools trace.
func RunWithRecorder(
	ctx context.Context,
	client *openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
	registry *tools.Registry,
	rec *devtools.RunRecorder,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	provider := inferProviderFromEnv()
	useStream := isStreamingEnabled()

	for {
		params := openai.ChatCompletionNewParams{
			Model:    shared.ChatModel(model),
			Messages: messages,
			Tools:    registry.Definitions(),
		}

		providerOpts := map[string]any{"baseURL": os.Getenv("DASHSCOPE_BASE_URL")}

		stepType := "generate"
		if useStream {
			stepType = "stream"
		}

		stepID, start := "", time.Time{}
		if rec != nil {
			stepID, start = rec.StartStep(ctx, stepType, model, provider, messages, registry.Definitions(), providerOpts, params)
		}

		var (
			choice    openai.ChatCompletionChoice
			resp      *openai.ChatCompletion
			rawChunks any
			callErr   error
		)

		if useStream {
			choice, resp, rawChunks, callErr = runStreaming(ctx, client, params)
		} else {
			resp, callErr = client.Chat.Completions.New(ctx, params)
			if callErr == nil {
				choice = resp.Choices[0]
			}
		}

		if callErr != nil {
			if rec != nil {
				rec.FinishStep(ctx, stepID, start, nil, nil, fmt.Errorf("API call failed: %w", callErr), params, nil, nil)
			}
			return messages, fmt.Errorf("API call failed: %w", callErr)
		}

		messages = append(messages, choice.Message.ToParam())

		if rec != nil {
			output := buildViewerOutput(choice.FinishReason, choice.Message)
			usage := buildViewerUsage(resp)
			rec.FinishStep(ctx, stepID, start, output, usage, nil, params, resp, rawChunks)
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

// isStreamingEnabled returns true when AI_SDK_DEVTOOLS_STREAM env var is truthy.
func isStreamingEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("AI_SDK_DEVTOOLS_STREAM")))
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// runStreaming executes a single LLM call via the SSE streaming API.
// It accumulates all chunks, reconstructs a synthetic ChatCompletion, and
// returns the raw chunks slice for DevTools recording.
func runStreaming(
	ctx context.Context,
	client *openai.Client,
	params openai.ChatCompletionNewParams,
) (choice openai.ChatCompletionChoice, resp *openai.ChatCompletion, rawChunks any, err error) {
	stream := client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	var (
		chunks       []openai.ChatCompletionChunk
		textBuf      strings.Builder
		finishReason string
		toolCallMap  = map[int]*openai.ChatCompletionMessageToolCall{} // index → accumulated tool call
		modelID      string
		id           string
	)

	for stream.Next() {
		chunk := stream.Current()
		chunks = append(chunks, chunk)

		if modelID == "" {
			modelID = chunk.Model
		}
		if id == "" {
			id = chunk.ID
		}

		for _, c := range chunk.Choices {
			if string(c.FinishReason) != "" {
				finishReason = string(c.FinishReason)
			}
			textBuf.WriteString(c.Delta.Content)

			// Accumulate tool call deltas.
			for _, tcDelta := range c.Delta.ToolCalls {
				idx := int(tcDelta.Index)
				if _, exists := toolCallMap[idx]; !exists {
					toolCallMap[idx] = &openai.ChatCompletionMessageToolCall{
						ID:   tcDelta.ID,
						Type: "function",
					}
				}
				tc := toolCallMap[idx]
				if tcDelta.ID != "" {
					tc.ID = tcDelta.ID
				}
				tc.Function.Name += tcDelta.Function.Name
				tc.Function.Arguments += tcDelta.Function.Arguments
			}
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		err = streamErr
		return
	}

	// Build tool calls slice in order.
	toolCalls := make([]openai.ChatCompletionMessageToolCall, len(toolCallMap))
	for i, tc := range toolCallMap {
		if i < len(toolCalls) {
			toolCalls[i] = *tc
		}
	}

	msg := openai.ChatCompletionMessage{
		Role:      "assistant",
		Content:   textBuf.String(),
		ToolCalls: toolCalls,
	}
	choice = openai.ChatCompletionChoice{
		Message:      msg,
		FinishReason: finishReason,
	}

	// Construct a synthetic ChatCompletion for usage recording (usage is typically
	// in the last chunk; we leave it zero here since streaming usage varies by provider).
	resp = &openai.ChatCompletion{
		ID:      id,
		Model:   modelID,
		Choices: []openai.ChatCompletionChoice{choice},
	}

	rawChunks = chunks
	return
}

// streamChunkToRaw is a type alias used only to satisfy the ssestream import.
var _ = ssestream.Stream[openai.ChatCompletionChunk]{}

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

	// Extract reasoning/thinking content from provider-specific fields.
	// Some providers (e.g. DeepSeek, QwQ) return reasoning_content alongside content.
	// We probe the raw JSON to find these fields.
	if reasoning := extractReasoningFromMessage(msg); reasoning != "" {
		parts = append(parts, map[string]any{
			"type":     "reasoning",
			"text":     reasoning,
			"thinking": reasoning,
		})
	}

	if strings.TrimSpace(msg.Content) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": msg.Content,
		})
	}

	toolCalls := make([]map[string]any, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		// Parse arguments JSON string into object so viewer renders it correctly.
		var parsedArgs any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &parsedArgs); err != nil {
				parsedArgs = tc.Function.Arguments
			}
		}
		call := map[string]any{
			"type":       "tool-call",
			"toolName":   tc.Function.Name,
			"toolCallId": tc.ID,
			"args":       parsedArgs,
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

// extractReasoningFromMessage probes the raw JSON of a ChatCompletionMessage for
// provider-specific reasoning/thinking fields (e.g. reasoning_content from DeepSeek).
func extractReasoningFromMessage(msg openai.ChatCompletionMessage) string {
	raw := msg.RawJSON()
	if raw == "" {
		return ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return ""
	}
	return devtools.ParseReasoningFromRawMessage(m)
}
