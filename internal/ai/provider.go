package ai

import (
	"context"
	"time"

	"github.com/christopherklint97/clockr/internal/clockify"
)

type Provider interface {
	MatchProjects(ctx context.Context, description string, projects []clockify.Project, interval time.Duration) (*Suggestion, error)
}
