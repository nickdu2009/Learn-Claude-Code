package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

// SkillContentProvider returns full skill content by name.
type SkillContentProvider interface {
	Content(name string) (string, error)
}

// LoadSkillToolDef returns the definition for the load_skill tool.
func LoadSkillToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name: "load_skill",
			Description: openai.String(
				"Load specialized knowledge by skill name. Use before tackling unfamiliar topics or workflows.",
			),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "The exact skill name to load.",
					},
				},
				"required": []string{"name"},
			},
		},
	}
}

// NewLoadSkillHandler creates a tool handler that serves skill bodies by name.
func NewLoadSkillHandler(provider SkillContentProvider) Handler {
	return func(_ context.Context, args map[string]any) (string, error) {
		if provider == nil {
			return "", fmt.Errorf("skill provider is not configured")
		}

		name, ok := args["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return "", fmt.Errorf("missing or invalid 'name' argument")
		}

		name = strings.TrimSpace(name)
		if strings.Contains(name, "/") || strings.Contains(name, `\`) || strings.Contains(name, "..") {
			return "", fmt.Errorf("invalid skill name: %q", name)
		}

		return provider.Content(name)
	}
}
