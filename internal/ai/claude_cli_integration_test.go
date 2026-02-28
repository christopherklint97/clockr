//go:build integration

package ai_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/christopherklint97/clockr/internal/ai"
	"github.com/christopherklint97/clockr/internal/clockify"
	"io"
	"log/slog"
	"os"
)

func skipIfNoClaude(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not found in PATH, skipping integration test")
	}
}

// testLogger creates a verbose slog.Logger that writes to testing.T
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

var testProjects = []clockify.Project{
	{ID: "proj-001", Name: "Backend API"},
	{ID: "proj-002", Name: "Frontend Dashboard"},
	{ID: "proj-003", Name: "DevOps / Infrastructure"},
	{ID: "proj-004", Name: "Meetings & Admin"},
}

func TestClaudeCLI_MatchProjects_Simple(t *testing.T) {
	skipIfNoClaude(t)

	logger := testLogger(t)
	cli := ai.NewClaudeCLI("haiku", logger)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("Starting MatchProjects with description: 'Fixed a bug in the login endpoint'")
	t.Logf("Projects: %+v", testProjects)
	t.Logf("Duration: 60 minutes")

	suggestion, err := cli.MatchProjects(ctx, "Fixed a bug in the login endpoint", testProjects, 60*time.Minute, nil)
	if err != nil {
		t.Fatalf("MatchProjects failed: %v", err)
	}

	t.Logf("Raw suggestion: %+v", suggestion)
	t.Logf("Allocations count: %d", len(suggestion.Allocations))
	t.Logf("Clarification: %q", suggestion.Clarification)

	if len(suggestion.Allocations) == 0 {
		if suggestion.Clarification != "" {
			t.Logf("Got clarification instead: %s", suggestion.Clarification)
		} else {
			t.Fatal("Expected at least one allocation, got none")
		}
		return
	}

	totalMinutes := 0
	for i, a := range suggestion.Allocations {
		t.Logf("Allocation[%d]: project=%s (%s), minutes=%d, desc=%q, confidence=%.2f",
			i, a.ProjectName, a.ProjectID, a.Minutes, a.Description, a.Confidence)

		if a.ProjectID == "" {
			t.Error("Allocation has empty project_id")
		}
		if a.ProjectName == "" {
			t.Error("Allocation has empty project_name")
		}
		if a.Minutes < 30 {
			t.Errorf("Allocation minutes %d < 30 minimum", a.Minutes)
		}
		if a.Description == "" {
			t.Error("Allocation has empty description")
		}
		if a.Confidence < 0 || a.Confidence > 1 {
			t.Errorf("Confidence %.2f out of [0,1] range", a.Confidence)
		}

		validProject := false
		for _, p := range testProjects {
			if a.ProjectID == p.ID {
				validProject = true
				break
			}
		}
		if !validProject {
			t.Errorf("Allocation project_id %q not in test projects", a.ProjectID)
		}

		totalMinutes += a.Minutes
	}

	if totalMinutes != 60 {
		t.Errorf("Total minutes %d, expected 60", totalMinutes)
	}
}

func TestClaudeCLI_MatchProjects_WithContext(t *testing.T) {
	skipIfNoClaude(t)

	logger := testLogger(t)
	cli := ai.NewClaudeCLI("haiku", logger)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	contextItems := []string{
		"commit: fix CORS headers in api/middleware.go",
		"PR #42: Add rate limiting to public endpoints",
	}

	t.Log("Starting MatchProjects with context items")
	t.Logf("Description: 'Worked on API security improvements'")
	t.Logf("Context items: %v", contextItems)
	t.Logf("Duration: 120 minutes")

	suggestion, err := cli.MatchProjects(ctx, "Worked on API security improvements", testProjects, 120*time.Minute, contextItems)
	if err != nil {
		t.Fatalf("MatchProjects failed: %v", err)
	}

	t.Logf("Raw suggestion: %+v", suggestion)
	t.Logf("Allocations count: %d", len(suggestion.Allocations))
	t.Logf("Clarification: %q", suggestion.Clarification)

	if len(suggestion.Allocations) == 0 && suggestion.Clarification == "" {
		t.Fatal("Expected allocations or clarification, got neither")
	}

	if len(suggestion.Allocations) > 0 {
		totalMinutes := 0
		for i, a := range suggestion.Allocations {
			t.Logf("Allocation[%d]: project=%s (%s), minutes=%d, desc=%q, confidence=%.2f",
				i, a.ProjectName, a.ProjectID, a.Minutes, a.Description, a.Confidence)
			totalMinutes += a.Minutes
		}
		if totalMinutes != 120 {
			t.Errorf("Total minutes %d, expected 120", totalMinutes)
		}
	}
}

func TestClaudeCLI_MatchProjects_AmbiguousDescription(t *testing.T) {
	skipIfNoClaude(t)

	logger := testLogger(t)
	cli := ai.NewClaudeCLI("haiku", logger)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("Starting MatchProjects with ambiguous description: 'stuff'")

	suggestion, err := cli.MatchProjects(ctx, "stuff", testProjects, 60*time.Minute, nil)
	if err != nil {
		t.Fatalf("MatchProjects failed: %v", err)
	}

	t.Logf("Raw suggestion: %+v", suggestion)
	t.Logf("Allocations count: %d", len(suggestion.Allocations))
	t.Logf("Clarification: %q", suggestion.Clarification)

	// Either a clarification or low-confidence allocations are acceptable
	if suggestion.Clarification != "" {
		t.Logf("Got clarification for ambiguous input: %s", suggestion.Clarification)
	} else if len(suggestion.Allocations) > 0 {
		for i, a := range suggestion.Allocations {
			t.Logf("Allocation[%d] (ambiguous): project=%s, minutes=%d, confidence=%.2f",
				i, a.ProjectName, a.Minutes, a.Confidence)
		}
	}
}

func TestClaudeCLI_MatchProjectsBatch(t *testing.T) {
	skipIfNoClaude(t)

	logger := testLogger(t)
	cli := ai.NewClaudeCLI("haiku", logger)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	now := time.Now()
	days := []ai.DaySlot{
		{
			Date:    now.Format("2006-01-02"),
			Weekday: now.Weekday().String(),
			Start:   time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, now.Location()),
			End:     time.Date(now.Year(), now.Month(), now.Day(), 17, 0, 0, 0, now.Location()),
			Minutes: 480,
			Events:  []string{"Team standup 09:00-09:30", "Sprint planning 14:00-15:00"},
			Commits: []string{"commit: refactor user service"},
		},
	}

	t.Log("Starting MatchProjectsBatch")
	t.Logf("Description: 'Backend development and meetings'")
	t.Logf("Days: %+v", days)

	suggestion, err := cli.MatchProjectsBatch(ctx, "Backend development and meetings", testProjects, days)
	if err != nil {
		t.Fatalf("MatchProjectsBatch failed: %v", err)
	}

	t.Logf("Raw suggestion: %+v", suggestion)
	t.Logf("Allocations count: %d", len(suggestion.Allocations))
	t.Logf("Clarification: %q", suggestion.Clarification)

	if len(suggestion.Allocations) == 0 {
		if suggestion.Clarification != "" {
			t.Logf("Got clarification: %s", suggestion.Clarification)
		} else {
			t.Fatal("Expected batch allocations, got none")
		}
		return
	}

	totalMinutes := 0
	for i, a := range suggestion.Allocations {
		t.Logf("Batch allocation[%d]: date=%s, %s-%s, project=%s (%s), minutes=%d, desc=%q, confidence=%.2f",
			i, a.Date, a.StartTime, a.EndTime, a.ProjectName, a.ProjectID, a.Minutes, a.Description, a.Confidence)

		if a.Date == "" {
			t.Error("Allocation has empty date")
		}
		if a.StartTime == "" {
			t.Error("Allocation has empty start_time")
		}
		if a.EndTime == "" {
			t.Error("Allocation has empty end_time")
		}
		if a.ProjectID == "" {
			t.Error("Allocation has empty project_id")
		}
		if a.Minutes < 30 {
			t.Errorf("Allocation minutes %d < 30 minimum", a.Minutes)
		}

		validProject := false
		for _, p := range testProjects {
			if a.ProjectID == p.ID {
				validProject = true
				break
			}
		}
		if !validProject {
			t.Errorf("Batch allocation project_id %q not in test projects", a.ProjectID)
		}

		totalMinutes += a.Minutes
	}

	if totalMinutes != 480 {
		t.Errorf("Total batch minutes %d, expected 480", totalMinutes)
	}
}

func TestClaudeCLI_MatchProjects_Streaming(t *testing.T) {
	skipIfNoClaude(t)

	logger := testLogger(t)
	cli := ai.NewClaudeCLI("haiku", logger)

	var chunks []string
	cli.OnThinking = func(text string) {
		chunks = append(chunks, text)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Log("Starting MatchProjects (streaming) with description: 'Updated the dashboard charts'")

	suggestion, err := cli.MatchProjects(ctx, "Updated the dashboard charts", testProjects, 60*time.Minute, nil)
	if err != nil {
		t.Fatalf("MatchProjects (streaming) failed: %v", err)
	}

	t.Logf("Received %d streaming chunks", len(chunks))
	if len(chunks) > 0 {
		t.Logf("First chunk: %q", truncateForLog(chunks[0], 200))
		t.Logf("Last chunk: %q", truncateForLog(chunks[len(chunks)-1], 200))
	}

	t.Logf("Raw suggestion: %+v", suggestion)
	t.Logf("Allocations count: %d", len(suggestion.Allocations))
	t.Logf("Clarification: %q", suggestion.Clarification)

	if len(suggestion.Allocations) == 0 && suggestion.Clarification == "" {
		t.Fatal("Expected allocations or clarification from streaming mode")
	}

	for i, a := range suggestion.Allocations {
		t.Logf("Streaming allocation[%d]: project=%s, minutes=%d, desc=%q",
			i, a.ProjectName, a.Minutes, a.Description)
	}
}

// suppress unused import warning
var _ = io.Discard

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
