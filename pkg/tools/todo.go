package tools

import (
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

// TodoToolDef returns the definition for the todo tool.
//
// The todo tool lets the model maintain a visible plan as structured state:
// items: [{id, text, status(pending|in_progress|completed)}]
func TodoToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "todo",
			Description: openai.String("Update task list. Track progress on multi-step tasks."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"items": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id": map[string]any{
									"type":        "string",
									"description": "Unique identifier for the todo item.",
								},
								"text": map[string]any{
									"type":        "string",
									"description": "What to do.",
								},
								"status": map[string]any{
									"type": "string",
									"enum": []string{"pending", "in_progress", "completed"},
								},
							},
							"required": []string{"id", "text", "status"},
						},
					},
				},
				"required": []string{"items"},
			},
		},
	}
}
