package main

import (
	"context"
	"strings"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/background"
)

func TestBuildS08SystemPrompt_FocusesOnBackgroundTasks(t *testing.T) {
	prompt := buildS08SystemPrompt("/tmp/workspace")

	for _, forbidden := range []string{"task_create", "task_update", "task_list", "task_get", "persistent task graph"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt should not mention %q: %s", forbidden, prompt)
		}
	}
	for _, forbidden := range []string{"background_run", "check_background"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt should not mention concrete tool name %q: %s", forbidden, prompt)
		}
	}
	for _, expected := range []string{
		"Prefer tools over prose",
		"background task tools",
		"keep making progress instead of waiting",
		"Treat background task updates as new information",
		"Avoid unnecessary status checks when there is no new signal",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt should mention %q: %s", expected, prompt)
		}
	}
}

func TestNewS08Registry_OnlyAddsBackgroundToolsBeyondBase(t *testing.T) {
	registry := newS08Registry(stubBackgroundToolService{})

	definitions := registry.Definitions()
	if len(definitions) != 6 {
		t.Fatalf("tool count = %d, want %d", len(definitions), 6)
	}

	toolNames := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		toolNames = append(toolNames, definition.Function.Name)
	}

	for _, expected := range []string{"bash", "read_file", "write_file", "edit_file", "background_run", "check_background"} {
		if !containsTool(toolNames, expected) {
			t.Fatalf("expected tool %q in registry, got %v", expected, toolNames)
		}
	}
	for _, forbidden := range []string{"list_dir", "task_create", "task_update", "task_list", "task_get"} {
		if containsTool(toolNames, forbidden) {
			t.Fatalf("unexpected task tool %q in registry: %v", forbidden, toolNames)
		}
	}
}

type stubBackgroundToolService struct{}

func (stubBackgroundToolService) Run(context.Context, string) (background.Task, error) {
	return background.Task{}, nil
}

func (stubBackgroundToolService) Check(string) (background.Task, error) {
	return background.Task{}, nil
}

func (stubBackgroundToolService) List() ([]background.Task, error) {
	return nil, nil
}
