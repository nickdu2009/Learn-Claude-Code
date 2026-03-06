package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

var dangerousPatterns = []string{
	"rm -rf /", "sudo", "shutdown", "reboot", "> /dev/",
}

// BashToolDef returns the definition for the bash tool.
func BashToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "bash",
			Description: openai.String("Run a shell command."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"command": map[string]any{"type": "string"},
				},
				"required": []string{"command"},
			},
		},
	}
}

// BashHandler executes the bash command.
func BashHandler(ctx context.Context, args map[string]any) (string, error) {
	command, ok := args["command"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'command' argument")
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(command, pattern) {
			return "Error: Dangerous command blocked", nil
		}
	}

	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir, _ = os.Getwd() // Default to current working directory
	out, err := cmd.CombinedOutput()

	result := strings.TrimSpace(string(out))
	if err != nil && result == "" {
		result = fmt.Sprintf("Error: %s", err)
	}
	if result == "" {
		result = "(no output)"
	}
	if len(result) > 50000 {
		result = result[:50000]
	}
	return result, nil
}
