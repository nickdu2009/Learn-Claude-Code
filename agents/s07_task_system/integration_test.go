package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/tasks"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

//go:embed testdata/task_graph_flow.md
var fixtureFS embed.FS

func TestIntegration_TaskGraphLifecycle(t *testing.T) {
	prompt := readFixture(t, "testdata/task_graph_flow.md")
	sandboxDir := sandboxS07Dir(t, "fake")
	tasksDir := filepath.Join(sandboxDir, ".tasks")

	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{ID: "call-create-1", Name: "task_create", Arguments: mustJSON(t, map[string]any{"subject": "Parse"})},
				{ID: "call-create-2", Name: "task_create", Arguments: mustJSON(t, map[string]any{"subject": "Transform"})},
				{ID: "call-create-3", Name: "task_create", Arguments: mustJSON(t, map[string]any{"subject": "Emit"})},
				{ID: "call-create-4", Name: "task_create", Arguments: mustJSON(t, map[string]any{"subject": "Test"})},
			}),
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{ID: "call-update-2", Name: "task_update", Arguments: mustJSON(t, map[string]any{"task_id": 2, "add_blocked_by": []int{1}})},
				{ID: "call-update-3", Name: "task_update", Arguments: mustJSON(t, map[string]any{"task_id": 3, "add_blocked_by": []int{1}})},
				{ID: "call-update-4", Name: "task_update", Arguments: mustJSON(t, map[string]any{"task_id": 4, "add_blocked_by": []int{2, 3}})},
			}),
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{ID: "call-complete-1", Name: "task_update", Arguments: mustJSON(t, map[string]any{"task_id": 1, "status": "completed"})},
				{ID: "call-get-2", Name: "task_get", Arguments: mustJSON(t, map[string]any{"task_id": 2})},
				{ID: "call-list", Name: "task_list", Arguments: mustJSON(t, map[string]any{})},
			}),
			makeHTTPStopResponse("Parse is completed. Transform and Emit are ready now, while Test is still blocked."),
		},
	}

	history := runS07FakeScenario(t, prompt, tasksDir, mock)

	toolNames := extractToolNames(history)
	for _, required := range []string{"task_create", "task_update", "task_get", "task_list"} {
		if !containsTool(toolNames, required) {
			t.Fatalf("expected tool %q in history, got %v", required, toolNames)
		}
	}

	repo, err := tasks.NewFileRepository(tasksDir)
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}
	taskService := tasks.NewService(repo)
	taskList, err := taskService.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(taskList) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(taskList))
	}

	bySubject := make(map[string]tasks.Task, len(taskList))
	for _, task := range taskList {
		bySubject[task.Subject] = task
	}

	if bySubject["Parse"].Status != tasks.StatusCompleted {
		t.Fatalf("Parse status = %q, want completed", bySubject["Parse"].Status)
	}
	if !bySubject["Transform"].IsReady() {
		t.Fatalf("Transform should be ready, got %+v", bySubject["Transform"])
	}
	if !bySubject["Emit"].IsReady() {
		t.Fatalf("Emit should be ready, got %+v", bySubject["Emit"])
	}
	if !bySubject["Test"].IsBlocked() {
		t.Fatalf("Test should still be blocked, got %+v", bySubject["Test"])
	}

	finalReply := extractFinalReply(history)
	if !strings.Contains(finalReply, "Transform") || !strings.Contains(finalReply, "Emit") {
		t.Fatalf("final reply should mention ready tasks, got %q", finalReply)
	}
}

func runS07FakeScenario(
	t *testing.T,
	prompt string,
	tasksDir string,
	mock *capturingMockHTTPClient,
) []openai.ChatCompletionMessageParamUnion {
	t.Helper()

	client := newCapturingMockClient(mock)
	repo, err := tasks.NewFileRepository(tasksDir)
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}
	taskService := tasks.NewService(repo)

	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(tools.TaskCreateToolDef(), tools.NewTaskCreateHandler(taskService))
	registry.Register(tools.TaskUpdateToolDef(), tools.NewTaskUpdateHandler(taskService))
	registry.Register(tools.TaskListToolDef(), tools.NewTaskListHandler(taskService))
	registry.Register(tools.TaskGetToolDef(), tools.NewTaskGetHandler(taskService))

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	system := fmt.Sprintf(
		"You are a coding agent at %s.\n"+
			"For multi-step work, create and maintain a persistent task graph using task_create, task_update, task_list, and task_get.\n"+
			"Use dependencies explicitly. Mark tasks in_progress before starting and completed when done.\n"+
			"Prefer tools over prose.",
		cwd,
	)

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}

	history, err = loop.Run(context.Background(), client, "mock-model", history, registry)
	if err != nil {
		t.Fatalf("agent loop error: %v", err)
	}
	return history
}

type toolCallSpec struct {
	ID        string
	Name      string
	Arguments string
}

type capturingMockHTTPClient struct {
	responses     []*http.Response
	callCount     int
	requestBodies [][]byte
}

func (m *capturingMockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if req != nil && req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		m.requestBodies = append(m.requestBodies, body)
		_ = req.Body.Close()
	}
	index := m.callCount
	m.callCount++
	if index < len(m.responses) {
		return m.responses[index], nil
	}
	return makeHTTPStopResponse("(default stop)"), nil
}

func newCapturingMockClient(mock *capturingMockHTTPClient) *openai.Client {
	client := openai.NewClient(
		option.WithAPIKey("mock-key"),
		option.WithBaseURL("https://mock.example.com/v1/"),
		option.WithHTTPClient(mock),
		option.WithMaxRetries(0),
	)
	return &client
}

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

func makeHTTPMultiToolCallResponse(calls []toolCallSpec) *http.Response {
	toolCalls := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		toolCalls = append(toolCalls, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}

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
					"role":       "assistant",
					"content":    "",
					"refusal":    "",
					"tool_calls": toolCalls,
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

func readFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := fixtureFS.ReadFile(name)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

func mustJSON(t *testing.T, value map[string]any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(data)
}

func sandboxS07Dir(t *testing.T, kind string) string {
	t.Helper()

	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s07", kind, t.Name(), runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create sandbox dir %s: %v", dir, err)
	}
	return dir
}

func extractToolNames(messages []openai.ChatCompletionMessageParamUnion) []string {
	seen := make(map[string]bool)
	var names []string
	for _, msg := range messages {
		if msg.OfAssistant == nil {
			continue
		}
		for _, tc := range msg.OfAssistant.ToolCalls {
			if !seen[tc.Function.Name] {
				seen[tc.Function.Name] = true
				names = append(names, tc.Function.Name)
			}
		}
	}
	return names
}

func containsTool(names []string, target string) bool {
	for _, name := range names {
		if name == target {
			return true
		}
	}
	return false
}

func extractFinalReply(messages []openai.ChatCompletionMessageParamUnion) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.OfAssistant == nil {
			continue
		}
		if msg.OfAssistant.Content.OfString.Value != "" {
			return msg.OfAssistant.Content.OfString.Value
		}
		for _, part := range msg.OfAssistant.Content.OfArrayOfContentParts {
			if part.OfText != nil && part.OfText.Text != "" {
				return part.OfText.Text
			}
		}
	}
	return ""
}
