package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/christopherklint97/clockr/internal/clockify"
)

type ClaudeCLI struct {
	Model  string
	Logger *slog.Logger
}

func NewClaudeCLI(model string, logger *slog.Logger) *ClaudeCLI {
	if model == "" {
		model = "sonnet"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &ClaudeCLI{Model: model, Logger: logger}
}

func (c *ClaudeCLI) MatchProjectsBatch(ctx context.Context, description string, projects []clockify.Project, days []DaySlot) (*BatchSuggestion, error) {
	systemPrompt := buildBatchSystemPrompt(projects, days)
	userPrompt := buildBatchUserPrompt(description)

	c.Logger.Info("starting batch AI request",
		"days", len(days),
		"projects", len(projects),
		"model", c.Model,
		"prompt_length", len(userPrompt),
		"system_prompt_length", len(systemPrompt),
	)

	start := time.Now()

	cmd := exec.CommandContext(ctx, "claude",
		"-p", userPrompt,
		"--output-format", "json",
		"--model", c.Model,
		"--system-prompt", systemPrompt,
		"--max-turns", "1",
		"--json-schema", batchJSONSchema,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		elapsed := time.Since(start)
		c.Logger.Error("claude CLI failed",
			"error", err,
			"elapsed", elapsed,
			"stderr", stderr.String(),
			"stdout_length", stdout.Len(),
		)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("claude CLI timed out after %s (try fewer days or a simpler description)", elapsed.Round(time.Second))
		}
		return nil, fmt.Errorf("running claude CLI: %w (stderr: %s)", err, stderr.String())
	}

	elapsed := time.Since(start)
	c.Logger.Info("batch AI request completed", "elapsed", elapsed, "response_length", stdout.Len())

	var cliResponse struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &cliResponse); err != nil {
		var suggestion BatchSuggestion
		if err2 := json.Unmarshal(stdout.Bytes(), &suggestion); err2 != nil {
			return nil, fmt.Errorf("parsing claude response: %w (raw: %s)", err, stdout.String())
		}
		c.Logger.Info("batch AI returned allocations", "count", len(suggestion.Allocations))
		return &suggestion, nil
	}

	var suggestion BatchSuggestion
	if err := json.Unmarshal([]byte(cliResponse.Result), &suggestion); err != nil {
		return nil, fmt.Errorf("parsing batch suggestion from result: %w (result: %s)", err, cliResponse.Result)
	}

	c.Logger.Info("batch AI returned allocations", "count", len(suggestion.Allocations))
	return &suggestion, nil
}

func (c *ClaudeCLI) MatchProjects(ctx context.Context, description string, projects []clockify.Project, interval time.Duration, contextItems []string) (*Suggestion, error) {
	systemPrompt := buildSystemPrompt(projects, interval, contextItems)
	userPrompt := buildUserPrompt(description)

	c.Logger.Info("starting AI request",
		"interval_min", int(interval.Minutes()),
		"projects", len(projects),
		"model", c.Model,
		"context_items", len(contextItems),
	)

	start := time.Now()

	cmd := exec.CommandContext(ctx, "claude",
		"-p", userPrompt,
		"--output-format", "json",
		"--model", c.Model,
		"--system-prompt", systemPrompt,
		"--max-turns", "1",
		"--json-schema", jsonSchema,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		elapsed := time.Since(start)
		c.Logger.Error("claude CLI failed",
			"error", err,
			"elapsed", elapsed,
			"stderr", stderr.String(),
			"stdout_length", stdout.Len(),
		)
		if ctx.Err() != nil {
			return nil, fmt.Errorf("claude CLI timed out after %s", elapsed.Round(time.Second))
		}
		return nil, fmt.Errorf("running claude CLI: %w (stderr: %s)", err, stderr.String())
	}

	elapsed := time.Since(start)
	c.Logger.Info("AI request completed", "elapsed", elapsed, "response_length", stdout.Len())

	// The claude CLI with --output-format json returns a JSON object with a "result" field
	var cliResponse struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &cliResponse); err != nil {
		// Try parsing directly as a Suggestion in case format changed
		var suggestion Suggestion
		if err2 := json.Unmarshal(stdout.Bytes(), &suggestion); err2 != nil {
			return nil, fmt.Errorf("parsing claude response: %w (raw: %s)", err, stdout.String())
		}
		c.Logger.Info("AI returned allocations", "count", len(suggestion.Allocations))
		return &suggestion, nil
	}

	var suggestion Suggestion
	if err := json.Unmarshal([]byte(cliResponse.Result), &suggestion); err != nil {
		return nil, fmt.Errorf("parsing suggestion from result: %w (result: %s)", err, cliResponse.Result)
	}

	c.Logger.Info("AI returned allocations", "count", len(suggestion.Allocations))
	return &suggestion, nil
}
