// Package cache is a small on-disk response cache for Billing-On queries.
//
// Billing data for past months is effectively immutable, so re-fetching it on
// every command is wasteful (and the portal allows only one session at a time).
// Callers pass a TTL derived from the queried month: the current month is never
// cached (it changes continuously), the previous month is cached briefly (the
// invoice may still settle), and older months are cached permanently.
//
// The cache key must capture everything that affects a response (account,
// endpoint, params); callers build it via Key(). Entries are stored as JSON
// envelopes under the OS user-cache directory.
package cache

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ForceRefresh, when true, makes Get always miss so callers fetch live data.
// The fresh result is still written back via Put. Set from the --refresh flag.
var ForceRefresh bool

type envelope struct {
	Saved int64  `json:"saved"` // unix seconds when stored
	Body  string `json:"body"`  // base64-encoded raw response bytes
}

func dir() string {
	base, err := os.UserCacheDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	return filepath.Join(base, "blo")
}

// Dir returns the cache directory path (for display).
func Dir() string { return dir() }

// Key derives a stable cache key from the given parts. A NUL separator keeps
// the parts unambiguous so distinct inputs can't collide.
func Key(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func entryPath(key string) string { return filepath.Join(dir(), key+".json") }

// Get returns the cached body for key if present and still fresh under ttl.
// ttl < 0 means the entry never expires; ttl == 0 disables the cache (always a
// miss). ForceRefresh also forces a miss.
func Get(key string, ttl time.Duration) ([]byte, bool) {
	if ttl == 0 || ForceRefresh {
		return nil, false
	}
	b, err := os.ReadFile(entryPath(key))
	if err != nil {
		return nil, false
	}
	var e envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, false
	}
	if ttl > 0 {
		age := time.Since(time.Unix(e.Saved, 0))
		if age < 0 || age > ttl {
			return nil, false
		}
	}
	body, err := base64.StdEncoding.DecodeString(e.Body)
	if err != nil {
		return nil, false
	}
	return body, true
}

// Put stores body under key. ttl == 0 means "do not cache", so the write is
// skipped — callers can pass the same ttl to Get and Put unconditionally.
// Failures are silent: the cache is a best-effort optimization, never required.
func Put(key string, body []byte, ttl time.Duration) {
	// ttl 0 means "do not cache"; an empty body is never worth caching (and a
	// blank response is more likely a transient hiccup than real data).
	if ttl == 0 || len(body) == 0 {
		return
	}
	if err := os.MkdirAll(dir(), 0o700); err != nil {
		return
	}
	e := envelope{Saved: time.Now().Unix(), Body: base64.StdEncoding.EncodeToString(body)}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	tmp := entryPath(key) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, entryPath(key))
}

// Clear removes all cached entries, returning the number of files deleted.
func Clear() (int, error) {
	entries, err := os.ReadDir(dir())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if err := os.Remove(filepath.Join(dir(), e.Name())); err == nil {
			n++
		}
	}
	return n, nil
}

// Stats returns the number of cached entries and their total size in bytes.
func Stats() (count int, size int64) {
	entries, err := os.ReadDir(dir())
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if info, err := e.Info(); err == nil {
			count++
			size += info.Size()
		}
	}
	return count, size
}
