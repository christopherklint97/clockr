package clockify

import "time"

type User struct {
	ID               string `json:"id"`
	Email            string `json:"email"`
	Name             string `json:"name"`
	ActiveWorkspace  string `json:"activeWorkspace"`
	DefaultWorkspace string `json:"defaultWorkspace"`
}

type Project struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Archived   bool   `json:"archived"`
	Color      string `json:"color"`
	ClientID   string `json:"clientId"`
	ClientName string `json:"-"` // populated after fetching clients
}

type ClockifyClient struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TimeEntryRequest struct {
	Start       string `json:"start"`
	End         string `json:"end"`
	ProjectID   string `json:"projectId"`
	Description string `json:"description"`
}

type TimeEntry struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	ProjectID   string `json:"projectId"`
	TimeInterval struct {
		Start time.Time `json:"start"`
		End   time.Time `json:"end"`
	} `json:"timeInterval"`
}
