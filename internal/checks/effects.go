package checks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// efx.csv columns, in the order CWorld::InitializeParticleTemplates
// (War3D/world.cpp:544) reads them.
const (
	fxName   = iota // read and thrown away
	fxID            // the key, upper-cased
	fxSprite        // resolved against the sprite set, as an effect
	fxChance
	fxGravity
	fxHeight
	fxOffset
	fxAlphaStart
	fxAlphaStep
	fxScaleStart
	fxScaleStep
	fxColumns
)

// fxGroup is how many effects the loader treats as one run. Within each run of
// four it accumulates the chance column, so the entries form a ladder rather
// than four independent odds.
const fxGroup = 4

// AddEffects records the effect ids.
func (n *NameSets) AddEffects(f *datacsv.File, short string) {
	addKeys(n.Effect, f, fxID, short, true)
}

// Effects validates efx.csv.
func Effects(f *datacsv.File, sprites *SpriteSet, rep *report.Report) {
	const check = "efx"

	seen := map[string]int{}
	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		id := row.Field(fxID)
		if id == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"effect has no ID -- nothing can name it, and the effect below it in the file takes its place in the order the engine indexes by")
			continue
		}
		if prev, dup := seen[strings.ToUpper(id)]; dup {
			rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
				"duplicate effect %q -- the loader replaces the earlier one in place, so the file's ordering is what survives, not both effects", id)
		} else {
			seen[strings.ToUpper(id)] = row.Line
		}

		// The sprite is not optional here: the loader logs a missing one by
		// name, because an effect with no sprite has nothing to draw.
		ref := row.Field(fxSprite)
		switch {
		case ref == "":
			rep.Errorf("efx-ref", f.Path, row.Line, "",
				"%s: %s is blank -- the effect has no sprite and draws nothing", id, colLabel(f, fxSprite, "SpriteID"))
		case !sprites.Has(ref):
			rep.Errorf("efx-ref", f.Path, row.Line,
				"the engine logs \"Effect file not found\"",
				"%s: %s %q is not defined in unitpack.csv, gfxpack.csv or gfx.csv",
				id, colLabel(f, fxSprite, "SpriteID"), ref)
		}

		checkEffectNumbers(f, row, id, rep)
	}
}

// efxNumeric describes a numeric column and the range the file's type row
// states for it. Unlike drills.csv, these ranges still match what the shipped
// data does, so where one is genuinely a bound it is worth checking.
var efxNumerics = []struct {
	col      int
	fallback string
	min, max float64
	bounded  bool
}{
	{fxChance, "Chance", 1, 100, true},
	{fxGravity, "Gravity", 0, 0, false},
	{fxHeight, "Height", 0, 0, false},
	{fxOffset, "Offset", 0, 0, false},
	{fxAlphaStart, "Alpha Start", 0, 255, true},
	{fxAlphaStep, "Alpha Step", 0, 0, false},
	{fxScaleStart, "Scale Start", 0, 0, false},
	{fxScaleStep, "Scale Step", 0, 0, false},
}

func checkEffectNumbers(f *datacsv.File, row datacsv.Row, id string, rep *report.Report) {
	const check = "efx"

	for _, n := range efxNumerics {
		raw := row.Field(n.col)
		if raw == "" {
			continue // read as 0, which is a usable default for all of these
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %q, not a number -- the loader reads it as 0", id, colLabel(f, n.col, n.fallback), raw)
			continue
		}
		// Alpha is written into a byte, so a value outside 0-255 is not a
		// matter of taste. The chance column is a percentage the loader
		// accumulates within each group of four.
		if n.bounded && (v < n.min || v > n.max) {
			rep.Warnf(check, f.Path, row.Line,
				fmt.Sprintf("the file's own type row gives this column as [%g/%g]", n.min, n.max),
				"%s: %s is %s, outside the stated range", id, colLabel(f, n.col, n.fallback), raw)
		}
	}
}
