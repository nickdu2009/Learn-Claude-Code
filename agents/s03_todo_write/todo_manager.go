package main

import (
	"fmt"
	"strconv"
	"strings"
)

type TodoStatus string

const (
	TodoStatusPending    TodoStatus = "pending"
	TodoStatusInProgress TodoStatus = "in_progress"
	TodoStatusCompleted  TodoStatus = "completed"
)

type TodoItem struct {
	ID     string
	Text   string
	Status TodoStatus
}

type TodoManager struct {
	items []TodoItem
}

func NewTodoManager() *TodoManager {
	return &TodoManager{items: []TodoItem{}}
}

func (m *TodoManager) HandleTodo(args map[string]any) (string, error) {
	rawItems, ok := args["items"]
	if !ok {
		return "", fmt.Errorf("missing 'items' argument")
	}
	arr, ok := rawItems.([]any)
	if !ok {
		return "", fmt.Errorf("invalid 'items' argument: expected array")
	}
	if len(arr) > 20 {
		return "", fmt.Errorf("max 20 todos allowed")
	}

	validated := make([]TodoItem, 0, len(arr))
	inProgressCount := 0

	for i, raw := range arr {
		obj, ok := raw.(map[string]any)
		if !ok {
			return "", fmt.Errorf("item %d: expected object", i+1)
		}

		id := strings.TrimSpace(toString(obj["id"]))
		if id == "" {
			id = strconv.Itoa(i + 1)
		}

		text := strings.TrimSpace(toString(obj["text"]))
		if text == "" {
			return "", fmt.Errorf("item %s: text required", id)
		}

		statusStr := strings.ToLower(strings.TrimSpace(toString(obj["status"])))
		if statusStr == "" {
			statusStr = string(TodoStatusPending)
		}
		status := TodoStatus(statusStr)
		switch status {
		case TodoStatusPending, TodoStatusInProgress, TodoStatusCompleted:
		default:
			return "", fmt.Errorf("item %s: invalid status %q", id, statusStr)
		}
		if status == TodoStatusInProgress {
			inProgressCount++
			if inProgressCount > 1 {
				return "", fmt.Errorf("only one task can be in_progress at a time")
			}
		}

		validated = append(validated, TodoItem{
			ID:     id,
			Text:   text,
			Status: status,
		})
	}

	m.items = validated
	return m.Render(), nil
}

func (m *TodoManager) Render() string {
	if len(m.items) == 0 {
		return "No todos."
	}

	lines := make([]string, 0, len(m.items)+2)
	done := 0

	for _, it := range m.items {
		var marker string
		switch it.Status {
		case TodoStatusPending:
			marker = "[ ]"
		case TodoStatusInProgress:
			marker = "[>]"
		case TodoStatusCompleted:
			marker = "[x]"
			done++
		default:
			marker = "[?]"
		}
		lines = append(lines, fmt.Sprintf("%s #%s: %s", marker, it.ID, it.Text))
	}

	lines = append(lines, fmt.Sprintf("\n(%d/%d completed)", done, len(m.items)))
	return strings.Join(lines, "\n")
}

func toString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
