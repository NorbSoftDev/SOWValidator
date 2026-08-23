package checks

import (
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// The other three files in a scenario folder: the objectives it is won by, the
// script that drives it, and the extra scenery it places.

// maplocations.csv columns, in the order CGame::Init (War3D/game.cpp:106)
// reads them. The position is a vector, which costs two columns.
const (
	mlName       = iota // a display tag, wrapped in #{} if it looks like one
	mlID                // upper-cased; what battlescript.csv refers to
	mlMajor             // "Major", or anything else for minor
	mlType              // "hold", or anything else for a waypoint
	mlValidForAI        //
	mlLocX
	mlLocY
	mlRadius
	mlNumMen
	mlPoints
	mlFatBonus
	mlMorBonus
	mlAmmoBonus
	mlOccMod
	mlBegTime
	mlEndTime
	mlInterval
	mlSprite1 // one or more sprites, running to the end of the line
	mlColumns
)

// battlescript.csv columns.
//
// The first column is either an event type or a time of day, told apart by
// whether it starts with a letter. The second names what the event is about:
// an objective for the three objective events, and a unit for every other.
const (
	bsWhen = iota
	bsID
	bsCommand
	bsFromID
	bsX
	bsZ
	bsWait
	bsColumns
)

// casfx.csv columns, in the order CTrees::Init (War3D/trees.cpp:294) reads
// them. This one opens with the sprite rather than the position.
const (
	cfSprite = iota
	cfLocX
	cfLocY
	cfDirX // the heading is a vector too, so it costs two columns
	cfDirY
	cfHeight
	cfMod1
	cfColumns = cfMod1 + casfxMods
)

// casfxMods is MAX_SPR_MODS: the overlay sprites laid over the base one.
const casfxMods = 2

// bsEventTypes are the event names gEvtType holds (War3D/events.cpp:24). A
// first column that is neither one of these nor a time is logged and the whole
// event thrown away.
var bsEventTypes = map[string]bool{
	"evttime": true, "evtgiveup": true, "evtcont": true, "evtseetarg": true,
	"evtcourier": true, "evtobjdone": true, "evtarrived": true, "evtdeath": true,
	"evtintrouble": true, "evtfighting": true, "evtgrade": true, "evtobjarmy1": true,
	"evtobjarmy2": true, "evtfailcheck": true, "evtiarrived": true, "evtran": true,
	"evtdisttarg": true,
}

// bsObjectiveEvents are the three events whose second column names an
// objective rather than a unit.
var bsObjectiveEvents = map[string]bool{
	"evtobjdone": true, "evtobjarmy1": true, "evtobjarmy2": true,
}

// MapLocations validates one scenario's maplocations.csv, returning the
// objective ids it defines so the battlescript can be checked against them.
func MapLocations(f *datacsv.File, sprites *SpriteSet, rep *report.Report) map[string]int {
	const check = "objectives"

	ids := map[string]int{}

	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		id := row.Field(mlID)
		if id == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"objective has no ID -- the battle script has no way to name it")
			continue
		}
		// An id deliberately repeats: an objective is written once per
		// scoring window, the same place worth different points between
		// different times of day, and the loader keeps every one of them. So a
		// repeat here is the file working as designed, not a mistake.
		if _, seen := ids[strings.ToUpper(id)]; !seen {
			ids[strings.ToUpper(id)] = row.Line
		}

		// Every sprite from the first one to the end of the line: the loader
		// keeps reading until the row runs out.
		for col := mlSprite1; col < row.Len(); col++ {
			ref := row.Field(col)
			if ref == "" {
				continue
			}
			if !sprites.Has(ref) {
				rep.Errorf("objectives-ref", f.Path, row.Line,
					"the engine logs \"Graphic not found\" and the objective is drawn with nothing",
					"%s: marker sprite %q is not defined in unitpack.csv, gfxpack.csv or gfx.csv", id, ref)
			}
		}

		checkObjectiveNumbers(f, row, id, rep)
	}

	if len(ids) == 0 {
		rep.Warnf(check, f.Path, 1, "",
			"this scenario has no objectives -- there is nothing on the map for either side to take")
	}
	return ids
}

func checkObjectiveNumbers(f *datacsv.File, row datacsv.Row, id string, rep *report.Report) {
	const check = "objectives"

	for _, c := range []struct {
		col      int
		fallback string
	}{
		{mlLocX, "loc x"}, {mlLocY, "loc z"}, {mlRadius, "radius"},
		{mlNumMen, "# of Men needed"}, {mlPoints, "Points"},
	} {
		raw := row.Field(c.col)
		if raw == "" {
			continue
		}
		if _, err := strconv.ParseFloat(raw, 64); err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %q, not a number -- the loader reads it as 0", id, colLabel(f, c.col, c.fallback), raw)
		}
	}

	// The radius is squared into a comparison distance, so at zero nothing is
	// ever inside the objective and it can never be taken.
	if raw := row.Field(mlRadius); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v <= 0 {
			rep.Warnf(check, f.Path, row.Line, "",
				"%s: %s is %g -- no unit is ever within it", id, colLabel(f, mlRadius, "radius"), v)
		}
	}
}

// BattleScript validates one scenario's battlescript.csv against the
// objectives beside it and the units its order of battle places.
//
// units may be nil, for a scenario whose order of battle could not be read; the
// unit references are then left unjudged rather than reported against a set
// that is missing them all.
func BattleScript(f *datacsv.File, objectives map[string]int, units map[string]int, rep *report.Report) {
	const check = "battlescript"

	// A file in the older layout has its unit and coordinate columns one place
	// out, so nothing below the first column can be judged. The event types
	// and times in that first column still can, and are.
	refsOK := checkLayout(f, battleScriptLayout, check, rep)

	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		when := strings.TrimSpace(row.Field(bsWhen))
		if when == "" {
			continue
		}

		kind := strings.ToLower(when)
		isEvent := len(when) > 0 && isLetter(when[0])

		if isEvent && !bsEventTypes[kind] {
			// The random event type is written with its argument stuck to it,
			// so it is matched by prefix rather than whole.
			if !strings.HasPrefix(kind, "evtran") {
				rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
					"the engine logs \"Could not find Event Type: %s\" and throws the whole event away", when)
				continue
			}
			kind = "evtran"
		}
		if !isEvent && !isClockTime(when) {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"%q is neither an event type nor a time of day -- the loader reads it as a time and gets 0, so the event fires as the battle opens", when)
		}

		if !refsOK {
			continue
		}
		checkBattleScriptRefs(f, row, kind, isEvent, objectives, units, rep)

		if cmd := row.Field(bsCommand); cmd != "" && !isCommand(cmd) {
			rep.Errorf(check, f.Path, row.Line, "",
				"command %q is not one the engine knows -- the event fires and does nothing", cmd)
		}
	}
}

func checkBattleScriptRefs(f *datacsv.File, row datacsv.Row, kind string, isEvent bool, objectives, units map[string]int, rep *report.Report) {
	const check = "battlescript-ref"

	id := row.Field(bsID)

	if isEvent && bsObjectiveEvents[kind] {
		if id == "" {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s names no objective -- the event watches nothing", kind)
			return
		}
		if _, ok := objectives[strings.ToUpper(id)]; !ok {
			rep.Errorf(check, f.Path, row.Line,
				"objectives come from this scenario's maplocations.csv",
				"%s names objective %q, which this scenario does not define", kind, id)
		}
		return
	}

	// Everything else names a unit, in this column and in the one the command
	// is sent from.
	if units == nil {
		return
	}
	for _, c := range []struct {
		col      int
		fallback string
	}{{bsID, "ID NAME"}, {bsFromID, "FromId"}} {
		ref := row.Field(c.col)
		if ref == "" {
			continue
		}
		if _, ok := units[strings.ToUpper(ref)]; !ok {
			rep.Errorf(check, f.Path, row.Line,
				"the engine logs \"Battlescript ERROR - unit not found\"",
				"%s %q is not a unit this scenario places", colLabel(f, c.col, c.fallback), ref)
		}
	}
}

// CaSfx validates one scenario's casfx.csv, the extra scenery it drops on the
// map. Every row here is one object, and the loader throws away any row whose
// sprite does not resolve.
func CaSfx(f *datacsv.File, sprites *SpriteSet, rep *report.Report) {
	const check = "casfx-ref"

	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		ref := row.Field(cfSprite)
		if ref == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"row names no sprite -- the loader drops it, so nothing is placed")
			continue
		}
		if !sprites.Has(ref) {
			rep.Errorf(check, f.Path, row.Line,
				"the loader drops the row when the sprite does not resolve, so nothing is placed",
				"sprite %q is not defined in unitpack.csv, gfxpack.csv or gfx.csv", ref)
			continue
		}

		for i := range casfxMods {
			mod := row.Field(cfMod1 + i)
			if mod == "" {
				continue
			}
			if !sprites.Has(mod) {
				rep.Errorf(check, f.Path, row.Line,
					"the engine logs \"Graphic not found\" and the overlay is left off",
					"overlay sprite %d %q is not defined in unitpack.csv, gfxpack.csv or gfx.csv", i+1, mod)
			}
		}
	}
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isClockTime reports whether a value reads as a time of day. The loader takes
// it apart on colons and reads whatever number it finds in each piece, so
// anything with a leading number in its first piece is accepted -- which is
// why a value that is not a time at all becomes hour zero.
func isClockTime(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	head := strings.SplitN(s, ":", 2)[0]
	head = strings.TrimSpace(head)
	if head == "" {
		return false
	}
	_, err := strconv.Atoi(head)
	return err == nil
}

// queueCommands are the two commands that carry another command after them.
// SComm::BuildCommand consumes one and goes round again, so a script can write
// "addcommqueue:moveto:..." and mean moveto.
var queueCommands = map[string]bool{"addcommqueue": true, "delcommqueue": true}

// isCommand reports whether a command string names something the engine can
// build.
//
// SComm::BuildCommand (War3D/commands.cpp:415) takes everything before the
// first colon as the name -- the rest is the command's arguments -- and then
// strips leading A and M flags off it, which FindCommand does again. Where
// this is unsure it answers true: a command wrongly called unknown would be
// reported against every script that uses it.
func isCommand(s string) bool {
	rest := strings.TrimSpace(s)

	for range 8 { // a chain longer than this is not a chain
		head := rest
		if i := strings.IndexByte(rest, ':'); i >= 0 {
			head, rest = rest[:i], rest[i+1:]
		} else {
			rest = ""
		}

		name := strings.ToLower(strings.TrimSpace(head))
		for {
			if queueCommands[name] {
				break // another command follows the colon
			}
			if commandNames[name] {
				return true
			}
			if len(name) > 2 && (name[0] == 'a' || name[0] == 'm') {
				name = name[1:]
				continue
			}
			return false
		}
		if rest == "" {
			return false
		}
	}
	return true
}
