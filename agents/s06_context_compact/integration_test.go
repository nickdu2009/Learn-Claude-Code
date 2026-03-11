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
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

//go:embed testdata/manual_compact.md testdata/auto_compact_many_reads.md testdata/auto_compact_turns.md
var fixtureFS embed.FS

func TestIntegration_ManualCompactFixture(t *testing.T) {
	prompt := readFixture(t, "testdata/manual_compact.md")
	transcriptDir := sandboxS06FixtureDir(t, "manual")

	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{ID: "call-list", Name: "list_dir", Arguments: mustJSON(t, map[string]any{"path": "pkg/loop"})},
				{ID: "call-read-agent", Name: "read_file", Arguments: mustJSON(t, map[string]any{"path": "pkg/loop/agent.go"})},
				{ID: "call-read-todo", Name: "read_file", Arguments: mustJSON(t, map[string]any{"path": "pkg/loop/todo_nag.go"})},
			}),
			makeHTTPToolCallResponse("call-compact", "compact", mustJSON(t, map[string]any{
				"focus": "preserve the inspected files, the difference between Run and RunWithTodoNag, and the next step",
			})),
			makeHTTPStopResponse(strings.TrimSpace(`
Goal
Validate manual compact behavior for s06.
Completed
- Listed pkg/loop.
- Read pkg/loop/agent.go and pkg/loop/todo_nag.go.
- Triggered the compact tool.
CurrentState
- The conversation has been compressed and continuity must preserve inspected files and the key loop difference.
Decisions
- Preserve the difference between Run and RunWithTodoNag.
Constraints
- Keep the answer concise and continue from compressed context.
NextSteps
- Report the inspected files and the preserved difference.
`)),
			makeHTTPStopResponse("Inspected `pkg/loop/agent.go` and `pkg/loop/todo_nag.go`; `RunWithTodoNag` adds reminder injection when todo is not used for several rounds, and I am continuing from compressed context."),
		},
	}

	history, requests := runS06FixtureScenario(t, prompt, transcriptDir, mock)

	if len(requests) != 4 {
		t.Fatalf("expected 4 request bodies, got %d", len(requests))
	}
	if !requestContainsText(t, requests[0], "You are testing the `compact` tool in the s06 context-compact session.") {
		t.Fatalf("expected first request to include embedded manual fixture, got: %s", requests[0])
	}
	if !requestContainsText(t, requests[2], "CompactionTrigger: manual") {
		t.Fatalf("expected summary request to mark manual compaction, got: %s", requests[2])
	}
	if !requestContainsText(t, requests[2], "difference between Run and RunWithTodoNag") {
		t.Fatalf("expected summary request to preserve manual focus, got: %s", requests[2])
	}

	toolNames := extractToolNames(history)
	for _, required := range []string{"list_dir", "read_file", "compact"} {
		if !containsTool(toolNames, required) {
			t.Fatalf("expected tool %q in history, got %v", required, toolNames)
		}
	}

	compressedFound := false
	for _, msg := range history {
		if msg.OfUser != nil && strings.Contains(msg.OfUser.Content.OfString.Value, "Conversation compressed via manual compact") {
			compressedFound = true
			break
		}
	}
	if !compressedFound {
		t.Fatal("expected manual compact summary message in history")
	}

	transcriptPaths := transcriptFiles(t, transcriptDir)
	if len(transcriptPaths) != 1 {
		t.Fatalf("expected 1 transcript, got %d", len(transcriptPaths))
	}
	transcript := readFileText(t, transcriptPaths[0])
	if !strings.Contains(transcript, `"name":"compact"`) {
		t.Fatalf("expected transcript to record compact tool call, got:\n%s", transcript)
	}

	finalReply := extractFinalReply(history)
	if !strings.Contains(finalReply, "compressed context") {
		t.Fatalf("final reply should mention compressed context, got %q", finalReply)
	}
}

func TestIntegration_AutoCompactFixture(t *testing.T) {
	prompt := readFixture(t, "testdata/auto_compact_many_reads.md")
	transcriptDir := sandboxS06FixtureDir(t, "auto")

	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{ID: "call-read-1", Name: "read_file", Arguments: mustJSON(t, map[string]any{"path": "pkg/loop/agent.go"})},
				{ID: "call-read-2", Name: "read_file", Arguments: mustJSON(t, map[string]any{"path": "pkg/loop/todo_nag.go"})},
				{ID: "call-read-3", Name: "read_file", Arguments: mustJSON(t, map[string]any{"path": "pkg/loop/subagent.go"})},
				{ID: "call-read-4", Name: "read_file", Arguments: mustJSON(t, map[string]any{"path": "pkg/tools/fs.go"})},
				{ID: "call-read-5", Name: "read_file", Arguments: mustJSON(t, map[string]any{"path": "pkg/tools/task.go"})},
				{ID: "call-read-6", Name: "read_file", Arguments: mustJSON(t, map[string]any{"path": "pkg/tools/skill.go"})},
				{ID: "call-read-7", Name: "read_file", Arguments: mustJSON(t, map[string]any{"path": "pkg/devtools/recorder.go"})},
			}),
			makeHTTPStopResponse(strings.TrimSpace(`
Goal
Summarize loop and tool responsibilities after the context exceeded the threshold.
Completed
- Read multiple large files across pkg/loop, pkg/tools, and pkg/devtools.
CurrentState
- Automatic compaction was triggered after the tool results inflated history size.
Decisions
- Preserve the boundary between orchestration and tool execution.
Constraints
- Mention that full history remains in transcripts.
NextSteps
- Return three concise bullets and mention auto compaction.
`)),
			makeHTTPStopResponse(strings.TrimSpace(`
- pkg/loop orchestrates agent control flow, compaction, and round management.
- pkg/tools defines executable capabilities and validates tool inputs.
- pkg/loop decides when tools run; pkg/tools decide how each action executes.
The conversation was compacted automatically, and the full pre-compact history should exist in .transcripts/.
`)),
		},
	}

	history, requests := runS06FixtureScenarioWithOptions(t, prompt, transcriptDir, mock, loop.CompactOptions{
		ThresholdTokens:       1000,
		KeepRecentToolResults: 3,
		KeepRecentMessages:    4,
		TranscriptDir:         transcriptDir,
		SummaryCharLimit:      16000,
	})

	if len(requests) != 3 {
		t.Fatalf("expected 3 request bodies, got %d", len(requests))
	}
	if !requestContainsText(t, requests[0], "You are stress-testing automatic context compaction in the s06 context-compact session.") {
		t.Fatalf("expected first request to include embedded auto fixture, got: %s", requests[0])
	}
	if !requestContainsText(t, requests[1], "CompactionTrigger: auto") {
		t.Fatalf("expected second request to be the auto compact summary request, got: %s", requests[1])
	}

	compressedFound := false
	for _, msg := range history {
		if msg.OfUser != nil && strings.Contains(msg.OfUser.Content.OfString.Value, "Conversation compressed via auto compact") {
			compressedFound = true
			break
		}
	}
	if !compressedFound {
		t.Fatal("expected auto compact summary message in history")
	}

	transcriptPaths := transcriptFiles(t, transcriptDir)
	if len(transcriptPaths) != 1 {
		t.Fatalf("expected 1 transcript, got %d", len(transcriptPaths))
	}
	transcript := readFileText(t, transcriptPaths[0])
	if !strings.Contains(transcript, `"name":"read_file"`) {
		t.Fatalf("expected transcript to record read_file tool calls, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "[Previous: used read_file]") {
		t.Fatalf("expected transcript to capture micro-compacted tool outputs, got:\n%s", transcript)
	}

	finalReply := strings.ToLower(extractFinalReply(history))
	if !strings.Contains(finalReply, "automatically") && !strings.Contains(finalReply, "automatic") {
		t.Fatalf("final reply should mention automatic compaction, got %q", finalReply)
	}
	if !strings.Contains(finalReply, ".transcripts") {
		t.Fatalf("final reply should mention .transcripts, got %q", finalReply)
	}
}

func runS06FixtureScenario(
	t *testing.T,
	prompt string,
	transcriptDir string,
	mock *capturingMockHTTPClient,
) ([]openai.ChatCompletionMessageParamUnion, []string) {
	t.Helper()
	return runS06FixtureScenarioWithOptions(t, prompt, transcriptDir, mock, loop.CompactOptions{
		ThresholdTokens:       50000,
		KeepRecentToolResults: 3,
		KeepRecentMessages:    6,
		TranscriptDir:         transcriptDir,
		SummaryCharLimit:      16000,
	})
}

func runS06FixtureScenarioWithOptions(
	t *testing.T,
	prompt string,
	transcriptDir string,
	mock *capturingMockHTTPClient,
	opts loop.CompactOptions,
) ([]openai.ChatCompletionMessageParamUnion, []string) {
	t.Helper()

	client := newCapturingMockClient(mock)
	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(tools.CompactToolDef(), tools.NewCompactHandler())

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	system := fmt.Sprintf(
		"You are a coding agent at %s.\n"+
			"Use tools to inspect and change the workspace.\n"+
			"When the context gets large or the task changes phases, use the compact tool to compress history while preserving continuity.\n"+
			"Prefer tools over prose.",
		cwd,
	)

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}

	history, err = loop.RunWithContextCompact(context.Background(), client, "mock-model", history, registry, opts)
	if err != nil {
		t.Fatalf("agent loop error: %v", err)
	}

	requestBodies := make([]string, 0, len(mock.requestBodies))
	for _, body := range mock.requestBodies {
		requestBodies = append(requestBodies, string(body))
	}
	return history, requestBodies
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
		b, _ := io.ReadAll(req.Body)
		m.requestBodies = append(m.requestBodies, b)
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
	return marshalToHTTPResponse(tidyJSON(raw))
}

func makeHTTPToolCallResponse(toolCallID, funcName, arguments string) *http.Response {
	return makeHTTPMultiToolCallResponse([]toolCallSpec{
		{ID: toolCallID, Name: funcName, Arguments: arguments},
	})
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
	return marshalToHTTPResponse(tidyJSON(raw))
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

func tidyJSON(value map[string]any) map[string]any {
	return value
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
		t.Fatalf("failed to marshal json: %v", err)
	}
	return string(data)
}

func sandboxS06FixtureDir(t *testing.T, scenario string) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s06", "fake", t.Name(), scenario, runID, "transcripts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create transcript dir %s: %v", dir, err)
	}
	return dir
}

func transcriptFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read transcript dir %s: %v", dir, err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		paths = append(paths, filepath.Join(dir, entry.Name()))
	}
	return paths
}

func readFileText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file %s: %v", path, err)
	}
	return string(data)
}

func requestContainsText(t *testing.T, body string, needle string) bool {
	t.Helper()

	var req struct {
		Messages []struct {
			Content any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("failed to decode request body: %v\nbody=%s", err, body)
	}

	for _, msg := range req.Messages {
		switch content := msg.Content.(type) {
		case string:
			if strings.Contains(content, needle) {
				return true
			}
		case []any:
			for _, part := range content {
				obj, ok := part.(map[string]any)
				if !ok {
					continue
				}
				text, _ := obj["text"].(string)
				if strings.Contains(text, needle) {
					return true
				}
			}
		}
	}
	return false
}

func extractToolNames(messages []openai.ChatCompletionMessageParamUnion) []string {
	seen := make(map[string]bool)
	var names []string
	for _, msg := range messages {
		if msg.OfAssistant == nil {
			continue
		}
		for _, tc := range msg.OfAssistant.ToolCalls {
			name := tc.Function.Name
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
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
