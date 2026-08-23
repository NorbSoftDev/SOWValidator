package checks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// munitions.csv and artytables.csv are read by one loader in one pass
// (CArtyAmmo::Init, War3D/artyammo.cpp:70), munitions first and then the
// tables, which is why they sit together here.

// munitions.csv columns, in the order the loader reads them.
const (
	amID     = iota // the key, upper-cased
	amSprite        // resolved against the sprite set, as an effect
	amSmoke
	amBurst
	amMinRange
	amMaxRange
	amKillProb
	amMaxKill
	amMissMorale
	amGunKill
	amAmmoKill
	amCanAngle
	amCanShots
	amSound
	amColumns
)

// artytables.csv columns.
const (
	atName       = iota // read and thrown away
	atTableID           // the table this row belongs to, upper-cased
	atExperience        // the row's slot within that table
	atCalculated        // read into the same variable as the two below it,
	atStress            // so only the last of the three survives
	atAdjusted
	atColumns
)

// atSlots is ATMULT (War3D/artyammo.cpp:25): the stride between one table's
// rows and the next in the flat map the loader builds. An experience value at
// or above it lands in the next table's space.
const atSlots = 100

// AddAmmo records the munition ids.
func (n *NameSets) AddAmmo(f *datacsv.File, short string) {
	addKeys(n.Ammo, f, amID, short, true)
}

// AddArtyTables records the artillery table ids. A table id repeats by design,
// once per experience level within it, and addKeys keeps the first of each.
func (n *NameSets) AddArtyTables(f *datacsv.File, short string) {
	addKeys(n.ArtyTable, f, atTableID, short, true)
}

// Munitions validates munitions.csv.
func Munitions(f *datacsv.File, sets *NameSets, sprites *SpriteSet, rep *report.Report) {
	const check = "munitions"

	seen := map[string]int{}
	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		id := row.Field(amID)
		if id == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"round has no name -- it is keyed on the empty string, so it and every other unnamed round collapse into one entry")
			continue
		}
		if prev, dup := seen[strings.ToUpper(id)]; dup {
			rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
				"duplicate round %q -- the loader deletes the earlier one and keeps this one", id)
		} else {
			seen[strings.ToUpper(id)] = row.Line
		}

		// Sprite -> a defined sprite, looked up as an effect.
		if ref := row.Field(amSprite); ref != "" && !sprites.Has(ref) {
			rep.Errorf("munitions-ref", f.Path, row.Line,
				"the engine logs \"Graphic not found\" and the round is drawn with nothing",
				"%s: %s %q is not defined in unitpack.csv, gfxpack.csv or gfx.csv",
				id, colLabel(f, amSprite, "sprite"), ref)
		}

		// Sound -> sfx.csv. A blank is normal; the round simply makes no noise.
		if ref := row.Field(amSound); ref != "" {
			if sets.Unread("sfx.csv") == "" {
				if _, ok := sets.Sfx[strings.ToUpper(ref)]; !ok {
					rep.Warnf("munitions-ref", f.Path, row.Line, "",
						"%s: %s %q is not defined in sfx.csv", id, colLabel(f, amSound, "sound"), ref)
				}
			}
		}

		checkMunitionRanges(f, row, id, rep)
	}
}

// checkMunitionRanges validates the two range columns. The engine squares both
// into comparison distances, so the order between them is what decides whether
// the round has any band it can be fired at.
func checkMunitionRanges(f *datacsv.File, row datacsv.Row, id string, rep *report.Report) {
	const check = "munitions"

	min, minOK := numField(f, row, amMinRange, id, "minimum range", rep)
	max, maxOK := numField(f, row, amMaxRange, id, "maximum range", rep)

	if minOK && maxOK && max > 0 && min > max {
		rep.Errorf(check, f.Path, row.Line, "",
			"%s: minimum range %g is above maximum range %g -- there is no distance the round can be fired at", id, min, max)
	}
	if maxOK && max == 0 {
		rep.Warnf(check, f.Path, row.Line, "",
			"%s: maximum range is 0 -- the round can only be fired at nothing", id)
	}
}

// numField reads a numeric column, reporting a value the loader would silently
// turn into 0. It returns false when there is nothing usable to compare.
func numField(f *datacsv.File, row datacsv.Row, col int, id, fallback string, rep *report.Report) (float64, bool) {
	raw := row.Field(col)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		rep.Errorf("munitions", f.Path, row.Line, "",
			"%s: %s is %q, not a number -- the loader reads it as 0", id, colLabel(f, col, fallback), raw)
		return 0, false
	}
	return v, true
}

// ArtyTables validates artytables.csv.
//
// The loader flattens this file into one map indexed by
// table-number × atSlots + experience, so the experience column is not a label
// but an offset, and one at or above atSlots writes into the next table.
func ArtyTables(f *datacsv.File, rep *report.Report) {
	const check = "artytables"

	// A table id repeats by design, once per experience level, so what has to
	// be unique is the pair.
	seen := map[string]int{}

	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		id := row.Field(atTableID)
		if id == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"row has no ArtyTableID -- it joins the table keyed on the empty string")
			continue
		}

		raw := row.Field(atExperience)
		exp, err := strconv.Atoi(raw)
		switch {
		case raw == "":
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: Experience is blank -- the loader reads it as 0 and this row takes the first slot of the table", id)
			exp = 0
		case err != nil:
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: Experience is %q, not a whole number -- the loader reads it as %d", id, raw, datacsv.Atoi(raw))
			exp = datacsv.Atoi(raw)
		case exp < 0:
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: Experience is %d -- it is an offset into the table, so a negative one lands in the table before it", id, exp)
		case exp >= atSlots:
			rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("each table holds %d slots", atSlots),
				"%s: Experience is %d -- it is an offset into the table, so this row lands in the next table's slots", id, exp)
		}

		key := fmt.Sprintf("%s/%d", strings.ToUpper(id), exp)
		if prev, dup := seen[key]; dup {
			rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first set at line %d", prev),
				"%s: experience %d is set twice -- the loader keeps this row and the earlier one is lost", id, exp)
		} else {
			seen[key] = row.Line
		}

		checkArtyTableValue(f, row, id, rep)
	}
}

// checkArtyTableValue validates the three value columns.
//
// Only the last of them reaches the table: the loader reads Calculated, Stress
// and Adjusted one after another into the same variable and stores what is
// left, so the first two are read and discarded. That is worth knowing, and
// worth reporting when Adjusted is the one left blank.
func checkArtyTableValue(f *datacsv.File, row datacsv.Row, id string, rep *report.Report) {
	const check = "artytables"

	raw := row.Field(atAdjusted)
	label := colLabel(f, atAdjusted, "Adjusted")

	if raw == "" {
		rep.Errorf(check, f.Path, row.Line,
			"the loader reads Calculated and Stress into the same variable first, so only this column reaches the table",
			"%s: %s is blank -- this table entry is 0 whatever the columns before it say", id, label)
		return
	}
	if _, err := strconv.ParseFloat(raw, 64); err != nil {
		rep.Errorf(check, f.Path, row.Line, "",
			"%s: %s is %q, not a number -- the loader reads it as 0", id, label, raw)
	}
}
