package ai

import (
	"encoding/json"
	"testing"
)

func TestNewOpenRouter_DefaultModel(t *testing.T) {
	p := NewOpenRouter("test-key", "", nil)
	if p.Model != "anthropic/claude-sonnet-4-6" {
		t.Errorf("default model = %q, want %q", p.Model, "anthropic/claude-sonnet-4-6")
	}
}

func TestNewOpenRouter_CustomModel(t *testing.T) {
	p := NewOpenRouter("test-key", "openai/gpt-4o", nil)
	if p.Model != "openai/gpt-4o" {
		t.Errorf("model = %q, want %q", p.Model, "openai/gpt-4o")
	}
}

func TestNewOpenRouter_ImplementsProvider(t *testing.T) {
	var _ Provider = (*OpenRouterProvider)(nil)
}

func TestVerifyOpenRouterAPIKey_WithKey(t *testing.T) {
	if err := VerifyOpenRouterAPIKey("sk-test"); err != nil {
		t.Errorf("expected no error with explicit key, got: %v", err)
	}
}

func TestVerifyOpenRouterAPIKey_Empty(t *testing.T) {
	// Clear env to ensure no fallback
	t.Setenv("OPENROUTER_API_KEY", "")
	if err := VerifyOpenRouterAPIKey(""); err == nil {
		t.Error("expected error with no key, got nil")
	}
}

func TestVerifyOpenRouterAPIKey_FromEnv(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "sk-env-test")
	if err := VerifyOpenRouterAPIKey(""); err != nil {
		t.Errorf("expected no error with env key, got: %v", err)
	}
}

func TestVerifyAPIKey_WithKey(t *testing.T) {
	if err := VerifyAPIKey("sk-test"); err != nil {
		t.Errorf("expected no error with explicit key, got: %v", err)
	}
}

func TestVerifyAPIKey_Empty(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	if err := VerifyAPIKey(""); err == nil {
		t.Error("expected error with no key, got nil")
	}
}

func TestSuggestionSchema_IsValid(t *testing.T) {
	if suggestionSchema == nil {
		t.Fatal("suggestionSchema is nil")
	}

	// Must have "properties" with our expected fields
	props, ok := suggestionSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("suggestionSchema missing properties")
	}
	for _, field := range []string{"allocations", "clarification"} {
		if _, ok := props[field]; !ok {
			t.Errorf("suggestionSchema missing property %q", field)
		}
	}
}

func TestBatchSuggestionSchema_IsValid(t *testing.T) {
	if batchSuggestionSchema == nil {
		t.Fatal("batchSuggestionSchema is nil")
	}

	props, ok := batchSuggestionSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("batchSuggestionSchema missing properties")
	}
	for _, field := range []string{"allocations", "clarification"} {
		if _, ok := props[field]; !ok {
			t.Errorf("batchSuggestionSchema missing property %q", field)
		}
	}
}

func TestSuggestionSchema_RequiredFields(t *testing.T) {
	data, err := json.Marshal(suggestionSchema)
	if err != nil {
		t.Fatalf("failed to marshal schema: %v", err)
	}

	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("failed to unmarshal schema: %v", err)
	}

	found := false
	for _, r := range schema.Required {
		if r == "allocations" {
			found = true
			break
		}
	}
	if !found {
		t.Error("suggestionSchema does not require 'allocations'")
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "pure JSON",
			input: `{"allocations":[{"project_id":"p1","minutes":60}],"clarification":""}`,
			want:  `{"allocations":[{"project_id":"p1","minutes":60}],"clarification":""}`,
		},
		{
			name:  "reasoning then JSON",
			input: "I'll match this to Backend API.\n\n" + `{"allocations":[{"project_id":"p1","minutes":60}]}`,
			want:  `{"allocations":[{"project_id":"p1","minutes":60}]}`,
		},
		{
			name:  "nested braces in strings",
			input: `{"allocations":[{"description":"Fixed {bug} in {module}"}]}`,
			want:  `{"allocations":[{"description":"Fixed {bug} in {module}"}]}`,
		},
		{
			name:  "escaped quotes in strings",
			input: `{"allocations":[{"description":"Said \"hello\""}]}`,
			want:  `{"allocations":[{"description":"Said \"hello\""}]}`,
		},
		{
			name:  "no JSON at all",
			input: "No JSON here, just text.",
			want:  "No JSON here, just text.",
		},
		{
			name:  "JSON with trailing text",
			input: `{"allocations":[]} Some extra text after`,
			want:  `{"allocations":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON() = %q, want %q", got, tt.want)
			}

			// If we expect valid JSON, verify it parses
			if tt.name != "no JSON at all" {
				var m map[string]interface{}
				if err := json.Unmarshal([]byte(got), &m); err != nil {
					t.Errorf("extracted JSON doesn't parse: %v", err)
				}
			}
		})
	}
}

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc..."},
	}

	for _, tt := range tests {
		got := truncateStr(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateStr(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
