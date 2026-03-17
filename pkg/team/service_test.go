package team

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

func TestService_BroadcastExcludesSender(t *testing.T) {
	repo := &fakeMemberRepository{
		members: []Member{
			{Name: "alice", Role: "coder", Status: StatusIdle, UpdatedAt: time.Now().UTC()},
			{Name: "bob", Role: "tester", Status: StatusIdle, UpdatedAt: time.Now().UTC()},
		},
	}
	mailbox := newFakeMailbox()
	service, err := NewService(context.Background(), nil, "", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	count, err := service.Broadcast("alice", "status update")
	if err != nil {
		t.Fatalf("Broadcast: %v", err)
	}
	if count != 1 {
		t.Fatalf("broadcast count = %d, want 1", count)
	}

	messages, err := service.DrainInbox("bob")
	if err != nil {
		t.Fatalf("DrainInbox: %v", err)
	}
	if len(messages) != 1 || messages[0].From != "alice" {
		t.Fatalf("unexpected messages: %#v", messages)
	}
}

func TestService_SpawnRejectsWorkingMember(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newTestOpenAIClient(makeStopResponse("done"))
	repo := &fakeMemberRepository{}
	mailbox := newFakeMailbox()
	service, err := NewService(ctx, client, "mock-model", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.SetFactory(fakeAgentFactory{})

	if _, err := service.Spawn(context.Background(), "alice", "coder", "do work"); err != nil {
		t.Fatalf("first Spawn: %v", err)
	}
	if _, err := service.Spawn(context.Background(), "alice", "coder", "do work again"); err == nil {
		t.Fatal("expected duplicate working spawn error")
	}
}

func TestService_TeammateReturnsToIdleAfterRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := newTestOpenAIClient(makeStopResponse("done"))
	repo := &fakeMemberRepository{}
	mailbox := newFakeMailbox()
	service, err := NewService(ctx, client, "mock-model", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.SetFactory(fakeAgentFactory{})

	if _, err := service.Spawn(context.Background(), "alice", "coder", "do work"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitForStatus(t, service, "alice", StatusIdle)
}

func TestService_TeammateTraceFinishesAfterSingleRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	traceDir := t.TempDir()
	t.Setenv("AI_SDK_DEVTOOLS", "1")
	t.Setenv("AI_SDK_DEVTOOLS_DIR", traceDir)

	client := newTestOpenAIClient(makeStopResponse("done"))
	repo := &fakeMemberRepository{}
	mailbox := newFakeMailbox()
	service, err := NewService(ctx, client, "mock-model", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.SetFactory(fakeAgentFactory{})

	if _, err := service.Spawn(context.Background(), "alice", "coder", "do work"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitForStatus(t, service, "alice", StatusIdle)
	waitForFinishedTeammateRun(t, filepath.Join(traceDir, "generations.json"), "alice")
}

func TestService_ShutdownWaitsForTeammates(t *testing.T) {
	ctx := context.Background()

	client := newTestOpenAIClient(makeStopResponse("done"))
	repo := &fakeMemberRepository{}
	mailbox := newFakeMailbox()
	service, err := NewService(ctx, client, "mock-model", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.SetFactory(fakeAgentFactory{})

	if _, err := service.Spawn(context.Background(), "alice", "coder", "do work"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitForStatus(t, service, "alice", StatusIdle)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := service.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	waitForStatus(t, service, "alice", StatusShutdown)
}

func TestService_TeammateTraceTitlesIncludeSequenceAndWakeupReason(t *testing.T) {
	ctx := context.Background()

	traceDir := t.TempDir()
	t.Setenv("AI_SDK_DEVTOOLS", "1")
	t.Setenv("AI_SDK_DEVTOOLS_DIR", traceDir)

	client := newTestOpenAIClient(makeStopResponse("done"))
	repo := &fakeMemberRepository{}
	mailbox := newFakeMailbox()
	service, err := NewService(ctx, client, "mock-model", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.SetFactory(fakeAgentFactory{})

	if _, err := service.Spawn(context.Background(), "alice", "coder", "draft release notes for version 1.2.3"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	waitForStatus(t, service, "alice", StatusIdle)

	if err := service.Send("lead", "alice", "please add rollback notes", MessageTypeMessage); err != nil {
		t.Fatalf("Send: %v", err)
	}
	waitForStatus(t, service, "alice", StatusIdle)

	titles := waitForFinishedTeammateRunTitles(t, filepath.Join(traceDir, "generations.json"), "alice", 2)
	if !containsPrefix(titles, "teammate alice (coder) #1 - initial task:") {
		t.Fatalf("expected initial teammate run title with sequence, got %v", titles)
	}
	if !containsPrefix(titles, "teammate alice (coder) #2 - lead message: please add rollback notes") {
		t.Fatalf("expected wakeup teammate run title with message summary, got %v", titles)
	}
}

func TestService_RequestShutdownCreatesPendingTrackerAndInboxMessage(t *testing.T) {
	repo := &fakeMemberRepository{
		members: []Member{
			{Name: "alice", Role: "coder", Status: StatusIdle, UpdatedAt: time.Now().UTC()},
		},
	}
	mailbox := newFakeMailbox()
	service, err := NewService(context.Background(), nil, "", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	req, err := service.RequestShutdown("alice")
	if err != nil {
		t.Fatalf("RequestShutdown: %v", err)
	}
	if req.Target != "alice" || req.Status != RequestPending {
		t.Fatalf("unexpected shutdown request: %#v", req)
	}

	stored, err := service.CheckShutdownRequest(req.RequestID)
	if err != nil {
		t.Fatalf("CheckShutdownRequest: %v", err)
	}
	if stored.Status != RequestPending {
		t.Fatalf("stored status = %s, want %s", stored.Status, RequestPending)
	}

	messages, err := service.DrainInbox("alice")
	if err != nil {
		t.Fatalf("DrainInbox: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if messages[0].Type != MessageTypeShutdownRequest {
		t.Fatalf("message type = %q, want %q", messages[0].Type, MessageTypeShutdownRequest)
	}
	if messages[0].Extra["request_id"] != req.RequestID {
		t.Fatalf("request_id = %q, want %q", messages[0].Extra["request_id"], req.RequestID)
	}
}

func TestService_RespondShutdownApproveMarksTrackerApproved(t *testing.T) {
	repo := &fakeMemberRepository{
		members: []Member{
			{Name: "alice", Role: "coder", Status: StatusIdle, UpdatedAt: time.Now().UTC()},
		},
	}
	mailbox := newFakeMailbox()
	service, err := NewService(context.Background(), nil, "", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	req, err := service.RequestShutdown("alice")
	if err != nil {
		t.Fatalf("RequestShutdown: %v", err)
	}
	if _, err := service.DrainInbox("alice"); err != nil {
		t.Fatalf("DrainInbox alice: %v", err)
	}

	updated, err := service.RespondShutdown("alice", req.RequestID, true, "done")
	if err != nil {
		t.Fatalf("RespondShutdown: %v", err)
	}
	if updated.Status != RequestApproved {
		t.Fatalf("status = %s, want %s", updated.Status, RequestApproved)
	}

	messages, err := service.DrainInbox("lead")
	if err != nil {
		t.Fatalf("DrainInbox lead: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if messages[0].Type != MessageTypeShutdownResponse {
		t.Fatalf("message type = %q, want %q", messages[0].Type, MessageTypeShutdownResponse)
	}
	if messages[0].Extra["approve"] != "true" {
		t.Fatalf("approve = %q, want true", messages[0].Extra["approve"])
	}
}

func TestService_RespondShutdownRejectMarksTrackerRejected(t *testing.T) {
	repo := &fakeMemberRepository{
		members: []Member{
			{Name: "alice", Role: "coder", Status: StatusIdle, UpdatedAt: time.Now().UTC()},
		},
	}
	mailbox := newFakeMailbox()
	service, err := NewService(context.Background(), nil, "", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	req, err := service.RequestShutdown("alice")
	if err != nil {
		t.Fatalf("RequestShutdown: %v", err)
	}
	if _, err := service.DrainInbox("alice"); err != nil {
		t.Fatalf("DrainInbox alice: %v", err)
	}

	updated, err := service.RespondShutdown("alice", req.RequestID, false, "busy")
	if err != nil {
		t.Fatalf("RespondShutdown: %v", err)
	}
	if updated.Status != RequestRejected {
		t.Fatalf("status = %s, want %s", updated.Status, RequestRejected)
	}
}

func TestService_CheckShutdownRequestReturnsErrorForUnknownID(t *testing.T) {
	service, err := NewService(context.Background(), nil, "", &fakeMemberRepository{}, newFakeMailbox())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := service.CheckShutdownRequest("missing"); err == nil {
		t.Fatal("expected unknown shutdown request error")
	}
}

func TestService_SubmitPlanCreatesPendingTrackerAndNotifiesLead(t *testing.T) {
	repo := &fakeMemberRepository{
		members: []Member{
			{Name: "alice", Role: "coder", Status: StatusIdle, UpdatedAt: time.Now().UTC()},
		},
	}
	mailbox := newFakeMailbox()
	service, err := NewService(context.Background(), nil, "", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	req, err := service.SubmitPlan("alice", "add rollout and rollback")
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if req.Status != RequestPending {
		t.Fatalf("status = %s, want %s", req.Status, RequestPending)
	}

	requests := service.ListPlanRequests()
	if len(requests) != 1 || requests[0].RequestID != req.RequestID {
		t.Fatalf("unexpected plan requests: %#v", requests)
	}

	messages, err := service.DrainInbox("lead")
	if err != nil {
		t.Fatalf("DrainInbox lead: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(messages))
	}
	if messages[0].Type != MessageTypePlanApprovalResponse {
		t.Fatalf("message type = %q, want %q", messages[0].Type, MessageTypePlanApprovalResponse)
	}
	if messages[0].Extra["request_id"] != req.RequestID {
		t.Fatalf("request_id = %q, want %q", messages[0].Extra["request_id"], req.RequestID)
	}
}

func TestService_ReviewPlanApproveRejectAndUnknown(t *testing.T) {
	repo := &fakeMemberRepository{
		members: []Member{
			{Name: "alice", Role: "coder", Status: StatusIdle, UpdatedAt: time.Now().UTC()},
		},
	}
	mailbox := newFakeMailbox()
	service, err := NewService(context.Background(), nil, "", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	req, err := service.SubmitPlan("alice", "initial plan")
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if _, err := service.DrainInbox("lead"); err != nil {
		t.Fatalf("DrainInbox lead: %v", err)
	}

	rejected, err := service.ReviewPlan(req.RequestID, false, "add rollback")
	if err != nil {
		t.Fatalf("ReviewPlan reject: %v", err)
	}
	if rejected.Status != RequestRejected {
		t.Fatalf("status = %s, want %s", rejected.Status, RequestRejected)
	}

	messages, err := service.DrainInbox("alice")
	if err != nil {
		t.Fatalf("DrainInbox alice: %v", err)
	}
	if len(messages) != 1 || messages[0].Extra["approve"] != "false" {
		t.Fatalf("unexpected review reject messages: %#v", messages)
	}

	req, err = service.SubmitPlan("alice", "revised plan")
	if err != nil {
		t.Fatalf("SubmitPlan revised: %v", err)
	}
	if _, err := service.DrainInbox("lead"); err != nil {
		t.Fatalf("DrainInbox lead revised: %v", err)
	}

	approved, err := service.ReviewPlan(req.RequestID, true, "looks good")
	if err != nil {
		t.Fatalf("ReviewPlan approve: %v", err)
	}
	if approved.Status != RequestApproved {
		t.Fatalf("status = %s, want %s", approved.Status, RequestApproved)
	}

	if _, err := service.ReviewPlan("missing", true, ""); err == nil {
		t.Fatal("expected unknown plan request error")
	}
}

func TestService_TeammateApprovedShutdownTransitionsToShutdownAfterRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var requestID string
	client := newDynamicShutdownClient(&requestID)
	repo := &fakeMemberRepository{
		members: []Member{
			{Name: "alice", Role: "coder", Status: StatusIdle, UpdatedAt: time.Now().UTC()},
		},
	}
	mailbox := newFakeMailbox()
	service, err := NewService(ctx, client, "mock-model", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.SetFactory(protocolAwareFactory{
		build: func(member Member) (string, *tools.Registry, error) {
			registry := tools.New()
			registry.Register(
				openai.ChatCompletionToolParam{
					Type: "function",
					Function: shared.FunctionDefinitionParam{
						Name:        "shutdown_response",
						Description: openai.String("Respond to shutdown request"),
						Parameters: openai.FunctionParameters{
							"type": "object",
							"properties": map[string]any{
								"request_id": map[string]any{"type": "string"},
								"approve":    map[string]any{"type": "boolean"},
								"reason":     map[string]any{"type": "string"},
							},
							"required": []string{"request_id", "approve"},
						},
					},
				},
				func(_ context.Context, args map[string]any) (string, error) {
					reqID, _ := args["request_id"].(string)
					approve, _ := args["approve"].(bool)
					reason, _ := args["reason"].(string)
					req, err := service.RespondShutdown(member.Name, reqID, approve, reason)
					if err != nil {
						return "", err
					}
					return FormatShutdownRequest(req)
				},
			)
			return "You are " + member.Name, registry, nil
		},
	})

	req, err := service.RequestShutdown("alice")
	if err != nil {
		t.Fatalf("RequestShutdown: %v", err)
	}
	requestID = req.RequestID

	if _, err := service.Spawn(context.Background(), "alice", "coder", "finish any work, then handle shutdown"); err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	waitForStatus(t, service, "alice", StatusShutdown)
}

type fakeMemberRepository struct {
	mu      sync.Mutex
	members []Member
}

func (r *fakeMemberRepository) Load() ([]Member, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := append([]Member(nil), r.members...)
	return out, nil
}

func (r *fakeMemberRepository) SaveAll(members []Member) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.members = append([]Member(nil), members...)
	return nil
}

type fakeMailbox struct {
	mu      sync.Mutex
	inboxes map[string][]Message
}

func newFakeMailbox() *fakeMailbox {
	return &fakeMailbox{inboxes: make(map[string][]Message)}
}

func (m *fakeMailbox) Send(msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inboxes[msg.To] = append(m.inboxes[msg.To], msg)
	return nil
}

func (m *fakeMailbox) Drain(recipient string) ([]Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := append([]Message(nil), m.inboxes[recipient]...)
	delete(m.inboxes, recipient)
	return out, nil
}

type fakeAgentFactory struct{}

func (fakeAgentFactory) Build(member Member) (string, *tools.Registry, error) {
	return "You are " + member.Name, tools.New(), nil
}

type protocolAwareFactory struct {
	build func(member Member) (string, *tools.Registry, error)
}

func (f protocolAwareFactory) Build(member Member) (string, *tools.Registry, error) {
	return f.build(member)
}

type staticHTTPClient struct {
	response *http.Response
}

func (c *staticHTTPClient) Do(*http.Request) (*http.Response, error) {
	data, err := io.ReadAll(c.response.Body)
	if err != nil {
		return nil, err
	}
	c.response.Body = io.NopCloser(bytes.NewReader(data))
	return &http.Response{
		StatusCode: c.response.StatusCode,
		Header:     c.response.Header.Clone(),
		Body:       io.NopCloser(bytes.NewReader(data)),
	}, nil
}

func newTestOpenAIClient(response *http.Response) *openai.Client {
	client := openai.NewClient(
		option.WithAPIKey("mock-key"),
		option.WithBaseURL("https://mock.example.com/v1/"),
		option.WithHTTPClient(&staticHTTPClient{response: response}),
		option.WithMaxRetries(0),
	)
	return &client
}

type dynamicShutdownHTTPClient struct {
	requestID *string
	callCount int
}

func (c *dynamicShutdownHTTPClient) Do(*http.Request) (*http.Response, error) {
	c.callCount++
	if c.callCount == 1 {
		payload, err := json.Marshal(map[string]any{
			"request_id": strings.TrimSpace(derefString(c.requestID)),
			"approve":    true,
			"reason":     "work finished",
		})
		if err != nil {
			return nil, err
		}
		return makeToolCallResponse("shutdown_response", string(payload)), nil
	}
	return makeStopResponse("graceful shutdown complete"), nil
}

func newDynamicShutdownClient(requestID *string) *openai.Client {
	client := openai.NewClient(
		option.WithAPIKey("mock-key"),
		option.WithBaseURL("https://mock.example.com/v1/"),
		option.WithHTTPClient(&dynamicShutdownHTTPClient{requestID: requestID}),
		option.WithMaxRetries(0),
	)
	return &client
}

func makeStopResponse(content string) *http.Response {
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

func makeToolThenStopResponses(toolName string, args map[string]any, finalReply string) []*http.Response {
	payload, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return []*http.Response{
		makeToolCallResponse(toolName, string(payload)),
		makeStopResponse(finalReply),
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func makeToolCallResponse(toolName string, arguments string) *http.Response {
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
					"role":    "assistant",
					"content": "",
					"refusal": "",
					"tool_calls": []map[string]any{
						{
							"id":   "mock-tool-call",
							"type": "function",
							"function": map[string]any{
								"name":      toolName,
								"arguments": arguments,
							},
						},
					},
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}
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

func waitForStatus(t *testing.T, service *Service, name string, want Status) {
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

func waitForFinishedTeammateRun(t *testing.T, tracePath, teammate string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		titles := finishedTeammateRunTitles(t, tracePath, teammate)
		if len(titles) > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for teammate %s run to finish", teammate)
}

func waitForFinishedTeammateRunTitles(t *testing.T, tracePath, teammate string, want int) []string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		titles := finishedTeammateRunTitles(t, tracePath, teammate)
		if len(titles) >= want {
			return titles
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %d finished teammate runs for %s", want, teammate)
	return nil
}

func finishedTeammateRunTitles(t *testing.T, tracePath, teammate string) []string {
	t.Helper()

	type traceRun struct {
		Kind       string  `json:"kind"`
		Title      string  `json:"title"`
		Status     string  `json:"status"`
		FinishedAt *string `json:"finished_at"`
	}
	type traceFile struct {
		Runs []traceRun `json:"runs"`
	}

	data, err := os.ReadFile(tracePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("ReadFile(%s): %v", tracePath, err)
	}

	var trace traceFile
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("Unmarshal trace: %v", err)
	}

	prefix := "teammate " + teammate + " (coder)"
	var titles []string
	for _, run := range trace.Runs {
		if run.Kind != "teammate" || run.Status != "completed" || run.FinishedAt == nil || *run.FinishedAt == "" {
			continue
		}
		if strings.HasPrefix(run.Title, prefix) {
			titles = append(titles, run.Title)
		}
	}
	return titles
}

func containsPrefix(items []string, prefix string) bool {
	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			return true
		}
	}
	return false
}
