package main

import (
	"context"
	"embed"
	"fmt"
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

//go:embed testdata/create_python_project.md testdata/nag_trigger.md
var fixtureFS embed.FS

// sandboxS03Dir 返回 s03 real 集成测试的隔离沙箱目录。
func sandboxS03Dir(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s03", "real", t.Name(), runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create sandbox dir %s: %v", dir, err)
	}
	return dir
}

// repoRoot returns the repository root for integration tests.
func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	return root
}

// enableViewerTrace routes real integration traces to the local viewer store.
func enableViewerTrace(t *testing.T) {
	t.Helper()

	t.Setenv("AI_SDK_DEVTOOLS", "1")
	t.Setenv("AI_SDK_DEVTOOLS_DIR", filepath.Join(repoRoot(t), ".devtools"))
}

// IT-S03-REAL-01: 教程"试一试"场景 — 创建 Python 项目。
//
// 验证：
//  1. Agent 在执行过程中至少调用一次 todo 工具
//  2. 任务完成后 main.py、tests/、README.md 均存在于沙箱目录
//  3. 最终 TodoManager 中所有 item 均为 completed
func TestIntegration_CreatePythonProject(t *testing.T) {
	// 未配置真实 API Key 时跳过，不影响 CI 的 go test ./...
	_ = godotenv.Load("../../.env")
	if os.Getenv("DASHSCOPE_API_KEY") == "" {
		t.Skip("DASHSCOPE_API_KEY not set, skipping real integration test")
	}

	promptBytes, err := fixtureFS.ReadFile("testdata/create_python_project.md")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	prompt := strings.TrimSpace(string(promptBytes))

	sandboxDir := sandboxS03Dir(t)
	t.Logf("sandbox dir: %s", sandboxDir)

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	model := getModel()

	todoManager := NewTodoManager()

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.ListDirToolDef(), tools.ListDirHandler)
	registry.Register(tools.EditFileToolDef(), tools.EditFileHandler)
	registry.Register(tools.TodoToolDef(), todoManager.HandleTodo)

	// 明确要求 LLM 使用绝对路径写入沙箱目录，避免相对路径写入测试源码目录。
	system := fmt.Sprintf(
		"You are a coding agent. Your working directory is %s.\n"+
			"IMPORTANT: Always use absolute paths starting with %s when creating or writing files.\n"+
			"Use the todo tool to plan multi-step tasks. Mark in_progress before starting, completed when done.\n"+
			"Prefer tools over prose.",
		sandboxDir, sandboxDir,
	)

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}

	enableViewerTrace(t)
	history, err = loop.RunWithManagedTrace(
		context.Background(),
		devtools.RunMeta{
			Kind:         "main",
			Title:        t.Name(),
			InputPreview: prompt,
		},
		loop.RunWithTodoNag,
		client,
		model,
		history,
		registry,
	)
	if err != nil {
		t.Fatalf("agent loop error: %v", err)
	}

	// 断言 1：history 中至少有一次 todo 工具调用
	todoCallFound := false
	for _, msg := range history {
		if msg.OfAssistant == nil {
			continue
		}
		for _, tc := range msg.OfAssistant.ToolCalls {
			if tc.Function.Name == "todo" {
				todoCallFound = true
				break
			}
		}
	}
	if !todoCallFound {
		t.Error("agent should have called the todo tool at least once")
	}

	// 断言 2：沙箱目录中存在预期文件/目录
	for _, rel := range []string{"main.py", "tests", "README.md"} {
		path := filepath.Join(sandboxDir, rel)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected %s to exist in sandbox, but it does not", rel)
		}
	}

	// 断言 3：所有 todo item 最终为 completed
	if len(todoManager.items) > 0 {
		render := todoManager.Render()
		if strings.Contains(render, "[ ]") || strings.Contains(render, "[>]") {
			t.Errorf("expected all todos to be completed, but render shows pending/in_progress:\n%s", render)
		}
		t.Logf("final todo state:\n%s", render)
	}
}

// IT-S03-REAL-02: 验证 nag 机制在真实 LLM 调用中被触发。
//
// 场景：system prompt 要求 LLM 先执行 3 次 bash（不调用 todo），
// 连续 3 轮未调用 todo 后，RunWithTodoNag 应向 messages 注入
// "Update your todos." 的 user message。
//
// 验证：返回的 history 中存在至少一条内容为 "Update your todos." 的 user message。
func TestIntegration_NagInjectedAfterThreeRoundsWithoutTodo(t *testing.T) {
	_ = godotenv.Load("../../.env")
	if os.Getenv("DASHSCOPE_API_KEY") == "" {
		t.Skip("DASHSCOPE_API_KEY not set, skipping real integration test")
	}

	promptBytes, err := fixtureFS.ReadFile("testdata/nag_trigger.md")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}
	prompt := strings.TrimSpace(string(promptBytes))

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	model := getModel()

	todoManager := NewTodoManager()

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.TodoToolDef(), todoManager.HandleTodo)

	// system prompt 明确要求先执行 3 次 bash，不要先调用 todo，
	// 以可控地触发 nag（连续 3 轮未调用 todo → 注入提醒）。
	system := "You are a coding agent. " +
		"IMPORTANT: Execute the user's bash commands one by one first, " +
		"do NOT call the todo tool until after all bash commands are done. " +
		"Each bash command must be a separate tool call in its own round."

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}

	enableViewerTrace(t)
	history, err = loop.RunWithManagedTrace(
		context.Background(),
		devtools.RunMeta{
			Kind:         "main",
			Title:        t.Name(),
			InputPreview: prompt,
		},
		loop.RunWithTodoNag,
		client,
		model,
		history,
		registry,
	)
	if err != nil {
		t.Fatalf("agent loop error: %v", err)
	}

	// 断言：history 中至少存在一条 nag user message
	nagFound := false
	for _, msg := range history {
		if msg.OfUser == nil {
			continue
		}
		content := msg.OfUser.Content
		if content.OfString.Value == "Update your todos." {
			nagFound = true
			break
		}
		for _, part := range content.OfArrayOfContentParts {
			if part.OfText != nil && strings.Contains(part.OfText.Text, "Update your todos.") {
				nagFound = true
				break
			}
		}
		if nagFound {
			break
		}
	}

	if !nagFound {
		// 打印 history 摘要便于调试
		for i, msg := range history {
			if msg.OfUser != nil {
				t.Logf("history[%d] user: %q", i, msg.OfUser.Content.OfString.Value)
			}
		}
		t.Error("expected at least one nag message 'Update your todos.' in history, but none found")
	} else {
		t.Log("nag message successfully injected into history")
	}
}
