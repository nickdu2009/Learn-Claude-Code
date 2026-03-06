package loop

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

func TestRunSubagentWithCaller_ReturnsFinalSummary(t *testing.T) {
	t.Setenv("AI_SDK_DEVTOOLS_STREAM", "")

	registry := tools.New()
	registry.Register(tools.BashToolDef(), func(_ context.Context, args map[string]any) (string, error) {
		return "tool-output", nil
	})

	callCount := 0
	summary, err := runSubagentWithCaller(
		context.Background(),
		nil,
		"mock-model",
		"system",
		"find the testing framework",
		registry,
		5,
		func(_ context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
			callCount++
			if callCount == 1 {
				return makeToolCallCompletion("call-1", "bash", `{"command":"echo hello"}`), nil
			}
			return makeStopCompletion("pytest"), nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "pytest" {
		t.Fatalf("expected final summary, got %q", summary)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 model calls, got %d", callCount)
	}
}

func TestRunSubagentWithCaller_StopsAtSafetyLimit(t *testing.T) {
	t.Setenv("AI_SDK_DEVTOOLS_STREAM", "")

	registry := tools.New()
	registry.Register(tools.BashToolDef(), func(_ context.Context, args map[string]any) (string, error) {
		return "tool-output", nil
	})

	callCount := 0
	summary, err := runSubagentWithCaller(
		context.Background(),
		nil,
		"mock-model",
		"system",
		"keep exploring forever",
		registry,
		2,
		func(_ context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
			callCount++
			return makeToolCallCompletion("call-loop", "bash", `{"command":"echo loop"}`), nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(summary, "safety limit (2 rounds)") {
		t.Fatalf("expected safety limit summary, got %q", summary)
	}
	if callCount != 2 {
		t.Fatalf("expected 2 model calls, got %d", callCount)
	}
}

func makeStopCompletion(content string) *openai.ChatCompletion {
	raw := map[string]any{
		"id":      "mock-id",
		"object":  "chat.completion",
		"created": 0,
		"model":   "mock-model",
		"choices": []map[string]any{
			{
				"index":         0,
				"finish_reason": "stop",
				"logprobs":      nil,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
					"refusal": "",
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
	return unmarshalCompletion(raw)
}

func makeToolCallCompletion(toolCallID, funcName, arguments string) *openai.ChatCompletion {
	raw := map[string]any{
		"id":      "mock-id",
		"object":  "chat.completion",
		"created": 0,
		"model":   "mock-model",
		"choices": []map[string]any{
			{
				"index":         0,
				"finish_reason": "tool_calls",
				"logprobs":      nil,
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"refusal": "",
					"tool_calls": []map[string]any{
						{
							"id":   toolCallID,
							"type": "function",
							"function": map[string]any{
								"name":      funcName,
								"arguments": arguments,
							},
						},
					},
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"total_tokens":      0,
		},
	}
	return unmarshalCompletion(raw)
}

func unmarshalCompletion(raw map[string]any) *openai.ChatCompletion {
	data, err := json.Marshal(raw)
	if err != nil {
		panic("marshal completion: " + err.Error())
	}

	var completion openai.ChatCompletion
	if err := json.Unmarshal(data, &completion); err != nil {
		panic("unmarshal completion: " + err.Error())
	}
	return &completion
}
