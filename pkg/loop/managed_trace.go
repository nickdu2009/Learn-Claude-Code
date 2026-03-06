package loop

import (
	"context"
	"fmt"

	"github.com/nickdu2009/learn-claude-code/pkg/devtools"
	"github.com/nickdu2009/learn-claude-code/pkg/tools"
	"github.com/openai/openai-go"
)

// AgentRunner matches the shared signature of loop runners like Run and
// RunWithTodoNag so callers can opt into managed trace lifecycle.
type AgentRunner func(
	ctx context.Context,
	client *openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
	registry *tools.Registry,
) ([]openai.ChatCompletionMessageParamUnion, error)

// RunWithManagedTrace wraps a single runner invocation in BeginRun/FinishRun.
//
// Recorder selection:
//   - Use the recorder already attached to ctx when present
//   - Otherwise create one from environment variables via devtools.NewRecorderFromEnv
//
// This helper is intended for one-shot runs such as integration tests or
// single-request task execution. Long-lived interactive sessions should still
// manage run lifecycle explicitly at the application layer.
func RunWithManagedTrace(
	ctx context.Context,
	meta devtools.RunMeta,
	runner AgentRunner,
	client *openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
	registry *tools.Registry,
) ([]openai.ChatCompletionMessageParamUnion, error) {
	rec := devtools.RecorderFrom(ctx)
	if rec.RunID() == "" {
		rec = devtools.NewRecorderFromEnv()
	}

	traceCtx := devtools.WithRecorder(ctx, rec)
	if err := rec.BeginRun(traceCtx, meta); err != nil {
		return messages, fmt.Errorf("begin trace run: %w", err)
	}

	history, runErr := runner(traceCtx, client, model, messages, registry)

	result := devtools.RunResult{
		Status:           "completed",
		CompletionReason: "normal",
	}
	if runErr != nil {
		result.Status = "failed"
		result.CompletionReason = "error"
		result.Error = runErr.Error()
	}

	if err := rec.FinishRun(traceCtx, result); err != nil {
		if runErr != nil {
			return history, fmt.Errorf("%w (finish trace run: %v)", runErr, err)
		}
		return history, fmt.Errorf("finish trace run: %w", err)
	}

	return history, runErr
}
