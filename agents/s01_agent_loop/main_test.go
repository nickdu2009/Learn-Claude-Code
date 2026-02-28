package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/openai/openai-go"
)

// ─────────────────────────────────────────────
// Mock LLM Client
// ─────────────────────────────────────────────

// mockLLMClient 按顺序返回预设的响应，用于隔离 LLM 调用。
type mockLLMClient struct {
	responses []*openai.ChatCompletion
	calls     int // 记录被调用次数
}

func (m *mockLLMClient) Complete(_ context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	if m.calls >= len(m.responses) {
		return nil, fmt.Errorf("mockLLMClient: no more responses (call #%d)", m.calls)
	}
	resp := m.responses[m.calls]
	m.calls++
	return resp, nil
}

// errorLLMClient 始终返回错误，用于测试错误处理路径。
type errorLLMClient struct{}

func (e *errorLLMClient) Complete(_ context.Context, _ openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	return nil, fmt.Errorf("simulated API error")
}

// ─────────────────────────────────────────────
// 构造辅助函数
// ─────────────────────────────────────────────

// makeTextResponse 构造一个纯文本（无工具调用）的 ChatCompletion 响应。
func makeTextResponse(text string) *openai.ChatCompletion {
	return &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "stop",
				Message: openai.ChatCompletionMessage{
					Role:    "assistant",
					Content: text,
				},
			},
		},
	}
}

// makeToolCallResponse 构造一个包含工具调用的 ChatCompletion 响应。
func makeToolCallResponse(toolCallID, command string) *openai.ChatCompletion {
	return &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "tool_calls",
				Message: openai.ChatCompletionMessage{
					Role: "assistant",
					ToolCalls: []openai.ChatCompletionMessageToolCall{
						{
							ID:   toolCallID,
							Type: "function",
							Function: openai.ChatCompletionMessageToolCallFunction{
								Name:      "bash",
								Arguments: fmt.Sprintf(`{"command":%q}`, command),
							},
						},
					},
				},
			},
		},
	}
}

// ─────────────────────────────────────────────
// runBash 测试
// ─────────────────────────────────────────────

func TestRunBash_SimpleCommand(t *testing.T) {
	out := runBash("echo hello")
	if out != "hello" {
		t.Errorf("expected 'hello', got %q", out)
	}
}

func TestRunBash_MultiLineOutput(t *testing.T) {
	out := runBash("printf 'a\nb\nc'")
	if !strings.Contains(out, "a") || !strings.Contains(out, "b") || !strings.Contains(out, "c") {
		t.Errorf("expected multi-line output, got %q", out)
	}
}

func TestRunBash_NoOutput(t *testing.T) {
	out := runBash("true")
	if out != "(no output)" {
		t.Errorf("expected '(no output)', got %q", out)
	}
}

func TestRunBash_CommandNotFound(t *testing.T) {
	out := runBash("nonexistent_command_xyz_12345")
	// 兼容中英文系统："not found" / "未找到命令" / "No such file"
	hasError := strings.Contains(out, "not found") ||
		strings.Contains(out, "未找到") ||
		strings.Contains(out, "No such file") ||
		strings.Contains(out, "command not found") ||
		strings.Contains(out, "Error")
	if !hasError {
		t.Errorf("expected error message for unknown command, got %q", out)
	}
}

func TestRunBash_StderrCaptured(t *testing.T) {
	out := runBash("echo error_msg >&2")
	if !strings.Contains(out, "error_msg") {
		t.Errorf("expected stderr to be captured, got %q", out)
	}
}

func TestRunBash_OutputTruncation(t *testing.T) {
	// 生成超过 50000 字节的输出
	out := runBash("python3 -c \"print('x' * 60000)\"")
	if len(out) > 50000 {
		t.Errorf("output should be truncated to 50000 chars, got %d", len(out))
	}
}

// ─────────────────────────────────────────────
// runBash 危险命令拦截测试
// ─────────────────────────────────────────────

func TestRunBash_BlocksRmRfRoot(t *testing.T) {
	out := runBash("rm -rf /")
	if out != "Error: Dangerous command blocked" {
		t.Errorf("expected dangerous command to be blocked, got %q", out)
	}
}

func TestRunBash_BlocksSudo(t *testing.T) {
	out := runBash("sudo ls")
	if out != "Error: Dangerous command blocked" {
		t.Errorf("expected sudo to be blocked, got %q", out)
	}
}

func TestRunBash_BlocksShutdown(t *testing.T) {
	out := runBash("shutdown now")
	if out != "Error: Dangerous command blocked" {
		t.Errorf("expected shutdown to be blocked, got %q", out)
	}
}

func TestRunBash_BlocksReboot(t *testing.T) {
	out := runBash("reboot")
	if out != "Error: Dangerous command blocked" {
		t.Errorf("expected reboot to be blocked, got %q", out)
	}
}

func TestRunBash_BlocksDevNull(t *testing.T) {
	out := runBash("echo foo > /dev/sda")
	if out != "Error: Dangerous command blocked" {
		t.Errorf("expected /dev/ redirect to be blocked, got %q", out)
	}
}

func TestRunBash_SafeCommandNotBlocked(t *testing.T) {
	out := runBash("echo safe")
	if out == "Error: Dangerous command blocked" {
		t.Error("safe command should not be blocked")
	}
}

// ─────────────────────────────────────────────
// agentLoop 测试
// ─────────────────────────────────────────────

func TestAgentLoop_DirectTextResponse(t *testing.T) {
	// LLM 直接返回文本，不调用工具，循环应立即结束
	llm := &mockLLMClient{
		responses: []*openai.ChatCompletion{
			makeTextResponse("Hello, I am your assistant."),
		},
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("say hello"),
	}

	result := agentLoop(llm, "system prompt", messages)

	if llm.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", llm.calls)
	}
	// 原始 user 消息 + assistant 回复
	if len(result) != 2 {
		t.Errorf("expected 2 messages, got %d", len(result))
	}
	if result[1].OfAssistant == nil {
		t.Error("last message should be assistant")
	}
}

func TestAgentLoop_OneToolCallThenText(t *testing.T) {
	// 第一轮：LLM 调用 bash 工具；第二轮：LLM 返回文本
	llm := &mockLLMClient{
		responses: []*openai.ChatCompletion{
			makeToolCallResponse("call-1", "echo hello"),
			makeTextResponse("The output is: hello"),
		},
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("run echo hello"),
	}

	result := agentLoop(llm, "system prompt", messages)

	if llm.calls != 2 {
		t.Errorf("expected 2 LLM calls, got %d", llm.calls)
	}
	// user + assistant(tool_call) + tool_result + assistant(text)
	if len(result) != 4 {
		t.Errorf("expected 4 messages, got %d", len(result))
	}
}

func TestAgentLoop_MultipleToolCallsThenText(t *testing.T) {
	// 连续两轮工具调用，最后返回文本
	llm := &mockLLMClient{
		responses: []*openai.ChatCompletion{
			makeToolCallResponse("call-1", "echo step1"),
			makeToolCallResponse("call-2", "echo step2"),
			makeTextResponse("All done."),
		},
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("do two steps"),
	}

	result := agentLoop(llm, "system prompt", messages)

	if llm.calls != 3 {
		t.Errorf("expected 3 LLM calls, got %d", llm.calls)
	}
	// user + asst(tc1) + tool_result + asst(tc2) + tool_result + asst(text)
	if len(result) != 6 {
		t.Errorf("expected 6 messages, got %d", len(result))
	}
}

func TestAgentLoop_ToolCallWithDangerousCommand(t *testing.T) {
	// 工具调用包含危险命令，runBash 应返回 blocked 信息，循环继续正常运行
	llm := &mockLLMClient{
		responses: []*openai.ChatCompletion{
			makeToolCallResponse("call-danger", "sudo rm -rf /"),
			makeTextResponse("Blocked as expected."),
		},
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("try dangerous command"),
	}

	result := agentLoop(llm, "system prompt", messages)

	if llm.calls != 2 {
		t.Errorf("expected 2 LLM calls, got %d", llm.calls)
	}
	// 确认 tool_result 消息包含 blocked 信息
	toolResult := result[2] // user + asst(tc) + tool_result
	if toolResult.OfTool == nil {
		t.Fatal("expected tool result message")
	}
	if !strings.Contains(toolResult.OfTool.Content.OfString.Value, "Dangerous command blocked") {
		t.Errorf("expected blocked message in tool result, got %q",
			toolResult.OfTool.Content.OfString.Value)
	}
}

func TestAgentLoop_ToolCallWithInvalidJSON(t *testing.T) {
	// 工具调用参数 JSON 格式错误，应追加错误消息并继续
	badResp := &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{
			{
				FinishReason: "tool_calls",
				Message: openai.ChatCompletionMessage{
					Role: "assistant",
					ToolCalls: []openai.ChatCompletionMessageToolCall{
						{
							ID:   "bad-call",
							Type: "function",
							Function: openai.ChatCompletionMessageToolCallFunction{
								Name:      "bash",
								Arguments: `{invalid json`,
							},
						},
					},
				},
			},
		},
	}

	llm := &mockLLMClient{
		responses: []*openai.ChatCompletion{
			badResp,
			makeTextResponse("Handled error."),
		},
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("trigger bad json"),
	}

	result := agentLoop(llm, "system prompt", messages)

	if llm.calls != 2 {
		t.Errorf("expected 2 LLM calls, got %d", llm.calls)
	}
	// 确认 tool_result 包含 error 信息
	toolResult := result[2]
	if toolResult.OfTool == nil {
		t.Fatal("expected tool result message")
	}
	if !strings.Contains(toolResult.OfTool.Content.OfString.Value, "error") {
		t.Errorf("expected error in tool result, got %q",
			toolResult.OfTool.Content.OfString.Value)
	}
}

func TestAgentLoop_APIError_ReturnsOriginalMessages(t *testing.T) {
	// API 调用失败时，应返回原始消息列表，不崩溃
	llm := &errorLLMClient{}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("trigger error"),
	}

	result := agentLoop(llm, "system prompt", messages)

	// 出错时返回原始消息，不追加任何内容
	if len(result) != 1 {
		t.Errorf("expected original 1 message on error, got %d", len(result))
	}
}

func TestAgentLoop_SystemPromptPrepended(t *testing.T) {
	// 验证 system prompt 被正确前置到每次 LLM 调用的消息列表中
	var capturedMessages []openai.ChatCompletionMessageParamUnion

	captureLLM := &capturingLLMClient{
		capture: &capturedMessages,
		response: makeTextResponse("ok"),
	}

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hello"),
	}

	agentLoop(captureLLM, "test-system-prompt", messages)

	if len(capturedMessages) == 0 {
		t.Fatal("no messages captured")
	}
	first := capturedMessages[0]
	if first.OfSystem == nil {
		t.Fatal("first message should be system message")
	}
	if first.OfSystem.Content.OfString.Value != "test-system-prompt" {
		t.Errorf("expected system prompt 'test-system-prompt', got %q",
			first.OfSystem.Content.OfString.Value)
	}
}

// capturingLLMClient 捕获传入的消息列表，用于验证 system prompt 注入。
type capturingLLMClient struct {
	capture  *[]openai.ChatCompletionMessageParamUnion
	response *openai.ChatCompletion
}

func (c *capturingLLMClient) Complete(_ context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	*c.capture = params.Messages
	return c.response, nil
}

// ─────────────────────────────────────────────
// getModel / newClient 测试
// ─────────────────────────────────────────────

func TestGetModel_DefaultValue(t *testing.T) {
	os.Unsetenv("DASHSCOPE_MODEL")
	if m := getModel(); m != "qwen-plus" {
		t.Errorf("expected default 'qwen-plus', got %q", m)
	}
}

func TestGetModel_FromEnv(t *testing.T) {
	os.Setenv("DASHSCOPE_MODEL", "qwen-max")
	defer os.Unsetenv("DASHSCOPE_MODEL")
	if m := getModel(); m != "qwen-max" {
		t.Errorf("expected 'qwen-max', got %q", m)
	}
}

func TestNewClient_MissingAPIKey(t *testing.T) {
	os.Unsetenv("DASHSCOPE_API_KEY")
	os.Setenv("DASHSCOPE_BASE_URL", "https://example.com")
	defer os.Unsetenv("DASHSCOPE_BASE_URL")

	_, err := newClient()
	if err == nil {
		t.Error("expected error when DASHSCOPE_API_KEY is missing")
	}
	if !strings.Contains(err.Error(), "DASHSCOPE_API_KEY") {
		t.Errorf("error should mention DASHSCOPE_API_KEY, got: %v", err)
	}
}

func TestNewClient_MissingBaseURL(t *testing.T) {
	os.Setenv("DASHSCOPE_API_KEY", "sk-test")
	os.Unsetenv("DASHSCOPE_BASE_URL")
	defer os.Unsetenv("DASHSCOPE_API_KEY")

	_, err := newClient()
	if err == nil {
		t.Error("expected error when DASHSCOPE_BASE_URL is missing")
	}
	if !strings.Contains(err.Error(), "DASHSCOPE_BASE_URL") {
		t.Errorf("error should mention DASHSCOPE_BASE_URL, got: %v", err)
	}
}

func TestNewClient_Success(t *testing.T) {
	os.Setenv("DASHSCOPE_API_KEY", "sk-test-key")
	os.Setenv("DASHSCOPE_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1")
	defer func() {
		os.Unsetenv("DASHSCOPE_API_KEY")
		os.Unsetenv("DASHSCOPE_BASE_URL")
	}()

	client, err := newClient()
	if err != nil {
		t.Errorf("expected no error, got: %v", err)
	}
	if client == nil {
		t.Error("expected non-nil client")
	}
}
