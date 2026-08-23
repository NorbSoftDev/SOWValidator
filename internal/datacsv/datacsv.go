// Package datacsv reads the game's CSV files the same way the engine does.
//
// This deliberately does NOT use encoding/csv. Fields are split by successive
// CUtil::ParseGet calls (Shared/util.cpp:1138) over CUtil::ParseFind, which
// knows one rule about quoting and no more: a quote counts only where a field
// begins, and the delimiter is then looked for after the closing quote. A quote
// anywhere else in a field is an ordinary character, and is stripped from the
// value.
//
// Splitting any other way would validate a file the engine never sees.
package datacsv

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// utf8BOM is written as bytes rather than an escape so the source file itself
// stays pure ASCII -- a literal BOM in Go source is a compile error.
var utf8BOM = string([]byte{0xEF, 0xBB, 0xBF})

// Row is one non-skipped line of a data file.
type Row struct {
	Line   int      // 1-based line number in the file
	Fields []string // raw comma-split fields, untrimmed
	Raw    string
}

// Field returns field i, or "" if the row is too short.
// The engine is worse than this: its parser returns the remainder of the line
// when it runs out of delimiters, so a short row yields garbage rather than
// an empty string. Callers should check Len() explicitly.
func (r Row) Field(i int) string {
	if i < 0 || i >= len(r.Fields) {
		return ""
	}
	return strings.TrimSpace(r.Fields[i])
}

func (r Row) Len() int { return len(r.Fields) }

// Int parses field i the way the engine does: leading numeric prefix,
// anything unparseable becomes 0. ok reports whether the field was a clean
// integer, so callers can distinguish "0" from "garbage".
func (r Row) Int(i int) (val int, ok bool) {
	s := r.Field(i)
	if s == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	return atoiPrefix(s), false
}

// Float parses field i leniently, mirroring the engine's float parser.
func (r Row) Float(i int) (val float64, ok bool) {
	s := r.Field(i)
	if s == "" {
		return 0, false
	}
	// Trailing '+' marks a variable distance in drills.csv; tolerate it.
	s = strings.TrimSuffix(s, "+")
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, true
	}
	return 0, false
}

// SplitLoader splits a line into fields the way successive ParseGet calls do,
// quoting rule and all. A quote is honoured only where a field begins; every
// quote is then stripped from the value, as ParseGet does.
func SplitLoader(s string) []string {
	var out []string
	for {
		i := parseFind(s)
		if i < 0 {
			return append(out, strings.ReplaceAll(s, `"`, ""))
		}
		out = append(out, strings.ReplaceAll(s[:i], `"`, ""))
		s = s[i+1:]
	}
}

// parseFind is ParseFind: the next comma, unless the field opens with a quote,
// in which case the next comma after the closing quote. An opening quote with
// no closing one falls back to the plain search, which is what the engine's
// Find does when it returns -1 for the closing quote.
func parseFind(s string) int {
	if len(s) > 0 && s[0] == '"' {
		if j := strings.IndexByte(s[1:], '"'); j >= 0 {
			if k := strings.IndexByte(s[j+2:], ','); k >= 0 {
				return j + 2 + k
			}
			return -1
		}
	}
	return strings.IndexByte(s, ',')
}

// Atoi is MyAtoi, which is C atoi: optional sign, digits, stop at the first
// thing that is neither. Anything it cannot make sense of is 0, which is what
// the engine then uses.
func Atoi(s string) int { return atoiPrefix(s) }

// atoiPrefix mimics C atoi(): optional sign, digits, stop at first non-digit.
func atoiPrefix(s string) int {
	s = strings.TrimSpace(s)
	i, neg := 0, false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return 0
	}
	n, err := strconv.Atoi(s[start:i])
	if err != nil {
		return 0
	}
	if neg {
		return -n
	}
	return n
}

// File is a parsed data file.
type File struct {
	Path   string
	Header Row
	Rows   []Row
	// Lines holds every line in the file, the dropped ones included. A file
	// whose records span a run of lines -- drills.csv, where a drill declares
	// on its first line how many lines of slot map follow it -- cannot be read
	// from Rows alone, because the row filter throws away exactly the blank and
	// comma-led lines such a run is free to contain.
	Lines []string
}

// LineCount is how many lines the file has.
func (f *File) LineCount() int { return len(f.Lines) }

// RawLine returns line n exactly as it was read, numbered from 1 to match
// Row.Line. A line past the end is "", which is also what a blank line gives,
// so a caller walking off the end sees what it would see at any other blank.
func (f *File) RawLine(n int) string {
	if n < 1 || n > len(f.Lines) {
		return ""
	}
	return f.Lines[n-1]
}

// Load reads path and applies the engine's row filter: the first line is the
// header, and any line that is empty or begins with a comma is skipped
// . Those skipped lines are how the CSVs carry
// their documentation rows.
func Load(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	out := &File{Path: path}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := strings.TrimRight(sc.Text(), "\r")
		if lineNo == 1 {
			// Strip a UTF-8 BOM so the first column name compares correctly.
			raw = strings.TrimPrefix(raw, utf8BOM)
		}
		out.Lines = append(out.Lines, raw)
		if lineNo == 1 {
			out.Header = Row{Line: lineNo, Fields: SplitLoader(raw), Raw: raw}
			continue
		}
		if raw == "" || raw[0] == ',' {
			continue
		}
		out.Rows = append(out.Rows, Row{
			Line:   lineNo,
			Fields: SplitLoader(raw),
			Raw:    raw,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
