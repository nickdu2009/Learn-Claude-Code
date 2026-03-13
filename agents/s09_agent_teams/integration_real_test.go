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
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

func TestIntegrationReal_TeammateWritesFileAndRepliesToLead(t *testing.T) {
	loadS09Env()
	skipIfNoS09APIKey(t)

	client, err := newClient()
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	sandboxDir := t.TempDir()
	targetFile := filepath.Join(sandboxDir, "hello.txt")
	prompt := strings.ReplaceAll(readFixture(t, "testdata/team_mailbox_flow.md"), "__TARGET_FILE__", targetFile)
	tracePath := enableS09TraceForTest(t)

	withWorkingDir(t, sandboxDir, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)

		service, err := newTeamService(ctx, client, getModel(), sandboxDir)
		if err != nil {
			t.Fatalf("newTeamService: %v", err)
		}

		if _, err := service.Spawn(ctx, "alice", "coder", prompt); err != nil {
			t.Fatalf("Spawn: %v", err)
		}

		outcome := waitForInboxMessageWithTimeout(t, service, "lead", 45*time.Second)
		if !strings.Contains(outcome, "alice finished "+targetFile) {
			t.Fatalf("unexpected lead inbox outcome: %q", outcome)
		}
		waitForFileWithTimeout(t, targetFile, 45*time.Second)

		data, err := os.ReadFile(targetFile)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if !strings.Contains(string(data), "hello from alice") {
			t.Fatalf("unexpected file content: %q", string(data))
		}

		trace := readS09IntegrationTraceFile(t, tracePath)
		if trace.Version != 2 {
			t.Fatalf("trace version = %d, want 2", trace.Version)
		}
		cancel()
		waitForAllMembersStatus(t, service, team.StatusShutdown, 5*time.Second)
	})
}

func TestIntegrationReal_TeamHandoffReleaseNote(t *testing.T) {
	loadS09Env()
	skipIfNoS09APIKey(t)

	client, err := newClient()
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}

	sandboxDir := t.TempDir()
	releaseNotesPath := filepath.Join(sandboxDir, "release_notes.md")
	reviewResultPath := filepath.Join(sandboxDir, "review_result.txt")
	basePrompt := readFixture(t, "testdata/team_handoff_release_note.md")
	prompt := buildS09HandoffPrompt(basePrompt, releaseNotesPath, reviewResultPath)
	tracePath := enableS09TraceForTest(t)

	withWorkingDir(t, sandboxDir, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)

		service, err := newTeamService(ctx, client, getModel(), sandboxDir)
		if err != nil {
			t.Fatalf("newTeamService: %v", err)
		}

		runner := loop.RunWithTeamInboxNotifications("lead", service)
		registry := newS09Registry(service)
		history := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(buildS09SystemPrompt(sandboxDir)),
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

		deadline := time.Now().Add(90 * time.Second)
		pass := 0
		for time.Now().Before(deadline) {
			if fileExists(releaseNotesPath) && fileExists(reviewResultPath) && allMembersIdle(t, service) {
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
			case <-time.After(500 * time.Millisecond):
			}
		}

		if !fileExists(releaseNotesPath) || !fileExists(reviewResultPath) {
			t.Fatalf("showcase did not produce both files: releaseNotes=%t reviewResult=%t", fileExists(releaseNotesPath), fileExists(reviewResultPath))
		}
		if !allMembersIdle(t, service) {
			t.Fatal("expected all teammates to be idle at the end of showcase")
		}
		history, err = drainLeadWakeups(ctx, t, runner, client, history, registry, service.Wakeups("lead"), pass)
		if err != nil {
			t.Fatalf("drain lead wakeups: %v", err)
		}

		assertFileContainsAll(t, releaseNotesPath,
			"# Release Notes",
			"- Added team inbox workflow",
			"- Added persistent teammates",
			"- Added reviewer handoff",
			"## Rollback",
		)
		assertTrimmedFileContent(t, reviewResultPath, "review=passed")

		toolNames := extractToolNames(history)
		for _, required := range []string{"spawn_teammate", "send_message"} {
			if !containsTool(toolNames, required) {
				t.Fatalf("expected lead history to include %q, got %v", required, toolNames)
			}
		}
		for _, token := range []string{
			"draft-ready::" + releaseNotesPath,
			"review-feedback::add rollback section",
			"review-ok::" + releaseNotesPath,
		} {
			if !historyContainsToken(history, token) {
				t.Fatalf("expected history to contain %q", token)
			}
		}
		if !historyContainsAnyToken(history, "draft-updated::"+releaseNotesPath, "revised::"+releaseNotesPath) {
			t.Fatalf("expected history to contain draft update token for %s", releaseNotesPath)
		}

		finalReply := strings.ToLower(extractFinalReply(history))
		for _, token := range []string{
			"draft-ready",
			"review-feedback",
			"review-ok",
		} {
			if !strings.Contains(finalReply, token) {
				t.Fatalf("final reply should mention %q, got %q", token, finalReply)
			}
		}
		if !strings.Contains(finalReply, "rollback section") || !strings.Contains(finalReply, "added") {
			t.Fatalf("final reply should mention rollback section being added, got %q", finalReply)
		}

		trace := readS09IntegrationTraceFile(t, tracePath)
		if trace.Version != 2 {
			t.Fatalf("trace version = %d, want 2", trace.Version)
		}
		assertTraceHasLinkedTeammates(t, trace, "alice", "bob")
		cancel()
		waitForAllMembersStatus(t, service, team.StatusShutdown, 5*time.Second)
	})
}

func loadS09Env() {
	cwd, err := os.Getwd()
	if err != nil {
		_ = godotenv.Load()
		return
	}

	repoRoot, err := findRepoRoot(cwd)
	if err != nil {
		_ = godotenv.Load()
		return
	}

	_ = godotenv.Load(filepath.Join(repoRoot, ".env"))
}

func skipIfNoS09APIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("DASHSCOPE_API_KEY") == "" || os.Getenv("DASHSCOPE_BASE_URL") == "" {
		t.Skip("skipping integration test because DASHSCOPE_API_KEY or DASHSCOPE_BASE_URL is not set")
	}
}

func findRepoRoot(start string) (string, error) {
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

func enableS09TraceForTest(t *testing.T) string {
	t.Helper()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	repoRoot, err := findRepoRoot(cwd)
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	traceDir := filepath.Join(repoRoot, ".devtools")
	tracePath := filepath.Join(traceDir, "generations.json")
	t.Setenv("AI_SDK_DEVTOOLS", "1")
	t.Setenv("AI_SDK_DEVTOOLS_DIR", traceDir)
	t.Setenv("AI_SDK_DEVTOOLS_ROOT", repoRoot)
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		t.Fatalf("failed to create trace dir %s: %v", traceDir, err)
	}

	return tracePath
}

type s09IntegrationTraceFile struct {
	Version int                   `json:"version"`
	Runs    []s09IntegrationRun   `json:"runs"`
	Steps   []s09IntegrationStep  `json:"steps"`
}

type s09IntegrationRun struct {
	ID          string  `json:"id"`
	Kind        string  `json:"kind"`
	Title       string  `json:"title"`
	ParentRunID *string `json:"parent_run_id"`
	ParentStepID *string `json:"parent_step_id"`
}

type s09IntegrationStep struct {
	ID                string   `json:"id"`
	RunID             string   `json:"run_id"`
	LinkedChildRunIDs []string `json:"linked_child_run_ids"`
}

func readS09IntegrationTraceFile(t *testing.T, path string) s09IntegrationTraceFile {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read trace file %s: %v", path, err)
	}

	var trace s09IntegrationTraceFile
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("failed to decode trace file %s: %v", path, err)
	}
	return trace
}

func assertTraceHasLinkedTeammates(t *testing.T, trace s09IntegrationTraceFile, teammateNames ...string) {
	t.Helper()

	foundLinkedChild := false
	for _, step := range trace.Steps {
		if len(step.LinkedChildRunIDs) > 0 {
			foundLinkedChild = true
			break
		}
	}
	if !foundLinkedChild {
		t.Fatal("expected at least one trace step to link teammate child runs")
	}

	for _, teammate := range teammateNames {
		found := false
		for _, run := range trace.Runs {
			if run.Kind != "teammate" {
				continue
			}
			if strings.Contains(strings.ToLower(run.Title), strings.ToLower(teammate)) && run.ParentRunID != nil && run.ParentStepID != nil {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected trace to include linked teammate run for %q", teammate)
		}
	}

	for _, run := range trace.Runs {
		if run.Kind != "teammate" || run.ParentStepID == nil {
			continue
		}
		parentStepFound := false
		for _, step := range trace.Steps {
			if step.ID != *run.ParentStepID {
				continue
			}
			parentStepFound = containsString(step.LinkedChildRunIDs, run.ID)
			break
		}
		if !parentStepFound {
			t.Fatalf("expected parent step %q to link child run %q", *run.ParentStepID, run.ID)
		}
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func waitForAllMembersStatus(t *testing.T, service interface {
	List() ([]team.Member, error)
}, want team.Status, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		memberList, err := service.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(memberList) == 0 {
			return
		}

		allMatch := true
		for _, member := range memberList {
			if member.Status != want {
				allMatch = false
				break
			}
		}
		if allMatch {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for all teammates to reach %s", want)
}

func waitForInboxMessageWithTimeout(t *testing.T, service interface {
	DrainInbox(string) ([]team.Message, error)
}, recipient string, timeout time.Duration) string {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		messages, err := service.DrainInbox(recipient)
		if err != nil {
			t.Fatalf("DrainInbox(%s): %v", recipient, err)
		}
		for _, msg := range messages {
			return msg.Content
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for inbox message for %s", recipient)
	return ""
}

func waitForFileWithTimeout(t *testing.T, path string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for file %s", path)
}

func historyContainsToken(messages []openai.ChatCompletionMessageParamUnion, token string) bool {
	for _, message := range messages {
		switch {
		case message.OfUser != nil && strings.Contains(message.OfUser.Content.OfString.Value, token):
			return true
		case message.OfAssistant != nil && strings.Contains(message.OfAssistant.Content.OfString.Value, token):
			return true
		case message.OfTool != nil && strings.Contains(message.OfTool.Content.OfString.Value, token):
			return true
		}
	}
	return false
}

func historyContainsAnyToken(messages []openai.ChatCompletionMessageParamUnion, tokens ...string) bool {
	for _, token := range tokens {
		if historyContainsToken(messages, token) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func allMembersIdle(t *testing.T, service interface {
	List() ([]team.Member, error)
}) bool {
	t.Helper()

	memberList, err := service.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(memberList) < 2 {
		return false
	}
	for _, member := range memberList {
		if member.Status != team.StatusIdle {
			return false
		}
	}
	return true
}

func assertFileContainsAll(t *testing.T, path string, parts ...string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	content := string(data)
	for _, part := range parts {
		if !strings.Contains(content, part) {
			t.Fatalf("expected %s to contain %q, got %q", path, part, content)
		}
	}
}

func assertTrimmedFileContent(t *testing.T, path string, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	got := strings.TrimSpace(string(data))
	if got != want {
		t.Fatalf("trimmed file content for %s = %q, want %q", path, got, want)
	}
}

func drainLeadWakeups(
	ctx context.Context,
	t *testing.T,
	runner loop.AgentRunner,
	client *openai.Client,
	history []openai.ChatCompletionMessageParamUnion,
	registry *tools.Registry,
	wakeups <-chan struct{},
	pass int,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	t.Helper()

	idleTimer := time.NewTimer(750 * time.Millisecond)
	defer idleTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return history, ctx.Err()
		case <-idleTimer.C:
			return history, nil
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
			idleTimer.Reset(750 * time.Millisecond)
		}
	}
}

