package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/christopherklint97/clockr/internal/clockify"
	"github.com/invopop/jsonschema"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

// Pre-compute JSON schemas for structured output.
var (
	suggestionSchema      map[string]any
	batchSuggestionSchema map[string]any
)

func init() {
	r := &jsonschema.Reflector{
		DoNotReference:             true,
		RequiredFromJSONSchemaTags: true,
	}

	suggestionSchema = schemaToMap(r.Reflect(&Suggestion{}))
	batchSuggestionSchema = schemaToMap(r.Reflect(&BatchSuggestion{}))
}

func schemaToMap(s *jsonschema.Schema) map[string]any {
	data, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal JSON schema: %v", err))
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		panic(fmt.Sprintf("failed to unmarshal JSON schema: %v", err))
	}
	return m
}

// OpenRouterProvider calls the OpenRouter API (OpenAI-compatible) using the official openai-go SDK.
type OpenRouterProvider struct {
	Model      string
	logger     *slog.Logger
	client     openai.Client
	OnThinking func(text string) // optional: called with streaming text chunks
}

func NewOpenRouter(apiKey, model string, logger *slog.Logger) *OpenRouterProvider {
	if model == "" {
		model = "anthropic/claude-sonnet-4-6"
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	var opts []option.RequestOption
	opts = append(opts, option.WithBaseURL("https://openrouter.ai/api/v1"))
	if apiKey != "" {
		opts = append(opts, option.WithAPIKey(apiKey))
	} else if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		opts = append(opts, option.WithAPIKey(key))
	}

	return &OpenRouterProvider{
		Model:  model,
		logger: logger,
		client: openai.NewClient(opts...),
	}
}

func (o *OpenRouterProvider) MatchProjects(ctx context.Context, description string, projects []clockify.Project, interval time.Duration, contextItems []string) (*Suggestion, error) {
	systemPrompt := buildSystemPrompt(projects, interval, contextItems)
	userPrompt := buildUserPrompt(description)

	o.logger.Debug("invoking OpenRouter API",
		"model", o.Model,
		"projects", len(projects),
		"context_items", len(contextItems),
		"system_prompt_len", len(systemPrompt),
		"user_prompt_len", len(userPrompt),
	)

	result, err := o.call(ctx, systemPrompt, userPrompt, suggestionSchema, "suggestion")
	if err != nil {
		return nil, err
	}

	o.logger.Debug("MatchProjects result to parse",
		"result_len", len(result),
		"result", truncateStr(result, 2000),
	)

	var suggestion Suggestion
	if err := json.Unmarshal([]byte(result), &suggestion); err != nil {
		o.logger.Error("failed to parse suggestion", "error", err, "raw", truncateStr(result, 2000))
		return nil, fmt.Errorf("parsing suggestion: %w (raw: %s)", err, truncateStr(result, 1000))
	}

	o.logger.Debug("parsed suggestion",
		"allocations", len(suggestion.Allocations),
		"clarification", suggestion.Clarification,
	)
	return &suggestion, nil
}

func (o *OpenRouterProvider) MatchProjectsBatch(ctx context.Context, description string, projects []clockify.Project, days []DaySlot) (*BatchSuggestion, error) {
	systemPrompt := buildBatchSystemPrompt(projects, days)
	userPrompt := buildBatchUserPrompt(description)

	o.logger.Debug("invoking OpenRouter API (batch)",
		"model", o.Model,
		"days", len(days),
		"projects", len(projects),
		"system_prompt_len", len(systemPrompt),
		"user_prompt_len", len(userPrompt),
	)

	result, err := o.call(ctx, systemPrompt, userPrompt, batchSuggestionSchema, "batch_suggestion")
	if err != nil {
		return nil, err
	}

	o.logger.Debug("MatchProjectsBatch result to parse",
		"result_len", len(result),
		"result", truncateStr(result, 2000),
	)

	var suggestion BatchSuggestion
	if err := json.Unmarshal([]byte(result), &suggestion); err != nil {
		o.logger.Error("failed to parse batch suggestion", "error", err, "raw", truncateStr(result, 2000))
		return nil, fmt.Errorf("parsing batch suggestion: %w (raw: %s)", err, truncateStr(result, 1000))
	}

	o.logger.Debug("parsed batch suggestion",
		"allocations", len(suggestion.Allocations),
		"clarification", suggestion.Clarification,
	)
	return &suggestion, nil
}

// call sends a chat completion request to OpenRouter and returns the text response.
// Uses streaming when OnThinking is set, buffered otherwise.
func (o *OpenRouterProvider) call(ctx context.Context, systemPrompt, userPrompt string, schema map[string]any, schemaName string) (string, error) {
	params := openai.ChatCompletionNewParams{
		Model: o.Model,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
		MaxTokens: openai.Int(4096),
		ResponseFormat: openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
				JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
					Name:   schemaName,
					Strict: openai.Bool(true),
					Schema: schema,
				},
			},
		},
	}

	startTime := time.Now()

	if o.OnThinking != nil {
		return o.callStreaming(ctx, params, startTime)
	}
	return o.callBuffered(ctx, params, startTime)
}

func (o *OpenRouterProvider) callBuffered(ctx context.Context, params openai.ChatCompletionNewParams, startTime time.Time) (string, error) {
	resp, err := o.client.Chat.Completions.New(ctx, params)
	elapsed := time.Since(startTime)

	if err != nil {
		o.logger.Error("OpenRouter API failed", "error", err, "elapsed", elapsed)
		if ctx.Err() != nil {
			return "", fmt.Errorf("OpenRouter API timed out after %s", elapsed.Truncate(time.Second))
		}
		return "", fmt.Errorf("calling OpenRouter API: %w", err)
	}

	o.logger.Debug("OpenRouter API finished",
		"elapsed", elapsed,
		"choices", len(resp.Choices),
	)

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in OpenRouter API response")
	}

	return resp.Choices[0].Message.Content, nil
}

func (o *OpenRouterProvider) callStreaming(ctx context.Context, params openai.ChatCompletionNewParams, startTime time.Time) (string, error) {
	stream := o.client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	var resultText string

	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) > 0 {
			delta := chunk.Choices[0].Delta.Content
			if delta != "" {
				o.OnThinking(delta)
				resultText += delta
			}
		}
	}

	elapsed := time.Since(startTime)

	if err := stream.Err(); err != nil {
		o.logger.Error("OpenRouter API streaming failed", "error", err, "elapsed", elapsed)
		if ctx.Err() != nil {
			return "", fmt.Errorf("OpenRouter API timed out after %s", elapsed.Truncate(time.Second))
		}
		return "", fmt.Errorf("streaming OpenRouter API: %w", err)
	}

	o.logger.Debug("OpenRouter API streaming finished",
		"elapsed", elapsed,
		"result_len", len(resultText),
	)

	if resultText == "" {
		return "", fmt.Errorf("no text content received from OpenRouter API")
	}
	return resultText, nil
}

// VerifyOpenRouterAPIKey checks that the OpenRouter API key is available.
func VerifyOpenRouterAPIKey(apiKey string) error {
	if apiKey != "" {
		return nil
	}
	if os.Getenv("OPENROUTER_API_KEY") != "" {
		return nil
	}
	return fmt.Errorf("OpenRouter API key not configured — set OPENROUTER_API_KEY env var or api_key under [ai] in config")
}

// VerifyAPIKey checks that the ANTHROPIC_API_KEY is available (either passed or in env).
func VerifyAPIKey(apiKey string) error {
	if apiKey != "" {
		return nil
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		return nil
	}
	return fmt.Errorf("Anthropic API key not configured — set ANTHROPIC_API_KEY env var or api_key under [ai] in config")
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractJSON finds and returns the first top-level JSON object in s.
// The model may output reasoning text before/after the JSON.
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		return s
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		ch := s[i]
		if escaped {
			escaped = false
			continue
		}
		if ch == '\\' && inString {
			escaped = true
			continue
		}
		if ch == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch ch {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}
