package billing

import (
	"testing"
	"time"
)

func TestCacheTTL(t *testing.T) {
	now := time.Now()
	y, m, _ := now.Date()
	first := time.Date(y, m, 1, 0, 0, 0, 0, now.Location())

	cur := first.Format("2006-01")
	prev := first.AddDate(0, -1, 0).Format("2006-01")
	twoAgo := first.AddDate(0, -2, 0).Format("2006-01")
	next := first.AddDate(0, 1, 0).Format("2006-01")

	cases := []struct {
		month string
		want  time.Duration
	}{
		{"", 0},                // unset → live
		{cur, 0},               // current month → live
		{next, 0},              // future month → live
		{prev, 24 * time.Hour}, // previous month → short TTL (invoice may settle)
		{twoAgo, -1},           // older → permanent
	}
	for _, c := range cases {
		if got := cacheTTL(c.month); got != c.want {
			t.Errorf("cacheTTL(%q) = %v, want %v", c.month, got, c.want)
		}
	}
}
