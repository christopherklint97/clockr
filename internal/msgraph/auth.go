package msgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultScope = "Calendars.Read offline_access"

// Auth handles OAuth2 device code flow for Microsoft Graph API.
type Auth struct {
	clientID   string
	tenantID   string
	httpClient *http.Client
	logger     *slog.Logger
}

// NewAuth creates a new Auth instance for the given Azure AD app.
func NewAuth(clientID, tenantID string, logger *slog.Logger) *Auth {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Auth{
		clientID: clientID,
		tenantID: tenantID,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		logger: logger,
	}
}

// DeviceCodeResponse holds the response from the device code endpoint.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

// tokenResponse is the internal response from the token endpoint.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

func (a *Auth) baseURL() string {
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0", a.tenantID)
}

// StartDeviceCodeFlow initiates the device code flow and returns the response
// containing the user code and verification URI.
func (a *Auth) StartDeviceCodeFlow(ctx context.Context) (*DeviceCodeResponse, error) {
	endpoint := a.baseURL() + "/devicecode"

	form := url.Values{
		"client_id": {a.clientID},
		"scope":     {defaultScope},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("requesting device code: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading device code response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed (status %d): %s", resp.StatusCode, string(body))
	}

	var dcResp DeviceCodeResponse
	if err := json.Unmarshal(body, &dcResp); err != nil {
		return nil, fmt.Errorf("parsing device code response: %w", err)
	}

	return &dcResp, nil
}

// PollForToken polls the token endpoint until the user completes authorization.
func (a *Auth) PollForToken(ctx context.Context, deviceCode string, interval int) (*TokenData, error) {
	endpoint := a.baseURL() + "/token"

	if interval < 1 {
		interval = 5
	}

	form := url.Values{
		"client_id":   {a.clientID},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(interval) * time.Second):
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, fmt.Errorf("creating token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := a.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("polling for token: %w", err)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading token response: %w", err)
		}

		var tokenResp tokenResponse
		if err := json.Unmarshal(body, &tokenResp); err != nil {
			return nil, fmt.Errorf("parsing token response: %w", err)
		}

		switch tokenResp.Error {
		case "":
			// Success
			return &TokenData{
				AccessToken:  tokenResp.AccessToken,
				RefreshToken: tokenResp.RefreshToken,
				ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
				Scope:        tokenResp.Scope,
			}, nil
		case "authorization_pending":
			a.logger.Debug("waiting for user authorization")
			continue
		case "slow_down":
			interval += 5
			a.logger.Debug("slowing down polling", "interval", interval)
			continue
		case "expired_token":
			return nil, fmt.Errorf("device code expired — please try again")
		default:
			return nil, fmt.Errorf("token error: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
		}
	}
}

// RefreshAccessToken uses a refresh token to obtain a new access token.
func (a *Auth) RefreshAccessToken(ctx context.Context, refreshToken string) (*TokenData, error) {
	endpoint := a.baseURL() + "/token"

	form := url.Values{
		"client_id":     {a.clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"scope":         {defaultScope},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("creating refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading refresh response: %w", err)
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, fmt.Errorf("parsing refresh response: %w", err)
	}

	if tokenResp.Error != "" {
		return nil, fmt.Errorf("refresh failed: %s — %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	return &TokenData{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
		Scope:        tokenResp.Scope,
	}, nil
}

// EnsureValidToken loads cached tokens, auto-refreshes if expired, and returns a valid access token.
// Returns an error telling the user to run `clockr calendar auth` if no tokens are cached.
func (a *Auth) EnsureValidToken(ctx context.Context) (string, error) {
	tokens, err := LoadTokens()
	if err != nil {
		return "", fmt.Errorf("loading cached tokens: %w", err)
	}
	if tokens == nil {
		return "", fmt.Errorf("not authenticated with Microsoft Graph — run 'clockr calendar auth' first")
	}

	if !tokens.IsExpired() {
		return tokens.AccessToken, nil
	}

	a.logger.Debug("access token expired, refreshing")
	newTokens, err := a.RefreshAccessToken(ctx, tokens.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("token refresh failed (run 'clockr calendar auth' to re-authenticate): %w", err)
	}

	if err := SaveTokens(newTokens); err != nil {
		a.logger.Warn("failed to cache refreshed tokens", "error", err)
	}

	return newTokens.AccessToken, nil
}
