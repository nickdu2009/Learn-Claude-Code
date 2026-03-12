package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nickdu2009/learn-claude-code/pkg/tasks"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

func TaskCreateToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "task_create",
			Description: openai.String("Create a new task in the persistent task graph."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"subject": map[string]any{
						"type":        "string",
						"description": "Short task title.",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Optional task details.",
					},
				},
				"required": []string{"subject"},
			},
		},
	}
}

func TaskUpdateToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "task_update",
			Description: openai.String("Update task status, owner, or dependency edges."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "integer",
						"description": "Task ID to update.",
					},
					"status": map[string]any{
						"type": "string",
						"enum": []string{"pending", "in_progress", "completed"},
					},
					"owner": map[string]any{
						"type":        "string",
						"description": "Optional owner or assignee.",
					},
					"add_blocked_by": map[string]any{
						"type":        "array",
						"description": "Task IDs that must finish before this task is ready.",
						"items":       map[string]any{"type": "integer"},
					},
					"add_blocks": map[string]any{
						"type":        "array",
						"description": "Task IDs that depend on this task.",
						"items":       map[string]any{"type": "integer"},
					},
				},
				"required": []string{"task_id"},
			},
		},
	}
}

func TaskListToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "task_list",
			Description: openai.String("List all tasks in the persistent task graph."),
			Parameters: openai.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

func TaskGetToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "task_get",
			Description: openai.String("Get a single task from the persistent task graph."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "integer",
						"description": "Task ID to retrieve.",
					},
				},
				"required": []string{"task_id"},
			},
		},
	}
}

func NewTaskCreateHandler(svc *tasks.Service) Handler {
	return func(_ context.Context, args map[string]any) (string, error) {
		if svc == nil {
			return "", fmt.Errorf("task service is not configured")
		}

		subject, ok := args["subject"].(string)
		if !ok || strings.TrimSpace(subject) == "" {
			return "", fmt.Errorf("missing or invalid 'subject' argument")
		}
		description, _ := args["description"].(string)

		task, err := svc.CreateTask(subject, description)
		if err != nil {
			return "", err
		}

		return marshalPrettyJSON(task)
	}
}

func NewTaskUpdateHandler(svc *tasks.Service) Handler {
	return func(_ context.Context, args map[string]any) (string, error) {
		if svc == nil {
			return "", fmt.Errorf("task service is not configured")
		}

		taskID, err := intArg(args["task_id"])
		if err != nil {
			return "", fmt.Errorf("invalid 'task_id': %w", err)
		}

		input := tasks.UpdateTaskInput{ID: taskID}

		if rawStatus, ok := args["status"].(string); ok && strings.TrimSpace(rawStatus) != "" {
			status := tasks.Status(strings.TrimSpace(rawStatus))
			input.Status = &status
		}
		if rawOwner, ok := args["owner"].(string); ok {
			owner := rawOwner
			input.Owner = &owner
		}

		addBlockedBy, err := intSliceArg(args["add_blocked_by"])
		if err != nil {
			return "", fmt.Errorf("invalid 'add_blocked_by': %w", err)
		}
		addBlocks, err := intSliceArg(args["add_blocks"])
		if err != nil {
			return "", fmt.Errorf("invalid 'add_blocks': %w", err)
		}
		input.AddBlockedBy = addBlockedBy
		input.AddBlocks = addBlocks

		task, err := svc.UpdateTask(input)
		if err != nil {
			return "", err
		}

		return marshalPrettyJSON(task)
	}
}

func NewTaskListHandler(svc *tasks.Service) Handler {
	return func(_ context.Context, _ map[string]any) (string, error) {
		if svc == nil {
			return "", fmt.Errorf("task service is not configured")
		}

		taskList, err := svc.ListTasks()
		if err != nil {
			return "", err
		}

		return marshalPrettyJSON(taskList)
	}
}

func NewTaskGetHandler(svc *tasks.Service) Handler {
	return func(_ context.Context, args map[string]any) (string, error) {
		if svc == nil {
			return "", fmt.Errorf("task service is not configured")
		}

		taskID, err := intArg(args["task_id"])
		if err != nil {
			return "", fmt.Errorf("invalid 'task_id': %w", err)
		}

		task, err := svc.GetTask(taskID)
		if err != nil {
			return "", err
		}

		return marshalPrettyJSON(task)
	}
}

func marshalPrettyJSON(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func intArg(v any) (int, error) {
	switch value := v.(type) {
	case int:
		return value, nil
	case int32:
		return int(value), nil
	case int64:
		return int(value), nil
	case float64:
		return int(value), nil
	case float32:
		return int(value), nil
	default:
		return 0, fmt.Errorf("expected number")
	}
}

func intSliceArg(v any) ([]int, error) {
	if v == nil {
		return nil, nil
	}

	switch values := v.(type) {
	case []int:
		return append([]int(nil), values...), nil
	case []any:
		out := make([]int, 0, len(values))
		for _, item := range values {
			id, err := intArg(item)
			if err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected array")
	}
}
