package tools

import (
	"context"
	"strings"
	"testing"
)

func TestNewTaskHandler_PassesPromptAndDescription(t *testing.T) {
	var (
		gotPrompt      string
		gotDescription string
	)

	handler := NewTaskHandler(func(_ context.Context, prompt string, description string) (string, error) {
		gotPrompt = prompt
		gotDescription = description
		return "summary", nil
	})

	result, err := handler(context.Background(), map[string]any{
		"prompt":      "inspect the project layout",
		"description": "explore repo",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "summary" {
		t.Fatalf("expected summary, got %q", result)
	}
	if gotPrompt != "inspect the project layout" {
		t.Fatalf("prompt mismatch: %q", gotPrompt)
	}
	if gotDescription != "explore repo" {
		t.Fatalf("description mismatch: %q", gotDescription)
	}
}

func TestNewTaskHandler_RequiresPrompt(t *testing.T) {
	handler := NewTaskHandler(func(_ context.Context, prompt string, description string) (string, error) {
		return "unexpected", nil
	})

	_, err := handler(context.Background(), map[string]any{
		"prompt": "   ",
	})
	if err == nil {
		t.Fatal("expected prompt validation error")
	}
	if !strings.Contains(err.Error(), "prompt") {
		t.Fatalf("expected prompt error, got %v", err)
	}
}
