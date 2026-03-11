package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

const (
	defaultCompactThresholdTokens   = 50000
	defaultKeepRecentToolResults    = 3
	defaultKeepRecentMessages       = 6
	defaultSummaryConversationLimit = 80000
	defaultSummaryTimeout           = 90 * time.Second
	minToolResultCompactChars       = 100
)

// CompactOptions controls the s06 three-layer compaction behavior.
type CompactOptions struct {
	// ThresholdTokens is a tutorial-style rough token budget, estimated as
	// serialized message characters / 4 instead of using a provider tokenizer.
	ThresholdTokens       int
	KeepRecentToolResults int
	KeepRecentMessages    int
	TranscriptDir         string
	SummaryCharLimit      int
	SummaryTimeout        time.Duration
}

// CompactResult captures the post-compaction state.
type CompactResult struct {
	Messages       []openai.ChatCompletionMessageParamUnion
	Summary        string
	TranscriptPath string
	Trigger        string
}

func withCompactDefaults(opts CompactOptions) CompactOptions {
	if opts.ThresholdTokens <= 0 {
		opts.ThresholdTokens = defaultCompactThresholdTokens
	}
	if opts.KeepRecentToolResults <= 0 {
		opts.KeepRecentToolResults = defaultKeepRecentToolResults
	}
	if opts.KeepRecentMessages <= 0 {
		opts.KeepRecentMessages = defaultKeepRecentMessages
	}
	if opts.SummaryCharLimit <= 0 {
		opts.SummaryCharLimit = defaultSummaryConversationLimit
	}
	if opts.SummaryTimeout <= 0 {
		opts.SummaryTimeout = defaultSummaryTimeout
	}
	return opts
}

// EstimateMessagesTokens returns a rough token estimate using the tutorial heuristic:
// around one token for every four characters in the serialized message history.
func EstimateMessagesTokens(messages []openai.ChatCompletionMessageParamUnion) int {
	data, err := json.Marshal(messages)
	if err != nil {
		rendered := fmt.Sprint(messages)
		if rendered == "" {
			return 0
		}
		return len(rendered) / 4
	}
	return len(data) / 4
}

// MicroCompact replaces older tool outputs with lightweight placeholders.
func MicroCompact(messages []openai.ChatCompletionMessageParamUnion, keepRecent int) []openai.ChatCompletionMessageParamUnion {
	if keepRecent <= 0 {
		keepRecent = defaultKeepRecentToolResults
	}

	toolNameByID := map[string]string{}
	toolIndexes := make([]int, 0, len(messages))
	for idx, msg := range messages {
		if msg.OfAssistant != nil {
			for _, tc := range msg.OfAssistant.ToolCalls {
				toolNameByID[tc.ID] = tc.Function.Name
			}
		}
		if msg.OfTool != nil {
			toolIndexes = append(toolIndexes, idx)
		}
	}

	if len(toolIndexes) <= keepRecent {
		return messages
	}

	compacted := append([]openai.ChatCompletionMessageParamUnion(nil), messages...)
	for _, idx := range toolIndexes[:len(toolIndexes)-keepRecent] {
		msg := compacted[idx]
		if msg.OfTool == nil {
			continue
		}

		content := msg.OfTool.Content.OfString.Value
		if len(content) <= minToolResultCompactChars {
			continue
		}

		toolName := toolNameByID[msg.OfTool.ToolCallID]
		if toolName == "" {
			toolName = "unknown"
		}
		compacted[idx] = openai.ToolMessage(
			fmt.Sprintf("[Previous: used %s]", toolName),
			msg.OfTool.ToolCallID,
		)
	}
	return compacted
}

// AutoCompact triggers LLM-based summarization after persisting the transcript.
func AutoCompact(
	ctx context.Context,
	client *openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
	opts CompactOptions,
) (CompactResult, error) {
	return autoCompactWithTrigger(ctx, client, model, messages, opts, "auto", "")
}

func autoCompactWithTrigger(
	ctx context.Context,
	client *openai.Client,
	model string,
	messages []openai.ChatCompletionMessageParamUnion,
	opts CompactOptions,
	trigger string,
	focus string,
) (CompactResult, error) {
	opts = withCompactDefaults(opts)

	store := TranscriptStore{Dir: opts.TranscriptDir}
	transcriptPath, err := store.Save(messages)
	if err != nil {
		return CompactResult{}, err
	}

	serialized, err := json.Marshal(messages)
	if err != nil {
		return CompactResult{}, fmt.Errorf("failed to serialize messages for summary: %w", err)
	}
	conversation := string(serialized)
	if len(conversation) > opts.SummaryCharLimit {
		conversation = conversation[:opts.SummaryCharLimit]
	}

	prompt := buildCompactPrompt(conversation, trigger, focus)
	summaryCtx, cancel := newSummaryRequestContext(ctx, opts.SummaryTimeout)
	defer cancel()

	resp, err := client.Chat.Completions.New(summaryCtx, openai.ChatCompletionNewParams{
		Model: shared.ChatModel(model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return CompactResult{}, fmt.Errorf("failed to summarize compacted conversation: %w", err)
	}
	if len(resp.Choices) == 0 {
		return CompactResult{}, fmt.Errorf("summary response did not contain choices")
	}

	summary := strings.TrimSpace(assistantSummary(resp.Choices[0].Message))
	if summary == "" {
		return CompactResult{}, fmt.Errorf("summary response was empty")
	}

	compressed := buildCompressedMessages(
		messages,
		summary,
		transcriptPath,
		trigger,
		opts.KeepRecentMessages,
		opts.ThresholdTokens,
	)
	return CompactResult{
		Messages:       compressed,
		Summary:        summary,
		TranscriptPath: transcriptPath,
		Trigger:        trigger,
	}, nil
}

func buildCompactPrompt(conversation, trigger, focus string) string {
	lines := []string{
		"Summarize this coding-agent conversation for continuity.",
		"Use the exact headings below and keep the summary concise but specific:",
		"Goal",
		"Completed",
		"CurrentState",
		"Decisions",
		"Constraints",
		"NextSteps",
		fmt.Sprintf("CompactionTrigger: %s", trigger),
	}
	if focus = strings.TrimSpace(focus); focus != "" {
		lines = append(lines, fmt.Sprintf("Focus: %s", focus))
	}
	lines = append(lines, "", "Conversation JSON:", conversation)
	return strings.Join(lines, "\n")
}

func buildCompressedMessages(
	original []openai.ChatCompletionMessageParamUnion,
	summary string,
	transcriptPath string,
	trigger string,
	keepRecentMessages int,
	thresholdTokens int,
) []openai.ChatCompletionMessageParamUnion {
	instructions, remainder := splitInstructionMessages(original)
	tail := keepTailMessages(remainder, keepRecentMessages)

	result := make([]openai.ChatCompletionMessageParamUnion, 0, len(instructions)+len(tail)+2)
	result = append(result, instructions...)
	result = append(result, openai.UserMessage(
		fmt.Sprintf("[Conversation compressed via %s compact. Transcript: %s]\n\n%s", trigger, transcriptPath, summary),
	))
	result = append(result, openai.AssistantMessage("Understood. I have the compressed context. Continuing."))
	result = append(result, tail...)

	if thresholdTokens > 0 {
		baseLen := len(result) - len(tail)
		for len(tail) > 0 && EstimateMessagesTokens(result) > thresholdTokens {
			tail = tail[1:]
			for len(tail) > 0 && !isValidMessageSuffix(tail) {
				tail = tail[1:]
			}
			result = append(result[:baseLen], tail...)
		}
	}
	return result
}

func splitInstructionMessages(messages []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, []openai.ChatCompletionMessageParamUnion) {
	splitAt := 0
	for splitAt < len(messages) {
		msg := messages[splitAt]
		if msg.OfSystem != nil || msg.OfDeveloper != nil {
			splitAt++
			continue
		}
		break
	}
	return append([]openai.ChatCompletionMessageParamUnion(nil), messages[:splitAt]...),
		append([]openai.ChatCompletionMessageParamUnion(nil), messages[splitAt:]...)
}

func keepTailMessages(messages []openai.ChatCompletionMessageParamUnion, keepRecent int) []openai.ChatCompletionMessageParamUnion {
	if keepRecent <= 0 || len(messages) <= keepRecent {
		return append([]openai.ChatCompletionMessageParamUnion(nil), messages...)
	}

	start := len(messages) - keepRecent
	for start > 0 && !isValidMessageSuffix(messages[start:]) {
		start--
	}
	return append([]openai.ChatCompletionMessageParamUnion(nil), messages[start:]...)
}

func isValidMessageSuffix(messages []openai.ChatCompletionMessageParamUnion) bool {
	seenToolCalls := make(map[string]struct{})
	for _, msg := range messages {
		if msg.OfAssistant != nil {
			for _, tc := range msg.OfAssistant.ToolCalls {
				seenToolCalls[tc.ID] = struct{}{}
			}
		}
		if msg.OfTool != nil {
			if _, ok := seenToolCalls[msg.OfTool.ToolCallID]; !ok {
				return false
			}
		}
	}
	return true
}

func newSummaryRequestContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	base := context.Background()
	if parent != nil {
		base = context.WithoutCancel(parent)
	}
	if timeout > 0 {
		return context.WithTimeout(base, timeout)
	}
	return context.WithCancel(base)
}
