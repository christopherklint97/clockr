package ai

import "time"

type Suggestion struct {
	Allocations   []Allocation `json:"allocations" jsonschema:"required"`
	Clarification string       `json:"clarification,omitempty"`
}

type Allocation struct {
	ProjectID   string  `json:"project_id" jsonschema:"required"`
	ProjectName string  `json:"project_name" jsonschema:"required"`
	ClientName  string  `json:"client_name,omitempty"`
	Minutes     int     `json:"minutes" jsonschema:"required"`
	Description string  `json:"description" jsonschema:"required"`
	Confidence  float64 `json:"confidence" jsonschema:"required"`
}

// DaySlot represents one work day in a batch time entry request.
type DaySlot struct {
	Date    string    // "YYYY-MM-DD"
	Weekday string    // "Monday", "Tuesday", etc.
	Start   time.Time // work start for this day
	End     time.Time // work end for this day
	Minutes int       // total work minutes this day
	Events  []string  // calendar event summaries
	Commits []string  // git commit/PR context messages
}

// BatchAllocation is like Allocation but tagged with date and time range.
type BatchAllocation struct {
	Date        string  `json:"date" jsonschema:"required"`        // "YYYY-MM-DD"
	StartTime   string  `json:"start_time" jsonschema:"required"`  // "HH:MM"
	EndTime     string  `json:"end_time" jsonschema:"required"`    // "HH:MM"
	ProjectID   string  `json:"project_id" jsonschema:"required"`
	ProjectName string  `json:"project_name" jsonschema:"required"`
	ClientName  string  `json:"client_name,omitempty"`
	Minutes     int     `json:"minutes" jsonschema:"required"`
	Description string  `json:"description" jsonschema:"required"`
	Confidence  float64 `json:"confidence" jsonschema:"required"`
}

// BatchSuggestion contains allocations across multiple days.
type BatchSuggestion struct {
	Allocations   []BatchAllocation `json:"allocations" jsonschema:"required"`
	Clarification string            `json:"clarification,omitempty"`
}
