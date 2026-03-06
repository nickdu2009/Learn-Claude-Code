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

const (
	defaultTodoNagEveryRounds = 3
	defaultTodoNagMessage     = "Update your todos."
)

// RunWithTodoNag executes an agent loop like Run, but injects a nag reminder
// when the model goes N rounds without calling the todo tool.
//
// The Recorder is obtained from ctx via devtools.RecorderFrom (see Run).
// For one-shot top-level tasks that want automatic BeginRun/FinishRun
// management, prefer wrapping this runner with RunWithManagedTrace.
func RunWithTodoNag(
	ctx context.Context,
	client *openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
	registry *tools.Registry,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	rec := devtools.RecorderFrom(ctx)
	provider := inferProviderFromEnv()
	useStream := isStreamingEnabled()

	roundsSinceTodo := 0

	for {
		// Inject reminder before the model call (OpenAI-style messages).
		if roundsSinceTodo >= defaultTodoNagEveryRounds {
			messages = append(messages, openai.UserMessage(defaultTodoNagMessage))
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

		// No tool calls → final text, loop ends.
		if choice.FinishReason != "tool_calls" {
			return messages, nil
		}

		usedTodoThisRound := false

		// Execute all tool calls and append tool results.
		for _, tc := range choice.Message.ToolCalls {
			rec.RegisterToolCall(tc.ID, tc.Function.Name)

			if tc.Function.Name == "todo" {
				usedTodoThisRound = true
			}

			var args map[string]any
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return messages, fmt.Errorf("failed to parse tool args for %s: %w", tc.Function.Name, err)
			}

			output, err := registry.Dispatch(ctx, tc.Function.Name, args)
			if err != nil {
				output = fmt.Sprintf("error: %s", err.Error())
			}

			messages = append(messages, openai.ToolMessage(output, tc.ID))
		}

		if usedTodoThisRound {
			roundsSinceTodo = 0
		} else {
			roundsSinceTodo++
		}
	}
}
