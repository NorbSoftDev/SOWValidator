package checks

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// GenericFiles are the data files validated structurally. These are the CSVs
// from the game's core data set that have no bespoke check.
//
// None have a documented schema, so the shape is inferred from each file's own
// contents rather than hardcoded. These files are messy by nature: they carry
// documentation rows, repeated sub-headers for multiple tables in one file, and
// column syntax that is deliberately non-numeric. The thresholds below are set
// so that only genuine outliers are reported -- a validator that cries wolf
// does not get run.
// GenericFiles is now empty: every data file the engine loads has a checker
// written against its own loader, in DataFiles. The checks below are kept for
// anything added here before its format is known -- a guess is better than
// nothing, and worse than reading the loader.
var GenericFiles []string

// ReplaceFiles are read only from the highest layer that provides them, so
// only the highest layer providing them is ever read. This is by design: their
// contents cannot be added to piecemeal, a layer must replace the file whole.
//
// Validation has to follow suit -- checking a lower layer's copy would report
// problems in a file the game will never load.
var ReplaceFiles = map[string]bool{
	"courier.csv":     true,
	"statetables.csv": true,
}

// KeySpec says which column holds a file's unique key. Read off the loaders:
// the key is whichever field the loader last parses before using it as a map
// index, so a file whose loader skips past the first field keys on
// column 1, not column 0.
//
// NewGated marks record-type files where column 0 is a record type and only
// rows reading "NEW" open a keyed record.
type KeySpec struct {
	Col      int
	NewGated bool
}

// FileKeys maps a data file to its key column.
//
// Only files whose loader does one map-insert per row are listed. That is the
// pattern where a repeated key is unambiguously a mistake: the loader deletes
// the existing entry and replaces it, so the earlier row is silently lost.
//
// Deliberately absent, because a repeated key is normal in them:
//
//   - artytables.csv  one row per experience level within a table, so
//     ArtyTableID repeats by design.
//   - statetables.csv sectioned state driven by blank separator rows
//
// .
//   - replroster.csv  a 2D side/army index, not a name table
//
// .
//   - gscreens/mscreens/courier/unitattributes  record-type files where
//     column 0 is a record type and only "NEW" rows open a record; the rows
//     between belong to the record above them.
//
// Those need per-file record-structure modelling rather than a key column, and
// guessing produced hundreds of false positives.
var FileKeys = map[string]KeySpec{
	// Key in column 0.
	"unitpack.csv":  {Col: 0},
	"gfxpack.csv":   {Col: 0},
	"gfx.csv":       {Col: 0},
	"unitmodel.csv": {Col: 0},
	"unittype.csv":  {Col: 0},
	"munitions.csv": {Col: 0},

	// Key in column 1: the loader parses past column 0 first.
	"artillery.csv": {Col: 1},
	"rifles.csv":    {Col: 1},
	"sfx.csv":       {Col: 1},
	"efx.csv":       {Col: 1},
}

const (
	// minSamples is how many non-empty values a column needs before its type
	// is inferred at all.
	minSamples = 20
	// numericRatio is the share that must parse as numbers to call a column
	// numeric.
	numericRatio = 0.98
	// maxOutliers caps how many non-numeric values a numeric column may have
	// before we conclude the column simply permits a non-numeric syntax
	// rather than containing typos.
	//
	// This threshold is a confession, not a design: inferring a column's type
	// from the other values in it can only ever guess. Where a file's real
	// format is known -- see drills.go and unitglobal.go, which read those
	// files the way their loaders do -- write the check against the format
	// instead and take the file out of GenericFiles.
	maxOutliers = 2
)

// Generic runs structural validation over one data file. Two checks survive
// the noise floor:
//
//   - short rows, because the engine's parser returns the remainder of the line
//     rather than failing when it runs out of delimiters
//
// , so missing values become plausible garbage.
//   - isolated non-numeric values in an otherwise numeric column, which
//     the engine silently converts to 0.
//
// Duplicate keys are reported using the per-file key column from FileKeys,
// read off each loader. Guessing that every file keys on column 0 produced
// over a thousand false positives.
func Generic(f *datacsv.File, rep *report.Report) {
	if len(f.Rows) == 0 {
		return
	}

	checkDuplicateKeys(f, rep)

	width, count := modalWidth(f)

	// If no single width dominates, the file holds several record shapes
	// (courier.csv, unitattributes.csv) and per-row width proves nothing.
	if float64(count) < 0.6*float64(len(f.Rows)) {
		return
	}

	checkShortRows(f, width, rep)
	checkNumericColumns(f, width, rep)
}

// modalWidth returns the most common field count and how many rows have it.
func modalWidth(f *datacsv.File) (width, count int) {
	tally := map[int]int{}
	for _, row := range f.Rows {
		tally[row.Len()]++
	}
	widths := make([]int, 0, len(tally))
	for w := range tally {
		widths = append(widths, w)
	}
	sort.Slice(widths, func(i, j int) bool {
		if tally[widths[i]] != tally[widths[j]] {
			return tally[widths[i]] > tally[widths[j]]
		}
		return widths[i] > widths[j]
	})
	return widths[0], tally[widths[0]]
}

// isSubHeader reports whether a row is a repeated header for another table
// embedded in the same file. Several of these CSVs stack multiple tables, each
// re-stating the column names; those rows are data to us but labels to a human.
func isSubHeader(f *datacsv.File, row datacsv.Row) bool {
	labels := map[string]bool{}
	for i := 0; i < f.Header.Len(); i++ {
		if l := strings.ToUpper(strings.TrimSpace(f.Header.Field(i))); l != "" {
			labels[l] = true
		}
	}
	hits := 0
	for i := 0; i < row.Len(); i++ {
		if v := strings.ToUpper(row.Field(i)); v != "" && labels[v] {
			hits++
		}
	}
	return hits >= 3
}

func checkShortRows(f *datacsv.File, width int, rep *report.Report) {
	const check = "row-shape"

	for _, row := range f.Rows {
		// Only short rows matter. Trailing commas are endemic in these files
		// and extra fields are simply never read.
		if row.Len() >= width || isSubHeader(f, row) {
			continue
		}
		name := row.Field(colName)
		if name == "" {
			name = "(unnamed)"
		}
		rep.Errorf(check, f.Path, row.Line,
			fmt.Sprintf("row: %s", trim(row.Raw)),
			"%s: %d columns, expected %d -- the engine reads the rest of the line into the missing fields instead of failing",
			name, row.Len(), width)
	}
}

func isNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	// A trailing '+' marks a variable distance in drills.csv.
	s = strings.TrimSuffix(s, "+")
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

func checkNumericColumns(f *datacsv.File, width int, rep *report.Report) {
	const check = "numeric"

	for col := 0; col < width; col++ {
		var samples, numeric int
		var outliers []datacsv.Row

		for _, row := range f.Rows {
			if isSubHeader(f, row) {
				continue
			}
			v := row.Field(col)
			if v == "" {
				continue
			}
			samples++
			if isNumeric(v) {
				numeric++
			} else {
				outliers = append(outliers, row)
			}
		}

		if samples < minSamples || len(outliers) == 0 || len(outliers) > maxOutliers {
			continue
		}
		if float64(numeric)/float64(samples) < numericRatio {
			continue
		}

		label := strings.TrimSpace(f.Header.Field(col))
		if label == "" {
			label = fmt.Sprintf("column %d", col)
		}

		for _, row := range outliers {
			name := row.Field(colName)
			if name == "" {
				name = "(unnamed)"
			}
			rep.Errorf(check, f.Path, row.Line,
				fmt.Sprintf("%d of %d values in this column parse as numbers", numeric, samples),
				"%s: %q in column %d (%s) is not a number -- the engine reads it as 0",
				name, row.Field(col), col, label)
		}
	}
}

// checkDuplicateKeys reports two rows claiming the same key. The loaders all
// resolve this the same way -- delete the existing entry and replace it -- so
// the earlier definition is silently discarded.
func checkDuplicateKeys(f *datacsv.File, rep *report.Report) {
	const check = "duplicate-key"

	spec, ok := FileKeys[strings.ToLower(filepath.Base(f.Path))]
	if !ok {
		return // no single unique key in this file
	}

	label := strings.TrimSpace(f.Header.Field(spec.Col))
	if label == "" {
		label = fmt.Sprintf("column %d", spec.Col)
	}

	seen := map[string]int{}
	for _, row := range f.Rows {
		if spec.NewGated && !strings.EqualFold(row.Field(0), "NEW") {
			continue
		}
		if isSubHeader(f, row) {
			continue
		}
		key := strings.ToUpper(row.Field(spec.Col))
		if key == "" {
			continue
		}
		if prev, ok := seen[key]; ok {
			rep.Warnf(check, f.Path, row.Line,
				fmt.Sprintf("first defined at line %d", prev),
				"duplicate %s %q -- the loader deletes the earlier entry and keeps this one", label, key)
			continue
		}
		seen[key] = row.Line
	}
}

func trim(s string) string {
	if len(s) > 160 {
		return s[:160] + "..."
	}
	return s
}
