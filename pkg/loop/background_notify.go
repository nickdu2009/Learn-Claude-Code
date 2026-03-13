package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nickdu2009/learn-claude-code/pkg/background"
	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

type BackgroundNotificationSource interface {
	DrainNotifications() []background.Notification
}

func RunWithBackgroundNotifications(source BackgroundNotificationSource) AgentRunner {
	return func(
		ctx context.Context,
		client *openai.Client,
		model string,
		messages []openai.ChatCompletionMessageParamUnion,
		registry *tools.Registry,
	) ([]openai.ChatCompletionMessageParamUnion, error) {
		rec := devtools.RecorderFrom(ctx)
		provider := inferProviderFromEnv()
		useStream := isStreamingEnabled()

		for {
			if source != nil {
				notifications := source.DrainNotifications()
				if len(notifications) > 0 {
					messages = append(messages, buildBackgroundNotificationMessages(notifications)...)
				}
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

			for _, tc := range choice.Message.ToolCalls {
				rec.RegisterToolCall(tc.ID, tc.Function.Name)
				var args map[string]any
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
					return messages, fmt.Errorf("failed to parse tool args for %s: %w", tc.Function.Name, err)
				}

				toolCtx := devtools.WithParentStep(ctx, stepID)
				output, err := registry.Dispatch(toolCtx, tc.Function.Name, args)
				if err != nil {
					output = fmt.Sprintf("error: %s", err.Error())
				}

				messages = append(messages, openai.ToolMessage(output, tc.ID))
			}
		}
	}
}

func buildBackgroundNotificationMessages(notifications []background.Notification) []openai.ChatCompletionMessageParamUnion {
	if len(notifications) == 0 {
		return nil
	}

	lines := make([]string, 0, len(notifications))
	for _, notification := range notifications {
		lines = append(lines, fmt.Sprintf(
			"[bg:%s] %s: %s",
			notification.TaskID,
			notification.Status,
			strings.TrimSpace(notification.Summary),
		))
	}

	content := "<background-results>\n" + strings.Join(lines, "\n") + "\n</background-results>"
	return []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(content),
		openai.AssistantMessage("Noted background results."),
	}
}
