package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/christopherklint97/clockr/internal/ai"
	"github.com/christopherklint97/clockr/internal/clockify"
)

type editField int

const (
	editProject editField = iota
	editMinutes
	editDescription
)

type editModel struct {
	allocations []ai.Allocation
	projects    []clockify.Project
	cursor      int
	field       editField
	textInput   textinput.Model
	editing     bool
	filtered    []clockify.Project
}

func newEditModel(allocations []ai.Allocation, projects []clockify.Project) editModel {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Width = 50

	return editModel{
		allocations: allocations,
		projects:    projects,
		textInput:   ti,
	}
}

func (m editModel) Update(msg tea.Msg) (editModel, tea.Cmd) {
	if m.editing {
		return m.updateEditing(msg)
	}
	return m.updateNavigating(msg)
}

func (m editModel) updateNavigating(msg tea.Msg) (editModel, tea.Cmd) {
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
			m.field = (m.field + 1) % 3
		case "enter":
			m.editing = true
			m.textInput.Focus()
			switch m.field {
			case editProject:
				m.textInput.SetValue("")
				m.textInput.Placeholder = "Search project..."
				m.filtered = m.projects
			case editMinutes:
				m.textInput.SetValue(strconv.Itoa(m.allocations[m.cursor].Minutes))
				m.textInput.Placeholder = "Minutes"
			case editDescription:
				m.textInput.SetValue(m.allocations[m.cursor].Description)
				m.textInput.Placeholder = "Description"
			}
			return m, m.textInput.Focus()
		}
	}
	return m, nil
}

func (m editModel) updateEditing(msg tea.Msg) (editModel, tea.Cmd) {
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

	if m.field == editProject {
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

func (m *editModel) applyEdit() {
	switch m.field {
	case editProject:
		if len(m.filtered) > 0 {
			m.allocations[m.cursor].ProjectID = m.filtered[0].ID
			m.allocations[m.cursor].ProjectName = m.filtered[0].Name
		}
	case editMinutes:
		if v, err := strconv.Atoi(m.textInput.Value()); err == nil && v > 0 {
			m.allocations[m.cursor].Minutes = v
		}
	case editDescription:
		if v := m.textInput.Value(); v != "" {
			m.allocations[m.cursor].Description = v
		}
	}
}

func (m editModel) View() string {
	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Edit Allocations"))
	sb.WriteString("\n")

	fieldNames := []string{"Project", "Minutes", "Description"}

	for i, a := range m.allocations {
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}

		line := fmt.Sprintf("%s%-20s  %3dmin  %s", prefix, a.ProjectName, a.Minutes, a.Description)
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

		if m.field == editProject && len(m.filtered) > 0 {
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
