package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/christopherklint97/clockr/internal/github"
)

const repoPickerVisible = 15

type repoPickerModel struct {
	repos    []github.Repo
	filtered []int // indices into repos
	selected map[int]bool
	cursor   int
	filter   textinput.Model
	done     bool
	canceled bool
}

// RepoPickerResult holds the repos the user selected.
type RepoPickerResult struct {
	Repos    []string // full names of selected repos
	Canceled bool
}

// RepoPickerApp wraps repoPickerModel for standalone use with tea.NewProgram.
type RepoPickerApp struct {
	picker repoPickerModel
	result *RepoPickerResult
}

func NewRepoPickerApp(repos []github.Repo) *RepoPickerApp {
	return &RepoPickerApp{
		picker: newRepoPicker(repos),
	}
}

func (a *RepoPickerApp) Init() tea.Cmd {
	return a.picker.Init()
}

func (a *RepoPickerApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	m, cmd := a.picker.Update(msg)
	a.picker = m.(repoPickerModel)

	if a.picker.done || a.picker.canceled {
		a.result = a.picker.Result()
		return a, tea.Quit
	}

	return a, cmd
}

func (a *RepoPickerApp) View() string {
	return a.picker.View()
}

func (a *RepoPickerApp) GetResult() *RepoPickerResult {
	return a.result
}

func newRepoPicker(repos []github.Repo) repoPickerModel {
	ti := textinput.New()
	ti.Placeholder = "Filter repos..."
	ti.Focus()

	filtered := make([]int, len(repos))
	for i := range repos {
		filtered[i] = i
	}

	return repoPickerModel{
		repos:    repos,
		filtered: filtered,
		selected: make(map[int]bool),
		filter:   ti,
	}
}

func (m repoPickerModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m repoPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.canceled = true
			return m, nil
		case "enter":
			if len(m.selected) > 0 {
				m.done = true
			}
			return m, nil
		case " ":
			if len(m.filtered) > 0 {
				idx := m.filtered[m.cursor]
				if m.selected[idx] {
					delete(m.selected, idx)
				} else {
					m.selected[idx] = true
				}
			}
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	prevFilter := m.filter.Value()
	m.filter, cmd = m.filter.Update(msg)

	// Re-filter on text change
	if m.filter.Value() != prevFilter {
		m.applyFilter()
	}

	return m, cmd
}

func (m *repoPickerModel) applyFilter() {
	query := strings.ToLower(m.filter.Value())
	m.filtered = m.filtered[:0]
	for i, r := range m.repos {
		if query == "" ||
			strings.Contains(strings.ToLower(r.FullName), query) ||
			strings.Contains(strings.ToLower(r.Description), query) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
}

func (m repoPickerModel) View() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Select GitHub Repositories"))
	b.WriteString("\n")
	b.WriteString(m.filter.View())
	b.WriteString("\n\n")

	if len(m.filtered) == 0 {
		b.WriteString(dimStyle.Render("  No repos match filter"))
		b.WriteString("\n")
	} else {
		// Calculate scroll window
		start := 0
		if m.cursor >= repoPickerVisible {
			start = m.cursor - repoPickerVisible + 1
		}
		end := min(start+repoPickerVisible, len(m.filtered))

		for vi := start; vi < end; vi++ {
			idx := m.filtered[vi]
			repo := m.repos[idx]

			cursor := "  "
			if vi == m.cursor {
				cursor = "> "
			}

			check := "[ ]"
			if m.selected[idx] {
				check = "[x]"
			}

			desc := ""
			if repo.Description != "" {
				d := repo.Description
				if len(d) > 50 {
					d = d[:50] + "..."
				}
				desc = dimStyle.Render(" — " + d)
			}

			line := fmt.Sprintf("%s%s %s%s", cursor, check, repo.FullName, desc)
			if vi == m.cursor {
				line = highlightStyle.Render(fmt.Sprintf("%s%s ", cursor, check)) + repo.FullName + desc
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	count := len(m.selected)
	b.WriteString(helpStyle.Render(fmt.Sprintf(
		"\n%d selected — Space: toggle — Enter: confirm — Ctrl+C: cancel", count)))

	return b.String()
}

func (m repoPickerModel) Result() *RepoPickerResult {
	if m.canceled {
		return &RepoPickerResult{Canceled: true}
	}
	var repos []string
	for idx := range m.selected {
		repos = append(repos, m.repos[idx].FullName)
	}
	return &RepoPickerResult{Repos: repos}
}
