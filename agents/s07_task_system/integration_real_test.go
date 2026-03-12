//go:build integration

package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/tasks"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

func TestIntegrationReal_TaskGraphLifecycle(t *testing.T) {
	loadS07Env()
	skipIfNoS07APIKey(t)

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	prompt := readFixture(t, "testdata/task_graph_flow.md")
	sandboxDir := sandboxS07Dir(t, "real")
	tasksDir := filepath.Join(sandboxDir, ".tasks")
	tracePath := enableS07TraceForTest(t)

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
	system := buildS07SystemPrompt(cwd)

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	history, err = loop.RunWithManagedTrace(
		ctx,
		devtools.RunMeta{
			Kind:         "main",
			Title:        t.Name(),
			InputPreview: prompt,
		},
		loop.Run,
		client,
		getModel(),
		history,
		registry,
	)
	if err != nil {
		t.Fatalf("agent loop error: %v", err)
	}

	toolNames := extractToolNames(history)
	for _, required := range []string{"task_create", "task_update", "task_list"} {
		if !containsTool(toolNames, required) {
			t.Fatalf("expected model to call %q, got %v", required, toolNames)
		}
	}

	taskList, err := taskService.ListTasks()
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(taskList) < 4 {
		t.Fatalf("expected at least 4 tasks, got %d", len(taskList))
	}

	bySubject := make(map[string]tasks.Task, len(taskList))
	for _, task := range taskList {
		bySubject[task.Subject] = task
	}

	for _, subject := range []string{"Parse", "Transform", "Emit", "Test"} {
		if _, ok := bySubject[subject]; !ok {
			t.Fatalf("expected task subject %q in %v", subject, keys(bySubject))
		}
	}
	if bySubject["Parse"].Status != tasks.StatusCompleted {
		t.Fatalf("Parse status = %q, want completed", bySubject["Parse"].Status)
	}
	parseID := bySubject["Parse"].ID
	if slices.Contains(bySubject["Transform"].BlockedBy, parseID) || slices.Contains(bySubject["Emit"].BlockedBy, parseID) {
		t.Fatalf("Transform and Emit should no longer be blocked by Parse: transform=%v emit=%v", bySubject["Transform"].BlockedBy, bySubject["Emit"].BlockedBy)
	}
	if len(bySubject["Test"].BlockedBy) == 0 {
		t.Fatalf("Test should remain blocked after only Parse completes, got %+v", bySubject["Test"])
	}

	finalReply := extractFinalReply(history)
	if !strings.Contains(strings.ToLower(finalReply), "ready") &&
		!strings.Contains(finalReply, "Transform") &&
		!strings.Contains(finalReply, "Emit") {
		t.Fatalf("final reply should mention ready tasks, got %q", finalReply)
	}

	trace := readS07IntegrationTraceFile(t, tracePath)
	if trace.Version != 2 {
		t.Fatalf("trace version = %d, want 2", trace.Version)
	}
}

func buildS07SystemPrompt(cwd string) string {
	return "You are a coding agent at " + cwd + ".\n" +
		"For multi-step work, create and maintain a persistent task graph using task_create, task_update, task_list, and task_get.\n" +
		"Use dependencies explicitly. Mark tasks in_progress before starting and completed when done.\n" +
		"Prefer tools over prose."
}

func loadS07Env() {
	_ = godotenv.Load("../../.env")
}

func skipIfNoS07APIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("DASHSCOPE_API_KEY") == "" || os.Getenv("DASHSCOPE_BASE_URL") == "" {
		t.Skip("DASHSCOPE_API_KEY or DASHSCOPE_BASE_URL not set, skipping real integration test")
	}
}

func enableS07TraceForTest(t *testing.T) string {
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

type s07IntegrationTraceFile struct {
	Version int `json:"version"`
}

func readS07IntegrationTraceFile(t *testing.T, path string) s07IntegrationTraceFile {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read trace file %s: %v", path, err)
	}

	var trace s07IntegrationTraceFile
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("failed to decode trace file %s: %v", path, err)
	}
	return trace
}

func keys(bySubject map[string]tasks.Task) []string {
	out := make([]string, 0, len(bySubject))
	for subject := range bySubject {
		out = append(out, subject)
	}
	return out
}
