// Package auth persists the authenticated session so subsequent CLI commands
// don't have to log in again. We store the session cookies (not the password)
// plus a few discovered identifiers, mirroring how `gcloud`/`gws` cache auth.
package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/kathar0s/blo-cli/internal/config"
)

// Cookie is a serializable subset of http.Cookie.
type Cookie struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Path  string `json:"path,omitempty"`
}

// Session is what we persist to disk.
type Session struct {
	Email       string    `json:"email"`
	Cookies     []Cookie  `json:"cookies"`
	CSRFHeader  string    `json:"csrfHeader,omitempty"`
	CSRFToken   string    `json:"csrfToken,omitempty"`
	CustomerIdx string    `json:"customerIdx,omitempty"`
	GroupID     string    `json:"groupId,omitempty"`
	SavedAt     time.Time `json:"savedAt"`
}

// Save writes the session to the credentials file with 0600 permissions.
func Save(s *Session) error {
	dir, err := config.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path, err := config.CredentialsPath()
	if err != nil {
		return err
	}
	s.SavedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Load reads the persisted session. Returns os.ErrNotExist if absent.
func Load() (*Session, error) {
	path, err := config.CredentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("auth: corrupt credentials file %s: %w", path, err)
	}
	return &s, nil
}

// Clear removes the stored credentials (logout).
func Clear() error {
	path, err := config.CredentialsPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ToHTTPCookies converts the stored cookies for use with a cookie jar.
func (s *Session) ToHTTPCookies() []*http.Cookie {
	out := make([]*http.Cookie, 0, len(s.Cookies))
	for _, c := range s.Cookies {
		path := c.Path
		if path == "" {
			path = "/"
		}
		out = append(out, &http.Cookie{Name: c.Name, Value: c.Value, Path: path})
	}
	return out
}

// FromHTTPCookies records cookies pulled from a jar for the given URL.
func (s *Session) FromHTTPCookies(cookies []*http.Cookie) {
	s.Cookies = s.Cookies[:0]
	for _, c := range cookies {
		s.Cookies = append(s.Cookies, Cookie{Name: c.Name, Value: c.Value, Path: c.Path})
	}
}

// CredentialsLocation returns the path for display purposes.
func CredentialsLocation() string {
	if p, err := config.CredentialsPath(); err == nil {
		return p
	}
	return filepath.Join("~", ".config", config.AppName, "credentials.json")
}

// MustBaseURL parses the configured base URL (panics only on a programmer error).
func MustBaseURL() *url.URL {
	u, err := url.Parse(config.BaseURL())
	if err != nil {
		panic(err)
	}
	return u
}
