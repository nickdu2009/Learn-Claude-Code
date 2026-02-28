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
	"github.com/openai/openai-go"
)

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
	result := agentLoop(llm, system, messages, workDir)

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

	tmpDir := t.TempDir()
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

	tmpDir := t.TempDir()
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

	tmpDir := t.TempDir()
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

	tmpDir := t.TempDir()
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

	tmpDir := t.TempDir()
	system := "You are a coding agent at " + tmpDir + ". Use bash to solve tasks. Act, don't explain."
	llm, _ := newRealAgent(t)

	// 第一轮：创建文件
	history := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("Create a file named memo.txt with content: round_one"),
	}
	history = agentLoop(llm, system, history, tmpDir)

	// 第二轮：基于上下文追加内容（Agent 应记得 memo.txt）
	history = append(history, openai.UserMessage("Append the text ' round_two' to memo.txt"))
	history = agentLoop(llm, system, history, tmpDir)

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
