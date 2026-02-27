package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type Config struct {
	Clockify  ClockifyConfig  `toml:"clockify"`
	Schedule  ScheduleConfig  `toml:"schedule"`
	AI        AIConfig        `toml:"ai"`
	Notifications NotifyConfig `toml:"notifications"`
}

type ClockifyConfig struct {
	APIKey      string `toml:"api_key"`
	WorkspaceID string `toml:"workspace_id"`
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
			return &cfg, nil
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return &cfg, nil
}

func EnsureConfigDir() error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(dir, 0755)
}
