package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/christopherklint97/clockr/internal/ai"
	"github.com/christopherklint97/clockr/internal/clockify"
	"github.com/christopherklint97/clockr/internal/store"
)

type viewState int

const (
	inputView viewState = iota
	loadingView
	suggestionView
	editView
	confirmationView
)

type Result struct {
	Skipped bool
	Entries []store.Entry
}

type aiResponseMsg struct {
	suggestion *ai.Suggestion
	err        error
}

type submitMsg struct {
	entries []store.Entry
	err     error
}

// thinkingMsg carries a streaming text chunk from the AI provider.
type thinkingMsg struct {
	text string
}

// thinkingDoneMsg signals that the thinking stream channel has closed.
type thinkingDoneMsg struct{}

// tickMsg fires every second during loading to update elapsed time.
type tickMsg time.Time

type App struct {
	state       viewState
	input       inputModel
	spinner     spinner.Model
	suggestions suggestionsModel
	edit        editModel
	result      *Result
	errMsg      string

	startTime    time.Time
	endTime      time.Time
	provider     ai.Provider
	projects     []clockify.Project
	clockify     *clockify.Client
	workspaceID  string
	db           *store.DB
	interval     time.Duration
	contextItems []string

	thinkCh          <-chan string
	thinkingText     string
	viewport         viewport.Model
	loadingStartTime time.Time
	termWidth        int
	termHeight       int
}

func NewApp(
	startTime, endTime time.Time,
	provider ai.Provider,
	projects []clockify.Project,
	client *clockify.Client,
	workspaceID string,
	db *store.DB,
	interval time.Duration,
	contextItems []string,
	lastInput string,
) *App {
	s := spinner.New()
	s.Spinner = spinner.Dot

	timeInfo := fmt.Sprintf("%s – %s (%d min)",
		startTime.Format("15:04"),
		endTime.Format("15:04"),
		int(endTime.Sub(startTime).Minutes()),
	)

	input := newInputModel(timeInfo)
	input.lastInput = lastInput

	return &App{
		state:       inputView,
		input:       input,
		spinner:     s,
		startTime:   startTime,
		endTime:     endTime,
		provider:    provider,
		projects:    projects,
		clockify:    client,
		workspaceID: workspaceID,
		db:          db,
		interval:    interval,
		contextItems: contextItems,
	}
}

func (a *App) SetInitialInput(text string) {
	a.input.textarea.SetValue(text)
}

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.input.textarea.Focus(), a.spinner.Tick)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		a.termWidth = wsMsg.Width
		a.termHeight = wsMsg.Height
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(wsMsg)
		if a.state == loadingView {
			a.viewport.Width = a.termWidth
			a.viewport.Height = max(a.termHeight-3, 1)
		}
		return a, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			a.result = &Result{Skipped: true}
			return a, tea.Quit
		}
	case aiResponseMsg:
		return a.handleAIResponse(msg)
	case submitMsg:
		return a.handleSubmit(msg)
	case thinkingMsg:
		a.thinkingText += msg.text
		a.viewport.SetContent(a.thinkingText)
		a.viewport.GotoBottom()
		return a, readThinking(a.thinkCh)
	case thinkingDoneMsg:
		return a, nil
	case tickMsg:
		if a.state == loadingView {
			return a, tickCmd()
		}
		return a, nil
	}

	switch a.state {
	case inputView:
		return a.updateInput(msg)
	case loadingView:
		return a.updateLoading(msg)
	case suggestionView:
		return a.updateSuggestion(msg)
	case editView:
		return a.updateEdit(msg)
	case confirmationView:
		return a.updateConfirmation(msg)
	}

	return a, nil
}

func (a *App) View() string {
	switch a.state {
	case inputView:
		return a.input.View()
	case loadingView:
		elapsed := time.Since(a.loadingStartTime).Truncate(time.Second)
		header := fmt.Sprintf("%s Thinking...  %s", a.spinner.View(), dimStyle.Render(formatElapsed(elapsed)))
		separator := dimStyle.Render(strings.Repeat("─", a.termWidth))
		return header + "\n" + separator + "\n" + a.viewport.View()
	case suggestionView:
		return a.suggestions.View()
	case editView:
		return a.edit.View()
	case confirmationView:
		if a.errMsg != "" {
			return errorStyle.Render("Error: ") + a.errMsg + "\n\n" + helpStyle.Render("Press any key to exit")
		}
		return successStyle.Render("Entries logged successfully!") + "\n\n" + helpStyle.Render("Press any key to exit")
	}
	return ""
}

func (a *App) GetResult() *Result {
	return a.result
}

func (a *App) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "enter" && a.input.Value() != "" {
			a.state = loadingView
			a.thinkingText = ""
			a.loadingStartTime = time.Now()
			a.viewport = viewport.New(a.termWidth, max(a.termHeight-3, 1))
			ch := make(chan string, 100)
			a.thinkCh = ch
			return a, tea.Batch(
				a.spinner.Tick,
				a.startAI(a.input.Value(), ch),
				readThinking(ch),
				tickCmd(),
			)
		}
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	return a, cmd
}

func (a *App) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	a.spinner, cmd = a.spinner.Update(msg)
	cmds = append(cmds, cmd)
	a.viewport, cmd = a.viewport.Update(msg)
	cmds = append(cmds, cmd)
	return a, tea.Batch(cmds...)
}

func (a *App) updateSuggestion(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "a":
			return a, a.submitAllocations(a.suggestions.suggestion.Allocations)
		case "e":
			a.state = editView
			a.edit = newEditModel(a.suggestions.suggestion.Allocations, a.projects)
			return a, nil
		case "r":
			a.state = inputView
			newInput := newInputModel(a.input.timeInfo)
			newInput, _ = newInput.Update(tea.WindowSizeMsg{Width: a.input.width, Height: a.input.height})
			a.input = newInput
			return a, a.input.textarea.Focus()
		case "s":
			a.result = &Result{Skipped: true}
			return a, tea.Quit
		case "up", "k":
			if a.suggestions.cursor > 0 {
				a.suggestions.cursor--
			}
		case "down", "j":
			if a.suggestions.cursor < len(a.suggestions.suggestion.Allocations)-1 {
				a.suggestions.cursor++
			}
		}
	}
	return a, nil
}

func (a *App) updateEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "esc" && !a.edit.editing {
			a.suggestions.suggestion.Allocations = a.edit.allocations
			a.state = suggestionView
			return a, nil
		}
	}

	var cmd tea.Cmd
	a.edit, cmd = a.edit.Update(msg)
	return a, cmd
}

func (a *App) updateConfirmation(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return a, tea.Quit
	}
	return a, nil
}

func (a *App) handleAIResponse(msg aiResponseMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.state = confirmationView
		a.errMsg = msg.err.Error()
		return a, nil
	}

	a.suggestions = newSuggestionsModel(msg.suggestion)
	a.state = suggestionView
	return a, nil
}

func (a *App) handleSubmit(msg submitMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.state = confirmationView
		a.errMsg = msg.err.Error()
		return a, nil
	}

	a.result = &Result{Entries: msg.entries}
	a.state = confirmationView
	return a, nil
}

// startAI runs the AI provider in a goroutine, streaming thinking text to ch.
func (a *App) startAI(description string, ch chan<- string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		resetIdle := idleTimeout(cancel, 2*time.Minute)

		if cli, ok := a.provider.(*ai.ClaudeCLI); ok {
			cli.OnThinking = func(text string) {
				resetIdle()
				select {
				case ch <- text:
				default:
				}
			}
			defer func() { cli.OnThinking = nil }()
		}
		defer close(ch)

		suggestion, err := a.provider.MatchProjects(ctx, description, a.projects, a.interval, a.contextItems)
		return aiResponseMsg{suggestion: suggestion, err: err}
	}
}

// readThinking reads the next chunk from the thinking channel.
func readThinking(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		text, ok := <-ch
		if !ok {
			return thinkingDoneMsg{}
		}
		return thinkingMsg{text: text}
	}
}

// tickCmd returns a command that fires a tickMsg after 1 second.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// formatElapsed formats a duration as "Xs" or "Xm Ys".
func formatElapsed(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %ds", s/60, s%60)
}

// idleTimeout runs a goroutine that cancels ctx after idleLimit of no activity.
// Call the returned resetFunc from OnThinking to reset the idle timer.
func idleTimeout(cancel context.CancelFunc, idleLimit time.Duration) (resetFunc func()) {
	var mu sync.Mutex
	lastActivity := time.Now()

	reset := func() {
		mu.Lock()
		lastActivity = time.Now()
		mu.Unlock()
	}

	go func() {
		for {
			time.Sleep(5 * time.Second)
			mu.Lock()
			idle := time.Since(lastActivity)
			mu.Unlock()
			if idle >= idleLimit {
				cancel()
				return
			}
		}
	}()

	return reset
}

func (a *App) submitAllocations(allocations []ai.Allocation) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var entries []store.Entry

		for _, alloc := range allocations {
			allocDuration := time.Duration(alloc.Minutes) * time.Minute
			entryStart := a.startTime
			entryEnd := entryStart.Add(allocDuration)

			if entryEnd.After(a.endTime) {
				entryEnd = a.endTime
			}

			entry := clockify.TimeEntryRequest{
				Start:       entryStart.UTC().Format("2006-01-02T15:04:05Z"),
				End:         entryEnd.UTC().Format("2006-01-02T15:04:05Z"),
				ProjectID:   alloc.ProjectID,
				Description: alloc.Description,
			}

			created, err := a.clockify.CreateTimeEntry(ctx, a.workspaceID, entry)

			status := "logged"
			clockifyID := ""
			if err != nil {
				status = "failed"
			} else {
				clockifyID = created.ID
			}

			storeEntry := store.Entry{
				ClockifyID:  clockifyID,
				ProjectID:   alloc.ProjectID,
				ProjectName: alloc.ProjectName,
				ClientName:  alloc.ClientName,
				Description: alloc.Description,
				StartTime:   entryStart,
				EndTime:     entryEnd,
				Minutes:     alloc.Minutes,
				Status:      status,
				RawInput:    a.input.Value(),
			}

			if a.db != nil {
				a.db.InsertEntry(&storeEntry)
			}

			entries = append(entries, storeEntry)

			// Advance start time for next allocation
			a.startTime = entryEnd
		}

		return submitMsg{entries: entries}
	}
}
