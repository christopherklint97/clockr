package msgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/christopherklint97/clockr/internal/calendar"
)

const graphBaseURL = "https://graph.microsoft.com/v1.0"

// Client is a Microsoft Graph API client for calendar operations.
type Client struct {
	auth       *Auth
	httpClient *http.Client
	logger     *slog.Logger
}

// NewClient creates a new Graph API client.
func NewClient(auth *Auth, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Client{
		auth: auth,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// calendarViewResponse represents the Graph API calendarView response.
type calendarViewResponse struct {
	Value    []graphEvent `json:"value"`
	NextLink string       `json:"@odata.nextLink"`
}

type graphEvent struct {
	Subject     string         `json:"subject"`
	Start       graphDateTime  `json:"start"`
	End         graphDateTime  `json:"end"`
	IsCancelled bool           `json:"isCancelled"`
	IsAllDay    bool           `json:"isAllDay"`
}

type graphDateTime struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// FetchEvents retrieves calendar events from Microsoft Graph for the given time range.
// Returns events in the same calendar.Event format used by the ICS path.
func (c *Client) FetchEvents(ctx context.Context, start, end time.Time) ([]calendar.Event, error) {
	token, err := c.auth.EnsureValidToken(ctx)
	if err != nil {
		return nil, err
	}

	params := url.Values{
		"startDateTime": {start.UTC().Format("2006-01-02T15:04:05")},
		"endDateTime":   {end.UTC().Format("2006-01-02T15:04:05")},
		"$select":       {"subject,start,end,isCancelled,isAllDay"},
		"$top":          {"100"},
		"$orderby":      {"start/dateTime"},
	}

	requestURL := graphBaseURL + "/me/calendarView?" + params.Encode()
	var allEvents []calendar.Event

	for requestURL != "" {
		events, nextLink, err := c.fetchPage(ctx, token, requestURL)
		if err != nil {
			return nil, err
		}
		allEvents = append(allEvents, events...)
		requestURL = nextLink
	}

	c.logger.Debug("graph calendar events fetched", "count", len(allEvents))
	return allEvents, nil
}

func (c *Client) fetchPage(ctx context.Context, token, requestURL string) ([]calendar.Event, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("creating graph request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Prefer", "outlook.timezone=\"UTC\"")

	var resp *http.Response
	maxRetries := 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		resp, err = c.httpClient.Do(req)
		if err != nil {
			if attempt == maxRetries {
				return nil, "", fmt.Errorf("graph API request failed: %w", err)
			}
			time.Sleep(backoff(attempt))
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt == maxRetries {
				return nil, "", fmt.Errorf("graph API returned status %d after %d retries", resp.StatusCode, maxRetries)
			}
			c.logger.Debug("graph API retrying", "status", resp.StatusCode, "attempt", attempt+1)
			time.Sleep(backoff(attempt))
			continue
		}
		break
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("reading graph response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("graph API error (status %d): %s", resp.StatusCode, truncateStr(string(body), 200))
	}

	var viewResp calendarViewResponse
	if err := json.Unmarshal(body, &viewResp); err != nil {
		return nil, "", fmt.Errorf("parsing graph response: %w", err)
	}

	var events []calendar.Event
	for _, ge := range viewResp.Value {
		if ge.IsCancelled || ge.IsAllDay {
			continue
		}
		if ge.Subject == "" {
			continue
		}

		startTime, err := parseGraphDateTime(ge.Start)
		if err != nil {
			c.logger.Debug("skipping event with unparseable start time", "subject", ge.Subject, "error", err)
			continue
		}
		endTime, err := parseGraphDateTime(ge.End)
		if err != nil {
			c.logger.Debug("skipping event with unparseable end time", "subject", ge.Subject, "error", err)
			continue
		}

		events = append(events, calendar.Event{
			Summary:   ge.Subject,
			StartTime: startTime,
			EndTime:   endTime,
		})
	}

	return events, viewResp.NextLink, nil
}

func parseGraphDateTime(gdt graphDateTime) (time.Time, error) {
	// When we request Prefer: outlook.timezone="UTC", times come back in UTC.
	// The dateTime field is in format "2006-01-02T15:04:05.0000000"
	loc := time.UTC
	if gdt.TimeZone != "" && gdt.TimeZone != "UTC" {
		l, err := time.LoadLocation(gdt.TimeZone)
		if err == nil {
			loc = l
		}
	}

	// Try multiple formats — Graph API can return with or without fractional seconds
	for _, layout := range []string{
		"2006-01-02T15:04:05.0000000",
		"2006-01-02T15:04:05",
	} {
		t, err := time.ParseInLocation(layout, gdt.DateTime, loc)
		if err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("cannot parse datetime %q", gdt.DateTime)
}

func backoff(attempt int) time.Duration {
	return time.Duration(math.Pow(2, float64(attempt))) * time.Second
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
