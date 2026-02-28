package tui

import (
	"fmt"
	"strings"

	"github.com/christopherklint97/clockr/internal/ai"
)

type suggestionsModel struct {
	suggestion *ai.Suggestion
	cursor     int
}

func newSuggestionsModel(s *ai.Suggestion) suggestionsModel {
	return suggestionsModel{suggestion: s}
}

func (m suggestionsModel) View() string {
	if m.suggestion.Clarification != "" {
		return warningStyle.Render("Clarification needed: ") + m.suggestion.Clarification + "\n\n" +
			helpStyle.Render("[r]etry with more detail • [s]kip")
	}

	var sb strings.Builder

	sb.WriteString(titleStyle.Render("Suggested Allocations"))
	sb.WriteString("\n")

	for i, a := range m.suggestion.Allocations {
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}

		projectDisplay := a.ProjectName
		if a.ClientName != "" {
			projectDisplay = a.ClientName + " / " + a.ProjectName
		}

		confidence := fmt.Sprintf("%.0f%%", a.Confidence*100)
		line := fmt.Sprintf("%s%-30s  %3dmin  %s  %s",
			prefix,
			projectDisplay,
			a.Minutes,
			dimStyle.Render(confidence),
			a.Description,
		)

		if i == m.cursor {
			line = highlightStyle.Render(line)
		}

		sb.WriteString(line)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("[a]ccept • [e]dit • [r]etry • [s]kip"))

	return boxStyle.Render(sb.String())
}
