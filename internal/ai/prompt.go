package ai

import (
	"encoding/json"
	"fmt"
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

func buildSystemPrompt(projects []clockify.Project, interval time.Duration) string {
	type projectInfo struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}

	var pList []projectInfo
	for _, p := range projects {
		pList = append(pList, projectInfo{ID: p.ID, Name: p.Name})
	}

	projectsJSON, _ := json.Marshal(pList)
	totalMinutes := int(interval.Minutes())

	return fmt.Sprintf(`You are a time-tracking assistant. Your job is to match work descriptions to Clockify projects and create time entry allocations.

Available projects:
%s

Rules:
- The time period is %d minutes total
- Each allocation must be at least 30 minutes
- Maximum 2 allocations per hour
- Allocations must sum to exactly %d minutes
- Use exact project IDs and names from the list above
- Write professional, concise descriptions suitable for Clockify time entries
- If the description is unclear, set clarification to ask for more detail and return empty allocations
- Set confidence between 0 and 1 based on how well the description matches a project
- If you cannot match to any project with reasonable confidence, set clarification to explain why

Return valid JSON matching the required schema.`, string(projectsJSON), totalMinutes, totalMinutes)
}

func buildUserPrompt(description string) string {
	return fmt.Sprintf("What I worked on: %s", description)
}
