package checks

import (
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// Several files in the game are still shipped, by mods and by the base game
// alike, in a layout their loader no longer reads. A column was added or
// dropped at some point, and every value after it now sits one place from
// where it is looked for.
//
// Nothing said about the rows of such a file would be true, and saying it once
// per row buries the one finding that matters, so each is reported once
// against its header and the columns after the shift are left alone.

// Layout says where a known column has to sit for a file to be in the layout
// its loader reads.
type Layout struct {
	// Label is a header label that both layouts spell the same way, so that
	// where it lands is what tells them apart.
	Label string
	// Col is the column the loader reads it from.
	Col int
	// Was describes what the older layout did differently, so the message says
	// what to look for rather than only that something is wrong.
	Was string
	// Required marks a label the current layout always carries, so that its
	// absence is itself the older layout rather than a file with no header to
	// compare against. Set it only where the older layout drops the label
	// instead of moving it.
	Required bool
}

// checkLayout reports whether a file is in the layout its loader reads.
//
// A file whose header does not carry the label at all is taken at face value:
// there is nothing to compare, and refusing to check it would be worse than
// checking it against the current layout.
func checkLayout(f *datacsv.File, l Layout, check string, rep *report.Report) bool {
	at := labelColumn(f, l.Label)
	if at == l.Col {
		return true
	}
	if at < 0 {
		if !l.Required {
			return true
		}
		rep.Errorf(check, f.Path, 1, "header: "+trim(f.Header.Raw),
			"this file has no %s column, which the loader reads from column %d -- %s, so nothing in it is where it is read from",
			l.Label, l.Col, l.Was)
		return false
	}

	moved := "one place to the left of"
	if at > l.Col {
		moved = "one place to the right of"
	}
	if at > l.Col+1 || at < l.Col-1 {
		moved = "away from"
	}

	rep.Errorf(check, f.Path, 1, "header: "+trim(f.Header.Raw),
		"this file puts %s in column %d, but the loader reads it from column %d -- %s, so every value from there on sits %s the field it is read as",
		strings.TrimSpace(f.Header.Field(at)), at, l.Col, l.Was, moved)
	return false
}

// The layouts each file is checked against. Every Was clause is the difference
// actually found in shipped content, not a guess at what else might vary.
var (
	drillsLayout = Layout{
		Label: "rows", Col: drRows,
		Was: "an older drills.csv opened on the drill ID, with no Name column before it",
	}
	unitGlobalLayout = Layout{
		Label: "uniform1", Col: ugUniform1,
		Was: "an older unitglobal.csv carried two speeds rather than three, with no Mid Speed between Walk and Run",
	}
	sfxLayout = Layout{
		Label: "file", Col: sxFile,
		Was: "an older sfx.csv had no ID column, so its sounds are keyed on their .wav filenames",
	}
	oobLayout = Layout{
		Label: "weapon", Col: oobWeapon,
		Was: "many mods add an OOBMOD column after CLASS that nothing in the engine reads",
	}
	scenarioLayout = Layout{
		Label: "formation", Col: scFormation,
		Was: "an older scenario.csv had no BTN column after REG",
	}
	battleScriptLayout = Layout{
		Label: "xcoord", Col: bsX,
		Was: "an older battlescript.csv had no FromId column after Command",
	}
	// Only artillery needs this. An older rifles.csv is short of the same MROF
	// column, but nothing follows it there, so the loader reads it as 0 and
	// every other value is still where it looks for it.
	artilleryLayout = Layout{
		Label: "artytableid", Col: wpArtyTable,
		Was: "an older artillery.csv had no MROF column after the rate of fire",
	}
	screensLayout = Layout{
		Label: "xcoord", Col: srX,
		Was: "an older gscreens.csv and mscreens.csv had one fewer column between the font and the position",
	}
	replRosterLayout = Layout{
		Label: "side", Col: rrSide, Required: true,
		Was: "an older replroster.csv was a list of ranks and names rather than a side and army index",
	}
)
