package tui

import (
	"fmt"
	"strconv"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type durationModel struct {
	textinput  textinput.Model
	defaultMin int
}

func newDurationModel(defaultMin int) durationModel {
	ti := textinput.New()
	ti.Placeholder = fmt.Sprintf("%d", defaultMin)
	ti.SetValue(fmt.Sprintf("%d", defaultMin))
	ti.Focus()
	ti.CharLimit = 4
	ti.Width = 10

	return durationModel{
		textinput:  ti,
		defaultMin: defaultMin,
	}
}

func (m durationModel) Update(msg tea.Msg) (durationModel, tea.Cmd) {
	var cmd tea.Cmd
	m.textinput, cmd = m.textinput.Update(msg)
	return m, cmd
}

func (m durationModel) View() string {
	header := titleStyle.Render("clockr — Time Entry")
	prompt := subtitleStyle.Render("How many minutes to log?")
	help := helpStyle.Render("Enter: confirm • Ctrl+C: cancel")

	return header + "\n" + prompt + "\n" + m.textinput.View() + "\n" + help
}

func (m durationModel) Value() int {
	v, err := strconv.Atoi(m.textinput.Value())
	if err != nil || v < 1 {
		return m.defaultMin
	}
	return v
}
