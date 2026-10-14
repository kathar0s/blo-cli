package cmd

import (
	"testing"

	"github.com/kathar0s/blo-cli/internal/output"
)

func TestFmtCost(t *testing.T) {
	cases := []struct {
		v    float64
		f    output.Format
		want string
	}{
		{0, output.Table, "0.00"},         // exact zero stays 0.00
		{0.004, output.Table, "< 0.01"},   // tiny positive → marker, not 0.00
		{-0.004, output.Table, "> -0.01"}, // tiny negative → marker
		{0.01, output.Table, "0.01"},      // at threshold → normal
		{1234.5, output.Table, "1,234.50"},
		{0.004, output.CSV, "0.00"},     // CSV keeps numeric form (no marker)
		{1234.5, output.CSV, "1234.50"}, // CSV has no thousands separator
	}
	for _, c := range cases {
		if got := fmtCost(c.v, c.f); got != c.want {
			t.Errorf("fmtCost(%v, %v) = %q, want %q", c.v, c.f, got, c.want)
		}
	}
}
