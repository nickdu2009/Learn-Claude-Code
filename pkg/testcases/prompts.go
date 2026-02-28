package testcases

import _ "embed"

// ReactViteTodoPromptMD is a reusable “real-world” prompt fixture for integration tests.
//
// Keep this file stable because multiple sessions may rely on it as a shared benchmark.
//
//go:embed react_vite_todo_prompt.md
var ReactViteTodoPromptMD string

func LoadReactViteTodoPrompt() string {
	return ReactViteTodoPromptMD
}
