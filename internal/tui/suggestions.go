package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/christopherklint97/clockr/internal/ai"
)

// truncate shortens s to maxWidth display characters, appending "..." if truncated.
func truncate(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	if maxWidth <= 3 {
		runes := []rune(s)
		if len(runes) > maxWidth {
			return string(runes[:maxWidth])
		}
		return s
	}
	runes := []rune(s)
	target := maxWidth - 3
	end := target
	if end > len(runes) {
		end = len(runes)
	}
	for end > 0 && lipgloss.Width(string(runes[:end])) > target {
		end--
	}
	return string(runes[:end]) + "..."
}

type suggestionsModel struct {
	suggestion *ai.Suggestion
	cursor     int
	termWidth  int
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

	// Build display data and compute column widths
	type row struct {
		project    string
		minutes    string
		confidence string
		desc       string
	}
	rows := make([]row, len(m.suggestion.Allocations))
	maxProject := 0
	maxMinutes := 0
	maxDesc := 0
	for i, a := range m.suggestion.Allocations {
		project := a.ProjectName
		if a.ClientName != "" {
			project = a.ProjectName + " (" + a.ClientName + ")"
		}
		minutes := fmt.Sprintf("%dmin", a.Minutes)
		confidence := fmt.Sprintf("%.0f%%", a.Confidence*100)
		rows[i] = row{project: project, minutes: minutes, confidence: confidence, desc: a.Description}
		maxProject = max(maxProject, len(project))
		maxMinutes = max(maxMinutes, len(minutes))
		maxDesc = max(maxDesc, len(a.Description))
	}

	// Truncate columns to fit terminal width
	// Layout: prefix(2) + project + gap(2) + minutes + gap(2) + confidence(4) + gap(2) + desc
	// Box overhead: border(2) + padding(2) = 4
	if m.termWidth > 0 {
		available := m.termWidth - 4
		fixed := 12 + maxMinutes // prefix(2) + 3 gaps(6) + confidence(4) + minutes
		remaining := available - fixed
		if remaining < maxProject+maxDesc {
			projectCap := min(maxProject, 35)
			descCap := remaining - projectCap
			if descCap < 10 {
				descCap = max(remaining/3, 5)
				projectCap = remaining - descCap
			}
			maxProject = max(projectCap, 5)
			maxDesc = max(remaining-maxProject, 0)
			for i, r := range rows {
				rows[i].project = truncate(r.project, maxProject)
				rows[i].desc = truncate(r.desc, maxDesc)
			}
		}
	}

	for i, r := range rows {
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}

		line := fmt.Sprintf("%s%-*s  %*s  %s  %s",
			prefix,
			maxProject, r.project,
			maxMinutes, r.minutes,
			dimStyle.Render(fmt.Sprintf("%4s", r.confidence)),
			r.desc,
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
