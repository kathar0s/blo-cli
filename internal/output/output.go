// Package output renders results as an aligned table (default), JSON, or CSV.
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Format selects the rendering style.
type Format string

const (
	Table Format = "table"
	JSON  Format = "json"
	CSV   Format = "csv"
)

// FromFlags resolves the output format from the --json/--csv booleans.
func FromFlags(asJSON, asCSV bool) Format {
	switch {
	case asJSON:
		return JSON
	case asCSV:
		return CSV
	default:
		return Table
	}
}

// JSONValue prints any value as indented JSON.
func JSONValue(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Table/CSV rendering of rows with a header.
type Rows struct {
	Header []string
	Data   [][]string
	// Align: 'l' or 'r' per column (defaults to left).
	Align []byte
}

// Render writes rows in the given format. For JSON pass the original struct via
// JSONValue instead; this handles table/csv text formats.
func (r Rows) Render(w io.Writer, f Format) error {
	switch f {
	case CSV:
		return r.renderCSV(w)
	default:
		return r.renderTable(w)
	}
}

func (r Rows) renderTable(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(r.Header, "\t"))
	sep := make([]string, len(r.Header))
	for i, h := range r.Header {
		sep[i] = strings.Repeat("-", max(3, len(h)))
	}
	fmt.Fprintln(tw, strings.Join(sep, "\t"))
	for _, row := range r.Data {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

func (r Rows) renderCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(r.Header); err != nil {
		return err
	}
	for _, row := range r.Data {
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// Money formats a USD amount with thousands separators and 2 decimals.
func Money(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	dot := strings.IndexByte(s, '.')
	intPart, frac := s[:dot], s[dot:]
	var b strings.Builder
	n := len(intPart)
	for i, c := range intPart {
		if i > 0 && (n-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	out := b.String() + frac
	if neg {
		return "-" + out
	}
	return out
}

// Num formats a plain float for CSV (no separators).
func Num(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// Stdout is the default writer.
var Stdout io.Writer = os.Stdout

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
