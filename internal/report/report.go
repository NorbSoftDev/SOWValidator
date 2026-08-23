// Package report collects and renders validation findings.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

type Severity int

const (
	Info Severity = iota
	Warn
	Error
)

func (s Severity) String() string {
	switch s {
	case Error:
		return "error"
	case Warn:
		return "warning"
	default:
		return "info"
	}
}

// Finding is a single problem found in a data file.
type Finding struct {
	Severity Severity `json:"-"`
	Level    string   `json:"severity"`
	File     string   `json:"file"`
	Line     int      `json:"line,omitempty"`
	Check    string   `json:"check"`
	Message  string   `json:"message"`
	// Detail is optional extra context shown indented under the message.
	Detail string `json:"detail,omitempty"`
}

type Report struct {
	Findings []Finding `json:"findings"`
	// Root is trimmed from displayed paths to keep output readable.
	Root string `json:"-"`
}

func New(root string) *Report { return &Report{Root: root} }

func (r *Report) Add(sev Severity, check, file string, line int, msg string, detail string) {
	r.Findings = append(r.Findings, Finding{
		Severity: sev,
		Level:    sev.String(),
		File:     file,
		Line:     line,
		Check:    check,
		Message:  msg,
		Detail:   detail,
	})
}

func (r *Report) Errorf(check, file string, line int, detail, format string, a ...any) {
	r.Add(Error, check, file, line, fmt.Sprintf(format, a...), detail)
}

func (r *Report) Warnf(check, file string, line int, detail, format string, a ...any) {
	r.Add(Warn, check, file, line, fmt.Sprintf(format, a...), detail)
}

func (r *Report) Count(sev Severity) int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == sev {
			n++
		}
	}
	return n
}

// Rel shortens a path for display, for callers building Detail text that
// mentions a file other than the one the finding is anchored to.
func (r *Report) Rel(p string) string { return r.rel(p) }

// rel shortens a path for display.
func (r *Report) rel(p string) string {
	if r.Root == "" {
		return p
	}
	if s, err := filepath.Rel(r.Root, p); err == nil && !strings.HasPrefix(s, "..") {
		return s
	}
	return p
}

// sortFindings orders by severity (worst first), then file, then line.
func (r *Report) sortFindings() {
	sort.SliceStable(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Severity != b.Severity {
			return a.Severity > b.Severity
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
}

// WriteText renders a human-readable report. Returns true if any errors were found.
func (r *Report) WriteText(w io.Writer, quiet bool) bool {
	r.sortFindings()

	for _, f := range r.Findings {
		if quiet && f.Severity < Error {
			continue
		}
		loc := r.rel(f.File)
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", loc, f.Line)
		}
		fmt.Fprintf(w, "%s: %s: %s [%s]\n", loc, f.Severity, f.Message, f.Check)
		if f.Detail != "" {
			for _, line := range strings.Split(f.Detail, "\n") {
				fmt.Fprintf(w, "    %s\n", line)
			}
		}
	}

	errs, warns := r.Count(Error), r.Count(Warn)
	fmt.Fprintf(w, "\n%d error(s), %d warning(s)\n", errs, warns)
	return errs > 0
}

// WriteJSON renders machine-readable output. Returns true if any errors were found.
func (r *Report) WriteJSON(w io.Writer) bool {
	r.sortFindings()
	for i := range r.Findings {
		r.Findings[i].File = r.rel(r.Findings[i].File)
	}
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(r)
	return r.Count(Error) > 0
}
