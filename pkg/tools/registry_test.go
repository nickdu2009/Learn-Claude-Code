package tools

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// Registry 测试
// ─────────────────────────────────────────────────────────────────────────────

// TestRegistry_Register: 注册工具后，Definitions 应包含该工具定义。
func TestRegistry_Register(t *testing.T) {
	r := New()
	r.Register(BashToolDef(), BashHandler)
	r.Register(ReadFileToolDef(), ReadFileHandler)

	defs := r.Definitions()
	if len(defs) != 2 {
		t.Fatalf("expected 2 definitions, got %d", len(defs))
	}

	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Function.Name] = true
	}
	if !names["bash"] {
		t.Error("expected 'bash' in definitions")
	}
	if !names["read_file"] {
		t.Error("expected 'read_file' in definitions")
	}
}

// TestRegistry_Dispatch_OK: Dispatch 能正确调用已注册的 Handler 并返回结果。
func TestRegistry_Dispatch_OK(t *testing.T) {
	r := New()
	called := false
	r.Register(BashToolDef(), func(args map[string]any) (string, error) {
		called = true
		return "dispatched", nil
	})

	result, err := r.Dispatch("bash", map[string]any{"command": "echo test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("handler was not called")
	}
	if result != "dispatched" {
		t.Errorf("expected 'dispatched', got %q", result)
	}
}

// TestRegistry_Dispatch_UnknownTool: 调用未注册的工具名，应返回 unknown tool 错误。
func TestRegistry_Dispatch_UnknownTool(t *testing.T) {
	r := New()

	_, err := r.Dispatch("magic_tool", map[string]any{})
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("error message should contain 'unknown tool', got: %q", err.Error())
	}
}

// TestRegistry_Dispatch_ArgsPassthrough: Dispatch 应将 args 原样传递给 Handler。
func TestRegistry_Dispatch_ArgsPassthrough(t *testing.T) {
	r := New()
	var receivedArgs map[string]any
	r.Register(ReadFileToolDef(), func(args map[string]any) (string, error) {
		receivedArgs = args
		return "ok", nil
	})

	inputArgs := map[string]any{"path": "/some/path.txt"}
	_, err := r.Dispatch("read_file", inputArgs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedArgs["path"] != "/some/path.txt" {
		t.Errorf("args not passed through correctly: got %v", receivedArgs)
	}
}

// TestRegistry_AllS02Tools: 验证 s02 中注册的所有工具均可正常 Dispatch（smoke test）。
func TestRegistry_AllS02Tools(t *testing.T) {
	r := New()
	r.Register(BashToolDef(), BashHandler)
	r.Register(ReadFileToolDef(), ReadFileHandler)
	r.Register(WriteFileToolDef(), WriteFileHandler)
	r.Register(ListDirToolDef(), ListDirHandler)

	if len(r.Definitions()) != 4 {
		t.Fatalf("expected 4 definitions, got %d", len(r.Definitions()))
	}

	// 验证每个工具名都能被路由（即使 handler 内部可能报错，也不应返回 unknown tool）
	toolNames := []string{"bash", "read_file", "write_file", "list_dir"}
	for _, name := range toolNames {
		_, err := r.Dispatch(name, map[string]any{})
		if err != nil && strings.Contains(err.Error(), "unknown tool") {
			t.Errorf("tool %q should be registered but got: %v", name, err)
		}
	}
}
