package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

// RunWithContextCompact executes the agent loop with the s06 three-layer
// compaction strategy: micro compact, threshold-triggered auto compact, and
// manual compact via tool call.
func RunWithContextCompact(
	ctx context.Context,
	client *openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
	registry *tools.Registry,
	opts CompactOptions,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	rec := devtools.RecorderFrom(ctx)
	provider := inferProviderFromEnv()
	useStream := isStreamingEnabled()
	opts = withCompactDefaults(opts)

	for {
		messages = MicroCompact(messages, opts.KeepRecentToolResults)
		if EstimateMessagesTokens(messages) > opts.ThresholdTokens {
			result, err := AutoCompact(ctx, client, model, messages, opts)
			if err != nil {
				return messages, err
			}
			messages = result.Messages
		}

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
			choice, resp, rawChunks, callErr = runStreaming(ctx, client, params)
		} else {
			resp, callErr = client.Chat.Completions.New(ctx, params)
			if callErr == nil {
				choice = resp.Choices[0]
			}
		}

		if callErr != nil {
			rec.FinishStep(ctx, stepID, start, nil, nil, fmt.Errorf("API call failed: %w", callErr), params, nil, nil)
			return messages, fmt.Errorf("API call failed: %w", callErr)
		}

		messages = append(messages, choice.Message.ToParam())

		output := buildViewerOutput(choice.FinishReason, choice.Message)
		usage := buildViewerUsage(resp)
		rec.FinishStep(ctx, stepID, start, output, usage, nil, params, resp, rawChunks)

		if choice.FinishReason != "tool_calls" {
			return messages, nil
		}

		manualCompact := false
		manualFocus := ""

		for _, tc := range choice.Message.ToolCalls {
			rec.RegisterToolCall(tc.ID, tc.Function.Name)

			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return messages, fmt.Errorf("failed to parse tool args for %s: %w", tc.Function.Name, err)
			}

			if tc.Function.Name == "compact" {
				focus, err := tools.CompactFocusFromArgs(args)
				if err != nil {
					return messages, err
				}
				manualCompact = true
				manualFocus = focus
			}

			toolCtx := devtools.WithParentStep(ctx, stepID)
			output, err := registry.Dispatch(toolCtx, tc.Function.Name, args)
			if err != nil {
				output = fmt.Sprintf("error: %s", err.Error())
			}

			messages = append(messages, openai.ToolMessage(output, tc.ID))
		}

		if manualCompact {
			result, err := autoCompactWithTrigger(ctx, client, model, messages, opts, "manual", manualFocus)
			if err != nil {
				return messages, err
			}
			messages = result.Messages
		}
	}
}
