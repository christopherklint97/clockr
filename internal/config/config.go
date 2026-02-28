package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Clockify      ClockifyConfig  `toml:"clockify"`
	Schedule      ScheduleConfig  `toml:"schedule"`
	AI            AIConfig        `toml:"ai"`
	Notifications NotifyConfig    `toml:"notifications"`
	Calendar      CalendarConfig  `toml:"calendar"`
	GitHub        GitHubConfig    `toml:"github"`
}

type GitHubConfig struct {
	Token string   `toml:"token"`
	Repos []string `toml:"repos"`
}

type ClockifyConfig struct {
	APIKey      string `toml:"api_key"`
	WorkspaceID string `toml:"workspace_id"`
	BaseURL     string `toml:"base_url"`
}

type ScheduleConfig struct {
	IntervalMinutes int    `toml:"interval_minutes"`
	WorkStart       string `toml:"work_start"`
	WorkEnd         string `toml:"work_end"`
	WorkDays        []int  `toml:"work_days"`
}

type AIConfig struct {
	Provider string `toml:"provider"` // "claude-cli" or "anthropic-api"
	Model    string `toml:"model"`
}

type NotifyConfig struct {
	Enabled       bool `toml:"enabled"`
	ReminderDelay int  `toml:"reminder_delay_seconds"`
}

type CalendarConfig struct {
	Enabled bool        `toml:"enabled"`
	Source  string      `toml:"source"` // "graph" | ICS URL | file path
	Graph   GraphConfig `toml:"graph"`
}

type GraphConfig struct {
	ClientID string `toml:"client_id"`
	TenantID string `toml:"tenant_id"`
}

func DefaultConfig() Config {
	return Config{
		Schedule: ScheduleConfig{
			IntervalMinutes: 60,
			WorkStart:       "09:00",
			WorkEnd:         "17:00",
			WorkDays:        []int{1, 2, 3, 4, 5},
		},
		AI: AIConfig{
			Provider: "claude-cli",
			Model:    "sonnet",
		},
		Notifications: NotifyConfig{
			Enabled:       true,
			ReminderDelay: 300,
		},
		Calendar: CalendarConfig{
			Enabled: false,
			Source:  "",
		},
	}
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".config", "clockr"), nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			applyEnvOverrides(&cfg)
			return &cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	applyEnvOverrides(&cfg)

	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("CLOCKIFY_API_KEY"); v != "" {
		cfg.Clockify.APIKey = v
	}
	if v := os.Getenv("CLOCKIFY_WORKSPACE_ID"); v != "" {
		cfg.Clockify.WorkspaceID = v
	}
	if v := os.Getenv("CLOCKIFY_BASE_URL"); v != "" {
		cfg.Clockify.BaseURL = v
	}
	if v := os.Getenv("GITHUB_TOKEN"); v != "" {
		cfg.GitHub.Token = v
	}
	if v := os.Getenv("MSGRAPH_CLIENT_ID"); v != "" {
		cfg.Calendar.Graph.ClientID = v
	}
	if v := os.Getenv("MSGRAPH_TENANT_ID"); v != "" {
		cfg.Calendar.Graph.TenantID = v
	}
}

func EnsureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0755)
}

// SaveGitHubRepos persists the selected GitHub repos to the config file
// using a read-modify-write approach to preserve other settings.
func SaveGitHubRepos(repos []string) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	cfg := make(map[string]any)

	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading config: %w", err)
	}
	if len(data) > 0 {
		if err := toml.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("parsing config: %w", err)
		}
	}

	gh, ok := cfg["github"].(map[string]any)
	if !ok {
		gh = make(map[string]any)
	}
	gh["repos"] = repos
	cfg["github"] = gh

	if err := EnsureConfigDir(); err != nil {
		return err
	}

	out, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(path, out, 0644)
}
