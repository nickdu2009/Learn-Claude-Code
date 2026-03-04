package main

import "testing"

func TestTodoManager_RenderEmpty(t *testing.T) {
	m := NewTodoManager()
	if got := m.Render(); got != "No todos." {
		t.Fatalf("expected %q, got %q", "No todos.", got)
	}
}

func TestTodoManager_HandleTodo_ValidAndSingleInProgress(t *testing.T) {
	m := NewTodoManager()
	out, err := m.HandleTodo(map[string]any{
		"items": []any{
			map[string]any{"id": "1", "text": "task a", "status": "pending"},
			map[string]any{"id": "2", "text": "task b", "status": "in_progress"},
			map[string]any{"id": "3", "text": "task c", "status": "completed"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty render output")
	}
}

func TestTodoManager_HandleTodo_RejectMultipleInProgress(t *testing.T) {
	m := NewTodoManager()
	_, err := m.HandleTodo(map[string]any{
		"items": []any{
			map[string]any{"id": "1", "text": "task a", "status": "in_progress"},
			map[string]any{"id": "2", "text": "task b", "status": "in_progress"},
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestTodoManager_HandleTodo_DefaultsIDAndStatus(t *testing.T) {
	m := NewTodoManager()
	out, err := m.HandleTodo(map[string]any{
		"items": []any{
			map[string]any{"text": "task a"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty render output")
	}
}
