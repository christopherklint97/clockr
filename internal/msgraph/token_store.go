package msgraph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// TokenData holds OAuth2 token data for Microsoft Graph API.
type TokenData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	Scope        string    `json:"scope"`
}

// IsExpired returns true if the token is expired or will expire within 5 minutes.
func (t *TokenData) IsExpired() bool {
	return time.Now().Add(5 * time.Minute).After(t.ExpiresAt)
}

func tokenPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("finding home directory: %w", err)
	}
	return filepath.Join(home, ".config", "clockr", "msgraph_tokens.json"), nil
}

// LoadTokens reads cached tokens from ~/.config/clockr/msgraph_tokens.json.
// Returns nil, nil if the file does not exist.
func LoadTokens() (*TokenData, error) {
	path, err := tokenPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading token file: %w", err)
	}

	var tokens TokenData
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parsing token file: %w", err)
	}

	return &tokens, nil
}

// SaveTokens writes tokens to ~/.config/clockr/msgraph_tokens.json with 0600 permissions.
// Uses atomic write (tmp + rename) to prevent corruption.
func SaveTokens(tokens *TokenData) error {
	path, err := tokenPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tokens: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("writing temp token file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming token file: %w", err)
	}

	return nil
}
