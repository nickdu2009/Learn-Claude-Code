// 集成测试：验证 s02 使用真实 LLM + 工具集完成实际任务的能力。
//
// 运行方式（需要真实 API Key）：
//
//	go test ./agents/s02_tool_use/ -run Integration -v -timeout 120s
//
// 默认跳过（CI 环境无 API Key 时自动跳过）。
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

func TestMain(m *testing.M) {
	cwd, _ := os.Getwd()
	root := findRepoRootForTest(cwd)
	if root != "" {
		_ = os.MkdirAll(filepath.Join(root, ".devtools"), 0o755)
		_ = os.Setenv("AI_SDK_DEVTOOLS_DIR", filepath.Join(root, ".devtools"))
	}
	os.Exit(m.Run())
}

func findRepoRootForTest(start string) string {
	dir := start
	for {
		if dir == "" || dir == string(filepath.Separator) {
			return ""
		}
		if fi, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			if fi.IsDir() || fi.Mode().IsRegular() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func skipIfNoAPIKey(t *testing.T) {
	t.Helper()
	_ = godotenv.Load("../../.env")
	if os.Getenv("DASHSCOPE_API_KEY") == "" {
		t.Skip("skipping integration test: DASHSCOPE_API_KEY not set")
	}
}

func newRealLoopInputs(t *testing.T) (*openai.Client, string, *tools.Registry) {
	t.Helper()
	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	model := getModel()

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.ListDirToolDef(), tools.ListDirHandler)

	return client, model, registry
}

func runLoopOnce(t *testing.T, client *openai.Client, model string, registry *tools.Registry, system, prompt string) (string, []openai.ChatCompletionMessageParamUnion) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}
	out, err := loop.RunWithRecorder(ctx, client, model, messages, registry, devtools.NewRunRecorderFromEnv())
	if err != nil {
		t.Fatalf("loop failed: %v", err)
	}

	last := out[len(out)-1]
	var reply string
	if last.OfAssistant != nil {
		if last.OfAssistant.Content.OfString.Value != "" {
			reply += last.OfAssistant.Content.OfString.Value
		}
		for _, part := range last.OfAssistant.Content.OfArrayOfContentParts {
			if part.OfText != nil {
				reply += part.OfText.Text
			}
		}
	}
	return reply, out
}

// TestIntegrationS02_CreateAndReadFile verifies the model can use tools to write then read a file.
func TestIntegrationS02_CreateAndReadFile(t *testing.T) {
	skipIfNoAPIKey(t)

	tmpDir := localWorkDir(t)
	system := "You are a coding agent at " + tmpDir + ". Use tools (read_file, write_file, list_dir) to solve tasks. Do not use bash. Act, don't explain."
	client, model, registry := newRealLoopInputs(t)

	target := filepath.Join(tmpDir, "hello.txt")
	prompt := strings.Join([]string{
		"Use write_file to create a file at this exact path:",
		target,
		"Set its content to exactly: Hello, s02!",
		"Then use read_file to read the same exact path and reply with the exact content only.",
	}, "\n")

	reply, _ := runLoopOnce(t, client, model, registry, system, prompt)

	// Verify side-effect on disk.
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected %s to be created, got: %v", target, err)
	}
	if string(content) != "Hello, s02!" {
		t.Fatalf("unexpected file content: %q", string(content))
	}

	// Reply should contain the file content (allow extra whitespace/newlines).
	if !strings.Contains(reply, "Hello, s02!") {
		t.Fatalf("expected reply to contain %q, got %q", "Hello, s02!", reply)
	}
}

// TestIntegrationS02_ListDir verifies list_dir can reveal directory contents.
func TestIntegrationS02_ListDir(t *testing.T) {
	skipIfNoAPIKey(t)

	tmpDir := localWorkDir(t)
	system := "You are a coding agent at " + tmpDir + ". Use tools (list_dir) to solve tasks. Do not use bash. Act, don't explain."
	client, model, registry := newRealLoopInputs(t)

	// Pre-create files to be discovered by list_dir.
	_ = os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(tmpDir, "b.txt"), []byte("b"), 0o644)

	prompt := "Use list_dir on this exact path: " + tmpDir + ". Reply with the filenames you see. Include both a.txt and b.txt."
	reply, _ := runLoopOnce(t, client, model, registry, system, prompt)

	if !strings.Contains(reply, "a.txt") || !strings.Contains(reply, "b.txt") {
		t.Fatalf("expected reply to mention a.txt and b.txt, got %q", reply)
	}
}

// TestIntegrationS02_WriteJSONAndExtractField verifies write_file can produce valid JSON and read_file can be used to extract a field.
func TestIntegrationS02_WriteJSONAndExtractField(t *testing.T) {
	skipIfNoAPIKey(t)

	tmpDir := localWorkDir(t)
	system := "You are a coding agent at " + tmpDir + ". Use tools (read_file, write_file) to solve tasks. Do not use bash. Act, don't explain."
	client, model, registry := newRealLoopInputs(t)

	target := filepath.Join(tmpDir, "data.json")
	expectedJSON := `{"name":"alice","count":2}`
	prompt := strings.Join([]string{
		"Use write_file to create a file at this exact path:",
		target,
		"Write this exact JSON (no extra keys): " + expectedJSON,
		"Then use read_file to read the same exact path and reply with the value of the 'name' field only.",
	}, "\n")

	reply, _ := runLoopOnce(t, client, model, registry, system, prompt)

	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected %s to be created, got: %v", target, err)
	}

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("expected valid JSON in %s, got error: %v, raw=%q", target, err, string(raw))
	}
	if obj["name"] != "alice" {
		t.Fatalf("expected name=alice, got: %#v", obj["name"])
	}
	if obj["count"] != float64(2) {
		t.Fatalf("expected count=2, got: %#v", obj["count"])
	}
	if !strings.Contains(strings.ToLower(reply), "alice") {
		t.Fatalf("expected reply to contain 'alice', got %q", reply)
	}
}

// TestIntegrationS02_OverwriteFile verifies repeated write_file overwrites content deterministically.
func TestIntegrationS02_OverwriteFile(t *testing.T) {
	skipIfNoAPIKey(t)

	tmpDir := localWorkDir(t)
	system := "You are a coding agent at " + tmpDir + ". Use tools (read_file, write_file) to solve tasks. Do not use bash. Act, don't explain."
	client, model, registry := newRealLoopInputs(t)

	target := filepath.Join(tmpDir, "note.txt")
	prompt := strings.Join([]string{
		"Use write_file to write to this exact path:",
		target,
		"First write content exactly: first",
		"Then write again to the same exact path with content exactly: second",
		"Then use read_file to read the same path and reply with the exact content only.",
	}, "\n")

	reply, _ := runLoopOnce(t, client, model, registry, system, prompt)

	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected %s to be created, got: %v", target, err)
	}
	if string(raw) != "second" {
		t.Fatalf("expected overwritten content 'second', got %q", string(raw))
	}
	if strings.TrimSpace(reply) != "second" && !strings.Contains(reply, "second") {
		t.Fatalf("expected reply to be 'second' (or contain it), got %q", reply)
	}
}

func localWorkDir(t *testing.T) string {
	t.Helper()
	cwd, _ := os.Getwd()
	root := findRepoRootForTest(cwd)
	if root == "" {
		t.Fatalf("failed to locate repo root from %s", cwd)
	}
	name := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ":", "_").Replace(t.Name())
	runID := time.Now().Format("20060102-150405.000000000")
	dir := filepath.Join(root, ".local", "test-artifacts", "s02", "real", name, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create work dir: %v", err)
	}
	return dir
}
