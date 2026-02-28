package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tj/go-naturaldate"
	"github.com/christopherklint97/clockr/internal/ai"
	"github.com/christopherklint97/clockr/internal/calendar"
	"github.com/christopherklint97/clockr/internal/clockify"
	"github.com/christopherklint97/clockr/internal/config"
	"github.com/christopherklint97/clockr/internal/github"
	"github.com/christopherklint97/clockr/internal/msgraph"
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

var calendarAuthCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate with Microsoft Graph API for calendar access",
	RunE:  runCalendarAuth,
}

var githubCmd = &cobra.Command{
	Use:   "github",
	Short: "GitHub integration commands",
}

var githubReposCmd = &cobra.Command{
	Use:   "repos",
	Short: "List saved GitHub repos",
	RunE:  runGitHubRepos,
}

var githubReposResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clear saved GitHub repos (re-prompts picker on next --github)",
	RunE:  runGitHubReposReset,
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose debug logging")

	logCmd.Flags().Bool("same", false, "Log the same project/description as the last entry")
	logCmd.Flags().Bool("repeat", false, "Pre-fill the textarea with the last description")
	logCmd.Flags().String("from", "", "Start date (YYYY-MM-DD, or natural: monday, last friday, etc.)")
	logCmd.Flags().String("to", "", "End date (YYYY-MM-DD, or natural: friday, today, etc.)")
	logCmd.Flags().Bool("github", false, "Include GitHub commit/PR context from saved repos")

	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(projectsCmd)
	rootCmd.AddCommand(configCmd)

	calendarCmd.AddCommand(calendarTestCmd)
	calendarCmd.AddCommand(calendarAuthCmd)
	rootCmd.AddCommand(calendarCmd)

	githubReposCmd.AddCommand(githubReposResetCmd)
	githubCmd.AddCommand(githubReposCmd)
	rootCmd.AddCommand(githubCmd)
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

func setupLogger(cmd *cobra.Command) *slog.Logger {
	verbose, _ := cmd.Flags().GetBool("verbose")
	level := slog.LevelError
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	}))
}

func newClockifyClient(cfg *config.Config, logger *slog.Logger) *clockify.Client {
	return clockify.NewClient(cfg.Clockify.APIKey, cfg.Clockify.BaseURL, 1*time.Hour, logger)
}

func resolveWorkspaceID(ctx context.Context, cfg *config.Config, client *clockify.Client) (string, error) {
	if cfg.Clockify.WorkspaceID != "" {
		return cfg.Clockify.WorkspaceID, nil
	}
	user, err := client.GetUser(ctx)
	if err != nil {
		return "", fmt.Errorf("getting user info: %w", err)
	}
	if user.DefaultWorkspace == "" {
		return "", fmt.Errorf("workspace ID not configured and user has no default workspace — set workspace_id in config or CLOCKIFY_WORKSPACE_ID env var")
	}
	return user.DefaultWorkspace, nil
}

func newAIProvider(cfg *config.Config, logger *slog.Logger) ai.Provider {
	return ai.NewClaudeCLI(cfg.AI.Model, logger)
}

func enrichProjectsWithClients(ctx context.Context, client *clockify.Client, workspaceID string, projects []clockify.Project, logger *slog.Logger) {
	logger.Debug("fetching clients")
	clients, err := client.GetClients(ctx, workspaceID)
	if err != nil {
		logger.Debug("failed to fetch clients, continuing without client names", "error", err)
		return
	}
	logger.Debug("clients loaded", "count", len(clients))

	clientMap := make(map[string]string, len(clients))
	for _, c := range clients {
		clientMap[c.ID] = c.Name
	}

	for i := range projects {
		if name, ok := clientMap[projects[i].ClientID]; ok {
			projects[i].ClientName = name
		}
	}
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

	logger := setupLogger(cmd)
	client := newClockifyClient(cfg, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workspaceID, err := resolveWorkspaceID(ctx, cfg, client)
	if err != nil {
		return err
	}

	provider := newAIProvider(cfg, logger)
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
	repeat, _ := cmd.Flags().GetBool("repeat")
	fromStr, _ := cmd.Flags().GetString("from")
	toStr, _ := cmd.Flags().GetString("to")
	useGitHub, _ := cmd.Flags().GetBool("github")

	// Validate flag combinations
	if (fromStr != "") != (toStr != "") {
		return fmt.Errorf("both --from and --to must be provided together")
	}
	if same && fromStr != "" {
		return fmt.Errorf("--same cannot be combined with --from/--to")
	}
	if same && useGitHub {
		return fmt.Errorf("--same cannot be combined with --github")
	}
	if same && repeat {
		return fmt.Errorf("--same cannot be combined with --repeat")
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

	logger := setupLogger(cmd)
	client := newClockifyClient(cfg, logger)
	ctx := context.Background()

	logger.Debug("resolving workspace ID")
	workspaceID, err := resolveWorkspaceID(ctx, cfg, client)
	if err != nil {
		return err
	}
	logger.Debug("workspace resolved", "workspace_id", workspaceID)

	if same {
		return runLogSame(ctx, cfg, client, workspaceID, db)
	}

	if fromStr != "" {
		return runLogBatch(ctx, cfg, client, workspaceID, db, fromStr, toStr, useGitHub, repeat, logger)
	}

	logger.Debug("fetching projects")
	projects, err := client.GetProjects(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("fetching projects: %w", err)
	}
	logger.Debug("projects loaded", "count", len(projects))
	enrichProjectsWithClients(ctx, client, workspaceID, projects, logger)

	provider := newAIProvider(cfg, logger)
	now := time.Now()
	interval := time.Duration(cfg.Schedule.IntervalMinutes) * time.Minute
	startTime := now.Add(-interval)
	endTime := now

	var contextItems []string
	if cfg.Calendar.Enabled && cfg.Calendar.Source != "" {
		fmt.Println("Fetching calendar events...")
		logger.Debug("fetching calendar events", "source", cfg.Calendar.Source, "start", startTime, "end", endTime)
		fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		events, err := fetchCalendarEvents(fetchCtx, cfg, startTime, endTime, logger)
		cancel()
		if err != nil {
			fmt.Printf("Warning: calendar fetch failed: %v\n", err)
			logger.Debug("calendar fetch error", "error", err)
		} else {
			logger.Debug("calendar events fetched", "count", len(events))
			for _, e := range events {
				contextItems = append(contextItems, e.Summary)
			}
		}
	}

	// Fetch GitHub context if requested (sent to AI via system prompt, not textarea)
	if useGitHub {
		logger.Debug("fetching GitHub context", "start", startTime, "end", endTime)
		ghItems, err := fetchGitHubContext(ctx, cfg, startTime, endTime, logger)
		if err != nil {
			fmt.Printf("Warning: GitHub fetch failed: %v\n", err)
			logger.Debug("GitHub fetch error", "error", err)
		} else {
			logger.Debug("GitHub items fetched", "count", len(ghItems))
			for _, item := range ghItems {
				contextItems = append(contextItems, item.Message)
			}
		}
	}

	lastInput, _ := db.GetLastRawInput()
	app := tui.NewApp(startTime, endTime, provider, projects, client, workspaceID, db, interval, contextItems, lastInput)
	if repeat && lastInput != "" {
		app.SetInitialInput(lastInput)
	}
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

func runLogBatch(ctx context.Context, cfg *config.Config, client *clockify.Client, workspaceID string, db *store.DB, fromStr, toStr string, useGitHub bool, repeat bool, logger *slog.Logger) error {
	from, err := parseDate(fromStr)
	if err != nil {
		return fmt.Errorf("invalid --from date: %w", err)
	}
	to, err := parseDate(toStr)
	if err != nil {
		return fmt.Errorf("invalid --to date: %w", err)
	}
	logger.Debug("batch date range parsed", "from", from.Format("2006-01-02"), "to", to.Format("2006-01-02"))
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
	logger.Debug("day slots built", "count", len(days), "dates", func() string {
		var dates []string
		for _, d := range days {
			dates = append(dates, d.Date)
		}
		return strings.Join(dates, ", ")
	}())

	logger.Debug("fetching projects")
	projects, err := client.GetProjects(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("fetching projects: %w", err)
	}
	logger.Debug("projects loaded", "count", len(projects))
	enrichProjectsWithClients(ctx, client, workspaceID, projects, logger)

	// Fetch calendar events for the full range and attach to day slots (per-day AI context)
	if cfg.Calendar.Enabled && cfg.Calendar.Source != "" {
		fmt.Println("Fetching calendar events...")
		rangeStart := days[0].Start
		rangeEnd := days[len(days)-1].End
		logger.Debug("fetching calendar events", "source", cfg.Calendar.Source, "start", rangeStart, "end", rangeEnd)
		fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		events, err := fetchCalendarEvents(fetchCtx, cfg, rangeStart, rangeEnd, logger)
		cancel()
		if err != nil {
			fmt.Printf("Warning: calendar fetch failed: %v\n", err)
			logger.Debug("calendar fetch error", "error", err)
		} else {
			logger.Debug("calendar events fetched", "count", len(events))
			grouped := calendar.GroupByDay(events)
			for i, d := range days {
				if dayEvents, ok := grouped[d.Date]; ok {
					for _, e := range dayEvents {
						days[i].Events = append(days[i].Events, e.Summary)
					}
				}
			}
		}
	}

	// Fetch GitHub commits/PRs and attach to day slots (sent to AI via system prompt, not textarea)
	if useGitHub {
		rangeStart := days[0].Start
		rangeEnd := days[len(days)-1].End
		logger.Debug("fetching GitHub context", "start", rangeStart, "end", rangeEnd)
		ghItems, err := fetchGitHubContext(ctx, cfg, rangeStart, rangeEnd, logger)
		if err != nil {
			fmt.Printf("Warning: GitHub fetch failed: %v\n", err)
			logger.Debug("GitHub fetch error", "error", err)
		} else if len(ghItems) > 0 {
			logger.Debug("GitHub items fetched", "count", len(ghItems))
			grouped := github.GroupByDay(ghItems)
			for i, d := range days {
				if dayItems, ok := grouped[d.Date]; ok {
					for _, item := range dayItems {
						days[i].Commits = append(days[i].Commits, item.Message)
					}
				}
			}
		}
	}

	provider := newAIProvider(cfg, logger)
	lastInput, _ := db.GetLastRawInput()
	app := tui.NewBatchApp(days, provider, projects, client, workspaceID, db, lastInput)
	if repeat && lastInput != "" {
		app.SetInitialInput(lastInput)
	}
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

func parseDate(s string) (time.Time, error) {
	loc := time.Now().Location()
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t, nil
	}
	t, err := naturaldate.Parse(s, time.Now(), naturaldate.WithDirection(naturaldate.Past))
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse date %q (use YYYY-MM-DD or natural language like 'monday', 'last friday')", s)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc), nil
}

func runLogSame(ctx context.Context, cfg *config.Config, client *clockify.Client, workspaceID string, db *store.DB) error {
	last, err := db.GetLastEntry()
	if err != nil {
		return fmt.Errorf("getting last entry: %w", err)
	}
	if last == nil {
		return fmt.Errorf("no previous entries found")
	}

	// Verify the project still exists in Clockify
	projects, err := client.GetProjects(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("fetching projects: %w", err)
	}
	found := false
	for _, p := range projects {
		if p.ID == last.ProjectID {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("project %q (%s) from last entry no longer exists in Clockify — use 'clockr log' instead", last.ProjectName, last.ProjectID)
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
		ClientName:  last.ClientName,
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
		projectDisplay := e.ProjectName
		if e.ClientName != "" {
			projectDisplay = e.ClientName + " / " + e.ProjectName
		}
		fmt.Printf("  %s–%s  %dmin  %-30s  %s  [%s]\n",
			localStart.Format("15:04"),
			localEnd.Format("15:04"),
			e.Minutes,
			projectDisplay,
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

	logger := setupLogger(cmd)
	client := newClockifyClient(cfg, logger)
	ctx := context.Background()

	workspaceID, err := resolveWorkspaceID(ctx, cfg, client)
	if err != nil {
		return err
	}

	projects, err := client.GetProjects(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("fetching projects: %w", err)
	}
	enrichProjectsWithClients(ctx, client, workspaceID, projects, logger)

	if len(projects) == 0 {
		fmt.Println("No projects found.")
		return nil
	}

	fmt.Printf("Found %d projects:\n\n", len(projects))
	for _, p := range projects {
		if p.ClientName != "" {
			fmt.Printf("  %s  %s / %s\n", p.ID, p.ClientName, p.Name)
		} else {
			fmt.Printf("  %s  %s\n", p.ID, p.Name)
		}
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

	logger := setupLogger(cmd)
	events, err := fetchCalendarEvents(ctx, cfg, windowStart, windowEnd, logger)
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
# base_url = ""  # set for regional servers (e.g. https://euc1.clockify.me/api/v1)

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
# For Microsoft Graph API calendar, set source = "graph" and configure below:
# [calendar.graph]
# client_id = ""  # Azure AD Application (client) ID
# tenant_id = ""  # Azure AD Directory (tenant) ID

[github]
# token = ""  # optional: uses 'gh auth token' or GITHUB_TOKEN env var by default
# repos = []  # auto-populated after first --github run via repo picker
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

func fetchCalendarEvents(ctx context.Context, cfg *config.Config, start, end time.Time, logger *slog.Logger) ([]calendar.Event, error) {
	if cfg.Calendar.Source == "graph" {
		clientID := cfg.Calendar.Graph.ClientID
		tenantID := cfg.Calendar.Graph.TenantID
		if clientID == "" {
			return nil, fmt.Errorf("calendar.graph.client_id not configured — see 'clockr calendar auth' setup instructions")
		}
		if tenantID == "" {
			return nil, fmt.Errorf("calendar.graph.tenant_id not configured — set it in config or MSGRAPH_TENANT_ID env var")
		}

		auth := msgraph.NewAuth(clientID, tenantID, logger)
		graphClient := msgraph.NewClient(auth, logger)
		return graphClient.FetchEvents(ctx, start, end)
	}

	return calendar.Fetch(ctx, cfg.Calendar.Source, start, end)
}

func runCalendarAuth(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	clientID := cfg.Calendar.Graph.ClientID
	tenantID := cfg.Calendar.Graph.TenantID
	if clientID == "" {
		return fmt.Errorf("calendar.graph.client_id not configured — add [calendar.graph] section with client_id to your config")
	}
	if tenantID == "" {
		return fmt.Errorf("calendar.graph.tenant_id not configured — add tenant_id to [calendar.graph] config section")
	}

	logger := setupLogger(cmd)
	auth := msgraph.NewAuth(clientID, tenantID, logger)

	ctx := context.Background()
	dcResp, err := auth.StartDeviceCodeFlow(ctx)
	if err != nil {
		return fmt.Errorf("starting device code flow: %w", err)
	}

	fmt.Println()
	fmt.Println(dcResp.Message)
	fmt.Println()

	fmt.Println("Waiting for authorization...")
	tokens, err := auth.PollForToken(ctx, dcResp.DeviceCode, dcResp.Interval)
	if err != nil {
		return fmt.Errorf("authorization failed: %w", err)
	}

	if err := msgraph.SaveTokens(tokens); err != nil {
		return fmt.Errorf("saving tokens: %w", err)
	}

	fmt.Println("Authentication successful! Tokens saved.")
	fmt.Println("You can now use source = \"graph\" in your [calendar] config.")
	return nil
}

func fetchGitHubContext(ctx context.Context, cfg *config.Config, start, end time.Time, logger *slog.Logger) ([]github.CommitContext, error) {
	logger.Debug("resolving GitHub token")
	token, err := github.ResolveToken(cfg.GitHub.Token)
	if err != nil {
		return nil, err
	}
	logger.Debug("GitHub token resolved")

	ghClient := github.NewClient(token, logger)

	repos := cfg.GitHub.Repos
	if len(repos) == 0 {
		// Launch repo picker
		fmt.Println("Fetching your GitHub repos...")
		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		allRepos, err := ghClient.GetRepos(fetchCtx)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("fetching GitHub repos: %w", err)
		}
		if len(allRepos) == 0 {
			return nil, fmt.Errorf("no GitHub repos found for your account")
		}

		picker := tui.NewRepoPickerApp(allRepos)
		p := tea.NewProgram(picker)
		if _, err := p.Run(); err != nil {
			return nil, fmt.Errorf("running repo picker: %w", err)
		}

		result := picker.GetResult()
		if result == nil || result.Canceled || len(result.Repos) == 0 {
			return nil, fmt.Errorf("no repos selected")
		}

		repos = result.Repos
		if err := config.SaveGitHubRepos(repos); err != nil {
			fmt.Printf("Warning: could not save repo selection: %v\n", err)
		} else {
			fmt.Printf("Saved %d repos to config.\n", len(repos))
		}
	}

	fmt.Printf("Fetching GitHub activity from %d repos...\n", len(repos))
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return github.Fetch(fetchCtx, ghClient, repos, start, end)
}

func runGitHubRepos(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.GitHub.Repos) == 0 {
		fmt.Println("No GitHub repos saved. Run 'clockr log --github' to select repos.")
		return nil
	}

	fmt.Printf("Saved repos (%d):\n\n", len(cfg.GitHub.Repos))
	for _, r := range cfg.GitHub.Repos {
		fmt.Printf("  %s\n", r)
	}
	return nil
}

func runGitHubReposReset(cmd *cobra.Command, args []string) error {
	if err := config.SaveGitHubRepos([]string{}); err != nil {
		return fmt.Errorf("clearing saved repos: %w", err)
	}
	fmt.Println("GitHub repos cleared. Next --github run will prompt for selection.")
	return nil
}
