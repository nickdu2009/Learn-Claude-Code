//go:build integration

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/team"
	pkgtools "github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

func TestIntegrationReal_MigrationPlanApprovalAndGracefulShutdown(t *testing.T) {
	loadS10Env()
	skipIfNoS10RealRun(t)

	client, err := newClient()
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	sandboxDir := sandboxS10Dir(t, "real")
	runbookPath := filepath.Join(sandboxDir, "auth_migration_runbook.md")
	checklistPath := filepath.Join(sandboxDir, "auth_migration_checklist.txt")
	prompt := buildS10MigrationPrompt(t, runbookPath, checklistPath)
	tracePath := enableS10TraceForTest(t)

	withWorkingDir(t, sandboxDir, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()

		service, err := newS10TeamService(ctx, client, getModel(), sandboxDir)
		if err != nil {
			t.Fatalf("newS10TeamService: %v", err)
		}
		defer shutdownS10TeamServiceWithTimeout(t, service, 5*time.Second)

		runner := loop.RunWithTeamInboxNotifications("lead", service)
		registry := newS10Registry(service)
		history := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(buildS10SystemPrompt(sandboxDir)),
			openai.UserMessage(prompt),
		}

		history, err = loop.RunWithManagedTrace(
			ctx,
			devtools.RunMeta{
				Kind:         "main",
				Title:        t.Name() + "/initial",
				InputPreview: prompt,
			},
			runner,
			client,
			getModel(),
			history,
			registry,
		)
		if err != nil {
			t.Fatalf("initial lead runner pass: %v", err)
		}

		deadline := time.Now().Add(150 * time.Second)
		pass := 0
		for time.Now().Before(deadline) {
			if fileExists(runbookPath) && fileExists(checklistPath) && memberHasStatus(t, service, "alice", team.StatusShutdown) {
				break
			}

			select {
			case <-ctx.Done():
				t.Fatalf("context done before showcase completed: %v", ctx.Err())
			case <-service.Wakeups("lead"):
				pass++
				history, err = loop.RunWithManagedTrace(
					ctx,
					devtools.RunMeta{
						Kind:         "main",
						Title:        fmt.Sprintf("%s/pass-%d", t.Name(), pass),
						InputPreview: "lead wakeup",
					},
					runner,
					client,
					getModel(),
					history,
					registry,
				)
				if err != nil {
					t.Fatalf("lead runner pass %d: %v", pass, err)
				}
			case <-time.After(750 * time.Millisecond):
			}
		}

		if !fileExists(runbookPath) || !fileExists(checklistPath) {
			t.Fatalf("showcase did not produce both files: runbook=%t checklist=%t", fileExists(runbookPath), fileExists(checklistPath))
		}
		if !memberHasStatus(t, service, "alice", team.StatusShutdown) {
			t.Fatal("expected alice to be shutdown at the end of showcase")
		}

		history, err = drainLeadWakeupsS10(ctx, t, runner, client, history, registry, service.Wakeups("lead"), pass)
		if err != nil {
			t.Fatalf("drain lead wakeups: %v", err)
		}

		assertFileContainsAll(t, runbookPath,
			"# Auth Migration Runbook",
			"## Rollout",
			"## Validation",
			"## Rollback",
		)
		assertTrimmedFileContent(t, checklistPath, "precheck=done\ncanary=required\nrollback=ready")

		planRequests := service.ListPlanRequests()
		if len(planRequests) < 1 {
			t.Fatalf("plan request count = %d, want at least 1", len(planRequests))
		}
		if planRequests[len(planRequests)-1].Status != team.RequestApproved {
			t.Fatalf("last plan status = %s, want %s", planRequests[len(planRequests)-1].Status, team.RequestApproved)
		}

		shutdownRequests := service.ListShutdownRequests()
		if len(shutdownRequests) != 1 {
			t.Fatalf("shutdown request count = %d, want 1", len(shutdownRequests))
		}
		if shutdownRequests[0].Status != team.RequestApproved {
			t.Fatalf("shutdown request status = %s, want %s", shutdownRequests[0].Status, team.RequestApproved)
		}

		toolNames := extractToolNames(history)
		for _, required := range []string{"spawn_teammate", "plan_approval", "shutdown_request"} {
			if !containsToolName(toolNames, required) {
				t.Fatalf("expected lead history to include %q, got %v", required, toolNames)
			}
		}

		finalReply := strings.ToLower(extractFinalReply(history))
		for _, token := range []string{
			"approved",
			"migration-ready",
			"shutdown",
			"rollback",
			"validation",
		} {
			if !strings.Contains(finalReply, token) {
				t.Fatalf("final reply should mention %q, got %q", token, finalReply)
			}
		}

		trace := readS10IntegrationTraceFile(t, tracePath)
		if trace.Version != 2 {
			t.Fatalf("trace version = %d, want 2", trace.Version)
		}
	})
}

func loadS10Env() {
	cwd, err := os.Getwd()
	if err != nil {
		_ = godotenv.Load()
		return
	}

	repoRoot, err := findS10RepoRoot(cwd)
	if err != nil {
		_ = godotenv.Load()
		return
	}

	_ = godotenv.Load(filepath.Join(repoRoot, ".env"))
}

func skipIfNoS10RealRun(t *testing.T) {
	t.Helper()
	if os.Getenv("DASHSCOPE_API_KEY") == "" || os.Getenv("DASHSCOPE_BASE_URL") == "" {
		t.Skip("skipping integration test because DASHSCOPE_API_KEY or DASHSCOPE_BASE_URL is not set")
	}
	if os.Getenv("S10_REAL_INTEGRATION") != "1" {
		t.Skip("skipping s10 real integration test because S10_REAL_INTEGRATION is not set to 1")
	}
}

func findS10RepoRoot(start string) (string, error) {
	dir := filepath.Clean(start)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("failed to locate repository root from %s", start)
}

func sandboxS10Dir(t *testing.T, mode string) string {
	t.Helper()
	repoRoot := resolveRepoRoot(t)
	dir := filepath.Join(
		repoRoot,
		".local",
		"test-artifacts",
		"s10_team_protocols",
		mode,
		t.Name(),
		fmt.Sprintf("%d", time.Now().UTC().UnixNano()),
	)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll sandbox: %v", err)
	}
	return dir
}

func enableS10TraceForTest(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	repoRoot, err := findS10RepoRoot(cwd)
	if err != nil {
		t.Fatalf("failed to find repo root: %v", err)
	}
	traceDir := filepath.Join(repoRoot, ".devtools")
	tracePath := filepath.Join(traceDir, "generations.json")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatalf("failed to create trace dir: %v", err)
	}
	if err := os.Remove(tracePath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to clear trace file: %v", err)
	}
	oldDevtools, hadDevtools := os.LookupEnv("AI_SDK_DEVTOOLS")
	oldDir, hadDir := os.LookupEnv("AI_SDK_DEVTOOLS_DIR")
	t.Cleanup(func() {
		if hadDevtools {
			_ = os.Setenv("AI_SDK_DEVTOOLS", oldDevtools)
		} else {
			_ = os.Unsetenv("AI_SDK_DEVTOOLS")
		}
		if hadDir {
			_ = os.Setenv("AI_SDK_DEVTOOLS_DIR", oldDir)
		} else {
			_ = os.Unsetenv("AI_SDK_DEVTOOLS_DIR")
		}
	})
	if err := os.Setenv("AI_SDK_DEVTOOLS", "1"); err != nil {
		t.Fatalf("failed to set AI_SDK_DEVTOOLS: %v", err)
	}
	if err := os.Setenv("AI_SDK_DEVTOOLS_DIR", traceDir); err != nil {
		t.Fatalf("failed to set AI_SDK_DEVTOOLS_DIR: %v", err)
	}
	return tracePath
}

type s10IntegrationTraceFile struct {
	Version int `json:"version"`
}

func readS10IntegrationTraceFile(t *testing.T, tracePath string) s10IntegrationTraceFile {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(tracePath)
		if err != nil {
			if os.IsNotExist(err) {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			t.Fatalf("failed to read trace file: %v", err)
		}
		if len(data) == 0 {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		var trace s10IntegrationTraceFile
		if err := json.Unmarshal(data, &trace); err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}
		return trace
	}
	t.Fatalf("timed out waiting for trace file %s", tracePath)
	return s10IntegrationTraceFile{}
}

func drainLeadWakeupsS10(
	ctx context.Context,
	t *testing.T,
	runner loop.AgentRunner,
	client *openai.Client,
	history []openai.ChatCompletionMessageParamUnion,
	registry *pkgtools.Registry,
	wakeups <-chan struct{},
	pass int,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	t.Helper()
	idleTimer := time.NewTimer(1200 * time.Millisecond)
	defer idleTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return history, ctx.Err()
		case <-wakeups:
			pass++
			var err error
			history, err = loop.RunWithManagedTrace(
				ctx,
				devtools.RunMeta{
					Kind:         "main",
					Title:        fmt.Sprintf("%s/drain-%d", t.Name(), pass),
					InputPreview: "lead wakeup drain",
				},
				runner,
				client,
				getModel(),
				history,
				registry,
			)
			if err != nil {
				return history, err
			}
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(1200 * time.Millisecond)
		case <-idleTimer.C:
			return history, nil
		}
	}
}

func shutdownS10TeamServiceWithTimeout(t *testing.T, service interface {
	Shutdown(context.Context) error
	List() ([]team.Member, error)
}, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := service.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}

func memberHasStatus(t *testing.T, service interface{ List() ([]team.Member, error) }, name string, want team.Status) bool {
	t.Helper()
	memberList, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, member := range memberList {
		if member.Name == name && member.Status == want {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func assertFileContainsAll(t *testing.T, path string, tokens ...string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	content := string(data)
	for _, token := range tokens {
		if !strings.Contains(content, token) {
			t.Fatalf("file %s should contain %q, got %q", path, token, content)
		}
	}
}

func assertTrimmedFileContent(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if strings.TrimSpace(string(data)) != want {
		t.Fatalf("trimmed file content for %s = %q, want %q", path, strings.TrimSpace(string(data)), want)
	}
}
