package cache

import (
	"path/filepath"
	"testing"
	"time"
)

// isolate points the cache at a temp dir for the duration of a test, working on
// both Linux (XDG_CACHE_HOME) and macOS ($HOME/Library/Caches).
func isolate(t *testing.T) {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", tmp)
	t.Setenv("HOME", tmp)
	ForceRefresh = false
	t.Cleanup(func() { ForceRefresh = false })
}

func TestPutGetRoundTrip(t *testing.T) {
	isolate(t)
	key := Key("acct", "https://x", "/a.do", "POST", `{"m":"2026-01"}`)
	body := []byte(`{"hello":"world"}`)

	if _, ok := Get(key, -1); ok {
		t.Fatal("expected miss before Put")
	}
	Put(key, body, -1)
	got, ok := Get(key, -1)
	if !ok {
		t.Fatal("expected hit after Put")
	}
	if string(got) != string(body) {
		t.Fatalf("body mismatch: %q", got)
	}
}

func TestNoCacheTTLSkipsWriteAndRead(t *testing.T) {
	isolate(t)
	key := Key("k")
	Put(key, []byte("x"), 0) // ttl 0 => do not store
	if _, ok := Get(key, -1); ok {
		t.Fatal("ttl=0 Put should not have written anything")
	}
	Put(key, []byte("x"), -1)
	if _, ok := Get(key, 0); ok {
		t.Fatal("ttl=0 Get should always miss")
	}
}

func TestEmptyBodyNotCached(t *testing.T) {
	isolate(t)
	key := Key("k")
	Put(key, nil, -1)
	Put(key, []byte{}, -1)
	if _, ok := Get(key, -1); ok {
		t.Fatal("empty body should not be cached")
	}
}

func TestForceRefreshForcesMiss(t *testing.T) {
	isolate(t)
	key := Key("k")
	Put(key, []byte("x"), -1)
	if _, ok := Get(key, -1); !ok {
		t.Fatal("expected hit")
	}
	ForceRefresh = true
	if _, ok := Get(key, -1); ok {
		t.Fatal("ForceRefresh should force a miss")
	}
}

func TestTTLExpiry(t *testing.T) {
	isolate(t)
	key := Key("k")
	Put(key, []byte("x"), time.Hour)
	if _, ok := Get(key, time.Hour); !ok {
		t.Fatal("fresh entry should be a hit")
	}
	// Backdate the stored timestamp to force expiry under a short TTL.
	if _, ok := Get(key, time.Nanosecond); ok {
		// Entry was written "now"; under a 1ns TTL it is older than the window.
		t.Fatal("entry should be expired under a 1ns TTL")
	}
}

func TestClearAndStats(t *testing.T) {
	isolate(t)
	Put(Key("a"), []byte("aaa"), -1)
	Put(Key("b"), []byte("bbbb"), -1)
	n, size := Stats()
	if n != 2 || size == 0 {
		t.Fatalf("stats: got n=%d size=%d", n, size)
	}
	deleted, err := Clear()
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Fatalf("Clear deleted %d, want 2", deleted)
	}
	if n, _ := Stats(); n != 0 {
		t.Fatalf("after Clear stats n=%d, want 0", n)
	}
}

func TestKeyStability(t *testing.T) {
	if Key("a", "b") != Key("a", "b") {
		t.Fatal("same inputs must yield same key")
	}
	// NUL separation: ("a","b") must differ from ("ab",).
	if Key("a", "b") == Key("ab") {
		t.Fatal("part boundaries must affect the key")
	}
}

func TestDirNonEmpty(t *testing.T) {
	isolate(t)
	if filepath.Base(Dir()) != "blo" {
		t.Fatalf("Dir should end in blo: %s", Dir())
	}
}
