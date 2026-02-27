package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/christopherklint97/clockr/internal/clockify"
)

type ClaudeCLI struct {
	Model string
}

func NewClaudeCLI(model string) *ClaudeCLI {
	if model == "" {
		model = "sonnet"
	}
	return &ClaudeCLI{Model: model}
}

func (c *ClaudeCLI) MatchProjects(ctx context.Context, description string, projects []clockify.Project, interval time.Duration) (*Suggestion, error) {
	systemPrompt := buildSystemPrompt(projects, interval)
	userPrompt := buildUserPrompt(description)

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
		return nil, fmt.Errorf("running claude CLI: %w (stderr: %s)", err, stderr.String())
	}

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
		return &suggestion, nil
	}

	var suggestion Suggestion
	if err := json.Unmarshal([]byte(cliResponse.Result), &suggestion); err != nil {
		return nil, fmt.Errorf("parsing suggestion from result: %w (result: %s)", err, cliResponse.Result)
	}

	return &suggestion, nil
}
