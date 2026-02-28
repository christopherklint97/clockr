package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.github.com"

// Repo represents a GitHub repository.
type Repo struct {
	FullName    string    `json:"full_name"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Private     bool      `json:"private"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Commit represents a single git commit.
type Commit struct {
	SHA     string
	Message string
	Date    time.Time
	Repo    string
}

// PullRequest represents a merged pull request.
type PullRequest struct {
	Number   int
	Title    string
	Body     string
	MergedAt time.Time
	Repo     string
}

// CommitContext is the unified context item passed to the AI prompt.
type CommitContext struct {
	Repo    string
	Message string // formatted: "reponame: commit msg"
	Date    time.Time
}

// Client is a GitHub API client with retry logic.
type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
	logger     *slog.Logger
	username   string // cached after first GetUser call
}

// ResolveToken tries to resolve a GitHub token from multiple sources:
// 1. `gh auth token` CLI command
// 2. GITHUB_TOKEN environment variable
// 3. Config file value passed in
func ResolveToken(configToken string) (string, error) {
	// Try gh CLI first
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		token := strings.TrimSpace(string(out))
		if token != "" {
			return token, nil
		}
	}

	// Try environment variable
	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		return v, nil
	}

	// Fall back to config value
	if configToken != "" {
		return configToken, nil
	}

	return "", fmt.Errorf("no GitHub token found — install gh CLI and run 'gh auth login', set GITHUB_TOKEN env var, or add token to [github] config")
}

// NewClient creates a new GitHub API client.
func NewClient(token string, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Client{
		token:   token,
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

func (c *Client) doRequest(ctx context.Context, method, path string) ([]byte, error) {
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")

	var resp *http.Response
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err = c.httpClient.Do(req)
		if err != nil {
			if attempt == maxRetries {
				c.logger.Error("GitHub API transport error", "method", method, "path", path, "error", err)
				return nil, fmt.Errorf("sending request: %w", err)
			}
			time.Sleep(backoff(attempt))
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt == maxRetries {
				c.logger.Error("GitHub API failed after retries", "method", method, "path", path, "status", resp.StatusCode)
				return nil, fmt.Errorf("GitHub API returned status %d after %d retries", resp.StatusCode, maxRetries)
			}
			time.Sleep(backoff(attempt))
			continue
		}
		break
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		c.logger.Error("GitHub API error", "method", method, "path", path, "status", resp.StatusCode, "response", truncate(string(body), 200))
		return nil, fmt.Errorf("GitHub API error (status %d): %s", resp.StatusCode, truncate(string(body), 200))
	}

	return body, nil
}

func backoff(attempt int) time.Duration {
	return time.Duration(math.Pow(2, float64(attempt))) * time.Second
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// GetUser returns the authenticated user's login name (cached).
func (c *Client) GetUser(ctx context.Context) (string, error) {
	if c.username != "" {
		return c.username, nil
	}

	data, err := c.doRequest(ctx, http.MethodGet, "/user")
	if err != nil {
		return "", fmt.Errorf("getting GitHub user: %w", err)
	}

	var user struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(data, &user); err != nil {
		return "", fmt.Errorf("parsing user response: %w", err)
	}

	c.username = user.Login
	return c.username, nil
}

// GetRepos returns all repos accessible to the authenticated user, sorted by recently updated.
func (c *Client) GetRepos(ctx context.Context) ([]Repo, error) {
	var allRepos []Repo
	page := 1

	for {
		path := fmt.Sprintf("/user/repos?sort=updated&per_page=100&page=%d", page)
		data, err := c.doRequest(ctx, http.MethodGet, path)
		if err != nil {
			return nil, fmt.Errorf("fetching repos: %w", err)
		}

		var repos []Repo
		if err := json.Unmarshal(data, &repos); err != nil {
			return nil, fmt.Errorf("parsing repos: %w", err)
		}

		allRepos = append(allRepos, repos...)

		if len(repos) < 100 {
			break
		}
		page++
	}

	return allRepos, nil
}

// GetCommits returns commits by the authenticated user in the given repo and date range.
func (c *Client) GetCommits(ctx context.Context, repoFullName string, since, until time.Time) ([]Commit, error) {
	user, err := c.GetUser(ctx)
	if err != nil {
		return nil, err
	}

	var allCommits []Commit
	page := 1

	for {
		path := fmt.Sprintf("/repos/%s/commits?author=%s&since=%s&until=%s&per_page=100&page=%d",
			repoFullName, user,
			since.UTC().Format(time.RFC3339),
			until.UTC().Format(time.RFC3339),
			page,
		)

		data, err := c.doRequest(ctx, http.MethodGet, path)
		if err != nil {
			return nil, fmt.Errorf("fetching commits for %s: %w", repoFullName, err)
		}

		var apiCommits []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Message string `json:"message"`
				Author  struct {
					Date time.Time `json:"date"`
				} `json:"author"`
			} `json:"commit"`
		}
		if err := json.Unmarshal(data, &apiCommits); err != nil {
			return nil, fmt.Errorf("parsing commits for %s: %w", repoFullName, err)
		}

		repoName := repoFullName
		if parts := strings.SplitN(repoFullName, "/", 2); len(parts) == 2 {
			repoName = parts[1]
		}

		for _, ac := range apiCommits {
			// First line only
			msg := ac.Commit.Message
			if idx := strings.IndexByte(msg, '\n'); idx >= 0 {
				msg = msg[:idx]
			}
			allCommits = append(allCommits, Commit{
				SHA:     ac.SHA[:7],
				Message: msg,
				Date:    ac.Commit.Author.Date,
				Repo:    repoName,
			})
		}

		if len(apiCommits) < 100 {
			break
		}
		page++
	}

	return allCommits, nil
}

// GetMergedPRs returns pull requests merged by the user in the given repo and date range.
func (c *Client) GetMergedPRs(ctx context.Context, repoFullName string, since, until time.Time) ([]PullRequest, error) {
	var allPRs []PullRequest
	page := 1

	user, err := c.GetUser(ctx)
	if err != nil {
		return nil, err
	}

	for {
		path := fmt.Sprintf("/repos/%s/pulls?state=closed&sort=updated&direction=desc&per_page=100&page=%d",
			repoFullName, page,
		)

		data, err := c.doRequest(ctx, http.MethodGet, path)
		if err != nil {
			return nil, fmt.Errorf("fetching PRs for %s: %w", repoFullName, err)
		}

		var apiPRs []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Body   string `json:"body"`
			User   struct {
				Login string `json:"login"`
			} `json:"user"`
			MergedAt *time.Time `json:"merged_at"`
		}
		if err := json.Unmarshal(data, &apiPRs); err != nil {
			return nil, fmt.Errorf("parsing PRs for %s: %w", repoFullName, err)
		}

		repoName := repoFullName
		if parts := strings.SplitN(repoFullName, "/", 2); len(parts) == 2 {
			repoName = parts[1]
		}

		foundInRange := false
		for _, pr := range apiPRs {
			if pr.MergedAt == nil {
				continue
			}
			if pr.User.Login != user {
				continue
			}
			if pr.MergedAt.Before(since) {
				continue
			}
			if pr.MergedAt.After(until) {
				continue
			}

			foundInRange = true
			body := pr.Body
			if len(body) > 200 {
				body = body[:200]
			}
			allPRs = append(allPRs, PullRequest{
				Number:   pr.Number,
				Title:    pr.Title,
				Body:     body,
				MergedAt: *pr.MergedAt,
				Repo:     repoName,
			})
		}

		// Stop paginating if we've gone past the date range
		if len(apiPRs) > 0 && !foundInRange {
			break
		}
		if len(apiPRs) < 100 {
			break
		}
		page++
	}

	return allPRs, nil
}

// Fetch retrieves commits and merged PRs from all repos for the given date range,
// returning unified CommitContext items sorted by date.
func Fetch(ctx context.Context, client *Client, repos []string, start, end time.Time) ([]CommitContext, error) {
	var items []CommitContext

	for _, repo := range repos {
		client.logger.Debug("fetching commits", "repo", repo, "since", start.Format(time.RFC3339), "until", end.Format(time.RFC3339))
		commits, err := client.GetCommits(ctx, repo, start, end)
		if err != nil {
			client.logger.Warn("failed to fetch commits", "repo", repo, "error", err)
			continue
		}
		client.logger.Debug("commits fetched", "repo", repo, "count", len(commits))
		for _, c := range commits {
			items = append(items, CommitContext{
				Repo:    c.Repo,
				Message: fmt.Sprintf("%s: %s", c.Repo, c.Message),
				Date:    c.Date,
			})
		}

		client.logger.Debug("fetching merged PRs", "repo", repo)
		prs, err := client.GetMergedPRs(ctx, repo, start, end)
		if err != nil {
			client.logger.Warn("failed to fetch PRs", "repo", repo, "error", err)
			continue
		}
		client.logger.Debug("PRs fetched", "repo", repo, "count", len(prs))
		for _, pr := range prs {
			items = append(items, CommitContext{
				Repo:    pr.Repo,
				Message: fmt.Sprintf("%s: PR #%d %s", pr.Repo, pr.Number, pr.Title),
				Date:    pr.MergedAt,
			})
		}
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Date.Before(items[j].Date)
	})

	return items, nil
}

// GroupByDay groups CommitContext items by date string (YYYY-MM-DD in local time).
func GroupByDay(items []CommitContext) map[string][]CommitContext {
	grouped := make(map[string][]CommitContext)
	for _, item := range items {
		key := item.Date.Local().Format("2006-01-02")
		grouped[key] = append(grouped[key], item)
	}
	return grouped
}

// FormatPrefill joins commit context messages with "; " for use as TUI textarea prefill.
func FormatPrefill(items []CommitContext) string {
	if len(items) == 0 {
		return ""
	}
	msgs := make([]string, len(items))
	for i, item := range items {
		msgs[i] = item.Message
	}
	return strings.Join(msgs, "; ")
}
