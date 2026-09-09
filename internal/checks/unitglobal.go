package checks

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// unitglobal.csv is a table, unlike drills.csv, but it is not read like one.
// CSoldCmn::Init (War3D/soldcmn.cpp:242) takes only the first two fields and
// puts the rest of the line away in a buffer; CSoldCmn::Load reads that buffer
// much later, the first time something asks for the class. So the column
// meanings live in two functions, most of the row is never touched unless the
// class is used, and a fault in the tail surfaces mid-battle rather than at
// load.
//
// The row is also mostly runs of same-typed columns -- six uniforms, nine
// sounds, four menus, then a list of drill ids that runs to the end of the
// line -- pointing at four different files. There is nothing about the shape
// of a column here that says what belongs in it, which is why this is read off
// the loader rather than inferred.

// Definition columns, in the order the two loaders read them.
const (
	ugClass  = iota // read by Init
	ugTypeID        // read by Init; everything below comes out of the buffer
	ugAltClass
	ugCapClass
	ugWalkSpeed
	ugMidSpeed
	ugRunSpeed
	ugUniform1
	ugSound1     = ugUniform1 + ugUniforms
	ugFlagBearer = ugSound1 + ugSounds
	ugMenu1      = ugFlagBearer + 1
	ugFormType1  = ugMenu1 + ugMenus
)

const (
	// ugUniforms is MAX_ALTSPRITES and ugRandomUniforms is MAX_RANSPRITES
	// (War3D/defines.h:42). Only the first ugRandomUniforms are drawn from at
	// random; the rest are picked by a drill's per-slot sprite value.
	ugUniforms       = 6
	ugRandomUniforms = 4
	// ugSounds is eStateMax and ugMenus is eUMMAX.
	ugSounds = 9
	ugMenus  = 4
	// ugFormTypes is eFTMIN: the loader reads at least this many drill ids,
	// and then keeps going for as long as the line has fields left.
	ugFormTypes = 10
)

// UnitGlobal validates one unitglobal.csv.
func UnitGlobal(f *datacsv.File, sets *NameSets, rep *report.Report) {
	if !checkLayout(f, unitGlobalLayout, "unitglobal", rep) {
		return
	}

	seen := map[string]int{}
	unformed, unheard := 0, 0

	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}
		class := row.Field(ugClass)
		if class == "" {
			rep.Errorf("unitglobal", f.Path, row.Line, "row: "+trim(row.Raw),
				"row has no Class -- it is keyed on the empty string, so it and every other Class-less row collapse into one entry")
			continue
		}
		if prev, dup := seen[strings.ToUpper(class)]; dup {
			rep.Errorf("unitglobal", f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
				"duplicate Class %q -- the loader deletes the earlier row and keeps this one", class)
		} else {
			seen[strings.ToUpper(class)] = row.Line
		}

		checkUnitGlobalType(f, row, class, sets, rep)
		checkUnitGlobalClassRefs(f, row, class, sets, rep)
		checkUnitGlobalSpeeds(f, row, class, rep)
		uniforms := checkUnitGlobalSprites(f, row, class, sets, rep)
		checkUnitGlobalFormSprites(f, row, class, uniforms, sets, rep)
		unheard += checkUnitGlobalSounds(f, row, class, sets, rep)
		unformed += checkUnitGlobalFormTypes(f, row, class, sets, rep)
	}

	// Silently skipping these would leave the report claiming a clean file it
	// never actually checked.
	if unformed > 0 {
		rep.Warnf("unitglobal-ref", f.Path, 1, "that file's drills are missing from the set these resolve against",
			"%d formation name(s) in this file were not checked, because %s could not be read", unformed, sets.Unread("drills.csv"))
	}
	if unheard > 0 {
		rep.Warnf("unitglobal-ref", f.Path, 1, "that file's sounds are missing from the set these resolve against",
			"%d sound name(s) in this file were not checked, because %s could not be read", unheard, sets.Unread("sfx.csv"))
	}
}

// AddUnitGlobalClasses records the Class of every row, so the Alt Class and
// Captured Class columns -- which name another row of this same file -- can be
// resolved. The loader keys these upper-cased and looks them up the same way,
// so the match is case-insensitive.
func (n *NameSets) AddUnitGlobalClasses(f *datacsv.File, short string) {
	// A file in the older layout is misread from the uniforms rightward, but
	// Class is before that and is still where the loader looks, so its classes
	// are worth collecting either way.
	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}
		k := strings.ToUpper(row.Field(ugClass))
		if k == "" {
			continue
		}
		if _, exists := n.Class[k]; !exists {
			n.Class[k] = fmt.Sprintf("%s:%d", short, row.Line)
		}
	}
}

// ugLabel is the file's own name for a column, which is what a modder is
// looking at. The fallback covers a column the header does not reach, and the
// tail of formation columns regularly runs past the labelled ones.
func ugLabel(f *datacsv.File, col int, fallback string) string {
	if l := strings.TrimSpace(f.Header.Field(col)); l != "" {
		return l
	}
	return fallback
}

// checkUnitGlobalType validates the one reference the engine treats as fatal.
func checkUnitGlobalType(f *datacsv.File, row datacsv.Row, class string, sets *NameSets, rep *report.Report) {
	const check = "unitglobal"

	// Init looks this up unconditionally, so a blank one fails exactly as a
	// wrong one does -- the lookup of "" simply misses.
	t := row.Field(ugTypeID)
	if t == "" {
		rep.Errorf(check, f.Path, row.Line,
			"the engine logs \"Unit type not found\" and sets its quit flag",
			"%s: Type is blank -- the loader looks it up anyway, finds nothing, and the game refuses to start", class)
		return
	}
	if _, ok := sets.UnitType[strings.ToUpper(t)]; !ok {
		rep.Errorf(check, f.Path, row.Line,
			"the engine logs \"Unit type not found\" and sets its quit flag",
			"%s: Type %q is not defined in unittype.csv -- the game refuses to start", class, t)
	}
}

// checkUnitGlobalClassRefs validates the two columns naming another row of this
// same file. Both are optional; a blank one simply means the class has no
// alternate or captured form.
func checkUnitGlobalClassRefs(f *datacsv.File, row datacsv.Row, class string, sets *NameSets, rep *report.Report) {
	const check = "unitglobal-ref"

	for _, c := range []struct {
		col   int
		label string
	}{
		{ugAltClass, ugLabel(f, ugAltClass, "Alt Class")},
		{ugCapClass, ugLabel(f, ugCapClass, "Captured Class")},
	} {
		ref := row.Field(c.col)
		if ref == "" {
			continue
		}
		if _, ok := sets.Class[strings.ToUpper(ref)]; !ok {
			rep.Errorf(check, f.Path, row.Line,
				"the engine logs \"Can't find unit class type\" and carries on without it",
				"%s: %s %q is not a Class defined in unitglobal.csv", class, c.label, ref)
		}
	}
}

// checkUnitGlobalSpeeds validates the three speeds. They are read straight into
// CFixed and then multiplied out to units per second, so a value the parser
// cannot read becomes 0 and the unit does not move.
func checkUnitGlobalSpeeds(f *datacsv.File, row datacsv.Row, class string, rep *report.Report) {
	const check = "unitglobal"

	for _, c := range []struct {
		col   int
		label string
	}{
		{ugWalkSpeed, ugLabel(f, ugWalkSpeed, "Walk Speed")},
		{ugMidSpeed, ugLabel(f, ugMidSpeed, "Mid Speed")},
		{ugRunSpeed, ugLabel(f, ugRunSpeed, "Run Speed")},
	} {
		raw := row.Field(c.col)
		if raw == "" {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is blank -- the loader reads it as 0 and the unit cannot move at that speed", class, c.label)
			continue
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %q, not a number -- the loader reads it as 0 and the unit cannot move at that speed",
				class, c.label, raw)
			continue
		}
		if v <= 0 {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s is %s -- the unit cannot move at that speed", class, c.label, raw)
		}
	}
}

// checkUnitGlobalSprites validates the six uniform columns and the flag bearer.
//
// The flag bearer is the dangerous one. CSoldCmn::Load resolves it with
// Lookup, which leaves its out parameter untouched when the name misses
// (Shared/vechelp.inl:44), and SSoldCmn has no constructor, so m_fsprite is
// whatever was on the heap. FindClass then dereferences it without a null
// check. The six uniforms do not have this problem: the loader sets each of
// them to NULL before looking it up.
func checkUnitGlobalSprites(f *datacsv.File, row datacsv.Row, class string, sets *NameSets, rep *report.Report) [ugUniforms]uniformState {
	const check = "unitglobal-ref"

	var state [ugUniforms]uniformState
	filled := 0
	for j := range ugUniforms {
		ref := row.Field(ugUniform1 + j)
		if ref == "" {
			state[j] = uniformBlank
			continue
		}
		if checkModelRef(f, row, class, ugLabel(f, ugUniform1+j, fmt.Sprintf("Uniform %d", j+1)), ref, sets, rep) {
			if j < ugRandomUniforms {
				filled++
			}
		} else {
			state[j] = uniformBroken
		}
	}

	// m_ransprcount counts the resolved uniforms among the first four. At zero
	// every man in the unit is forced to uniform slot 0, which is the one that
	// did not resolve.
	if filled == 0 {
		rep.Errorf("unitglobal", f.Path, row.Line,
			fmt.Sprintf("the loader draws a man's uniform at random from the first %d", ugRandomUniforms),
			"%s: none of the first %d uniforms resolve -- every man in the unit falls back to uniform 1, which is not there either",
			class, ugRandomUniforms)
	}

	flag := row.Field(ugFlagBearer)
	label := ugLabel(f, ugFlagBearer, "Flag Bearer Sprite")
	if flag == "" {
		rep.Errorf("unitglobal", f.Path, row.Line,
			"SSoldCmn has no constructor, so the pointer is whatever was on the heap; FindClass then dereferences it without checking",
			"%s: %s is blank -- the loader leaves the flag bearer pointer uninitialised rather than null, and the game reads through it the first time this class is used",
			class, label)
		return state
	}
	if !checkModelRef(f, row, class, label, flag, sets, rep) {
		rep.Errorf("unitglobal", f.Path, row.Line,
			"unlike the uniform slots, this one is not set to null before the lookup, and FindClass dereferences it without checking",
			"%s: because %s does not resolve, the loader leaves that pointer uninitialised and the game reads through it the first time this class is used",
			class, label)
	}
	return state
}

// uniformState is what one of a class's six uniform slots came to once the
// loader had tried to resolve it. Both failures leave the slot NULL, which is
// what a drill selecting that slot then runs into.
type uniformState int

const (
	uniformOK uniformState = iota
	uniformBlank
	uniformBroken
)

// classDrills lists the drills a class's own men can be placed in, which is
// exactly what its formation columns name: "m_formation = theApp.Form()->
// GetForm( m_class->m_formtype[eFTMarch] )" (War3D/unit.cpp:344), and every
// other assignment goes through m_formtype the same way.
//
// A drill's SubForm and ArtyForm are deliberately not followed. Those are not
// another formation for these men -- SForm::Sub is called only from
// unitbrig.cpp, where a brigade uses it to pick the formation of a subordinate
// unit, and it switches on that subordinate's own type. The subordinate is a
// separate unit with its own class, so the uniforms its drill selects are
// asked of that class, not this one. Following the chain would blame an
// infantry commander for what an artillery drill wants.
func classDrills(row datacsv.Row) []string {
	seen := map[string]bool{}
	var out []string

	for col := ugFormType1; col < row.Len(); col++ {
		id := strings.ToUpper(row.Field(col))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// checkUnitGlobalFormSprites pairs a class against the drills it can be placed
// in. A drill cell may choose which of the class's six uniforms one man wears,
// and the engine takes that value as 1-based (War3D/unit.cpp:923):
//
//	m_man[k].sindex = form->Spr(k) - 1;
//	if ( !m_class->GetSprite(m_man[k].sindex, eUnitStand) )
//	{
//	    CUtil::AddLog( "ERROR Form Sprite Request. Form:%s Class:%s Index:%d", ... );
//	    m_man[k].sindex = 0;
//	}
//
// So a value well inside the array can still land on a slot this class left
// NULL, and that man silently wears uniform 1 instead of the one the drill
// asked for. Neither file is wrong on its own, which is why no check that
// reads one file at a time can see it. The engine's other assignment site
// (War3D/unit.cpp:2940) does not even test: it stores the index and lets
// GetSprite hand back NULL.
func checkUnitGlobalFormSprites(f *datacsv.File, row datacsv.Row, class string, state [ugUniforms]uniformState, sets *NameSets, rep *report.Report) {
	const check = "unitglobal-ref"

	// Which drills select each uniform slot the class does not fill, keyed by
	// slot: one missing uniform is one thing to fix however many drills want
	// it, and a class is typically named by a dozen drills at once.
	asked := map[int][]string{}
	where := map[int]string{}

	for _, id := range classDrills(row) {
		form := sets.DrillForm[id]
		if form == nil {
			continue
		}
		for v, at := range form.Sprites {
			// Past the sixth there is no slot to fill, and the drill checks
			// have already reported that value against the cell holding it.
			if v > ugUniforms || state[v-1] == uniformOK {
				continue
			}
			if _, dup := where[v]; !dup {
				where[v] = at
			}
			asked[v] = append(asked[v], id)
		}
	}

	for _, v := range sortedKeys(asked) {
		drills := asked[v]
		sort.Strings(drills)

		label := ugLabel(f, ugUniform1+v-1, fmt.Sprintf("Uniform %d", v))
		reason := label + " is blank"
		if state[v-1] == uniformBroken {
			reason = label + " names a sprite set that does not resolve"
		}
		phrase, verb := drillsPhrase(drills)
		rep.Errorf(check, f.Path, row.Line,
			fmt.Sprintf("%s selects it; %s", where[v], reason),
			"%s: %s %s uniform %d, which this class does not fill -- those men wear uniform 1 instead, and the engine logs \"ERROR Form Sprite Request\"",
			class, phrase, verb, v)
	}
}

// drillsPhrase names the drills asking for a uniform without printing a list
// as long as the file, and hands back the verb that agrees with it. One name
// is the useful case; past a couple, the count is what says this is a property
// of the class rather than of any one drill.
func drillsPhrase(ids []string) (phrase, verb string) {
	switch len(ids) {
	case 1:
		return "drill " + ids[0], "selects"
	case 2:
		return "drills " + ids[0] + " and " + ids[1], "select"
	default:
		return fmt.Sprintf("drill %s and %d others", ids[0], len(ids)-1), "select"
	}
}

// sortedKeys orders a map's integer keys, so the report reads the same way
// every run.
func sortedKeys[V any](m map[int]V) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// It returns how many names it had to leave unjudged.
func checkUnitGlobalSounds(f *datacsv.File, row datacsv.Row, class string, sets *NameSets, rep *report.Report) int {
	const check = "unitglobal-ref"

	// A blank sound is normal: the loader zeroes the whole array first, so an
	// unnamed state simply plays nothing.
	unjudged := 0
	for j := range ugSounds {
		ref := row.Field(ugSound1 + j)
		if ref == "" {
			continue
		}
		if _, ok := sets.Sfx[strings.ToUpper(ref)]; ok {
			continue
		}
		if sets.Unread("sfx.csv") != "" {
			unjudged++
			continue
		}
		rep.Warnf(check, f.Path, row.Line, "",
			"%s: %s %q is not defined in sfx.csv", class, ugLabel(f, ugSound1+j, fmt.Sprintf("sound %d", j+1)), ref)
	}
	return unjudged
}

// checkUnitGlobalFormTypes validates the tail of the row, which is a list of
// drill ids rather than a fixed set of columns: the loader reads at least
// ugFormTypes of them and then keeps reading for as long as the line has
// fields left.
//
// A blank one is how the file says this class has no such formation, and most
// rows are short for exactly that reason, so only a name that fails to resolve
// is worth reporting.
// It returns how many names it had to leave unjudged.
func checkUnitGlobalFormTypes(f *datacsv.File, row datacsv.Row, class string, sets *NameSets, rep *report.Report) int {
	const check = "unitglobal-ref"

	unjudged := 0
	for col := ugFormType1; col < row.Len(); col++ {
		ref := row.Field(col)
		if ref == "" {
			continue
		}
		if _, ok := sets.Drill[strings.ToUpper(ref)]; ok {
			continue
		}
		// With a drills.csv rejected for its layout, the drill set is short of
		// whatever that file defines, so "not defined" is not a conclusion
		// this can honestly draw.
		if sets.Unread("drills.csv") != "" {
			unjudged++
			continue
		}
		rep.Errorf(check, f.Path, row.Line,
			"the engine logs \"Formation not found\" and stores -1, so the class cannot take this formation",
			"%s: %s names drill %q, which is not defined in drills.csv",
			class, ugLabel(f, col, fmt.Sprintf("formation %d", col-ugFormType1)), ref)
	}
	return unjudged
}

// checkModelRef resolves a unitmodel reference and reports whether it did. The
// engine matches these names exactly -- neither the stored key nor the lookup
// string is upper-cased (War3D/soldcmn.cpp:137) -- so a name that differs only
// in case fails like any other, and is called out separately because it is far
// easier to fix.
func checkModelRef(f *datacsv.File, row datacsv.Row, class, label, ref string, sets *NameSets, rep *report.Report) bool {
	const check = "unitglobal-ref"

	if _, ok := sets.UnitModel[ref]; ok {
		return true
	}
	for known := range sets.UnitModel {
		if strings.EqualFold(known, ref) {
			rep.Errorf(check, f.Path, row.Line,
				fmt.Sprintf("unitmodel.csv defines %q; the engine matches these names case-sensitively", known),
				"%s: %s %q differs only in case from a defined unitmodel entry", class, label, ref)
			return false
		}
	}
	rep.Errorf(check, f.Path, row.Line,
		"the engine logs \"Unitglobal sprite set not found\" and leaves the slot empty",
		"%s: %s %q is not defined in unitmodel.csv", class, label, ref)
	return false
}
