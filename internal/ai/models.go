package ai

import "time"

type Suggestion struct {
	Allocations   []Allocation `json:"allocations"`
	Clarification string       `json:"clarification,omitempty"`
}

type Allocation struct {
	ProjectID   string  `json:"project_id"`
	ProjectName string  `json:"project_name"`
	ClientName  string  `json:"client_name,omitempty"`
	Minutes     int     `json:"minutes"`
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"`
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
	Date        string  `json:"date"`        // "YYYY-MM-DD"
	StartTime   string  `json:"start_time"`  // "HH:MM"
	EndTime     string  `json:"end_time"`    // "HH:MM"
	ProjectID   string  `json:"project_id"`
	ProjectName string  `json:"project_name"`
	ClientName  string  `json:"client_name,omitempty"`
	Minutes     int     `json:"minutes"`
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"`
}

// BatchSuggestion contains allocations across multiple days.
type BatchSuggestion struct {
	Allocations   []BatchAllocation `json:"allocations"`
	Clarification string            `json:"clarification,omitempty"`
}
