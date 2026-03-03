//go:build integration

// 真实 LLM 端到端测试（E2E）。
// 运行方式：go test -v -tags=integration ./agents/s02_tool_use/
// 需要设置环境变量：DASHSCOPE_API_KEY, DASHSCOPE_BASE_URL
package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

//go:embed testdata/file_loop.md
var fixtureFileLoop string

// e2eSandboxDir 返回 E2E 测试的隔离目录，路径格式：
// .local/test-artifacts/s02/real/<testName>/<runID>/
func e2eSandboxDir(t *testing.T) string {
	t.Helper()
	// agents/s02_tool_use/ -> 向上两级到 repo root
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s02", "real", t.Name(), runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create sandbox dir %s: %v", dir, err)
	}
	return dir
}

// loadEnv 尝试加载 repo root 下的 .env 文件（忽略不存在的情况）。
func loadEnv() {
	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		return
	}
	_ = godotenv.Load(filepath.Join(repoRoot, ".env"))
}

// skipIfNoAPIKey 在缺少必要环境变量时跳过测试。
func skipIfNoAPIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("DASHSCOPE_API_KEY") == "" || os.Getenv("DASHSCOPE_BASE_URL") == "" {
		t.Skip("skipping E2E test: DASHSCOPE_API_KEY or DASHSCOPE_BASE_URL not set")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// E2E 测试
// ─────────────────────────────────────────────────────────────────────────────

// E2E-REAL-01: 验证 Agent 能够按顺序使用 list_dir -> write_file -> list_dir -> read_file
// 完成文件操作闭环，并在最终回复中返回文件内容。
func TestE2E_FileLoop(t *testing.T) {
	loadEnv()
	skipIfNoAPIKey(t)

	dir := e2eSandboxDir(t)
	t.Logf("sandbox dir: %s", dir)

	// 从 embed 读取 Prompt Fixture，替换占位符
	prompt := strings.ReplaceAll(fixtureFileLoop, "{{WORK_DIR}}", dir)

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.ListDirToolDef(), tools.ListDirHandler)

	history := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(prompt),
	}

	result, err := loop.Run(context.Background(), client, getModel(), history, registry)
	if err != nil {
		t.Fatalf("loop error: %v", err)
	}

	// 断言 1：历史记录中必须出现 write_file 和 read_file 的 ToolCall
	toolsUsed := extractToolNames(result)
	t.Logf("tools used by model: %v", toolsUsed)

	if !containsTool(toolsUsed, "write_file") {
		t.Error("expected model to use write_file, but it did not")
	}
	if !containsTool(toolsUsed, "read_file") {
		t.Error("expected model to use read_file, but it did not")
	}
	if !containsTool(toolsUsed, "list_dir") {
		t.Error("expected model to use list_dir, but it did not")
	}

	// 断言 2：物理文件必须真实存在于沙箱目录
	secretFile := filepath.Join(dir, "secret.txt")
	data, err := os.ReadFile(secretFile)
	if err != nil {
		t.Fatalf("secret.txt should have been created at %s: %v", secretFile, err)
	}
	if strings.TrimSpace(string(data)) != "42" {
		t.Errorf("secret.txt content should be '42', got: %q", string(data))
	}

	// 断言 3：最终回复中应包含文件内容 "42"
	finalReply := extractFinalReply(result)
	t.Logf("final reply: %s", finalReply)
	if !strings.Contains(finalReply, "42") {
		t.Errorf("final reply should mention '42', got: %q", finalReply)
	}
}

// E2E-REAL-02: 验证 bash 危险命令被拦截后，Agent 能感知到错误并正常结束对话。
func TestE2E_BashDangerousBlocked(t *testing.T) {
	loadEnv()
	skipIfNoAPIKey(t)

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	registry := tools.New()
	registry.Register(tools.BashToolDef(), tools.BashHandler)

	history := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("Please run the bash command: rm -rf /tmp/s02_e2e_test_nonexistent_12345. Tell me the result."),
	}

	result, err := loop.Run(context.Background(), client, getModel(), history, registry)
	if err != nil {
		t.Fatalf("loop error: %v", err)
	}

	// 验证 bash 工具的返回结果中包含拦截信息
	toolResults := extractToolResults(result)
	t.Logf("tool results: %v", toolResults)

	blocked := false
	for _, r := range toolResults {
		if strings.Contains(r, "Dangerous command blocked") {
			blocked = true
			break
		}
	}
	if !blocked {
		t.Error("expected dangerous command to be blocked in tool results")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// 辅助函数
// ─────────────────────────────────────────────────────────────────────────────

// extractToolNames 从历史记录中提取所有被调用的工具名称（去重）。
func extractToolNames(messages []openai.ChatCompletionMessageParamUnion) []string {
	seen := make(map[string]bool)
	var names []string
	for _, msg := range messages {
		if msg.OfAssistant == nil {
			continue
		}
		for _, tc := range msg.OfAssistant.ToolCalls {
			name := tc.Function.Name
			if !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
	}
	return names
}

// extractToolResults 从历史记录中提取所有 ToolMessage 的内容。
func extractToolResults(messages []openai.ChatCompletionMessageParamUnion) []string {
	var results []string
	for _, msg := range messages {
		if msg.OfTool != nil {
			results = append(results, msg.OfTool.Content.OfString.Value)
		}
	}
	return results
}

// extractFinalReply 提取历史记录中最后一条 assistant 消息的文本内容。
func extractFinalReply(messages []openai.ChatCompletionMessageParamUnion) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.OfAssistant == nil {
			continue
		}
		if msg.OfAssistant.Content.OfString.Value != "" {
			return msg.OfAssistant.Content.OfString.Value
		}
		for _, part := range msg.OfAssistant.Content.OfArrayOfContentParts {
			if part.OfText != nil && part.OfText.Text != "" {
				return part.OfText.Text
			}
		}
	}
	return ""
}

func containsTool(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}
