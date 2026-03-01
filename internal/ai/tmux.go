package ai

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// attemptTmuxInjection tries to find a Claude Code pane in tmux and send it
// an instruction to read the prompt file and write a response. Returns a status
// message describing what happened.
func attemptTmuxInjection(tmpDir, promptPath string, logger *slog.Logger) string {
	if os.Getenv("TMUX") == "" {
		logger.Debug("not in tmux, skipping injection")
		return ""
	}

	pane, err := findClaudeCodePane(logger)
	if err != nil {
		logger.Debug("could not find Claude Code pane", "error", err)
		return "No Claude Code pane found in tmux"
	}

	logger.Debug("found Claude Code pane", "pane", pane)

	responsePath := tmpDir + "/clockr_response.json"

	// Send /clear first
	if err := tmuxSendKeys(pane, "/clear"); err != nil {
		logger.Debug("failed to send /clear", "error", err)
		return "Found Claude Code pane but failed to send command"
	}
	if err := tmuxSendKeys(pane, "Enter"); err != nil {
		logger.Debug("failed to send Enter after /clear", "error", err)
	}

	// Wait for /clear to take effect
	time.Sleep(1 * time.Second)

	// Send the instruction to read the prompt file
	instruction := fmt.Sprintf("Read the file %s, follow its instructions, and write your JSON response to %s", promptPath, responsePath)
	if err := tmuxSendKeys(pane, instruction); err != nil {
		logger.Debug("failed to send instruction", "error", err)
		return "Found Claude Code pane but failed to send instruction"
	}
	if err := tmuxSendKeys(pane, "Enter"); err != nil {
		logger.Debug("failed to send Enter after instruction", "error", err)
	}

	return "Sent prompt to Claude Code pane (" + pane + ")"
}

// findClaudeCodePane lists tmux panes and returns the ID of one running Claude Code.
// It excludes the current pane (where clockr is running).
func findClaudeCodePane(logger *slog.Logger) (string, error) {
	// Get the current pane ID so we can exclude it
	currentPane := os.Getenv("TMUX_PANE")

	// List panes in the current window only (no -a flag)
	cmd := exec.Command("tmux", "list-panes", "-F", "#{pane_id} #{pane_current_command} #{pane_title}")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("listing tmux panes: %w", err)
	}

	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			continue
		}
		paneID := parts[0]
		command := strings.ToLower(parts[1])
		title := ""
		if len(parts) >= 3 {
			title = strings.ToLower(parts[2])
		}

		// Skip ourselves
		if paneID == currentPane {
			continue
		}

		// Look for "claude" in command name or pane title
		if strings.Contains(command, "claude") || strings.Contains(title, "claude") {
			logger.Debug("matched Claude Code pane",
				"pane_id", paneID,
				"command", command,
				"title", title,
			)
			return paneID, nil
		}
	}

	// Fallback: check pane content for claude prompt (current window only)
	cmd = exec.Command("tmux", "list-panes", "-F", "#{pane_id}")
	out, err = cmd.Output()
	if err != nil {
		return "", fmt.Errorf("listing pane IDs: %w", err)
	}

	for _, paneID := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		paneID = strings.TrimSpace(paneID)
		if paneID == "" || paneID == currentPane {
			continue
		}

		// Capture pane content
		captureCmd := exec.Command("tmux", "capture-pane", "-t", paneID, "-p")
		content, err := captureCmd.Output()
		if err != nil {
			continue
		}

		lower := strings.ToLower(string(content))
		if strings.Contains(lower, "claude") {
			logger.Debug("matched Claude Code pane by content",
				"pane_id", paneID,
			)
			return paneID, nil
		}
	}

	return "", fmt.Errorf("no Claude Code pane found")
}

// tmuxSendKeys sends keystrokes to a tmux pane.
func tmuxSendKeys(paneID, keys string) error {
	cmd := exec.Command("tmux", "send-keys", "-t", paneID, keys)
	return cmd.Run()
}
