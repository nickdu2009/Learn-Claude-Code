package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

// TaskRunner executes a delegated subtask and returns a summary for the parent.
type TaskRunner func(ctx context.Context, prompt string, description string) (string, error)

// TaskToolDef returns the definition for the task tool.
func TaskToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name: "task",
			Description: openai.String(
				"Spawn a subagent with fresh context. It shares the filesystem but not conversation history.",
			),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "The task instructions for the subagent.",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Short description of the delegated task.",
					},
				},
				"required": []string{"prompt"},
			},
		},
	}
}

// NewTaskHandler creates a tool handler that delegates work to a subagent runner.
func NewTaskHandler(runner TaskRunner) Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		if runner == nil {
			return "", fmt.Errorf("task runner is not configured")
		}

		prompt, ok := args["prompt"].(string)
		if !ok || strings.TrimSpace(prompt) == "" {
			return "", fmt.Errorf("missing or invalid 'prompt' argument")
		}

		description, _ := args["description"].(string)
		return runner(ctx, prompt, description)
	}
}
