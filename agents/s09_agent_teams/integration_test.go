package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/team"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

//go:embed testdata/*.md
var fixtureFS embed.FS

func TestIntegration_TeammateWritesFileAndRepliesToLead(t *testing.T) {
	sandboxDir := t.TempDir()
	targetFile := filepath.Join(sandboxDir, "hello.txt")
	prompt := strings.ReplaceAll(readFixture(t, "testdata/team_mailbox_flow.md"), "__TARGET_FILE__", targetFile)

	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{
					ID:   "alice-bash",
					Name: "bash",
					Arguments: mustJSON(t, map[string]any{
						"command": "printf 'hello from alice' > " + shellQuote(targetFile),
					}),
				},
				{
					ID:   "alice-send",
					Name: "send_message",
					Arguments: mustJSON(t, map[string]any{
						"to":      "lead",
						"content": "alice finished " + targetFile,
					}),
				},
			}),
			makeHTTPStopResponse("Task complete."),
		},
	}

	withWorkingDir(t, sandboxDir, func() {
		client := newCapturingMockClient(mock)
		service, err := newTeamService(context.Background(), client, "mock-model", sandboxDir)
		if err != nil {
			t.Fatalf("newTeamService: %v", err)
		}
		registry := newS09Registry(service)
		_, err = registry.Dispatch(context.Background(), "spawn_teammate", map[string]any{
			"name":   "alice",
			"role":   "coder",
			"prompt": prompt,
		})
		if err != nil {
			t.Fatalf("Dispatch spawn_teammate: %v", err)
		}

		waitForMockCalls(t, mock, 2)
		outcome := waitForInboxMessage(t, service, "lead")
		if strings.Contains(outcome, "stopped with error:") {
			t.Fatalf("teammate failed: %s", outcome)
		}
		if !strings.Contains(outcome, "alice finished "+targetFile) {
			t.Fatalf("unexpected teammate outcome: %q", outcome)
		}
		waitForFile(t, targetFile)

		data, err := os.ReadFile(targetFile)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		if string(data) != "hello from alice" {
			t.Fatalf("file content = %q, want %q", string(data), "hello from alice")
		}

		memberList, err := service.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(memberList) != 1 || memberList[0].Status != "idle" {
			t.Fatalf("unexpected teammates: %#v", memberList)
		}
	})
}

func TestIntegration_LeadRunnerInjectsInboxMessages(t *testing.T) {
	sandboxDir := t.TempDir()
	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPStopResponse("Lead processed inbox update."),
		},
	}

	withWorkingDir(t, sandboxDir, func() {
		client := newCapturingMockClient(mock)
		service, err := newTeamService(context.Background(), client, "mock-model", sandboxDir)
		if err != nil {
			t.Fatalf("newTeamService: %v", err)
		}
		if err := service.Send("lead", "lead", "manual inbox update", "message"); err != nil {
			t.Fatalf("Send: %v", err)
		}

		history := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(buildS09SystemPrompt(sandboxDir)),
			openai.UserMessage("check inbox"),
		}
		runner := loop.RunWithTeamInboxNotifications("lead", service)

		history, err = runner(context.Background(), client, "mock-model", history, newS09Registry(service))
		if err != nil {
			t.Fatalf("runner: %v", err)
		}

		if len(mock.requestBodies) == 0 {
			t.Fatal("expected at least one captured request body")
		}
		body := string(mock.requestBodies[0])
		if !strings.Contains(body, "\\u003cinbox\\u003e") || !strings.Contains(body, "manual inbox update") {
			t.Fatalf("expected inbox payload injection, got %s", body)
		}

		finalReply := extractFinalReply(history)
		if !strings.Contains(finalReply, "Lead processed inbox update.") {
			t.Fatalf("unexpected final reply: %q", finalReply)
		}
	})
}

func TestIntegration_PersistentTeammateHandoffWorkflow(t *testing.T) {
	sandboxDir := t.TempDir()
	releaseNotesPath := filepath.Join(sandboxDir, "release_notes.md")
	reviewResultPath := filepath.Join(sandboxDir, "review_result.txt")
	initialContent := "# Release Notes\n- Added team inbox workflow\n- Added persistent teammates\n- Added reviewer handoff\n"
	finalContent := initialContent + "\n## Rollback\n- Restore previous version\n"

	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{
					ID:   "alice-write-draft",
					Name: "bash",
					Arguments: mustJSON(t, map[string]any{
						"command": "printf %s " + shellQuote(initialContent) + " > " + shellQuote(releaseNotesPath),
					}),
				},
				{
					ID:   "alice-draft-ready",
					Name: "send_message",
					Arguments: mustJSON(t, map[string]any{
						"to":      "lead",
						"content": "draft-ready::" + releaseNotesPath,
					}),
				},
			}),
			makeHTTPStopResponse("alice draft ready"),
			makeHTTPStopResponse("bob waiting for review request"),
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{
					ID:   "bob-read-draft",
					Name: "read_file",
					Arguments: mustJSON(t, map[string]any{
						"path": releaseNotesPath,
					}),
				},
				{
					ID:   "bob-feedback",
					Name: "send_message",
					Arguments: mustJSON(t, map[string]any{
						"to":      "lead",
						"content": "review-feedback::add rollback section",
					}),
				},
			}),
			makeHTTPStopResponse("bob sent feedback"),
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{
					ID:   "alice-edit-draft",
					Name: "bash",
					Arguments: mustJSON(t, map[string]any{
						"command": "printf %s " + shellQuote(finalContent) + " > " + shellQuote(releaseNotesPath),
					}),
				},
				{
					ID:   "alice-updated",
					Name: "send_message",
					Arguments: mustJSON(t, map[string]any{
						"to":      "lead",
						"content": "draft-updated::" + releaseNotesPath,
					}),
				},
			}),
			makeHTTPStopResponse("alice updated draft"),
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{
					ID:   "bob-read-final",
					Name: "read_file",
					Arguments: mustJSON(t, map[string]any{
						"path": releaseNotesPath,
					}),
				},
				{
					ID:   "bob-write-review",
					Name: "bash",
					Arguments: mustJSON(t, map[string]any{
						"command": "printf %s " + shellQuote("review=passed") + " > " + shellQuote(reviewResultPath),
					}),
				},
				{
					ID:   "bob-review-ok",
					Name: "send_message",
					Arguments: mustJSON(t, map[string]any{
						"to":      "lead",
						"content": "review-ok::" + releaseNotesPath,
					}),
				},
			}),
			makeHTTPStopResponse("bob review ok"),
		},
	}

	withWorkingDir(t, sandboxDir, func() {
		client := newCapturingMockClient(mock)
		service, err := newTeamService(context.Background(), client, "mock-model", sandboxDir)
		if err != nil {
			t.Fatalf("newTeamService: %v", err)
		}

		if _, err := service.Spawn(context.Background(), "alice", "writer", buildAliceDraftPrompt(releaseNotesPath)); err != nil {
			t.Fatalf("Spawn alice: %v", err)
		}
		waitForMemberStatus(t, service, "alice", "idle")
		outcome := waitForInboxMessage(t, service, "lead")
		if !strings.Contains(outcome, "draft-ready::"+releaseNotesPath) {
			t.Fatalf("unexpected draft-ready outcome: %q", outcome)
		}

		if _, err := service.Spawn(context.Background(), "bob", "reviewer", buildBobReviewPrompt(releaseNotesPath, reviewResultPath)); err != nil {
			t.Fatalf("Spawn bob: %v", err)
		}
		waitForMemberStatus(t, service, "bob", "idle")

		if err := service.Send("lead", "bob", "please review "+releaseNotesPath, "message"); err != nil {
			t.Fatalf("send review request: %v", err)
		}
		waitForMemberStatus(t, service, "bob", "idle")
		outcome = waitForInboxMessage(t, service, "lead")
		if !strings.Contains(outcome, "review-feedback::add rollback section") {
			t.Fatalf("unexpected review feedback: %q", outcome)
		}

		if err := service.Send("lead", "alice", "review-feedback::add rollback section", "message"); err != nil {
			t.Fatalf("forward feedback to alice: %v", err)
		}
		waitForMemberStatus(t, service, "alice", "idle")
		outcome = waitForInboxMessage(t, service, "lead")
		if !strings.Contains(outcome, "draft-updated::"+releaseNotesPath) {
			t.Fatalf("unexpected draft updated outcome: %q", outcome)
		}

		if err := service.Send("lead", "bob", "please review updated "+releaseNotesPath, "message"); err != nil {
			t.Fatalf("send re-review request: %v", err)
		}
		waitForMemberStatus(t, service, "bob", "idle")
		outcome = waitForInboxMessage(t, service, "lead")
		if !strings.Contains(outcome, "review-ok::"+releaseNotesPath) {
			t.Fatalf("unexpected review ok outcome: %q", outcome)
		}

		assertFileContent(t, releaseNotesPath, finalContent)
		assertFileContent(t, reviewResultPath, "review=passed")

		memberList, err := service.List()
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(memberList) != 2 {
			t.Fatalf("member count = %d, want 2", len(memberList))
		}
		for _, member := range memberList {
			if member.Status != "idle" {
				t.Fatalf("member %s status = %s, want idle", member.Name, member.Status)
			}
		}
	})
}

type toolCallSpec struct {
	ID        string
	Name      string
	Arguments string
}

type capturingMockHTTPClient struct {
	responses     []*http.Response
	callCount     int
	requestBodies [][]byte
}

func (m *capturingMockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if req != nil && req.Body != nil {
		body, _ := io.ReadAll(req.Body)
		m.requestBodies = append(m.requestBodies, body)
		_ = req.Body.Close()
	}
	index := m.callCount
	m.callCount++
	if index < len(m.responses) {
		return cloneHTTPResponse(m.responses[index]), nil
	}
	return makeHTTPStopResponse("(default stop)"), nil
}

func newCapturingMockClient(mock *capturingMockHTTPClient) *openai.Client {
	client := openai.NewClient(
		option.WithAPIKey("mock-key"),
		option.WithBaseURL("https://mock.example.com/v1/"),
		option.WithHTTPClient(mock),
		option.WithMaxRetries(0),
	)
	return &client
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

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := fixtureFS.ReadFile(name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(data)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func buildS09HandoffPrompt(basePrompt string, releaseNotesPath string, reviewResultPath string) string {
	replacements := map[string]string{
		"__RELEASE_NOTES_PATH__": releaseNotesPath,
		"__REVIEW_RESULT_PATH__": reviewResultPath,
	}
	prompt := basePrompt
	for placeholder, value := range replacements {
		prompt = strings.ReplaceAll(prompt, placeholder, value)
	}
	return prompt
}

func buildAliceDraftPrompt(releaseNotesPath string) string {
	return "Create the release notes draft at " + releaseNotesPath + " with the required initial content, then send lead the exact message draft-ready::" + releaseNotesPath + "."
}

func buildBobReviewPrompt(releaseNotesPath string, reviewResultPath string) string {
	return "Wait for lead to ask for a review. When the draft at " + releaseNotesPath + " is missing a rollback section, send lead review-feedback::add rollback section. After a second review request when the rollback section exists, write " + reviewResultPath + " with review=passed and send lead review-ok::" + releaseNotesPath + "."
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

func waitForInboxMessage(t *testing.T, service interface {
	DrainInbox(string) ([]team.Message, error)
}, recipient string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		messages, err := service.DrainInbox(recipient)
		if err != nil {
			t.Fatalf("DrainInbox(%s): %v", recipient, err)
		}
		for _, msg := range messages {
			return msg.Content
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for inbox message for %s", recipient)
	return ""
}

func waitForMockCalls(t *testing.T, mock *capturingMockHTTPClient, wantAtLeast int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mock.callCount >= wantAtLeast {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d mock calls, got %d", wantAtLeast, mock.callCount)
}

func waitForMemberStatus(t *testing.T, service interface {
	List() ([]team.Member, error)
}, name string, want team.Status) {
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

