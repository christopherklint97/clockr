package tui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
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

type App struct {
	state       viewState
	input       inputModel
	spinner     spinner.Model
	suggestions suggestionsModel
	edit        editModel
	result      *Result
	errMsg      string

	startTime   time.Time
	endTime     time.Time
	provider    ai.Provider
	projects    []clockify.Project
	clockify    *clockify.Client
	workspaceID string
	db          *store.DB
	interval    time.Duration
	contextItems []string
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
) *App {
	s := spinner.New()
	s.Spinner = spinner.Dot

	timeInfo := fmt.Sprintf("%s – %s (%d min)",
		startTime.Format("15:04"),
		endTime.Format("15:04"),
		int(endTime.Sub(startTime).Minutes()),
	)

	return &App{
		state:       inputView,
		input:       newInputModel(timeInfo),
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

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.input.textarea.Focus(), a.spinner.Tick)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(wsMsg)
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
		return a.spinner.View() + " Thinking..."
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
			return a, tea.Batch(a.spinner.Tick, a.queryAI(a.input.Value()))
		}
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	return a, cmd
}

func (a *App) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	a.spinner, cmd = a.spinner.Update(msg)
	return a, cmd
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

func (a *App) queryAI(description string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		suggestion, err := a.provider.MatchProjects(ctx, description, a.projects, a.interval, a.contextItems)
		return aiResponseMsg{suggestion: suggestion, err: err}
	}
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
