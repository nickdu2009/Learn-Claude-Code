package loop

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

type managedTraceFile struct {
	Version int             `json:"version"`
	Runs    []devtools.Run  `json:"runs"`
	Steps   []devtools.Step `json:"steps"`
}

func TestRunWithManagedTrace_WritesRunLifecycle(t *testing.T) {
	traceDir := t.TempDir()
	t.Setenv("AI_SDK_DEVTOOLS", "1")
	t.Setenv("AI_SDK_DEVTOOLS_DIR", traceDir)

	registry := tools.New()
	runner := func(
		ctx context.Context,
		client *openai.Client,
		model string,
		messages []openai.ChatCompletionMessageParamUnion,
		registry *tools.Registry,
	) ([]openai.ChatCompletionMessageParamUnion, error) {
		return append(messages, openai.AssistantMessage("done")), nil
	}

	history, err := RunWithManagedTrace(
		context.Background(),
		devtools.RunMeta{
			Kind:         "main",
			Title:        "managed trace test",
			InputPreview: "hello trace",
		},
		runner,
		nil,
		"mock-model",
		[]openai.ChatCompletionMessageParamUnion{openai.UserMessage("hello trace")},
		registry,
	)
	if err != nil {
		t.Fatalf("RunWithManagedTrace returned error: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("history length = %d, want 2", len(history))
	}

	data, err := os.ReadFile(filepath.Join(traceDir, "generations.json"))
	if err != nil {
		t.Fatalf("read trace file: %v", err)
	}

	var trace managedTraceFile
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("decode trace file: %v", err)
	}
	if len(trace.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(trace.Runs))
	}

	run := trace.Runs[0]
	if run.Title != "managed trace test" {
		t.Fatalf("run title = %q, want %q", run.Title, "managed trace test")
	}
	if run.Status != "completed" {
		t.Fatalf("run status = %q, want %q", run.Status, "completed")
	}
	if run.InputPreview == nil || *run.InputPreview != "hello trace" {
		t.Fatalf("run input preview = %v, want %q", run.InputPreview, "hello trace")
	}
}
