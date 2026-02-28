package tools

import (
	"fmt"
	"os"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

// ReadFileToolDef returns the definition for the read_file tool.
func ReadFileToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "read_file",
			Description: openai.String("Read the contents of a file."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "The absolute or relative path to the file."},
				},
				"required": []string{"path"},
			},
		},
	}
}

// ReadFileHandler executes the read_file tool.
func ReadFileHandler(args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'path' argument")
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(content), nil
}

// WriteFileToolDef returns the definition for the write_file tool.
func WriteFileToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "write_file",
			Description: openai.String("Write content to a file. Overwrites the file if it exists."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string", "description": "The absolute or relative path to the file."},
					"content": map[string]any{"type": "string", "description": "The content to write."},
				},
				"required": []string{"path", "content"},
			},
		},
	}
}

// WriteFileHandler executes the write_file tool.
func WriteFileHandler(args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'path' argument")
	}
	content, ok := args["content"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'content' argument")
	}

	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}
	return fmt.Sprintf("Successfully wrote to %s", path), nil
}

// ListDirToolDef returns the definition for the list_dir tool.
func ListDirToolDef() openai.ChatCompletionToolParam {
	return openai.ChatCompletionToolParam{
		Type: "function",
		Function: shared.FunctionDefinitionParam{
			Name:        "list_dir",
			Description: openai.String("List contents of a directory."),
			Parameters: openai.FunctionParameters{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string", "description": "The absolute or relative path to the directory."},
				},
				"required": []string{"path"},
			},
		},
	}
}

// ListDirHandler executes the list_dir tool.
func ListDirHandler(args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok {
		return "", fmt.Errorf("missing or invalid 'path' argument")
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to list directory: %w", err)
	}

	if len(entries) == 0 {
		return "(empty directory)", nil
	}

	var result string
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.IsDir() {
			result += fmt.Sprintf("[DIR]  %s\n", info.Name())
		} else {
			result += fmt.Sprintf("[FILE] %s (%d bytes)\n", info.Name(), info.Size())
		}
	}
	return result, nil
}
