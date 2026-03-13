package main

import (
	"context"
	"strings"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/team"
)

func TestBuildS09SystemPrompt_FocusesOnAgentTeams(t *testing.T) {
	prompt := buildS09SystemPrompt("/tmp/workspace")

	for _, forbidden := range []string{
		"task_create", "task_update", "task_list", "task_get",
		"background_run", "check_background",
		"spawn_teammate", "list_teammates", "send_message", "read_inbox", "broadcast",
	} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt should not mention %q: %s", forbidden, prompt)
		}
	}
	for _, expected := range []string{
		"Prefer tools over prose",
		"delegate focused work to persistent teammates",
		"Treat inbox updates from teammates as new information",
		"avoid unnecessary inbox checks when there is no new signal",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("prompt should mention %q: %s", expected, prompt)
		}
	}
}

func TestNewS09Registry_MatchesOriginalTutorialToolList(t *testing.T) {
	service := newStubTeamService(t)
	registry := newS09Registry(service)

	definitions := registry.Definitions()
	if len(definitions) != 9 {
		t.Fatalf("tool count = %d, want 9", len(definitions))
	}

	toolNames := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		toolNames = append(toolNames, definition.Function.Name)
	}

	for _, expected := range []string{
		"bash", "read_file", "write_file", "edit_file",
		"spawn_teammate", "list_teammates", "send_message", "read_inbox", "broadcast",
	} {
		if !containsTool(toolNames, expected) {
			t.Fatalf("expected tool %q in registry, got %v", expected, toolNames)
		}
	}
	for _, forbidden := range []string{"list_dir", "load_skill", "task", "background_run", "check_background"} {
		if containsTool(toolNames, forbidden) {
			t.Fatalf("unexpected tool %q in registry: %v", forbidden, toolNames)
		}
	}
}

func TestNewTeammateRegistry_MatchesOriginalTutorialToolList(t *testing.T) {
	service := newStubTeamService(t)
	registry := newTeammateRegistry(service, "alice")

	definitions := registry.Definitions()
	if len(definitions) != 6 {
		t.Fatalf("tool count = %d, want 6", len(definitions))
	}

	toolNames := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		toolNames = append(toolNames, definition.Function.Name)
	}
	for _, expected := range []string{
		"bash", "read_file", "write_file", "edit_file", "send_message", "read_inbox",
	} {
		if !containsTool(toolNames, expected) {
			t.Fatalf("expected tool %q in registry, got %v", expected, toolNames)
		}
	}
	for _, forbidden := range []string{"list_dir", "broadcast", "spawn_teammate", "list_teammates"} {
		if containsTool(toolNames, forbidden) {
			t.Fatalf("unexpected tool %q in teammate registry: %v", forbidden, toolNames)
		}
	}
}

func newStubTeamService(t *testing.T) *team.Service {
	t.Helper()

	repo, err := team.NewFileRepository(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}
	mailbox, err := team.NewFileMailbox(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileMailbox: %v", err)
	}
	service, err := team.NewService(context.Background(), nil, "", repo, mailbox)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func containsTool(toolNames []string, want string) bool {
	for _, name := range toolNames {
		if name == want {
			return true
		}
	}
	return false
}

