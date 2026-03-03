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
