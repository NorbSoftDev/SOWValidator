package checks

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// An order of battle is not one file. COOB::Init (War3D/oob.cpp:54) reads a
// scenario's scenario.csv, takes the file named on its MASTER row as the
// master order of battle out of OOBs\, and then splices the two together
// column by column: the ranks, position and condition come from the scenario,
// the identity, class, weapon and abilities from the master.
//
// So a scenario.csv row on its own is half a unit, and its ID is the join. An
// ID with no match in the master is logged and the unit is dropped -- it
// simply is not in the battle.

// Master order-of-battle columns, in the order the splice reads them.
const (
	oobName = iota // display name, read and thrown away
	oobID          // the key the scenario joins on
	oobName1
	oobName2
	oobSide
	oobArmy
	oobCorps
	oobDiv
	oobBgde
	oobReg
	oobClass     // a unitglobal.csv Class
	oobPortrait  // "(x-y)" into the portrait sheet, not a file name
	oobWeapon    // a rifles.csv or artillery.csv ID
	oobAmmo      //
	oobFlags     //
	oobFlag2     //
	oobFormation // a drills.csv ID
	oobHeadCount
	oobAbility
	oobCommand
	oobControl
	oobLeadership
	oobStyle
	oobExperience
	oobFatigue
	oobMorale
	oobColumns
)

// scenario.csv columns. The loader takes seven rank fields, then five for
// ammunition and position, then formation and head count, then fatigue and
// morale -- which is exactly the file's own header.
const (
	scName = iota // must not be blank, but is otherwise unread
	scID          // joins to the master order of battle
	scSide
	scArmy
	scCorps
	scDiv
	scBgde
	scReg
	scBtn
	scAmmo
	scDirX
	scDirZ
	scLocX
	scLocZ
	scFormation // a drills.csv ID, overriding the master's
	scHeadCount
	scFatigue
	scMorale
	scColumns
)

// OOBSet holds the unit ids each master order of battle defines, so a scenario
// can be checked against the one it names.
type OOBSet struct {
	// byFile is keyed on the file's lower-cased base name, as the scenario
	// names it, and holds each id upper-cased, as the loader stores it.
	byFile map[string]map[string]int
	// where remembers each file's path, for a finding that has to name it.
	where map[string]string
}

func NewOOBSet() *OOBSet {
	return &OOBSet{byFile: map[string]map[string]int{}, where: map[string]string{}}
}

// Add records one master order of battle. A later layer's file of the same
// name is read instead of the earlier one, so it replaces it here too.
//
// A file whose ID column is not where the loader reads it is registered with
// no ids at all rather than with the wrong ones: a scenario naming it then
// gets one finding saying so, instead of one per unit it places.
func (o *OOBSet) Add(f *datacsv.File) {
	base := strings.ToLower(filepath.Base(f.Path))
	o.where[base] = f.Path

	if !oobIDColumnOK(f) {
		o.byFile[base] = nil
		return
	}

	ids := map[string]int{}
	for _, row := range f.Rows {
		id := strings.ToUpper(row.Field(oobID))
		if id == "" {
			continue
		}
		if _, seen := ids[id]; !seen {
			ids[id] = row.Line
		}
	}
	o.byFile[base] = ids
}

// oobIDColumnOK reports whether the id is where the loader joins on it. The
// older layouts move it, and an id read from the wrong column is worse than no
// id at all.
func oobIDColumnOK(f *datacsv.File) bool {
	for _, label := range []string{"id", "idname"} {
		if at := labelColumn(f, label); at >= 0 {
			return at == oobID
		}
	}
	return true // no header to compare against
}

// Lookup finds a master order of battle by the name a scenario gives it, with
// or without its extension.
func (o *OOBSet) Lookup(name string) (map[string]int, string, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.TrimSuffix(n, ".csv") + ".csv"
	ids, ok := o.byFile[n]
	return ids, o.where[n], ok
}

// OOB validates one master order of battle.
func OOB(f *datacsv.File, sets *NameSets, rep *report.Report) {
	const check = "oob"

	// A file whose columns are not where the splice reads them still joins to
	// a scenario correctly -- the id is before the shift -- so its ids are
	// still worth having. Only the columns after it mean nothing.
	refsOK := checkLayout(f, oobLayout, "oob", rep)
	if !oobIDColumnOK(f) {
		return // nothing in this file is where it is read from
	}

	seen := map[string]int{}
	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		id := row.Field(oobID)
		if id == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"unit has no ID -- no scenario can name it, so it is never in a battle")
			continue
		}
		if prev, dup := seen[strings.ToUpper(id)]; dup {
			rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
				"the engine logs \"repeated id name\"; a scenario naming this id gets whichever row was read last")
			_ = prev
		} else {
			seen[strings.ToUpper(id)] = row.Line
		}

		if refsOK {
			checkOOBRefs(f, row, id, sets, rep)
		}
		checkOOBRanks(f, row, id, rep)
	}
}

// checkOOBRefs validates the three columns naming something in another file.
func checkOOBRefs(f *datacsv.File, row datacsv.Row, id string, sets *NameSets, rep *report.Report) {
	const check = "oob-ref"

	// A unit with no class has nothing to draw and no type at all.
	class := row.Field(oobClass)
	switch {
	case class == "":
		rep.Errorf(check, f.Path, row.Line, "",
			"%s: %s is blank -- the unit has no class, so nothing about how it looks or behaves is defined", id, colLabel(f, oobClass, "CLASS"))
	case sets.Unread("unitglobal.csv") == "":
		if _, ok := sets.Class[strings.ToUpper(class)]; !ok {
			rep.Errorf(check, f.Path, row.Line,
				"the engine logs \"Can't find unit class type\"",
				"%s: %s %q is not defined in unitglobal.csv", id, colLabel(f, oobClass, "CLASS"), class)
		}
	}

	// A blank weapon is normal: commanders carry none.
	if w := row.Field(oobWeapon); w != "" {
		if _, ok := sets.Weapon[strings.ToUpper(w)]; !ok {
			rep.Errorf(check, f.Path, row.Line,
				"the weapon is looked up in the one table rifles.csv and artillery.csv are read into",
				"%s: %s %q is not defined in rifles.csv or artillery.csv", id, colLabel(f, oobWeapon, "Weapon"), w)
		}
	}

	if d := row.Field(oobFormation); d != "" && sets.Unread("drills.csv") == "" {
		if _, ok := sets.Drill[strings.ToUpper(d)]; !ok {
			rep.Errorf(check, f.Path, row.Line,
				"the engine logs \"Formation not found\" and the unit starts in no formation",
				"%s: %s %q is not defined in drills.csv", id, colLabel(f, oobFormation, "Formation"), d)
		}
	}
}

// checkOOBRanks validates the six columns that place a unit in the chain of
// command. They are read as whole numbers, so anything else becomes 0 and the
// unit is attached at the top of whatever it should have hung below.
func checkOOBRanks(f *datacsv.File, row datacsv.Row, id string, rep *report.Report) {
	const check = "oob"

	for _, c := range []struct {
		col      int
		fallback string
	}{
		{oobSide, "SIDE"}, {oobArmy, "ARMY"}, {oobCorps, "CORPS"},
		{oobDiv, "DIV"}, {oobBgde, "BGDE"}, {oobReg, "REG"},
	} {
		raw := row.Field(c.col)
		if raw == "" {
			continue
		}
		if _, err := strconv.Atoi(raw); err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %q, not a whole number -- the loader reads it as %d",
				id, colLabel(f, c.col, c.fallback), raw, datacsv.Atoi(raw))
		}
	}
}

// Scenario validates one scenario.csv against the master order of battle it
// names.
func Scenario(f *datacsv.File, oobs *OOBSet, sets *NameSets, rep *report.Report) {
	const check = "scenario"

	if !checkLayout(f, scenarioLayout, check, rep) {
		return
	}

	master, masterLine := scenarioMaster(f)
	if master == "" {
		rep.Errorf(check, f.Path, 1, "",
			"this scenario has no MASTER row -- the loader finds no master order of battle to join its units to, and reads the file by a different path entirely")
		return
	}

	ids, where, ok := oobs.Lookup(master)
	if !ok {
		rep.Errorf(check, f.Path, masterLine,
			"the master order of battle is looked for in the OOBs folder of every loaded layer",
			"MASTER names %q, which is not there -- no unit in this scenario can be joined to a master entry", master)
		return
	}
	if ids == nil {
		rep.Errorf(check, f.Path, masterLine,
			fmt.Sprintf("see the finding against %s", rep.Rel(where)),
			"MASTER names %q, which is in a layout the loader no longer reads -- this scenario's units were not checked, because there is nothing trustworthy to check them against", master)
		return
	}

	seen := map[string]int{}
	for _, row := range f.Rows {
		if row.Line == masterLine || isSubHeader(f, row) {
			continue
		}
		if strings.EqualFold(row.Field(scName), "MASTER") {
			continue // a second MASTER row; the loader skips these outright
		}

		id := row.Field(scID)
		if id == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"row has no ID -- there is nothing to join it to the master order of battle, and the unit is dropped")
			continue
		}
		if _, found := ids[strings.ToUpper(id)]; !found {
			rep.Errorf(check, f.Path, row.Line,
				fmt.Sprintf("the master order of battle is %s", rep.Rel(where)),
				"the engine logs %q not found and leaves this unit out of the battle", id)
			continue
		}
		if prev, dup := seen[strings.ToUpper(id)]; dup {
			rep.Warnf(check, f.Path, row.Line, fmt.Sprintf("first placed at line %d", prev),
				"%s is placed twice -- both rows are joined to the same master entry, putting two of the unit on the field", id)
		} else {
			seen[strings.ToUpper(id)] = row.Line
		}

		checkScenarioRow(f, row, id, sets, rep)
	}

	if len(seen) == 0 {
		rep.Errorf(check, f.Path, 1, "",
			"this scenario places no units")
	}
}

// scenarioMaster finds the row naming the master order of battle. The loader
// takes the first one it meets and stops looking, so a second is never read.
func scenarioMaster(f *datacsv.File) (string, int) {
	for _, row := range f.Rows {
		if strings.EqualFold(row.Field(scName), "MASTER") {
			return row.Field(scID), row.Line
		}
	}
	return "", 0
}

// checkScenarioRow validates the columns the scenario contributes to the
// splice: where the unit stands, what formation it starts in, and what
// condition it is in.
func checkScenarioRow(f *datacsv.File, row datacsv.Row, id string, sets *NameSets, rep *report.Report) {
	const check = "scenario"

	// The file's notes say the name cannot be blank on a valid line. The
	// loader reads it and throws it away, so this costs nothing at runtime.
	if row.Field(scName) == "" {
		rep.Warnf(check, f.Path, row.Line, "", "%s: the name column is blank", id)
	}

	if d := row.Field(scFormation); d != "" && sets.Unread("drills.csv") == "" {
		if _, ok := sets.Drill[strings.ToUpper(d)]; !ok {
			rep.Errorf("scenario-ref", f.Path, row.Line,
				"the engine logs \"Formation not found\" and the unit starts in no formation",
				"%s: %s %q is not defined in drills.csv", id, colLabel(f, scFormation, "Formation"), d)
		}
	}

	for _, c := range []struct {
		col      int
		fallback string
	}{
		{scDirX, "dir x"}, {scDirZ, "dir z"},
		{scLocX, "loc x"}, {scLocZ, "loc z"},
		{scHeadCount, "Head Count"}, {scFatigue, "Fatigue"}, {scMorale, "Morale"},
	} {
		raw := row.Field(c.col)
		if raw == "" {
			continue
		}
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %q, not a number -- the loader reads it as 0",
				id, colLabel(f, c.col, c.fallback), raw)
		}
	}

	// A unit with no men is a unit that is on the field and does nothing.
	if raw := row.Field(scHeadCount); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v <= 0 {
			rep.Warnf(check, f.Path, row.Line, "",
				"%s: %s is %d", id, colLabel(f, scHeadCount, "Head Count"), v)
		}
	}
}

// ScenarioUnits returns the ids a scenario places, for the battle script beside
// it to be checked against. It returns nil when the scenario's master order of
// battle could not be found, so a caller can tell "no units" from "no way to
// tell", and leave the references alone rather than report every one of them.
func ScenarioUnits(f *datacsv.File, oobs *OOBSet) map[string]int {
	master, masterLine := scenarioMaster(f)
	if master == "" {
		return nil
	}
	if ids, _, ok := oobs.Lookup(master); !ok || ids == nil {
		return nil
	}

	ids := map[string]int{}
	for _, row := range f.Rows {
		if row.Line == masterLine {
			continue
		}
		if id := strings.ToUpper(row.Field(scID)); id != "" {
			ids[id] = row.Line
		}
	}
	return ids
}
