package loop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"context"

	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// ─────────────────────────────────────────────────────────────────────────────
// Mock HTTP Client 基础设施
// ─────────────────────────────────────────────────────────────────────────────

// mockHTTPClient 按顺序返回预设的 HTTP 响应，用于拦截 openai.Client 的 API 请求。
type mockHTTPClient struct {
	responses []*http.Response
	callCount int
}

func (m *mockHTTPClient) Do(_ *http.Request) (*http.Response, error) {
	i := m.callCount
	m.callCount++
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	// 防止越界：返回一个默认的 stop 响应
	return makeHTTPStopResponse("(default stop)"), nil
}

// capturingMockHTTPClient records request bodies for assertions.
type capturingMockHTTPClient struct {
	responses     []*http.Response
	callCount     int
	requestBodies [][]byte
}

func (m *capturingMockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if req != nil && req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		m.requestBodies = append(m.requestBodies, b)
		_ = req.Body.Close()
	}
	i := m.callCount
	m.callCount++
	if i < len(m.responses) {
		return m.responses[i], nil
	}
	return makeHTTPStopResponse("(default stop)"), nil
}

// makeHTTPStopResponse 构造一个 FinishReason=stop 的 HTTP 响应体。
func makeHTTPStopResponse(content string) *http.Response {
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
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}
	return marshalToHTTPResponse(raw)
}

// makeHTTPToolCallResponse 构造一个 FinishReason=tool_calls 的 HTTP 响应体。
func makeHTTPToolCallResponse(toolCallID, funcName, arguments string) *http.Response {
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
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}
	return marshalToHTTPResponse(raw)
}

func marshalToHTTPResponse(body map[string]any) *http.Response {
	data, err := json.Marshal(body)
	if err != nil {
		panic("marshalToHTTPResponse: " + err.Error())
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

// newMockClient 创建一个注入了 mockHTTPClient 的 openai.Client。
func newMockClient(mock *mockHTTPClient) *openai.Client {
	c := openai.NewClient(
		option.WithAPIKey("mock-key"),
		option.WithBaseURL("https://mock.example.com/v1/"),
		option.WithHTTPClient(mock),
		option.WithMaxRetries(0),
	)
	return &c
}

func newCapturingMockClient(mock *capturingMockHTTPClient) *openai.Client {
	c := openai.NewClient(
		option.WithAPIKey("mock-key"),
		option.WithBaseURL("https://mock.example.com/v1/"),
		option.WithHTTPClient(mock),
		option.WithMaxRetries(0),
	)
	return &c
}

// sandboxLoopDir 返回 loop 集成测试的隔离目录。
func sandboxLoopDir(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s02", "fake", t.Name(), runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create sandbox dir %s: %v", dir, err)
	}
	return dir
}

// ─────────────────────────────────────────────────────────────────────────────
// Agent Loop 集成测试
// ─────────────────────────────────────────────────────────────────────────────

// IT-LOOP-01: LLM 直接返回 stop，循环应在 1 轮后结束，历史记录追加 assistant 消息。
func TestLoop_Stop(t *testing.T) {
	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeHTTPStopResponse("Hello from mock LLM"),
		},
	}
	client := newMockClient(mock)

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("hi"),
	}

	result, err := Run(context.Background(), client, "mock-model", initial, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 初始 1 条 + assistant 回复 1 条 = 2 条
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	last := result[len(result)-1]
	if last.OfAssistant == nil {
		t.Fatal("last message should be assistant")
	}
	if mock.callCount != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.callCount)
	}
}

// IT-LOOP-02: Mock LLM 返回 write_file 的 ToolCall，验证 Dispatch 正确执行并将结果追加到历史记录。
func TestLoop_WriteFileTool(t *testing.T) {
	dir := sandboxLoopDir(t)
	targetFile := filepath.Join(dir, "test.txt")

	argsJSON, _ := json.Marshal(map[string]any{
		"path":    targetFile,
		"content": "hello from loop test",
	})

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeHTTPToolCallResponse("call-wf-1", "write_file", string(argsJSON)),
			makeHTTPStopResponse("File written successfully."),
		},
	}
	client := newMockClient(mock)

	registry := tools.New()
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("write a file"),
	}

	result, err := Run(context.Background(), client, "mock-model", initial, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 初始 1 + assistant(tool_calls) 1 + tool_result 1 + assistant(stop) 1 = 4
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	// 第 3 条消息应为 ToolMessage，内容包含写入成功的反馈
	toolMsg := result[2]
	if toolMsg.OfTool == nil {
		t.Fatal("third message should be a tool message")
	}
	toolContent := toolMsg.OfTool.Content.OfString.Value
	if !strings.Contains(toolContent, targetFile) {
		t.Errorf("tool result should mention the file path, got: %q", toolContent)
	}

	// 验证文件真实存在于磁盘上
	data, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("file should have been created at %s: %v", targetFile, err)
	}
	if string(data) != "hello from loop test" {
		t.Errorf("file content mismatch: got %q", string(data))
	}

	if mock.callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", mock.callCount)
	}
}

// IT-LOOP-03: Mock LLM 返回 list_dir 的 ToolCall，验证目录列表被正确返回并追加到历史记录。
func TestLoop_ListDirTool(t *testing.T) {
	dir := sandboxLoopDir(t)

	// 在沙箱目录下预先创建一个文件，以便 list_dir 有内容可返回
	if err := os.WriteFile(filepath.Join(dir, "dummy.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	argsJSON, _ := json.Marshal(map[string]any{"path": dir})

	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeHTTPToolCallResponse("call-ld-1", "list_dir", string(argsJSON)),
			makeHTTPStopResponse("Directory listed."),
		},
	}
	client := newMockClient(mock)

	registry := tools.New()
	registry.Register(tools.ListDirToolDef(), tools.ListDirHandler)

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("list the directory"),
	}

	result, err := Run(context.Background(), client, "mock-model", initial, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	toolMsg := result[2]
	if toolMsg.OfTool == nil {
		t.Fatal("third message should be a tool message")
	}
	toolContent := toolMsg.OfTool.Content.OfString.Value
	if !strings.Contains(toolContent, "dummy.txt") {
		t.Errorf("tool result should contain 'dummy.txt', got: %q", toolContent)
	}
}

// IT-LOOP-04: Mock LLM 返回未注册的工具名，循环不应 panic，应将错误信息喂回 LLM。
func TestLoop_UnknownTool(t *testing.T) {
	mock := &mockHTTPClient{
		responses: []*http.Response{
			makeHTTPToolCallResponse("call-unk-1", "magic_tool", `{}`),
			makeHTTPStopResponse("I understand the tool failed."),
		},
	}
	client := newMockClient(mock)

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("use magic tool"),
	}

	result, err := Run(context.Background(), client, "mock-model", initial, registry)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	toolMsg := result[2]
	if toolMsg.OfTool == nil {
		t.Fatal("third message should be a tool message")
	}
	toolContent := toolMsg.OfTool.Content.OfString.Value
	if !strings.Contains(toolContent, "error") {
		t.Errorf("tool result should contain 'error', got: %q", toolContent)
	}
}

// IT-LOOP-06: 调用 todo 后 roundsSinceTodo 重置为 0，之后不足 3 轮时不注入 nag。
//
// 场景序列：bash → bash → todo → bash → stop
// 预期：第 4 次请求（index=3，bash 后 roundsSinceTodo=1）的 messages 中不包含 nag。
func TestLoop_TodoNagResetAfterTodoCall(t *testing.T) {
	todoArgs, _ := json.Marshal(map[string]any{
		"items": []any{
			map[string]any{"id": "1", "text": "step one", "status": "in_progress"},
		},
	})

	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPToolCallResponse("call-1", "bash", `{"command":"echo 1"}`),
			makeHTTPToolCallResponse("call-2", "bash", `{"command":"echo 2"}`),
			makeHTTPToolCallResponse("call-3", "todo", string(todoArgs)),
			makeHTTPToolCallResponse("call-4", "bash", `{"command":"echo 4"}`),
			makeHTTPStopResponse("done"),
		},
	}
	client := newCapturingMockClient(mock)

	// 注册真实的 TodoManager，使 todo 工具调用能正常 dispatch。
	type todoHandlerFunc func(args map[string]any) (string, error)
	todoItems := []any{
		map[string]any{"id": "1", "text": "step one", "status": "in_progress"},
	}
	_ = todoItems

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)

	// 用闭包模拟 todo handler（不依赖 s03 包，保持 pkg/loop 独立）。
	todoCallCount := 0
	registry.Register(tools.TodoToolDef(), func(args map[string]any) (string, error) {
		todoCallCount++
		return "[ ] #1: step one\n(0/1 completed)", nil
	})

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("run steps"),
	}

	_, err := RunWithTodoNag(context.Background(), client, "mock-model", initial, registry, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.callCount < 5 {
		t.Fatalf("expected at least 5 LLM calls, got %d", mock.callCount)
	}
	if todoCallCount != 1 {
		t.Errorf("expected todo handler called once, got %d", todoCallCount)
	}

	// 第 4 次请求（index=3）：todo 调用后 roundsSinceTodo 重置为 0，再过 1 轮 bash 后为 1，
	// 未达到阈值 3，不应注入 nag。
	if len(mock.requestBodies) < 4 {
		t.Fatalf("expected at least 4 request bodies, got %d", len(mock.requestBodies))
	}
	var req map[string]any
	if err := json.Unmarshal(mock.requestBodies[3], &req); err != nil {
		t.Fatalf("failed to parse 4th request body: %v", err)
	}
	msgs, _ := req["messages"].([]any)
	for _, m := range msgs {
		obj, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := obj["role"].(string)
		content, _ := obj["content"].(string)
		if role == "user" && strings.Contains(content, "Update your todos.") {
			t.Fatalf("nag should NOT be injected in 4th request after todo reset, but found it in messages")
		}
	}
}

// IT-LOOP-05: 连续 N 轮未调用 todo 时，应注入 nag reminder（Update your todos.）。
func TestLoop_TodoNagReminderInjected(t *testing.T) {
	// 4 次 tool_calls（都不是 todo），第 5 次 stop 结束。
	// 预期：第 4 次请求（0-based index=3）开始，messages 中包含提醒 "Update your todos."
	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPToolCallResponse("call-1", "bash", `{"command":"echo 1"}`),
			makeHTTPToolCallResponse("call-2", "bash", `{"command":"echo 2"}`),
			makeHTTPToolCallResponse("call-3", "bash", `{"command":"echo 3"}`),
			makeHTTPToolCallResponse("call-4", "bash", `{"command":"echo 4"}`),
			makeHTTPStopResponse("done"),
		},
	}
	client := newCapturingMockClient(mock)

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	// todo 工具是否注册不影响注入逻辑（注入发生在模型调用前）。

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("run a few steps"),
	}

	_, err := RunWithTodoNag(context.Background(), client, "mock-model", initial, registry, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.callCount < 4 {
		t.Fatalf("expected at least 4 LLM calls, got %d", mock.callCount)
	}
	if len(mock.requestBodies) < 4 {
		t.Fatalf("expected at least 4 request bodies, got %d", len(mock.requestBodies))
	}

	var req map[string]any
	if err := json.Unmarshal(mock.requestBodies[3], &req); err != nil {
		t.Fatalf("failed to parse request body: %v", err)
	}
	msgs, _ := req["messages"].([]any)
	found := false
	for _, m := range msgs {
		obj, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := obj["role"].(string)
		content, _ := obj["content"].(string)
		if role == "user" && strings.Contains(content, "Update your todos.") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected nag reminder in 4th request messages, got: %s", string(mock.requestBodies[3]))
	}
}
