// s01: The Agent Loop
// Motto: "One loop & Bash is all you need"
//
// The entire secret of an AI coding agent in one pattern:
//
//	for stop_reason == "tool_calls" {
//	    response = LLM(messages, tools)
//	    execute tools
//	    append results
//	}
//
// +----------+    +-------+    +---------+
// | User     | -> | LLM   | -> | Tool    |
// | prompt   |    |       |    | execute |
// +----------+    +---+---+    +----+----+
//
//	^              |
//	| tool_result  |
//	+--------------+
//	  (loop continues)
//
// This is the core loop: feed tool results back to the model
// until the model decides to stop.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// ANSI 颜色码
const (
	colorCyan   = "\033[36m"
	colorYellow = "\033[33m"
	colorReset  = "\033[0m"
)

var dangerousPatterns = []string{
	"rm -rf /", "sudo", "shutdown", "reboot", "> /dev/",
}

// LLMClient 抽象 LLM 调用，便于单元测试时注入 mock。
type LLMClient interface {
	Complete(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error)
}

// realLLMClient 包装真实的 openai.Client。
type realLLMClient struct {
	client *openai.Client
	model  string
}

func (r *realLLMClient) Complete(ctx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
	params.Model = shared.ChatModel(r.model)
	return r.client.Chat.Completions.New(ctx, params)
}

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "no .env file found, using system env")
	}

	client, err := newClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	llm := &realLLMClient{client: client, model: getModel()}
	cwd, _ := os.Getwd()
	system := fmt.Sprintf("You are a coding agent at %s. Use bash to solve tasks. Act, don't explain.", cwd)

	// DevTools recorder: keep one run for the whole REPL session
	rec := devtools.NewRunRecorderFromEnv()

	// 持久化对话历史，跨轮次保留上下文
	history := []openai.ChatCompletionMessageParamUnion{}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("%ss01 >> %s", colorCyan, colorReset)
		if !scanner.Scan() {
			break
		}
		query := strings.TrimSpace(scanner.Text())
		if query == "" || query == "q" || query == "exit" {
			break
		}

		history = append(history, openai.UserMessage(query))
		history = agentLoop(llm, system, history, "", rec)

		// 打印最终回复
		last := history[len(history)-1]
		if last.OfAssistant != nil {
			content := last.OfAssistant.Content
			if content.OfString.Value != "" {
				fmt.Println(content.OfString.Value)
			}
			for _, part := range content.OfArrayOfContentParts {
				if part.OfText != nil {
					fmt.Println(part.OfText.Text)
				}
			}
		}
		fmt.Println()
	}
}

// agentLoop 是核心循环：调用 LLM → 检测 tool_calls → 执行工具 → 追加结果 → 循环。
// workDir 指定 bash 命令的工作目录，为空时使用当前进程工作目录。
func agentLoop(
	llm LLMClient,
	system string,
	messages []openai.ChatCompletionMessageParamUnion,
	workDir string,
	rec *devtools.RunRecorder,
) []openai.ChatCompletionMessageParamUnion {
	provider := inferProviderFromEnv()
	modelID := ""
	if r, ok := llm.(*realLLMClient); ok {
		modelID = r.model
	}

	cwd := ""
	if workDir != "" {
		cwd = workDir
	}

	for {
		// system prompt 作为首条消息传入（OpenAI 协议）
		fullMessages := append([]openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(system),
		}, messages...)

		stepID, start := "", time.Time{}
		if rec != nil {
			stepID, start = rec.StartStep(context.Background(), "generate", modelID, provider, fullMessages, []openai.ChatCompletionToolParam{bashToolDef()}, map[string]any{
				"baseURL": os.Getenv("DASHSCOPE_BASE_URL"),
			})
		}

		params := openai.ChatCompletionNewParams{
			Messages: fullMessages,
			Tools:    []openai.ChatCompletionToolParam{bashToolDef()},
		}
		resp, err := llm.Complete(context.Background(), params)
		if err != nil {
			if rec != nil {
				rec.FinishStep(context.Background(), stepID, start, nil, nil, err, params, nil, nil)
			}
			fmt.Fprintln(os.Stderr, "API error:", err)
			return messages
		}

		choice := resp.Choices[0]
		messages = append(messages, choice.Message.ToParam())

		if rec != nil {
			output := buildViewerOutput(choice.FinishReason, choice.Message)
			usage := buildViewerUsage(resp)
			rec.FinishStep(context.Background(), stepID, start, output, usage, nil, params, resp, nil)
		}

		// 没有工具调用时，模型返回最终文本，循环结束
		if choice.FinishReason != "tool_calls" {
			return messages
		}

		// 执行每个工具调用，收集结果
		for _, tc := range choice.Message.ToolCalls {
			if rec != nil {
				rec.RegisterToolCall(tc.ID, tc.Function.Name)
			}
			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				messages = append(messages, openai.ToolMessage(fmt.Sprintf("error: %s", err), tc.ID))
				continue
			}

			command, _ := args["command"].(string)
			fmt.Printf("%s$ %s%s\n", colorYellow, command, colorReset)

			output := runBashIn(command, cwd)
			preview := output
			if len(preview) > 200 {
				preview = preview[:200]
			}
			fmt.Println(preview)

			messages = append(messages, openai.ToolMessage(output, tc.ID))
		}
	}
}

func inferProviderFromEnv() string {
	baseURL := os.Getenv("DASHSCOPE_BASE_URL")
	if baseURL == "" {
		return ""
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Host)
	switch {
	case strings.Contains(host, "dashscope"):
		return "dashscope"
	case strings.Contains(host, "aliyun"):
		return "aliyun"
	default:
		if host != "" {
			return host
		}
		return ""
	}
}

func buildViewerUsage(resp *openai.ChatCompletion) any {
	if resp == nil || (resp.Usage.PromptTokens == 0 && resp.Usage.CompletionTokens == 0 && resp.Usage.TotalTokens == 0) {
		return nil
	}
	return map[string]any{
		"inputTokens":  resp.Usage.PromptTokens,
		"outputTokens": resp.Usage.CompletionTokens,
		"totalTokens":  resp.Usage.TotalTokens,
		"raw":          resp.Usage,
	}
}

func buildViewerOutput(finishReason string, msg openai.ChatCompletionMessage) any {
	fr := finishReason
	if fr == "tool_calls" {
		fr = "tool-calls"
	}

	parts := make([]map[string]any, 0, 4)
	if strings.TrimSpace(msg.Content) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": msg.Content,
		})
	}

	toolCalls := make([]map[string]any, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		call := map[string]any{
			"type":       "tool-call",
			"toolName":   tc.Function.Name,
			"toolCallId": tc.ID,
			"args":       tc.Function.Arguments,
		}
		toolCalls = append(toolCalls, call)
		parts = append(parts, call)
	}

	out := map[string]any{
		"finishReason": fr,
		"content":      parts,
	}
	if len(toolCalls) > 0 {
		out["toolCalls"] = toolCalls
	}
	return out
}

// runBash 执行 shell 命令，工作目录为当前进程目录。
func runBash(command string) string {
	return runBashIn(command, "")
}

// runBashIn 执行 shell 命令，拦截危险指令，限制输出长度。
// dir 为空时使用当前进程工作目录。
func runBashIn(command, dir string) string {
	for _, pattern := range dangerousPatterns {
		if strings.Contains(command, pattern) {
			return "Error: Dangerous command blocked"
		}
	}

	cmd := exec.Command("bash", "-c", command)
	if dir != "" {
		cmd.Dir = dir
	} else {
		cmd.Dir, _ = os.Getwd()
	}
	out, err := cmd.CombinedOutput()

	result := strings.TrimSpace(string(out))
	if err != nil && result == "" {
		result = fmt.Sprintf("Error: %s", err)
	}
	if result == "" {
		result = "(no output)"
	}
	// 截断超长输出，防止撑爆上下文
	if len(result) > 50000 {
		result = result[:50000]
	}
	return result
}

func bashToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "bash",
			Description: openai.String("Run a shell command."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
				"required": []string{"command"},
			},
		},
	}
}

func newClient() (*openai.Client, error) {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("DASHSCOPE_API_KEY is not set")
	}
	baseURL := os.Getenv("DASHSCOPE_BASE_URL")
	if baseURL == "" {
		return nil, fmt.Errorf("DASHSCOPE_BASE_URL is not set")
	}
	c := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	)
	return &c, nil
}

func getModel() string {
	if m := os.Getenv("DASHSCOPE_MODEL"); m != "" {
		return m
	}
	return "qwen-plus"
}
