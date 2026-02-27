package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/christopherklint97/clockr/internal/ai"
	"github.com/christopherklint97/clockr/internal/clockify"
	"github.com/christopherklint97/clockr/internal/store"
)

type batchViewState int

const (
	batchInputView batchViewState = iota
	batchLoadingView
	batchSuggestionView
	batchEditView
	batchConfirmationView
)

type batchAIResponseMsg struct {
	suggestion *ai.BatchSuggestion
	err        error
}

type batchSubmitMsg struct {
	entries []store.Entry
	err     error
}

// BatchApp is the Bubbletea model for batch/multi-day time entry.
type BatchApp struct {
	state       batchViewState
	input       inputModel
	spinner     spinner.Model
	suggestions batchSuggestionsModel
	edit        batchEditModel
	result      *Result
	errMsg      string

	days        []ai.DaySlot
	provider    ai.Provider
	projects    []clockify.Project
	clockify    *clockify.Client
	workspaceID string
	db          *store.DB
}

func NewBatchApp(
	days []ai.DaySlot,
	provider ai.Provider,
	projects []clockify.Project,
	client *clockify.Client,
	workspaceID string,
	db *store.DB,
) *BatchApp {
	s := spinner.New()
	s.Spinner = spinner.Dot

	totalDays := len(days)
	totalMin := 0
	for _, d := range days {
		totalMin += d.Minutes
	}
	timeInfo := fmt.Sprintf("Batch: %s to %s (%d days, %d min total)",
		days[0].Date, days[totalDays-1].Date, totalDays, totalMin)

	return &BatchApp{
		state:       batchInputView,
		input:       newInputModel(timeInfo),
		spinner:     s,
		days:        days,
		provider:    provider,
		projects:    projects,
		clockify:    client,
		workspaceID: workspaceID,
		db:          db,
	}
}

func (a *BatchApp) Init() tea.Cmd {
	return tea.Batch(a.input.textarea.Focus(), a.spinner.Tick)
}

func (a *BatchApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case batchAIResponseMsg:
		return a.handleAIResponse(msg)
	case batchSubmitMsg:
		return a.handleSubmit(msg)
	}

	switch a.state {
	case batchInputView:
		return a.updateInput(msg)
	case batchLoadingView:
		return a.updateLoading(msg)
	case batchSuggestionView:
		return a.updateSuggestion(msg)
	case batchEditView:
		return a.updateEdit(msg)
	case batchConfirmationView:
		return a.updateConfirmation(msg)
	}

	return a, nil
}

func (a *BatchApp) View() string {
	switch a.state {
	case batchInputView:
		return a.input.View()
	case batchLoadingView:
		return a.spinner.View() + " Thinking (batch mode, this may take a moment)..."
	case batchSuggestionView:
		return a.suggestions.View()
	case batchEditView:
		return a.edit.View()
	case batchConfirmationView:
		if a.errMsg != "" {
			return errorStyle.Render("Error: ") + a.errMsg + "\n\n" + helpStyle.Render("Press any key to exit")
		}
		return a.confirmationView()
	}
	return ""
}

func (a *BatchApp) GetResult() *Result {
	return a.result
}

func (a *BatchApp) updateInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "enter" && a.input.Value() != "" {
			a.state = batchLoadingView
			return a, tea.Batch(a.spinner.Tick, a.queryAI(a.input.Value()))
		}
	}

	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	return a, cmd
}

func (a *BatchApp) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	a.spinner, cmd = a.spinner.Update(msg)
	return a, cmd
}

func (a *BatchApp) updateSuggestion(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "a":
			return a, a.submitAllocations(a.suggestions.suggestion.Allocations)
		case "e":
			a.state = batchEditView
			a.edit = newBatchEditModel(a.suggestions.suggestion.Allocations, a.projects)
			return a, nil
		case "r":
			a.state = batchInputView
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

func (a *BatchApp) updateEdit(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "esc" && !a.edit.editing {
			a.suggestions.suggestion.Allocations = a.edit.allocations
			a.state = batchSuggestionView
			return a, nil
		}
	}

	var cmd tea.Cmd
	a.edit, cmd = a.edit.Update(msg)
	return a, cmd
}

func (a *BatchApp) updateConfirmation(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		return a, tea.Quit
	}
	return a, nil
}

func (a *BatchApp) handleAIResponse(msg batchAIResponseMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.state = batchConfirmationView
		a.errMsg = msg.err.Error()
		return a, nil
	}

	a.suggestions = newBatchSuggestionsModel(msg.suggestion)
	a.state = batchSuggestionView
	return a, nil
}

func (a *BatchApp) handleSubmit(msg batchSubmitMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		a.state = batchConfirmationView
		a.errMsg = msg.err.Error()
		return a, nil
	}

	a.result = &Result{Entries: msg.entries}
	a.state = batchConfirmationView
	return a, nil
}

func (a *BatchApp) queryAI(description string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		suggestion, err := a.provider.MatchProjectsBatch(ctx, description, a.projects, a.days)
		return batchAIResponseMsg{suggestion: suggestion, err: err}
	}
}

func (a *BatchApp) submitAllocations(allocations []ai.BatchAllocation) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		var entries []store.Entry

		for _, alloc := range allocations {
			entryStart, err := parseBatchTime(alloc.Date, alloc.StartTime)
			if err != nil {
				return batchSubmitMsg{err: fmt.Errorf("parsing start time for %s: %w", alloc.Date, err)}
			}
			entryEnd, err := parseBatchTime(alloc.Date, alloc.EndTime)
			if err != nil {
				return batchSubmitMsg{err: fmt.Errorf("parsing end time for %s: %w", alloc.Date, err)}
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
		}

		return batchSubmitMsg{entries: entries}
	}
}

func (a *BatchApp) confirmationView() string {
	if a.result == nil || len(a.result.Entries) == 0 {
		return successStyle.Render("No entries to log.") + "\n\n" + helpStyle.Render("Press any key to exit")
	}

	dayCount := make(map[string]int)
	dayMinutes := make(map[string]int)
	for _, e := range a.result.Entries {
		date := e.StartTime.Local().Format("2006-01-02")
		dayCount[date]++
		dayMinutes[date] += e.Minutes
	}

	var sb strings.Builder
	sb.WriteString(successStyle.Render(fmt.Sprintf("Logged %d entries across %d days!", len(a.result.Entries), len(dayCount))))
	sb.WriteString("\n\n")

	for _, d := range a.days {
		if count, ok := dayCount[d.Date]; ok {
			sb.WriteString(fmt.Sprintf("  %s %s: %d entries, %d min\n", d.Date, d.Weekday, count, dayMinutes[d.Date]))
		}
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("Press any key to exit"))
	return sb.String()
}

func parseBatchTime(date, timeStr string) (time.Time, error) {
	combined := date + " " + timeStr
	return time.ParseInLocation("2006-01-02 15:04", combined, time.Now().Location())
}

// --- Batch suggestions model ---

type batchSuggestionsModel struct {
	suggestion *ai.BatchSuggestion
	cursor     int
}

func newBatchSuggestionsModel(s *ai.BatchSuggestion) batchSuggestionsModel {
	return batchSuggestionsModel{suggestion: s}
}

func (m batchSuggestionsModel) View() string {
	if m.suggestion.Clarification != "" {
		return warningStyle.Render("Clarification needed: ") + m.suggestion.Clarification + "\n\n" +
			helpStyle.Render("[r]etry with more detail • [s]kip")
	}

	var sb strings.Builder
	sb.WriteString(titleStyle.Render("Suggested Batch Allocations"))
	sb.WriteString("\n")

	// Group allocations by date for display
	type dayGroup struct {
		date        string
		allocations []int // indices into the full allocations slice
		totalMin    int
	}
	var groups []dayGroup
	groupMap := make(map[string]int)

	for i, a := range m.suggestion.Allocations {
		idx, ok := groupMap[a.Date]
		if !ok {
			idx = len(groups)
			groupMap[a.Date] = idx
			groups = append(groups, dayGroup{date: a.Date})
		}
		groups[idx].allocations = append(groups[idx].allocations, i)
		groups[idx].totalMin += a.Minutes
	}

	globalIdx := 0
	for _, g := range groups {
		// Parse date for weekday display
		weekday := ""
		if t, err := time.Parse("2006-01-02", g.date); err == nil {
			weekday = t.Weekday().String()[:3]
		}

		dayHeader := fmt.Sprintf("%s %s (%d min)", weekday, g.date, g.totalMin)
		sb.WriteString(subtitleStyle.Render(dayHeader))
		sb.WriteString("\n")

		for _, allocIdx := range g.allocations {
			a := m.suggestion.Allocations[allocIdx]
			prefix := "  "
			if globalIdx == m.cursor {
				prefix = "> "
			}

			confidence := fmt.Sprintf("%.0f%%", a.Confidence*100)
			line := fmt.Sprintf("%s%-20s  %3dmin  %s  %s–%s  %s",
				prefix,
				a.ProjectName,
				a.Minutes,
				dimStyle.Render(confidence),
				a.StartTime,
				a.EndTime,
				a.Description,
			)

			if globalIdx == m.cursor {
				line = highlightStyle.Render(line)
			}

			sb.WriteString(line)
			sb.WriteString("\n")
			globalIdx++
		}
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("[a]ccept all • [e]dit • [r]etry • [s]kip"))

	return boxStyle.Render(sb.String())
}

// --- Batch edit model ---

type batchEditField int

const (
	batchEditProject batchEditField = iota
	batchEditMinutes
	batchEditDescription
	batchEditStartTime
	batchEditEndTime
)

type batchEditModel struct {
	allocations []ai.BatchAllocation
	projects    []clockify.Project
	cursor      int
	field       batchEditField
	textInput   textinput.Model
	editing     bool
	filtered    []clockify.Project
}

func newBatchEditModel(allocations []ai.BatchAllocation, projects []clockify.Project) batchEditModel {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Width = 50

	return batchEditModel{
		allocations: allocations,
		projects:    projects,
		textInput:   ti,
	}
}

func (m batchEditModel) Update(msg tea.Msg) (batchEditModel, tea.Cmd) {
	if m.editing {
		return m.updateEditing(msg)
	}
	return m.updateNavigating(msg)
}

func (m batchEditModel) updateNavigating(msg tea.Msg) (batchEditModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.allocations)-1 {
				m.cursor++
			}
		case "tab":
			m.field = (m.field + 1) % 5
		case "enter":
			m.editing = true
			m.textInput.Focus()
			alloc := m.allocations[m.cursor]
			switch m.field {
			case batchEditProject:
				m.textInput.SetValue("")
				m.textInput.Placeholder = "Search project..."
				m.filtered = m.projects
			case batchEditMinutes:
				m.textInput.SetValue(strconv.Itoa(alloc.Minutes))
				m.textInput.Placeholder = "Minutes"
			case batchEditDescription:
				m.textInput.SetValue(alloc.Description)
				m.textInput.Placeholder = "Description"
			case batchEditStartTime:
				m.textInput.SetValue(alloc.StartTime)
				m.textInput.Placeholder = "Start time (HH:MM)"
			case batchEditEndTime:
				m.textInput.SetValue(alloc.EndTime)
				m.textInput.Placeholder = "End time (HH:MM)"
			}
			return m, m.textInput.Focus()
		}
	}
	return m, nil
}

func (m batchEditModel) updateEditing(msg tea.Msg) (batchEditModel, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			m.applyEdit()
			m.editing = false
			m.textInput.Blur()
			return m, nil
		case "esc":
			m.editing = false
			m.textInput.Blur()
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)

	if m.field == batchEditProject {
		query := strings.ToLower(m.textInput.Value())
		m.filtered = nil
		for _, p := range m.projects {
			if strings.Contains(strings.ToLower(p.Name), query) {
				m.filtered = append(m.filtered, p)
			}
		}
	}

	return m, cmd
}

func (m *batchEditModel) applyEdit() {
	switch m.field {
	case batchEditProject:
		if len(m.filtered) > 0 {
			m.allocations[m.cursor].ProjectID = m.filtered[0].ID
			m.allocations[m.cursor].ProjectName = m.filtered[0].Name
		}
	case batchEditMinutes:
		if v, err := strconv.Atoi(m.textInput.Value()); err == nil && v > 0 {
			m.allocations[m.cursor].Minutes = v
		}
	case batchEditDescription:
		if v := m.textInput.Value(); v != "" {
			m.allocations[m.cursor].Description = v
		}
	case batchEditStartTime:
		if v := m.textInput.Value(); v != "" {
			m.allocations[m.cursor].StartTime = v
		}
	case batchEditEndTime:
		if v := m.textInput.Value(); v != "" {
			m.allocations[m.cursor].EndTime = v
		}
	}
}

func (m batchEditModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Edit Batch Allocations"))
	sb.WriteString("\n")

	fieldNames := []string{"Project", "Minutes", "Description", "Start Time", "End Time"}

	for i, a := range m.allocations {
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}

		line := fmt.Sprintf("%s%s %-20s  %3dmin  %s–%s  %s",
			prefix, a.Date, a.ProjectName, a.Minutes, a.StartTime, a.EndTime, a.Description)
		if i == m.cursor {
			line = highlightStyle.Render(line)
		}
		sb.WriteString(line)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("Field: %s\n", selectedStyle.Render(fieldNames[m.field])))

	if m.editing {
		sb.WriteString(m.textInput.View())
		sb.WriteString("\n")

		if m.field == batchEditProject && len(m.filtered) > 0 {
			limit := 5
			if len(m.filtered) < limit {
				limit = len(m.filtered)
			}
			for _, p := range m.filtered[:limit] {
				sb.WriteString(fmt.Sprintf("  %s\n", dimStyle.Render(p.Name)))
			}
		}
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("Enter: edit field • Tab: next field • j/k: nav • Esc: done editing"))

	return boxStyle.Render(sb.String())
}
