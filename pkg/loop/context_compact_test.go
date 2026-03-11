package loop

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

func TestMicroCompact_ReplacesOlderToolResults(t *testing.T) {
	longResult := strings.Repeat("x", 160)
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("system"),
		assistantToolCallParam("call-1", "read_file", `{"path":"a.txt"}`),
		openai.ToolMessage(longResult, "call-1"),
		assistantToolCallParam("call-2", "bash", `{"command":"pwd"}`),
		openai.ToolMessage(longResult, "call-2"),
		assistantToolCallParam("call-3", "list_dir", `{"path":"."}`),
		openai.ToolMessage(longResult, "call-3"),
		assistantToolCallParam("call-4", "edit_file", `{"path":"a.txt"}`),
		openai.ToolMessage(longResult, "call-4"),
	}

	compacted := MicroCompact(messages, 2)

	if got := compacted[2].OfTool.Content.OfString.Value; !strings.Contains(got, "read_file") {
		t.Fatalf("expected first tool result to be compacted, got %q", got)
	}
	if got := compacted[4].OfTool.Content.OfString.Value; !strings.Contains(got, "bash") {
		t.Fatalf("expected second tool result to be compacted, got %q", got)
	}
	if got := compacted[6].OfTool.Content.OfString.Value; got != longResult {
		t.Fatalf("expected recent tool result to stay intact, got %q", got)
	}
	if got := compacted[8].OfTool.Content.OfString.Value; got != longResult {
		t.Fatalf("expected most recent tool result to stay intact, got %q", got)
	}
}

func TestRunWithContextCompact_ManualCompactWritesTranscript(t *testing.T) {
	transcriptDir := sandboxContextCompactDir(t)
	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPToolCallResponse("compact-1", "compact", `{"focus":"preserve pending edits"}`),
			makeHTTPStopResponse("Goal\nCompleted\nCurrentState\nDecisions\nConstraints\nNextSteps"),
			makeHTTPStopResponse("done after compact"),
		},
	}
	client := newCapturingMockClient(mock)

	registry := tools.New()
	registry.Register(tools.CompactToolDef(), tools.NewCompactHandler())

	initial := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("system"),
		openai.UserMessage("please compact when appropriate"),
	}

	result, err := RunWithContextCompact(context.Background(), client, "mock-model", initial, registry, CompactOptions{
		ThresholdTokens:       100000,
		KeepRecentToolResults: 2,
		KeepRecentMessages:    2,
		TranscriptDir:         transcriptDir,
		SummaryCharLimit:      4000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.callCount != 3 {
		t.Fatalf("expected 3 API calls, got %d", mock.callCount)
	}
	if len(mock.requestBodies) < 2 || !strings.Contains(string(mock.requestBodies[1]), "Focus: preserve pending edits") {
		t.Fatalf("expected summary request to include manual focus, got: %s", string(mock.requestBodies[1]))
	}

	foundSummary := false
	for _, msg := range result {
		if msg.OfUser != nil && strings.Contains(msg.OfUser.Content.OfString.Value, "Conversation compressed via manual compact") {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Fatalf("expected compressed summary message in history")
	}

	entries, err := os.ReadDir(transcriptDir)
	if err != nil {
		t.Fatalf("failed to read transcript dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected transcript file to be written")
	}
}

func TestRunWithContextCompact_AutoCompactBeforeMainLoop(t *testing.T) {
	transcriptDir := sandboxContextCompactDir(t)
	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPStopResponse("Goal\nCompleted\nCurrentState\nDecisions\nConstraints\nNextSteps"),
			makeHTTPStopResponse("final answer"),
		},
	}
	client := newCapturingMockClient(mock)

	registry := tools.New()
	initial := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("system"),
		openai.UserMessage(strings.Repeat("very large context ", 40)),
	}

	result, err := RunWithContextCompact(context.Background(), client, "mock-model", initial, registry, CompactOptions{
		ThresholdTokens:       20,
		KeepRecentToolResults: 1,
		KeepRecentMessages:    1,
		TranscriptDir:         transcriptDir,
		SummaryCharLimit:      4000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mock.callCount != 2 {
		t.Fatalf("expected 2 API calls, got %d", mock.callCount)
	}
	if len(mock.requestBodies) == 0 || !strings.Contains(string(mock.requestBodies[0]), "CompactionTrigger: auto") {
		t.Fatalf("expected first API call to be auto compact summary, got: %s", string(mock.requestBodies[0]))
	}

	foundSummary := false
	for _, msg := range result {
		if msg.OfUser != nil && strings.Contains(msg.OfUser.Content.OfString.Value, "Conversation compressed via auto compact") {
			foundSummary = true
			break
		}
	}
	if !foundSummary {
		t.Fatalf("expected auto compact summary message in history")
	}
}

func TestKeepTailMessages_PreservesAssistantToolCallForToolSuffix(t *testing.T) {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("old"),
		assistantToolCallParam("call-1", "read_file", `{"path":"a.txt"}`),
		openai.ToolMessage(strings.Repeat("x", 120), "call-1"),
		openai.AssistantMessage("done"),
	}

	tail := keepTailMessages(messages, 2)

	if len(tail) < 3 {
		t.Fatalf("expected tail to expand and keep a valid tool-call pair, got %d messages", len(tail))
	}
	if tail[0].OfAssistant == nil || len(tail[0].OfAssistant.ToolCalls) == 0 {
		t.Fatalf("expected first kept message to be assistant tool call, got %+v", tail[0])
	}
	if tail[1].OfTool == nil || tail[1].OfTool.ToolCallID != "call-1" {
		t.Fatalf("expected second kept message to be matching tool result, got %+v", tail[1])
	}
}

func TestBuildCompressedMessages_KeepsValidTailWhileShrinkingToThreshold(t *testing.T) {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("system"),
		openai.UserMessage("older context"),
		assistantToolCallParam("call-1", "read_file", `{"path":"a.txt"}`),
		openai.ToolMessage(strings.Repeat("x", 160), "call-1"),
		openai.AssistantMessage("recent reply"),
	}

	compressed := buildCompressedMessages(
		messages,
		"Goal\nCompleted\nCurrentState\nDecisions\nConstraints\nNextSteps",
		"/tmp/transcript.jsonl",
		"auto",
		2,
		80,
	)

	for i, msg := range compressed {
		if msg.OfTool == nil {
			continue
		}
		if i == 0 || compressed[i-1].OfAssistant == nil || len(compressed[i-1].OfAssistant.ToolCalls) == 0 {
			t.Fatalf("tool message at index %d should not become orphaned after threshold shrink", i)
		}
	}
}

func TestWithCompactDefaults_SetsSummaryTimeout(t *testing.T) {
	opts := withCompactDefaults(CompactOptions{})
	if opts.SummaryTimeout != defaultSummaryTimeout {
		t.Fatalf("summary timeout = %v, want %v", opts.SummaryTimeout, defaultSummaryTimeout)
	}
}

func TestNewSummaryRequestContext_IgnoresExpiredParentDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	time.Sleep(20 * time.Millisecond)

	summaryCtx, summaryCancel := newSummaryRequestContext(parent, 50*time.Millisecond)
	defer summaryCancel()

	if err := parent.Err(); err == nil {
		t.Fatal("expected parent context to be expired")
	}
	if err := summaryCtx.Err(); err != nil {
		t.Fatalf("summary context should ignore parent deadline, got %v", err)
	}
	deadline, ok := summaryCtx.Deadline()
	if !ok {
		t.Fatal("expected summary context to have its own deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > 50*time.Millisecond {
		t.Fatalf("unexpected summary timeout window: %v", remaining)
	}
}

func assistantToolCallParam(id, name, args string) openai.ChatCompletionMessageParamUnion {
	return openai.ChatCompletionMessageParamUnion{
		OfAssistant: &openai.ChatCompletionAssistantMessageParam{
			ToolCalls: []openai.ChatCompletionMessageToolCallParam{
				{
					ID: id,
					Function: openai.ChatCompletionMessageToolCallFunctionParam{
						Name:      name,
						Arguments: args,
					},
				},
			},
		},
	}
}

func sandboxContextCompactDir(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	runID := time.Now().Format("20060102-150405.000000000")
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s06", "fake", t.Name(), runID, "transcripts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create transcript dir %s: %v", dir, err)
	}
	return dir
}
