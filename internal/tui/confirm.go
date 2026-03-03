package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type ConfirmResult struct {
	Confirmed bool
}

type confirmModel struct {
	message  string
	options  []string
	cursor   int
	result   *ConfirmResult
	quitting bool
}

func NewConfirmApp(message string) *confirmModel {
	return &confirmModel{
		message: message,
		options: []string{"Start anyway", "Cancel"},
	}
}

func (m *confirmModel) GetResult() *ConfirmResult {
	return m.result
}

func (m *confirmModel) Init() tea.Cmd {
	return nil
}

func (m *confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
		case "enter":
			m.result = &ConfirmResult{Confirmed: m.cursor == 0}
			m.quitting = true
			return m, tea.Quit
		case "ctrl+c", "esc":
			m.result = &ConfirmResult{Confirmed: false}
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *confirmModel) View() string {
	if m.quitting {
		return ""
	}

	s := warningStyle.Render("Outside work hours") + "\n"
	s += dimStyle.Render(m.message) + "\n\n"

	for i, opt := range m.options {
		if i == m.cursor {
			s += highlightStyle.Render(fmt.Sprintf("> %s", opt)) + "\n"
		} else {
			s += fmt.Sprintf("  %s\n", opt)
		}
	}

	s += helpStyle.Render("↑/↓ select • enter confirm • esc cancel")

	return s
}
