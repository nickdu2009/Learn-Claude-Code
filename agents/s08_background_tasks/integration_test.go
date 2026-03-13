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
	"testing"
	"time"

	"github.com/nickdu2009/learn-claude-code/pkg/background"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

//go:embed testdata/*.md
var fixtureFS embed.FS

const (
	releaseEnvFilename                     = "release.env"
	featureFlagsFilename                   = "feature_flags.json"
	deployDocFilename                      = "DEPLOY.md"
	releaseValidationNotificationMaxLength = 500
)

const releaseEnvContent = "APP_ENV=production\nPORT=8080"

const featureFlagsContent = `{"enableBackgroundJobs": true, "enableMetrics": false}`

const deployDocContent = "# Deploy\n## Steps\n1. Run smoke test\n2. Deploy service\n## Rollback\n1. Restore previous image"

func TestIntegration_BackgroundTaskLifecycle(t *testing.T) {
	prompt := readFixture(t, "testdata/background_task_flow.md")
	sandboxDir := sandboxS08Dir(t, "fake")
	configPath := filepath.Join(sandboxDir, "config.txt")

	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{ID: "call-bg-run", Name: "background_run", Arguments: mustJSON(t, map[string]any{"command": "sleep 1; echo bg-finished"})},
				{ID: "call-write", Name: "write_file", Arguments: mustJSON(t, map[string]any{"path": configPath, "content": "configured"})},
			}),
			makeHTTPStopResponse("Started the background task and wrote config.txt while it runs."),
			makeHTTPStopResponse("The background task finished successfully and the config file is already in place."),
		},
	}

	withWorkingDir(t, sandboxDir, func() {
		history, requests, bgService := runS08FakeScenario(t, prompt, mock)

		toolNames := extractToolNames(history)
		for _, required := range []string{"background_run", "write_file"} {
			if !containsTool(toolNames, required) {
				t.Fatalf("expected tool %q in history, got %v", required, toolNames)
			}
		}

		if bgService.runCount != 1 {
			t.Fatalf("background run count = %d, want 1", bgService.runCount)
		}

		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("expected config.txt to be written: %v", err)
		}
		if string(data) != "configured" {
			t.Fatalf("config content = %q, want %q", string(data), "configured")
		}

		if len(requests) != 3 {
			t.Fatalf("request count = %d, want 3", len(requests))
		}
		assertS08RequestDoesNotExposeNonTutorialTools(t, string(requests[0]))
		if !strings.Contains(string(requests[2]), "\\u003cbackground-results\\u003e") {
			t.Fatalf("expected background notification injection, got %s", string(requests[2]))
		}
		if !strings.Contains(string(requests[2]), "bg-finished") {
			t.Fatalf("expected background summary in injected request, got %s", string(requests[2]))
		}

		finalReply := extractFinalReply(history)
		if !strings.Contains(strings.ToLower(finalReply), "background task finished") {
			t.Fatalf("final reply should mention background completion, got %q", finalReply)
		}
	})
}

func TestIntegration_BackgroundReleaseBundleValidation(t *testing.T) {
	basePrompt := readFixture(t, "testdata/release_bundle_validation.md")
	sandboxDir := sandboxS08Dir(t, "fake")
	releaseEnvPath := filepath.Join(sandboxDir, releaseEnvFilename)
	featureFlagsPath := filepath.Join(sandboxDir, featureFlagsFilename)
	deployDocPath := filepath.Join(sandboxDir, deployDocFilename)
	prompt := buildS08ReleaseBundlePrompt(basePrompt, releaseEnvPath, featureFlagsPath, deployDocPath)
	backgroundCommand := buildReleaseValidationCommand(releaseEnvPath, featureFlagsPath, deployDocPath)
	fullResult := buildReleaseValidationResult()
	summary := trimReleaseValidationSummary(fullResult)

	mock := &capturingMockHTTPClient{
		responses: []*http.Response{
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{ID: "call-bg-run", Name: "background_run", Arguments: mustJSON(t, map[string]any{"command": backgroundCommand})},
				{ID: "call-write-env", Name: "write_file", Arguments: mustJSON(t, map[string]any{"path": releaseEnvPath, "content": releaseEnvContent})},
				{ID: "call-write-flags", Name: "write_file", Arguments: mustJSON(t, map[string]any{"path": featureFlagsPath, "content": featureFlagsContent})},
				{ID: "call-write-doc", Name: "write_file", Arguments: mustJSON(t, map[string]any{"path": deployDocPath, "content": deployDocContent})},
			}),
			makeHTTPStopResponse("Started the background validation and created the release bundle files."),
			makeHTTPMultiToolCallResponse([]toolCallSpec{
				{ID: "call-check-bg", Name: "check_background", Arguments: mustJSON(t, map[string]any{"task_id": "bg-1"})},
			}),
			makeHTTPStopResponse("All three files were created. Release bundle ready. RESULT=passed FILES=3 PORT=8080 ROLLBACK_SECTION=yes BACKGROUND_JOBS=true"),
		},
	}

	withWorkingDir(t, sandboxDir, func() {
		history, requests, bgService := runS08FakeScenarioWithCompletion(
			t,
			prompt,
			mock,
			background.Notification{
				TaskID:  "bg-1",
				Command: backgroundCommand,
				Status:  background.StatusCompleted,
				Summary: summary,
			},
			fullResult,
		)

		toolNames := extractToolNames(history)
		for _, required := range []string{"background_run", "write_file", "check_background"} {
			if !containsTool(toolNames, required) {
				t.Fatalf("expected tool %q in history, got %v", required, toolNames)
			}
		}

		if bgService.runCount != 1 {
			t.Fatalf("background run count = %d, want 1", bgService.runCount)
		}

		assertFileContent(t, releaseEnvPath, releaseEnvContent)
		assertFileContent(t, featureFlagsPath, featureFlagsContent)
		assertFileContent(t, deployDocPath, deployDocContent)

		if len(requests) != 4 {
			t.Fatalf("request count = %d, want 4", len(requests))
		}
		assertS08RequestDoesNotExposeNonTutorialTools(t, string(requests[0]))
		if !strings.Contains(string(requests[2]), "\\u003cbackground-results\\u003e") {
			t.Fatalf("expected background notification injection, got %s", string(requests[2]))
		}
		if !strings.Contains(string(requests[3]), "RESULT=passed") ||
			!strings.Contains(string(requests[3]), "BACKGROUND_JOBS=true") {
			t.Fatalf("expected full background result after check_background, got %s", string(requests[3]))
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
	})
}

func runS08FakeScenario(
	t *testing.T,
	prompt string,
	mock *capturingMockHTTPClient,
) ([]openai.ChatCompletionMessageParamUnion, [][]byte, *fakeBackgroundService) {
	return runS08FakeScenarioWithCompletion(
		t,
		prompt,
		mock,
		background.Notification{
			TaskID:  "bg-1",
			Command: "sleep 1; echo bg-finished",
			Status:  background.StatusCompleted,
			Summary: "bg-finished",
		},
		"bg-finished",
	)
}

func runS08FakeScenarioWithCompletion(
	t *testing.T,
	prompt string,
	mock *capturingMockHTTPClient,
	completion background.Notification,
	fullResult string,
) ([]openai.ChatCompletionMessageParamUnion, [][]byte, *fakeBackgroundService) {
	t.Helper()

	client := newCapturingMockClient(mock)
	bgService := newFakeBackgroundService()

	registry := newS08Registry(bgService)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	runner := loop.RunWithBackgroundNotifications(bgService)
	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(buildS08SystemPrompt(cwd)),
		openai.UserMessage(prompt),
	}

	history, err = runner(context.Background(), client, "mock-model", history, registry)
	if err != nil {
		t.Fatalf("runner first pass: %v", err)
	}

	bgService.complete(completion, fullResult)

	history, err = runner(context.Background(), client, "mock-model", history, registry)
	if err != nil {
		t.Fatalf("runner second pass: %v", err)
	}

	return history, mock.requestBodies, bgService
}

type fakeBackgroundService struct {
	runCount      int
	nextID        int
	tasks         map[string]background.Task
	notifications []background.Notification
}

func newFakeBackgroundService() *fakeBackgroundService {
	return &fakeBackgroundService{
		tasks: make(map[string]background.Task),
	}
}

func (f *fakeBackgroundService) Run(_ context.Context, command string) (background.Task, error) {
	f.runCount++
	f.nextID++
	task := background.Task{
		ID:        fmt.Sprintf("bg-%d", f.nextID),
		Command:   command,
		Status:    background.StatusRunning,
		StartedAt: time.Now().UTC(),
	}
	f.tasks[task.ID] = task
	return task, nil
}

func (f *fakeBackgroundService) Check(taskID string) (background.Task, error) {
	task, ok := f.tasks[taskID]
	if !ok {
		return background.Task{}, fmt.Errorf("task %s not found", taskID)
	}
	return task, nil
}

func (f *fakeBackgroundService) List() ([]background.Task, error) {
	taskList := make([]background.Task, 0, len(f.tasks))
	for _, task := range f.tasks {
		taskList = append(taskList, task)
	}
	return taskList, nil
}

func (f *fakeBackgroundService) DrainNotifications() []background.Notification {
	out := append([]background.Notification(nil), f.notifications...)
	f.notifications = nil
	for _, notification := range out {
		task := f.tasks[notification.TaskID]
		task.Status = notification.Status
		if strings.TrimSpace(task.Result) == "" {
			task.Result = notification.Summary
		}
		now := time.Now().UTC()
		task.FinishedAt = &now
		f.tasks[notification.TaskID] = task
	}
	return out
}

func (f *fakeBackgroundService) enqueue(notification background.Notification) {
	f.notifications = append(f.notifications, notification)
}

func (f *fakeBackgroundService) complete(notification background.Notification, fullResult string) {
	task := f.tasks[notification.TaskID]
	task.Status = notification.Status
	task.Result = fullResult
	now := time.Now().UTC()
	task.FinishedAt = &now
	f.tasks[notification.TaskID] = task
	f.enqueue(notification)
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
		return m.responses[index], nil
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
	raw := map[string]any{
		"id":      "mock-id",
		"object":  "chat.completion",
		"created": 0,
		"model":   "mock-model",
		"choices": []map[string]any{
			{
				"index":         0,
				"finish_reason": "stop",
				"logprobs":      nil,
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
	return marshalToHTTPResponse(raw)
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

	raw := map[string]any{
		"id":      "mock-id",
		"object":  "chat.completion",
		"created": 0,
		"model":   "mock-model",
		"choices": []map[string]any{
			{
				"index":         0,
				"finish_reason": "tool_calls",
				"logprobs":      nil,
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
	return marshalToHTTPResponse(raw)
}

func marshalToHTTPResponse(body map[string]any) *http.Response {
	data, err := json.Marshal(body)
	if err != nil {
		panic("marshalToHTTPResponse: " + err.Error())
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}

func readFixture(t *testing.T, name string) string {
	t.Helper()

	data, err := fixtureFS.ReadFile(name)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

func buildS08ReleaseBundlePrompt(basePrompt string, releaseEnvPath string, featureFlagsPath string, deployDocPath string) string {
	replacements := map[string]string{
		"__RELEASE_ENV_PATH__":   releaseEnvPath,
		"__FEATURE_FLAGS_PATH__": featureFlagsPath,
		"__DEPLOY_DOC_PATH__":    deployDocPath,
		"__BACKGROUND_COMMAND__": buildReleaseValidationCommand(releaseEnvPath, featureFlagsPath, deployDocPath),
	}

	prompt := basePrompt
	for placeholder, value := range replacements {
		prompt = strings.ReplaceAll(prompt, placeholder, value)
	}
	return prompt
}

func buildReleaseValidationCommand(releaseEnvPath string, featureFlagsPath string, deployDocPath string) string {
	return strings.Join([]string{
		fmt.Sprintf("for i in $(seq 1 25); do [ -f %s ] && [ -f %s ] && [ -f %s ] && break; sleep 0.2; done", shellQuote(releaseEnvPath), shellQuote(featureFlagsPath), shellQuote(deployDocPath)),
		fmt.Sprintf("test -f %s || { echo 'RESULT=failed'; echo 'ERROR=missing release.env'; exit 1; }", shellQuote(releaseEnvPath)),
		fmt.Sprintf("test -f %s || { echo 'RESULT=failed'; echo 'ERROR=missing feature_flags.json'; exit 1; }", shellQuote(featureFlagsPath)),
		fmt.Sprintf("test -f %s || { echo 'RESULT=failed'; echo 'ERROR=missing DEPLOY.md'; exit 1; }", shellQuote(deployDocPath)),
		fmt.Sprintf("grep -q '^APP_ENV=production$' %s || { echo 'RESULT=failed'; echo 'ERROR=bad APP_ENV'; exit 1; }", shellQuote(releaseEnvPath)),
		fmt.Sprintf("grep -q '^PORT=8080$' %s || { echo 'RESULT=failed'; echo 'ERROR=bad PORT'; exit 1; }", shellQuote(releaseEnvPath)),
		fmt.Sprintf("grep -Fq '\"enableBackgroundJobs\": true' %s || { echo 'RESULT=failed'; echo 'ERROR=missing enableBackgroundJobs'; exit 1; }", shellQuote(featureFlagsPath)),
		fmt.Sprintf("grep -q '^## Rollback$' %s || { echo 'RESULT=failed'; echo 'ERROR=missing rollback section'; exit 1; }", shellQuote(deployDocPath)),
		"padding=$(printf 'pad%03d' $(seq 1 180))",
		"echo \"$padding\"",
		"echo 'RESULT=passed'",
		"echo 'FILES=3'",
		"echo 'PORT=8080'",
		"echo 'ROLLBACK_SECTION=yes'",
		"echo 'BACKGROUND_JOBS=true'",
	}, "; ")
}

func buildReleaseValidationResult() string {
	paddingParts := make([]string, 0, 180)
	for i := 1; i <= 180; i++ {
		paddingParts = append(paddingParts, fmt.Sprintf("pad%03d", i))
	}
	return strings.Join([]string{
		strings.Join(paddingParts, ""),
		"RESULT=passed",
		"FILES=3",
		"PORT=8080",
		"ROLLBACK_SECTION=yes",
		"BACKGROUND_JOBS=true",
	}, "\n")
}

func trimReleaseValidationSummary(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= releaseValidationNotificationMaxLength {
		return value
	}
	return value[:releaseValidationNotificationMaxLength]
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func mustJSON(t *testing.T, value map[string]any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return string(data)
}

func assertFileContent(t *testing.T, path string, want string) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file %s to be written: %v", path, err)
	}
	if string(data) != want {
		t.Fatalf("file %s content = %q, want %q", path, string(data), want)
	}
}

func sandboxS08Dir(t *testing.T, kind string) string {
	t.Helper()

	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s08", kind, t.Name(), runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create sandbox dir %s: %v", dir, err)
	}
	return dir
}

func extractToolNames(messages []openai.ChatCompletionMessageParamUnion) []string {
	seen := make(map[string]bool)
	var names []string
	for _, msg := range messages {
		if msg.OfAssistant == nil {
			continue
		}
		for _, tc := range msg.OfAssistant.ToolCalls {
			if !seen[tc.Function.Name] {
				seen[tc.Function.Name] = true
				names = append(names, tc.Function.Name)
			}
		}
	}
	return names
}

func containsTool(names []string, target string) bool {
	for _, name := range names {
		if name == target {
			return true
		}
	}
	return false
}

func assertS08RequestDoesNotExposeNonTutorialTools(t *testing.T, body string) {
	t.Helper()

	for _, forbiddenToolName := range []string{"list_dir", "task_create", "task_update", "task_list", "task_get"} {
		if strings.Contains(body, forbiddenToolName) {
			t.Fatalf("unexpected tool %q exposed in s08 request: %s", forbiddenToolName, body)
		}
	}
}

func extractFinalReply(messages []openai.ChatCompletionMessageParamUnion) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.OfAssistant == nil {
			continue
		}
		if msg.OfAssistant.Content.OfString.Value != "" {
			return msg.OfAssistant.Content.OfString.Value
		}
		for _, part := range msg.OfAssistant.Content.OfArrayOfContentParts {
			if part.OfText != nil && part.OfText.Text != "" {
				return part.OfText.Text
			}
		}
	}
	return ""
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	}()

	fn()
}
