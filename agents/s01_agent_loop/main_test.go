package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/openai/openai-go"
)

// mockLLM 实现 LLMClient 接口，按顺序返回预设的响应或错误。
type mockLLM struct {
	responses []*openai.ChatCompletion
	errs      []error
	callCount int
}

func (m *mockLLM) Complete(_ context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	i := m.callCount
	m.callCount++
	if i < len(m.errs) && m.errs[i] != nil {
		return nil, m.errs[i]
	}
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	// 防止越界：默认返回 stop
	return makeStopResponse("(default stop)"), nil
}

// makeStopResponse 构造一个 FinishReason=stop 的 ChatCompletion。
func makeStopResponse(content string) *openai.ChatCompletion {
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

// makeToolCallResponse 构造一个 FinishReason=tool_calls 的 ChatCompletion。
func makeToolCallResponse(toolCallID, funcName, arguments string) *openai.ChatCompletion {
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

// unmarshalCompletion 将 map 序列化为 JSON 再反序列化为 ChatCompletion，
// 以确保 SDK 内部的 respjson 元数据字段被正确填充。
func unmarshalCompletion(raw map[string]any) *openai.ChatCompletion {
	data, err := json.Marshal(raw)
	if err != nil {
		panic("unmarshalCompletion marshal: " + err.Error())
	}
	var c openai.ChatCompletion
	if err := json.Unmarshal(data, &c); err != nil {
		panic("unmarshalCompletion unmarshal: " + err.Error())
	}
	return &c
}

// ─────────────────────────────────────────────────────────────────────────────
// agentLoop 测试
// ─────────────────────────────────────────────────────────────────────────────

// Case 1: LLM 直接返回文本（FinishReason=stop），循环应在 1 轮后退出。
func TestAgentLoop_Stop(t *testing.T) {
	mock := &mockLLM{
		responses: []*openai.ChatCompletion{
			makeStopResponse("Hello from LLM"),
		},
	}

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hi"),
	}
	result := agentLoop(mock, "system", initial, "", devtools.Noop())

	// 初始 1 条 + Assistant 回复 1 条 = 2 条
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	last := result[len(result)-1]
	if last.OfAssistant == nil {
		t.Fatal("last message should be assistant")
	}
	if last.OfAssistant.Content.OfString.Value != "Hello from LLM" {
		t.Errorf("unexpected content: %q", last.OfAssistant.Content.OfString.Value)
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.callCount)
	}
}

// Case 2: LLM 先返回合法的 tool_call，执行后再返回 stop。
// 验证 bash 命令被正确执行，结果被追加为 ToolMessage。
func TestAgentLoop_ToolCall(t *testing.T) {
	mock := &mockLLM{
		responses: []*openai.ChatCompletion{
			makeToolCallResponse("call-1", "bash", `{"command":"echo test_tool_output"}`),
			makeStopResponse("done"),
		},
	}

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("run echo"),
	}
	result := agentLoop(mock, "system", initial, "", devtools.Noop())

	// 初始 1 + assistant(tool_calls) 1 + tool_result 1 + assistant(stop) 1 = 4
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	// 第 3 条消息应为 ToolMessage，内容包含 echo 的输出
	toolMsg := result[2]
	if toolMsg.OfTool == nil {
		t.Fatal("third message should be a tool message")
	}
	content := toolMsg.OfTool.Content.OfString.Value
	if !strings.Contains(content, "test_tool_output") {
		t.Errorf("tool result should contain 'test_tool_output', got: %q", content)
	}
	if mock.callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", mock.callCount)
	}
}

// Case 4: LLM 返回的 tool_call 参数是非法 JSON，
// 验证系统不崩溃，且将错误信息作为 ToolMessage 喂回 LLM。
func TestAgentLoop_InvalidJSON(t *testing.T) {
	mock := &mockLLM{
		responses: []*openai.ChatCompletion{
			makeToolCallResponse("call-bad", "bash", `{"command": "ls"`), // 缺少闭合括号
			makeStopResponse("ok"),
		},
	}

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("do something"),
	}
	result := agentLoop(mock, "system", initial, "", devtools.Noop())

	// 初始 1 + assistant(tool_calls) 1 + tool_result(error) 1 + assistant(stop) 1 = 4
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	toolMsg := result[2]
	if toolMsg.OfTool == nil {
		t.Fatal("third message should be a tool message")
	}
	content := toolMsg.OfTool.Content.OfString.Value
	if !strings.Contains(content, "error") {
		t.Errorf("tool result should contain 'error', got: %q", content)
	}
}

// Case 5: LLM API 调用直接返回 error，
// 验证 agentLoop 安全中断并返回当前消息切片（不 panic）。
func TestAgentLoop_APIError(t *testing.T) {
	mock := &mockLLM{
		errs: []error{errors.New("network timeout")},
	}

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hello"),
	}
	result := agentLoop(mock, "system", initial, "", devtools.Noop())

	// API 出错时应立即返回原始消息，不追加任何内容
	if len(result) != len(initial) {
		t.Errorf("expected %d messages on error, got %d", len(initial), len(result))
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 LLM call attempt, got %d", mock.callCount)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runBashIn 测试
// ─────────────────────────────────────────────────────────────────────────────

// Case 7: 正常执行有输出的命令，返回标准输出内容。
func TestRunBashIn_Normal(t *testing.T) {
	result := runBashIn("echo 'hello world'", "")
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

// Case 9: 执行必然报错的命令，返回值应包含错误信息而不是 panic。
func TestRunBashIn_Error(t *testing.T) {
	result := runBashIn("ls /nonexistent_path_s01_test_12345", "")
	// 命令有 stderr 输出时，result 不为空；无输出时返回 "Error: ..."
	if result == "" {
		t.Error("expected non-empty error output")
	}
	// 不应返回正常内容
	if result == "(no output)" {
		// ls 报错通常有 stderr，但如果真的没有输出，也不应 panic
		t.Log("command produced no output, but did not panic — acceptable")
	}
}

// Case 10: 触发危险命令拦截，命令不应被执行。
func TestRunBashIn_Dangerous(t *testing.T) {
	result := runBashIn("rm -rf /", "")
	if result != "Error: Dangerous command blocked" {
		t.Errorf("expected dangerous command to be blocked, got %q", result)
	}
}
