# CLAUDE.md

## Project overview

clockr is a Go CLI time-tracking assistant. It prompts the user for plain-English work descriptions, uses AI (via OpenRouter) to match them to Clockify projects, and creates time entries.

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
    openrouter.go             — OpenRouter API provider (OpenAI-compatible SDK), JSON schema helpers
    prompt.go                 — System prompt builder, JSON schema definition (single + batch)
    prompt_file.go            — File-based prompt provider: writes prompt to file/clipboard, waits for manual response
    tmux.go                   — Tmux pane detection and auto-injection of prompts into Claude Code sessions
    models.go                 — Suggestion, Allocation, DaySlot, BatchAllocation, BatchSuggestion types
  calendar/
    calendar.go               — iCal fetch (URL or file), GroupByDay, FormatPrefill
  msgraph/
    token_store.go            — OAuth2 token persistence (JSON file, atomic write)
    auth.go                   — Device code flow, token refresh, EnsureValidToken
    client.go                 — Graph API calendarView client, returns []calendar.Event
  github/
    client.go                 — GitHub API client (retry on 429/5xx, Bearer auth), repo/commit/PR fetch, GroupByDay, FormatPrefill
  tui/
    app.go                    — Bubbletea root model, view state machine (single entry)
    batch.go                  — BatchApp TUI for multi-day time entry (--from/--to)
    duration.go               — Duration prompt view (single entry only, lets user override interval)
    input.go                  — Text input view (shared by single and batch)
    suggestions.go            — Suggestion display with accept/edit/retry/skip
    edit.go                   — Inline allocation editor with fuzzy project search
    repopicker.go             — Searchable multi-select repo picker for GitHub integration
    confirm.go                — Work-hours override confirmation TUI (Start anyway / Cancel)
    styles.go                 — Lipgloss style definitions
  scheduler/
    ticker.go                 — Work-hours-aware tick loop, PID file, failed entry retry, IsWorkTime export
    notify.go                 — Platform-aware prompt dialog (macOS osascript, Linux zenity/kdialog, terminal fallback) with snooze support
```

## Key conventions

- All commands are defined in `cmd/clockr/main.go` — no separate command files
- Clockify API base URL: `https://api.clockify.me/api/v1`
- Config/DB/PID files live in `~/.config/clockr/`
- The AI provider (OpenRouter) uses the OpenAI-compatible API with JSON schema for structured output
- Time entries store both in Clockify and local SQLite; failed Clockify entries are retried automatically
- The TUI uses a view state machine: duration → input → loading → suggestion → edit → confirmation (duration view only in single-entry mode)
- The batch TUI (`BatchApp`) has its own parallel state machine with the same flow but day-grouped views
- Clockify credentials can be set via environment variables (`CLOCKIFY_API_KEY`, `CLOCKIFY_WORKSPACE_ID`) for `.env`/direnv support; AI key via `OPENROUTER_API_KEY`
- Calendar integration supports ICS (URL/file) or Microsoft Graph API (`source = "graph"`); batch mode groups events by day
- Microsoft Graph integration uses OAuth2 device code flow; tokens cached in `~/.config/clockr/msgraph_tokens.json` with auto-refresh; requires Azure AD app with `Calendars.Read` delegated permission; config via `[calendar.graph]` or `MSGRAPH_CLIENT_ID`/`MSGRAPH_TENANT_ID` env vars
- GitHub integration (`--github` flag) fetches commits/PRs from user-selected repos; token resolved via `gh auth token` → `GITHUB_TOKEN` env → config; repos saved to config after first picker selection
- `--from`/`--to` flags accept `YYYY-MM-DD` or natural language dates (e.g., `monday`, `last friday`, `today`) via `tj/go-naturaldate`; bare weekday names default to past direction
- `--repeat` flag (and Ctrl+R in TUI) reuses the last description without re-typing
- `--prompt-file` flag writes the AI prompt to `~/.config/clockr/tmp/clockr_prompt.md` and clipboard instead of calling the AI API; if running in tmux, auto-injects into an adjacent Claude Code pane; waits for user to press Enter after the response is written to `~/.config/clockr/tmp/clockr_response.json`
- Scheduler notifications show a platform-aware dialog (Log Now / Snooze / Next Timer); snooze durations configured via `snooze_options` in `[notifications]`; `enabled = false` skips the dialog
- `clockr start` outside work hours shows a TUI confirmation; if overridden, `skipWorkTimeCheck` bypasses work-hours gating for the entire session
- All runtime files (config, DB, PID, tokens, temp prompt/response) are stored under `~/.config/clockr/`

## Testing

```sh
go test ./...
go vet ./...
```

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
- `github.com/openai/openai-go/v3` — OpenAI-compatible SDK (used with OpenRouter)
- `github.com/invopop/jsonschema` — JSON schema generation for structured AI output
