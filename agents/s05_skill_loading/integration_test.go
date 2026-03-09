//go:build integration

package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/loop"
	"github.com/nickdu2009/learn-claude-code/pkg/skills"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

//go:embed testdata/load_pdf_skill.md
var fixtureLoadPDFSkill string

//go:embed testdata/review_buggy_file.md
var fixtureReviewBuggyFile string

func TestIntegration_LoadPDFSkillOnDemand(t *testing.T) {
	_ = godotenv.Load("../../.env")
	if os.Getenv("DASHSCOPE_API_KEY") == "" || os.Getenv("DASHSCOPE_BASE_URL") == "" {
		t.Skip("DASHSCOPE_API_KEY or DASHSCOPE_BASE_URL not set, skipping real integration test")
	}

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	repoRoot, err := findRepoRoot(cwd)
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	loader, err := skills.NewLoader(filepath.Join(repoRoot, "skills"))
	if err != nil {
		t.Fatalf("failed to create loader: %v", err)
	}

	system := strings.TrimSpace(
		"You are a coding agent.\n" +
			"Before answering unfamiliar domain-specific questions, load the relevant skill first.\n\n" +
			"Skills available:\n" + loader.Descriptions(),
	)

	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(tools.LoadSkillToolDef(), tools.NewLoadSkillHandler(loader))

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(strings.TrimSpace(fixtureLoadPDFSkill)),
	}

	tracePath := enableTraceForTest(t)
	history, err = loop.RunWithManagedTrace(
		context.Background(),
		devtools.RunMeta{
			Kind:         "main",
			Title:        t.Name(),
			InputPreview: strings.TrimSpace(fixtureLoadPDFSkill),
		},
		loop.Run,
		client,
		getModel(),
		history,
		registry,
	)
	if err != nil {
		t.Fatalf("agent loop error: %v", err)
	}

	toolNames := extractToolNames(history)
	if !containsTool(toolNames, "load_skill") {
		t.Fatalf("expected model to call load_skill, got tools %v", toolNames)
	}

	trace := readIntegrationTraceFile(t, tracePath)
	if trace.Version != 2 {
		t.Fatalf("trace version = %d, want 2", trace.Version)
	}

	finalReply := extractFinalReply(history)
	t.Logf("final reply: %s", finalReply)
	if !strings.Contains(strings.ToLower(finalReply), "pdftotext") {
		t.Fatalf("final reply should mention pdftotext, got %q", finalReply)
	}
	if !strings.Contains(strings.ToLower(finalReply), "pandoc") {
		t.Fatalf("final reply should mention pandoc, got %q", finalReply)
	}
}

func TestIntegration_LoadCodeReviewSkillAndReviewFile(t *testing.T) {
	_ = godotenv.Load("../../.env")
	if os.Getenv("DASHSCOPE_API_KEY") == "" || os.Getenv("DASHSCOPE_BASE_URL") == "" {
		t.Skip("DASHSCOPE_API_KEY or DASHSCOPE_BASE_URL not set, skipping real integration test")
	}

	sandboxDir := sandboxS05Dir(t)
	targetFile := filepath.Join(sandboxDir, "buggy_review_sample.py")
	source := strings.TrimSpace(`
import os
import sqlite3

def fetch_user(conn, user_id):
    query = f"SELECT * FROM users WHERE id = {user_id}"
    return conn.execute(query).fetchall()

def run_list(path):
    os.system(f"ls {path}")
`)
	if err := os.WriteFile(targetFile, []byte(source), 0644); err != nil {
		t.Fatalf("failed to write sample file: %v", err)
	}

	client, err := newClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}

	repoRoot, err := findRepoRoot(cwd)
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	loader, err := skills.NewLoader(filepath.Join(repoRoot, "skills"))
	if err != nil {
		t.Fatalf("failed to create loader: %v", err)
	}

	system := strings.TrimSpace(
		"You are a coding agent.\n" +
			"Before doing domain-specific review work, load the relevant skill first.\n\n" +
			"Skills available:\n" + loader.Descriptions(),
	)

	prompt := strings.ReplaceAll(strings.TrimSpace(fixtureReviewBuggyFile), "{{TARGET_FILE}}", targetFile)

	registry := tools.New()
	registerBaseTools(registry)
	registry.Register(tools.LoadSkillToolDef(), tools.NewLoadSkillHandler(loader))

	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}

	tracePath := enableTraceForTest(t)
	history, err = loop.RunWithManagedTrace(
		context.Background(),
		devtools.RunMeta{
			Kind:         "main",
			Title:        t.Name(),
			InputPreview: prompt,
		},
		loop.Run,
		client,
		getModel(),
		history,
		registry,
	)
	if err != nil {
		t.Fatalf("agent loop error: %v", err)
	}

	toolNames := extractToolNames(history)
	if !containsTool(toolNames, "load_skill") {
		t.Fatalf("expected model to call load_skill, got tools %v", toolNames)
	}
	if !containsTool(toolNames, "read_file") {
		t.Fatalf("expected model to read the target file, got tools %v", toolNames)
	}

	trace := readIntegrationTraceFile(t, tracePath)
	if trace.Version != 2 {
		t.Fatalf("trace version = %d, want 2", trace.Version)
	}

	finalReply := strings.ToLower(extractFinalReply(history))
	t.Logf("final reply: %s", finalReply)
	if !strings.Contains(finalReply, "critical issues") {
		t.Fatalf("final reply should include Critical Issues section, got %q", finalReply)
	}
	if !strings.Contains(finalReply, "sql injection") {
		t.Fatalf("final reply should mention sql injection, got %q", finalReply)
	}
	if !strings.Contains(finalReply, "command injection") {
		t.Fatalf("final reply should mention command injection, got %q", finalReply)
	}
}

func sandboxS05Dir(t *testing.T) string {
	t.Helper()

	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	dir := filepath.Join(repoRoot, ".local", "test-artifacts", "s05", "real", t.Name(), runID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create sandbox dir %s: %v", dir, err)
	}
	return dir
}

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

func containsTool(names []string, target string) bool {
	for _, n := range names {
		if n == target {
			return true
		}
	}
	return false
}

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

func enableTraceForTest(t *testing.T) string {
	t.Helper()

	repoRoot, err := filepath.Abs("../../")
	if err != nil {
		t.Fatalf("failed to resolve repo root: %v", err)
	}

	traceDir := filepath.Join(repoRoot, ".devtools")
	tracePath := filepath.Join(traceDir, "generations.json")
	t.Setenv("AI_SDK_DEVTOOLS", "1")
	t.Setenv("AI_SDK_DEVTOOLS_DIR", traceDir)
	if err := os.MkdirAll(traceDir, 0755); err != nil {
		t.Fatalf("failed to create trace dir %s: %v", traceDir, err)
	}

	return tracePath
}

type integrationTraceFile struct {
	Version int             `json:"version"`
	Runs    []devtools.Run  `json:"runs"`
	Steps   []devtools.Step `json:"steps"`
}

func readIntegrationTraceFile(t *testing.T, path string) integrationTraceFile {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read trace file %s: %v", path, err)
	}
	var trace integrationTraceFile
	if err := json.Unmarshal(data, &trace); err != nil {
		t.Fatalf("failed to decode trace file %s: %v", path, err)
	}
	return trace
}
