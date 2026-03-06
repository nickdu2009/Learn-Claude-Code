package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileHandler_CreatesParentDirectories(t *testing.T) {
	withWorkingDir(t, t.TempDir(), func() {
		target := filepath.Join("nested", "dir", "hello.txt")
		result, err := WriteFileHandler(context.Background(), map[string]any{
			"path":    target,
			"content": "hello",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, target) {
			t.Fatalf("expected result to mention target, got %q", result)
		}

		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("expected file to be created: %v", err)
		}
		if string(data) != "hello" {
			t.Fatalf("content mismatch: %q", string(data))
		}
	})
}

func TestReadFileHandler_ReadsInsideWorkspace(t *testing.T) {
	withWorkingDir(t, t.TempDir(), func() {
		target := "note.txt"
		if err := os.WriteFile(target, []byte("workspace file"), 0644); err != nil {
			t.Fatalf("failed to create fixture: %v", err)
		}

		result, err := ReadFileHandler(context.Background(), map[string]any{"path": target})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != "workspace file" {
			t.Fatalf("expected file content, got %q", result)
		}
	})
}

func TestEditFileHandler_ReplacesFirstMatch(t *testing.T) {
	withWorkingDir(t, t.TempDir(), func() {
		target := "edit.txt"
		if err := os.WriteFile(target, []byte("hello world hello"), 0644); err != nil {
			t.Fatalf("failed to create fixture: %v", err)
		}

		result, err := EditFileHandler(context.Background(), map[string]any{
			"path":     target,
			"old_text": "hello",
			"new_text": "hi",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, target) {
			t.Fatalf("expected result to mention target, got %q", result)
		}

		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("failed to read updated file: %v", err)
		}
		if string(data) != "hi world hello" {
			t.Fatalf("unexpected edited content: %q", string(data))
		}
	})
}

func TestListDirHandler_ListsWorkspaceEntries(t *testing.T) {
	withWorkingDir(t, t.TempDir(), func() {
		if err := os.WriteFile("file1.txt", []byte("data"), 0644); err != nil {
			t.Fatalf("failed to create file fixture: %v", err)
		}
		if err := os.MkdirAll("subdir", 0755); err != nil {
			t.Fatalf("failed to create dir fixture: %v", err)
		}

		result, err := ListDirHandler(context.Background(), map[string]any{"path": "."})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "[FILE] file1.txt") {
			t.Fatalf("expected file entry, got:\n%s", result)
		}
		if !strings.Contains(result, "[DIR]  subdir") {
			t.Fatalf("expected directory entry, got:\n%s", result)
		}
	})
}

func TestFSHandlers_RejectPathEscape(t *testing.T) {
	withWorkingDir(t, t.TempDir(), func() {
		cases := []struct {
			name   string
			invoke func() error
		}{
			{
				name: "read_file",
				invoke: func() error {
					_, err := ReadFileHandler(context.Background(), map[string]any{"path": "../outside.txt"})
					return err
				},
			},
			{
				name: "write_file",
				invoke: func() error {
					_, err := WriteFileHandler(context.Background(), map[string]any{
						"path":    "../outside.txt",
						"content": "blocked",
					})
					return err
				},
			},
			{
				name: "edit_file",
				invoke: func() error {
					_, err := EditFileHandler(context.Background(), map[string]any{
						"path":     "../outside.txt",
						"old_text": "a",
						"new_text": "b",
					})
					return err
				},
			},
			{
				name: "list_dir",
				invoke: func() error {
					_, err := ListDirHandler(context.Background(), map[string]any{"path": ".."})
					return err
				},
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := tc.invoke()
				if err == nil {
					t.Fatal("expected path escape error")
				}
				if !strings.Contains(err.Error(), "path escapes workspace") {
					t.Fatalf("unexpected error: %v", err)
				}
			})
		}
	})
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir to %s: %v", dir, err)
	}
	defer func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("failed to restore working directory: %v", err)
		}
	}()

	fn()
}
