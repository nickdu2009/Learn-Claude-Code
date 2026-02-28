package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

func TestS02_ToolUse_EndToEnd_WithFakeOpenAI(t *testing.T) {
	tmpDir := localWorkDirUnderLocal(t)
	targetFile := filepath.Join(tmpDir, "hello.txt")

	var callCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		n := callCount.Add(1)
		switch n {
		case 1:
			// First response: request tool calls (list_dir, write_file, read_file).
			writeJSON(w, 200, fakeToolCallsResponse(t, []fakeToolCall{
				{ID: "call-1", Name: "list_dir", Args: mustJSON(map[string]any{"path": tmpDir})},
				{ID: "call-2", Name: "write_file", Args: mustJSON(map[string]any{"path": targetFile, "content": "Hello, s02!"})},
				{ID: "call-3", Name: "read_file", Args: mustJSON(map[string]any{"path": targetFile})},
			}))
		case 2:
			// Second request should include tool results.
			var req map[string]any
			if err := json.Unmarshal(body, &req); err != nil {
				t.Fatalf("failed to parse request JSON: %v", err)
			}
			msgs, _ := req["messages"].([]any)
			if len(msgs) < 4 {
				t.Fatalf("expected messages to include tool results, got %d", len(msgs))
			}

			var toolMsgs int
			for _, m := range msgs {
				obj, _ := m.(map[string]any)
				if role, _ := obj["role"].(string); role == "tool" {
					toolMsgs++
				}
			}
			if toolMsgs < 3 {
				t.Fatalf("expected >=3 tool messages, got %d", toolMsgs)
			}

			writeJSON(w, 200, fakeTextResponse("done"))
		default:
			t.Fatalf("unexpected extra call to fake OpenAI server: %d", n)
		}
	}))
	t.Cleanup(server.Close)

	client := openai.NewClient(
		option.WithAPIKey("sk-test"),
		option.WithBaseURL(server.URL+"/v1"),
	)

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.ListDirToolDef(), tools.ListDirHandler)

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("system"),
		openai.UserMessage("Create and read a file via tools."),
	}

	out, err := loop.RunWithRecorder(context.Background(), &client, "test-model", messages, registry, nil)
	if err != nil {
		t.Fatalf("loop failed: %v", err)
	}

	// Ensure file was written.
	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", targetFile, err)
	}
	if string(content) != "Hello, s02!" {
		t.Fatalf("unexpected file content: %q", string(content))
	}

	// Ensure final assistant text exists.
	last := out[len(out)-1]
	if last.OfAssistant == nil {
		t.Fatalf("expected last message to be assistant, got %+v", last)
	}
	text := last.OfAssistant.Content.OfString.Value
	for _, part := range last.OfAssistant.Content.OfArrayOfContentParts {
		if part.OfText != nil {
			text += part.OfText.Text
		}
	}
	if !strings.Contains(text, "done") {
		t.Fatalf("expected final reply to contain 'done', got %q", text)
	}
}

func localWorkDirUnderLocal(t *testing.T) string {
	t.Helper()
	cwd, _ := os.Getwd()
	root := findRepoRootForTest(cwd)
	if root == "" {
		t.Fatalf("failed to locate repo root from %s", cwd)
	}
	name := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_").Replace(t.Name())
	dir := filepath.Join(root, ".local", "test-artifacts", "s02", "fake", name, time.Now().Format("20060102-150405.000000000"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create work dir: %v", err)
	}
	return dir
}

type fakeToolCall struct {
	ID   string
	Name string
	Args string
}

func fakeToolCallsResponse(t *testing.T, calls []fakeToolCall) map[string]any {
	t.Helper()
	toolCalls := make([]map[string]any, 0, len(calls))
	for _, c := range calls {
		toolCalls = append(toolCalls, map[string]any{
			"id":   c.ID,
			"type": "function",
			"function": map[string]any{
				"name":      c.Name,
				"arguments": c.Args,
			},
		})
	}
	return map[string]any{
		"id":      "chatcmpl-test-1",
		"object":  "chat.completion",
		"created": 1,
		"model":   "test-model",
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":       "assistant",
					"content":    "",
					"tool_calls": toolCalls,
				},
				"finish_reason": "tool_calls",
			},
		},
	}
}

func fakeTextResponse(text string) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-test-2",
		"object":  "chat.completion",
		"created": 2,
		"model":   "test-model",
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": text,
				},
				"finish_reason": "stop",
			},
		},
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	_, _ = w.Write(buf.Bytes())
}
