// Package tools provides a tool registry and dispatch mechanism for the agent loop.
package tools

import (
	"fmt"

	"github.com/openai/openai-go"
)

// Handler is the function signature for a tool implementation.
// It receives the raw JSON arguments and returns a string result or an error.
type Handler func(args map[string]any) (string, error)

// Registry holds tool definitions and their corresponding handlers.
type Registry struct {
	definitions []openai.ChatCompletionToolParam
	handlers    map[string]Handler
}

// New creates an empty Registry.
func New() *Registry {
	return &Registry{
		handlers: make(map[string]Handler),
	}
}

// Register adds a tool definition and its handler to the registry.
func (r *Registry) Register(def openai.ChatCompletionToolParam, handler Handler) {
	r.definitions = append(r.definitions, def)
	r.handlers[def.Function.Name] = handler
}

// Definitions returns the list of tool definitions for the API request.
func (r *Registry) Definitions() []openai.ChatCompletionToolParam {
	return r.definitions
}

// Dispatch executes the handler for the given tool name with the provided arguments.
func (r *Registry) Dispatch(name string, args map[string]any) (string, error) {
	handler, ok := r.handlers[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return handler(args)
}
