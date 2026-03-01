package scheduler

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/gen2brain/beeep"
)

// DialogAction represents the user's choice from the prompt dialog.
type DialogAction int

const (
	ActionLogNow    DialogAction = iota
	ActionSnooze    DialogAction = iota
	ActionNextTimer DialogAction = iota
)

// DialogResult holds the action chosen and, if snoozed, the duration.
type DialogResult struct {
	Action        DialogAction
	SnoozeMinutes int
}

// ShowPromptDialog displays a platform-aware dialog asking the user to log now,
// snooze, or skip to the next timer tick. snoozeOptions contains durations in
// minutes; if empty, only "Log Now" and "Next Timer" are shown.
func ShowPromptDialog(ctx context.Context, title, message string, snoozeOptions []int) (DialogResult, error) {
	switch runtime.GOOS {
	case "darwin":
		return showDarwinDialog(ctx, title, message, snoozeOptions)
	case "linux":
		return showLinuxDialog(ctx, title, message, snoozeOptions)
	default:
		return showTerminalDialog(ctx, message, snoozeOptions)
	}
}

func showDarwinDialog(ctx context.Context, title, message string, snoozeOptions []int) (DialogResult, error) {
	// Build button list: rightmost button is default, listed right-to-left.
	// We want: "Log Now" (default), snooze options, "Next Timer".
	var buttons []string
	buttons = append(buttons, "Next Timer")
	for i := len(snoozeOptions) - 1; i >= 0; i-- {
		buttons = append(buttons, fmt.Sprintf("%d min", snoozeOptions[i]))
	}
	buttons = append(buttons, "Log Now")

	buttonStr := `"` + strings.Join(buttons, `", "`) + `"`

	script := fmt.Sprintf(
		`display dialog %q with title %q buttons {%s} default button "Log Now"`,
		message, title, buttonStr,
	)

	cmd := exec.CommandContext(ctx, "osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		// If the user closed the dialog (e.g. Escape/Cmd+.), treat as Log Now
		// so we don't silently skip prompts.
		if ctx.Err() != nil {
			return DialogResult{Action: ActionLogNow}, ctx.Err()
		}
		return DialogResult{Action: ActionLogNow}, nil
	}

	result := strings.TrimSpace(string(out))
	// osascript returns "button returned:Log Now"
	result = strings.TrimPrefix(result, "button returned:")

	if result == "Next Timer" {
		return DialogResult{Action: ActionNextTimer}, nil
	}

	for _, mins := range snoozeOptions {
		if result == fmt.Sprintf("%d min", mins) {
			return DialogResult{Action: ActionSnooze, SnoozeMinutes: mins}, nil
		}
	}

	return DialogResult{Action: ActionLogNow}, nil
}

func showLinuxDialog(ctx context.Context, title, message string, snoozeOptions []int) (DialogResult, error) {
	// Try zenity first, then kdialog, then fall back to terminal.
	if path, err := exec.LookPath("zenity"); err == nil {
		return showZenityDialog(ctx, path, title, message, snoozeOptions)
	}
	if path, err := exec.LookPath("kdialog"); err == nil {
		return showKDialog(ctx, path, title, message, snoozeOptions)
	}
	return showTerminalDialog(ctx, message, snoozeOptions)
}

func showZenityDialog(ctx context.Context, zenityPath, title, message string, snoozeOptions []int) (DialogResult, error) {
	args := []string{
		"--list", "--radiolist",
		"--title", title,
		"--text", message,
		"--column", "", "--column", "Action",
	}

	// Default selection is "Log Now"
	args = append(args, "TRUE", "Log Now")
	for _, mins := range snoozeOptions {
		args = append(args, "FALSE", fmt.Sprintf("Snooze %d min", mins))
	}
	args = append(args, "FALSE", "Next Timer")

	cmd := exec.CommandContext(ctx, zenityPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return DialogResult{Action: ActionLogNow}, ctx.Err()
		}
		return DialogResult{Action: ActionLogNow}, nil
	}

	result := strings.TrimSpace(string(out))

	if result == "Next Timer" {
		return DialogResult{Action: ActionNextTimer}, nil
	}

	for _, mins := range snoozeOptions {
		if result == fmt.Sprintf("Snooze %d min", mins) {
			return DialogResult{Action: ActionSnooze, SnoozeMinutes: mins}, nil
		}
	}

	return DialogResult{Action: ActionLogNow}, nil
}

func showKDialog(ctx context.Context, kdialogPath, title, message string, snoozeOptions []int) (DialogResult, error) {
	args := []string{"--menu", message, "--title", title}

	args = append(args, "log", "Log Now")
	for _, mins := range snoozeOptions {
		args = append(args, fmt.Sprintf("snooze_%d", mins), fmt.Sprintf("Snooze %d min", mins))
	}
	args = append(args, "next", "Next Timer")

	cmd := exec.CommandContext(ctx, kdialogPath, args...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return DialogResult{Action: ActionLogNow}, ctx.Err()
		}
		return DialogResult{Action: ActionLogNow}, nil
	}

	result := strings.TrimSpace(string(out))

	if result == "next" {
		return DialogResult{Action: ActionNextTimer}, nil
	}

	for _, mins := range snoozeOptions {
		if result == fmt.Sprintf("snooze_%d", mins) {
			return DialogResult{Action: ActionSnooze, SnoozeMinutes: mins}, nil
		}
	}

	return DialogResult{Action: ActionLogNow}, nil
}

func showTerminalDialog(ctx context.Context, message string, snoozeOptions []int) (DialogResult, error) {
	fmt.Println()
	fmt.Println(message)
	fmt.Println()

	options := []struct {
		label  string
		result DialogResult
	}{
		{"Log Now", DialogResult{Action: ActionLogNow}},
	}
	for _, mins := range snoozeOptions {
		options = append(options, struct {
			label  string
			result DialogResult
		}{
			fmt.Sprintf("Snooze %d min", mins),
			DialogResult{Action: ActionSnooze, SnoozeMinutes: mins},
		})
	}
	options = append(options, struct {
		label  string
		result DialogResult
	}{"Next Timer", DialogResult{Action: ActionNextTimer}})

	for i, opt := range options {
		fmt.Printf("  %d) %s\n", i+1, opt.label)
	}
	fmt.Print("\nChoice [1]: ")

	// Read input with context cancellation support.
	type readResult struct {
		line string
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			ch <- readResult{line: scanner.Text()}
		} else {
			ch <- readResult{err: scanner.Err()}
		}
	}()

	select {
	case <-ctx.Done():
		return DialogResult{Action: ActionLogNow}, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return DialogResult{Action: ActionLogNow}, nil
		}
		line := strings.TrimSpace(r.line)
		if line == "" {
			return DialogResult{Action: ActionLogNow}, nil
		}

		var choice int
		if _, err := fmt.Sscanf(line, "%d", &choice); err != nil || choice < 1 || choice > len(options) {
			return DialogResult{Action: ActionLogNow}, nil
		}

		return options[choice-1].result, nil
	}
}

// SendNotification sends a simple desktop notification via beeep.
// Deprecated: Use ShowPromptDialog for interactive scheduler prompts.
func SendNotification(title, message string) error {
	return beeep.Notify(title, message, "")
}
