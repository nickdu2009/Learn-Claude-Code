//go:build integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

func TestIntegrationReal_ManualCompactFixture(t *testing.T) {
	loadS06Env()
	skipIfNoS06APIKey(t)

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	prompt := readFixture(t, "testdata/manual_compact.md")
	transcriptDir := sandboxS06RealDir(t, "manual")
	tracePath := enableS06TraceForTest(t)

	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(tools.CompactToolDef(), tools.NewCompactHandler())

	system := buildS06SystemPrompt(t)
	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}

	opts := loop.CompactOptions{
		ThresholdTokens:       50000,
		KeepRecentToolResults: 3,
		KeepRecentMessages:    6,
		TranscriptDir:         transcriptDir,
		SummaryCharLimit:      80000,
		SummaryTimeout:        45 * time.Second,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	history, err = loop.RunWithManagedTrace(
		ctx,
		devtools.RunMeta{
			Kind:         "main",
			Title:        t.Name(),
			InputPreview: prompt,
		},
		func(
			ctx context.Context,
			client *openai.Client,
			model string,
			messages []openai.ChatCompletionMessageParamUnion,
			registry *tools.Registry,
		) ([]openai.ChatCompletionMessageParamUnion, error) {
			return loop.RunWithContextCompact(ctx, client, model, messages, registry, opts)
		},
		client,
		getModel(),
		history,
		registry,
	)
	if err != nil {
		t.Fatalf("agent loop error: %v", err)
	}

	toolNames := extractToolNames(history)
	if !containsTool(toolNames, "compact") {
		t.Fatalf("expected model to call compact, got tools %v", toolNames)
	}

	manualCompactFound := false
	for _, msg := range history {
		if msg.OfUser != nil && strings.Contains(msg.OfUser.Content.OfString.Value, "Conversation compressed via manual compact") {
			manualCompactFound = true
			break
		}
	}
	if !manualCompactFound {
		t.Fatalf("expected manual compact marker in history, tools=%v final=%q", toolNames, extractFinalReply(history))
	}

	transcriptPaths := transcriptFiles(t, transcriptDir)
	if len(transcriptPaths) == 0 {
		t.Fatalf("expected transcripts in %s", transcriptDir)
	}
	transcript := readFileText(t, transcriptPaths[len(transcriptPaths)-1])
	if !strings.Contains(transcript, `"name":"compact"`) {
		t.Fatalf("expected transcript to include compact tool call, got:\n%s", transcript)
	}

	trace := readS06IntegrationTraceFile(t, tracePath)
	if trace.Version != 2 {
		t.Fatalf("trace version = %d, want 2", trace.Version)
	}
}

func TestIntegrationReal_AutoCompactFixture(t *testing.T) {
	loadS06Env()
	skipIfNoS06APIKey(t)
	skipIfS06SlowAutoTestDisabled(t)

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	turns := readTurnFixture(t, "testdata/auto_compact_turns.md")
	transcriptDir := sandboxS06RealDir(t, "auto")
	tracePath := enableS06TraceForTest(t)

	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(tools.CompactToolDef(), tools.NewCompactHandler())

	system := buildS06SystemPrompt(t)
	history := []openai.ChatCompletionMessageParamUnion{openai.SystemMessage(system)}

	opts := loop.CompactOptions{
		ThresholdTokens:       1000,
		KeepRecentToolResults: 3,
		KeepRecentMessages:    4,
		TranscriptDir:         transcriptDir,
		SummaryCharLimit:      1000,
		SummaryTimeout:        45 * time.Second,
	}

	rec := devtools.NewRecorderFromEnv()
	baseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ctx := devtools.WithRecorder(baseCtx, rec)
	if err := rec.BeginRun(ctx, devtools.RunMeta{
		Kind:         "main",
		Title:        t.Name(),
		InputPreview: strings.Join(turns, " | "),
	}); err != nil {
		t.Fatalf("begin trace run: %v", err)
	}
	for _, turn := range turns {
		history = append(history, openai.UserMessage(turn))
		history, err = loop.RunWithContextCompact(ctx, client, getModel(), history, registry, opts)
		if err != nil {
			_ = rec.FinishRun(ctx, devtools.RunResult{
				Status:           "failed",
				CompletionReason: "error",
				Error:            err.Error(),
			})
			t.Fatalf("agent loop error: %v", err)
		}
	}
	if err := rec.FinishRun(ctx, devtools.RunResult{
		Status:           "completed",
		CompletionReason: "normal",
		Summary:          extractFinalReply(history),
	}); err != nil {
		t.Fatalf("finish trace run: %v", err)
	}

	autoCompactFound := false
	for _, msg := range history {
		if msg.OfUser != nil && strings.Contains(msg.OfUser.Content.OfString.Value, "Conversation compressed via auto compact") {
			autoCompactFound = true
			break
		}
	}
	finalReply := strings.ToLower(extractFinalReply(history))
	if !autoCompactFound &&
		!strings.Contains(finalReply, "automatic compaction") &&
		!strings.Contains(finalReply, "automatically") &&
		!strings.Contains(finalReply, "compaction occurred") {
		t.Fatalf("expected auto compact evidence in history or final reply, final=%q", extractFinalReply(history))
	}

	transcriptPaths := transcriptFiles(t, transcriptDir)
	if len(transcriptPaths) == 0 {
		t.Fatalf("expected transcripts in %s", transcriptDir)
	}
	transcript := readFileText(t, transcriptPaths[len(transcriptPaths)-1])
	if !strings.Contains(transcript, `"name":"read_file"`) {
		t.Fatalf("expected transcript to include read_file tool calls, got:\n%s", transcript)
	}
	if !strings.Contains(transcript, "[Previous: used read_file]") &&
		!strings.Contains(transcript, "[Conversation compressed via auto compact.") {
		t.Fatalf("expected transcript to include either micro-compacted read_file results or auto compact marker, got:\n%s", transcript)
	}

	trace := readS06IntegrationTraceFile(t, tracePath)
	if trace.Version != 2 {
		t.Fatalf("trace version = %d, want 2", trace.Version)
	}
}

func loadS06Env() {
	_ = godotenv.Load("../../.env")
}

func skipIfNoS06APIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("DASHSCOPE_API_KEY") == "" || os.Getenv("DASHSCOPE_BASE_URL") == "" {
		t.Skip("DASHSCOPE_API_KEY or DASHSCOPE_BASE_URL not set, skipping real integration test")
	}
}

func skipIfS06SlowAutoTestDisabled(t *testing.T) {
	t.Helper()
	v := strings.TrimSpace(strings.ToLower(os.Getenv("S06_RUN_AUTO_COMPACT_REAL_TEST")))
	switch v {
	case "1", "true", "yes", "on":
		return
	default:
		t.Skip("auto compact real integration test is slow and provider-latency-sensitive; set S06_RUN_AUTO_COMPACT_REAL_TEST=1 to enable")
	}
}

func buildS06SystemPrompt(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	return strings.TrimSpace(
		"You are a coding agent at " + cwd + ".\n" +
			"Use tools to inspect and change the workspace.\n" +
			"When the context gets large or the task changes phases, use the compact tool to compress history while preserving continuity.\n" +
			"Prefer tools over prose.",
	)
}

func sandboxS06RealDir(t *testing.T, scenario string) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	runID := time.Now().Format("20060102-150405.000000000")
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s06", "real", t.Name(), scenario, runID, "transcripts")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create transcript dir %s: %v", dir, err)
	}
	return dir
}

func enableS06TraceForTest(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	traceDir := filepath.Join(repoRoot, ".devtools")
	tracePath := filepath.Join(traceDir, "generations.json")
	t.Setenv("AI_SDK_DEVTOOLS", "1")
	t.Setenv("AI_SDK_DEVTOOLS_DIR", traceDir)
	if err := os.MkdirAll(traceDir, 0755); err != nil {
		t.Fatalf("failed to create trace dir %s: %v", traceDir, err)
	}
	return tracePath
}

type s06IntegrationTraceFile struct {
	Version int             `json:"version"`
	Runs    []devtools.Run  `json:"runs"`
	Steps   []devtools.Step `json:"steps"`
}

func readS06IntegrationTraceFile(t *testing.T, path string) s06IntegrationTraceFile {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read trace file %s: %v", path, err)
	}
	var trace s06IntegrationTraceFile
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("failed to decode trace file %s: %v", path, err)
	}
	return trace
}

func readTurnFixture(t *testing.T, name string) []string {
	t.Helper()
	raw := readFixture(t, name)
	parts := strings.Split(raw, "\n---\n")
	turns := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			turns = append(turns, part)
		}
	}
	if len(turns) == 0 {
		t.Fatalf("fixture %s produced no turns", name)
	}
	return turns
}
