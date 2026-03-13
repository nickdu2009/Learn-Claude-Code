package loop

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/background"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

func TestRunWithBackgroundNotifications_InjectsBeforeModelCall(t *testing.T) {
	mock := &backgroundMockHTTPClient{
		responses: []*http.Response{makeLoopHTTPStopResponse("done")},
	}
	client := newLoopMockClient(mock)
	runner := RunWithBackgroundNotifications(&stubNotificationSource{
		notifications: []background.Notification{
			{TaskID: "bg-1", Status: background.StatusCompleted, Summary: "tests passed"},
		},
	})

	_, err := runner(
		context.Background(),
		client,
		"mock-model",
		[]openai.ChatCompletionMessageParamUnion{openai.UserMessage("continue")},
		tools.New(),
	)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if len(mock.requestBodies) != 1 {
		t.Fatalf("request count = %d, want 1", len(mock.requestBodies))
	}
	body := string(mock.requestBodies[0])
	if !strings.Contains(body, "\\u003cbackground-results\\u003e") {
		t.Fatalf("expected injected background results, got %s", body)
	}
	if !strings.Contains(body, "tests passed") {
		t.Fatalf("expected notification summary, got %s", body)
	}
}

func TestRunWithBackgroundNotifications_DrainsOnlyOnce(t *testing.T) {
	source := &stubNotificationSource{
		notifications: []background.Notification{
			{TaskID: "bg-1", Status: background.StatusCompleted, Summary: "one-shot"},
		},
	}

	first := buildBackgroundNotificationMessages(source.DrainNotifications())
	second := buildBackgroundNotificationMessages(source.DrainNotifications())
	if len(first) == 0 {
		t.Fatal("expected first drain to produce messages")
	}
	if second != nil {
		t.Fatalf("expected second drain to be empty, got %d messages", len(second))
	}
}

func TestBuildBackgroundNotificationMessages_NoNotifications(t *testing.T) {
	messages := buildBackgroundNotificationMessages(nil)
	if messages != nil {
		t.Fatalf("expected nil messages, got %d", len(messages))
	}
}

type stubNotificationSource struct {
	notifications []background.Notification
}

func (s *stubNotificationSource) DrainNotifications() []background.Notification {
	out := append([]background.Notification(nil), s.notifications...)
	s.notifications = nil
	return out
}

type backgroundMockHTTPClient struct {
	responses     []*http.Response
	callCount     int
	requestBodies [][]byte
}

func (m *backgroundMockHTTPClient) Do(req *http.Request) (*http.Response, error) {
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
	return makeLoopHTTPStopResponse("(default stop)"), nil
}

func newLoopMockClient(mock *backgroundMockHTTPClient) *openai.Client {
	client := openai.NewClient(
		option.WithAPIKey("mock-key"),
		option.WithBaseURL("https://mock.example.com/v1/"),
		option.WithHTTPClient(mock),
		option.WithMaxRetries(0),
	)
	return &client
}

func makeLoopHTTPStopResponse(content string) *http.Response {
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
	}
	data, err := json.Marshal(raw)
	if err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(data)),
	}
}
