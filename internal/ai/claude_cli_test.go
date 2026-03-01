package ai

import (
	"encoding/json"
	"testing"
)

// dispatchEvent simulates the switch statement logic from runStreamingCLI.
// Returns resultText if this was a result event.
func dispatchEvent(event streamEvent, onThinking func(string)) string {
	var resultText string

	switch event.Type {
	case "stream_event":
		if event.Event.Type == "content_block_delta" && event.Event.Delta.Type == "text_delta" {
			if event.Event.Delta.Text != "" {
				onThinking(event.Event.Delta.Text)
			}
		}
	case "content_block_delta":
		if event.Delta.Text != "" {
			onThinking(event.Delta.Text)
		}
	case "assistant":
		for _, block := range event.Message.Content {
			if block.Type == "text" && block.Text != "" {
				onThinking(block.Text)
			}
		}
	case "result":
		if len(event.StructuredOutput) > 0 && event.StructuredOutput[0] == '{' {
			resultText = string(event.StructuredOutput)
		} else if len(event.Result) > 0 {
			var s string
			if err := json.Unmarshal(event.Result, &s); err == nil {
				resultText = s
			} else {
				resultText = string(event.Result)
			}
		}
	}

	return resultText
}

func TestStreamEventParsing(t *testing.T) {
	tests := []struct {
		name             string
		jsonLine         string
		wantType         string
		wantThinkingText string
		wantResultText   string
	}{
		{
			name:     "system event is ignored",
			jsonLine: `{"type":"system","subtype":"init","session_id":"abc123"}`,
			wantType: "system",
		},
		{
			name:             "stream_event with content_block_delta text_delta",
			jsonLine:         `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello "}}}`,
			wantType:         "stream_event",
			wantThinkingText: "Hello ",
		},
		{
			name:     "stream_event with non-text delta is ignored",
			jsonLine: `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"input_json_delta","partial_json":"{"}}}`,
			wantType: "stream_event",
		},
		{
			name:     "stream_event with non-delta event type is ignored",
			jsonLine: `{"type":"stream_event","event":{"type":"content_block_start"}}`,
			wantType: "stream_event",
		},
		{
			name:             "assistant event with text content",
			jsonLine:         `{"type":"assistant","message":{"content":[{"type":"text","text":"Full response text"}]}}`,
			wantType:         "assistant",
			wantThinkingText: "Full response text",
		},
		{
			name:     "assistant event with empty content",
			jsonLine: `{"type":"assistant","message":{"content":[]}}`,
			wantType: "assistant",
		},
		{
			name:           "result event with structured_output",
			jsonLine:       `{"type":"result","subtype":"success","result":"some text","structured_output":{"allocations":[{"project_id":"p1","project_name":"Project 1","minutes":60,"description":"Work","confidence":0.9}],"clarification":""}}`,
			wantType:       "result",
			wantResultText: `{"allocations":[{"project_id":"p1","project_name":"Project 1","minutes":60,"description":"Work","confidence":0.9}],"clarification":""}`,
		},
		{
			name:           "result event with result string fallback",
			jsonLine:       `{"type":"result","subtype":"success","result":"{\"allocations\":[]}"}`,
			wantType:       "result",
			wantResultText: `{"allocations":[]}`,
		},
		{
			name:             "content_block_delta direct (legacy fallback)",
			jsonLine:         `{"type":"content_block_delta","delta":{"type":"text_delta","text":"chunk"}}`,
			wantType:         "content_block_delta",
			wantThinkingText: "chunk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var event streamEvent
			if err := json.Unmarshal([]byte(tt.jsonLine), &event); err != nil {
				t.Fatalf("failed to parse JSON: %v", err)
			}

			if event.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", event.Type, tt.wantType)
			}

			var gotThinking string
			onThinking := func(text string) {
				gotThinking += text
			}

			gotResult := dispatchEvent(event, onThinking)

			if gotThinking != tt.wantThinkingText {
				t.Errorf("thinking text = %q, want %q", gotThinking, tt.wantThinkingText)
			}
			if gotResult != tt.wantResultText {
				t.Errorf("result text = %q, want %q", gotResult, tt.wantResultText)
			}
		})
	}
}

func TestStreamEventMultipleChunks(t *testing.T) {
	lines := []string{
		`{"type":"system","subtype":"init"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"I'll "}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"analyze "}}}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"your work."}}}`,
		`{"type":"assistant","message":{"content":[{"type":"text","text":"I'll analyze your work."}]}}`,
		`{"type":"result","subtype":"success","result":"I'll analyze your work.\n\n{\"allocations\":[],\"clarification\":\"Need more info\"}"}`,
	}

	var thinkingChunks []string
	var resultText string
	onThinking := func(text string) {
		thinkingChunks = append(thinkingChunks, text)
	}

	for _, line := range lines {
		var event streamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("failed to parse: %v", err)
		}

		if r := dispatchEvent(event, onThinking); r != "" {
			resultText = r
		}
	}

	// 3 stream_event text_delta + 1 assistant fallback
	expectedChunks := []string{"I'll ", "analyze ", "your work.", "I'll analyze your work."}
	if len(thinkingChunks) != len(expectedChunks) {
		t.Fatalf("got %d thinking chunks, want %d: %v", len(thinkingChunks), len(expectedChunks), thinkingChunks)
	}
	for i, want := range expectedChunks {
		if thinkingChunks[i] != want {
			t.Errorf("chunk[%d] = %q, want %q", i, thinkingChunks[i], want)
		}
	}

	if resultText == "" {
		t.Fatal("expected a result, got empty string")
	}

	// extractJSON should pull the JSON out of the reasoning text
	jsonStr := extractJSON(resultText)
	var result struct {
		Clarification string `json:"clarification"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		t.Fatalf("failed to parse extracted JSON: %v\nresult: %s\nextracted: %s", err, resultText, jsonStr)
	}
	if result.Clarification != "Need more info" {
		t.Errorf("clarification = %q, want %q", result.Clarification, "Need more info")
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
			name:  "reasoning with JSON in code block",
			input: "Here's my analysis:\n\n```json\n" + `{"allocations":[]}` + "\n```\n",
			want:  `{"allocations":[]}`,
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
