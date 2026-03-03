package tools

import (
	"strings"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// BashHandler 测试
// ─────────────────────────────────────────────────────────────────────────────

// UT-BASH-01: 正常执行有输出的命令，返回标准输出内容。
func TestBashHandler_Normal(t *testing.T) {
	result, err := BashHandler(map[string]any{
		"command": "echo 'hello s02'",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello s02" {
		t.Errorf("expected 'hello s02', got %q", result)
	}
}

// UT-BASH-02: 执行危险命令 rm -rf /，应被拦截，不执行。
func TestBashHandler_DangerousRmRf(t *testing.T) {
	result, err := BashHandler(map[string]any{
		"command": "rm -rf /",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Error: Dangerous command blocked" {
		t.Errorf("expected dangerous command to be blocked, got %q", result)
	}
}

// UT-BASH-03: 包含 sudo 的命令应被拦截。
func TestBashHandler_DangerousSudo(t *testing.T) {
	result, err := BashHandler(map[string]any{
		"command": "sudo apt-get install something",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Error: Dangerous command blocked" {
		t.Errorf("expected sudo to be blocked, got %q", result)
	}
}

// UT-BASH-04: 执行报错的命令，返回值应包含错误信息而不是 panic。
func TestBashHandler_CommandError(t *testing.T) {
	result, err := BashHandler(map[string]any{
		"command": "ls /nonexistent_path_s02_test_12345",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Error("expected non-empty error output")
	}
}

// UT-BASH-05: 缺少 command 参数，应返回错误。
func TestBashHandler_MissingCommand(t *testing.T) {
	_, err := BashHandler(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing command, got nil")
	}
	if !strings.Contains(err.Error(), "missing or invalid") {
		t.Errorf("error message mismatch: got %q", err.Error())
	}
}

// UT-BASH-06: 执行无输出的命令，应返回占位符 "(no output)"。
func TestBashHandler_NoOutput(t *testing.T) {
	result, err := BashHandler(map[string]any{
		"command": "true",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "(no output)" {
		t.Errorf("expected '(no output)', got %q", result)
	}
}
