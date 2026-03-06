//go:build integration

// 真实 LLM 端到端测试（E2E）。
// 运行方式：go test -v -tags=integration ./agents/s04_subagent/
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

//go:embed testdata/delegate_write_and_verify.md
var fixtureDelegateWriteAndVerify string

// e2eSandboxDir 返回 E2E 测试的隔离目录，路径格式：
// .local/test-artifacts/s04/real/<testName>/<runID>/
func e2eSandboxDir(t *testing.T) string {
	t.Helper()

	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s04", "real", t.Name(), runID)
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

// E2E-REAL-01: 验证父 Agent 使用 task 委派子 Agent 写文件，并由父 Agent 自行读回校验。
func TestE2E_TaskDelegationWriteAndVerify(t *testing.T) {
	loadEnv()
	skipIfNoAPIKey(t)

	dir := e2eSandboxDir(t)
	t.Logf("sandbox dir: %s", dir)

	prompt := strings.ReplaceAll(fixtureDelegateWriteAndVerify, "{{WORK_DIR}}", dir)

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	model := getModel()
	parentSystem := fmt.Sprintf(
		"You are a coding agent at %s. Use the task tool to delegate exploration or subtasks.",
		dir,
	)
	childSystem := fmt.Sprintf(
		"You are a coding subagent at %s. Complete the given task, then summarize your findings.",
		dir,
	)

	childRegistry := tools.New()
	registerBaseTools(childRegistry)

	parentRegistry := tools.New()
	registerBaseTools(parentRegistry)
	parentRegistry.Register(
		tools.TaskToolDef(),
		tools.NewTaskHandler(func(ctx context.Context, prompt string, description string) (string, error) {
			return loop.RunSubagent(ctx, client, model, childSystem, prompt, childRegistry)
		}),
	)

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(parentSystem),
		openai.UserMessage(prompt),
	}

	result, err := loop.Run(context.Background(), client, model, history, parentRegistry)
	if err != nil {
		t.Fatalf("loop error: %v", err)
	}

	toolsUsed := extractToolNames(result)
	t.Logf("tools used by parent model: %v", toolsUsed)
	if !containsTool(toolsUsed, "task") {
		t.Fatal("expected parent model to use task, but it did not")
	}
	if !containsTool(toolsUsed, "read_file") {
		t.Fatal("expected parent model to verify with read_file, but it did not")
	}

	delegatedFile := filepath.Join(dir, "delegated.txt")
	data, err := os.ReadFile(delegatedFile)
	if err != nil {
		t.Fatalf("delegated.txt should have been created at %s: %v", delegatedFile, err)
	}
	if strings.TrimSpace(string(data)) != "subagent-success" {
		t.Fatalf("delegated.txt content should be 'subagent-success', got %q", string(data))
	}

	finalReply := extractFinalReply(result)
	t.Logf("final reply: %s", finalReply)
	if !strings.Contains(strings.ToLower(finalReply), "task") {
		t.Errorf("final reply should mention task delegation, got %q", finalReply)
	}
	if !strings.Contains(finalReply, "subagent-success") {
		t.Errorf("final reply should mention exact file content, got %q", finalReply)
	}
}

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
