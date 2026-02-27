package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

type inputModel struct {
	textarea textarea.Model
	timeInfo string
}

func newInputModel(timeInfo string, prefill string) inputModel {
	ta := textarea.New()
	ta.Placeholder = "Describe what you worked on..."
	ta.Focus()
	ta.CharLimit = 500
	ta.SetWidth(60)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false

	if prefill != "" {
		ta.SetValue(prefill)
	}

	return inputModel{
		textarea: ta,
		timeInfo: timeInfo,
	}
}

func (m inputModel) Update(msg tea.Msg) (inputModel, tea.Cmd) {
	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m inputModel) View() string {
	header := titleStyle.Render("clockr — Time Entry")
	timeLabel := subtitleStyle.Render(m.timeInfo)
	help := helpStyle.Render("Enter: submit • Ctrl+C: cancel")

	return header + "\n" + timeLabel + "\n" + m.textarea.View() + "\n" + help
}

func (m inputModel) Value() string {
	return m.textarea.Value()
}
