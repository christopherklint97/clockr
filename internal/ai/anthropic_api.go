package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/christopherklint97/clockr/internal/clockify"
)

// AnthropicAPI is a stub for direct API usage as a fallback provider.
type AnthropicAPI struct {
	APIKey string
	Model  string
}

func NewAnthropicAPI(apiKey, model string) *AnthropicAPI {
	return &AnthropicAPI{APIKey: apiKey, Model: model}
}

func (a *AnthropicAPI) MatchProjects(ctx context.Context, description string, projects []clockify.Project, interval time.Duration) (*Suggestion, error) {
	return nil, fmt.Errorf("anthropic API provider not yet implemented — use claude-cli provider instead")
}
