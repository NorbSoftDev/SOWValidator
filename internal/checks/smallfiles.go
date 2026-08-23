package checks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// gamefonts.csv and replroster.csv are both three or four columns of one
// thing, with a loader each and little to say, so they share a file.

// gamefonts.csv columns, in the order CFonts::Init (War3D/fonts.cpp:62) reads
// them.
const (
	gfName  = iota // read and thrown away
	gfID           // what the rest of the game names the font by
	gfFile         // a font file in Layout\, upper-cased and resolved on disk
	gfScale        // the size it is loaded at
	gfColumns
)

// AddFonts records the font ids.
func (n *NameSets) AddFonts(f *datacsv.File, short string) {
	addKeys(n.Font, f, gfID, short, true)
}

// GameFonts validates gamefonts.csv.
//
// The one thing to know about this loader is what a missing font file costs:
// it skips the row before building the definition, so the id is never created
// at all and everything naming it gets nothing. Two rows sharing a font file
// is not a fault -- the loaded face is cached by file, but each row still gets
// its own definition and its own scale.
func GameFonts(f *datacsv.File, assets *Assets, rep *report.Report) {
	const check = "gamefonts"

	seenID := map[string]int{}

	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		id := row.Field(gfID)
		if id == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"font has no ID -- nothing can name it")
			continue
		}
		if prev, dup := seenID[strings.ToUpper(id)]; dup {
			rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
				"duplicate font ID %q", id)
		} else {
			seenID[strings.ToUpper(id)] = row.Line
		}

		file := row.Field(gfFile)
		switch {
		case file == "":
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is blank -- the loader logs \"Font file not found\" and skips the row, so this font is never created", id, colLabel(f, gfFile, "Font"))
			continue
		case !assets.Has(DirFonts, file, ".pft"):
			rep.Errorf(check, f.Path, row.Line,
				"the engine logs \"Font file not found\" and skips the row",
				"%s: %s %q is not in the "+DirFonts+" folder of any loaded layer", id, colLabel(f, gfFile, "Font"), file)
		}

		if raw := row.Field(gfScale); raw != "" {
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				rep.Errorf(check, f.Path, row.Line, "",
					"%s: %s is %q, not a number -- the loader reads it as 0", id, colLabel(f, gfScale, "Scale"), raw)
			} else if v <= 0 {
				rep.Errorf(check, f.Path, row.Line, "",
					"%s: %s is %s -- the font is loaded at no size", id, colLabel(f, gfScale, "Scale"), raw)
			}
		}
	}
}

// replroster.csv columns, in the order CWorld::LoadNames
// (War3D/world.cpp:494) reads them.
//
// This is not a name table but a two-dimensional index: the loader resizes a
// vector of vectors to fit whatever side and army numbers it is given and
// pushes the name into that cell. The numbers are therefore allocation sizes,
// not labels.
const (
	rrSide = iota
	rrArmy
	rrName
	rrColumns
)

// rrSaneIndex is the point past which a side or army number stops looking like
// an index and starts looking like a typo. The loader resizes to fit whatever
// it is given, so a number with an extra digit quietly allocates that many
// empty rows.
const rrSaneIndex = 64

// ReplRoster validates replroster.csv.
func ReplRoster(f *datacsv.File, rep *report.Report) {
	const check = "replroster"

	if !checkLayout(f, replRosterLayout, check, rep) {
		return
	}

	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		// The loader drops a row whose side is below 1 without a word, so the
		// names on it are simply never used.
		side, sideOK := replIndex(f, row, rrSide, "Side", rep)
		if !sideOK {
			continue
		}
		if side < 1 {
			rep.Warnf(check, f.Path, row.Line, "",
				"%s is %d -- the loader skips any row below 1, so this name is never used",
				colLabel(f, rrSide, "Side"), side)
			continue
		}

		army, armyOK := replIndex(f, row, rrArmy, "Army", rep)
		if !armyOK {
			continue
		}
		if army < 0 {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s is %d -- the loader sizes an array to it", colLabel(f, rrArmy, "Army"), army)
			continue
		}

		for _, c := range []struct {
			v     int
			label string
		}{
			{side, colLabel(f, rrSide, "Side")},
			{army, colLabel(f, rrArmy, "Army")},
		} {
			if c.v > rrSaneIndex {
				rep.Warnf(check, f.Path, row.Line,
					"the loader resizes its table to fit whatever number it is given",
					"%s is %d -- if that is a typo the table is grown to match it and the name lands where nothing looks",
					c.label, c.v)
			}
		}

		if row.Field(rrName) == "" {
			rep.Warnf(check, f.Path, row.Line, "",
				"row has no name -- an empty one is added to side %d, army %d", side, army)
		}
	}
}

func replIndex(f *datacsv.File, row datacsv.Row, col int, fallback string, rep *report.Report) (int, bool) {
	raw := row.Field(col)
	if raw == "" {
		return 0, true // reads as 0, which the caller judges
	}
	if _, err := strconv.Atoi(raw); err != nil {
		rep.Errorf("replroster", f.Path, row.Line, "",
			"%s is %q, not a whole number -- the loader reads it as %d", colLabel(f, col, fallback), raw, datacsv.Atoi(raw))
		return datacsv.Atoi(raw), false
	}
	v, _ := strconv.Atoi(raw)
	return v, true
}
