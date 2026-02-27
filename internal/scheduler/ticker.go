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
	"github.com/christopherklint97/clockr/internal/clockify"
	"github.com/christopherklint97/clockr/internal/config"
	"github.com/christopherklint97/clockr/internal/store"
	"github.com/christopherklint97/clockr/internal/tui"
)

type Scheduler struct {
	cfg         *config.Config
	client      *clockify.Client
	db          *store.DB
	provider    ai.Provider
	workspaceID string
}

func New(cfg *config.Config, client *clockify.Client, db *store.DB, provider ai.Provider, workspaceID string) *Scheduler {
	return &Scheduler{
		cfg:         cfg,
		client:      client,
		db:          db,
		provider:    provider,
		workspaceID: workspaceID,
	}
}

func (s *Scheduler) Run(ctx context.Context) error {
	if err := s.writePID(); err != nil {
		return fmt.Errorf("writing PID file: %w", err)
	}
	defer s.removePID()

	// Retry any failed entries from previous runs
	s.retryFailed(ctx)

	interval := time.Duration(s.cfg.Schedule.IntervalMinutes) * time.Minute

	fmt.Printf("Scheduler started (interval: %s, hours: %s–%s)\n",
		interval, s.cfg.Schedule.WorkStart, s.cfg.Schedule.WorkEnd)

	for {
		nextTick := s.nextAlignedTick(time.Now(), interval)
		fmt.Printf("Next prompt at %s\n", nextTick.Format("15:04"))

		select {
		case <-ctx.Done():
			fmt.Println("\nScheduler stopped.")
			return nil
		case <-time.After(time.Until(nextTick)):
		}

		if !s.isWorkTime(time.Now()) {
			continue
		}

		s.prompt(ctx, nextTick, interval)
	}
}

func (s *Scheduler) prompt(ctx context.Context, tickTime time.Time, interval time.Duration) {
	if s.cfg.Notifications.Enabled {
		SendNotification("clockr", "What did you work on this hour?")
	}

	projects, err := s.client.GetProjects(ctx, s.workspaceID)
	if err != nil {
		fmt.Printf("Error fetching projects: %v\n", err)
		return
	}

	startTime := tickTime.Add(-interval)
	endTime := tickTime

	app := tui.NewApp(startTime, endTime, s.provider, projects, s.client, s.workspaceID, s.db, interval)
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

func (s *Scheduler) isWorkTime(t time.Time) bool {
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday = 7
	}

	isWorkDay := false
	for _, d := range s.cfg.Schedule.WorkDays {
		if d == weekday {
			isWorkDay = true
			break
		}
	}
	if !isWorkDay {
		return false
	}

	startH, startM := parseTime(s.cfg.Schedule.WorkStart)
	endH, endM := parseTime(s.cfg.Schedule.WorkEnd)

	nowMins := t.Hour()*60 + t.Minute()
	startMins := startH*60 + startM
	endMins := endH*60 + endM

	return nowMins >= startMins && nowMins <= endMins
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
