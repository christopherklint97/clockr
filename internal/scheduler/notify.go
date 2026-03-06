package scheduler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ncruces/zenity"
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

// TmuxTarget holds the tmux session/window/pane info for focusing.
type TmuxTarget struct {
	Session string
	Window  string
	PaneID  string
}

// DetectTmuxTarget returns the current tmux target if running inside tmux.
func DetectTmuxTarget() *TmuxTarget {
	if os.Getenv("TMUX") == "" {
		return nil
	}
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return nil
	}

	cmd := exec.Command("tmux", "display-message", "-p", "#{session_name}:#{window_index}")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	parts := strings.SplitN(strings.TrimSpace(string(out)), ":", 2)
	if len(parts) != 2 {
		return nil
	}

	return &TmuxTarget{
		Session: parts[0],
		Window:  parts[1],
		PaneID:  paneID,
	}
}

// FocusCommand returns a shell command that activates the terminal and
// selects the tmux window/pane where clockr is running.
func (t *TmuxTarget) FocusCommand() string {
	if t == nil {
		return ""
	}

	var parts []string

	if runtime.GOOS == "darwin" {
		bundleID := terminalBundleID()
		if bundleID != "" {
			parts = append(parts, fmt.Sprintf("open -b %s", bundleID))
		}
	}

	target := fmt.Sprintf("%s:%s", t.Session, t.Window)
	parts = append(parts, fmt.Sprintf("tmux select-window -t '%s'", target))
	parts = append(parts, fmt.Sprintf("tmux select-pane -t '%s'", t.PaneID))

	return strings.Join(parts, " && ")
}

// terminalBundleID returns the macOS bundle identifier for the current terminal.
func terminalBundleID() string {
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app":
		return "com.googlecode.iterm2"
	case "WezTerm":
		return "com.github.wez.wezterm"
	case "ghostty":
		return "com.mitchellh.ghostty"
	default:
		return "com.apple.Terminal"
	}
}

// ShowPromptDialog displays a cross-platform dialog asking the user to log now,
// snooze, or skip to the next timer tick. snoozeOptions contains durations in
// minutes; if empty, only "Log Now" and "Next Timer" are shown.
func ShowPromptDialog(ctx context.Context, title, message string, snoozeOptions []int) (DialogResult, error) {
	items := []string{"Log Now"}
	for _, mins := range snoozeOptions {
		items = append(items, fmt.Sprintf("Snooze %d min", mins))
	}
	items = append(items, "Next Timer")

	opts := []zenity.Option{
		zenity.Title(title),
		zenity.DefaultItems("Log Now"),
		zenity.DisallowEmpty(),
	}

	selected, err := zenity.List(message, items, opts...)
	if err != nil {
		// Dialog cancelled or closed — default to Log Now so we don't
		// silently skip prompts.
		if ctx.Err() != nil {
			return DialogResult{Action: ActionLogNow}, ctx.Err()
		}
		return DialogResult{Action: ActionLogNow}, nil
	}

	if selected == "Next Timer" {
		return DialogResult{Action: ActionNextTimer}, nil
	}

	for _, mins := range snoozeOptions {
		if selected == fmt.Sprintf("Snooze %d min", mins) {
			return DialogResult{Action: ActionSnooze, SnoozeMinutes: mins}, nil
		}
	}

	return DialogResult{Action: ActionLogNow}, nil
}

// SendNotification sends a desktop notification. If tmuxTarget is provided and
// terminal-notifier is available on macOS, clicking the notification will focus
// the tmux pane where clockr is running.
func SendNotification(title, message string, tmuxTarget *TmuxTarget) error {
	if runtime.GOOS == "darwin" {
		if notifierPath, err := exec.LookPath("terminal-notifier"); err == nil {
			return sendTerminalNotification(notifierPath, title, message, tmuxTarget)
		}
	}
	return zenity.Notify(message, zenity.Title(title), zenity.InfoIcon)
}

// sendTerminalNotification uses terminal-notifier on macOS to show a
// notification that focuses the clockr tmux pane when clicked.
func sendTerminalNotification(notifierPath, title, message string, target *TmuxTarget) error {
	args := []string{"-title", title, "-message", message, "-sound", "default", "-group", "clockr"}

	if focusCmd := target.FocusCommand(); focusCmd != "" {
		args = append(args, "-execute", focusCmd)
	} else {
		// No tmux target — just activate the terminal on click.
		bundleID := terminalBundleID()
		if bundleID != "" {
			args = append(args, "-activate", bundleID)
		}
	}

	cmd := exec.Command(notifierPath, args...)
	// Start without blocking — terminal-notifier waits for user interaction
	// and will run the -execute command when the notification is clicked.
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap the process in the background to avoid zombies.
	go cmd.Wait()
	return nil
}
