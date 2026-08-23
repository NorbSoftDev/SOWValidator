package checks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// rifles.csv and artillery.csv are one format read by one loader
// (CWeapon::Init, War3D/weapon.cpp:70). It reads both files in turn into the
// same table, keyed on the same column, so an id used in both collides -- and
// then reads eleven more columns from an artillery row than from a rifle one.
//
// The two files therefore cannot be checked apart: the shared key set is the
// point, and the extra columns are what tells them apart.

// Columns shared by both files, in the order CWeapon::Init reads them.
// Every column past the ranges is given its position explicitly. Leaving them
// to iota would repeat the last expression rather than continue the count, and
// two of them silently landed on the same column.
const (
	wpName   = 0 // read and thrown away
	wpID     = 1 // the key, upper-cased
	wpAmmoID = 2 // artillery: a munitions.csv id; rifles: display text
	wpRange1 = 3 // five ranges, in yards

	wpMisfire = wpRange1 + wpRanges // 8
	wpROF     = wpMisfire + 1       // 9
	wpMROF    = wpROF + 1           // 10

	// Artillery only, from here down. A rifle row simply ends before them.
	wpAmmoPct1  = wpMROF + 1               // 11, four of them
	wpArtyTable = wpAmmoPct1 + wpArtyAmmo  // 15
	wpArtyAmmo1 = wpArtyTable + 1          // 16, four of them
	wpColumns   = wpArtyAmmo1 + wpArtyAmmo // 20
)

const (
	// wpRanges is eWeapMax: min, optimal, typical, long, max, in that order.
	wpRanges = 5
	// wpArtyAmmo is eAAMAX: canister, shell, shrapnel, solid, in that order.
	wpArtyAmmo = 4
)

// wpRangeNames are the five ranges in the order the loader stores them. They
// have to rise: the engine picks a band by comparing a distance against each
// in turn.
var wpRangeNames = [wpRanges]string{"min", "optimal", "typical", "long", "max"}

// wpArtyAmmoNames are the four artillery ammunition slots, in loader order.
var wpArtyAmmoNames = [wpArtyAmmo]string{"canister", "shell", "shrapnel", "solid"}

// AddWeapons records the weapon ids from rifles.csv or artillery.csv. Both go
// into one set because the loader puts them in one table.
func (n *NameSets) AddWeapons(f *datacsv.File, short string) {
	addKeys(n.Weapon, f, wpID, short, true)
}

// Weapons validates rifles.csv or artillery.csv. arty says which: an artillery
// row carries eleven columns a rifle row does not, and only those are read as
// ammunition percentages, an artillery table and four ammunition ids.
func Weapons(f *datacsv.File, arty bool, sets *NameSets, rep *report.Report) {
	const check = "weapon"

	kind := "rifle"
	if arty {
		kind = "artillery piece"
	}

	if arty && !checkLayout(f, artilleryLayout, check, rep) {
		return
	}

	seen := map[string]int{}
	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		id := row.Field(wpID)
		if id == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"%s has no ID -- it is keyed on the empty string, so it and every other ID-less row collapse into one entry", kind)
			continue
		}
		if prev, dup := seen[strings.ToUpper(id)]; dup {
			rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
				"duplicate ID %q -- the loader deletes the earlier %s and keeps this one", id, kind)
		} else {
			seen[strings.ToUpper(id)] = row.Line
		}

		checkWeaponRanges(f, row, id, rep)
		checkWeaponRates(f, row, id, rep)

		if !arty {
			continue
		}
		checkArtyAmmoPercents(f, row, id, rep)
		checkArtyRefs(f, row, id, sets, rep)
	}
}

// checkWeaponRanges validates the five range columns. The engine squares each
// of them into a comparison distance and picks a band by walking them in
// order, so a band that does not rise above the one before it can never be
// chosen.
func checkWeaponRanges(f *datacsv.File, row datacsv.Row, id string, rep *report.Report) {
	const check = "weapon"

	prev, prevOK := 0.0, false
	for i := range wpRanges {
		col := wpRange1 + i
		raw := row.Field(col)
		label := colLabel(f, col, wpRangeNames[i]+" range")

		if raw == "" {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is blank -- the loader reads it as 0", id, label)
			prevOK = false
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %q, not a number -- the loader reads it as 0", id, label, raw)
			prevOK = false
			continue
		}
		if v < 0 {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %s -- the engine squares it to compare distances, so a negative one behaves as its positive", id, label, raw)
		}
		if prevOK && v < prev {
			rep.Warnf(check, f.Path, row.Line,
				"the engine walks the five ranges in order -- min, optimal, typical, long, max",
				"%s: %s is %s, below the range before it -- the bands are out of order and this one is never chosen", id, label, raw)
		}
		prev, prevOK = v, true
	}
}

// checkWeaponRates validates the three timing columns. The reload is scaled by
// ten and rounded up; a rate of fire at zero means the weapon never completes
// a shot.
func checkWeaponRates(f *datacsv.File, row datacsv.Row, id string, rep *report.Report) {
	const check = "weapon"

	for _, c := range []struct {
		col   int
		label string
	}{
		{wpMisfire, colLabel(f, wpMisfire, "misfire factor")},
		{wpROF, colLabel(f, wpROF, "rate of fire")},
		{wpMROF, colLabel(f, wpMROF, "best rate of fire")},
	} {
		raw := row.Field(c.col)
		if raw == "" {
			continue // read as 0, which several of these legitimately are
		}
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %q, not a number -- the loader reads it as 0", id, c.label, raw)
		}
	}
}

// checkArtyAmmoPercents validates the four ammunition percentages. The loader
// divides each by 100 and the engine picks a round by them, so a row that adds
// up to nothing leaves the piece with no round to choose.
func checkArtyAmmoPercents(f *datacsv.File, row datacsv.Row, id string, rep *report.Report) {
	const check = "weapon"

	total, any := 0.0, false
	for i := range wpArtyAmmo {
		col := wpAmmoPct1 + i
		raw := row.Field(col)
		if raw == "" {
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s percentage is %q, not a number -- the loader reads it as 0", id, wpArtyAmmoNames[i], raw)
			continue
		}
		if v < 0 {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s percentage is %s", id, wpArtyAmmoNames[i], raw)
		}
		total += v
		any = true
	}
	if any && total == 0 {
		rep.Errorf(check, f.Path, row.Line, "",
			"%s: every ammunition percentage is 0 -- the piece is issued no rounds", id)
	}
}

// checkArtyRefs validates the artillery-only references: the fire table and
// the four ammunition ids, one per round type.
func checkArtyRefs(f *datacsv.File, row datacsv.Row, id string, sets *NameSets, rep *report.Report) {
	const check = "weapon-ref"

	if t := row.Field(wpArtyTable); t != "" {
		if unread := sets.Unread("artytables.csv"); unread != "" {
			// nothing to say: the table set is short of a whole file
		} else if _, ok := sets.ArtyTable[strings.ToUpper(t)]; !ok {
			rep.Errorf(check, f.Path, row.Line,
				"GetArtyTable returns -1, and the piece falls back to the first table in the file",
				"%s: %s %q is not defined in artytables.csv", id, colLabel(f, wpArtyTable, "ArtyTableID"), t)
		}
	}

	for i := range wpArtyAmmo {
		col := wpArtyAmmo1 + i
		ref := row.Field(col)
		if ref == "" {
			continue
		}
		if _, ok := sets.Ammo[strings.ToUpper(ref)]; !ok {
			rep.Errorf(check, f.Path, row.Line,
				"the engine logs \"Ammunition not found\" and the piece has no round in that slot",
				"%s: %s round %q is not defined in munitions.csv", id, wpArtyAmmoNames[i], ref)
		}
	}
}

// colLabel is the file's own name for a column, which is what a modder is
// looking at, falling back to a description of what the loader does with it.
func colLabel(f *datacsv.File, col int, fallback string) string {
	if l := strings.TrimSpace(f.Header.Field(col)); l != "" {
		return l
	}
	return fallback
}
