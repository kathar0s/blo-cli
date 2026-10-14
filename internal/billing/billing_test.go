package billing

import (
	"testing"

	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/transform"
)

func TestParseMoney(t *testing.T) {
	cases := []struct {
		in   string
		want float64
	}{
		{"1,510.42", 1510.42},
		{"  2,000  ", 2000},
		{"0", 0},
		{"", 0},
		{"-1,224.46", -1224.46},
		{"junk", 0},
		{"3.14159", 3.14159},
	}
	for _, c := range cases {
		if got := parseMoney(c.in); got != c.want {
			t.Errorf("parseMoney(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestSplitAccountID(t *testing.T) {
	cases := []struct {
		in, no, alias string
	}{
		{"123456789012|team@example.com", "123456789012", "team@example.com"},
		{"123456789012", "123456789012", ""},
		{"", "", ""},
		{"a|b|c", "a", "b|c"}, // splits on first separator only
	}
	for _, c := range cases {
		no, alias := splitAccountID(c.in)
		if no != c.no || alias != c.alias {
			t.Errorf("splitAccountID(%q) = (%q,%q), want (%q,%q)", c.in, no, alias, c.no, c.alias)
		}
	}
}

func TestMonthFormats(t *testing.T) {
	if got := monthHyphen("202601"); got != "2026-01" {
		t.Errorf("monthHyphen(202601) = %q", got)
	}
	if got := monthHyphen("2026-01"); got != "2026-01" {
		t.Errorf("monthHyphen(2026-01) = %q", got)
	}
	if got := monthCompact("2026-01"); got != "202601" {
		t.Errorf("monthCompact(2026-01) = %q", got)
	}
	if got := monthCompact("202601"); got != "202601" {
		t.Errorf("monthCompact(202601) = %q", got)
	}
}

func TestPickDefaultGroup(t *testing.T) {
	if g := pickDefaultGroup(nil); g.GroupID != "" {
		t.Errorf("empty input should yield zero Group, got %+v", g)
	}
	gs := []Group{
		{GroupID: "g1", GroupName: "ACME Corp Group 1"},
		{GroupID: "g0", GroupName: "ACME"}, // shortest name = unified parent
		{GroupID: "g2", GroupName: "ACME Corp Group 2"},
	}
	if g := pickDefaultGroup(gs); g.GroupID != "g0" {
		t.Errorf("pickDefaultGroup picked %q, want g0 (shortest name)", g.GroupID)
	}
}

func TestSplitCSVHeader(t *testing.T) {
	h, body := splitCSVHeader([]byte("a,b,c\n1,2,3\n4,5,6\n"))
	if string(h) != "a,b,c" {
		t.Errorf("header = %q", h)
	}
	if string(body) != "1,2,3\n4,5,6" {
		t.Errorf("body = %q", body)
	}
	// header only, no data rows
	h, body = splitCSVHeader([]byte("a,b,c\n"))
	if string(h) != "a,b,c" || len(body) != 0 {
		t.Errorf("header-only: h=%q body=%q", h, body)
	}
	// empty
	h, body = splitCSVHeader(nil)
	if h != nil || body != nil {
		t.Errorf("empty: h=%q body=%q", h, body)
	}
}

func TestCountLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"one", 1},
		{"one\ntwo", 2},
		{"one\ntwo\nthree", 3},
	}
	for _, c := range cases {
		if got := countLines([]byte(c.in)); got != c.want {
			t.Errorf("countLines(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestDecodeCP949(t *testing.T) {
	if got, err := decodeCP949(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty: got=%q err=%v", got, err)
	}
	// Round-trip a Korean string: encode to EUC-KR, then decodeCP949 back.
	want := "금액(USD)"
	euckr, _, err := transform.Bytes(korean.EUCKR.NewEncoder(), []byte(want))
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeCP949(euckr)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("decodeCP949 round-trip = %q, want %q", got, want)
	}
}
