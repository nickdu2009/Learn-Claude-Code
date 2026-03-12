package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/tasks"
)

func TestNewTaskCreateHandler_CreatesTask(t *testing.T) {
	handler := NewTaskCreateHandler(newTaskService(t))

	result, err := handler(context.Background(), map[string]any{
		"subject":     "parse",
		"description": "read input",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"subject": "parse"`) {
		t.Fatalf("expected created task json, got %s", result)
	}
}

func TestNewTaskCreateHandler_RequiresSubject(t *testing.T) {
	handler := NewTaskCreateHandler(newTaskService(t))

	_, err := handler(context.Background(), map[string]any{
		"subject": "   ",
	})
	if err == nil {
		t.Fatal("expected subject validation error")
	}
	if !strings.Contains(err.Error(), "subject") {
		t.Fatalf("expected subject error, got %v", err)
	}
}

func TestNewTaskUpdateHandler_UpdatesDependenciesAndStatus(t *testing.T) {
	svc := newTaskService(t)
	parseTask, err := svc.CreateTask("parse", "")
	if err != nil {
		t.Fatalf("CreateTask(parse): %v", err)
	}
	transformTask, err := svc.CreateTask("transform", "")
	if err != nil {
		t.Fatalf("CreateTask(transform): %v", err)
	}

	handler := NewTaskUpdateHandler(svc)
	result, err := handler(context.Background(), map[string]any{
		"task_id":        transformTask.ID,
		"status":         "in_progress",
		"add_blocked_by": []any{float64(parseTask.ID)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, `"status": "in_progress"`) {
		t.Fatalf("expected updated status in result, got %s", result)
	}
	if !strings.Contains(result, `"blockedBy": [`) {
		t.Fatalf("expected blockedBy in result, got %s", result)
	}
}

func TestNewTaskGetAndListHandlers_ReturnStoredTasks(t *testing.T) {
	svc := newTaskService(t)
	parseTask, err := svc.CreateTask("parse", "")
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	getHandler := NewTaskGetHandler(svc)
	getResult, err := getHandler(context.Background(), map[string]any{"task_id": parseTask.ID})
	if err != nil {
		t.Fatalf("unexpected get error: %v", err)
	}
	if !strings.Contains(getResult, `"subject": "parse"`) {
		t.Fatalf("expected parse task in get result, got %s", getResult)
	}

	listHandler := NewTaskListHandler(svc)
	listResult, err := listHandler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if !strings.Contains(listResult, `"subject": "parse"`) {
		t.Fatalf("expected parse task in list result, got %s", listResult)
	}
}

func newTaskService(t *testing.T) *tasks.Service {
	t.Helper()

	repo, err := tasks.NewFileRepository(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileRepository: %v", err)
	}
	return tasks.NewService(repo)
}
