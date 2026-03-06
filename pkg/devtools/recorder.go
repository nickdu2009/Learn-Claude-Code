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
	ID               string  `json:"id"`
	Kind             string  `json:"kind"`
	Title            string  `json:"title"`
	Status           string  `json:"status"`
	CompletionReason *string `json:"completion_reason"`
	StartedAt        string  `json:"started_at"`
	FinishedAt       *string `json:"finished_at"`
	ParentRunID      *string `json:"parent_run_id"`
	ParentStepID     *string `json:"parent_step_id"`
	Summary          *string `json:"summary"`
	InputPreview     *string `json:"input_preview"`
	StepCount        int     `json:"step_count"`
	Error            *string `json:"error"`
}

type Step struct {
	ID                string   `json:"id"`
	RunID             string   `json:"run_id"`
	StepNumber        int      `json:"step_number"`
	Type              string   `json:"type"` // "generate" | "stream"
	ModelID           string   `json:"model_id"`
	Provider          *string  `json:"provider"`
	StartedAt         string   `json:"started_at"`
	DurationMS        *int64   `json:"duration_ms"`
	Input             string   `json:"input"`
	Output            *string  `json:"output"`
	Usage             *string  `json:"usage"`
	Error             *string  `json:"error"`
	RawRequest        *string  `json:"raw_request"`
	RawResponse       *string  `json:"raw_response"`
	RawChunks         *string  `json:"raw_chunks"`
	ProviderOption    *string  `json:"provider_options"`
	LinkedChildRunIDs []string `json:"linked_child_run_ids"`
}

type database struct {
	Version     int    `json:"version"`
	GeneratedAt string `json:"generated_at"`
	Runs        []Run  `json:"runs"`
	Steps       []Step `json:"steps"`
}

type RunMeta struct {
	Kind         string
	Title        string
	InputPreview string
}

type RunResult struct {
	Status           string
	CompletionReason string
	Summary          string
	Error            string
}

type ChildRunMeta struct {
	Kind         string
	Title        string
	InputPreview string
}

// Recorder is the public interface for DevTools trace recording.
// Use NewRecorderFromEnv to create a real implementation, or Noop() for a
// no-op placeholder. RecorderFrom(ctx) never returns nil — it returns Noop()
// when no recorder was injected.
type Recorder interface {
	BeginRun(ctx context.Context, meta RunMeta) error
	FinishRun(ctx context.Context, result RunResult) error
	RunID() string
	SpawnChild(ctx context.Context, parentStepID string, meta ChildRunMeta) (Recorder, error)
	StartStep(ctx context.Context, stepType string, modelID string, provider string, input any, tools []openai.ChatCompletionToolParam, providerOptions any, requestParams ...openai.ChatCompletionNewParams) (stepID string, start time.Time)
	FinishStep(ctx context.Context, stepID string, start time.Time, output any, usage any, stepErr error, rawRequest any, rawResponse any, rawChunks any)
	RegisterToolCall(toolCallID, toolName string)
}

// noopRecorder is a Recorder that does nothing.
type noopRecorder struct{}

func (noopRecorder) BeginRun(context.Context, RunMeta) error { return nil }
func (noopRecorder) FinishRun(context.Context, RunResult) error {
	return nil
}
func (noopRecorder) RunID() string { return "" }
func (noopRecorder) SpawnChild(context.Context, string, ChildRunMeta) (Recorder, error) {
	return _noop, nil
}
func (noopRecorder) StartStep(context.Context, string, string, string, any, []openai.ChatCompletionToolParam, any, ...openai.ChatCompletionNewParams) (string, time.Time) {
	return "", time.Time{}
}
func (noopRecorder) FinishStep(context.Context, string, time.Time, any, any, error, any, any, any) {
}
func (noopRecorder) RegisterToolCall(string, string) {}

var _noop Recorder = noopRecorder{}

// Noop returns a Recorder that silently discards all calls.
func Noop() Recorder { return _noop }

// runRecorder represents one DevTools "run" (a multi-step interaction).
// It is safe for concurrent use.
type runRecorder struct {
	mu sync.Mutex

	store *recorderStore

	runID     string
	startedAt time.Time
	stepNo    int

	toolNameByCallID map[string]string

	kind         string
	title        string
	parentRunID  *string
	parentStepID *string
	inputPreview *string
}

type recorderStore struct {
	mu sync.Mutex

	enabled bool
	dbDir   string
	dbPath  string
	port    int
	http    *http.Client
}

// NewRecorderFromEnv creates a Recorder from environment variables.
// Returns Noop() when AI_SDK_DEVTOOLS is not truthy.
//
// Env:
//   - AI_SDK_DEVTOOLS: 1/true/yes/on to enable
//   - AI_SDK_DEVTOOLS_PORT: viewer port (default 4983)
//   - AI_SDK_DEVTOOLS_DIR: directory to write generations.json into
//   - if absolute: used as-is
//   - if relative: resolved under the git root
func NewRecorderFromEnv() Recorder {
	if !envTruthy("AI_SDK_DEVTOOLS") {
		return _noop
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
	store := &recorderStore{
		enabled: true,
		dbDir:   dbDir,
		dbPath:  filepath.Join(dbDir, "generations.json"),
		port:    port,
		http: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
	return &runRecorder{
		store:            store,
		runID:            generateRunID(),
		startedAt:        time.Now(),
		toolNameByCallID: make(map[string]string),
		kind:             "main",
		title:            "main agent",
	}
}

func (r *runRecorder) BeginRun(ctx context.Context, meta RunMeta) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	if strings.TrimSpace(meta.Kind) != "" {
		r.kind = strings.TrimSpace(meta.Kind)
	}
	if strings.TrimSpace(meta.Title) != "" {
		r.title = strings.TrimSpace(meta.Title)
	}
	if preview := strings.TrimSpace(meta.InputPreview); preview != "" {
		r.inputPreview = stringPtr(preview)
	}
	run := r.snapshotRunLocked()
	r.mu.Unlock()

	return r.withDB(ctx, func(db *database) (bool, error) {
		return upsertRun(db, run), nil
	}, "run")
}

func (r *runRecorder) FinishRun(ctx context.Context, result RunResult) error {
	if r == nil {
		return nil
	}

	r.mu.Lock()
	run := r.snapshotRunLocked()
	status := strings.TrimSpace(result.Status)
	if status == "" {
		status = "completed"
	}
	run.Status = status
	run.FinishedAt = stringPtr(time.Now().UTC().Format(time.RFC3339Nano))
	if reason := strings.TrimSpace(result.CompletionReason); reason != "" {
		run.CompletionReason = stringPtr(reason)
	} else if status == "completed" {
		run.CompletionReason = stringPtr("normal")
	}
	if summary := strings.TrimSpace(result.Summary); summary != "" {
		run.Summary = stringPtr(summary)
	}
	if run.Status == "error" {
		run.Error = stringPtr(strings.TrimSpace(result.Error))
	}
	r.mu.Unlock()

	return r.withDB(ctx, func(db *database) (bool, error) {
		return upsertRun(db, run), nil
	}, "run-finish")
}

func (r *runRecorder) RunID() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runID
}

func (r *runRecorder) SpawnChild(ctx context.Context, parentStepID string, meta ChildRunMeta) (Recorder, error) {
	if r == nil {
		return _noop, nil
	}
	parentStepID = strings.TrimSpace(parentStepID)
	if parentStepID == "" {
		return nil, fmt.Errorf("parent step id is required")
	}
	if err := r.ensureRun(ctx); err != nil {
		return nil, err
	}

	childKind := strings.TrimSpace(meta.Kind)
	if childKind == "" {
		childKind = "subagent"
	}
	childTitle := strings.TrimSpace(meta.Title)
	if childTitle == "" {
		childTitle = "subagent"
	}
	var previewPtr *string
	if preview := strings.TrimSpace(meta.InputPreview); preview != "" {
		previewPtr = stringPtr(preview)
	}

	parentRunID := r.RunID()
	child := &runRecorder{
		store:            r.store,
		runID:            generateRunID(),
		startedAt:        time.Now(),
		toolNameByCallID: make(map[string]string),
		kind:             childKind,
		title:            childTitle,
		parentRunID:      stringPtr(parentRunID),
		parentStepID:     stringPtr(parentStepID),
		inputPreview:     previewPtr,
	}
	childRun := child.snapshotRun()

	if err := r.withDB(ctx, func(db *database) (bool, error) {
		parentStep, ok := findStep(db.Steps, parentStepID)
		if !ok {
			return false, fmt.Errorf("parent step %q not found", parentStepID)
		}
		if parentStep.RunID != parentRunID {
			return false, fmt.Errorf("parent step %q belongs to run %q, want %q", parentStepID, parentStep.RunID, parentRunID)
		}
		changed := upsertRun(db, childRun)
		if !containsString(parentStep.LinkedChildRunIDs, childRun.ID) {
			parentStep.LinkedChildRunIDs = append(parentStep.LinkedChildRunIDs, childRun.ID)
			changed = true
		}
		return changed, nil
	}, "run") ; err != nil {
		return nil, err
	}

	return child, nil
}

func (r *runRecorder) RegisterToolCall(toolCallID, toolName string) {
	if toolCallID == "" || toolName == "" {
		return
	}
	r.mu.Lock()
	r.toolNameByCallID[toolCallID] = toolName
	r.mu.Unlock()
}

func (r *runRecorder) ensureRun(ctx context.Context) error {
	if r == nil {
		return nil
	}
	run := r.snapshotRun()
	return r.withDB(ctx, func(db *database) (bool, error) {
		return upsertRun(db, run), nil
	}, "run")
}

// StartStep writes an in-progress step entry (duration/output/usage are null).
//
// requestParams is optional (may be nil). When provided it must be an
// openai.ChatCompletionNewParams value; sampling parameters (Temperature, TopP,
// MaxTokens, MaxCompletionTokens, ToolChoice) are extracted and written into
// the step input so the Viewer StepConfigBar can display them.
func (r *runRecorder) StartStep(
	ctx context.Context,
	stepType string,
	modelID string,
	provider string,
	input any,
	tools []openai.ChatCompletionToolParam,
	providerOptions any,
	requestParams ...openai.ChatCompletionNewParams,
) (stepID string, start time.Time) {
	if err := r.ensureRun(ctx); err != nil {
		return "", time.Time{}
	}
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
		ID:                stepID,
		RunID:             r.RunID(),
		StepNumber:        stepNumber,
		Type:              normalizeStepType(stepType),
		ModelID:           modelID,
		Provider:          providerPtr,
		StartedAt:         startedAt,
		DurationMS:        nil,
		Input:             inputJSON,
		Output:            nil,
		Usage:             nil,
		Error:             nil,
		RawRequest:        nil,
		RawResponse:       nil,
		RawChunks:         nil,
		LinkedChildRunIDs: []string{},
		ProviderOption: func() *string {
			if providerOptions == nil {
				return nil
			}
			s := mustJSON(providerOptions)
			return &s
		}(),
	}

	_ = r.withDB(ctx, func(db *database) (bool, error) {
		db.Steps = append(db.Steps, step)
		if run, ok := findRun(db.Runs, step.RunID); ok {
			run.StepCount++
		}
		return true, nil
	}, "step")
	return stepID, start
}

func (r *runRecorder) FinishStep(
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
	if stepID == "" || start.IsZero() {
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

	_ = r.withDB(ctx, func(db *database) (bool, error) {
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
			return true, nil
		}
		return false, nil
	}, "step-update")
}

func (r *runRecorder) snapshotToolNames() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]string, len(r.toolNameByCallID))
	for k, v := range r.toolNameByCallID {
		out[k] = v
	}
	return out
}

const traceVersion = 2

func (r *runRecorder) snapshotRun() Run {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.snapshotRunLocked()
}

func (r *runRecorder) snapshotRunLocked() Run {
	kind := strings.TrimSpace(r.kind)
	if kind == "" {
		kind = "main"
	}
	title := strings.TrimSpace(r.title)
	if title == "" {
		title = kind
	}
	return Run{
		ID:           r.runID,
		Kind:         kind,
		Title:        title,
		Status:       "running",
		StartedAt:    r.startedAt.UTC().Format(time.RFC3339Nano),
		ParentRunID:  cloneStringPtr(r.parentRunID),
		ParentStepID: cloneStringPtr(r.parentStepID),
		InputPreview: cloneStringPtr(r.inputPreview),
	}
}

func newTraceFile() database {
	return database{
		Version: traceVersion,
		Runs:    []Run{},
		Steps:   []Step{},
	}
}

func normalizeTraceFile(db database) database {
	db.Version = traceVersion
	if db.Runs == nil {
		db.Runs = []Run{}
	}
	if db.Steps == nil {
		db.Steps = []Step{}
	}
	for i := range db.Steps {
		if db.Steps[i].LinkedChildRunIDs == nil {
			db.Steps[i].LinkedChildRunIDs = []string{}
		}
	}
	return db
}

func upsertRun(db *database, run Run) bool {
	for i := range db.Runs {
		if db.Runs[i].ID != run.ID {
			continue
		}
		run.StepCount = db.Runs[i].StepCount
		db.Runs[i] = mergeRun(db.Runs[i], run)
		return true
	}
	db.Runs = append(db.Runs, run)
	return true
}

func mergeRun(existing Run, incoming Run) Run {
	if incoming.Kind != "" {
		existing.Kind = incoming.Kind
	}
	if incoming.Title != "" {
		existing.Title = incoming.Title
	}
	if incoming.Status != "" {
		existing.Status = incoming.Status
	}
	if incoming.CompletionReason != nil {
		existing.CompletionReason = cloneStringPtr(incoming.CompletionReason)
	}
	if incoming.StartedAt != "" {
		existing.StartedAt = incoming.StartedAt
	}
	if incoming.FinishedAt != nil {
		existing.FinishedAt = cloneStringPtr(incoming.FinishedAt)
	}
	if incoming.ParentRunID != nil || existing.ParentRunID == nil {
		existing.ParentRunID = cloneStringPtr(incoming.ParentRunID)
	}
	if incoming.ParentStepID != nil || existing.ParentStepID == nil {
		existing.ParentStepID = cloneStringPtr(incoming.ParentStepID)
	}
	if incoming.Summary != nil {
		existing.Summary = cloneStringPtr(incoming.Summary)
	}
	if incoming.InputPreview != nil {
		existing.InputPreview = cloneStringPtr(incoming.InputPreview)
	}
	if incoming.Error != nil {
		existing.Error = cloneStringPtr(incoming.Error)
	}
	if incoming.StepCount > existing.StepCount {
		existing.StepCount = incoming.StepCount
	}
	return existing
}

func findRun(runs []Run, id string) (*Run, bool) {
	for i := range runs {
		if runs[i].ID == id {
			return &runs[i], true
		}
	}
	return nil, false
}

func findStep(steps []Step, id string) (*Step, bool) {
	for i := range steps {
		if steps[i].ID == id {
			return &steps[i], true
		}
	}
	return nil, false
}

func validateTraceFile(db database) error {
	if db.Version != traceVersion {
		return fmt.Errorf("unsupported trace version %d", db.Version)
	}

	runByID := make(map[string]Run, len(db.Runs))
	for _, run := range db.Runs {
		if strings.TrimSpace(run.ID) == "" {
			return fmt.Errorf("run id is required")
		}
		if _, exists := runByID[run.ID]; exists {
			return fmt.Errorf("duplicate run id %q", run.ID)
		}
		runByID[run.ID] = run
	}

	stepByID := make(map[string]Step, len(db.Steps))
	stepNumbersByRun := make(map[string]map[int]struct{}, len(db.Runs))
	stepCountByRun := make(map[string]int, len(db.Runs))
	for _, step := range db.Steps {
		if strings.TrimSpace(step.ID) == "" {
			return fmt.Errorf("step id is required")
		}
		if _, exists := stepByID[step.ID]; exists {
			return fmt.Errorf("duplicate step id %q", step.ID)
		}
		if _, exists := runByID[step.RunID]; !exists {
			return fmt.Errorf("step %q references missing run %q", step.ID, step.RunID)
		}
		if stepNumbersByRun[step.RunID] == nil {
			stepNumbersByRun[step.RunID] = make(map[int]struct{})
		}
		if _, exists := stepNumbersByRun[step.RunID][step.StepNumber]; exists {
			return fmt.Errorf("duplicate step number %d in run %q", step.StepNumber, step.RunID)
		}
		stepNumbersByRun[step.RunID][step.StepNumber] = struct{}{}
		stepCountByRun[step.RunID]++
		stepByID[step.ID] = step
	}

	for _, run := range db.Runs {
		if run.StepCount != stepCountByRun[run.ID] {
			return fmt.Errorf("run %q step_count mismatch: got %d want %d", run.ID, run.StepCount, stepCountByRun[run.ID])
		}
		switch run.Status {
		case "", "running", "completed", "error":
		default:
			return fmt.Errorf("run %q has invalid status %q", run.ID, run.Status)
		}
		if run.Status == "running" && run.FinishedAt != nil {
			return fmt.Errorf("running run %q must not have finished_at", run.ID)
		}
		if (run.Status == "completed" || run.Status == "error") && run.FinishedAt == nil {
			return fmt.Errorf("terminal run %q must have finished_at", run.ID)
		}
		if (run.ParentRunID == nil) != (run.ParentStepID == nil) {
			return fmt.Errorf("run %q must set parent_run_id and parent_step_id together", run.ID)
		}
		if run.Kind == "subagent" {
			if run.ParentRunID == nil || run.ParentStepID == nil {
				return fmt.Errorf("subagent run %q must include parent linkage", run.ID)
			}
		}
		if run.ParentRunID != nil {
			if _, exists := runByID[*run.ParentRunID]; !exists {
				return fmt.Errorf("run %q references missing parent run %q", run.ID, *run.ParentRunID)
			}
			parentStep, exists := stepByID[derefString(run.ParentStepID)]
			if !exists {
				return fmt.Errorf("run %q references missing parent step %q", run.ID, derefString(run.ParentStepID))
			}
			if parentStep.RunID != derefString(run.ParentRunID) {
				return fmt.Errorf("run %q parent step %q belongs to run %q, want %q", run.ID, parentStep.ID, parentStep.RunID, derefString(run.ParentRunID))
			}
		}
	}

	for _, step := range db.Steps {
		for _, childRunID := range step.LinkedChildRunIDs {
			child, exists := runByID[childRunID]
			if !exists {
				return fmt.Errorf("step %q links missing child run %q", step.ID, childRunID)
			}
			if derefString(child.ParentStepID) != step.ID {
				return fmt.Errorf("step %q links child run %q but child points to parent step %q", step.ID, childRunID, derefString(child.ParentStepID))
			}
		}
	}

	return nil
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	out := *value
	return &out
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (r *runRecorder) withDB(
	ctx context.Context,
	mutate func(db *database) (changed bool, err error),
	notifyEvent string,
) error {
	if r == nil || r.store == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.store.mu.Lock()
	defer r.store.mu.Unlock()

	db := r.readDBLocked()
	changed, err := mutate(&db)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	if err := r.writeDBLocked(db); err != nil {
		return err
	}
	r.notify(ctx, notifyEvent)
	return nil
}

func (r *runRecorder) readDBLocked() database {
	if r.store == nil {
		return newTraceFile()
	}
	b, err := os.ReadFile(r.store.dbPath)
	if err != nil {
		return newTraceFile()
	}
	var db database
	if err := json.Unmarshal(b, &db); err != nil {
		return newTraceFile()
	}
	if db.Version != traceVersion {
		return newTraceFile()
	}
	return normalizeTraceFile(db)
}

func (r *runRecorder) writeDBLocked(db database) error {
	if r.store == nil {
		return nil
	}
	db = normalizeTraceFile(db)
	db.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := validateTraceFile(db); err != nil {
		return err
	}
	if err := os.MkdirAll(r.store.dbDir, 0o755); err != nil {
		return err
	}
	ensureGitignoreForDir(r.store.dbDir)
	b, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := r.store.dbPath + ".tmp"
	if err := os.WriteFile(tmpPath, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, r.store.dbPath)
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

func (r *runRecorder) notify(ctx context.Context, event string) {
	if r == nil || r.store == nil || !r.store.enabled || r.store.http == nil {
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
	url := fmt.Sprintf("http://localhost:%d/api/notify", r.store.port)
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(notifyCtx, http.MethodPost, url, bytes.NewReader(b))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := r.store.http.Do(req)
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

// ─────────────────────────────────────────────────────────────────────────────
// Context-based Recorder propagation
// ─────────────────────────────────────────────────────────────────────────────

type recorderKey struct{}
type parentStepKey struct{}

// WithRecorder returns a child context carrying the given Recorder.
func WithRecorder(ctx context.Context, rec Recorder) context.Context {
	return context.WithValue(ctx, recorderKey{}, rec)
}

// RecorderFrom extracts the Recorder previously stored via WithRecorder.
// Never returns nil — returns Noop() when no recorder is present.
func RecorderFrom(ctx context.Context) Recorder {
	if rec, ok := ctx.Value(recorderKey{}).(Recorder); ok && rec != nil {
		return rec
	}
	return _noop
}

// WithParentStep returns a child context carrying the current parent step id.
func WithParentStep(ctx context.Context, stepID string) context.Context {
	return context.WithValue(ctx, parentStepKey{}, strings.TrimSpace(stepID))
}

// ParentStepFrom extracts the parent step id from context.
func ParentStepFrom(ctx context.Context) string {
	stepID, _ := ctx.Value(parentStepKey{}).(string)
	return strings.TrimSpace(stepID)
}
