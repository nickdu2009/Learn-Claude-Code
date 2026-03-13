package tools

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/nickdu2009/learn-claude-code/pkg/background"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

type backgroundService interface {
	Run(ctx context.Context, command string) (background.Task, error)
	Check(taskID string) (background.Task, error)
	List() ([]background.Task, error)
}

func BackgroundRunToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "background_run",
			Description: openai.String("Start a long-running shell command asynchronously. Use this for installs, tests, builds, or other slow commands when you should keep working instead of waiting. Returns immediately with a task ID."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{
						"type":        "string",
						"description": "The shell command to run in the background as a single shell invocation.",
					},
				},
				"required": []string{"command"},
			},
		},
	}
}

func CheckBackgroundToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "check_background",
			Description: openai.String("Inspect background task progress or final output. Provide a task_id to get detailed status for one task, or omit it to list all background tasks."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "string",
						"description": "Optional background task ID. Omit this field to list all background tasks.",
					},
				},
			},
		},
	}
}

func NewBackgroundRunHandler(svc backgroundService) Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		if svc == nil {
			return "", fmt.Errorf("background service is not configured")
		}

		command, ok := args["command"].(string)
		if !ok || strings.TrimSpace(command) == "" {
			return "", fmt.Errorf("missing or invalid 'command' argument")
		}

		task, err := svc.Run(ctx, command)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Background task %s started: %s", task.ID, trimPreview(task.Command, 80)), nil
	}
}

func NewCheckBackgroundHandler(svc backgroundService) Handler {
	return func(_ context.Context, args map[string]any) (string, error) {
		if svc == nil {
			return "", fmt.Errorf("background service is not configured")
		}

		if rawTaskID, ok := args["task_id"].(string); ok && strings.TrimSpace(rawTaskID) != "" {
			task, err := svc.Check(rawTaskID)
			if err != nil {
				return "", err
			}
			return formatBackgroundTask(task), nil
		}

		taskList, err := svc.List()
		if err != nil {
			return "", err
		}
		if len(taskList) == 0 {
			return "No background tasks.", nil
		}

		slices.SortFunc(taskList, func(a, b background.Task) int {
			switch {
			case a.ID < b.ID:
				return -1
			case a.ID > b.ID:
				return 1
			default:
				return 0
			}
		})

		lines := make([]string, 0, len(taskList))
		for _, task := range taskList {
			lines = append(lines, fmt.Sprintf("%s: [%s] %s", task.ID, task.Status, trimPreview(task.Command, 60)))
		}
		return strings.Join(lines, "\n"), nil
	}
}

func formatBackgroundTask(task background.Task) string {
	result := task.Result
	if strings.TrimSpace(result) == "" && task.Status == background.StatusRunning {
		result = "(running)"
	}
	if strings.TrimSpace(result) == "" {
		result = "(no output)"
	}
	return fmt.Sprintf("[%s] %s\n%s", task.Status, trimPreview(task.Command, 60), result)
}

func trimPreview(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen]
}
