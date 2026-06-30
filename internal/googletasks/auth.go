package googletasks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gtasks "google.golang.org/api/tasks/v1"
)

func tokenPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "taskctl", "google_token.json")
}

func oauthConfig(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Scopes:       []string{gtasks.TasksScope},
		Endpoint:     google.Endpoint,
		RedirectURL:  "urn:ietf:wg:oauth:2.0:oob",
	}
}

// IsConfigured returns true if Google Tasks credentials are set in config.
func IsConfigured() bool {
	cfg := loadGoogleConfig()
	return cfg.ClientID != "" && cfg.ClientSecret != ""
}

// IsAuthenticated returns true if a token file exists.
func IsAuthenticated() bool {
	_, err := os.ReadFile(tokenPath())
	return err == nil
}

func newHTTPClient(ctx context.Context, clientID, clientSecret string) (*http.Client, error) {
	cfg := oauthConfig(clientID, clientSecret)

	data, err := os.ReadFile(tokenPath())
	if err != nil {
		return nil, fmt.Errorf("not authenticated — run: taskctl auth google")
	}
	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("invalid token file: %w", err)
	}
	ts := cfg.TokenSource(ctx, &token)
	// persist refreshed token
	newToken, _ := ts.Token()
	if newToken != nil && newToken.AccessToken != token.AccessToken {
		_ = saveToken(newToken)
	}
	return oauth2.NewClient(ctx, ts), nil
}

// AuthURL returns the OAuth2 URL the user must visit.
func AuthURL(clientID, clientSecret string) string {
	cfg := oauthConfig(clientID, clientSecret)
	return cfg.AuthCodeURL("taskctl", oauth2.AccessTypeOffline)
}

// ExchangeCode exchanges an auth code for a token and saves it.
func ExchangeCode(ctx context.Context, clientID, clientSecret, code string) error {
	cfg := oauthConfig(clientID, clientSecret)
	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange: %w", err)
	}
	return saveToken(token)
}

func saveToken(token *oauth2.Token) error {
	data, err := json.Marshal(token)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(tokenPath()), 0755); err != nil {
		return err
	}
	return os.WriteFile(tokenPath(), data, 0600)
}
