package crypto

import (
	"encoding/hex"
	"strings"
	"testing"
)

// Captured encBody from a real listCustomerAjax.do request (contains no
// sensitive data). It must decrypt to the known plaintext, proving our key
// derivation matches the site's JS exactly.
const (
	sampleEncBody   = "9cdd857baed7bac3c450101a3dd21e12d7bb5ceca4ccf615fcab0b0ff4baea12ff7f6461cc17b5e4c8d345daab85d04c1e00163b5bc4c353f0ea3ccd6a1c507c"
	samplePlaintext = `{"partnerIdx":"","approveFlag":"Y"}`
)

func TestAESKeyMatchesSite(t *testing.T) {
	got := hex.EncodeToString(aesKey())
	const want = "6c29625d04c184ca45bf80b3629cf798"
	if got != want {
		t.Fatalf("aesKey = %s, want %s", got, want)
	}
}

func TestDecBodyCaptured(t *testing.T) {
	got, err := DecBody(sampleEncBody)
	if err != nil {
		t.Fatalf("DecBody error: %v", err)
	}
	if got != samplePlaintext {
		t.Fatalf("DecBody = %q, want %q", got, samplePlaintext)
	}
}

func TestEncBodyRoundTrip(t *testing.T) {
	in := `{"searchDate":"2026-05","customerIdx":"1234","groupIdForCustomer":"5678"}`
	enc, err := EncBody(in)
	if err != nil {
		t.Fatalf("EncBody error: %v", err)
	}
	if len(enc) < 32 || strings.ContainsAny(enc, "ghijklmnopqrstuvwxyz") {
		t.Fatalf("EncBody output not hex: %q", enc)
	}
	out, err := DecBody(enc)
	if err != nil {
		t.Fatalf("DecBody error: %v", err)
	}
	if out != in {
		t.Fatalf("round-trip = %q, want %q", out, in)
	}
}
