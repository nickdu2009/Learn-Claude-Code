// Package qwen wraps the OpenAI-compatible Qwen API client.
package qwen

import (
	"fmt"
	"os"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const defaultModel = "qwen-plus"

// NewClient creates an OpenAI client pointed at DashScope's compatible endpoint.
// Required env vars: DASHSCOPE_API_KEY, DASHSCOPE_BASE_URL
func NewClient() (*openai.Client, error) {
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

// Model returns the model name from env, falling back to defaultModel.
func Model() string {
	if m := os.Getenv("DASHSCOPE_MODEL"); m != "" {
		return m
	}
	return defaultModel
}
