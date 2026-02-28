// 集成测试：验证 Agent 使用真实 LLM（通义千问）完成实际任务的能力。
//
// 运行方式（需要真实 API Key）：
//
//	go test ./agents/s01_agent_loop/ -run Integration -v -timeout 120s
//
// 默认跳过（CI 环境无 API Key 时自动跳过）。
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/testcases"
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

// skipIfNoAPIKey 在没有真实 API Key 时跳过集成测试。
func skipIfNoAPIKey(t *testing.T) {
	t.Helper()
	_ = godotenv.Load("../../.env")
	if os.Getenv("DASHSCOPE_API_KEY") == "" {
		t.Skip("skipping integration test: DASHSCOPE_API_KEY not set")
	}
}

// newRealAgent 创建连接真实 LLM 的 Agent。
func newRealAgent(t *testing.T) (LLMClient, string) {
	t.Helper()
	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	return &realLLMClient{client: client, model: getModel()}, getModel()
}

// runAgent 用给定的 prompt 运行一次 Agent，返回最终回复文本。
// workDir 指定 bash 命令的工作目录。
func runAgent(t *testing.T, llm LLMClient, system, prompt, workDir string) (string, []openai.ChatCompletionMessageParamUnion) {
	t.Helper()
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(prompt),
	}
	result := agentLoop(llm, system, messages, workDir, devtools.NewRunRecorderFromEnv())

	// 提取最终文本回复
	last := result[len(result)-1]
	var reply string
	if last.OfAssistant != nil {
		if last.OfAssistant.Content.OfString.Value != "" {
			reply = last.OfAssistant.Content.OfString.Value
		}
		for _, part := range last.OfAssistant.Content.OfArrayOfContentParts {
			if part.OfText != nil {
				reply += part.OfText.Text
			}
		}
	}
	return reply, result
}

// ─────────────────────────────────────────────
// 集成测试用例
// ─────────────────────────────────────────────

// TestIntegration_CreateFile 验证 Agent 能创建文件并写入内容。
func TestIntegration_CreateFile(t *testing.T) {
	skipIfNoAPIKey(t)

	tmpDir := localWorkDir(t)
	system := "You are a coding agent at " + tmpDir + ". Use bash to solve tasks. Act, don't explain."
	llm, _ := newRealAgent(t)

	targetFile := filepath.Join(tmpDir, "hello.txt")
	prompt := "Create a file named hello.txt in the current directory with content: Hello, Agent!"

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	_ = ctx

	reply, _ := runAgent(t, llm, system, prompt, tmpDir)
	t.Logf("Agent reply: %s", reply)

	// 验证文件确实被创建
	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("expected file %s to be created, but got error: %v", targetFile, err)
	}
	if !strings.Contains(string(content), "Hello, Agent!") {
		t.Errorf("expected file content to contain 'Hello, Agent!', got: %q", string(content))
	}
}

// TestIntegration_ReadFile 验证 Agent 能读取文件内容并在回复中提及。
func TestIntegration_ReadFile(t *testing.T) {
	skipIfNoAPIKey(t)

	tmpDir := localWorkDir(t)
	system := "You are a coding agent at " + tmpDir + ". Use bash to solve tasks. Act, don't explain."
	llm, _ := newRealAgent(t)

	// 预先创建文件
	targetFile := filepath.Join(tmpDir, "secret.txt")
	if err := os.WriteFile(targetFile, []byte("magic_token_xyz_42"), 0644); err != nil {
		t.Fatal(err)
	}

	prompt := "Read the file secret.txt and tell me its content."
	reply, _ := runAgent(t, llm, system, prompt, tmpDir)
	t.Logf("Agent reply: %s", reply)

	if !strings.Contains(reply, "magic_token_xyz_42") {
		t.Errorf("expected reply to contain file content 'magic_token_xyz_42', got: %q", reply)
	}
}

// TestIntegration_MultiStep 验证 Agent 能完成多步骤任务（创建目录 → 写文件 → 列出文件）。
func TestIntegration_MultiStep(t *testing.T) {
	skipIfNoAPIKey(t)

	tmpDir := localWorkDir(t)
	system := "You are a coding agent at " + tmpDir + ". Use bash to solve tasks. Act, don't explain."
	llm, _ := newRealAgent(t)

	prompt := "Do these steps in order: " +
		"1) create a directory named 'output', " +
		"2) create a file output/result.txt with content 'step_done', " +
		"3) list all files under output/."

	reply, messages := runAgent(t, llm, system, prompt, tmpDir)
	t.Logf("Agent reply: %s", reply)
	t.Logf("Total messages: %d", len(messages))

	// 验证目录和文件存在
	resultFile := filepath.Join(tmpDir, "output", "result.txt")
	content, err := os.ReadFile(resultFile)
	if err != nil {
		t.Fatalf("expected output/result.txt to exist, got: %v", err)
	}
	if !strings.Contains(string(content), "step_done") {
		t.Errorf("expected 'step_done' in file, got: %q", string(content))
	}

	// 多步骤任务应触发多次工具调用（消息数 > 3）
	if len(messages) <= 3 {
		t.Errorf("expected multi-step task to generate >3 messages, got %d", len(messages))
	}
}

// TestIntegration_DangerousCommandRefused 验证 Agent 在被要求执行危险命令时，
// runBash 会拦截，Agent 仍能正常返回（不崩溃）。
func TestIntegration_DangerousCommandRefused(t *testing.T) {
	skipIfNoAPIKey(t)

	tmpDir := localWorkDir(t)
	system := "You are a coding agent at " + tmpDir + ". Use bash to solve tasks. Act, don't explain."
	llm, _ := newRealAgent(t)

	// 直接要求执行危险命令
	prompt := "Run this exact bash command: sudo ls /root"
	reply, _ := runAgent(t, llm, system, prompt, tmpDir)
	t.Logf("Agent reply: %s", reply)

	// Agent 应该完成（不 panic），系统目录不应被访问
	// 只要没有崩溃就算通过，reply 内容不做强断言（模型可能拒绝或说明被拦截）
}

// TestIntegration_MultiRound 验证多轮对话中历史上下文被正确保留。
func TestIntegration_MultiRound(t *testing.T) {
	skipIfNoAPIKey(t)

	tmpDir := localWorkDir(t)
	system := "You are a coding agent at " + tmpDir + ". Use bash to solve tasks. Act, don't explain."
	llm, _ := newRealAgent(t)

	// 第一轮：创建文件
	history := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("Create a file named memo.txt with content: round_one"),
	}
	history = agentLoop(llm, system, history, tmpDir, nil)

	// 第二轮：基于上下文追加内容（Agent 应记得 memo.txt）
	history = append(history, openai.UserMessage("Append the text ' round_two' to memo.txt"))
	history = agentLoop(llm, system, history, tmpDir, nil)

	// 验证文件包含两轮内容
	content, err := os.ReadFile(filepath.Join(tmpDir, "memo.txt"))
	if err != nil {
		t.Fatalf("memo.txt not found: %v", err)
	}
	t.Logf("memo.txt content: %q", string(content))

	if !strings.Contains(string(content), "round_one") {
		t.Errorf("expected 'round_one' in file, got: %q", string(content))
	}
	if !strings.Contains(string(content), "round_two") {
		t.Errorf("expected 'round_two' in file after second round, got: %q", string(content))
	}
}

// TestIntegration_ReactViteProject 使用一个“真实/通用”前端需求（Todo App）作为集成测试用例，
// 验证 Agent 能在禁网约束下通过 bash 工具在 ./frontend 生成 React + Vite 项目骨架与指定 src/ 结构。
//
// 该需求以 Markdown 形式存储在 pkg/testcases/react_vite_todo_prompt.md，并通过 go:embed 方式读取，
// 以保证测试不依赖运行时路径与 repo root 搜索逻辑。
//
// 约束：
// - 禁止执行 npm/pnpm/yarn/curl/wget/git 等下载/联网命令（如需“npm create”，改为手动创建等价文件）
// - 写多行源码必须使用 cat <<'EOF' heredoc，避免 shell 插值破坏 JSX
// - 最终回复必须仅为 "done"（确保动作通过工具执行，而不是把脚本粘贴在回复里）
func TestIntegration_ReactViteProject(t *testing.T) {
	skipIfNoAPIKey(t)

	tmpDir := localWorkDir(t)
	system := strings.Join([]string{
		"You are a coding agent at " + tmpDir + ". Use bash to solve tasks. Act, don't explain.",
		"Network/download commands are forbidden (npm/pnpm/yarn/curl/wget/git).",
		"If the prompt asks you to initialize a project with npm create, manually create the equivalent files instead.",
		"Create the project root at ./frontend (do not create nested ./frontend/frontend).",
		"When writing source files, do NOT use echo with double quotes.",
		"Always use a single-quoted heredoc to avoid shell interpolation, for example:",
		"cat > path/to/file <<'EOF'\n...file content...\nEOF",
		"Do NOT paste shell scripts/commands in your reply. If you need to run a command, call the bash tool.",
		"After finishing, reply with the single word: done (and nothing else).",
	}, " ")
	llm, _ := newRealAgent(t)

	tc := testcases.LoadReactViteTodoPrompt()
	prompt := strings.Join([]string{
		tc,
		"",
		"Additional constraints for this run:",
		"- Project root is ./frontend (single folder; do not create frontend/frontend).",
		"- Use JavaScript (JSX) per the file structure in the spec.",
		"- Do not run npm create/install; just create files.",
		"- Create a runnable Vite + React project scaffold in frontend/, including at least: package.json and index.html (plus src/ per the spec).",
		"- Important: execute all file creation via the bash tool. Do not output command blocks/scripts in the reply.",
		"",
		"Hard requirements (must create all of these files under ./frontend):",
		"- package.json",
		"- index.html",
		"- src/main.jsx",
		"- src/App.jsx",
		"- src/App.css",
		"- src/components/TodoInput.jsx",
		"- src/components/TodoList.jsx",
		"- src/components/TodoItem.jsx",
		"- src/components/TodoFooter.jsx",
		"",
		"Before replying 'done', run a single bash command that verifies all files exist (e.g. ls -la for each, or a shell test loop).",
	}, "\n")

	reply, _ := runAgent(t, llm, system, prompt, tmpDir)
	t.Logf("Agent reply: %s", reply)

	// Reply must be exactly "done" to ensure the agent executed actions via tools, not by pasting scripts.
	if strings.TrimSpace(strings.ToLower(reply)) != "done" {
		t.Fatalf("expected reply to be exactly 'done', got %q", reply)
	}

	// Verify key files exist
	frontendDir := filepath.Join(tmpDir, "frontend")
	paths := []string{
		filepath.Join(frontendDir, "package.json"),
		filepath.Join(frontendDir, "index.html"),
		filepath.Join(frontendDir, "src", "main.jsx"),
		filepath.Join(frontendDir, "src", "App.jsx"),
		filepath.Join(frontendDir, "src", "App.css"),
		filepath.Join(frontendDir, "src", "components", "TodoInput.jsx"),
		filepath.Join(frontendDir, "src", "components", "TodoList.jsx"),
		filepath.Join(frontendDir, "src", "components", "TodoItem.jsx"),
		filepath.Join(frontendDir, "src", "components", "TodoFooter.jsx"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected %s to exist, got error: %v", p, err)
		}
	}

	appPath := filepath.Join(frontendDir, "src", "App.jsx")
	appJSX, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", appPath, err)
	}
	appLower := strings.ToLower(string(appJSX))
	if !strings.Contains(appLower, "localstorage") || !strings.Contains(string(appJSX), "todo-list-data") {
		t.Errorf("expected App.jsx to persist to localStorage key 'todo-list-data', got: %s", string(appJSX))
	}
	if !strings.Contains(appLower, "useeffect") {
		t.Errorf("expected App.jsx to use useEffect for persistence, got: %s", string(appJSX))
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
	dir := filepath.Join(root, ".local", "test-artifacts", "s01", name, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create work dir: %v", err)
	}
	return dir
}
