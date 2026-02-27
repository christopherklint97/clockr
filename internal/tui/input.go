package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

type inputModel struct {
	textarea textarea.Model
	timeInfo string
	width    int
	height   int
}

func newInputModel(timeInfo string) inputModel {
	ta := textarea.New()
	ta.Placeholder = "Describe what you worked on..."
	ta.Focus()
	ta.CharLimit = 5000
	ta.SetWidth(76)
	ta.SetHeight(15)
	ta.ShowLineNumbers = false

	return inputModel{
		textarea: ta,
		timeInfo: timeInfo,
		width:    80,
		height:   20,
	}
}

func (m inputModel) Update(msg tea.Msg) (inputModel, tea.Cmd) {
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = wsMsg.Width
		m.height = wsMsg.Height
		if m.width > 4 {
			m.textarea.SetWidth(m.width - 4)
		}
		if m.height > 5 {
			m.textarea.SetHeight(m.height - 5)
		}
		return m, nil
	}
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
