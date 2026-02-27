package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/christopherklint97/clockr/internal/ai"
	"github.com/christopherklint97/clockr/internal/calendar"
	"github.com/christopherklint97/clockr/internal/clockify"
	"github.com/christopherklint97/clockr/internal/config"
	"github.com/christopherklint97/clockr/internal/scheduler"
	"github.com/christopherklint97/clockr/internal/store"
	"github.com/christopherklint97/clockr/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "clockr",
	Short: "Time-tracking assistant powered by AI",
	Long:  "clockr prompts you periodically, takes plain-English descriptions of your work, and creates Clockify time entries.",
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the time-tracking scheduler",
	RunE:  runStart,
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running scheduler",
	RunE:  runStop,
}

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Log a time entry interactively",
	RunE:  runLog,
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show today's logged entries",
	RunE:  runStatus,
}

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "List Clockify projects",
	RunE:  runProjects,
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Open config file in your editor",
	RunE:  runConfig,
}

var calendarCmd = &cobra.Command{
	Use:   "calendar",
	Short: "Calendar integration commands",
}

var calendarTestCmd = &cobra.Command{
	Use:   "test",
	Short: "Test calendar integration by fetching upcoming events",
	RunE:  runCalendarTest,
}

func init() {
	logCmd.Flags().Bool("same", false, "Log the same project/description as the last entry")
	logCmd.Flags().String("from", "", "Start date for batch logging (YYYY-MM-DD)")
	logCmd.Flags().String("to", "", "End date for batch logging (YYYY-MM-DD)")

	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(projectsCmd)
	rootCmd.AddCommand(configCmd)

	calendarCmd.AddCommand(calendarTestCmd)
	rootCmd.AddCommand(calendarCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if cfg.Clockify.APIKey == "" {
		return nil, fmt.Errorf("clockify API key not configured — run 'clockr config' to set it up")
	}
	return cfg, nil
}

func newClockifyClient(cfg *config.Config) *clockify.Client {
	return clockify.NewClient(cfg.Clockify.APIKey, 1*time.Hour)
}

func resolveWorkspaceID(ctx context.Context, cfg *config.Config, client *clockify.Client) (string, error) {
	if cfg.Clockify.WorkspaceID != "" {
		return cfg.Clockify.WorkspaceID, nil
	}
	user, err := client.GetUser(ctx)
	if err != nil {
		return "", fmt.Errorf("getting user info: %w", err)
	}
	return user.DefaultWorkspace, nil
}

func newAIProvider(cfg *config.Config) ai.Provider {
	return ai.NewClaudeCLI(cfg.AI.Model)
}

func runStart(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	db, err := store.Open()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	client := newClockifyClient(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workspaceID, err := resolveWorkspaceID(ctx, cfg, client)
	if err != nil {
		return err
	}

	provider := newAIProvider(cfg)
	sched := scheduler.New(cfg, client, db, provider, workspaceID)

	// Handle graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	return sched.Run(ctx)
}

func runStop(cmd *cobra.Command, args []string) error {
	pid, err := scheduler.ReadPID()
	if err != nil {
		return err
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process %d: %w", pid, err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("sending stop signal: %w", err)
	}

	fmt.Printf("Sent stop signal to clockr (PID %d)\n", pid)
	return nil
}

func runLog(cmd *cobra.Command, args []string) error {
	same, _ := cmd.Flags().GetBool("same")
	fromStr, _ := cmd.Flags().GetString("from")
	toStr, _ := cmd.Flags().GetString("to")

	// Validate flag combinations
	if (fromStr != "") != (toStr != "") {
		return fmt.Errorf("both --from and --to must be provided together")
	}
	if same && fromStr != "" {
		return fmt.Errorf("--same cannot be combined with --from/--to")
	}

	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	db, err := store.Open()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	client := newClockifyClient(cfg)
	ctx := context.Background()

	workspaceID, err := resolveWorkspaceID(ctx, cfg, client)
	if err != nil {
		return err
	}

	if same {
		return runLogSame(ctx, cfg, client, workspaceID, db)
	}

	if fromStr != "" {
		return runLogBatch(ctx, cfg, client, workspaceID, db, fromStr, toStr)
	}

	projects, err := client.GetProjects(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("fetching projects: %w", err)
	}

	provider := newAIProvider(cfg)
	now := time.Now()
	interval := time.Duration(cfg.Schedule.IntervalMinutes) * time.Minute
	startTime := now.Add(-interval)
	endTime := now

	prefill := ""
	if cfg.Calendar.Enabled && cfg.Calendar.Source != "" {
		fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		events, err := calendar.Fetch(fetchCtx, cfg.Calendar.Source, startTime, endTime)
		cancel()
		if err != nil {
			fmt.Printf("Warning: calendar fetch failed: %v\n", err)
		} else {
			prefill = calendar.FormatPrefill(events)
		}
	}

	app := tui.NewApp(startTime, endTime, provider, projects, client, workspaceID, db, interval, prefill)
	p := tea.NewProgram(app)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running TUI: %w", err)
	}

	result := app.GetResult()
	if result != nil && result.Skipped {
		fmt.Println("Entry skipped.")
	}

	return nil
}

func runLogBatch(ctx context.Context, cfg *config.Config, client *clockify.Client, workspaceID string, db *store.DB, fromStr, toStr string) error {
	from, err := time.ParseInLocation("2006-01-02", fromStr, time.Now().Location())
	if err != nil {
		return fmt.Errorf("invalid --from date %q (expected YYYY-MM-DD): %w", fromStr, err)
	}
	to, err := time.ParseInLocation("2006-01-02", toStr, time.Now().Location())
	if err != nil {
		return fmt.Errorf("invalid --to date %q (expected YYYY-MM-DD): %w", toStr, err)
	}
	if to.Before(from) {
		return fmt.Errorf("--to date must be on or after --from date")
	}

	days, err := buildDaySlots(cfg, from, to)
	if err != nil {
		return err
	}
	if len(days) == 0 {
		return fmt.Errorf("no work days in the range %s to %s (check work_days config)", fromStr, toStr)
	}
	if len(days) > 10 {
		return fmt.Errorf("batch limited to 10 work days, got %d (narrow the date range)", len(days))
	}

	projects, err := client.GetProjects(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("fetching projects: %w", err)
	}

	// Fetch calendar events for the full range and attach to day slots
	prefill := ""
	if cfg.Calendar.Enabled && cfg.Calendar.Source != "" {
		rangeStart := days[0].Start
		rangeEnd := days[len(days)-1].End
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		events, err := calendar.Fetch(fetchCtx, cfg.Calendar.Source, rangeStart, rangeEnd)
		cancel()
		if err != nil {
			fmt.Printf("Warning: calendar fetch failed: %v\n", err)
		} else {
			grouped := calendar.GroupByDay(events)
			var allSummaries []string
			for i, d := range days {
				if dayEvents, ok := grouped[d.Date]; ok {
					for _, e := range dayEvents {
						days[i].Events = append(days[i].Events, e.Summary)
						allSummaries = append(allSummaries, e.Summary)
					}
				}
			}
			prefill = strings.Join(allSummaries, "; ")
		}
	}

	provider := newAIProvider(cfg)
	app := tui.NewBatchApp(days, provider, projects, client, workspaceID, db, prefill)
	p := tea.NewProgram(app)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("running batch TUI: %w", err)
	}

	result := app.GetResult()
	if result != nil && result.Skipped {
		fmt.Println("Batch entry skipped.")
	}

	return nil
}

func buildDaySlots(cfg *config.Config, from, to time.Time) ([]ai.DaySlot, error) {
	workStartH, workStartM, err := parseTimeConfig(cfg.Schedule.WorkStart)
	if err != nil {
		return nil, fmt.Errorf("parsing work_start: %w", err)
	}
	workEndH, workEndM, err := parseTimeConfig(cfg.Schedule.WorkEnd)
	if err != nil {
		return nil, fmt.Errorf("parsing work_end: %w", err)
	}

	workDays := make(map[int]bool)
	for _, d := range cfg.Schedule.WorkDays {
		workDays[d] = true
	}

	var days []ai.DaySlot
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		// Convert Go weekday (Sun=0) to ISO weekday (Mon=1..Sun=7)
		goWd := int(d.Weekday())
		isoWd := goWd
		if goWd == 0 {
			isoWd = 7
		}
		if !workDays[isoWd] {
			continue
		}

		start := time.Date(d.Year(), d.Month(), d.Day(), workStartH, workStartM, 0, 0, d.Location())
		end := time.Date(d.Year(), d.Month(), d.Day(), workEndH, workEndM, 0, 0, d.Location())
		minutes := int(end.Sub(start).Minutes())

		days = append(days, ai.DaySlot{
			Date:    d.Format("2006-01-02"),
			Weekday: d.Weekday().String(),
			Start:   start,
			End:     end,
			Minutes: minutes,
		})
	}

	return days, nil
}

func parseTimeConfig(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM format, got %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid hour in %q: %w", s, err)
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid minute in %q: %w", s, err)
	}
	return h, m, nil
}

func runLogSame(ctx context.Context, cfg *config.Config, client *clockify.Client, workspaceID string, db *store.DB) error {
	last, err := db.GetLastEntry()
	if err != nil {
		return fmt.Errorf("getting last entry: %w", err)
	}
	if last == nil {
		return fmt.Errorf("no previous entries found")
	}

	now := time.Now()
	interval := time.Duration(cfg.Schedule.IntervalMinutes) * time.Minute
	startTime := now.Add(-interval)
	endTime := now

	entry := clockify.TimeEntryRequest{
		Start:       startTime.UTC().Format("2006-01-02T15:04:05Z"),
		End:         endTime.UTC().Format("2006-01-02T15:04:05Z"),
		ProjectID:   last.ProjectID,
		Description: last.Description,
	}

	created, err := client.CreateTimeEntry(ctx, workspaceID, entry)

	status := "logged"
	clockifyID := ""
	if err != nil {
		status = "failed"
		fmt.Printf("Warning: failed to create Clockify entry: %v\n", err)
	} else {
		clockifyID = created.ID
	}

	storeEntry := store.Entry{
		ClockifyID:  clockifyID,
		ProjectID:   last.ProjectID,
		ProjectName: last.ProjectName,
		Description: last.Description,
		StartTime:   startTime,
		EndTime:     endTime,
		Minutes:     int(interval.Minutes()),
		Status:      status,
		RawInput:    "(--same)",
	}

	if _, err := db.InsertEntry(&storeEntry); err != nil {
		return fmt.Errorf("saving entry: %w", err)
	}

	fmt.Printf("Logged: %s — %s (%dmin) [%s]\n",
		storeEntry.ProjectName, storeEntry.Description, storeEntry.Minutes, status)

	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	db, err := store.Open()
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer db.Close()

	entries, err := db.GetTodayEntries()
	if err != nil {
		return fmt.Errorf("fetching today's entries: %w", err)
	}

	if len(entries) == 0 {
		fmt.Println("No entries logged today.")
		return nil
	}

	totalMinutes := 0
	fmt.Println("Today's entries:")
	fmt.Println()
	for _, e := range entries {
		localStart := e.StartTime.Local()
		localEnd := e.EndTime.Local()
		fmt.Printf("  %s–%s  %dmin  %-20s  %s  [%s]\n",
			localStart.Format("15:04"),
			localEnd.Format("15:04"),
			e.Minutes,
			e.ProjectName,
			e.Description,
			e.Status,
		)
		totalMinutes += e.Minutes
	}

	hours := totalMinutes / 60
	mins := totalMinutes % 60
	fmt.Printf("\nTotal: %dh %dmin (%d entries)\n", hours, mins, len(entries))

	return nil
}

func runProjects(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	client := newClockifyClient(cfg)
	ctx := context.Background()

	workspaceID, err := resolveWorkspaceID(ctx, cfg, client)
	if err != nil {
		return err
	}

	projects, err := client.GetProjects(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("fetching projects: %w", err)
	}

	if len(projects) == 0 {
		fmt.Println("No projects found.")
		return nil
	}

	fmt.Printf("Found %d projects:\n\n", len(projects))
	for _, p := range projects {
		fmt.Printf("  %s  %s\n", p.ID, p.Name)
	}

	return nil
}

func runCalendarTest(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if !cfg.Calendar.Enabled || cfg.Calendar.Source == "" {
		return fmt.Errorf("calendar not configured — add [calendar] section to config with enabled = true and source = \"...\"")
	}

	now := time.Now()
	windowStart := now.Add(-24 * time.Hour)
	windowEnd := now.Add(7 * 24 * time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	events, err := calendar.Fetch(ctx, cfg.Calendar.Source, windowStart, windowEnd)
	if err != nil {
		return fmt.Errorf("fetching calendar: %w", err)
	}

	if len(events) == 0 {
		fmt.Println("No events found in the past 24h to next 7 days.")
		return nil
	}

	fmt.Printf("Found %d events:\n\n", len(events))
	for _, e := range events {
		fmt.Printf("  %s – %s  %s\n",
			e.StartTime.Local().Format("Mon Jan 02 15:04"),
			e.EndTime.Local().Format("15:04"),
			e.Summary,
		)
	}

	fmt.Printf("\nPrefill text: %s\n", calendar.FormatPrefill(events))
	return nil
}

func runConfig(cmd *cobra.Command, args []string) error {
	if err := config.EnsureConfigDir(); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	configPath, err := config.ConfigPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Create default config file
		cfg := config.DefaultConfig()
		data := fmt.Sprintf(`[clockify]
api_key = "%s"
workspace_id = "%s"

[schedule]
interval_minutes = %d
work_start = "%s"
work_end = "%s"
work_days = [1, 2, 3, 4, 5]

[ai]
provider = "%s"
model = "%s"

[notifications]
enabled = %t
reminder_delay_seconds = %d

[calendar]
enabled = %t
source = "%s"
`,
			cfg.Clockify.APIKey,
			cfg.Clockify.WorkspaceID,
			cfg.Schedule.IntervalMinutes,
			cfg.Schedule.WorkStart,
			cfg.Schedule.WorkEnd,
			cfg.AI.Provider,
			cfg.AI.Model,
			cfg.Notifications.Enabled,
			cfg.Notifications.ReminderDelay,
			cfg.Calendar.Enabled,
			cfg.Calendar.Source,
		)
		if err := os.WriteFile(configPath, []byte(data), 0644); err != nil {
			return fmt.Errorf("writing default config: %w", err)
		}
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	fmt.Printf("Opening %s with %s...\n", configPath, editor)

	proc := os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	}
	process, err := os.StartProcess(editor, []string{editor, configPath}, &proc)
	if err != nil {
		// If editor fails, just print the path
		fmt.Printf("Could not open editor. Config file is at: %s\n", configPath)
		return nil
	}
	_, err = process.Wait()
	return err
}
