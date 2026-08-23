package checks

import (
	"fmt"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/plist"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// Column layout of the packed-sprite CSVs, matching the order the engine
// reads the fields in.
const (
	colName = iota
	colFile
	colFirst
	colScale
	colAngles
	colFrames
	colTiming
	colLevitate
	colAction // unitpack.csv only
)

// Minimum column counts. unitpack.csv's trailing Action column is optional in
// practice -- most rows omit it and the engine reads the missing field as 0,
// which means "no action". Only the columns through Levitate are required.
const (
	unitpackCols = 8
	gfxpackCols  = 8
)

// PackSpec describes one packed-sprite CSV to validate.
type PackSpec struct {
	Name    string // file name, e.g. "unitpack.csv"
	MinCols int
}

var PackSpecs = []PackSpec{
	{Name: "unitpack.csv", MinCols: unitpackCols},
	{Name: "gfxpack.csv", MinCols: gfxpackCols},
}

// PackFrames validates that every packed-sprite row has all the frames it
// declares actually present in the .plist dictionaries.
//
// The engine computes need = Frames * Angles and checks that the pack holds
// at least need + begin - 1 frames, where begin is First - 1. It then reads
// frames begin through begin + need - 1 inclusive, whose top index is exactly
// need + begin - 1. The check is therefore one too permissive: a pack holding
// exactly that many frames passes and the engine reads one frame past the end
// of the pack. We require the correct bound, need + begin, and flag
// the boundary case explicitly.
func PackFrames(f *datacsv.File, spec PackSpec, ix *plist.Index, rep *report.Report) {
	const check = "pack-frames"

	for _, row := range f.Rows {
		name := row.Field(colName)
		if name == "" {
			continue
		}

		if row.Len() < spec.MinCols {
			rep.Errorf(check, f.Path, row.Line,
				fmt.Sprintf("row: %s", row.Raw),
				"%s: row has %d columns, expected %d -- the engine reads the rest of the line as the missing values instead of failing",
				name, row.Len(), spec.MinCols)
			continue
		}

		if strings.Contains(row.Raw, "\"") {
			rep.Warnf(check, f.Path, row.Line, "",
				"%s: row contains a quote character; the engine splits on raw commas and does not honour quoting", name)
		}

		file := strings.ToUpper(row.Field(colFile))
		first, firstOK := row.Int(colFirst)
		angles, anglesOK := row.Int(colAngles)
		frames, framesOK := row.Int(colFrames)

		switch {
		case !firstOK:
			rep.Errorf(check, f.Path, row.Line, "", "%s: First is not a valid integer (%q)", name, row.Field(colFirst))
			continue
		case !anglesOK:
			rep.Errorf(check, f.Path, row.Line, "", "%s: Angles is not a valid integer (%q)", name, row.Field(colAngles))
			continue
		case !framesOK:
			rep.Errorf(check, f.Path, row.Line, "", "%s: Frames is not a valid integer (%q)", name, row.Field(colFrames))
			continue
		}

		if angles <= 0 {
			rep.Errorf(check, f.Path, row.Line,
				"the engine divides 360 by the angle count to size each view",
				"%s: Angles is %d; must be >= 1", name, angles)
			continue
		}
		if frames <= 0 {
			rep.Errorf(check, f.Path, row.Line, "", "%s: Frames is %d; must be >= 1", name, frames)
			continue
		}
		if first <= 0 {
			rep.Errorf(check, f.Path, row.Line,
				"the engine subtracts 1 from First, so it must be 1-based and at least 1",
				"%s: First is %d; must be >= 1", name, first)
			continue
		}

		begin := first - 1 // the engine treats First as 1-based
		need := frames * angles

		pack, ok := ix.Get(file)
		if !ok {
			rep.Errorf(check, f.Path, row.Line,
				"engine logs: \"ERROR: unknown packed sprite\"",
				"%s: references packed sprite %q, which no .plist defines", name, file)
			continue
		}

		have := pack.Max() + 1
		if missing := pack.Missing(begin, need); len(missing) > 0 {
			detail := fmt.Sprintf("needs frames %d-%d (Frames %d x Angles %d = %d, starting at First %d)\n"+
				"%s defines %d frame(s), highest index %d\nmissing: %s",
				begin, begin+need-1, frames, angles, need, first,
				file, len(pack.Frames), pack.Max(), summarise(missing))

			// The exact off-by-one the engine lets through.
			if have == begin+need-1 && len(missing) == 1 && missing[0] == begin+need-1 {
				detail += "\n\nNOTE: this is exactly one frame short, which the engine's own" +
					"\nbounds check lets through -- it then reads frame " +
					fmt.Sprint(begin+need-1) + ", one past the end of the pack."
			}

			rep.Errorf(check, f.Path, row.Line, detail,
				"%s: packed sprite %s is short %d frame(s)", name, file, len(missing))
			continue
		}

		if gaps := pack.Gaps(); len(gaps) > 0 {
			reportGaps(rep, check, f, row.Line, name, file, pack, gaps, begin, need)
		}
	}
}

// reportGaps explains slots that no .plist defines for a pack. Either the pack
// simply starts numbering late -- the artist exported from 0065 onwards, say --
// or there are holes punched into the middle of its range. The engine sees
// neither, so spell out which slots are empty, what the pack therefore starts
// at, and why an empty slot is not harmless.
func reportGaps(rep *report.Report, check string, f *datacsv.File, line int,
	name, file string, pack *plist.Pack, gaps []int, begin, need int) {

	lowest := pack.Min()
	leadingOnly := len(gaps) == lowest // the gaps are exactly slots 0..lowest-1

	var msg string
	if leadingOnly {
		msg = fmt.Sprintf("%s: pack %s defines no sprites for slots %s, so its first sprite is slot %d",
			name, file, summarise(gaps), lowest)
	} else {
		msg = fmt.Sprintf("%s: pack %s defines no sprites for slots %s, which sit below its last slot %d",
			name, file, summarise(gaps), pack.Max())
	}

	var b strings.Builder
	for _, src := range pack.Files() {
		fmt.Fprintf(&b, "defined by: %s\n", rep.Rel(src))
	}
	if leadingOnly {
		fmt.Fprintf(&b, "Slots are 0-based and the .plist numbers its keys from 1, so the empty\n"+
			"slots are the keys %s%04d.png through %s%04d.png -- simply not in the .plist.\n",
			file, 1, file, lowest)
	} else {
		fmt.Fprintf(&b, "Slots are 0-based and the .plist numbers its keys from 1, so empty slot\n"+
			"%d is the key %s%04d.png.\n", gaps[0], file, gaps[0]+1)
	}
	fmt.Fprintf(&b, "This row itself is fine -- it reads slots %d-%d, which are all defined.\n",
		begin, begin+need-1)
	b.WriteString("The empty slots are still allocated, though: the engine sizes its frame\n" +
		"table from slot 0 and zero-fills it, then marks only the slots it loaded.\n" +
		"So they waste memory, and an empty slot reads back as a valid-looking texture\n" +
		"rather than a missing one -- any row pointing into the empty range silently\n" +
		"draws the wrong sprite instead of failing.")

	rep.Warnf(check, f.Path, line, b.String(), "%s", msg)
}

// summarise collapses a sorted int slice into compact ranges, capped so a
// wholly-missing pack does not print thousands of numbers.
func summarise(v []int) string {
	if len(v) == 0 {
		return "(none)"
	}
	var parts []string
	start, prev := v[0], v[0]
	flush := func() {
		if start == prev {
			parts = append(parts, fmt.Sprint(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, prev))
		}
	}
	for _, n := range v[1:] {
		if n == prev+1 {
			prev = n
			continue
		}
		flush()
		start, prev = n, n
	}
	flush()

	if len(parts) > 8 {
		return strings.Join(parts[:8], ", ") + fmt.Sprintf(", ... (%d ranges total)", len(parts))
	}
	return strings.Join(parts, ", ")
}
