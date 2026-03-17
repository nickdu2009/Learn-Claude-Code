package tools

import (
	"context"
	"strings"
	"testing"
)

func TestNewSendMessageHandler_DefaultsMessageType(t *testing.T) {
	var receivedType string
	handler := NewSendMessageHandler(func(_ context.Context, to, content, msgType string) (string, error) {
		receivedType = msgType
		return to + ":" + content, nil
	})

	output, err := handler(context.Background(), map[string]any{
		"to":      "alice",
		"content": "hello",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if output != "alice:hello" {
		t.Fatalf("output = %q, want %q", output, "alice:hello")
	}
	if receivedType != "message" {
		t.Fatalf("msgType = %q, want %q", receivedType, "message")
	}
}

func TestNewSpawnTeammateHandler_ValidatesArgs(t *testing.T) {
	handler := NewSpawnTeammateHandler(func(_ context.Context, name, role, prompt string) (string, error) {
		return name + role + prompt, nil
	})

	_, err := handler(context.Background(), map[string]any{
		"name": "alice",
		"role": "coder",
	})
	if err == nil || !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("expected prompt validation error, got %v", err)
	}
}

func TestNewShutdownRequestHandler_ValidatesArgs(t *testing.T) {
	handler := NewShutdownRequestHandler(func(_ context.Context, teammate string) (string, error) {
		return teammate, nil
	})

	_, err := handler(context.Background(), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "teammate") {
		t.Fatalf("expected teammate validation error, got %v", err)
	}
}

func TestNewShutdownResponseHandler_SupportsInspectAndRespondModes(t *testing.T) {
	var (
		gotRequestID string
		gotApprove   *bool
		gotReason    string
	)
	handler := NewShutdownResponseHandler(func(_ context.Context, requestID string, approve *bool, reason string) (string, error) {
		gotRequestID = requestID
		gotApprove = approve
		gotReason = reason
		return "ok", nil
	})

	output, err := handler(context.Background(), map[string]any{
		"request_id": "req-1",
	})
	if err != nil {
		t.Fatalf("inspect mode error: %v", err)
	}
	if output != "ok" || gotRequestID != "req-1" || gotApprove != nil || gotReason != "" {
		t.Fatalf("unexpected inspect mode values: output=%q requestID=%q approve=%v reason=%q", output, gotRequestID, gotApprove, gotReason)
	}

	output, err = handler(context.Background(), map[string]any{
		"request_id": "req-2",
		"approve":    false,
		"reason":     "still working",
	})
	if err != nil {
		t.Fatalf("respond mode error: %v", err)
	}
	if output != "ok" {
		t.Fatalf("output = %q, want ok", output)
	}
	if gotApprove == nil || *gotApprove {
		t.Fatalf("approve = %v, want false", gotApprove)
	}
	if gotReason != "still working" {
		t.Fatalf("reason = %q, want %q", gotReason, "still working")
	}
}

func TestNewShutdownResponseHandler_RejectsInvalidApproveType(t *testing.T) {
	handler := NewShutdownResponseHandler(func(_ context.Context, _ string, _ *bool, _ string) (string, error) {
		return "", nil
	})

	_, err := handler(context.Background(), map[string]any{
		"request_id": "req-1",
		"approve":    "yes",
	})
	if err == nil || !strings.Contains(err.Error(), "approve") {
		t.Fatalf("expected approve validation error, got %v", err)
	}
}

func TestNewPlanApprovalHandler_SupportsSubmitAndReviewModes(t *testing.T) {
	var (
		gotRequestID string
		gotApprove   *bool
		gotFeedback  string
		gotPlan      string
	)
	handler := NewPlanApprovalHandler(func(_ context.Context, requestID string, approve *bool, feedback string, plan string) (string, error) {
		gotRequestID = requestID
		gotApprove = approve
		gotFeedback = feedback
		gotPlan = plan
		return "ok", nil
	})

	output, err := handler(context.Background(), map[string]any{
		"plan": "include rollback and validation",
	})
	if err != nil {
		t.Fatalf("submit mode error: %v", err)
	}
	if output != "ok" {
		t.Fatalf("output = %q, want ok", output)
	}
	if gotPlan == "" || gotApprove != nil || gotRequestID != "" {
		t.Fatalf("unexpected submit mode values: requestID=%q approve=%v plan=%q", gotRequestID, gotApprove, gotPlan)
	}

	output, err = handler(context.Background(), map[string]any{
		"request_id": "req-2",
		"approve":    true,
		"feedback":   "looks good",
	})
	if err != nil {
		t.Fatalf("review mode error: %v", err)
	}
	if output != "ok" {
		t.Fatalf("output = %q, want ok", output)
	}
	if gotApprove == nil || !*gotApprove || gotRequestID != "req-2" || gotFeedback != "looks good" {
		t.Fatalf("unexpected review mode values: requestID=%q approve=%v feedback=%q", gotRequestID, gotApprove, gotFeedback)
	}
}

func TestNewPlanApprovalHandler_RejectsInvalidApproveType(t *testing.T) {
	handler := NewPlanApprovalHandler(func(_ context.Context, _ string, _ *bool, _ string, _ string) (string, error) {
		return "", nil
	})

	_, err := handler(context.Background(), map[string]any{
		"request_id": "req-1",
		"approve":    "true",
	})
	if err == nil || !strings.Contains(err.Error(), "approve") {
		t.Fatalf("expected approve validation error, got %v", err)
	}
}

