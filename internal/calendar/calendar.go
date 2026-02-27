package calendar

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	ical "github.com/emersion/go-ical"
)

// Event represents a parsed calendar event.
type Event struct {
	Summary   string
	StartTime time.Time
	EndTime   time.Time
}

// Fetch retrieves and parses iCalendar events from a URL or file path,
// returning events that overlap with the given time window.
func Fetch(ctx context.Context, source string, windowStart, windowEnd time.Time) ([]Event, error) {
	var r io.ReadCloser

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, fmt.Errorf("creating request: %w", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetching calendar: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("calendar fetch returned status %d", resp.StatusCode)
		}
		r = resp.Body
	} else {
		f, err := os.Open(source)
		if err != nil {
			return nil, fmt.Errorf("opening calendar file: %w", err)
		}
		r = f
	}
	defer r.Close()

	dec := ical.NewDecoder(r)
	var events []Event

	for {
		cal, err := dec.Decode()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parsing calendar: %w", err)
		}

		for _, component := range cal.Children {
			if component.Name != ical.CompEvent {
				continue
			}
			event := ical.Event{Component: component}

			start, err := event.DateTimeStart(nil)
			if err != nil {
				continue // skip malformed events
			}
			end, err := event.DateTimeEnd(nil)
			if err != nil {
				continue
			}

			// Include events that overlap with the window
			if start.Before(windowEnd) && end.After(windowStart) {
				summary, _ := event.Props.Text(ical.PropSummary)
				if summary != "" {
					events = append(events, Event{
						Summary:   summary,
						StartTime: start,
						EndTime:   end,
					})
				}
			}
		}
	}

	return events, nil
}

// GroupByDay groups events by date string (YYYY-MM-DD in local time).
func GroupByDay(events []Event) map[string][]Event {
	grouped := make(map[string][]Event)
	for _, e := range events {
		key := e.StartTime.Local().Format("2006-01-02")
		grouped[key] = append(grouped[key], e)
	}
	return grouped
}

// FormatPrefill joins event summaries with "; " for use as TUI textarea prefill text.
func FormatPrefill(events []Event) string {
	if len(events) == 0 {
		return ""
	}
	summaries := make([]string, len(events))
	for i, e := range events {
		summaries[i] = e.Summary
	}
	return strings.Join(summaries, "; ")
}
