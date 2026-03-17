package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

func SpawnTeammateToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "spawn_teammate",
			Description: openai.String("Spawn a persistent teammate that works asynchronously and can continue receiving inbox messages after the first task completes."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "Unique teammate name, for example alice or bob.",
					},
					"role": map[string]any{
						"type":        "string",
						"description": "Teammate role, for example coder or tester.",
					},
					"prompt": map[string]any{
						"type":        "string",
						"description": "The initial assignment for this teammate.",
					},
				},
				"required": []string{"name", "role", "prompt"},
			},
		},
	}
}

func ListTeammatesToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "list_teammates",
			Description: openai.String("List all teammates with their roles and statuses."),
			Parameters: openai.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

func SendMessageToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "send_message",
			Description: openai.String("Send a message to a teammate or the lead inbox."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"to": map[string]any{
						"type":        "string",
						"description": "Recipient teammate name or lead.",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "Message body.",
					},
					"msg_type": map[string]any{
						"type":        "string",
						"description": "Optional message type. Defaults to message.",
					},
				},
				"required": []string{"to", "content"},
			},
		},
	}
}

func ReadInboxToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "read_inbox",
			Description: openai.String("Read and drain your inbox."),
			Parameters: openai.FunctionParameters{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
	}
}

func BroadcastToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "broadcast",
			Description: openai.String("Broadcast a message to all teammates except the sender."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"content": map[string]any{
						"type":        "string",
						"description": "Message body.",
					},
				},
				"required": []string{"content"},
			},
		},
	}
}

func ShutdownRequestToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "shutdown_request",
			Description: openai.String("Request a teammate to shut down gracefully. Returns a request id for tracking."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"teammate": map[string]any{
						"type":        "string",
						"description": "Target teammate name.",
					},
				},
				"required": []string{"teammate"},
			},
		},
	}
}

func ShutdownResponseToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "shutdown_response",
			Description: openai.String("Respond to or inspect a shutdown request, depending on your role."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"request_id": map[string]any{
						"type":        "string",
						"description": "Shutdown request id.",
					},
					"approve": map[string]any{
						"type":        "boolean",
						"description": "Approval decision when responding as a teammate.",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Optional reason for the shutdown response.",
					},
				},
				"required": []string{"request_id"},
			},
		},
	}
}

func PlanApprovalToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "plan_approval",
			Description: openai.String("Submit a plan for approval or review a submitted plan, depending on your role."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"request_id": map[string]any{
						"type":        "string",
						"description": "Existing plan request id when reviewing as lead.",
					},
					"approve": map[string]any{
						"type":        "boolean",
						"description": "Approval decision when reviewing as lead.",
					},
					"feedback": map[string]any{
						"type":        "string",
						"description": "Optional lead feedback.",
					},
					"plan": map[string]any{
						"type":        "string",
						"description": "Plan text when submitting as a teammate.",
					},
				},
			},
		},
	}
}

func NewSpawnTeammateHandler(spawn func(context.Context, string, string, string) (string, error)) Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		if spawn == nil {
			return "", fmt.Errorf("spawn handler is not configured")
		}
		name, ok := args["name"].(string)
		if !ok || strings.TrimSpace(name) == "" {
			return "", fmt.Errorf("missing or invalid 'name' argument")
		}
		role, ok := args["role"].(string)
		if !ok || strings.TrimSpace(role) == "" {
			return "", fmt.Errorf("missing or invalid 'role' argument")
		}
		prompt, ok := args["prompt"].(string)
		if !ok || strings.TrimSpace(prompt) == "" {
			return "", fmt.Errorf("missing or invalid 'prompt' argument")
		}
		return spawn(ctx, name, role, prompt)
	}
}

func NewListTeammatesHandler(list func() (string, error)) Handler {
	return func(_ context.Context, _ map[string]any) (string, error) {
		if list == nil {
			return "", fmt.Errorf("list handler is not configured")
		}
		return list()
	}
}

func NewSendMessageHandler(send func(context.Context, string, string, string) (string, error)) Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		if send == nil {
			return "", fmt.Errorf("send handler is not configured")
		}
		to, ok := args["to"].(string)
		if !ok || strings.TrimSpace(to) == "" {
			return "", fmt.Errorf("missing or invalid 'to' argument")
		}
		content, ok := args["content"].(string)
		if !ok || strings.TrimSpace(content) == "" {
			return "", fmt.Errorf("missing or invalid 'content' argument")
		}
		msgType := "message"
		if rawMsgType, ok := args["msg_type"].(string); ok && strings.TrimSpace(rawMsgType) != "" {
			msgType = rawMsgType
		}
		return send(ctx, to, content, msgType)
	}
}

func NewReadInboxHandler(read func(context.Context) (string, error)) Handler {
	return func(ctx context.Context, _ map[string]any) (string, error) {
		if read == nil {
			return "", fmt.Errorf("read inbox handler is not configured")
		}
		return read(ctx)
	}
}

func NewBroadcastHandler(broadcast func(context.Context, string) (string, error)) Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		if broadcast == nil {
			return "", fmt.Errorf("broadcast handler is not configured")
		}
		content, ok := args["content"].(string)
		if !ok || strings.TrimSpace(content) == "" {
			return "", fmt.Errorf("missing or invalid 'content' argument")
		}
		return broadcast(ctx, content)
	}
}

func NewShutdownRequestHandler(request func(context.Context, string) (string, error)) Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		if request == nil {
			return "", fmt.Errorf("shutdown request handler is not configured")
		}
		teammate, ok := args["teammate"].(string)
		if !ok || strings.TrimSpace(teammate) == "" {
			return "", fmt.Errorf("missing or invalid 'teammate' argument")
		}
		return request(ctx, teammate)
	}
}

func NewShutdownResponseHandler(
	respond func(context.Context, string, *bool, string) (string, error),
) Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		if respond == nil {
			return "", fmt.Errorf("shutdown response handler is not configured")
		}
		requestID, ok := args["request_id"].(string)
		if !ok || strings.TrimSpace(requestID) == "" {
			return "", fmt.Errorf("missing or invalid 'request_id' argument")
		}

		var approve *bool
		if rawApprove, ok := args["approve"]; ok {
			value, ok := rawApprove.(bool)
			if !ok {
				return "", fmt.Errorf("invalid 'approve' argument")
			}
			approve = &value
		}
		reason, _ := args["reason"].(string)
		return respond(ctx, requestID, approve, reason)
	}
}

func NewPlanApprovalHandler(
	review func(context.Context, string, *bool, string, string) (string, error),
) Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		if review == nil {
			return "", fmt.Errorf("plan approval handler is not configured")
		}
		requestID, _ := args["request_id"].(string)
		feedback, _ := args["feedback"].(string)
		plan, _ := args["plan"].(string)

		var approve *bool
		if rawApprove, ok := args["approve"]; ok {
			value, ok := rawApprove.(bool)
			if !ok {
				return "", fmt.Errorf("invalid 'approve' argument")
			}
			approve = &value
		}
		return review(ctx, requestID, approve, feedback, plan)
	}
}

