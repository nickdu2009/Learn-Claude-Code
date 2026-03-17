package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/team"
	pkgtools "github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

//go:embed testdata/*.md
var fixtureFS embed.FS

func TestIntegration_MigrationPlanApprovalAndGracefulShutdown(t *testing.T) {
	repoRoot := resolveRepoRoot(t)
	sandboxDir := filepath.Join(repoRoot, ".local", "test-artifacts", "s10_team_protocols", "fake", t.Name(), fmt.Sprintf("%d", time.Now().UTC().UnixNano()))
	if err := os.MkdirAll(sandboxDir, 0o755); err != nil {
		t.Fatalf("MkdirAll sandbox: %v", err)
	}

	runbookPath := filepath.Join(sandboxDir, "auth_migration_runbook.md")
	checklistPath := filepath.Join(sandboxDir, "auth_migration_checklist.txt")
	prompt := buildS10MigrationPrompt(t, runbookPath, checklistPath)

	mock := &protocolMockHTTPClient{runbookPath: runbookPath, checklistPath: checklistPath}

	withWorkingDir(t, sandboxDir, func() {
		client := newProtocolMockClient(mock)
		service, err := newS10TeamService(context.Background(), client, "mock-model", sandboxDir)
		if err != nil {
			t.Fatalf("newS10TeamService: %v", err)
		}
		mock.service = service

		history := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(buildS10SystemPrompt(sandboxDir)),
			openai.UserMessage(prompt),
		}
		runner := loop.RunWithTeamInboxNotifications("lead", service)
		registry := newS10Registry(service)

		history = runLeadRound(t, runner, client, registry, history)
		waitForMemberStatus(t, service, "alice", team.StatusIdle)
		waitForPlanRequestCount(t, service, 1)

		history = runLeadRound(t, runner, client, registry, history)
		waitForMemberStatus(t, service, "alice", team.StatusIdle)
		waitForPlanRequestCount(t, service, 2)

		history = runLeadRound(t, runner, client, registry, history)
		waitForMemberStatus(t, service, "alice", team.StatusIdle)
		waitForFile(t, runbookPath)
		waitForFile(t, checklistPath)
		waitForInboxContains(t, service, "lead", "migration-ready::"+runbookPath)

		history = runLeadRound(t, runner, client, registry, history)
		waitForMemberStatus(t, service, "alice", team.StatusShutdown)
		waitForInboxContains(t, service, "lead", "shutdown_response::migration completed safely")

		history = runLeadRound(t, runner, client, registry, history)
		finalReply := extractFinalReply(history)
		for _, want := range []string{
			"initial plan was rejected",
			"revised plan was approved",
			"migration-ready",
			"alice shut down gracefully",
			runbookPath,
			checklistPath,
			"rollback",
			"validation",
		} {
			if !strings.Contains(finalReply, want) {
				t.Fatalf("final reply %q missing %q", finalReply, want)
			}
		}

		toolNames := extractToolNames(history)
		for _, want := range []string{"spawn_teammate", "plan_approval", "shutdown_request", "shutdown_response"} {
			if !containsToolName(toolNames, want) {
				t.Fatalf("tool calls %v missing %s", toolNames, want)
			}
		}

		assertFileContent(t, runbookPath, "# Auth Migration Runbook\n## Rollout\n- Migrate from legacy tokens to signed session tokens\n## Validation\n- Verify canary sessions\n## Rollback\n- Restore legacy token verification\n")
		assertFileContent(t, checklistPath, "precheck=done\ncanary=required\nrollback=ready\n")

		planRequests := service.ListPlanRequests()
		if len(planRequests) != 2 {
			t.Fatalf("plan request count = %d, want 2", len(planRequests))
		}
		if planRequests[0].Status != team.RequestRejected {
			t.Fatalf("first plan status = %s, want %s", planRequests[0].Status, team.RequestRejected)
		}
		if planRequests[1].Status != team.RequestApproved {
			t.Fatalf("second plan status = %s, want %s", planRequests[1].Status, team.RequestApproved)
		}

		shutdownRequests := service.ListShutdownRequests()
		if len(shutdownRequests) != 1 {
			t.Fatalf("shutdown request count = %d, want 1", len(shutdownRequests))
		}
		if shutdownRequests[0].Status != team.RequestApproved {
			t.Fatalf("shutdown status = %s, want %s", shutdownRequests[0].Status, team.RequestApproved)
		}
	})
}

type toolCallSpec struct {
	ID        string
	Name      string
	Arguments string
}

type protocolMockHTTPClient struct {
	mu            sync.Mutex
	leadCalls     int
	teammateCalls int
	service       *team.Service
	runbookPath   string
	checklistPath string
}

func (m *protocolMockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	bodyText := ""
	if req != nil && req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		bodyText = string(body)
		_ = req.Body.Close()
	}

	if isTeammateRequest(bodyText) {
		m.teammateCalls++
		return m.nextTeammateResponse(), nil
	}

	m.leadCalls++
	return m.nextLeadResponse(), nil
}

func (m *protocolMockHTTPClient) nextLeadResponse() *http.Response {
	switch m.leadCalls {
	case 1:
		return makeHTTPMultiToolCallResponse([]toolCallSpec{{
			ID:   "lead-spawn-alice",
			Name: "spawn_teammate",
			Arguments: mustJSON(map[string]any{
				"name":   "alice",
				"role":   "migration-engineer",
				"prompt": buildAliceMigrationPrompt(m.runbookPath, m.checklistPath),
			}),
		}})
	case 2:
		return makeHTTPStopResponse("Spawned alice and waiting for the first plan.")
	case 3:
		return makeHTTPMultiToolCallResponse([]toolCallSpec{{
			ID:   "lead-reject-plan",
			Name: "plan_approval",
			Arguments: mustJSON(map[string]any{
				"request_id": latestPlanRequestID(m.service),
				"approve":    false,
				"feedback":   "Please add rollback and validation details before you proceed.",
			}),
		}})
	case 4:
		return makeHTTPStopResponse("Initial plan rejected.")
	case 5:
		return makeHTTPMultiToolCallResponse([]toolCallSpec{{
			ID:   "lead-approve-plan",
			Name: "plan_approval",
			Arguments: mustJSON(map[string]any{
				"request_id": latestPlanRequestID(m.service),
				"approve":    true,
				"feedback":   "Approved. Proceed with the migration runbook and checklist.",
			}),
		}})
	case 6:
		return makeHTTPStopResponse("Revised plan approved.")
	case 7:
		return makeHTTPMultiToolCallResponse([]toolCallSpec{{
			ID:   "lead-request-shutdown",
			Name: "shutdown_request",
			Arguments: mustJSON(map[string]any{
				"teammate": "alice",
			}),
		}})
	case 8:
		return makeHTTPStopResponse("Shutdown requested.")
	case 9:
		return makeHTTPMultiToolCallResponse([]toolCallSpec{{
			ID:   "lead-check-shutdown",
			Name: "shutdown_response",
			Arguments: mustJSON(map[string]any{
				"request_id": latestShutdownRequestID(m.service),
			}),
		}})
	case 10:
		return makeHTTPStopResponse("The initial plan was rejected because rollback and validation were missing. The revised plan was approved, migration-ready was received, and alice shut down gracefully after producing " + m.runbookPath + " and " + m.checklistPath + ". The runbook includes rollback and validation sections.")
	default:
		return makeHTTPStopResponse("(default lead stop)")
	}
}

func (m *protocolMockHTTPClient) nextTeammateResponse() *http.Response {
	switch m.teammateCalls {
	case 1:
		return makeHTTPMultiToolCallResponse([]toolCallSpec{{
			ID:   "alice-submit-initial-plan",
			Name: "plan_approval",
			Arguments: mustJSON(map[string]any{
				"plan": "Roll out signed session tokens in phases.",
			}),
		}})
	case 2:
		return makeHTTPStopResponse("Waiting for plan review.")
	case 3:
		return makeHTTPMultiToolCallResponse([]toolCallSpec{{
			ID:   "alice-submit-revised-plan",
			Name: "plan_approval",
			Arguments: mustJSON(map[string]any{
				"plan": "Roll out signed session tokens in phases, validate canary sessions, and keep a rollback path to legacy token verification.",
			}),
		}})
	case 4:
		return makeHTTPStopResponse("Waiting for revised plan review.")
	case 5:
		return makeHTTPMultiToolCallResponse([]toolCallSpec{
			{
				ID:   "alice-write-runbook",
				Name: "write_file",
				Arguments: mustJSON(map[string]any{
					"path":    m.runbookPath,
					"content": "# Auth Migration Runbook\n## Rollout\n- Migrate from legacy tokens to signed session tokens\n## Validation\n- Verify canary sessions\n## Rollback\n- Restore legacy token verification\n",
				}),
			},
			{
				ID:   "alice-write-checklist",
				Name: "write_file",
				Arguments: mustJSON(map[string]any{
					"path":    m.checklistPath,
					"content": "precheck=done\ncanary=required\nrollback=ready\n",
				}),
			},
			{
				ID:   "alice-migration-ready",
				Name: "send_message",
				Arguments: mustJSON(map[string]any{
					"to":      "lead",
					"content": "migration-ready::" + m.runbookPath,
				}),
			},
		})
	case 6:
		return makeHTTPStopResponse("Migration artifacts ready.")
	case 7:
		return makeHTTPMultiToolCallResponse([]toolCallSpec{{
			ID:   "alice-approve-shutdown",
			Name: "shutdown_response",
			Arguments: mustJSON(map[string]any{
				"request_id": latestShutdownRequestID(m.service),
				"approve":    true,
				"reason":     "shutdown_response::migration completed safely",
			}),
		}})
	case 8:
		return makeHTTPStopResponse("Shutting down after completed work.")
	default:
		return makeHTTPStopResponse("(default teammate stop)")
	}
}

func isTeammateRequest(body string) bool {
	return strings.Contains(body, "Before major or risky work, submit a plan with plan_approval")
}

func newProtocolMockClient(mock *protocolMockHTTPClient) *openai.Client {
	client := openai.NewClient(
		option.WithAPIKey("mock-key"),
		option.WithBaseURL("https://mock.example.com/v1/"),
		option.WithHTTPClient(mock),
		option.WithMaxRetries(0),
	)
	return &client
}

func runLeadRound(
	t *testing.T,
	runner loop.AgentRunner,
	client *openai.Client,
	registry *pkgtools.Registry,
	history []openai.ChatCompletionMessageParamUnion,
) []openai.ChatCompletionMessageParamUnion {
	t.Helper()
	nextHistory, err := runner(context.Background(), client, "mock-model", history, registry)
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	return nextHistory
}

func buildS10MigrationPrompt(t *testing.T, runbookPath, checklistPath string) string {
	t.Helper()
	prompt := readFixture(t, "testdata/migration_plan_approval_and_graceful_shutdown.md")
	prompt = strings.ReplaceAll(prompt, "__RUNBOOK_PATH__", runbookPath)
	prompt = strings.ReplaceAll(prompt, "__CHECKLIST_PATH__", checklistPath)
	return prompt
}

func buildAliceMigrationPrompt(runbookPath, checklistPath string) string {
	return "Work on the auth migration carefully. First submit a plan for approval that omits rollback and validation details. After lead feedback arrives, submit a revised plan that includes rollback and validation. Only after approval, write the runbook to " + runbookPath + " with rollout, validation, and rollback sections, write the checklist to " + checklistPath + " with the exact checklist content, send lead migration-ready::" + runbookPath + ", and if a shutdown request arrives after the work is complete, approve it."
}

func latestPlanRequestID(service *team.Service) string {
	requests := service.ListPlanRequests()
	if len(requests) == 0 {
		return ""
	}
	return requests[len(requests)-1].RequestID
}

func latestShutdownRequestID(service *team.Service) string {
	requests := service.ListShutdownRequests()
	if len(requests) == 0 {
		return ""
	}
	return requests[len(requests)-1].RequestID
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := fixtureFS.ReadFile(name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func makeHTTPStopResponse(content string) *http.Response {
	body := map[string]any{
		"id":      "mock-id",
		"object":  "chat.completion",
		"created": 0,
		"model":   "mock-model",
		"choices": []map[string]any{
			{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
					"refusal": "",
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}
	return marshalToHTTPResponse(body)
}

func makeHTTPMultiToolCallResponse(calls []toolCallSpec) *http.Response {
	toolCalls := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		toolCalls = append(toolCalls, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": call.Arguments,
			},
		})
	}

	body := map[string]any{
		"id":      "mock-id",
		"object":  "chat.completion",
		"created": 0,
		"model":   "mock-model",
		"choices": []map[string]any{
			{
				"index":         0,
				"finish_reason": "tool_calls",
				"message": map[string]any{
					"role":       "assistant",
					"content":    "",
					"refusal":    "",
					"tool_calls": toolCalls,
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}
	return marshalToHTTPResponse(body)
}

func marshalToHTTPResponse(body map[string]any) *http.Response {
	data, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

func cloneHTTPResponse(resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(data))
	return &http.Response{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

func extractToolNames(messages []openai.ChatCompletionMessageParamUnion) []string {
	toolNames := make([]string, 0)
	for _, message := range messages {
		if message.OfAssistant == nil {
			continue
		}
		for _, call := range message.OfAssistant.ToolCalls {
			toolNames = append(toolNames, call.Function.Name)
		}
	}
	return toolNames
}

func containsToolName(toolNames []string, want string) bool {
	for _, name := range toolNames {
		if name == want {
			return true
		}
	}
	return false
}

func extractFinalReply(messages []openai.ChatCompletionMessageParamUnion) string {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.OfAssistant == nil {
			continue
		}
		if message.OfAssistant.Content.OfString.Value != "" {
			return message.OfAssistant.Content.OfString.Value
		}
	}
	return ""
}

func resolveRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for dir := filepath.Clean(cwd); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found from %s", cwd)
		}
	}
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%s): %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()
	fn()
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for file %s", path)
}

func waitForPlanRequestCount(t *testing.T, service *team.Service, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(service.ListPlanRequests()) >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d plan requests", want)
}

func waitForInboxContains(t *testing.T, service interface {
	DrainInbox(string) ([]team.Message, error)
}, recipient string, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := service.DrainInbox(recipient)
		if err != nil {
			t.Fatalf("DrainInbox(%s): %v", recipient, err)
		}
		for _, msg := range messages {
			if strings.Contains(msg.Content, want) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for inbox message containing %q for %s", want, recipient)
}

func waitForMemberStatus(t *testing.T, service interface{ List() ([]team.Member, error) }, name string, want team.Status) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		memberList, err := service.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		for _, member := range memberList {
			if member.Name == name && member.Status == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to reach %s", name, want)
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("file content for %s = %q, want %q", path, string(data), want)
	}
}
