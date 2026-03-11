package tools

import (
	"context"
	"strings"
	"testing"
)

func TestCompactToolDef_DeclaresFocusMaxLength(t *testing.T) {
	def := CompactToolDef()
	properties, ok := def.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties should be a map, got %T", def.Function.Parameters["properties"])
	}
	focus, ok := properties["focus"].(map[string]any)
	if !ok {
		t.Fatalf("focus should be a map, got %T", properties["focus"])
	}
	if got := focus["maxLength"]; got != maxCompactFocusLength {
		t.Fatalf("maxLength = %v, want %d", got, maxCompactFocusLength)
	}
}

func TestNewCompactHandler_AllowsEmptyFocus(t *testing.T) {
	handler := NewCompactHandler()

	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Manual compaction requested") {
		t.Fatalf("unexpected result: %q", result)
	}
}

func TestNewCompactHandler_ValidatesFocusType(t *testing.T) {
	handler := NewCompactHandler()

	_, err := handler(context.Background(), map[string]any{"focus": 123})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "focus") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCompactFocusFromArgs_TrimsFocus(t *testing.T) {
	focus, err := CompactFocusFromArgs(map[string]any{"focus": "  preserve pending edits  "})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if focus != "preserve pending edits" {
		t.Fatalf("got %q, want trimmed focus", focus)
	}
}
