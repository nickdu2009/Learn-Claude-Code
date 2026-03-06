package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
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

func TestRunSubagentWithCaller_WritesCompletedChildRun(t *testing.T) {
	t.Setenv("AI_SDK_DEVTOOLS_STREAM", "")

	ctx, tracePath, childRunID, parentStepID := newChildRunContext(t)
	registry := tools.New()
	registry.Register(tools.BashToolDef(), func(_ context.Context, args map[string]any) (string, error) {
		return "tool-output", nil
	})

	callCount := 0
	summary, err := runSubagentWithCaller(
		ctx,
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

	trace := readLoopTraceFile(t, tracePath)
	child := findTraceRun(t, trace.Runs, childRunID)
	if child.Status != "completed" {
		t.Fatalf("child status = %q, want completed", child.Status)
	}
	if derefStringPtr(child.CompletionReason) != "normal" {
		t.Fatalf("completion_reason = %q, want normal", derefStringPtr(child.CompletionReason))
	}
	if derefStringPtr(child.Summary) != "pytest" {
		t.Fatalf("summary = %q, want pytest", derefStringPtr(child.Summary))
	}
	parentStep := findTraceStep(t, trace.Steps, parentStepID)
	if len(parentStep.LinkedChildRunIDs) != 1 || parentStep.LinkedChildRunIDs[0] != childRunID {
		t.Fatalf("parent step linked child runs = %v", parentStep.LinkedChildRunIDs)
	}
}

func TestRunSubagentWithCaller_WritesSafetyLimitCompletion(t *testing.T) {
	t.Setenv("AI_SDK_DEVTOOLS_STREAM", "")

	ctx, tracePath, childRunID, _ := newChildRunContext(t)
	registry := tools.New()
	registry.Register(tools.BashToolDef(), func(_ context.Context, args map[string]any) (string, error) {
		return "tool-output", nil
	})

	summary, err := runSubagentWithCaller(
		ctx,
		nil,
		"mock-model",
		"system",
		"keep exploring forever",
		registry,
		2,
		func(_ context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
			return makeToolCallCompletion("call-loop", "bash", `{"command":"echo loop"}`), nil
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(summary, "safety limit (2 rounds)") {
		t.Fatalf("expected safety limit summary, got %q", summary)
	}

	trace := readLoopTraceFile(t, tracePath)
	child := findTraceRun(t, trace.Runs, childRunID)
	if child.Status != "completed" {
		t.Fatalf("child status = %q, want completed", child.Status)
	}
	if derefStringPtr(child.CompletionReason) != "safety-limit" {
		t.Fatalf("completion_reason = %q, want safety-limit", derefStringPtr(child.CompletionReason))
	}
	if !strings.Contains(derefStringPtr(child.Summary), "safety limit") {
		t.Fatalf("unexpected summary: %q", derefStringPtr(child.Summary))
	}
}

func TestRunSubagentWithCaller_WritesErrorChildRun(t *testing.T) {
	t.Setenv("AI_SDK_DEVTOOLS_STREAM", "")

	ctx, tracePath, childRunID, _ := newChildRunContext(t)
	registry := tools.New()
	registry.Register(tools.BashToolDef(), func(_ context.Context, args map[string]any) (string, error) {
		return "tool-output", nil
	})

	_, err := runSubagentWithCaller(
		ctx,
		nil,
		"mock-model",
		"system",
		"trigger an API failure",
		registry,
		2,
		func(_ context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
			return nil, fmt.Errorf("boom")
		},
	)
	if err == nil {
		t.Fatal("expected subagent error")
	}

	trace := readLoopTraceFile(t, tracePath)
	child := findTraceRun(t, trace.Runs, childRunID)
	if child.Status != "error" {
		t.Fatalf("child status = %q, want error", child.Status)
	}
	if derefStringPtr(child.CompletionReason) != "error" {
		t.Fatalf("completion_reason = %q, want error", derefStringPtr(child.CompletionReason))
	}
	if !strings.Contains(derefStringPtr(child.Error), "boom") {
		t.Fatalf("unexpected run error: %q", derefStringPtr(child.Error))
	}
	if !strings.Contains(derefStringPtr(child.Summary), "boom") {
		t.Fatalf("unexpected run summary: %q", derefStringPtr(child.Summary))
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

type traceFile struct {
	Version int            `json:"version"`
	Runs    []devtools.Run `json:"runs"`
	Steps   []devtools.Step `json:"steps"`
}

func newChildRunContext(t *testing.T) (context.Context, string, string, string) {
	t.Helper()

	traceDir := t.TempDir()
	t.Setenv("AI_SDK_DEVTOOLS", "1")
	t.Setenv("AI_SDK_DEVTOOLS_DIR", traceDir)

	parentRecorder := devtools.NewRecorderFromEnv()
	ctx := context.Background()
	if err := parentRecorder.BeginRun(ctx, devtools.RunMeta{
		Kind:  "main",
		Title: "parent",
	}); err != nil {
		t.Fatalf("begin parent run: %v", err)
	}

	parentStepID, parentStart := parentRecorder.StartStep(
		ctx,
		"generate",
		"mock-model",
		"dashscope",
		[]openai.ChatCompletionMessageParamUnion{openai.UserMessage("delegate this task")},
		nil,
		nil,
	)
	parentRecorder.FinishStep(ctx, parentStepID, parentStart, nil, nil, nil, nil, nil, nil)

	childRecorder, err := parentRecorder.SpawnChild(ctx, parentStepID, devtools.ChildRunMeta{
		Kind:         "subagent",
		Title:        "child",
		InputPreview: "inspect project",
	})
	if err != nil {
		t.Fatalf("spawn child run: %v", err)
	}

	tracePath := filepath.Join(traceDir, "generations.json")
	return devtools.WithRecorder(ctx, childRecorder), tracePath, childRecorder.RunID(), parentStepID
}

func readLoopTraceFile(t *testing.T, path string) traceFile {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace file: %v", err)
	}
	var trace traceFile
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("unmarshal trace file: %v", err)
	}
	return trace
}

func findTraceRun(t *testing.T, runs []devtools.Run, id string) devtools.Run {
	t.Helper()
	for _, run := range runs {
		if run.ID == id {
			return run
		}
	}
	t.Fatalf("run %q not found", id)
	return devtools.Run{}
}

func findTraceStep(t *testing.T, steps []devtools.Step, id string) devtools.Step {
	t.Helper()
	for _, step := range steps {
		if step.ID == id {
			return step
		}
	}
	t.Fatalf("step %q not found", id)
	return devtools.Step{}
}

func derefStringPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
