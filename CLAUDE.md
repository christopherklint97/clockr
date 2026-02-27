# CLAUDE.md

## Project overview

clockr is a Go CLI time-tracking assistant. It prompts the user for plain-English work descriptions, uses Claude (via the `claude` CLI) to match them to Clockify projects, and creates time entries.

## Build & run

```sh
go build -o bin/clockr ./cmd/clockr   # or: make build
./bin/clockr --help
```

## Project structure

```
cmd/clockr/main.go           — CLI entry point, all cobra commands wired here
internal/
  config/config.go            — TOML config loading from ~/.config/clockr/config.toml
  clockify/
    client.go                 — HTTP client (retry on 429/5xx, X-Api-Key auth)
    models.go                 — API types: User, Project, TimeEntry
    cache.go                  — In-memory project cache with TTL
  store/
    db.go                     — SQLite DB (WAL mode), migrations, state KV
    entries.go                — Entry CRUD (insert, today, last, failed queries)
  ai/
    provider.go               — Provider interface
    claude_cli.go             — Calls `claude` CLI subprocess with --json-schema
    anthropic_api.go          — Stub for direct Anthropic API fallback
    prompt.go                 — System prompt builder, JSON schema definition (single + batch)
    models.go                 — Suggestion, Allocation, DaySlot, BatchAllocation, BatchSuggestion types
  calendar/
    calendar.go               — iCal fetch (URL or file), GroupByDay, FormatPrefill
  tui/
    app.go                    — Bubbletea root model, view state machine (single entry)
    batch.go                  — BatchApp TUI for multi-day time entry (--from/--to)
    input.go                  — Text input view (shared by single and batch)
    suggestions.go            — Suggestion display with accept/edit/retry/skip
    edit.go                   — Inline allocation editor with fuzzy project search
    styles.go                 — Lipgloss style definitions
  scheduler/
    ticker.go                 — Work-hours-aware tick loop, PID file, failed entry retry
    notify.go                 — Desktop notifications via beeep
```

## Key conventions

- All commands are defined in `cmd/clockr/main.go` — no separate command files
- Clockify API base URL: `https://api.clockify.me/api/v1`
- Config/DB/PID files live in `~/.config/clockr/`
- The Claude CLI is invoked with `--output-format json --json-schema` for structured output
- Time entries store both in Clockify and local SQLite; failed Clockify entries are retried automatically
- The TUI uses a view state machine: input → loading → suggestion → edit → confirmation
- The batch TUI (`BatchApp`) has its own parallel state machine with the same flow but day-grouped views
- Clockify credentials can be set via environment variables (`CLOCKIFY_API_KEY`, `CLOCKIFY_WORKSPACE_ID`) for `.env`/direnv support
- Calendar integration fetches `.ics` events to pre-fill work descriptions; batch mode groups events by day
- `--from`/`--to` flags accept `YYYY-MM-DD` or natural language dates (e.g., `monday`, `last friday`, `today`) via `tj/go-naturaldate`; bare weekday names default to past direction

## Testing

```sh
go vet ./...
```

No test files yet.

## Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/pelletier/go-toml/v2` — Config parsing
- `modernc.org/sqlite` — SQLite (pure Go, no CGO required)
- `github.com/charmbracelet/bubbletea` — TUI framework
- `github.com/charmbracelet/lipgloss` — TUI styling
- `github.com/charmbracelet/bubbles` — TUI components (textarea, spinner, textinput)
- `github.com/gen2brain/beeep` — Desktop notifications
- `github.com/emersion/go-ical` — iCalendar parsing for calendar integration
- `github.com/tj/go-naturaldate` — Natural language date parsing for `--from`/`--to` flags
