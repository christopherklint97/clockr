package scheduler

import (
	"context"
	"fmt"

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

// SendNotification sends a cross-platform desktop notification.
func SendNotification(title, message string) error {
	return zenity.Notify(message, zenity.Title(title), zenity.InfoIcon)
}
