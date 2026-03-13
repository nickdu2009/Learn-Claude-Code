package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nickdu2009/learn-claude-code/pkg/background"
)

func TestNewBackgroundRunHandler_StartsTask(t *testing.T) {
	service := &stubBackgroundService{
		runTask: background.Task{
			ID:      "bg-1",
			Command: "sleep 1 && echo done",
			Status:  background.StatusRunning,
		},
	}
	handler := NewBackgroundRunHandler(service)

	result, err := handler(context.Background(), map[string]any{"command": "sleep 1 && echo done"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if service.runCommand != "sleep 1 && echo done" {
		t.Fatalf("run command = %q", service.runCommand)
	}
	if !strings.Contains(result, "Background task bg-1 started") {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestNewBackgroundRunHandler_RequiresCommand(t *testing.T) {
	handler := NewBackgroundRunHandler(&stubBackgroundService{})

	_, err := handler(context.Background(), map[string]any{"command": "   "})
	if err == nil {
		t.Fatal("expected command validation error")
	}
	if !strings.Contains(err.Error(), "command") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewBackgroundRunHandler_PropagatesError(t *testing.T) {
	expectedErr := fmt.Errorf("run failed")
	handler := NewBackgroundRunHandler(&stubBackgroundService{runErr: expectedErr})

	_, err := handler(context.Background(), map[string]any{"command": "echo fail"})
	if err != expectedErr {
		t.Fatalf("got err %v, want %v", err, expectedErr)
	}
}

func TestNewCheckBackgroundHandler_ReturnsSingleTask(t *testing.T) {
	handler := NewCheckBackgroundHandler(&stubBackgroundService{
		checkTask: background.Task{
			ID:      "bg-2",
			Command: "pytest -q",
			Status:  background.StatusCompleted,
			Result:  "ok",
		},
	})

	result, err := handler(context.Background(), map[string]any{"task_id": "bg-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "[completed] pytest -q") {
		t.Fatalf("unexpected result: %q", result)
	}
	if !strings.Contains(result, "ok") {
		t.Fatalf("expected result body, got %q", result)
	}
}

func TestNewCheckBackgroundHandler_ListsAllTasks(t *testing.T) {
	handler := NewCheckBackgroundHandler(&stubBackgroundService{
		listTasks: []background.Task{
			{ID: "bg-2", Command: "sleep 2", Status: background.StatusRunning},
			{ID: "bg-1", Command: "echo done", Status: background.StatusCompleted},
		},
	})

	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Index(result, "bg-1") > strings.Index(result, "bg-2") {
		t.Fatalf("expected sorted task list, got %q", result)
	}
}

func TestNewCheckBackgroundHandler_EmptyList(t *testing.T) {
	handler := NewCheckBackgroundHandler(&stubBackgroundService{})

	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "No background tasks." {
		t.Fatalf("result = %q, want %q", result, "No background tasks.")
	}
}

type stubBackgroundService struct {
	runTask    background.Task
	runErr     error
	runCommand string
	checkTask  background.Task
	checkErr   error
	listTasks  []background.Task
	listErr    error
}

func (s *stubBackgroundService) Run(_ context.Context, command string) (background.Task, error) {
	s.runCommand = command
	if s.runErr != nil {
		return background.Task{}, s.runErr
	}
	return s.runTask, nil
}

func (s *stubBackgroundService) Check(taskID string) (background.Task, error) {
	if s.checkErr != nil {
		return background.Task{}, s.checkErr
	}
	if s.checkTask.ID == "" {
		return background.Task{}, fmt.Errorf("task %s not found", taskID)
	}
	return s.checkTask, nil
}

func (s *stubBackgroundService) List() ([]background.Task, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.listTasks, nil
}
