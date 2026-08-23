package checks

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// drills.csv is not a table, so nothing that treats it as one can say anything
// true about it. Each drill is a definition line followed by exactly Rows more
// lines holding its slot map, and the loader reads those inside a nested loop
// that never considers them definitions. They are written in a grammar of
// their own -- "(rowdist-coldist-sprite-facing-subform-subtype-lock)slot" --
// which the file documents in its own notes rows.
//
// Everything below is read off CForm::Init in War3D/form.cpp and off those
// notes rows. Where the loader accepts a value and then quietly does nothing
// with it, that is reported too: the file says one thing and the game does
// another, which is the failure this validator exists to catch.

// Definition columns, in the order CForm::Init parses them. The last two carry
// no header label in the file, but the loader reads them.
const (
	drName = iota
	drID
	drRows
	drCols
	drRowDist
	drColDist
	drSubForm
	drKeepForm
	drCanWheel
	drCanFight
	drMoveRateMod
	drAboutFace
	drArtyForm
	drMinEnemy
	drFireMod
	drMeleeMod
	drCantMove
	drCantCharge
	drCantFlank
	drCantCover
	drColumns
)

// Slot-map parameters, in the order the loader pulls them out of the
// parentheses. The notes rows document the first six; the loader reads a
// seventh into SForm::lock.
const (
	prRowDist = iota
	prColDist
	prSprite
	prFacing
	prSubForm
	prSubType
	prLock
	prCount
)

var paramNames = [prCount]string{
	"row distance", "column distance", "sprite", "facing", "subform", "subtype", "lock",
}

const (
	// maxMen is MAXMEN. The loader's grid is all[MAXMEN+1][MAXMEN+1] and every
	// SForm array is [MAXMEN+1], so Rows or Columns above maxMen-1 walks off
	// the grid, and a slot id above maxMen is thrown away.
	maxMen = 200
	// maxDocSlot and maxDocSprite are the limits the file's own notes state:
	// "1 - flagbearer, 2-125 men", and sprite "valid values are currently 1-6".
	maxDocSlot   = 125
	maxDocSprite = 6
	// maxSubType is the highest unit type those notes list: 1-Inf, 2-Cav, 3-Art.
	maxSubType = 3
)

// DrillRecord is one drill as the loader reads it: the definition line, and
// the lines after it that the loader spends on the slot map without ever
// looking at them as definitions.
type DrillRecord struct {
	Line   int      // 1-based line of the definition
	Raw    string   // the definition line
	Fields []string // split the way the loader splits it
	Rows   int      // declared Rows: how many lines of slot map follow
	Cols   int      // declared Columns: how many cells the loader reads per line
	MapAt  int      // 1-based line of the first slot-map line
	Map    []string // the slot-map lines, raw and in file order
	// Truncated is set when the file ended before Rows lines were there to
	// read. The loader's read loop simply stops, leaving the rest of the
	// drill's slots unset.
	Truncated bool
}

func (d DrillRecord) Field(i int) string {
	if i < 0 || i >= len(d.Fields) {
		return ""
	}
	return strings.TrimSpace(d.Fields[i])
}

// Label is what to call this drill in a message.
func (d DrillRecord) Label() string {
	if id := d.Field(drID); id != "" {
		return id
	}
	if n := d.Field(drName); n != "" {
		return n
	}
	return "(unnamed drill)"
}

// WalkDrills splits a drills.csv into its records, following the loader's own
// path through the file: skip blank and comma-led lines, read the next line as
// a definition, then consume the Rows lines after it whatever they look like.
//
// Rows is therefore what holds the file together. Get it wrong and every drill
// below it shifts, which is why the checks that follow lean on this walk
// rather than on the file's column shape.
func WalkDrills(f *datacsv.File) []DrillRecord {
	var out []DrillRecord

	n := f.LineCount()
	for i := 2; i <= n; i++ { // line 1 is the column header
		raw := f.RawLine(i)
		if raw == "" || raw[0] == ',' {
			continue
		}

		d := DrillRecord{Line: i, Raw: raw, Fields: datacsv.SplitLoader(raw), MapAt: i + 1}
		d.Rows = datacsv.Atoi(d.Field(drRows))
		d.Cols = datacsv.Atoi(d.Field(drCols))

		if d.Rows > 0 {
			for j := i + 1; j <= i+d.Rows; j++ {
				if j > n {
					d.Truncated = true
					break
				}
				d.Map = append(d.Map, f.RawLine(j))
			}
			i += d.Rows
		}
		out = append(out, d)
	}
	return out
}

// AddDrills records every drill id in f, so a SubForm or ArtyForm naming a
// drill defined further down its own file, or in another layer, still
// resolves. The engine resolves these only once every drills.csv has been
// read, so a forward reference is legal and must not be reported.
func (n *NameSets) AddDrills(f *datacsv.File, short string) {
	// A file in the older layout has no ID column where the loader looks for
	// one, so what the walk would collect from it is not drill ids at all.
	// Taking those into the set would let a genuinely dangling reference
	// resolve against a number.
	if at := labelColumn(f, drillsLayout.Label); at >= 0 && at != drillsLayout.Col {
		n.MarkUnread("drills.csv", short)
		return
	}
	for _, d := range WalkDrills(f) {
		id := strings.ToUpper(d.Field(drID))
		if id == "" {
			continue
		}
		if _, exists := n.Drill[id]; !exists {
			n.Drill[id] = fmt.Sprintf("%s:%d", short, d.Line)
		}
	}
}

// maxOutOfStep is how many stranded lines to list one by one before summing up
// the rest. A file whose whole layout is wrong strands every line in it, and
// the hundredth report of the same fault says nothing the first did not.
const maxOutOfStep = 5

// Drills validates one drills.csv.
func Drills(f *datacsv.File, sets *NameSets, rep *report.Report) {
	if !checkLayout(f, drillsLayout, "drill", rep) {
		return
	}

	seen := map[string]int{}
	var adrift []int

	for _, d := range WalkDrills(f) {
		// A line the walk reaches as a definition but which holds slot-map
		// data is a line the drill above it should have consumed and did not,
		// because its Rows is too small. The loader does not notice: it builds
		// a drill out of the line, with no ID and no men in it, and the men
		// written on the line are simply never placed.
		if !looksLikeDef(d.Fields) {
			adrift = append(adrift, d.Line)
			if len(adrift) <= maxOutOfStep {
				rep.Errorf("drill", f.Path, d.Line, "line: "+trim(d.Raw),
					"this line holds slot-map data but is read as a drill definition -- the Rows value of the drill above it is too small to reach it, so the men on it are never placed and a drill with no ID is made out of it instead")
			}
			continue
		}
		if !checkDrillDef(f, d, sets, seen, rep) {
			continue
		}
		checkDrillMap(f, d, sets, rep)
	}

	// The walk picks itself up at the next real definition, so the rest of the
	// file is still worth checking -- only the repetition is not worth
	// printing.
	if rest := adrift[min(len(adrift), maxOutOfStep):]; len(rest) > 0 {
		rep.Errorf("drill", f.Path, rest[0], "line(s) "+compactInts(rest),
			"%d further line(s) in this file are read as drill definitions but hold slot-map data", len(rest))
	}
}

// checkDrillDef validates a definition line, and returns false once it has
// found something that makes the rest of the record meaningless.
func checkDrillDef(f *datacsv.File, d DrillRecord, sets *NameSets, seen map[string]int, rep *report.Report) bool {
	const check = "drill"

	label := d.Label()

	// The notes row says the name cannot be blank. The loader reads it and
	// throws it away, so this costs nothing at runtime -- but a drill with no
	// name is a drill nobody can find again.
	if d.Field(drName) == "" {
		rep.Warnf(check, f.Path, d.Line, "", "%s: Name is blank", label)
	}

	id := d.Field(drID)
	if id == "" {
		rep.Errorf(check, f.Path, d.Line, "line: "+trim(d.Raw),
			"drill has no ID -- it is keyed on the empty string, so it and every other ID-less drill collapse into one entry")
		return false
	}
	if prev, dup := seen[strings.ToUpper(id)]; dup {
		rep.Errorf(check, f.Path, d.Line, fmt.Sprintf("first defined at line %d", prev),
			"duplicate drill ID %q -- the loader deletes the earlier drill and keeps this one", id)
	} else {
		seen[strings.ToUpper(id)] = d.Line
	}

	// Rows and Columns size the loader's fixed grid and decide where the next
	// drill starts, so they are checked before anything that leans on them.
	checkDrillExtent(f, d, drRows, "Rows", d.Rows, rep)
	checkDrillExtent(f, d, drCols, "Columns", d.Cols, rep)

	if d.Truncated {
		rep.Errorf(check, f.Path, d.Line, fmt.Sprintf("the slot map starts at line %d and runs past the end of the file", d.MapAt),
			"%s: declares Rows=%d but only %d line(s) of slot map follow -- the rest of its slots are never set",
			label, d.Rows, len(d.Map))
	}

	checkDrillNumbers(f, d, label, rep)
	checkDrillSubRef(f, d, drSubForm, "SubForm", sets, rep)
	checkDrillSubRef(f, d, drArtyForm, "ArtyForm", sets, rep)

	return true
}

// checkDrillExtent validates Rows or Columns. Both index a fixed grid the
// loader never bounds-checks: it logs a complaint about the size and then
// writes to the grid anyway.
func checkDrillExtent(f *datacsv.File, d DrillRecord, col int, label string, v int, rep *report.Report) {
	const check = "drill"

	raw := d.Field(col)
	if raw == "" {
		rep.Errorf(check, f.Path, d.Line, "",
			"%s: %s is blank -- the loader reads it as 0", d.Label(), label)
		return
	}
	if _, err := strconv.Atoi(raw); err != nil {
		rep.Errorf(check, f.Path, d.Line, "",
			"%s: %s is %q, not a whole number -- the loader reads it as %d", d.Label(), label, raw, v)
		return
	}
	if v < 0 {
		rep.Errorf(check, f.Path, d.Line, "",
			"%s: %s is %d -- the loader reads no slot map at all", d.Label(), label, v)
		return
	}
	if v > maxMen-1 {
		rep.Errorf(check, f.Path, d.Line, fmt.Sprintf("the loader's grid holds %d", maxMen-1),
			"%s: %s is %d -- the loader logs the size and then writes past the end of its grid",
			d.Label(), label, v)
	}
}

// drillNumeric describes a definition column the loader parses as a number,
// and how it parses it.
//
// The ranges on the file's type row are deliberately not enforced. They no
// longer describe the loader: it stores these in a 64-bit fixed-point type
// with no clamp anywhere, and the shipped data itself sits outside them --
// brigade drills carry a RowDist of 350+, and several carry an AboutFace of 2
// where the type row says [0/1] and the loader only ever asks whether it is
// above zero. Reporting a value the loader is perfectly happy with is how a
// validator teaches people to ignore it.
//
// What is left is what the loader really does to a value: read a number it
// cannot parse as 0, and truncate a fraction in a column it reads as a whole
// number.
type drillNumeric struct {
	col   int
	label string
	whole bool // read with MyAtoi, so a fraction is truncated
	plus  bool // a trailing '+' is meaningful here: the distance scales with unit size
}

var drillNumerics = []drillNumeric{
	{drRowDist, "RowDist", false, true},
	{drColDist, "ColDist", false, true},
	{drKeepForm, "KeepForm", true, false},
	{drCanWheel, "CanWheel", true, false},
	{drCanFight, "CanFight", true, false},
	{drMoveRateMod, "MoveRateMod", false, false},
	{drAboutFace, "AboutFace", true, false},
	{drMinEnemy, "MinEnemy", false, false},
	{drFireMod, "FireMod", true, false},
	{drMeleeMod, "MeleeMod", true, false},
	{drCantMove, "CantMove", true, false},
	{drCantCharge, "CantCharge", true, false},
	{drCantFlank, "CantFlank", true, false},
	{drCantCover, "CantCover", true, false},
}

func checkDrillNumbers(f *datacsv.File, d DrillRecord, label string, rep *report.Report) {
	const check = "drill"

	for _, n := range drillNumerics {
		raw := d.Field(n.col)
		if raw == "" {
			continue // an omitted value reads as 0, which is the default
		}
		s := raw
		if n.plus {
			s = strings.TrimSuffix(s, "+")
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			rep.Errorf(check, f.Path, d.Line, "",
				"%s: %s is %q, not a number -- the loader reads it as %d",
				label, n.label, raw, datacsv.Atoi(raw))
			continue
		}
		if n.whole && v != float64(int(v)) {
			rep.Warnf(check, f.Path, d.Line, "",
				"%s: %s is %q -- the loader reads a whole number here, so this becomes %d",
				label, n.label, raw, datacsv.Atoi(raw))
		}
	}
}

// checkDrillSubRef validates a column naming another drill. A blank is normal:
// CForm::GetIndex returns -1 for an empty name and the drill simply has no sub
// formation. A name that resolves to nothing gets the same -1, silently.
func checkDrillSubRef(f *datacsv.File, d DrillRecord, col int, label string, sets *NameSets, rep *report.Report) {
	const check = "drill-ref"

	name := d.Field(col)
	if name == "" {
		return
	}
	if _, ok := sets.Drill[strings.ToUpper(name)]; !ok {
		rep.Errorf(check, f.Path, d.Line, "",
			"%s: %s names drill %q, which is not defined -- the drill ends up with no sub formation",
			d.Label(), label, name)
	}
}

// checkDrillMap validates the slot map: the lines the loader reads cell by
// cell to place each man.
func checkDrillMap(f *datacsv.File, d DrillRecord, sets *NameSets, rep *report.Report) {
	const check = "drill-map"

	label := d.Label()
	slotAt := map[int]int{} // slot -> the line it was placed on
	maxSlot := 0

	for k, line := range d.Map {
		lineNo := d.MapAt + k

		// A slot-map line that reads as a definition means this drill's Rows
		// is too large and it is eating the drill below it.
		if line != "" && line[0] != ',' && looksLikeDef(datacsv.SplitLoader(line)) {
			rep.Errorf(check, f.Path, lineNo,
				fmt.Sprintf("read as slot map by %s at line %d, which declares Rows=%d", label, d.Line, d.Rows),
				"this line defines a drill but is read as slot-map data -- the drill it defines is never loaded")
			continue
		}

		cells, extra := drillCells(line, d.Cols)
		for _, c := range cells {
			checkDrillCell(f, d, c, lineNo, slotAt, &maxSlot, sets, rep)
		}
		// With Columns at zero the loader reads no cells at all, and the whole
		// line is "past" it. That is worth one finding about the drill, which
		// the men-placed check below makes, not one per line here.
		if d.Cols > 0 && strings.Trim(extra, ", ") != "" {
			rep.Warnf(check, f.Path, lineNo, "never read: "+trim(strings.Trim(extra, ",")),
				"%s: this line has cells past column %d -- the loader stops at Columns, so they are never read",
				label, d.Cols)
		}
	}

	if maxSlot < 2 {
		// SForm::Max loops "while (i >= imaxmen) i = i - imaxmen + 1", so an
		// imaxmen of 0 or 1 never terminates, and every slot accessor calls it.
		placed := "places no men"
		if maxSlot == 1 {
			placed = "places only the flag bearer"
		}
		rep.Errorf(check, f.Path, d.Line, "",
			"%s: the slot map %s -- the game hangs the first time it looks up a spot in this drill", label, placed)
		return
	}
	if _, ok := slotAt[1]; !ok {
		rep.Errorf(check, f.Path, d.Line, "",
			"%s: slot 1, the flag bearer, is never placed -- every other man is positioned relative to it", label)
	}

	var missing []int
	for s := 2; s <= maxSlot; s++ {
		if _, ok := slotAt[s]; !ok {
			missing = append(missing, s)
		}
	}
	if len(missing) > 0 {
		sort.Ints(missing)
		rep.Errorf(check, f.Path, d.Line, fmt.Sprintf("the highest slot placed is %d", maxSlot),
			"%s: slot(s) %s are never placed -- the loader logs each one and leaves those men without a spot",
			label, compactInts(missing))
	}
}

// drillCell is one cell of a slot map, as the loader reads it.
type drillCell struct {
	Col      int
	Text     string // the cell as written
	Slot     int    // the slot id, as the loader reads it
	SlotText string // the text the slot id was read from
	Empty    bool   // the loader took its empty-cell path and placed nobody
	Paren    bool
	Unclosed bool
	Params   []string
}

func checkDrillCell(f *datacsv.File, d DrillRecord, c drillCell, lineNo int, slotAt map[int]int, maxSlot *int, sets *NameSets, rep *report.Report) {
	const check = "drill-map"

	if c.Empty {
		return
	}
	where := fmt.Sprintf("%s, column %d of the slot map", d.Label(), c.Col+1)

	if c.Unclosed {
		rep.Errorf(check, f.Path, lineNo, "cell: "+trim(c.Text),
			"%s: '(' with no ')' -- the loader reads the rest of the line as this cell's parameters and places nobody after it", where)
		return
	}

	if c.Slot == 0 {
		// The loader only takes its empty-cell shortcut when the cell opens
		// with a comma. Anything else carrying no slot number falls through to
		// the placement code, which indexes the drill's arrays at slot-1.
		rep.Errorf(check, f.Path, lineNo, "cell: "+trim(c.Text),
			"%s: cell has no slot number -- the loader writes this cell's sprite, facing, subtype and lock one element before the start of the drill's arrays", where)
		return
	}
	if _, err := strconv.Atoi(c.SlotText); err != nil {
		rep.Errorf(check, f.Path, lineNo, "cell: "+trim(c.Text),
			"%s: slot id %q is not a whole number -- the loader reads it as %d", where, c.SlotText, c.Slot)
	}
	if c.Slot < 0 {
		rep.Errorf(check, f.Path, lineNo, "cell: "+trim(c.Text),
			"%s: slot id is %d -- the loader indexes the drill's arrays with it and writes outside them", where, c.Slot)
		return
	}
	if c.Slot > maxMen {
		rep.Errorf(check, f.Path, lineNo, fmt.Sprintf("the engine's limit is %d", maxMen),
			"%s: slot id is %d -- the loader drops this man", where, c.Slot)
		return
	}
	if c.Slot > maxDocSlot {
		rep.Warnf(check, f.Path, lineNo, "the file's notes give the slots as 1 for the flag bearer and 2-125 for men",
			"%s: slot id is %d, above the documented maximum of %d", where, c.Slot, maxDocSlot)
	}
	if prev, dup := slotAt[c.Slot]; dup {
		rep.Errorf(check, f.Path, lineNo, fmt.Sprintf("already placed at line %d", prev),
			"%s: slot %d is placed twice -- the loader logs it and keeps this position", where, c.Slot)
	} else {
		slotAt[c.Slot] = lineNo
	}
	if c.Slot > *maxSlot {
		*maxSlot = c.Slot
	}

	checkDrillParams(f, c, lineNo, where, sets, rep)
}

func checkDrillParams(f *datacsv.File, c drillCell, lineNo int, where string, sets *NameSets, rep *report.Report) {
	const check = "drill-map"

	if !c.Paren {
		return
	}
	for i, raw := range c.Params {
		if i >= prCount || raw == "" {
			continue
		}
		switch i {
		case prRowDist, prColDist:
			v, err := strconv.ParseFloat(strings.TrimSuffix(raw, "+"), 64)
			if err != nil {
				rep.Errorf(check, f.Path, lineNo, "cell: "+trim(c.Text),
					"%s: %s is %q, not a number -- the loader reads it as 0 and falls back to the drill's own spacing",
					where, paramNames[i], raw)
				continue
			}
			// The loader applies a per-slot distance only when it is above
			// zero -- "if (srow > 0) form->row[...] = srow" -- so a negative
			// one is parsed and then dropped on the floor.
			if v < 0 {
				rep.Warnf(check, f.Path, lineNo, "cell: "+trim(c.Text),
					"%s: %s is %s -- the loader only applies a distance above zero, so this man keeps the drill's own spacing",
					where, paramNames[i], raw)
			}
		case prSubForm:
			if _, ok := sets.Drill[strings.ToUpper(raw)]; !ok {
				rep.Errorf(check, f.Path, lineNo, "cell: "+trim(c.Text),
					"%s: subform names drill %q, which is not defined -- this man falls back to the drill's own sub formation",
					where, raw)
			}
		default:
			v, err := strconv.Atoi(raw)
			if err != nil {
				rep.Errorf(check, f.Path, lineNo, "cell: "+trim(c.Text),
					"%s: %s is %q, not a whole number -- the loader reads it as %d",
					where, paramNames[i], raw, datacsv.Atoi(raw))
				continue
			}
			switch i {
			case prSprite:
				if v < 0 || v > maxDocSprite {
					rep.Warnf(check, f.Path, lineNo, "the file's notes give the sprite as 0 for the default and 1-6 otherwise",
						"%s: sprite is %d, outside the documented range", where, v)
				}
			case prSubType:
				if v < 0 || v > maxSubType {
					rep.Warnf(check, f.Path, lineNo, "the file's notes give the subtype as 1-Inf, 2-Cav, 3-Art, or 0 for any",
						"%s: subtype is %d, outside the documented range", where, v)
				}
			}
		}
	}
}

// drillCells walks a slot-map line the way the loader's inner loop does: it
// takes cols cells off the front, one at a time, and stops. Whatever is left
// over comes back separately, because the loader never looks at it.
func drillCells(line string, cols int) (cells []drillCell, extra string) {
	s := line
	for j := 0; j < cols; j++ {
		var c drillCell
		c, s = nextDrillCell(s)
		c.Col = j
		cells = append(cells, c)
	}
	return cells, s
}

// nextDrillCell takes one cell off the front of s, mirroring the body of the
// loader's inner loop, and returns what is left.
func nextDrillCell(s string) (c drillCell, rest string) {
	// The loader's own test for an empty cell. Note how narrow it is: only a
	// cell that opens with the delimiter, or nothing at all, takes this path.
	// A cell holding a space does not.
	if s == "" || s[0] == ',' {
		c.Empty = true
		if i := strings.IndexByte(s, ','); i >= 0 {
			return c, s[i+1:]
		}
		return c, ""
	}

	start := s
	if s[0] == '(' {
		c.Paren = true
		s = s[1:]
		i := strings.IndexByte(s, ')')
		if i < 0 {
			// ParseGet with no closing delimiter to find takes the whole
			// remainder and leaves nothing behind, so the line ends here.
			c.Unclosed = true
			c.Params = strings.Split(s, "-")
			c.Text = strings.TrimSpace(start)
			return c, ""
		}
		c.Params = splitParams(s[:i])
		s = s[i+1:]
	}

	var field string
	if i := strings.IndexByte(s, ','); i >= 0 {
		field, s = s[:i], s[i+1:]
	} else {
		field, s = s, ""
	}
	c.SlotText = strings.TrimSpace(field)
	c.Slot = datacsv.Atoi(field)
	c.Text = strings.TrimSuffix(strings.TrimSpace(start[:len(start)-len(s)]), ",")
	return c, s
}

// splitParams splits the text inside the parentheses the way the loader's
// seven successive ParseGet calls do. The first two go through the CFixed
// parser, which reads a leading '-' as a sign rather than as the separator, so
// "-5-10" is -5 then 10 rather than an empty value then 5.
func splitParams(s string) []string {
	var out []string
	for s != "" && len(out) < prCount {
		neg := ""
		if (len(out) == prRowDist || len(out) == prColDist) && s[0] == '-' {
			neg, s = "-", s[1:]
		}
		i := strings.IndexByte(s, '-')
		if i < 0 || len(out) == prLock {
			out = append(out, neg+s)
			s = ""
			break
		}
		out = append(out, neg+s[:i])
		s = s[i+1:]
	}
	// Anything the loader would never have reached still tells the reader that
	// too much was written, so keep it as one trailing value.
	if s != "" {
		out = append(out, s)
	}
	return out
}

// looksLikeDef reports whether a line reads as a drill definition rather than
// as slot-map data. A definition opens with two text fields, a name and an id;
// a slot-map line holds numbers and parenthesised cells. Telling the two apart
// is how a wrong Rows value is caught, so this leans on the properties that
// actually differ rather than on anything about column counts.
func looksLikeDef(fields []string) bool {
	textish := func(i int) bool {
		if i >= len(fields) {
			return false
		}
		v := strings.TrimSpace(fields[i])
		if v == "" || v[0] == '(' {
			return false
		}
		_, err := strconv.ParseFloat(strings.TrimSuffix(v, "+"), 64)
		return err != nil
	}
	return textish(drName) && textish(drID)
}

// compactInts renders a slot list as ranges, so a drill missing forty
// consecutive men reads as one span instead of forty numbers.
func compactInts(v []int) string {
	var parts []string
	for i := 0; i < len(v); {
		j := i
		for j+1 < len(v) && v[j+1] == v[j]+1 {
			j++
		}
		switch {
		case j == i:
			parts = append(parts, strconv.Itoa(v[i]))
		case j == i+1:
			parts = append(parts, strconv.Itoa(v[i]), strconv.Itoa(v[j]))
		default:
			parts = append(parts, fmt.Sprintf("%d-%d", v[i], v[j]))
		}
		i = j + 1
	}
	if len(parts) > 12 {
		return strings.Join(parts[:12], ", ") + fmt.Sprintf(", ... (%d in all)", len(v))
	}
	return strings.Join(parts, ", ")
}
