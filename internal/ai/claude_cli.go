package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"time"

	"github.com/christopherklint97/clockr/internal/clockify"
)

type ClaudeCLI struct {
	Model  string
	logger *slog.Logger
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

func (c *ClaudeCLI) MatchProjectsBatch(ctx context.Context, description string, projects []clockify.Project, days []DaySlot) (*BatchSuggestion, error) {
	systemPrompt := buildBatchSystemPrompt(projects, days)
	userPrompt := buildBatchUserPrompt(description)

	args := []string{
		"-p", userPrompt,
		"--output-format", "json",
		"--model", c.Model,
		"--system-prompt", systemPrompt,
		"--max-turns", "1",
		"--json-schema", batchJSONSchema,
	}

	c.logger.Debug("invoking claude CLI (batch)",
		"model", c.Model,
		"days", len(days),
		"projects", len(projects),
		"system_prompt_len", len(systemPrompt),
		"user_prompt_len", len(userPrompt),
		"schema_len", len(batchJSONSchema),
	)

	cmd := exec.CommandContext(ctx, "claude", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err := cmd.Run()
	elapsed := time.Since(startTime)

	c.logger.Debug("claude CLI finished (batch)",
		"elapsed", elapsed,
		"stdout_bytes", stdout.Len(),
		"stderr_bytes", stderr.Len(),
		"error", err,
	)

	if err != nil {
		c.logger.Error("claude CLI failed (batch)",
			"error", err,
			"elapsed", elapsed,
			"stderr", stderr.String(),
			"stdout_bytes", stdout.Len(),
		)
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("claude CLI timed out after %s — try a shorter date range or fewer projects", elapsed.Truncate(time.Second))
		}
		return nil, fmt.Errorf("running claude CLI: %w (stderr: %s)", err, stderr.String())
	}

	c.logger.Debug("claude CLI raw response (batch)", "stdout", truncateStr(stdout.String(), 500))

	var cliResponse struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &cliResponse); err != nil {
		c.logger.Debug("parsing as wrapper failed, trying direct parse", "error", err)
		var suggestion BatchSuggestion
		if err2 := json.Unmarshal(stdout.Bytes(), &suggestion); err2 != nil {
			c.logger.Error("failed to parse claude response (batch)",
				"wrapper_error", err,
				"direct_error", err2,
				"raw_response", truncateStr(stdout.String(), 1000),
			)
			return nil, fmt.Errorf("parsing claude response: %w (raw: %s)", err, stdout.String())
		}
		c.logger.Debug("parsed batch suggestion via direct parse", "allocations", len(suggestion.Allocations))
		return &suggestion, nil
	}

	var suggestion BatchSuggestion
	if err := json.Unmarshal([]byte(cliResponse.Result), &suggestion); err != nil {
		c.logger.Error("failed to parse batch suggestion from result field",
			"error", err,
			"result", truncateStr(cliResponse.Result, 1000),
		)
		return nil, fmt.Errorf("parsing batch suggestion from result: %w (result: %s)", err, cliResponse.Result)
	}

	c.logger.Debug("parsed batch suggestion", "allocations", len(suggestion.Allocations))
	return &suggestion, nil
}

func (c *ClaudeCLI) MatchProjects(ctx context.Context, description string, projects []clockify.Project, interval time.Duration, contextItems []string) (*Suggestion, error) {
	systemPrompt := buildSystemPrompt(projects, interval, contextItems)
	userPrompt := buildUserPrompt(description)

	args := []string{
		"-p", userPrompt,
		"--output-format", "json",
		"--model", c.Model,
		"--system-prompt", systemPrompt,
		"--max-turns", "1",
		"--json-schema", jsonSchema,
	}

	c.logger.Debug("invoking claude CLI",
		"model", c.Model,
		"projects", len(projects),
		"context_items", len(contextItems),
		"system_prompt_len", len(systemPrompt),
		"user_prompt_len", len(userPrompt),
		"schema_len", len(jsonSchema),
	)

	cmd := exec.CommandContext(ctx, "claude", args...)

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
			"stdout_bytes", stdout.Len(),
		)
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return nil, fmt.Errorf("claude CLI timed out after %s", elapsed.Truncate(time.Second))
		}
		return nil, fmt.Errorf("running claude CLI: %w (stderr: %s)", err, stderr.String())
	}

	c.logger.Debug("claude CLI raw response", "stdout", truncateStr(stdout.String(), 500))

	// The claude CLI with --output-format json returns a JSON object with a "result" field
	var cliResponse struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &cliResponse); err != nil {
		c.logger.Debug("parsing as wrapper failed, trying direct parse", "error", err)
		// Try parsing directly as a Suggestion in case format changed
		var suggestion Suggestion
		if err2 := json.Unmarshal(stdout.Bytes(), &suggestion); err2 != nil {
			c.logger.Error("failed to parse claude response",
				"wrapper_error", err,
				"direct_error", err2,
				"raw_response", truncateStr(stdout.String(), 1000),
			)
			return nil, fmt.Errorf("parsing claude response: %w (raw: %s)", err, stdout.String())
		}
		c.logger.Debug("parsed suggestion via direct parse", "allocations", len(suggestion.Allocations))
		return &suggestion, nil
	}

	var suggestion Suggestion
	if err := json.Unmarshal([]byte(cliResponse.Result), &suggestion); err != nil {
		c.logger.Error("failed to parse suggestion from result field",
			"error", err,
			"result", truncateStr(cliResponse.Result, 1000),
		)
		return nil, fmt.Errorf("parsing suggestion from result: %w (result: %s)", err, cliResponse.Result)
	}

	c.logger.Debug("parsed suggestion", "allocations", len(suggestion.Allocations))
	return &suggestion, nil
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
