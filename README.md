# clockr

AI-powered time-tracking CLI that prompts you periodically, takes plain-English descriptions of your work, uses AI (via OpenRouter) to match them to Clockify projects, and creates time entries.

## Prerequisites

- Go 1.24+
- An [OpenRouter](https://openrouter.ai) API key (or any OpenAI-compatible API)
- A [Clockify](https://clockify.me) account and API key

## Install

### Homebrew

```sh
brew install christopherklint97/tap/clockr
```

### Go

```sh
go install github.com/christopherklint97/clockr/cmd/clockr@latest
```

### From source

```sh
make build        # builds to bin/clockr
make install      # installs to $GOPATH/bin
```

## Setup

```sh
clockr config     # opens ~/.config/clockr/config.toml in $EDITOR
```

Set your Clockify API key at minimum:

```toml
[clockify]
api_key = "your-api-key-here"
workspace_id = ""  # optional, auto-detected from your default workspace

[schedule]
interval_minutes = 60
work_start = "09:00"
work_end = "17:00"
work_days = [1, 2, 3, 4, 5]

[ai]
provider = "openrouter"
model = "anthropic/claude-sonnet-4-6"
# api_key = ""  # or set OPENROUTER_API_KEY env var

[notifications]
enabled = true
snooze_options = [5, 15]

[calendar]
enabled = false
source = ""  # URL or file path to an .ics calendar
```

You can also set credentials via environment variables (works with `.env` / direnv):

```sh
export CLOCKIFY_API_KEY="your-api-key-here"
export CLOCKIFY_WORKSPACE_ID="your-workspace-id"  # optional
```

Verify your setup:

```sh
clockr projects   # should list your Clockify projects
```

## Usage

### Log a time entry interactively

```sh
clockr log
```

Opens a TUI where you first confirm the duration (defaults to your configured interval), then describe your work in plain English. The AI matches it to your Clockify projects and suggests time allocations. You can accept, edit, retry, or skip.

### Repeat the last entry

```sh
clockr log --same
```

### Pre-fill the last description

```sh
clockr log --repeat
```

Pre-fills the TUI with your last description. You can also press `Ctrl+R` inside the TUI to load it.

### Log a date range (batch mode)

```sh
clockr log --from 2026-02-23 --to 2026-02-27
clockr log --from monday --to friday
clockr log --from "last tuesday" --to today
```

Dates accept `YYYY-MM-DD` or natural language (`monday`, `last friday`, `today`, etc.). Bare weekday names resolve to the most recent past occurrence.

Opens a batch TUI for logging multiple days at once. The AI sees all work days in the range (skipping weekends/non-work-days), your calendar events per day, and your description, then produces allocations grouped by day for review. Useful when you've missed logging for several days. Limited to 10 work days per batch.

### GitHub integration

Add GitHub commit and PR context to help the AI match your work to projects:

```sh
clockr log --github
clockr log --from monday --to friday --github
```

On first run, clockr fetches your repos and presents a searchable picker to select which ones to track. Selections are saved to config for reuse. Authentication resolves automatically via `gh auth token`, `GITHUB_TOKEN` env var, or config value.

Manage saved repos:

```sh
clockr github repos         # list saved repos
clockr github repos reset   # clear saved repos (re-prompts picker)
```

### Calendar integration

#### ICS (Google Calendar, etc.)

Configure an `.ics` calendar source to pre-fill the work description with your meeting/event summaries:

```toml
[calendar]
enabled = true
source = "https://calendar.google.com/calendar/ical/.../basic.ics"
```

#### Microsoft Graph API (Outlook/Microsoft 365)

For Outlook calendars, use the Microsoft Graph API to fetch past and future events (ICS published URLs only include future events):

1. Register an app in [Azure Portal](https://portal.azure.com) → App registrations
2. Set "Supported account types" to single-tenant (your org)
3. Under Authentication → "Allow public client flows" → Yes
4. Under API permissions → Add `Calendars.Read` (delegated)
5. Add the app IDs to your config:

```toml
[calendar]
enabled = true
source = "graph"

[calendar.graph]
client_id = "your-azure-app-client-id"
tenant_id = "your-azure-tenant-id"
```

Or set via environment variables: `MSGRAPH_CLIENT_ID`, `MSGRAPH_TENANT_ID`.

6. Authenticate:

```sh
clockr calendar auth   # opens browser for device code flow
```

Test it with:

```sh
clockr calendar test
```

### Prompt file mode

```sh
clockr log --prompt-file
clockr log --from monday --to friday --prompt-file
```

Instead of calling the AI API directly, writes the AI prompt to `~/.config/clockr/tmp/clockr_prompt.md` and copies it to your clipboard. If you're in tmux with a Claude Code session in an adjacent pane, the prompt is automatically injected. Press Enter in the TUI once the response has been written to `~/.config/clockr/tmp/clockr_response.json`.

### Run the scheduler

```sh
clockr start
```

Runs in the foreground (use tmux/screen to background). Prompts you at each interval during work hours with a dialog and TUI. If you start the scheduler outside work hours, a confirmation prompt lets you override and receive prompts regardless of work hours for that session.

#### Notification dialog

When a scheduler tick fires, clockr shows a platform-aware dialog with three options:

- **Log Now** — opens the TUI immediately
- **Snooze** (5 min, 15 min) — re-prompts after the chosen delay
- **Next Timer** — skips this tick entirely

On macOS the dialog uses `osascript` (native system dialog). On Linux it tries `zenity`, then `kdialog`, then falls back to a terminal menu. Snooze durations are configurable via `snooze_options` in `[notifications]`. Set `enabled = false` to skip the dialog and go straight to the TUI.

```sh
clockr stop       # sends SIGTERM to the running scheduler
```

### View today's entries

```sh
clockr status
```

### All commands

| Command | Description |
|---------|-------------|
| `clockr start` | Start the time-tracking scheduler |
| `clockr stop` | Stop the running scheduler |
| `clockr log` | Log a time entry interactively |
| `clockr log --same` | Repeat last entry for current interval |
| `clockr log --repeat` | Pre-fill TUI with last description (also Ctrl+R) |
| `clockr log --from DATE --to DATE` | Batch log a date range (supports natural language dates) |
| `clockr log --github` | Include GitHub commit/PR context (combinable with other flags) |
| `clockr log --prompt-file` | Write prompt to file/clipboard instead of calling the AI API |
| `clockr status` | Show today's logged entries |
| `clockr projects` | List Clockify projects |
| `clockr config` | Open config in $EDITOR |
| `clockr calendar auth` | Authenticate with Microsoft Graph API |
| `clockr calendar test` | Test calendar integration |
| `clockr github repos` | List saved GitHub repos |
| `clockr github repos reset` | Clear saved repos |

## How it works

1. You describe your work in plain English (e.g., "reviewed PRs and fixed auth bug")
2. The AI matches your description to your Clockify projects and suggests time allocations
3. You accept, edit, or retry the suggestions in the TUI
4. Entries are created in Clockify and stored locally in SQLite
5. Failed entries are automatically retried on the next scheduler tick

## Data

- Config: `~/.config/clockr/config.toml`
- Graph API tokens: `~/.config/clockr/msgraph_tokens.json`
- Database: `~/.config/clockr/clockr.db`
- PID file: `~/.config/clockr/clockr.pid`
- Prompt file temp: `~/.config/clockr/tmp/`
