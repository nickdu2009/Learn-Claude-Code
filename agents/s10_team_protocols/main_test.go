package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	pkgtools "github.com/nickdu2009/learn-claude-code/pkg/tools"
)

func TestBuildS10SystemPromptIncludesProtocolRules(t *testing.T) {
	prompt := buildS10SystemPrompt("/tmp/demo")

	checks := []string{
		"review important plans before execution",
		"shutdown_request",
		"shutdown_response",
	}
	for _, want := range checks {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt %q missing %q", prompt, want)
		}
	}
}

func TestS10RegistriesIncludeProtocolTools(t *testing.T) {
	sandboxDir := t.TempDir()
	service, err := newS10TeamService(context.Background(), nil, "", sandboxDir)
	if err != nil {
		t.Fatalf("newS10TeamService: %v", err)
	}

	leadTools := toolNames(newS10Registry(service))
	for _, want := range []string{"shutdown_request", "shutdown_response", "plan_approval"} {
		if !slices.Contains(leadTools, want) {
			t.Fatalf("lead registry missing %s in %v", want, leadTools)
		}
	}

	teammateTools := toolNames(newS10TeammateRegistry(service, "alice"))
	for _, want := range []string{"shutdown_response", "plan_approval"} {
		if !slices.Contains(teammateTools, want) {
			t.Fatalf("teammate registry missing %s in %v", want, teammateTools)
		}
	}
}

func toolNames(registry *pkgtools.Registry) []string {
	defs := registry.Definitions()
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Function.Name)
	}
	return names
}
