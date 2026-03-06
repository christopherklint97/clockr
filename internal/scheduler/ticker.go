package scheduler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/christopherklint97/clockr/internal/ai"
	"github.com/christopherklint97/clockr/internal/calendar"
	"github.com/christopherklint97/clockr/internal/clockify"
	"github.com/christopherklint97/clockr/internal/config"
	"github.com/christopherklint97/clockr/internal/store"
	"github.com/christopherklint97/clockr/internal/tui"
)

type Scheduler struct {
	cfg               *config.Config
	client            *clockify.Client
	db                *store.DB
	provider          ai.Provider
	workspaceID       string
	skipWorkTimeCheck bool
	tmuxTarget        *TmuxTarget
}

func New(cfg *config.Config, client *clockify.Client, db *store.DB, provider ai.Provider, workspaceID string) *Scheduler {
	return &Scheduler{
		cfg:         cfg,
		client:      client,
		db:          db,
		provider:    provider,
		workspaceID: workspaceID,
		tmuxTarget:  DetectTmuxTarget(),
	}
}

func (s *Scheduler) SetSkipWorkTimeCheck(skip bool) {
	s.skipWorkTimeCheck = skip
}

func (s *Scheduler) Run(ctx context.Context) error {
	if err := s.writePID(); err != nil {
		return fmt.Errorf("writing PID file: %w", err)
	}
	defer s.removePID()

	// Retry any failed entries from previous runs
	s.retryFailed(ctx)

	interval := time.Duration(s.cfg.Schedule.IntervalMinutes) * time.Minute

	if s.skipWorkTimeCheck {
		fmt.Printf("Scheduler started (interval: %s, work hours overridden)\n", interval)
	} else {
		fmt.Printf("Scheduler started (interval: %s, hours: %s–%s)\n",
			interval, s.cfg.Schedule.WorkStart, s.cfg.Schedule.WorkEnd)
	}

	for {
		nextTick := s.nextAlignedTick(time.Now(), interval)
		fmt.Printf("Next prompt at %s\n", nextTick.Format("15:04"))

		select {
		case <-ctx.Done():
			fmt.Println("\nScheduler stopped.")
			return nil
		case <-time.After(time.Until(nextTick)):
		}

		if !s.skipWorkTimeCheck && !s.isWorkTime(time.Now()) {
			continue
		}

		s.prompt(ctx, nextTick, interval)
	}
}

// showDialogWithSnooze shows the prompt dialog in a loop, handling snooze
// internally. Returns only ActionLogNow or ActionNextTimer.
func (s *Scheduler) showDialogWithSnooze(ctx context.Context) DialogAction {
	for {
		result, err := ShowPromptDialog(
			ctx,
			"clockr",
			"What did you work on this hour?",
			s.cfg.Notifications.SnoozeOptions,
		)
		if err != nil {
			// On error (including context cancellation), default to log now
			// so we don't silently skip prompts.
			return ActionLogNow
		}

		if result.Action != ActionSnooze {
			return result.Action
		}

		// Snooze: wait then re-show dialog.
		fmt.Printf("Snoozed for %d minutes.\n", result.SnoozeMinutes)
		snoozeTimer := time.NewTimer(time.Duration(result.SnoozeMinutes) * time.Minute)
		select {
		case <-ctx.Done():
			snoozeTimer.Stop()
			return ActionLogNow
		case <-snoozeTimer.C:
		}
	}
}

func (s *Scheduler) prompt(ctx context.Context, tickTime time.Time, interval time.Duration) {
	if s.cfg.Notifications.Enabled {
		// Send a system notification first so the user gets a banner + sound
		// even if the interactive dialog appears behind other windows.
		_ = SendNotification("clockr", "Time to log your work!", s.tmuxTarget)

		action := s.showDialogWithSnooze(ctx)
		if action == ActionNextTimer {
			fmt.Println("Skipped to next timer.")
			return
		}
	}

	projects, err := s.client.GetProjects(ctx, s.workspaceID)
	if err != nil {
		fmt.Printf("Error fetching projects: %v\n", err)
		return
	}
	s.client.EnrichProjectsWithClients(ctx, s.workspaceID, projects)

	startTime := tickTime.Add(-interval)
	endTime := tickTime

	var contextItems []string
	if s.cfg.Calendar.Enabled && s.cfg.Calendar.Source != "" {
		fmt.Println("Fetching calendar events...")
		fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		events, err := calendar.Fetch(fetchCtx, s.cfg.Calendar.Source, startTime, endTime)
		cancel()
		if err != nil {
			fmt.Printf("Warning: calendar fetch failed: %v\n", err)
		} else {
			for _, e := range events {
				contextItems = append(contextItems, e.Summary)
			}
		}
	}

	lastInput, _ := s.db.GetLastRawInput()
	app := tui.NewApp(startTime, endTime, s.provider, projects, s.client, s.workspaceID, s.db, interval, contextItems, lastInput)
	p := tea.NewProgram(app)

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running TUI: %v\n", err)
		return
	}

	result := app.GetResult()
	if result != nil && result.Skipped {
		fmt.Println("Entry skipped.")
	}
}

func (s *Scheduler) nextAlignedTick(now time.Time, interval time.Duration) time.Time {
	mins := int(interval.Minutes())
	if mins <= 0 {
		mins = 60
	}

	currentMinute := now.Minute()
	nextMinute := ((currentMinute / mins) + 1) * mins

	next := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	next = next.Add(time.Duration(nextMinute) * time.Minute)

	return next
}

// IsWorkTime checks whether the given time falls within configured work hours and work days.
func IsWorkTime(cfg *config.Config, t time.Time) bool {
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday = 7
	}

	isWorkDay := false
	for _, d := range cfg.Schedule.WorkDays {
		if d == weekday {
			isWorkDay = true
			break
		}
	}
	if !isWorkDay {
		return false
	}

	startH, startM := parseTime(cfg.Schedule.WorkStart)
	endH, endM := parseTime(cfg.Schedule.WorkEnd)

	nowMins := t.Hour()*60 + t.Minute()
	startMins := startH*60 + startM
	endMins := endH*60 + endM

	return nowMins >= startMins && nowMins <= endMins
}

func (s *Scheduler) isWorkTime(t time.Time) bool {
	return IsWorkTime(s.cfg, t)
}

func parseTime(s string) (int, int) {
	if len(s) == 5 && s[2] == ':' {
		h, _ := strconv.Atoi(s[:2])
		m, _ := strconv.Atoi(s[3:])
		return h, m
	}
	return 9, 0
}

func (s *Scheduler) retryFailed(ctx context.Context) {
	entries, err := s.db.GetFailedEntries()
	if err != nil || len(entries) == 0 {
		return
	}

	fmt.Printf("Retrying %d failed entries...\n", len(entries))
	for _, e := range entries {
		entry := clockify.TimeEntryRequest{
			Start:       e.StartTime.UTC().Format("2006-01-02T15:04:05Z"),
			End:         e.EndTime.UTC().Format("2006-01-02T15:04:05Z"),
			ProjectID:   e.ProjectID,
			Description: e.Description,
		}

		created, err := s.client.CreateTimeEntry(ctx, s.workspaceID, entry)
		if err != nil {
			fmt.Printf("  Retry failed for entry %d: %v\n", e.ID, err)
			continue
		}

		if err := s.db.UpdateEntryStatus(e.ID, "logged", created.ID); err != nil {
			fmt.Printf("  Failed to update entry %d status: %v\n", e.ID, err)
			continue
		}

		fmt.Printf("  Retried entry %d successfully\n", e.ID)
	}
}

func pidPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clockr.pid"), nil
}

func (s *Scheduler) writePID() error {
	path, err := pidPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func (s *Scheduler) removePID() {
	if path, err := pidPath(); err == nil {
		os.Remove(path)
	}
}

func ReadPID() (int, error) {
	path, err := pidPath()
	if err != nil {
		return 0, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("no running scheduler found")
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file")
	}

	return pid, nil
}
