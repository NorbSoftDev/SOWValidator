package checks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// A map's .csv is not one table but several, laid end to end and separated by
// a line reading "TERRAIN TABLE <NAME>". CUtil::GetTable (Shared/util.cpp:801)
// pulls out one section at a time by scanning for its opening line and taking
// everything up to the next one, and each consumer then skips that section's
// first line as a sub-header of its own.
//
// The sections have nothing in common with each other -- different widths,
// different meanings, different files referenced -- so a single column shape
// for the file does not exist.

// Map section names, as the engine spells them in War3D/defines.h.
const (
	mapSectionPrefix = "TERRAIN TABLE "
	mapBrush         = "BRUSH"
	mapSounds        = "SOUNDS"
	mapObjectives    = "OBJECTIVES"
	mapForts         = "FORTS"
	mapStarts        = "STARTSPointS"
	mapFiringLine    = "FIRING LINE"
)

// TERRAIN TABLE BRUSH columns, in the order CWorld::Init (War3D/world.cpp:246)
// reads them.
const (
	brName    = iota // the terrain's display name
	brVal            // its greyscale value, and its index into the ground table
	brMoveMod        // read into the y of a vector, as the movement rate modifier
	brDensity
	brVisibility // read into the x of that vector
	brHeight     // in feet; the loader divides by three for yards
	brDefBonus
	brFatigue
	brWall
	brNoFade
	brNoThin
	brLevel
	brSprite1 // six sprite names, one per fill slot
	brColumns = brSprite1 + mapFillSprites
)

// TERRAIN TABLE SOUNDS columns, in the order STree::Init (War3D/trees.cpp:107)
// reads them. The first four are two vectors: ParseGet reads a vector as two
// fields, an x and a y, so a location costs two columns and so does a heading.
const (
	snLocX = iota
	snLocY
	snDirX
	snDirY
	snSprite  // a terrain sprite
	snSound   // an sfx id
	snTerrain // a ground value, for fort smoke
	snHeight
	snStartLoc
	snID // names a town, when set
	snColumns
)

// TERRAIN TABLE OBJECTIVES columns.
const (
	obName = iota
	obX
	obY
	obColumns
)

// TERRAIN TABLE FORTS columns. A row either opens a fort, or is one firing
// line belonging to the fort above it, told apart by its first column.
const (
	ftName = iota
	ftVal
	ftOffset
	ftDefBonus
	ftMidX
	ftMidY
	ftColumns
)

// A FIRING LINE row means something else entirely from its second column on.
const (
	flGround = iota + 1
	flMaxMen
	flPosX
	flPosY
	flDirX
	flDirY
	flShootX
	flShootY
	flMaxDamage
	flColumns
)

const (
	// mapGroundSlots is how many entries the ground table holds. The loader
	// resizes to exactly this and then indexes it by the greyscale value, so a
	// value outside it writes past the end of the vector.
	mapGroundSlots = 256
	// mapFillSprites is MAXFILL (Shared/land.h:156): the sprite slots each
	// terrain can scatter over itself.
	mapFillSprites = 6
	// mapDeadVal and mapPathVal are the two greyscale values the engine claims
	// for itself after the file is read.
	mapDeadVal = 0
	mapPathVal = 190
)

// MapFile is a map's .csv, split into the sections the engine reads it as.
type MapFile struct {
	File     *datacsv.File
	Sections map[string]MapSection
}

// MapSection is one table within a map file: the lines between its opening
// line and the next one.
type MapSection struct {
	Name string
	// Line is where the section's opening line sits, for a finding about the
	// section as a whole.
	Line int
	// Rows are the section's lines with the sub-header and the lines the
	// consumers skip already dropped, so a row here is a row the engine reads.
	Rows []datacsv.Row
}

// SplitMap divides a map file into its sections.
//
// This cannot go through datacsv's row filter, because a section is delimited
// by position rather than by content: the consumers skip the first line of
// each section whatever it holds, and drop blank and comma-led lines only
// after that.
func SplitMap(f *datacsv.File) *MapFile {
	m := &MapFile{File: f, Sections: map[string]MapSection{}}

	var cur *MapSection
	first := false

	for i := 1; i <= f.LineCount(); i++ {
		raw := f.RawLine(i)

		if name, ok := mapSectionName(raw); ok {
			if cur != nil {
				m.Sections[cur.Name] = *cur
			}
			cur = &MapSection{Name: name, Line: i}
			first = true
			continue
		}
		if cur == nil {
			continue // preamble, before any section opens
		}
		if first {
			first = false // the section's own sub-header
			continue
		}
		if raw == "" || raw[0] == ',' {
			continue
		}
		cur.Rows = append(cur.Rows, datacsv.Row{Line: i, Fields: datacsv.SplitLoader(raw), Raw: raw})
	}
	if cur != nil {
		m.Sections[cur.Name] = *cur
	}
	return m
}

// mapSectionName reports whether a line opens a section, and which.
func mapSectionName(raw string) (string, bool) {
	if !strings.HasPrefix(strings.ToUpper(raw), mapSectionPrefix) {
		return "", false
	}
	rest := strings.TrimPrefix(strings.ToUpper(raw), mapSectionPrefix)
	// The opening line is padded out with commas like every other row.
	name := strings.TrimSpace(strings.Split(rest, ",")[0])
	return name, name != ""
}

// Maps validates one map's .csv.
func Maps(f *datacsv.File, sets *NameSets, sprites *SpriteSet, rep *report.Report) {
	const check = "map"

	m := SplitMap(f)

	brush, ok := m.Sections[mapBrush]
	if !ok {
		rep.Errorf(check, f.Path, 1, "",
			"this map has no %s%s section -- it has no terrain table at all, and every greyscale value on its bitmap reads as nothing",
			mapSectionPrefix, mapBrush)
		return
	}

	// The ground table is one table, not one per section: the brush rows and
	// the fort rows both write into gLand.m_ground[val] (War3D/world.cpp:311
	// and :428), the forts merely tagged SGFort. So the forts have to be read
	// into it before anything resolves a value against it -- the sounds
	// section names a fort's own greyscale, and reading it after would report
	// every fort in the game as undefined terrain.
	grounds := checkMapBrush(f, brush, sprites, rep)
	checkMapForts(f, m.Sections[mapForts], grounds, rep)
	checkMapSounds(f, m.Sections[mapSounds], grounds, sets, sprites, rep)
	checkMapObjectives(f, m.Sections[mapObjectives], rep)
}

// checkMapBrush validates the terrain table and returns the greyscale values it
// defines, which the sections after it index by.
func checkMapBrush(f *datacsv.File, s MapSection, sprites *SpriteSet, rep *report.Report) map[int]int {
	const check = "map"

	grounds := map[int]int{}

	for _, row := range s.Rows {
		name := row.Field(brName)
		if name == "" {
			name = "(unnamed terrain)"
		}

		raw := row.Field(brVal)
		val, err := strconv.Atoi(raw)
		switch {
		case raw == "":
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: greyscale value is blank -- the loader reads it as 0, which the engine claims for impassable ground", name)
			continue
		case err != nil:
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: greyscale value is %q, not a whole number -- the loader reads it as %d", name, raw, datacsv.Atoi(raw))
			continue
		case val < 0 || val >= mapGroundSlots:
			rep.Errorf(check, f.Path, row.Line,
				fmt.Sprintf("the ground table holds %d entries, one per greyscale value", mapGroundSlots),
				"%s: greyscale value is %d -- the loader stores the terrain at that index without checking it, writing past the end of the table",
				name, val)
			continue
		}

		if prev, dup := grounds[val]; dup {
			rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
				"%s: greyscale value %d is already used -- both terrains share one slot in the table, and this row takes it", name, val)
		} else {
			grounds[val] = row.Line
		}

		if val == mapDeadVal {
			rep.Warnf(check, f.Path, row.Line,
				"the loader overwrites this entry after reading the file, keeping only its impassable flag",
				"%s: greyscale value %d is the one the engine reserves for impassable ground -- everything else on this row is discarded", name, val)
		}

		checkMapBrushSprites(f, row, name, sprites, rep)
	}

	if len(grounds) == 0 {
		rep.Errorf(check, f.Path, s.Line, "",
			"the %s section defines no terrain", mapBrush)
	}
	return grounds
}

// checkMapBrushSprites validates the six fill slots. These are the map's
// references into the sprite files under Logistics, and a name that resolves
// to nothing leaves that slot scattering nothing over the terrain.
func checkMapBrushSprites(f *datacsv.File, row datacsv.Row, name string, sprites *SpriteSet, rep *report.Report) {
	for i := range mapFillSprites {
		ref := row.Field(brSprite1 + i)
		if ref == "" {
			continue // an unused fill slot, which most terrains have
		}
		if !sprites.Has(ref) {
			rep.Errorf("map-ref", f.Path, row.Line,
				"the engine logs \"Graphic not found\" and the slot scatters nothing",
				"%s: fill sprite %d %q is not defined in unitpack.csv, gfxpack.csv or gfx.csv", name, i+1, ref)
		}
	}
}

// checkMapSounds validates the scattered sprites and ambient sounds. A row here
// places one object: a tree, a building, a patch of noise, or a town marker.
func checkMapSounds(f *datacsv.File, s MapSection, grounds map[int]int, sets *NameSets, sprites *SpriteSet, rep *report.Report) {
	const check = "map-ref"

	for _, row := range s.Rows {
		where := fmt.Sprintf("at %s,%s", row.Field(snLocX), row.Field(snLocY))

		// The sprite is the map's other reference into the sprite files. A
		// blank one is normal: a row with only a sound places noise, not an
		// object.
		if ref := row.Field(snSprite); ref != "" && !sprites.Has(ref) {
			rep.Errorf(check, f.Path, row.Line,
				"the engine logs \"Graphic not found\" and places nothing",
				"sprite %q %s is not defined in unitpack.csv, gfxpack.csv or gfx.csv", ref, where)
		}

		if ref := row.Field(snSound); ref != "" && sets.Unread("sfx.csv") == "" {
			if _, ok := sets.Sfx[strings.ToUpper(ref)]; !ok {
				rep.Warnf(check, f.Path, row.Line, "",
					"sound %q %s is not defined in sfx.csv", ref, where)
			}
		}

		// The terrain column names a greyscale value in the ground table,
		// and what it is for is a fort: STree::Init reads it into m_fort,
		// "ground value for fort smoke" (War3D/trees.cpp:144). Most of these
		// name a fort rather than a brush terrain, so both sections count. A
		// zero is how the row says it is not fort smoke at all
		// (War3D/trees.cpp:183), and the engine claims the path value for
		// itself after reading the file.
		if raw := row.Field(snTerrain); raw != "" {
			if v, err := strconv.Atoi(raw); err == nil {
				if _, ok := grounds[v]; !ok && v != mapDeadVal && v != mapPathVal {
					rep.Warnf("map", f.Path, row.Line,
						"the engine reads gLand.m_ground[m_fort]->CurDam() without checking it (War3D/trees.cpp:488)",
						"terrain %d %s names neither a %s terrain nor a %s", v, where, mapBrush, mapForts)
				}
			}
		}
	}
}

// checkMapObjectives validates the named locations. These are what a scenario's
// objectives and a courier's directions are written against, so a blank name
// is a location nothing can refer to.
func checkMapObjectives(f *datacsv.File, s MapSection, rep *report.Report) {
	const check = "map"

	seen := map[string]int{}
	for _, row := range s.Rows {
		name := row.Field(obName)
		if name == "" {
			rep.Warnf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"objective has no name -- nothing can refer to it")
			continue
		}
		if prev, dup := seen[strings.ToUpper(name)]; dup {
			rep.Warnf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
				"duplicate objective %q -- both are kept, and which one a reference finds is the order they were read in", name)
		} else {
			seen[strings.ToUpper(name)] = row.Line
		}

		for _, c := range []struct {
			col   int
			label string
		}{{obX, "X"}, {obY, "Y"}} {
			raw := row.Field(c.col)
			if raw == "" {
				rep.Warnf(check, f.Path, row.Line, "",
					"%s: %s is blank -- the objective sits at 0", name, c.label)
				continue
			}
			if _, err := strconv.ParseFloat(raw, 64); err != nil {
				rep.Errorf(check, f.Path, row.Line, "",
					"%s: %s is %q, not a number -- the objective sits at 0", name, c.label, raw)
			}
		}
	}
}

// checkMapForts validates the fort table, whose firing lines are the one place
// in a map file where a wrong number is a crash rather than a wrong value.
func checkMapForts(f *datacsv.File, s MapSection, grounds map[int]int, rep *report.Report) {
	const check = "map"

	for _, row := range s.Rows {
		first := row.Field(ftName)

		// The consumer skips a row whose first column reads "Name", which is
		// how a second sub-header inside the section is tolerated.
		if strings.EqualFold(first, "Name") {
			continue
		}

		if !strings.EqualFold(first, mapFiringLine) {
			checkMapFortDefinition(f, row, grounds, rep)
			continue
		}

		// A firing line indexes the ground table directly and then reads
		// through what it finds. Every slot starts empty, so a value with no
		// terrain behind it is a null the engine follows.
		raw := row.Field(flGround)
		v, err := strconv.Atoi(raw)
		switch {
		case raw == "":
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: the terrain value is blank -- the loader reads it as 0", mapFiringLine)
		case err != nil:
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: the terrain value is %q, not a whole number -- the loader reads it as %d", mapFiringLine, raw, datacsv.Atoi(raw))
		case v < 0 || v >= mapGroundSlots:
			rep.Errorf(check, f.Path, row.Line,
				fmt.Sprintf("the ground table holds %d entries", mapGroundSlots),
				"%s: the terrain value is %d -- the loader indexes the ground table with it and reads outside it", mapFiringLine, v)
		default:
			if _, ok := grounds[v]; !ok {
				rep.Errorf(check, f.Path, row.Line,
					"every slot in the ground table starts empty, and the loader reads through this one without checking it",
					"%s: terrain %d has no row in the %s section -- the game follows a null pointer as this map loads", mapFiringLine, v, mapBrush)
			}
		}
	}
}

// checkMapFortDefinition validates a row that opens a fort. It carries its own
// greyscale value, and the loader stores it in the ground table exactly as the
// brush section does.
func checkMapFortDefinition(f *datacsv.File, row datacsv.Row, grounds map[int]int, rep *report.Report) {
	const check = "map"

	name := row.Field(ftName)
	if name == "" {
		return
	}

	raw := row.Field(ftVal)
	v, err := strconv.Atoi(raw)
	switch {
	case raw == "":
		rep.Errorf(check, f.Path, row.Line, "",
			"%s: greyscale value is blank -- the fort is stored at 0, over the impassable-ground entry", name)
	case err != nil:
		rep.Errorf(check, f.Path, row.Line, "",
			"%s: greyscale value is %q, not a whole number -- the loader reads it as %d", name, raw, datacsv.Atoi(raw))
	case v < 0 || v >= mapGroundSlots:
		rep.Errorf(check, f.Path, row.Line,
			fmt.Sprintf("the ground table holds %d entries", mapGroundSlots),
			"%s: greyscale value is %d -- the loader stores the fort at that index without checking it, writing past the end of the table", name, v)
	default:
		if prev, dup := grounds[v]; dup {
			rep.Warnf(check, f.Path, row.Line, fmt.Sprintf("terrain defined at line %d", prev),
				"%s: greyscale value %d already names a terrain -- the fort replaces it in the table", name, v)
		}
		grounds[v] = row.Line
	}
}
