// Package devtools writes local trace data compatible with @ai-sdk/devtools viewer.
//
// It stores all interactions in plain text under .devtools/generations.json.
// Only enable in local development.
package devtools

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openai/openai-go"
)

type Run struct {
	ID        string `json:"id"`
	StartedAt string `json:"started_at"`
}

type Step struct {
	ID             string  `json:"id"`
	RunID          string  `json:"run_id"`
	StepNumber     int     `json:"step_number"`
	Type           string  `json:"type"` // "generate" | "stream"
	ModelID        string  `json:"model_id"`
	Provider       *string `json:"provider"`
	StartedAt      string  `json:"started_at"`
	DurationMS     *int64  `json:"duration_ms"`
	Input          string  `json:"input"`
	Output         *string `json:"output"`
	Usage          *string `json:"usage"`
	Error          *string `json:"error"`
	RawRequest     *string `json:"raw_request"`
	RawResponse    *string `json:"raw_response"`
	RawChunks      *string `json:"raw_chunks"`
	ProviderOption *string `json:"provider_options"`
}

type database struct {
	Runs  []Run  `json:"runs"`
	Steps []Step `json:"steps"`
}

// RunRecorder represents one DevTools "run" (a multi-step interaction).
// It is safe for concurrent use.
type RunRecorder struct {
	mu sync.Mutex

	enabled bool

	runID     string
	startedAt time.Time
	stepNo    int

	toolNameByCallID map[string]string

	dbDir  string
	dbPath string

	port int
	http *http.Client
}

// NewRunRecorderFromEnv creates a recorder when AI_SDK_DEVTOOLS is truthy.
//
// Env:
// - AI_SDK_DEVTOOLS: 1/true/yes/on to enable
// - AI_SDK_DEVTOOLS_PORT: viewer port (default 4983)
// - AI_SDK_DEVTOOLS_DIR: directory to write generations.json into
//   - if absolute: used as-is
//   - if relative: resolved under the git root
func NewRunRecorderFromEnv() *RunRecorder {
	if !envTruthy("AI_SDK_DEVTOOLS") {
		return nil
	}
	port := 4983
	if v := os.Getenv("AI_SDK_DEVTOOLS_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 && p < 65536 {
			port = p
		}
	}
	cwd, _ := os.Getwd()
	root := findGitRoot(cwd)

	dbDir := filepath.Join(root, ".devtools")
	if v := strings.TrimSpace(os.Getenv("AI_SDK_DEVTOOLS_DIR")); v != "" {
		if filepath.IsAbs(v) {
			dbDir = filepath.Clean(v)
		} else {
			dbDir = filepath.Join(root, filepath.Clean(v))
		}
	}
	return &RunRecorder{
		enabled:          true,
		runID:            generateRunID(),
		startedAt:        time.Now(),
		toolNameByCallID: make(map[string]string),
		dbDir:            dbDir,
		dbPath:           filepath.Join(dbDir, "generations.json"),
		port:             port,
		http: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

func (r *RunRecorder) RunID() string {
	if r == nil {
		return ""
	}
	return r.runID
}

func (r *RunRecorder) RegisterToolCall(toolCallID, toolName string) {
	if r == nil || !r.enabled || toolCallID == "" || toolName == "" {
		return
	}
	r.mu.Lock()
	r.toolNameByCallID[toolCallID] = toolName
	r.mu.Unlock()
}

func (r *RunRecorder) EnsureRun(ctx context.Context) {
	if r == nil || !r.enabled {
		return
	}
	_ = r.withDB(ctx, func(db *database) bool {
		for _, existing := range db.Runs {
			if existing.ID == r.runID {
				return false
			}
		}
		db.Runs = append(db.Runs, Run{
			ID:        r.runID,
			StartedAt: r.startedAt.UTC().Format(time.RFC3339Nano),
		})
		return true
	}, "run")
}

// StartStep writes an in-progress step entry (duration/output/usage are null).
//
// requestParams is optional (may be nil). When provided it must be an
// openai.ChatCompletionNewParams value; sampling parameters (Temperature, TopP,
// MaxTokens, MaxCompletionTokens, ToolChoice) are extracted and written into
// the step input so the Viewer StepConfigBar can display them.
func (r *RunRecorder) StartStep(
	ctx context.Context,
	stepType string,
	modelID string,
	provider string,
	input any,
	tools []openai.ChatCompletionToolParam,
	providerOptions any,
	requestParams ...openai.ChatCompletionNewParams,
) (stepID string, start time.Time) {
	if r == nil || !r.enabled {
		return "", time.Time{}
	}
	r.EnsureRun(ctx)
	start = time.Now()
	stepID = newUUID()

	r.mu.Lock()
	r.stepNo++
	stepNumber := r.stepNo
	r.mu.Unlock()

	inputObj := map[string]any{
		"prompt": buildPromptForViewer(input, r.snapshotToolNames()),
	}
	if len(tools) > 0 {
		inputObj["tools"] = buildToolsForViewer(tools)
	}
	if providerOptions != nil {
		inputObj["providerOptions"] = providerOptions
	}
	// Merge sampling parameters into inputObj so Viewer StepConfigBar renders them.
	if len(requestParams) > 0 {
		mergeSamplingParams(inputObj, requestParams[0])
	}

	inputJSON := mustJSON(inputObj)

	var providerPtr *string
	if strings.TrimSpace(provider) != "" {
		p := provider
		providerPtr = &p
	}

	startedAt := start.UTC().Format(time.RFC3339Nano)
	step := Step{
		ID:          stepID,
		RunID:       r.runID,
		StepNumber:  stepNumber,
		Type:        normalizeStepType(stepType),
		ModelID:     modelID,
		Provider:    providerPtr,
		StartedAt:   startedAt,
		DurationMS:  nil,
		Input:       inputJSON,
		Output:      nil,
		Usage:       nil,
		Error:       nil,
		RawRequest:  nil,
		RawResponse: nil,
		RawChunks:   nil,
		ProviderOption: func() *string {
			if providerOptions == nil {
				return nil
			}
			s := mustJSON(providerOptions)
			return &s
		}(),
	}

	_ = r.withDB(ctx, func(db *database) bool {
		db.Steps = append(db.Steps, step)
		return true
	}, "step")
	return stepID, start
}

func (r *RunRecorder) FinishStep(
	ctx context.Context,
	stepID string,
	start time.Time,
	output any,
	usage any,
	stepErr error,
	rawRequest any,
	rawResponse any,
	rawChunks any,
) {
	if r == nil || !r.enabled || stepID == "" || start.IsZero() {
		return
	}
	duration := time.Since(start).Milliseconds()
	durationMS := duration

	var outputStr *string
	if output != nil {
		s := mustJSON(output)
		outputStr = &s
	}
	var usageStr *string
	if usage != nil {
		s := mustJSON(usage)
		usageStr = &s
	}
	var errStr *string
	if stepErr != nil {
		s := stepErr.Error()
		errStr = &s
	}
	var rawReqStr *string
	if rawRequest != nil {
		s := mustJSON(rawRequest)
		rawReqStr = &s
	}
	var rawRespStr *string
	if rawResponse != nil {
		s := mustJSON(rawResponse)
		rawRespStr = &s
	}
	var rawChunksStr *string
	if rawChunks != nil {
		s := mustJSON(rawChunks)
		rawChunksStr = &s
	}

	_ = r.withDB(ctx, func(db *database) bool {
		for i := range db.Steps {
			if db.Steps[i].ID != stepID {
				continue
			}
			db.Steps[i].DurationMS = &durationMS
			db.Steps[i].Output = outputStr
			db.Steps[i].Usage = usageStr
			db.Steps[i].Error = errStr
			db.Steps[i].RawRequest = rawReqStr
			db.Steps[i].RawResponse = rawRespStr
			db.Steps[i].RawChunks = rawChunksStr
			return true
		}
		return false
	}, "step-update")
}

func (r *RunRecorder) snapshotToolNames() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.toolNameByCallID))
	for k, v := range r.toolNameByCallID {
		out[k] = v
	}
	return out
}

func (r *RunRecorder) withDB(
	ctx context.Context,
	mutate func(db *database) (changed bool),
	notifyEvent string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	db := r.readDBLocked()
	changed := mutate(&db)
	if !changed {
		return nil
	}
	if err := r.writeDBLocked(db); err != nil {
		return err
	}
	r.notify(ctx, notifyEvent)
	return nil
}

func (r *RunRecorder) readDBLocked() database {
	b, err := os.ReadFile(r.dbPath)
	if err != nil {
		return database{Runs: []Run{}, Steps: []Step{}}
	}
	var db database
	if err := json.Unmarshal(b, &db); err != nil {
		return database{Runs: []Run{}, Steps: []Step{}}
	}
	if db.Runs == nil {
		db.Runs = []Run{}
	}
	if db.Steps == nil {
		db.Steps = []Step{}
	}
	return db
}

func (r *RunRecorder) writeDBLocked(db database) error {
	if err := os.MkdirAll(r.dbDir, 0o755); err != nil {
		return err
	}
	ensureGitignoreForDir(r.dbDir)
	b, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.dbPath, b, 0o644)
}

func ensureGitignoreForDir(dbDir string) {
	dbDir = strings.TrimSpace(dbDir)
	if dbDir == "" {
		return
	}

	// Only touch .gitignore when dbDir is inside the repo.
	root := findGitRoot(".")
	rel, err := filepath.Rel(root, dbDir)
	if err != nil {
		return
	}
	rel = filepath.Clean(rel)
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return
	}

	ignoreEntry := filepath.ToSlash(rel)
	if !strings.HasSuffix(ignoreEntry, "/") {
		ignoreEntry += "/"
	}

	gitignorePath := filepath.Join(root, ".gitignore")
	b, err := os.ReadFile(gitignorePath)
	if err != nil {
		return
	}
	lines := strings.Split(string(b), "\n")
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		if t == ignoreEntry || t == strings.TrimSuffix(ignoreEntry, "/") {
			return
		}
	}
	var buf bytes.Buffer
	buf.Write(b)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteString(ignoreEntry)
	buf.WriteByte('\n')
	_ = os.WriteFile(gitignorePath, buf.Bytes(), 0o644)
}

func findGitRoot(start string) string {
	// Allow overriding for unusual layouts (e.g., monorepos)
	if v := strings.TrimSpace(os.Getenv("AI_SDK_DEVTOOLS_ROOT")); v != "" {
		return v
	}

	dir := start
	for {
		if dir == "" || dir == string(filepath.Separator) {
			return start
		}
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			// .git can be a dir (normal) or file (worktree/submodule)
			if fi.IsDir() || fi.Mode().IsRegular() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

func (r *RunRecorder) notify(ctx context.Context, event string) {
	if r == nil || !r.enabled || r.http == nil {
		return
	}
	if event == "" {
		return
	}

	// Viewer keeps an in-memory cache and only reloads when /api/notify is received.
	// Notifications are best-effort, but should be resilient to short startup delays.
	notifyCtx := ctx
	if notifyCtx == nil {
		notifyCtx = context.Background()
	}
	notifyCtx, cancel := context.WithTimeout(notifyCtx, 2*time.Second)
	defer cancel()

	reqBody := map[string]any{
		"event":     event,
		"timestamp": time.Now().UnixMilli(),
	}
	b := mustJSONBytes(reqBody)
	url := fmt.Sprintf("http://localhost:%d/api/notify", r.port)
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(notifyCtx, http.MethodPost, url, bytes.NewReader(b))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := r.http.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		// Small backoff (viewer may still be booting).
		time.Sleep(time.Duration(150*(attempt+1)) * time.Millisecond)
	}
}

func normalizeStepType(t string) string {
	switch t {
	case "stream":
		return "stream"
	default:
		return "generate"
	}
}

func buildToolsForViewer(defs []openai.ChatCompletionToolParam) []map[string]any {
	out := make([]map[string]any, 0, len(defs))
	for _, def := range defs {
		fnJSON := mustJSONBytes(def.Function)
		var fn struct {
			Name        string         `json:"name"`
			Description *string        `json:"description,omitempty"`
			Parameters  map[string]any `json:"parameters,omitempty"`
		}
		_ = json.Unmarshal(fnJSON, &fn)
		m := map[string]any{
			"name": fn.Name,
		}
		if fn.Description != nil {
			m["description"] = *fn.Description
		}
		if fn.Parameters != nil {
			m["parameters"] = fn.Parameters
		}
		out = append(out, m)
	}
	return out
}

// buildPromptForViewer accepts either:
// - []openai.ChatCompletionMessageParamUnion
// - any JSON-marshalable prompt already in AI SDK message format
//
// It normalises every message to the AI SDK viewer content-parts format so
// the viewer never shows "Empty message":
//
//	assistant → content: [{type:"text",...}, {type:"tool-call",...}, {type:"reasoning",...}]
//	tool      → content: [{type:"tool-result",...}]
func buildPromptForViewer(input any, toolNameByCallID map[string]string) any {
	switch v := input.(type) {
	case []openai.ChatCompletionMessageParamUnion:
		// Best-effort tool name resolution:
		// 1) Prefer toolNameByCallID (registered at runtime)
		// 2) Fall back to scanning prior assistant tool_calls within the same prompt
		// This makes tool-result display stable across multi-round sessions even
		// when the recorder is not reused between rounds.
		localToolNameByCallID := make(map[string]string, 16)

		prompt := make([]map[string]any, 0, len(v))
		for _, msg := range v {
			b, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				continue
			}
			role, _ := m["role"].(string)

			// Normalise assistant messages from OpenAI wire format to AI SDK viewer format.
			// OpenAI wire: { role, content: ""|[...], tool_calls: [{id, function:{name,arguments}}] }
			// AI SDK:      { role, content: [{type:"text"}, {type:"tool-call"}, {type:"reasoning"}] }
			if role == "assistant" {
				contentParts := extractAssistantContentParts(m)

				// Collect tool-call parts from OpenAI tool_calls array and register names.
				if rawCalls, ok := m["tool_calls"].([]any); ok {
					for _, rawCall := range rawCalls {
						callObj, ok := rawCall.(map[string]any)
						if !ok {
							continue
						}
						callID, _ := callObj["id"].(string)
						fnObj, _ := callObj["function"].(map[string]any)
						fnName, _ := fnObj["name"].(string)
						fnArgs, _ := fnObj["arguments"].(string)

						if callID != "" && fnName != "" {
							localToolNameByCallID[callID] = fnName
						}

						// arguments is a JSON string; parse to object so viewer renders structured args.
						var parsedArgs any
						if fnArgs != "" {
							if err := json.Unmarshal([]byte(fnArgs), &parsedArgs); err != nil {
								parsedArgs = fnArgs
							}
						}

						contentParts = append(contentParts, map[string]any{
							"type":       "tool-call",
							"toolName":   fnName,
							"toolCallId": callID,
							"args":       parsedArgs,
						})
					}
				}

				m = map[string]any{
					"role":    "assistant",
					"content": contentParts,
				}
			}

			if role == "tool" {
				toolCallID, _ := m["tool_call_id"].(string)
				toolName := toolNameByCallID[toolCallID]
				if strings.TrimSpace(toolName) == "" {
					toolName = localToolNameByCallID[toolCallID]
				}

				// If content is already an array of tool-result parts, keep it as-is.
				if parts, ok := m["content"].([]any); ok && isToolResultParts(parts) {
					m = map[string]any{
						"role":    "tool",
						"content": parts,
					}
				} else {
					result := m["content"]
					m = map[string]any{
						"role": "tool",
						"content": []map[string]any{
							{
								"type":       "tool-result",
								"toolName":   toolName,
								"toolCallId": toolCallID,
								"result":     result,
							},
						},
					}
				}
			}
			prompt = append(prompt, m)
		}
		return prompt
	default:
		return input
	}
}

// extractAssistantContentParts converts the OpenAI content field of an assistant
// message into AI SDK content parts, preserving text and reasoning/thinking parts.
func extractAssistantContentParts(m map[string]any) []map[string]any {
	parts := make([]map[string]any, 0, 4)

	switch c := m["content"].(type) {
	case string:
		if strings.TrimSpace(c) != "" {
			parts = append(parts, map[string]any{"type": "text", "text": c})
		}
	case []any:
		// Content is already an array of parts (e.g. from a previous normalisation pass).
		for _, raw := range c {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			partType, _ := part["type"].(string)
			switch partType {
			case "text", "tool-call":
				parts = append(parts, part)
			case "thinking", "reasoning":
				parts = append(parts, part)
			default:
				// Preserve unknown part types as-is.
				parts = append(parts, part)
			}
		}
	}

	// Also check for provider-specific reasoning fields at the message level.
	// Some providers (e.g. DeepSeek, QwQ) expose reasoning_content alongside content.
	for _, key := range []string{"reasoning_content", "thinking", "reasoning"} {
		if val, ok := m[key].(string); ok && strings.TrimSpace(val) != "" {
			parts = append(parts, map[string]any{
				"type":     "reasoning",
				"text":     val,
				"thinking": val, // Viewer accepts both fields
			})
			break
		}
	}

	return parts
}

// isToolResultParts returns true when parts is a non-empty slice where every
// element is a map containing type == "tool-result".
func isToolResultParts(parts []any) bool {
	if len(parts) == 0 {
		return false
	}
	for _, p := range parts {
		m, ok := p.(map[string]any)
		if !ok {
			return false
		}
		if t, _ := m["type"].(string); t != "tool-result" {
			return false
		}
	}
	return true
}

// mergeSamplingParams extracts white-listed sampling parameters from params and
// writes them into inputObj so the Viewer StepConfigBar can display them.
// Fields are only written when the caller explicitly set them (Valid() == true).
func mergeSamplingParams(inputObj map[string]any, params openai.ChatCompletionNewParams) {
	if params.Temperature.Valid() {
		inputObj["temperature"] = params.Temperature.Value
	}
	if params.TopP.Valid() {
		inputObj["topP"] = params.TopP.Value
	}
	// MaxCompletionTokens takes precedence over the deprecated MaxTokens.
	if params.MaxCompletionTokens.Valid() {
		inputObj["maxOutputTokens"] = params.MaxCompletionTokens.Value
	} else if params.MaxTokens.Valid() {
		inputObj["maxOutputTokens"] = params.MaxTokens.Value
	}
	// ToolChoice: serialise to a viewer-friendly string or object.
	if tc := params.ToolChoice; !isZeroToolChoice(tc) {
		if tc.OfAuto.Valid() {
			inputObj["toolChoice"] = tc.OfAuto.Value
		} else if tc.OfChatCompletionNamedToolChoice != nil {
			inputObj["toolChoice"] = map[string]any{
				"type":     "tool",
				"toolName": tc.OfChatCompletionNamedToolChoice.Function.Name,
			}
		}
	}
}

// isZeroToolChoice reports whether a ToolChoice union is the zero value (unset).
func isZeroToolChoice(tc openai.ChatCompletionToolChoiceOptionUnionParam) bool {
	return !tc.OfAuto.Valid() && tc.OfChatCompletionNamedToolChoice == nil
}

// ParseReasoningFromRawMessage extracts reasoning/thinking content from a raw
// provider response message map. It checks common provider-specific fields.
// Returns empty string when no reasoning content is found.
func ParseReasoningFromRawMessage(msg map[string]any) string {
	for _, key := range []string{"reasoning_content", "thinking", "reasoning"} {
		if val, ok := msg[key].(string); ok && strings.TrimSpace(val) != "" {
			return val
		}
	}
	return ""
}

func mustJSON(v any) string {
	b := mustJSONBytes(v)
	return string(b)
}

func mustJSONBytes(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// avoid panicking in devtools path
		return []byte(`null`)
	}
	return b
}

func envTruthy(name string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(name)))
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func generateRunID() string {
	now := time.Now().UTC()
	ts := now.Format("20060102150405.000000000")
	ts = strings.ReplaceAll(ts, ".", "")
	ts = ts[:17]
	return fmt.Sprintf("%s-%s", ts, randHex(4))
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func newUUID() string {
	// RFC4122 v4, without external deps.
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
