// s05: Skills
// Motto: "Load knowledge when you need it, not upfront"
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/skills"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const (
	colorCyan  = "\033[36m"
	colorReset = "\033[0m"
)

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "no .env file found, using system env")
	}

	client, err := newClient()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	repoRoot, err := findRepoRoot(cwd)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	loader, err := skills.NewLoader(filepath.Join(repoRoot, "skills"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	model := getModel()
	system := fmt.Sprintf(
		"You are a coding agent at %s.\n"+
			"Use load_skill to access specialized knowledge before tackling unfamiliar topics.\n\n"+
			"Skills available:\n%s",
		cwd,
		loader.Descriptions(),
	)

	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(tools.LoadSkillToolDef(), tools.NewLoadSkillHandler(loader))

	rec := devtools.NewRecorderFromEnv()
	_ = rec.BeginRun(context.Background(), devtools.RunMeta{
		Kind:  "main",
		Title: "s05 skills agent",
	})
	defer func() {
		_ = rec.FinishRun(context.Background(), devtools.RunResult{
			Status:           "completed",
			CompletionReason: "normal",
		})
	}()

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("%ss05 >> %s", colorCyan, colorReset)
		if !scanner.Scan() {
			break
		}

		query := strings.TrimSpace(scanner.Text())
		if query == "" || query == "q" || query == "exit" {
			break
		}

		history = append(history, openai.UserMessage(query))
		ctx := devtools.WithRecorder(context.Background(), rec)
		history, err = loop.Run(ctx, client, model, history, registry)
		if err != nil {
			fmt.Fprintln(os.Stderr, "loop error:", err)
			continue
		}

		printAssistantReply(history[len(history)-1])
		fmt.Println()
	}
}

func registerBaseTools(registry *tools.Registry) {
	registry.Register(tools.BashToolDef(), tools.BashHandler)
	registry.Register(tools.ReadFileToolDef(), tools.ReadFileHandler)
	registry.Register(tools.WriteFileToolDef(), tools.WriteFileHandler)
	registry.Register(tools.EditFileToolDef(), tools.EditFileHandler)
	registry.Register(tools.ListDirToolDef(), tools.ListDirHandler)
}

func printAssistantReply(message openai.ChatCompletionMessageParamUnion) {
	if message.OfAssistant == nil {
		return
	}

	content := message.OfAssistant.Content
	if content.OfString.Value != "" {
		fmt.Println(content.OfString.Value)
	}
	for _, part := range content.OfArrayOfContentParts {
		if part.OfText != nil {
			fmt.Println(part.OfText.Text)
		}
	}
}

func findRepoRoot(start string) (string, error) {
	dir := filepath.Clean(start)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("failed to locate repository root from %s", start)
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

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	)
	return &client, nil
}

func getModel() string {
	if m := os.Getenv("DASHSCOPE_MODEL"); m != "" {
		return m
	}
	return "qwen-plus"
}
