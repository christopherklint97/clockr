package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/christopherklint97/clockr/internal/clockify"
	"github.com/christopherklint97/clockr/internal/config"
)

// PromptFileProvider writes the AI prompt to a file, copies it to the clipboard,
// optionally injects it into an adjacent tmux pane running Claude Code, and
// waits for the user to confirm the response file is ready.
type PromptFileProvider struct {
	logger   *slog.Logger
	OnStatus func(string) // called with status messages for the loading view
	ReadyCh  chan struct{} // TUI sends on this channel when user presses Enter
	tmpDir   string        // absolute path to tmp/ directory
}

func NewPromptFileProvider(logger *slog.Logger) (*PromptFileProvider, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("resolving config dir: %w", err)
	}
	return &PromptFileProvider{
		logger:  logger,
		ReadyCh: make(chan struct{}),
		tmpDir:  filepath.Join(dir, "tmp"),
	}, nil
}

func (p *PromptFileProvider) MatchProjects(_ context.Context, description string, projects []clockify.Project, interval time.Duration, contextItems []string) (*Suggestion, error) {
	systemPrompt := buildSystemPrompt(projects, interval, contextItems)
	userPrompt := buildUserPrompt(description)
	combined := buildCombinedPrompt(systemPrompt, userPrompt, false, p.tmpDir)

	if err := p.writeAndWait(combined); err != nil {
		return nil, err
	}

	raw, err := p.readResponse()
	if err != nil {
		return nil, err
	}

	jsonStr := extractJSON(raw)
	p.logger.Debug("extracted JSON from response file", "json_len", len(jsonStr))

	var suggestion Suggestion
	if err := json.Unmarshal([]byte(jsonStr), &suggestion); err != nil {
		return nil, fmt.Errorf("parsing suggestion from response file: %w (raw: %s)", err, truncateStr(raw, 1000))
	}

	return &suggestion, nil
}

func (p *PromptFileProvider) MatchProjectsBatch(_ context.Context, description string, projects []clockify.Project, days []DaySlot) (*BatchSuggestion, error) {
	systemPrompt := buildBatchSystemPrompt(projects, days)
	userPrompt := buildBatchUserPrompt(description)
	combined := buildCombinedPrompt(systemPrompt, userPrompt, true, p.tmpDir)

	if err := p.writeAndWait(combined); err != nil {
		return nil, err
	}

	raw, err := p.readResponse()
	if err != nil {
		return nil, err
	}

	jsonStr := extractJSON(raw)
	p.logger.Debug("extracted JSON from batch response file", "json_len", len(jsonStr))

	var suggestion BatchSuggestion
	if err := json.Unmarshal([]byte(jsonStr), &suggestion); err != nil {
		return nil, fmt.Errorf("parsing batch suggestion from response file: %w (raw: %s)", err, truncateStr(raw, 1000))
	}

	return &suggestion, nil
}

// writeAndWait writes the prompt file, copies to clipboard, attempts tmux injection,
// and blocks until the user signals readiness.
func (p *PromptFileProvider) writeAndWait(prompt string) error {
	// Ensure tmp dir exists
	if err := os.MkdirAll(p.tmpDir, 0755); err != nil {
		return fmt.Errorf("creating tmp dir: %w", err)
	}

	// Write prompt file
	promptPath := filepath.Join(p.tmpDir, "clockr_prompt.md")
	if err := os.WriteFile(promptPath, []byte(prompt), 0644); err != nil {
		return fmt.Errorf("writing prompt file: %w", err)
	}
	p.logger.Debug("wrote prompt file", "path", promptPath, "len", len(prompt))
	p.emit("Prompt written to " + promptPath)

	// Remove stale response file
	responsePath := filepath.Join(p.tmpDir, "clockr_response.json")
	os.Remove(responsePath)

	// Copy to clipboard
	if err := copyToClipboard(prompt); err != nil {
		p.logger.Debug("clipboard copy failed", "error", err)
		p.emit("Clipboard copy failed: " + err.Error())
	} else {
		p.emit("Prompt copied to clipboard")
	}

	// Attempt tmux injection
	tmuxStatus := attemptTmuxInjection(p.tmpDir, promptPath, p.logger)
	if tmuxStatus != "" {
		p.emit(tmuxStatus)
	}

	p.emit("\nPress Enter when the response is ready at " + responsePath)

	// Block until user presses Enter
	<-p.ReadyCh
	return nil
}

// readResponse reads and returns the content of the response file.
func (p *PromptFileProvider) readResponse() (string, error) {
	responsePath := filepath.Join(p.tmpDir, "clockr_response.json")
	data, err := os.ReadFile(responsePath)
	if err != nil {
		return "", fmt.Errorf("reading response file %s: %w", responsePath, err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("response file %s is empty", responsePath)
	}
	p.logger.Debug("read response file", "path", responsePath, "len", len(data))
	return string(data), nil
}

// emit sends a status message through OnStatus if set.
func (p *PromptFileProvider) emit(msg string) {
	if p.OnStatus != nil {
		p.OnStatus(msg)
	}
}

// buildCombinedPrompt wraps system and user prompts into a self-contained markdown document.
func buildCombinedPrompt(systemPrompt, userPrompt string, isBatch bool, tmpDir string) string {
	mode := "single time entry"
	if isBatch {
		mode = "batch (multi-day) time entries"
	}

	responsePath := filepath.Join(tmpDir, "clockr_response.json")

	return fmt.Sprintf(`# Clockr Time Entry Request

## Mode
%s

## Instructions
%s

## User Input
%s

## Response Format
Respond with ONLY a JSON object matching the schema described above.
No markdown code fences, no explanation — just the raw JSON.

If you are Claude Code, write the JSON to %s
`, mode, systemPrompt, userPrompt, responsePath)
}

// copyToClipboard pipes the given text to pbcopy.
func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}
