package ai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/christopherklint97/clockr/internal/clockify"
)

// cleanEnv returns os.Environ() with Claude Code session vars removed
// so the subprocess doesn't get blocked by the nested-session check.
// It also injects env vars to enable extended thinking with high effort.
func cleanEnv() []string {
	blocked := map[string]bool{
		"CLAUDECODE":                           true,
		"CLAUDE_CODE_ENTRYPOINT":               true,
		"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": true,
	}
	overrides := map[string]string{
		"CLAUDE_CODE_EFFORT_LEVEL": "high",
	}
	var env []string
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		if blocked[key] || overrides[key] != "" {
			continue
		}
		env = append(env, e)
	}
	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	return env
}

type ClaudeCLI struct {
	Model      string
	logger     *slog.Logger
	OnThinking func(text string) // optional: called with streaming text chunks
}

func NewClaudeCLI(model string, logger *slog.Logger) *ClaudeCLI {
	if model == "" {
		model = "sonnet"
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &ClaudeCLI{Model: model, logger: logger}
}

func (c *ClaudeCLI) MatchProjects(ctx context.Context, description string, projects []clockify.Project, interval time.Duration, contextItems []string) (*Suggestion, error) {
	systemPrompt := buildSystemPrompt(projects, interval, contextItems)
	userPrompt := buildUserPrompt(description)

	args := []string{
		"-p", userPrompt,
		"--output-format", "json",
		"--model", c.Model,
		"--system-prompt", systemPrompt,
		"--no-session-persistence",
	}

	c.logger.Debug("invoking claude CLI",
		"model", c.Model,
		"args", args,
		"projects", len(projects),
		"context_items", len(contextItems),
		"system_prompt_len", len(systemPrompt),
		"user_prompt_len", len(userPrompt),
	)

	result, err := c.runCLI(ctx, args)
	if err != nil {
		return nil, err
	}

	c.logger.Debug("MatchProjects result to parse",
		"result_len", len(result),
		"result", truncateStr(result, 2000),
	)

	jsonStr := extractJSON(result)
	c.logger.Debug("extracted JSON from result",
		"json_len", len(jsonStr),
		"json", truncateStr(jsonStr, 2000),
	)

	var suggestion Suggestion
	if err := json.Unmarshal([]byte(jsonStr), &suggestion); err != nil {
		c.logger.Error("failed to parse suggestion",
			"error", err,
			"raw", truncateStr(result, 2000),
		)
		return nil, fmt.Errorf("parsing suggestion: %w (raw: %s)", err, truncateStr(result, 1000))
	}

	c.logger.Debug("parsed suggestion",
		"allocations", len(suggestion.Allocations),
		"clarification", suggestion.Clarification,
	)
	for i, a := range suggestion.Allocations {
		c.logger.Debug("allocation",
			"index", i,
			"project_id", a.ProjectID,
			"project_name", a.ProjectName,
			"minutes", a.Minutes,
			"description", a.Description,
			"confidence", a.Confidence,
		)
	}
	return &suggestion, nil
}

func (c *ClaudeCLI) MatchProjectsBatch(ctx context.Context, description string, projects []clockify.Project, days []DaySlot) (*BatchSuggestion, error) {
	systemPrompt := buildBatchSystemPrompt(projects, days)
	userPrompt := buildBatchUserPrompt(description)

	args := []string{
		"-p", userPrompt,
		"--output-format", "json",
		"--model", c.Model,
		"--system-prompt", systemPrompt,
		"--no-session-persistence",
	}

	c.logger.Debug("invoking claude CLI (batch)",
		"model", c.Model,
		"args", args,
		"days", len(days),
		"projects", len(projects),
		"system_prompt_len", len(systemPrompt),
		"user_prompt_len", len(userPrompt),
	)

	result, err := c.runCLI(ctx, args)
	if err != nil {
		return nil, err
	}

	c.logger.Debug("MatchProjectsBatch result to parse",
		"result_len", len(result),
		"result", truncateStr(result, 2000),
	)

	jsonStr := extractJSON(result)
	c.logger.Debug("extracted JSON from batch result",
		"json_len", len(jsonStr),
		"json", truncateStr(jsonStr, 2000),
	)

	var suggestion BatchSuggestion
	if err := json.Unmarshal([]byte(jsonStr), &suggestion); err != nil {
		c.logger.Error("failed to parse batch suggestion",
			"error", err,
			"raw", truncateStr(result, 2000),
		)
		return nil, fmt.Errorf("parsing batch suggestion: %w (raw: %s)", err, truncateStr(result, 1000))
	}

	c.logger.Debug("parsed batch suggestion",
		"allocations", len(suggestion.Allocations),
		"clarification", suggestion.Clarification,
	)
	for i, a := range suggestion.Allocations {
		c.logger.Debug("batch allocation",
			"index", i,
			"date", a.Date,
			"start_time", a.StartTime,
			"end_time", a.EndTime,
			"project_id", a.ProjectID,
			"project_name", a.ProjectName,
			"minutes", a.Minutes,
			"description", a.Description,
			"confidence", a.Confidence,
		)
	}
	return &suggestion, nil
}

// runCLI executes the claude CLI, using streaming if OnThinking is set.
func (c *ClaudeCLI) runCLI(ctx context.Context, args []string) (string, error) {
	if c.OnThinking != nil {
		return c.runStreamingCLI(ctx, args)
	}
	return c.runBufferedCLI(ctx, args)
}

// runBufferedCLI runs the CLI and captures all output at once.
func (c *ClaudeCLI) runBufferedCLI(ctx context.Context, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Env = cleanEnv()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	elapsed := time.Since(startTime)

	c.logger.Debug("claude CLI finished",
		"elapsed", elapsed,
		"stdout_bytes", stdout.Len(),
		"stderr_bytes", stderr.Len(),
		"error", err,
	)

	if err != nil {
		c.logger.Error("claude CLI failed",
			"error", err,
			"elapsed", elapsed,
			"stderr", stderr.String(),
		)
		if ctx.Err() != nil {
			return "", fmt.Errorf("claude CLI timed out after %s", elapsed.Truncate(time.Second))
		}
		return "", fmt.Errorf("running claude CLI: %w (stderr: %s)", err, stderr.String())
	}

	rawOutput := stdout.String()
	c.logger.Debug("claude CLI raw response",
		"stdout", truncateStr(rawOutput, 2000),
		"stdout_len", len(rawOutput),
	)

	// Unwrap claude --output-format json envelope.
	// Prefer structured_output (typed JSON from --json-schema) over result (human-readable text).
	var rawWrapper struct {
		Type             string          `json:"type"`
		Subtype          string          `json:"subtype"`
		Result           json.RawMessage `json:"result"`
		StructuredOutput json.RawMessage `json:"structured_output"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &rawWrapper); err == nil {
		c.logger.Debug("parsed wrapper envelope",
			"type", rawWrapper.Type,
			"subtype", rawWrapper.Subtype,
			"has_structured_output", len(rawWrapper.StructuredOutput) > 0,
		)

		// Prefer structured_output — this is the typed JSON from --json-schema
		if len(rawWrapper.StructuredOutput) > 0 && rawWrapper.StructuredOutput[0] == '{' {
			c.logger.Debug("using structured_output", "len", len(rawWrapper.StructuredOutput))
			return string(rawWrapper.StructuredOutput), nil
		}

		// Fall back to result field
		if len(rawWrapper.Result) > 0 {
			// Case 1: result is a JSON string (e.g. "{\"allocations\":...}")
			var resultStr string
			if err := json.Unmarshal(rawWrapper.Result, &resultStr); err == nil && resultStr != "" {
				c.logger.Debug("unwrapped result as string", "len", len(resultStr))
				return resultStr, nil
			}

			// Case 2: result is a JSON object/array directly
			if rawWrapper.Result[0] == '{' || rawWrapper.Result[0] == '[' {
				c.logger.Debug("unwrapped result as raw JSON object", "len", len(rawWrapper.Result))
				return string(rawWrapper.Result), nil
			}

			c.logger.Debug("result field present but could not unwrap",
				"result_preview", truncateStr(string(rawWrapper.Result), 500),
			)
		}
	} else {
		c.logger.Debug("wrapper parse failed, treating as raw output", "error", err)
	}

	// Return raw stdout for direct parsing
	return rawOutput, nil
}

// streamEvent represents a single event in the stream-json output.
type streamEvent struct {
	Type             string          `json:"type"`
	Subtype          string          `json:"subtype,omitempty"`
	Result           json.RawMessage `json:"result,omitempty"`
	StructuredOutput json.RawMessage `json:"structured_output,omitempty"`
	Delta            struct {
		Type string `json:"type,omitempty"`
		Text string `json:"text,omitempty"`
	} `json:"delta"`
	Message struct {
		Content []struct {
			Type string `json:"type,omitempty"`
			Text string `json:"text,omitempty"`
		} `json:"content,omitempty"`
	} `json:"message"`
	// Event is the nested event inside a "stream_event" wrapper
	// (only emitted when --include-partial-messages is passed).
	Event struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`
}

// runStreamingCLI runs the CLI with stream-json output, calling OnThinking for text chunks.
func (c *ClaudeCLI) runStreamingCLI(ctx context.Context, args []string) (string, error) {
	// Replace --output-format json with stream-json and add --verbose (required for stream-json with --print)
	streamArgs := make([]string, len(args))
	copy(streamArgs, args)
	for i, a := range streamArgs {
		if a == "json" && i > 0 && streamArgs[i-1] == "--output-format" {
			streamArgs[i] = "stream-json"
		}
	}
	streamArgs = append(streamArgs, "--verbose", "--include-partial-messages")

	cmd := exec.CommandContext(ctx, "claude", streamArgs...)
	cmd.Env = cleanEnv()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("creating stdout pipe: %w", err)
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	startTime := time.Now()
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("starting claude CLI: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var resultText string

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event streamEvent
		if err := json.Unmarshal(line, &event); err != nil {
			c.logger.Debug("skipping unparseable stream line",
				"error", err,
				"line", truncateStr(string(line), 200),
			)
			continue
		}

		c.logger.Debug("stream event received",
			"type", event.Type,
			"event_type", event.Event.Type,
			"event_delta_type", event.Event.Delta.Type,
		)

		switch event.Type {
		case "stream_event":
			if event.Event.Type == "content_block_delta" {
				if (event.Event.Delta.Type == "text_delta" || event.Event.Delta.Type == "thinking_delta") && event.Event.Delta.Text != "" {
					c.OnThinking(event.Event.Delta.Text)
				}
			}
		case "content_block_delta":
			// Fallback: direct content_block_delta (without stream_event wrapping)
			if event.Delta.Text != "" {
				c.OnThinking(event.Delta.Text)
			}
		case "assistant":
			for _, block := range event.Message.Content {
				if (block.Type == "text" || block.Type == "thinking") && block.Text != "" {
					c.OnThinking(block.Text)
				}
			}
		case "result":
			// Prefer structured_output over result
			if len(event.StructuredOutput) > 0 && event.StructuredOutput[0] == '{' {
				resultText = string(event.StructuredOutput)
				c.logger.Debug("stream result event (structured_output)",
					"result_len", len(resultText),
					"result_preview", truncateStr(resultText, 500),
				)
			} else if len(event.Result) > 0 {
				// Try as string first (escaped JSON)
				var s string
				if err := json.Unmarshal(event.Result, &s); err == nil {
					resultText = s
				} else {
					// Raw JSON object/array
					resultText = string(event.Result)
				}
				c.logger.Debug("stream result event",
					"result_len", len(resultText),
					"result_preview", truncateStr(resultText, 500),
				)
			}
		}
	}

	elapsed := time.Since(startTime)

	if err := cmd.Wait(); err != nil {
		c.logger.Error("claude CLI failed (streaming)",
			"error", err,
			"elapsed", elapsed,
			"stderr", stderr.String(),
		)
		if ctx.Err() != nil {
			return "", fmt.Errorf("claude CLI timed out after %s", elapsed.Truncate(time.Second))
		}
		return "", fmt.Errorf("running claude CLI: %w (stderr: %s)", err, stderr.String())
	}

	c.logger.Debug("claude CLI streaming finished",
		"elapsed", elapsed,
		"result_len", len(resultText),
	)

	if resultText == "" {
		return "", fmt.Errorf("no result received from claude CLI stream")
	}

	// The result might be wrapped or direct — try to unwrap
	var rawWrapper struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal([]byte(resultText), &rawWrapper); err == nil && len(rawWrapper.Result) > 0 {
		// Try as string first
		var s string
		if err := json.Unmarshal(rawWrapper.Result, &s); err == nil && s != "" {
			c.logger.Debug("stream: unwrapped nested result as string", "len", len(s))
			return s, nil
		}
		// Try as raw JSON object
		if rawWrapper.Result[0] == '{' || rawWrapper.Result[0] == '[' {
			c.logger.Debug("stream: unwrapped nested result as JSON object", "len", len(rawWrapper.Result))
			return string(rawWrapper.Result), nil
		}
	}

	return resultText, nil
}

// extractJSON finds and returns the first top-level JSON object in s.
// The model may output reasoning text before/after the JSON.
func extractJSON(s string) string {
	// Find first '{' and match to its closing '}'
	start := strings.Index(s, "{")
	if start == -1 {
		return s
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	// No balanced closing brace found, return from start
	return s[start:]
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
