package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

const DefaultSubagentMaxRounds = 30

type chatCompletionCaller func(context.Context, openai.ChatCompletionNewParams) (*openai.ChatCompletion, error)

// RunSubagent executes a delegated task with fresh messages and returns only the
// final summary text to the parent.
func RunSubagent(
	ctx context.Context,
	client *openai.Client,
	model string,
	systemPrompt string,
	prompt string,
	registry *tools.Registry,
) (string, error) {
	return RunSubagentWithLimit(ctx, client, model, systemPrompt, prompt, registry, DefaultSubagentMaxRounds)
}

// RunSubagentWithLimit is like RunSubagent, but allows callers to set a custom
// safety limit for tests and experiments.
func RunSubagentWithLimit(
	ctx context.Context,
	client *openai.Client,
	model string,
	systemPrompt string,
	prompt string,
	registry *tools.Registry,
	maxRounds int,
) (string, error) {
	return runSubagentWithCaller(
		ctx,
		client,
		model,
		systemPrompt,
		prompt,
		registry,
		maxRounds,
		func(callCtx context.Context, params openai.ChatCompletionNewParams) (*openai.ChatCompletion, error) {
			return client.Chat.Completions.New(callCtx, params)
		},
	)
}

func runSubagentWithCaller(
	ctx context.Context,
	client *openai.Client,
	model string,
	systemPrompt string,
	prompt string,
	registry *tools.Registry,
	maxRounds int,
	call chatCompletionCaller,
) (string, error) {
	if registry == nil {
		return "", fmt.Errorf("subagent registry is nil")
	}
	if call == nil {
		return "", fmt.Errorf("subagent caller is nil")
	}
	if strings.TrimSpace(prompt) == "" {
		return "", fmt.Errorf("subagent prompt is empty")
	}
	if maxRounds <= 0 {
		maxRounds = DefaultSubagentMaxRounds
	}

	rec := devtools.RecorderFrom(ctx)
	provider := inferProviderFromEnv()
	useStream := isStreamingEnabled()
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(systemPrompt),
		openai.UserMessage(prompt),
	}

	for round := 0; round < maxRounds; round++ {
		params := openai.ChatCompletionNewParams{
			Model:    shared.ChatModel(model),
			Messages: messages,
			Tools:    registry.Definitions(),
		}

		providerOpts := map[string]any{"baseURL": os.Getenv("DASHSCOPE_BASE_URL")}
		stepType := "generate"
		if useStream {
			stepType = "stream"
		}

		stepID, start := rec.StartStep(ctx, stepType, model, provider, messages, registry.Definitions(), providerOpts, params)

		var (
			choice    openai.ChatCompletionChoice
			resp      *openai.ChatCompletion
			rawChunks any
			callErr   error
		)

		if useStream {
			if client == nil {
				err := fmt.Errorf("streaming subagent requires a client")
				_ = rec.FinishRun(ctx, devtools.RunResult{
					Status:           "error",
					CompletionReason: "error",
					Summary:          err.Error(),
					Error:            err.Error(),
				})
				return "", err
			}
			choice, resp, rawChunks, callErr = runStreaming(ctx, client, params)
		} else {
			resp, callErr = call(ctx, params)
			if callErr == nil {
				choice = resp.Choices[0]
			}
		}

		if callErr != nil {
			rec.FinishStep(ctx, stepID, start, nil, nil, fmt.Errorf("API call failed: %w", callErr), params, nil, nil)
			err := fmt.Errorf("API call failed: %w", callErr)
			_ = rec.FinishRun(ctx, devtools.RunResult{
				Status:           "error",
				CompletionReason: "error",
				Summary:          err.Error(),
				Error:            err.Error(),
			})
			return "", err
		}

		messages = append(messages, choice.Message.ToParam())

		output := buildViewerOutput(choice.FinishReason, choice.Message)
		usage := buildViewerUsage(resp)
		rec.FinishStep(ctx, stepID, start, output, usage, nil, params, resp, rawChunks)

		if choice.FinishReason != "tool_calls" {
			summary := assistantSummary(choice.Message)
			_ = rec.FinishRun(ctx, devtools.RunResult{
				Status:           "completed",
				CompletionReason: "normal",
				Summary:          summary,
			})
			return summary, nil
		}

		for _, tc := range choice.Message.ToolCalls {
			rec.RegisterToolCall(tc.ID, tc.Function.Name)

			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				runErr := fmt.Errorf("failed to parse tool args for %s: %w", tc.Function.Name, err)
				_ = rec.FinishRun(ctx, devtools.RunResult{
					Status:           "error",
					CompletionReason: "error",
					Summary:          runErr.Error(),
					Error:            runErr.Error(),
				})
				return "", runErr
			}

			toolCtx := devtools.WithParentStep(ctx, stepID)
			output, err := registry.Dispatch(toolCtx, tc.Function.Name, args)
			if err != nil {
				output = fmt.Sprintf("error: %s", err.Error())
			}

			messages = append(messages, openai.ToolMessage(output, tc.ID))
		}
	}

	summary := fmt.Sprintf("Subagent stopped after reaching the safety limit (%d rounds).", maxRounds)
	_ = rec.FinishRun(ctx, devtools.RunResult{
		Status:           "completed",
		CompletionReason: "safety-limit",
		Summary:          summary,
	})
	return summary, nil
}

func assistantSummary(msg openai.ChatCompletionMessage) string {
	if strings.TrimSpace(msg.Content) != "" {
		return msg.Content
	}
	return "(no summary)"
}
