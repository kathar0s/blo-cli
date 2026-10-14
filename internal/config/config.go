package config

import (
	"os"
	"path/filepath"
)

const (
	// AppName is used for the config directory under the user's home.
	AppName = "billing-on"

	// EnvEmail and EnvPassword allow non-interactive auth (CI/automation).
	EnvEmail    = "BILLINGON_EMAIL"
	EnvPassword = "BILLINGON_PASSWORD"

	// EnvBaseURL overrides the API base URL (useful for testing).
	EnvBaseURL = "BILLINGON_BASE_URL"

	// DefaultBaseURL is the production host. The exact API base path is
	// confirmed from the captured network traffic (see _captures/).
	DefaultBaseURL = "https://www.billing-on.com"
)

// Dir returns the per-user config directory, e.g. ~/.config/billing-on.
// Honors XDG_CONFIG_HOME when set.
func Dir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, AppName), nil
}

// CredentialsPath returns the path to the stored credentials file.
func CredentialsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// BaseURL resolves the API base URL, honoring the env override.
func BaseURL() string {
	if v := os.Getenv(EnvBaseURL); v != "" {
		return v
	}
	return DefaultBaseURL
}
