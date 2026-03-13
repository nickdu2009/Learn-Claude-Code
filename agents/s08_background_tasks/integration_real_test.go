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
	"github.com/nickdu2009/learn-claude-code/pkg/background"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/openai/openai-go"
)

func TestIntegrationReal_BackgroundTaskLifecycle(t *testing.T) {
	loadS08Env()
	skipIfNoS08APIKey(t)

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	sandboxDir := sandboxS08Dir(t, "real")
	configPath := filepath.Join(sandboxDir, "config.txt")
	prompt := buildS08RealPrompt(readFixture(t, "testdata/background_task_flow.md"), configPath)
	tracePath := enableS08TraceForTest(t)

	withWorkingDir(t, sandboxDir, func() {
		backgroundManager, err := background.NewManager(sandboxDir, background.WithExecutionTimeout(30*time.Second))
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}

		registry := newS08Registry(backgroundManager)

		runner := loop.RunWithBackgroundNotifications(backgroundManager)
		history := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(buildS08SystemPrompt(sandboxDir)),
			openai.UserMessage(prompt),
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
			runner,
			client,
			getModel(),
			history,
			registry,
		)
		if err != nil {
			t.Fatalf("first runner pass: %v", err)
		}

		waitForRealBackgroundCompletion(t, backgroundManager)

		history, err = loop.RunWithManagedTrace(
			ctx,
			devtools.RunMeta{
				Kind:         "main",
				Title:        t.Name() + "/background-finish",
				InputPreview: "background completion",
			},
			runner,
			client,
			getModel(),
			history,
			registry,
		)
		if err != nil {
			t.Fatalf("second runner pass: %v", err)
		}

		toolNames := extractToolNames(history)
		for _, required := range []string{"background_run", "write_file"} {
			if !containsTool(toolNames, required) {
				t.Fatalf("expected model to call %q, got %v", required, toolNames)
			}
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("expected config.txt to exist: %v", err)
		}
		if !strings.Contains(string(data), "configured") {
			t.Fatalf("unexpected config content: %q", string(data))
		}

		finalReply := extractFinalReply(history)
		if !strings.Contains(strings.ToLower(finalReply), "background") &&
			!strings.Contains(strings.ToLower(finalReply), "finished") {
			t.Fatalf("final reply should mention background completion, got %q", finalReply)
		}

		trace := readS08IntegrationTraceFile(t, tracePath)
		if trace.Version != 2 {
			t.Fatalf("trace version = %d, want 2", trace.Version)
		}
	})
}

func TestIntegrationReal_BackgroundReleaseBundleValidation(t *testing.T) {
	loadS08Env()
	skipIfNoS08APIKey(t)

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	sandboxDir := sandboxS08Dir(t, "real")
	releaseEnvPath := filepath.Join(sandboxDir, releaseEnvFilename)
	featureFlagsPath := filepath.Join(sandboxDir, featureFlagsFilename)
	deployDocPath := filepath.Join(sandboxDir, deployDocFilename)
	basePrompt := readFixture(t, "testdata/release_bundle_validation.md")
	prompt := buildS08ReleaseBundlePrompt(basePrompt, releaseEnvPath, featureFlagsPath, deployDocPath)
	tracePath := enableS08TraceForTest(t)

	withWorkingDir(t, sandboxDir, func() {
		backgroundManager, err := background.NewManager(sandboxDir, background.WithExecutionTimeout(45*time.Second))
		if err != nil {
			t.Fatalf("NewManager: %v", err)
		}

		registry := newS08Registry(backgroundManager)

		runner := loop.RunWithBackgroundNotifications(backgroundManager)
		history := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(buildS08SystemPrompt(sandboxDir)),
			openai.UserMessage(prompt),
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
			runner,
			client,
			getModel(),
			history,
			registry,
		)
		if err != nil {
			t.Fatalf("first runner pass: %v", err)
		}

		waitForRealBackgroundCompletion(t, backgroundManager)
		task := requireSingleBackgroundTask(t, backgroundManager)

		history, err = loop.RunWithManagedTrace(
			ctx,
			devtools.RunMeta{
				Kind:         "main",
				Title:        t.Name() + "/background-finish",
				InputPreview: "background completion",
			},
			runner,
			client,
			getModel(),
			history,
			registry,
		)
		if err != nil {
			t.Fatalf("second runner pass: %v", err)
		}

		toolNames := extractToolNames(history)
		for _, required := range []string{"background_run", "check_background"} {
			if !containsTool(toolNames, required) {
				t.Fatalf("expected model to call %q, got %v", required, toolNames)
			}
		}

		assertFileContent(t, releaseEnvPath, releaseEnvContent)
		assertFileContent(t, featureFlagsPath, featureFlagsContent)
		assertFileContent(t, deployDocPath, deployDocContent)

		if task.Status != background.StatusCompleted {
			t.Fatalf("background task status = %s, want %s", task.Status, background.StatusCompleted)
		}
		for _, token := range []string{
			"RESULT=passed",
			"FILES=3",
			"PORT=8080",
			"ROLLBACK_SECTION=yes",
			"BACKGROUND_JOBS=true",
		} {
			if !strings.Contains(task.Result, token) {
				t.Fatalf("background task result should contain %q, got %q", token, task.Result)
			}
		}

		finalReply := strings.ToLower(extractFinalReply(history))
		for _, token := range []string{
			"all three files were created",
			"result=passed",
			"files=3",
			"port=8080",
			"rollback_section=yes",
			"background_jobs=true",
		} {
			if !strings.Contains(finalReply, token) {
				t.Fatalf("final reply should mention %q, got %q", token, finalReply)
			}
		}

		trace := readS08IntegrationTraceFile(t, tracePath)
		if trace.Version != 2 {
			t.Fatalf("trace version = %d, want 2", trace.Version)
		}
	})
}

func loadS08Env() {
	_ = godotenv.Load("../../.env")
}

func buildS08RealPrompt(basePrompt string, configPath string) string {
	return basePrompt + "\nUse the exact absolute path `" + configPath + "` for config.txt."
}

func skipIfNoS08APIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("DASHSCOPE_API_KEY") == "" || os.Getenv("DASHSCOPE_BASE_URL") == "" {
		t.Skip("DASHSCOPE_API_KEY or DASHSCOPE_BASE_URL not set, skipping real integration test")
	}
}

func waitForRealBackgroundCompletion(t *testing.T, manager *background.Manager) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		taskList, err := manager.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, task := range taskList {
			if task.Status == background.StatusCompleted || task.Status == background.StatusError || task.Status == background.StatusTimeout {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out waiting for background task completion")
}

func requireSingleBackgroundTask(t *testing.T, manager *background.Manager) background.Task {
	t.Helper()

	taskList, err := manager.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(taskList) != 1 {
		t.Fatalf("background task count = %d, want 1", len(taskList))
	}
	return taskList[0]
}

func enableS08TraceForTest(t *testing.T) string {
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

type s08IntegrationTraceFile struct {
	Version int `json:"version"`
}

func readS08IntegrationTraceFile(t *testing.T, path string) s08IntegrationTraceFile {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read trace file %s: %v", path, err)
	}

	var trace s08IntegrationTraceFile
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("failed to decode trace file %s: %v", path, err)
	}
	return trace
}
