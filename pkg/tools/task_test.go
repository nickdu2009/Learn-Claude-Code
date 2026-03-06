package tools

import (
	"context"
	"fmt"
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

func TestNewTaskHandler_DefaultsDescriptionFromPrompt(t *testing.T) {
	var gotDescription string

	handler := NewTaskHandler(func(_ context.Context, prompt string, description string) (string, error) {
		gotDescription = description
		return "summary", nil
	})

	_, err := handler(context.Background(), map[string]any{
		"prompt": "inspect the repository structure and summarize the important folders",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotDescription == "" {
		t.Fatal("expected default description")
	}
	if !strings.Contains(gotDescription, "inspect the repository structure") {
		t.Fatalf("unexpected default description: %q", gotDescription)
	}
}

func TestNewTaskHandler_PropagatesRunnerError(t *testing.T) {
	expectedErr := fmt.Errorf("subagent failed")
	handler := NewTaskHandler(func(_ context.Context, prompt string, description string) (string, error) {
		return "", expectedErr
	})

	_, err := handler(context.Background(), map[string]any{
		"prompt": "investigate the error",
	})
	if err == nil {
		t.Fatal("expected runner error")
	}
	if err != expectedErr {
		t.Fatalf("got err %v, want %v", err, expectedErr)
	}
}
