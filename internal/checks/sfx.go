package checks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// sfx.csv columns, in the order CSounds::Init (War3D/sounds.cpp:150) reads
// them. It reads past the Name column and keys on the ID after it, which is
// what tells the current layout from the one that had no ID column.
const (
	sxName = iota // read and thrown away
	sxID          // the key, upper-cased
	sxFile        // a .wav in Sounds\, resolved on disk
	sxMinDist
	sxMaxDist
	sxLoop
	sxVolume
	sxMusic
	sxColumns
)

// Sfx validates sfx.csv.
//
// The layout guard lives in AddSfx rather than here, because a file in the
// older layout must be kept out of the name set before anything resolves
// against it.
func Sfx(f *datacsv.File, sets *NameSets, assets *Assets, rep *report.Report) {
	const check = "sfx"

	if labelColumn(f, sfxLayout.Label) != sfxLayout.Col {
		return // already reported, and the columns below mean nothing
	}

	seen := map[string]int{}
	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		id := row.Field(sxID)
		if id == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"sound has no ID -- it is keyed on the empty string, so it and every other ID-less sound collapse into one entry, and nothing can name it")
			continue
		}
		if prev, dup := seen[strings.ToUpper(id)]; dup {
			rep.Warnf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
				"duplicate ID %q -- the loader deletes the earlier sound and keeps this one", id)
		} else {
			seen[strings.ToUpper(id)] = row.Line
		}

		// The .wav. A missing one is silent in both senses: the loader stores
		// an empty path and says nothing about it.
		wav := row.Field(sxFile)
		switch {
		case wav == "":
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is blank -- the sound has no file and never plays", id, colLabel(f, sxFile, "File"))
		case !assets.Has(DirSounds, wav, ".wav"):
			rep.Errorf(check, f.Path, row.Line,
				"the loader stores an empty path and logs nothing, so the sound is simply never heard",
				"%s: %s %q is not in the Sounds folder of any loaded layer", id, colLabel(f, sxFile, "File"), wav)
		}

		checkSfxDistances(f, row, id, rep)
	}
}

// checkSfxDistances validates the two audible distances. The engine squares the
// maximum into a comparison distance and fades between the two, so a maximum
// below the minimum leaves the sound with no distance it can be heard at.
func checkSfxDistances(f *datacsv.File, row datacsv.Row, id string, rep *report.Report) {
	const check = "sfx"

	read := func(col int, fallback string) (float64, bool) {
		raw := row.Field(col)
		if raw == "" {
			return 0, false
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %q, not a number -- the loader reads it as 0", id, colLabel(f, col, fallback), raw)
			return 0, false
		}
		return v, true
	}

	min, minOK := read(sxMinDist, "MinDist")
	max, maxOK := read(sxMaxDist, "MaxDist")

	if maxOK && max <= 0 {
		rep.Warnf(check, f.Path, row.Line, "",
			"%s: %s is %g -- there is no distance at which the sound is audible", id, colLabel(f, sxMaxDist, "MaxDist"), max)
		return
	}
	if minOK && maxOK && min > max {
		rep.Errorf(check, f.Path, row.Line, "",
			"%s: %s %g is above %s %g -- the sound fades between the two, so it is never heard",
			id, colLabel(f, sxMinDist, "MinDist"), min, colLabel(f, sxMaxDist, "MaxDist"), max)
	}
}
