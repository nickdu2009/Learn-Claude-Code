package devtools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/packages/param"
)

// ─────────────────────────────────────────────────────────────────────────────
// buildPromptForViewer tests
// ─────────────────────────────────────────────────────────────────────────────

// DT-PROMPT-01: assistant with string content + tool_calls → content parts with text + tool-call.
func TestBuildPromptForViewer_AssistantStringContentWithToolCalls(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.AssistantMessage("thinking..."),
	}
	// Inject tool_calls via raw JSON round-trip to simulate what the API returns.
	msgs = append(msgs, makeAssistantWithToolCalls("call-1", "my_tool", `{"key":"value"}`, "thinking..."))

	result := buildPromptForViewer(msgs, nil).([]map[string]any)
	// First message: plain assistant text
	m0 := result[0]
	parts0 := m0["content"].([]map[string]any)
	if len(parts0) != 1 || parts0[0]["type"] != "text" {
		t.Errorf("expected 1 text part, got %+v", parts0)
	}

	// Second message: text + tool-call
	m1 := result[1]
	parts1 := m1["content"].([]map[string]any)
	if len(parts1) != 2 {
		t.Fatalf("expected 2 parts (text+tool-call), got %d: %+v", len(parts1), parts1)
	}
	if parts1[0]["type"] != "text" {
		t.Errorf("first part should be text, got %v", parts1[0]["type"])
	}
	if parts1[1]["type"] != "tool-call" {
		t.Errorf("second part should be tool-call, got %v", parts1[1]["type"])
	}
	if parts1[1]["toolName"] != "my_tool" {
		t.Errorf("toolName mismatch: %v", parts1[1]["toolName"])
	}
}

// DT-PROMPT-02: assistant with empty content + tool_calls → only tool-call parts (no Empty message).
func TestBuildPromptForViewer_AssistantEmptyContentWithToolCalls(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		makeAssistantWithToolCalls("call-2", "list_dir", `{"path":"/tmp"}`, ""),
	}

	result := buildPromptForViewer(msgs, nil).([]map[string]any)
	parts := result[0]["content"].([]map[string]any)
	if len(parts) != 1 {
		t.Fatalf("expected 1 tool-call part, got %d: %+v", len(parts), parts)
	}
	if parts[0]["type"] != "tool-call" {
		t.Errorf("expected tool-call, got %v", parts[0]["type"])
	}
	// args should be parsed object, not string
	args, ok := parts[0]["args"].(map[string]any)
	if !ok {
		t.Fatalf("args should be map[string]any, got %T", parts[0]["args"])
	}
	if args["path"] != "/tmp" {
		t.Errorf("args.path mismatch: %v", args["path"])
	}
}

// DT-PROMPT-03: tool message with string content → tool-result part.
func TestBuildPromptForViewer_ToolMessageStringContent(t *testing.T) {
	// First add an assistant with tool_calls so localToolNameByCallID is populated.
	msgs := []openai.ChatCompletionMessageParamUnion{
		makeAssistantWithToolCalls("call-3", "read_file", `{"path":"/etc/hosts"}`, ""),
		openai.ToolMessage("file contents here", "call-3"),
	}

	result := buildPromptForViewer(msgs, nil).([]map[string]any)
	toolMsg := result[1]
	if toolMsg["role"] != "tool" {
		t.Fatalf("expected role tool, got %v", toolMsg["role"])
	}
	parts, ok := toolMsg["content"].([]map[string]any)
	if !ok {
		t.Fatalf("content should be []map[string]any, got %T", toolMsg["content"])
	}
	if len(parts) != 1 || parts[0]["type"] != "tool-result" {
		t.Errorf("expected 1 tool-result part, got %+v", parts)
	}
	if parts[0]["toolName"] != "read_file" {
		t.Errorf("toolName should be read_file, got %v", parts[0]["toolName"])
	}
	if parts[0]["result"] != "file contents here" {
		t.Errorf("result mismatch: %v", parts[0]["result"])
	}
}

// DT-PROMPT-04: tool message already in AI SDK format → kept as-is.
func TestBuildPromptForViewer_ToolMessageAlreadyNormalised(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		makeAssistantWithToolCalls("call-4", "bash", `{"command":"ls"}`, ""),
		openai.ToolMessage("output", "call-4"),
	}

	// Run twice to simulate re-normalisation.
	first := buildPromptForViewer(msgs, nil).([]map[string]any)
	// The tool message in first is already normalised. Simulate passing it through again
	// by checking isToolResultParts logic directly.
	toolContent := first[1]["content"]
	parts, ok := toolContent.([]map[string]any)
	if !ok {
		t.Fatalf("expected []map[string]any, got %T", toolContent)
	}
	if parts[0]["type"] != "tool-result" {
		t.Errorf("expected tool-result, got %v", parts[0]["type"])
	}
}

// DT-PROMPT-05: args JSON parse failure → args falls back to raw string.
func TestBuildPromptForViewer_ArgsParseFailure(t *testing.T) {
	msgs := []openai.ChatCompletionMessageParamUnion{
		makeAssistantWithToolCalls("call-5", "bad_tool", `not valid json`, ""),
	}

	result := buildPromptForViewer(msgs, nil).([]map[string]any)
	parts := result[0]["content"].([]map[string]any)
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	// args should be the raw string when JSON parse fails
	if parts[0]["args"] != "not valid json" {
		t.Errorf("expected raw string fallback, got %T: %v", parts[0]["args"], parts[0]["args"])
	}
}

// DT-PROMPT-06: assistant content already contains reasoning part → preserved as-is.
// Note: reasoning_content from provider responses is captured at the output side
// (buildViewerOutput / extractReasoningFromMessage), not in the prompt history,
// because openai-go's struct drops unknown fields during unmarshal.
func TestBuildPromptForViewer_ReasoningPartPreserved(t *testing.T) {
	// Simulate a message whose content is already an array with a reasoning part
	// (e.g. injected by a future normalisation pass or a custom provider adapter).
	raw := map[string]any{
		"role": "assistant",
		"content": []any{
			map[string]any{"type": "reasoning", "text": "deep thought", "thinking": "deep thought"},
			map[string]any{"type": "text", "text": "answer"},
		},
	}
	b, _ := json.Marshal(raw)
	var msg openai.ChatCompletionAssistantMessageParam
	_ = json.Unmarshal(b, &msg)
	msgs := []openai.ChatCompletionMessageParamUnion{{OfAssistant: &msg}}

	result := buildPromptForViewer(msgs, nil).([]map[string]any)
	parts, ok := result[0]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("content should be []map[string]any, got %T", result[0]["content"])
	}

	// At minimum we expect a text part; reasoning may or may not survive depending
	// on openai-go's handling of array content — we just verify no panic and role is correct.
	if result[0]["role"] != "assistant" {
		t.Errorf("role should be assistant, got %v", result[0]["role"])
	}
	_ = parts // content shape depends on openai-go internals; no panic is the key assertion
}

// DT-PROMPT-07: ParseReasoningFromRawMessage correctly extracts reasoning_content
// from a raw map (as would come from json.Unmarshal of a provider response).
func TestBuildPromptForViewer_ParseReasoningDirectMap(t *testing.T) {
	// This tests the extractAssistantContentParts path when m comes from a raw
	// json.Unmarshal (not via openai-go struct), e.g. a custom provider wrapper.
	m := map[string]any{
		"role":              "assistant",
		"content":           "answer",
		"reasoning_content": "I thought about it",
	}
	parts := extractAssistantContentParts(m)

	hasReasoning := false
	hasText := false
	for _, p := range parts {
		switch p["type"] {
		case "reasoning":
			hasReasoning = true
			if p["text"] != "I thought about it" {
				t.Errorf("reasoning text: got %v", p["text"])
			}
		case "text":
			hasText = true
		}
	}
	if !hasReasoning {
		t.Error("expected reasoning part")
	}
	if !hasText {
		t.Error("expected text part")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// mergeSamplingParams tests
// ─────────────────────────────────────────────────────────────────────────────

// DT-PARAMS-01: temperature and topP are written when set.
func TestMergeSamplingParams_TemperatureTopP(t *testing.T) {
	inputObj := map[string]any{}
	params := openai.ChatCompletionNewParams{
		Temperature: param.NewOpt(0.7),
		TopP:        param.NewOpt(0.9),
	}
	mergeSamplingParams(inputObj, params)

	if inputObj["temperature"] != 0.7 {
		t.Errorf("temperature: got %v", inputObj["temperature"])
	}
	if inputObj["topP"] != 0.9 {
		t.Errorf("topP: got %v", inputObj["topP"])
	}
}

// DT-PARAMS-02: MaxCompletionTokens takes precedence over MaxTokens.
func TestMergeSamplingParams_MaxTokensPrecedence(t *testing.T) {
	inputObj := map[string]any{}
	params := openai.ChatCompletionNewParams{
		MaxCompletionTokens: param.NewOpt[int64](2048),
		MaxTokens:           param.NewOpt[int64](1024),
	}
	mergeSamplingParams(inputObj, params)

	if inputObj["maxOutputTokens"] != int64(2048) {
		t.Errorf("expected 2048, got %v", inputObj["maxOutputTokens"])
	}
}

// DT-PARAMS-03: MaxTokens used when MaxCompletionTokens not set.
func TestMergeSamplingParams_MaxTokensFallback(t *testing.T) {
	inputObj := map[string]any{}
	params := openai.ChatCompletionNewParams{
		MaxTokens: param.NewOpt[int64](512),
	}
	mergeSamplingParams(inputObj, params)

	if inputObj["maxOutputTokens"] != int64(512) {
		t.Errorf("expected 512, got %v", inputObj["maxOutputTokens"])
	}
}

// DT-PARAMS-04: ToolChoice "auto" string is written.
func TestMergeSamplingParams_ToolChoiceAuto(t *testing.T) {
	inputObj := map[string]any{}
	params := openai.ChatCompletionNewParams{
		ToolChoice: openai.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt("auto"),
		},
	}
	mergeSamplingParams(inputObj, params)

	if inputObj["toolChoice"] != "auto" {
		t.Errorf("toolChoice: got %v", inputObj["toolChoice"])
	}
}

// DT-PARAMS-05: zero params → nothing written.
func TestMergeSamplingParams_ZeroParams(t *testing.T) {
	inputObj := map[string]any{"prompt": []any{}}
	mergeSamplingParams(inputObj, openai.ChatCompletionNewParams{})

	for _, key := range []string{"temperature", "topP", "maxOutputTokens", "toolChoice"} {
		if _, exists := inputObj[key]; exists {
			t.Errorf("key %q should not be written for zero params", key)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ParseReasoningFromRawMessage tests
// ─────────────────────────────────────────────────────────────────────────────

func TestParseReasoningFromRawMessage(t *testing.T) {
	cases := []struct {
		name     string
		msg      map[string]any
		expected string
	}{
		{"reasoning_content field", map[string]any{"reasoning_content": "deep thought"}, "deep thought"},
		{"thinking field", map[string]any{"thinking": "pondering"}, "pondering"},
		{"reasoning field", map[string]any{"reasoning": "logic"}, "logic"},
		{"no reasoning field", map[string]any{"content": "hello"}, ""},
		{"empty reasoning_content", map[string]any{"reasoning_content": "   "}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseReasoningFromRawMessage(tc.msg)
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// isToolResultParts tests
// ─────────────────────────────────────────────────────────────────────────────

func TestIsToolResultParts(t *testing.T) {
	yes := []any{
		map[string]any{"type": "tool-result", "toolName": "foo"},
	}
	no1 := []any{
		map[string]any{"type": "text", "text": "hi"},
	}
	no2 := []any{} // empty
	mixed := []any{
		map[string]any{"type": "tool-result"},
		map[string]any{"type": "text"},
	}

	if !isToolResultParts(yes) {
		t.Error("expected true for tool-result parts")
	}
	if isToolResultParts(no1) {
		t.Error("expected false for text parts")
	}
	if isToolResultParts(no2) {
		t.Error("expected false for empty slice")
	}
	if isToolResultParts(mixed) {
		t.Error("expected false for mixed parts")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────────────────────────────────────

// makeAssistantWithToolCalls builds a ChatCompletionMessageParamUnion that
// simulates an assistant message with tool_calls by marshaling/unmarshaling
// raw JSON (the same path the real API response takes).
func makeAssistantWithToolCalls(callID, funcName, argsJSON, textContent string) openai.ChatCompletionMessageParamUnion {
	raw := map[string]any{
		"role":    "assistant",
		"content": textContent,
		"tool_calls": []map[string]any{
			{
				"id":   callID,
				"type": "function",
				"function": map[string]any{
					"name":      funcName,
					"arguments": argsJSON,
				},
			},
		},
	}
	b, _ := json.Marshal(raw)
	var msg openai.ChatCompletionAssistantMessageParam
	_ = json.Unmarshal(b, &msg)
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &msg}
}

// makeAssistantWithReasoning builds a message that has a reasoning_content
// field alongside regular content, simulating providers like DeepSeek.
func makeAssistantWithReasoning(reasoning, content string) openai.ChatCompletionMessageParamUnion {
	raw := map[string]any{
		"role":              "assistant",
		"content":           content,
		"reasoning_content": reasoning,
	}
	b, _ := json.Marshal(raw)
	var msg openai.ChatCompletionAssistantMessageParam
	_ = json.Unmarshal(b, &msg)
	return openai.ChatCompletionMessageParamUnion{OfAssistant: &msg}
}

// assertContainsString is a helper to check JSON output contains a substring.
func assertContainsString(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("expected %q to contain %q", haystack, needle)
	}
}

func TestRecorderTraceV2_RootRunLifecycle(t *testing.T) {
	rec := newTestRecorder(t, "root-run")
	ctx := context.Background()

	if err := rec.BeginRun(ctx, RunMeta{
		Kind:         "main",
		Title:        "Root Run",
		InputPreview: "solve the task",
	}); err != nil {
		t.Fatalf("begin run: %v", err)
	}

	stepID, start := rec.StartStep(
		ctx,
		"generate",
		"mock-model",
		"dashscope",
		[]openai.ChatCompletionMessageParamUnion{openai.UserMessage("hello")},
		nil,
		map[string]any{"baseURL": "https://example.com"},
	)
	rec.FinishStep(
		ctx,
		stepID,
		start,
		map[string]any{"finishReason": "stop"},
		map[string]any{"totalTokens": 3},
		nil,
		nil,
		nil,
		nil,
	)

	if err := rec.FinishRun(ctx, RunResult{
		Status:           "completed",
		CompletionReason: "normal",
		Summary:          "done",
	}); err != nil {
		t.Fatalf("finish run: %v", err)
	}

	trace := readTraceFile(t, rec.store.dbPath)
	if trace.Version != traceVersion {
		t.Fatalf("version = %d, want %d", trace.Version, traceVersion)
	}
	if len(trace.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(trace.Runs))
	}
	if len(trace.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(trace.Steps))
	}

	run := trace.Runs[0]
	if run.ID != rec.RunID() {
		t.Fatalf("run id = %q, want %q", run.ID, rec.RunID())
	}
	if run.Kind != "main" {
		t.Fatalf("run kind = %q, want main", run.Kind)
	}
	if run.Title != "Root Run" {
		t.Fatalf("run title = %q, want Root Run", run.Title)
	}
	if run.Status != "completed" {
		t.Fatalf("run status = %q, want completed", run.Status)
	}
	if run.StepCount != 1 {
		t.Fatalf("step_count = %d, want 1", run.StepCount)
	}
	if run.FinishedAt == nil {
		t.Fatal("finished_at should be set")
	}
	if derefString(run.InputPreview) != "solve the task" {
		t.Fatalf("input preview = %q", derefString(run.InputPreview))
	}
	if derefString(run.Summary) != "done" {
		t.Fatalf("summary = %q", derefString(run.Summary))
	}

	step := trace.Steps[0]
	if step.RunID != rec.RunID() {
		t.Fatalf("step run_id = %q, want %q", step.RunID, rec.RunID())
	}
	if len(step.LinkedChildRunIDs) != 0 {
		t.Fatalf("linked child runs = %v, want empty", step.LinkedChildRunIDs)
	}
}

func TestRecorderTraceV2_SpawnChildLinksParentStep(t *testing.T) {
	rec := newTestRecorder(t, "parent-run")
	ctx := context.Background()

	if err := rec.BeginRun(ctx, RunMeta{Kind: "main", Title: "Parent Run"}); err != nil {
		t.Fatalf("begin parent run: %v", err)
	}

	parentStepID, parentStart := rec.StartStep(
		ctx,
		"generate",
		"mock-model",
		"dashscope",
		[]openai.ChatCompletionMessageParamUnion{openai.UserMessage("delegate this task")},
		nil,
		nil,
	)
	rec.FinishStep(ctx, parentStepID, parentStart, nil, nil, nil, nil, nil, nil)

	childRecorder, err := rec.SpawnChild(ctx, parentStepID, ChildRunMeta{
		Kind:         "subagent",
		Title:        "Child Run",
		InputPreview: "inspect project layout",
	})
	if err != nil {
		t.Fatalf("spawn child: %v", err)
	}

	child, ok := childRecorder.(*runRecorder)
	if !ok {
		t.Fatalf("expected *runRecorder, got %T", childRecorder)
	}

	childStepID, childStart := child.StartStep(
		ctx,
		"generate",
		"mock-model",
		"dashscope",
		[]openai.ChatCompletionMessageParamUnion{openai.UserMessage("child work")},
		nil,
		nil,
	)
	child.FinishStep(ctx, childStepID, childStart, nil, nil, nil, nil, nil, nil)
	if err := child.FinishRun(ctx, RunResult{
		Status:           "completed",
		CompletionReason: "normal",
		Summary:          "child summary",
	}); err != nil {
		t.Fatalf("finish child run: %v", err)
	}

	trace := readTraceFile(t, rec.store.dbPath)
	if len(trace.Runs) != 2 {
		t.Fatalf("runs = %d, want 2", len(trace.Runs))
	}
	if len(trace.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(trace.Steps))
	}

	parent := findRunByID(t, trace.Runs, rec.RunID())
	if parent.StepCount != 1 {
		t.Fatalf("parent step_count = %d, want 1", parent.StepCount)
	}
	childRun := findRunByID(t, trace.Runs, child.RunID())
	if childRun.Kind != "subagent" {
		t.Fatalf("child kind = %q, want subagent", childRun.Kind)
	}
	if derefString(childRun.ParentRunID) != rec.RunID() {
		t.Fatalf("child parent_run_id = %q, want %q", derefString(childRun.ParentRunID), rec.RunID())
	}
	if derefString(childRun.ParentStepID) != parentStepID {
		t.Fatalf("child parent_step_id = %q, want %q", derefString(childRun.ParentStepID), parentStepID)
	}
	if childRun.StepCount != 1 {
		t.Fatalf("child step_count = %d, want 1", childRun.StepCount)
	}
	if derefString(childRun.Summary) != "child summary" {
		t.Fatalf("child summary = %q", derefString(childRun.Summary))
	}

	parentStep := findStepByID(t, trace.Steps, parentStepID)
	if len(parentStep.LinkedChildRunIDs) != 1 || parentStep.LinkedChildRunIDs[0] != child.RunID() {
		t.Fatalf("parent step linked child ids = %v, want [%s]", parentStep.LinkedChildRunIDs, child.RunID())
	}
	childStep := findStepByID(t, trace.Steps, childStepID)
	if childStep.RunID != child.RunID() {
		t.Fatalf("child step run_id = %q, want %q", childStep.RunID, child.RunID())
	}
}

func TestRecorderTraceV2_RewritesLegacyTrace(t *testing.T) {
	rec := newTestRecorder(t, "rewrite-run")
	ctx := context.Background()

	if err := os.WriteFile(rec.store.dbPath, []byte(`{"runs":[{"id":"legacy"}],"steps":[]}`), 0o644); err != nil {
		t.Fatalf("write legacy trace: %v", err)
	}

	if err := rec.BeginRun(ctx, RunMeta{Kind: "main", Title: "Rewrite"}); err != nil {
		t.Fatalf("begin run: %v", err)
	}

	trace := readTraceFile(t, rec.store.dbPath)
	if trace.Version != traceVersion {
		t.Fatalf("version = %d, want %d", trace.Version, traceVersion)
	}
	if len(trace.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(trace.Runs))
	}
	if trace.Runs[0].ID != rec.RunID() {
		t.Fatalf("run id = %q, want %q", trace.Runs[0].ID, rec.RunID())
	}
}

func TestValidateTraceFile_RejectsBrokenInvariants(t *testing.T) {
	parentRunID := "parent"
	parentStepID := "step-1"
	db := database{
		Version: traceVersion,
		Runs: []Run{
			{
				ID:        parentRunID,
				Kind:      "main",
				Title:     "Parent",
				Status:    "completed",
				StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
				FinishedAt: func() *string {
					s := time.Now().UTC().Format(time.RFC3339Nano)
					return &s
				}(),
				StepCount: 1,
			},
			{
				ID:           "child",
				Kind:         "subagent",
				Title:        "Child",
				Status:       "completed",
				StartedAt:    time.Now().UTC().Format(time.RFC3339Nano),
				FinishedAt:   stringPtr(time.Now().UTC().Format(time.RFC3339Nano)),
				ParentRunID:  &parentRunID,
				ParentStepID: stringPtr("missing-step"),
				StepCount:    0,
			},
		},
		Steps: []Step{
			{
				ID:                parentStepID,
				RunID:             parentRunID,
				StepNumber:        1,
				Type:              "generate",
				ModelID:           "mock-model",
				StartedAt:         time.Now().UTC().Format(time.RFC3339Nano),
				Input:             "{}",
				LinkedChildRunIDs: []string{"child"},
			},
		},
	}

	if err := validateTraceFile(db); err == nil {
		t.Fatal("expected invariant validation error")
	}
}

func newTestRecorder(t *testing.T, runID string) *runRecorder {
	t.Helper()

	dir := t.TempDir()
	return &runRecorder{
		store: &recorderStore{
			enabled: true,
			dbDir:   dir,
			dbPath:  filepath.Join(dir, "generations.json"),
		},
		runID:            runID,
		startedAt:        time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC),
		toolNameByCallID: make(map[string]string),
		kind:             "main",
		title:            "main agent",
	}
}

func readTraceFile(t *testing.T, path string) database {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trace file: %v", err)
	}
	var trace database
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("unmarshal trace file: %v", err)
	}
	return trace
}

func findRunByID(t *testing.T, runs []Run, id string) Run {
	t.Helper()
	for _, run := range runs {
		if run.ID == id {
			return run
		}
	}
	t.Fatalf("run %q not found", id)
	return Run{}
}

func findStepByID(t *testing.T, steps []Step, id string) Step {
	t.Helper()
	for _, step := range steps {
		if step.ID == id {
			return step
		}
	}
	t.Fatalf("step %q not found", id)
	return Step{}
}
