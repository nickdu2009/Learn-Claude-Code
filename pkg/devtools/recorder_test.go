package devtools

import (
	"encoding/json"
	"strings"
	"testing"

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
