package ai

type Suggestion struct {
	Allocations   []Allocation `json:"allocations"`
	Clarification string       `json:"clarification,omitempty"`
}

type Allocation struct {
	ProjectID   string  `json:"project_id"`
	ProjectName string  `json:"project_name"`
	Minutes     int     `json:"minutes"`
	Description string  `json:"description"`
	Confidence  float64 `json:"confidence"`
}
