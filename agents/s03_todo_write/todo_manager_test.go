package main

import (
	"context"
	"strings"
	"testing"
)

func TestTodoManager_RenderEmpty(t *testing.T) {
	m := NewTodoManager()
	if got := m.Render(); got != "No todos." {
		t.Fatalf("expected %q, got %q", "No todos.", got)
	}
}

// UT-02: 验证正常单 in_progress 场景下 Render 输出格式精确正确。
func TestTodoManager_HandleTodo_ValidAndSingleInProgress(t *testing.T) {
	m := NewTodoManager()
	out, err := m.HandleTodo(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"id": "1", "text": "task a", "status": "pending"},
			map[string]any{"id": "2", "text": "task b", "status": "in_progress"},
			map[string]any{"id": "3", "text": "task c", "status": "completed"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"[ ] #1: task a", "[>] #2: task b", "[x] #3: task c", "(1/3 completed)"} {
		if !strings.Contains(out, want) {
			t.Errorf("render output missing %q\ngot:\n%s", want, out)
		}
	}
}

func TestTodoManager_HandleTodo_RejectMultipleInProgress(t *testing.T) {
	m := NewTodoManager()
	_, err := m.HandleTodo(context.Background(), map[string]any{
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
	out, err := m.HandleTodo(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"text": "task a"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 默认 ID 应为 "1"，默认 status 为 pending
	if !strings.Contains(out, "[ ] #1: task a") {
		t.Errorf("expected default id=1 and pending marker, got:\n%s", out)
	}
}

// UT-05: 超过 20 条限制应返回错误。
func TestTodoManager_HandleTodo_ExceedsMaxItems(t *testing.T) {
	m := NewTodoManager()
	items := make([]any, 21)
	for i := range items {
		items[i] = map[string]any{"text": "task", "status": "pending"}
	}
	_, err := m.HandleTodo(context.Background(), map[string]any{"items": items})
	if err == nil {
		t.Fatal("expected error for >20 items, got nil")
	}
	if !strings.Contains(err.Error(), "max 20") {
		t.Errorf("expected 'max 20' in error, got: %v", err)
	}
}

// UT-06: text 为空应返回错误。
func TestTodoManager_HandleTodo_EmptyText(t *testing.T) {
	m := NewTodoManager()
	_, err := m.HandleTodo(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"id": "1", "text": "", "status": "pending"},
		},
	})
	if err == nil {
		t.Fatal("expected error for empty text, got nil")
	}
}

// UT-07: 无效 status 应返回错误且包含 "invalid status"。
func TestTodoManager_HandleTodo_InvalidStatus(t *testing.T) {
	m := NewTodoManager()
	_, err := m.HandleTodo(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"id": "1", "text": "task", "status": "unknown"},
		},
	})
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Errorf("expected 'invalid status' in error, got: %v", err)
	}
}

// UT-08: 第二次调用应全量替换第一次的结果，而非追加。
func TestTodoManager_HandleTodo_FullReplacement(t *testing.T) {
	m := NewTodoManager()
	_, err := m.HandleTodo(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"id": "1", "text": "task a", "status": "pending"},
			map[string]any{"id": "2", "text": "task b", "status": "pending"},
			map[string]any{"id": "3", "text": "task c", "status": "pending"},
		},
	})
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if len(m.items) != 3 {
		t.Fatalf("expected 3 items after first call, got %d", len(m.items))
	}

	_, err = m.HandleTodo(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"id": "1", "text": "only task", "status": "pending"},
		},
	})
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	// 全量替换：第二次调用后只剩 1 条
	if len(m.items) != 1 {
		t.Errorf("expected 1 item after full replacement, got %d", len(m.items))
	}
}

// UT-09: 缺少 items 参数应返回 "missing 'items'" 错误。
func TestTodoManager_HandleTodo_MissingItems(t *testing.T) {
	m := NewTodoManager()
	_, err := m.HandleTodo(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing items, got nil")
	}
	if !strings.Contains(err.Error(), "missing 'items'") {
		t.Errorf("expected \"missing 'items'\" in error, got: %v", err)
	}
}

// UT-10: items 为非数组类型应返回 "expected array" 错误。
func TestTodoManager_HandleTodo_ItemsNotArray(t *testing.T) {
	m := NewTodoManager()
	_, err := m.HandleTodo(context.Background(), map[string]any{
		"items": "not-an-array",
	})
	if err == nil {
		t.Fatal("expected error for non-array items, got nil")
	}
	if !strings.Contains(err.Error(), "expected array") {
		t.Errorf("expected 'expected array' in error, got: %v", err)
	}
}

// UT-11: Render 输出中 completed 计数应精确正确。
func TestTodoManager_Render_CompletedCount(t *testing.T) {
	m := NewTodoManager()
	_, err := m.HandleTodo(context.Background(), map[string]any{
		"items": []any{
			map[string]any{"id": "1", "text": "task a", "status": "completed"},
			map[string]any{"id": "2", "text": "task b", "status": "completed"},
			map[string]any{"id": "3", "text": "task c", "status": "pending"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := m.Render()
	if !strings.Contains(out, "(2/3 completed)") {
		t.Errorf("expected '(2/3 completed)' in render output, got:\n%s", out)
	}
}
