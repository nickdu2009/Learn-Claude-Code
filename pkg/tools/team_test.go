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

