package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/christopherklint97/clockr/internal/clockify"
)

const jsonSchema = `{
  "type": "object",
  "properties": {
    "allocations": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "project_id": {"type": "string"},
          "project_name": {"type": "string"},
          "client_name": {"type": "string"},
          "minutes": {"type": "integer"},
          "description": {"type": "string"},
          "confidence": {"type": "number"}
        },
        "required": ["project_id", "project_name", "minutes", "description", "confidence"]
      }
    },
    "clarification": {"type": "string"}
  },
  "required": ["allocations"]
}`

func buildSystemPrompt(projects []clockify.Project, interval time.Duration, contextItems []string) string {
	type projectInfo struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		ClientName string `json:"client_name,omitempty"`
	}

	var pList []projectInfo
	for _, p := range projects {
		pList = append(pList, projectInfo{ID: p.ID, Name: p.Name, ClientName: p.ClientName})
	}

	projectsJSON, _ := json.Marshal(pList)
	totalMinutes := int(interval.Minutes())

	commitsSection := ""
	if len(contextItems) > 0 {
		commitsSection = fmt.Sprintf("\nContext (calendar events, commits, PRs):\n%s\n", formatCommitsList(contextItems))
	}

	return fmt.Sprintf(`You are a time-tracking assistant. Your job is to match work descriptions to Clockify projects and create time entry allocations.

Available projects:
%s
%sRules:
- The time period is %d minutes total
- Each allocation must be at least 30 minutes
- Maximum 2 allocations per hour
- Allocations must sum to exactly %d minutes
- Use exact project IDs and names from the list above
- Write professional, concise descriptions suitable for Clockify time entries
- Use git commits and PRs as additional context clues for what was worked on and which projects to assign
- If the description is unclear, set clarification to ask for more detail and return empty allocations
- Set confidence between 0 and 1 based on how well the description matches a project
- If you cannot match to any project with reasonable confidence, set clarification to explain why

Return valid JSON matching the required schema.`, string(projectsJSON), commitsSection, totalMinutes, totalMinutes)
}

func formatCommitsList(commits []string) string {
	var sb strings.Builder
	for _, c := range commits {
		sb.WriteString("  - ")
		sb.WriteString(c)
		sb.WriteString("\n")
	}
	return sb.String()
}

func buildUserPrompt(description string) string {
	return fmt.Sprintf("What I worked on: %s", description)
}

const batchJSONSchema = `{
  "type": "object",
  "properties": {
    "allocations": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "date": {"type": "string"},
          "start_time": {"type": "string"},
          "end_time": {"type": "string"},
          "project_id": {"type": "string"},
          "project_name": {"type": "string"},
          "client_name": {"type": "string"},
          "minutes": {"type": "integer"},
          "description": {"type": "string"},
          "confidence": {"type": "number"}
        },
        "required": ["date", "start_time", "end_time", "project_id", "project_name", "minutes", "description", "confidence"]
      }
    },
    "clarification": {"type": "string"}
  },
  "required": ["allocations"]
}`

func buildBatchSystemPrompt(projects []clockify.Project, days []DaySlot) string {
	type projectInfo struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		ClientName string `json:"client_name,omitempty"`
	}

	var pList []projectInfo
	for _, p := range projects {
		pList = append(pList, projectInfo{ID: p.ID, Name: p.Name, ClientName: p.ClientName})
	}
	projectsJSON, _ := json.Marshal(pList)

	var schedule string
	for _, d := range days {
		eventsStr := "none"
		if len(d.Events) > 0 {
			eventsStr = fmt.Sprintf("%s", d.Events)
		}
		commitsStr := "none"
		if len(d.Commits) > 0 {
			commitsStr = fmt.Sprintf("%s", d.Commits)
		}
		schedule += fmt.Sprintf("  %s %s: %s–%s (%d min), calendar: %s, commits: %s\n",
			d.Date, d.Weekday,
			d.Start.Format("15:04"), d.End.Format("15:04"),
			d.Minutes, eventsStr, commitsStr)
	}

	return fmt.Sprintf(`You are a time-tracking assistant. Your job is to match work descriptions to Clockify projects and create time entry allocations across multiple days.

Available projects:
%s

Work schedule:
%s
Rules:
- Create allocations for EACH work day listed above
- Each day's allocations must sum to exactly that day's total minutes
- Each allocation must be at least 30 minutes
- Allocations must be contiguous within work hours (no gaps or overlaps within a day)
- Use exact project IDs and names from the list above
- The "date" field must be "YYYY-MM-DD" format
- The "start_time" and "end_time" fields must be "HH:MM" format (24h)
- Write professional, concise descriptions suitable for Clockify time entries
- Use calendar events as context clues for what was worked on
- Use git commits and PRs as additional context clues for what was worked on and which projects to assign
- If the description is unclear, set clarification to ask for more detail and return empty allocations
- Set confidence between 0 and 1 based on how well the description matches a project

Return valid JSON matching the required schema.`, string(projectsJSON), schedule)
}

func buildBatchUserPrompt(description string) string {
	return fmt.Sprintf("What I worked on: %s", description)
}
