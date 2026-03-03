package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sandboxDir returns an isolated test directory under .local/test-artifacts/s02/unit/fs/<testName>/<runID>/
// It creates the directory and returns the absolute path.
func sandboxDir(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s02", "unit", "fs", t.Name(), runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create sandbox dir %s: %v", dir, err)
	}
	return dir
}

// ─────────────────────────────────────────────────────────────────────────────
// WriteFileHandler 测试
// ─────────────────────────────────────────────────────────────────────────────

// UT-FS-01: 正常写入文件，验证文件被创建且内容正确。
func TestWriteFileHandler_Normal(t *testing.T) {
	dir := sandboxDir(t)
	target := filepath.Join(dir, "hello.txt")

	result, err := WriteFileHandler(map[string]any{
		"path":    target,
		"content": "hello s02",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, target) {
		t.Errorf("result should mention the file path, got: %q", result)
	}

	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("file was not created: %v", err)
	}
	if string(data) != "hello s02" {
		t.Errorf("file content mismatch: got %q", string(data))
	}
}

// UT-FS-02: 缺少 path 参数，应返回错误。
func TestWriteFileHandler_MissingPath(t *testing.T) {
	_, err := WriteFileHandler(map[string]any{
		"content": "some content",
	})
	if err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}

// UT-FS-02b: 缺少 content 参数，应返回错误。
func TestWriteFileHandler_MissingContent(t *testing.T) {
	dir := sandboxDir(t)
	_, err := WriteFileHandler(map[string]any{
		"path": filepath.Join(dir, "no-content.txt"),
	})
	if err == nil {
		t.Fatal("expected error for missing content, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ReadFileHandler 测试
// ─────────────────────────────────────────────────────────────────────────────

// UT-FS-03: 读取刚写入的文件，内容应与写入时一致。
func TestReadFileHandler_Normal(t *testing.T) {
	dir := sandboxDir(t)
	target := filepath.Join(dir, "read_me.txt")
	if err := os.WriteFile(target, []byte("read content 42"), 0644); err != nil {
		t.Fatalf("setup: failed to create test file: %v", err)
	}

	result, err := ReadFileHandler(map[string]any{"path": target})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "read content 42" {
		t.Errorf("content mismatch: got %q", result)
	}
}

// UT-FS-04: 读取不存在的文件，应返回错误。
func TestReadFileHandler_NotFound(t *testing.T) {
	_, err := ReadFileHandler(map[string]any{
		"path": "/nonexistent/path/s02_test_file_12345.txt",
	})
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read file") {
		t.Errorf("error message mismatch: got %q", err.Error())
	}
}

// UT-FS-04b: 缺少 path 参数，应返回错误。
func TestReadFileHandler_MissingPath(t *testing.T) {
	_, err := ReadFileHandler(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListDirHandler 测试
// ─────────────────────────────────────────────────────────────────────────────

// UT-FS-05: 列出包含文件和子目录的目录，验证输出格式。
func TestListDirHandler_Normal(t *testing.T) {
	dir := sandboxDir(t)

	// 准备测试数据：一个文件和一个子目录
	if err := os.WriteFile(filepath.Join(dir, "file1.txt"), []byte("data"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subdir"), 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	result, err := ListDirHandler(map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "[FILE] file1.txt") {
		t.Errorf("result should contain '[FILE] file1.txt', got:\n%s", result)
	}
	if !strings.Contains(result, "[DIR]  subdir") {
		t.Errorf("result should contain '[DIR]  subdir', got:\n%s", result)
	}
}

// UT-FS-06: 列出空目录，应返回固定占位符。
func TestListDirHandler_EmptyDir(t *testing.T) {
	dir := sandboxDir(t)

	result, err := ListDirHandler(map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "(empty directory)" {
		t.Errorf("expected '(empty directory)', got: %q", result)
	}
}

// UT-FS-06b: 列出不存在的目录，应返回错误。
func TestListDirHandler_NotFound(t *testing.T) {
	_, err := ListDirHandler(map[string]any{
		"path": "/nonexistent/s02_test_dir_12345",
	})
	if err == nil {
		t.Fatal("expected error for non-existent directory, got nil")
	}
	if !strings.Contains(err.Error(), "failed to list directory") {
		t.Errorf("error message mismatch: got %q", err.Error())
	}
}

// UT-FS-06c: 缺少 path 参数，应返回错误。
func TestListDirHandler_MissingPath(t *testing.T) {
	_, err := ListDirHandler(map[string]any{})
	if err == nil {
		t.Fatal("expected error for missing path, got nil")
	}
}
