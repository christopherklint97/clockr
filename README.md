# clockr

AI-powered time-tracking CLI that prompts you periodically, takes plain-English descriptions of your work, uses Claude to match them to Clockify projects, and creates time entries.

## Prerequisites

- Go 1.24+
- [claude CLI](https://docs.anthropic.com/en/docs/claude-code) installed and authenticated
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
provider = "claude-cli"
model = "sonnet"

[notifications]
enabled = true
reminder_delay_seconds = 300

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

Opens a TUI where you describe your work in plain English. Claude matches it to your Clockify projects and suggests time allocations. You can accept, edit, retry, or skip.

### Repeat the last entry

```sh
clockr log --same
```

### Log a date range (batch mode)

```sh
clockr log --from 2026-02-23 --to 2026-02-27
clockr log --from monday --to friday
clockr log --from "last tuesday" --to today
```

Dates accept `YYYY-MM-DD` or natural language (`monday`, `last friday`, `today`, etc.). Bare weekday names resolve to the most recent past occurrence.

Opens a batch TUI for logging multiple days at once. The AI sees all work days in the range (skipping weekends/non-work-days), your calendar events per day, and your description, then produces allocations grouped by day for review. Useful when you've missed logging for several days. Limited to 10 work days per batch.

### Calendar integration

Configure an `.ics` calendar source to pre-fill the work description with your meeting/event summaries:

```toml
[calendar]
enabled = true
source = "https://calendar.google.com/calendar/ical/.../basic.ics"
```

Test it with:

```sh
clockr calendar test
```

### Run the scheduler

```sh
clockr start
```

Runs in the foreground (use tmux/screen to background). Prompts you at each interval during work hours with a desktop notification and TUI.

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
| `clockr log --from DATE --to DATE` | Batch log a date range (supports natural language dates) |
| `clockr status` | Show today's logged entries |
| `clockr projects` | List Clockify projects |
| `clockr config` | Open config in $EDITOR |
| `clockr calendar test` | Test calendar integration |

## How it works

1. You describe your work in plain English (e.g., "reviewed PRs and fixed auth bug")
2. Claude matches your description to your Clockify projects and suggests time allocations
3. You accept, edit, or retry the suggestions in the TUI
4. Entries are created in Clockify and stored locally in SQLite
5. Failed entries are automatically retried on the next scheduler tick

## Data

- Config: `~/.config/clockr/config.toml`
- Database: `~/.config/clockr/clockr.db`
- PID file: `~/.config/clockr/clockr.pid`
