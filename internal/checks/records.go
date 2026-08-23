package checks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// courier.csv, unitattributes.csv and statetables.csv are the three files
// where a row's meaning comes from the row above it rather than from its own
// columns. None of them is a table, and the shape of a column says nothing
// about what belongs in it.

// courier.csv columns, in the order CVars::LoadCourier (War3D/vars.cpp:235)
// reads them. A row reading NEW opens a list, named by its second column;
// every row after it is one entry on that list.
const (
	crType = iota
	crText // on a NEW row, the list's name; on any other, the entry's text
	crList // the list this entry opens, if it opens one
	crCommand
	crMessage
	crColumns
)

// courierEntryTypes are the three entry kinds the loader knows. A row that is
// none of them, and is not NEW, is skipped without a word.
var courierEntryTypes = map[string]bool{"menu": true, "item": true, "butt": true}

// Courier validates courier.csv, the tree of menus a courier message is built
// from.
func Courier(f *datacsv.File, rep *report.Report) {
	const check = "courier"

	// Two passes: the sub-list a menu opens is often defined further down the
	// file, and the loader resolves these only once the whole file is read.
	lists := map[string]int{}
	for _, row := range f.Rows {
		if strings.EqualFold(row.Field(crType), "NEW") {
			if id := row.Field(crText); id != "" {
				if _, seen := lists[strings.ToUpper(id)]; !seen {
					lists[strings.ToUpper(id)] = row.Line
				}
			}
		}
	}

	open := ""
	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		kind := strings.ToLower(row.Field(crType))
		if kind == "" {
			continue
		}

		if kind == "new" {
			id := row.Field(crText)
			if id == "" {
				rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
					"list has no name -- it is keyed on the empty string, so nothing can open it")
				open = ""
				continue
			}
			if prev := lists[strings.ToUpper(id)]; prev != row.Line {
				rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
					"duplicate list %q -- the loader replaces the earlier one, and every menu opening it reaches this list instead", id)
			}
			open = id
			continue
		}

		if !courierEntryTypes[kind] {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"%q is not NEW, MENU, ITEM or BUTT -- the loader skips the row without a word, so this entry is simply not on the menu",
				row.Field(crType))
			continue
		}

		if open == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"the engine logs \"Bad list definitions in courier.csv file\" and drops this entry -- it comes before any NEW row, so there is no list for it to be on")
			continue
		}

		// The sub-list is what makes this a tree. Picking an entry either
		// opens another list -- one this file defines, or one the engine
		// builds itself -- or runs a command, which CScrListMenu builds
		// straight out of this column (War3D/scrobj.cpp:3032). Anything else
		// is a menu the player can pick that does nothing at all.
		if sub := row.Field(crList); sub != "" {
			_, ok := lists[strings.ToUpper(sub)]
			if !ok && !builtInLists[strings.ToLower(sub)] && !isCommand(sub) {
				rep.Errorf("courier-ref", f.Path, row.Line, "",
					"%s: %s %q is not a list this file defines, one the engine builds, or a command it knows -- picking this entry does nothing",
					open, colLabel(f, crList, "Sub List"), sub)
			}
		}

		if cmd := row.Field(crCommand); cmd != "" && !isCommand(cmd) {
			rep.Warnf(check, f.Path, row.Line, "",
				"%s: command %q is not one the engine knows", open, cmd)
		}
	}
}

// unitattributes.csv columns. A row starting NEW opens an attribute table,
// named by its second column; every row after it is one experience level
// within that table, and the loader throws away a table that got none.
const (
	uaKind   = iota // "NEW", or unread on a level row
	uaPoints        // on a NEW row, the table's name; on a level row, its threshold
	uaName
	uaMod1
	uaColumns = uaMod1 + uaGameMods
)

// uaGameMods is eGameMax: the modifiers each experience level carries, from
// the fallback chance through to the most ammunition a unit will hand over.
const uaGameMods = 16

// UnitAttributes validates unitattributes.csv.
func UnitAttributes(f *datacsv.File, rep *report.Report) {
	const check = "unitattributes"

	// A file with no NEW row in it at all is in the older layout, where a
	// table was opened by writing its name in the first column. The loader
	// reads none of it into any table, which is one thing to say about the
	// file rather than one about each of its rows.
	if !hasUnitAttributeTable(f) {
		rep.Errorf(check, f.Path, 1, "",
			"this file has no NEW row -- the loader opens an attribute table only on one of those, so nothing in this file is read into a table at all")
		return
	}

	tables := map[string]int{}
	open := ""
	levels := 0
	openLine := 0

	closeTable := func() {
		if open != "" && levels == 0 {
			rep.Errorf(check, f.Path, openLine, "",
				"%s has no experience levels under it -- the loader throws the whole table away", open)
		}
	}

	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		// The loader tests the start of the raw line, not a trimmed field.
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(row.Raw)), "NEW") {
			closeTable()

			name := row.Field(uaPoints)
			if name == "" {
				rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
					"attribute table has no name -- nothing can look it up")
			} else if prev, dup := tables[strings.ToUpper(name)]; dup {
				rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
					"duplicate attribute table %q -- the loader replaces the earlier one in place", name)
			} else {
				tables[strings.ToUpper(name)] = row.Line
			}

			open, openLine, levels = name, row.Line, 0
			continue
		}

		if open == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"this level comes before any NEW row -- the loader reads it into whichever table it last had, and into none at all on the first row of the file")
			continue
		}
		levels++

		// The second column is only sometimes a threshold. CLevel::Level walks
		// the levels and takes the first whose value is at or above the unit's
		// points, which needs them to rise -- but other tables here are looked
		// up by index instead, and hold a value rather than a threshold. The
		// Weather table is one: its column is a visibility in yards. There is
		// no way to tell the two apart from the file, so the order is not
		// checked and only a value the loader cannot read is reported.
		raw := row.Field(uaPoints)
		if raw == "" {
			continue
		}
		if _, err := strconv.Atoi(raw); err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %q, not a whole number -- the loader reads it as %d",
				open, colLabel(f, uaPoints, "points"), raw, datacsv.Atoi(raw))
		}
	}
	closeTable()
}

// hasUnitAttributeTable reports whether any row opens a table.
func hasUnitAttributeTable(f *datacsv.File) bool {
	for _, row := range f.Rows {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(row.Raw)), "NEW") {
			return true
		}
	}
	return false
}

// statetables.csv holds three tables of numbers, told apart by a row naming
// one. A blank or comma-led line closes whichever is open, and the next
// non-blank row has to name the next -- CTables::InitStateTables
// (War3D/tables.cpp:166) keeps trying to match one until it does, so rows
// between a blank line and a name are read as failed attempts at a name and
// thrown away.
var stateTableNames = map[string]bool{
	"tablefatigue": true, "tableelevation": true, "tablemelee": true,
}

// StateTables validates statetables.csv.
func StateTables(f *datacsv.File, rep *report.Report) {
	const check = "statetables"

	// The row filter drops exactly the blank and comma-led lines that close a
	// table, so the raw lines are what shows the structure.
	open := ""
	seen := map[string]int{}
	stray := 0
	strayFrom := 0

	for i := 2; i <= f.LineCount(); i++ {
		raw := f.RawLine(i)
		if raw == "" || raw[0] == ',' {
			open = ""
			continue
		}

		first := strings.ToLower(strings.TrimSpace(datacsv.SplitLoader(raw)[0]))

		if open == "" {
			if stateTableNames[first] {
				if prev, dup := seen[first]; dup {
					rep.Errorf(check, f.Path, i, fmt.Sprintf("first opened at line %d", prev),
						"%s is opened twice -- the loader appends to the table rather than replacing it, so the rows of both runs end up in one", first)
				} else {
					seen[first] = i
				}
				open = first
				continue
			}
			// Not a table name, and no table open: the loader reads the row
			// only to see whether it names one, and moves on.
			if stray == 0 {
				strayFrom = i
			}
			stray++
			continue
		}
	}

	if stray > 0 {
		rep.Errorf(check, f.Path, strayFrom, fmt.Sprintf("%d line(s) in all", stray),
			"line(s) here sit between a blank line and the next table name, so the loader reads them only to see whether they name a table and then discards them")
	}
	for name := range stateTableNames {
		if _, ok := seen[name]; !ok {
			rep.Warnf(check, f.Path, 1, "",
				"this file defines no %s -- the engine looks the table up anyway and finds it empty", name)
		}
	}
}
