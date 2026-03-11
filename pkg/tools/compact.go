package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

const maxCompactFocusLength = 500

// CompactToolDef returns the definition for the compact tool.
func CompactToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name: "compact",
			Description: openai.String(
				"Compress the conversation history when context is getting large or the task is switching phases.",
			),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"focus": map[string]any{
						"type":        "string",
						"description": "Optional guidance for what the summary should preserve for the next phase.",
						"maxLength":   maxCompactFocusLength,
					},
				},
			},
		},
	}
}

// CompactFocusFromArgs validates and normalizes the optional compact focus.
func CompactFocusFromArgs(args map[string]any) (string, error) {
	raw, ok := args["focus"]
	if !ok || raw == nil {
		return "", nil
	}

	focus, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("invalid 'focus' argument")
	}

	focus = strings.TrimSpace(focus)
	if len(focus) > maxCompactFocusLength {
		return "", fmt.Errorf("'focus' is too long (max %d characters)", maxCompactFocusLength)
	}
	return focus, nil
}

// NewCompactHandler creates a tool handler that acknowledges manual compaction.
func NewCompactHandler() Handler {
	return func(_ context.Context, args map[string]any) (string, error) {
		focus, err := CompactFocusFromArgs(args)
		if err != nil {
			return "", err
		}
		if focus == "" {
			return "Manual compaction requested.", nil
		}
		return fmt.Sprintf("Manual compaction requested. Preserve focus: %s", focus), nil
	}
}
