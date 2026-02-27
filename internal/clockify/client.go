package clockify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const baseURL = "https://api.clockify.me/api/v1"

type Client struct {
	apiKey     string
	httpClient *http.Client
	cache      *ProjectCache
}

func NewClient(apiKey string, cacheTTL time.Duration) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		cache: NewProjectCache(cacheTTL),
	}
}

func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	url := baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("X-Api-Key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	var resp *http.Response
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err = c.httpClient.Do(req)
		if err != nil {
			if attempt == maxRetries {
				return nil, fmt.Errorf("sending request: %w", err)
			}
			time.Sleep(backoff(attempt))
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt == maxRetries {
				return nil, fmt.Errorf("API returned status %d after %d retries", resp.StatusCode, maxRetries)
			}
			time.Sleep(backoff(attempt))
			continue
		}
		break
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func backoff(attempt int) time.Duration {
	return time.Duration(math.Pow(2, float64(attempt))) * time.Second
}

func (c *Client) GetUser(ctx context.Context) (*User, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "/user", nil)
	if err != nil {
		return nil, fmt.Errorf("getting user: %w", err)
	}

	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return nil, fmt.Errorf("parsing user response: %w", err)
	}

	return &user, nil
}

func (c *Client) GetProjects(ctx context.Context, workspaceID string) ([]Project, error) {
	if cached := c.cache.Get(); cached != nil {
		return cached, nil
	}

	var allProjects []Project
	page := 1
	pageSize := 500

	for {
		path := fmt.Sprintf("/workspaces/%s/projects?page-size=%d&page=%d&archived=false", workspaceID, pageSize, page)
		data, err := c.doRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, fmt.Errorf("getting projects: %w", err)
		}

		var projects []Project
		if err := json.Unmarshal(data, &projects); err != nil {
			return nil, fmt.Errorf("parsing projects response: %w", err)
		}

		allProjects = append(allProjects, projects...)

		if len(projects) < pageSize {
			break
		}
		page++
	}

	c.cache.Set(allProjects)
	return allProjects, nil
}

func (c *Client) CreateTimeEntry(ctx context.Context, workspaceID string, entry TimeEntryRequest) (*TimeEntry, error) {
	path := fmt.Sprintf("/workspaces/%s/time-entries", workspaceID)
	data, err := c.doRequest(ctx, http.MethodPost, path, entry)
	if err != nil {
		return nil, fmt.Errorf("creating time entry: %w", err)
	}

	var created TimeEntry
	if err := json.Unmarshal(data, &created); err != nil {
		return nil, fmt.Errorf("parsing time entry response: %w", err)
	}

	return &created, nil
}
