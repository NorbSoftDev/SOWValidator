package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// ugHeader is the column header of a current-layout unitglobal.csv, trimmed to
// the columns the loader reads.
var ugHeader = strings.Join([]string{
	"Class", "Type", "Alt Class", "Captured Class",
	"Walk Speed (miles/hr)", "Mid Speed", "Run Speed (miles/hr)",
	"Uniform 1", "Uniform 2", "Uniform 3", "Uniform 4", "Uniform 5", "Uniform 6",
	"Walk Sound", "Stand Sound", "Load Sound", "Ram Sound", "Fire Sound",
	"Run Sound", "Charge Sound", "Melee Sound", "Prone Sound",
	"Flag Bearer Sprite",
	"Menu Destination", "Menu Stand", "Menu March", "Menu Targets",
	"Fighting Formation (formtype:0)", "Walk Formation(formtype:1)",
	"Double Line(formtype:2)", "Column By Division(formtype:3)",
	"Skirmish Formation(formtype:4)", "Road Column(formtype:5)",
	"Line With Reserve(formtype:6)", "Column of Regiments in Line(formtype:7)",
	"Column Full Distance Formation (formtype:8)", "Special Formation (formtype:9)",
}, ",")

// ugRow builds a row from the fields that matter to a case, leaving the rest
// blank. Writing these out by hand invites an off-by-one in the very column
// indices the test exists to pin down.
func ugRow(fields map[int]string) string {
	row := make([]string, ugFormType1+ugFormTypes)
	for col, v := range fields {
		for col >= len(row) {
			row = append(row, "")
		}
		row[col] = v
	}
	return strings.Join(row, ",")
}

// ugValid is a row with nothing wrong with it, for a case to spoil one field of.
func ugValid() map[int]string {
	return map[int]string{
		ugClass:      "UGLB_Test",
		ugTypeID:     "UT_Inf",
		ugWalkSpeed:  "3",
		ugMidSpeed:   "4",
		ugRunSpeed:   "6",
		ugUniform1:   "USPR_Test",
		ugSound1:     "SFX_Walk",
		ugFlagBearer: "USPR_Flag",
		ugFormType1:  "DRIL_Test",
	}
}

// runUnitGlobal checks the given rows against a world holding one of each thing
// they can point at.
func runUnitGlobal(t *testing.T, rows ...string) *report.Report {
	t.Helper()
	return runUnitGlobalIn(t, newUnitGlobalWorld(), rows...)
}

func newUnitGlobalWorld() *NameSets {
	sets := NewNameSets()
	sets.UnitType["UT_INF"] = "unittype.csv:2"
	sets.UnitModel["USPR_Test"] = "unitmodel.csv:2"
	sets.UnitModel["USPR_Flag"] = "unitmodel.csv:3"
	sets.Sfx["SFX_WALK"] = "sfx.csv:2"
	sets.Drill["DRIL_TEST"] = "drills.csv:2"
	return sets
}

func runUnitGlobalIn(t *testing.T, sets *NameSets, rows ...string) *report.Report {
	t.Helper()

	p := filepath.Join(t.TempDir(), "unitglobal.csv")
	body := ugHeader + "\n" + strings.Join(rows, "\n") + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := datacsv.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	sets.AddUnitGlobalClasses(f, "unitglobal.csv")

	rep := report.New("")
	UnitGlobal(f, sets, rep)
	return rep
}

func TestUnitGlobalClean(t *testing.T) {
	wantClean(t, runUnitGlobal(t, ugRow(ugValid())))
}

// Most rows in the shipped file stop well short of the last formation column,
// which is how the file says a class has no such formation.
func TestUnitGlobalShortRowIsFine(t *testing.T) {
	row := ugRow(ugValid())
	wantClean(t, runUnitGlobal(t, strings.TrimRight(row, ",")))
}

func TestUnitGlobalDuplicateClass(t *testing.T) {
	rep := runUnitGlobal(t, ugRow(ugValid()), ugRow(ugValid()))
	wantFinding(t, rep, 3, `duplicate Class "UGLB_Test"`)
}

// Init looks the type up whether or not one was given, so a blank is fatal in
// exactly the way a wrong one is.
func TestUnitGlobalBlankType(t *testing.T) {
	r := ugValid()
	r[ugTypeID] = ""
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, "Type is blank")
}

func TestUnitGlobalUnknownType(t *testing.T) {
	r := ugValid()
	r[ugTypeID] = "UT_Nope"
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, "the game refuses to start")
}

func TestUnitGlobalUnknownAltClass(t *testing.T) {
	r := ugValid()
	r[ugAltClass] = "UGLB_Nope"
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, `Alt Class "UGLB_Nope" is not a Class`)
}

// A class naming another row of the same file resolves whichever order the two
// are written in, and the match ignores case because the loader upper-cases
// both sides.
func TestUnitGlobalAltClassResolvesForwardAndByCase(t *testing.T) {
	first := ugValid()
	first[ugAltClass] = "uglb_other"
	second := ugValid()
	second[ugClass] = "UGLB_Other"
	wantClean(t, runUnitGlobal(t, ugRow(first), ugRow(second)))
}

func TestUnitGlobalSpeeds(t *testing.T) {
	for _, c := range []struct {
		name, value, want string
	}{
		{"blank", "", "Mid Speed is blank"},
		{"not a number", "quick", `Mid Speed is "quick"`},
		{"zero", "0", "Mid Speed is 0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ugValid()
			r[ugMidSpeed] = c.value
			rep := runUnitGlobal(t, ugRow(r))
			wantFinding(t, rep, 2, c.want)
		})
	}
}

func TestUnitGlobalUnknownUniform(t *testing.T) {
	r := ugValid()
	r[ugUniform1+1] = "USPR_Nope"
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, `Uniform 2 "USPR_Nope" is not defined in unitmodel.csv`)
}

// The engine matches these names exactly, so a case difference fails like any
// other miss -- and is worth saying so, because it looks right on the page.
func TestUnitGlobalUniformWrongCase(t *testing.T) {
	r := ugValid()
	r[ugUniform1] = "uspr_test"
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, "differs only in case")
}

// With none of the first four resolving, every man in the unit is forced back
// to uniform 1, which is the one that did not resolve.
func TestUnitGlobalNoRandomUniforms(t *testing.T) {
	r := ugValid()
	r[ugUniform1] = ""
	r[ugUniform1+4] = "USPR_Test"
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, "none of the first 4 uniforms resolve")
}

// The flag bearer is the one sprite the loader does not null before looking it
// up, and FindClass dereferences it without checking.
func TestUnitGlobalBlankFlagBearer(t *testing.T) {
	r := ugValid()
	r[ugFlagBearer] = ""
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, "leaves the flag bearer pointer uninitialised")
}

func TestUnitGlobalUnknownFlagBearer(t *testing.T) {
	r := ugValid()
	r[ugFlagBearer] = "USPR_Nope"
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, `Flag Bearer Sprite "USPR_Nope" is not defined in unitmodel.csv`)
	wantFinding(t, rep, 2, "leaves that pointer uninitialised")
}

func TestUnitGlobalUnknownSound(t *testing.T) {
	r := ugValid()
	r[ugSound1+4] = "SFX_Nope"
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, `Fire Sound "SFX_Nope" is not defined in sfx.csv`)
}

func TestUnitGlobalUnknownFormation(t *testing.T) {
	r := ugValid()
	r[ugFormType1+3] = "DRIL_Nope"
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, `names drill "DRIL_Nope"`)
}

// The loader keeps reading drill ids to the end of the line, past the columns
// the header bothers to name.
func TestUnitGlobalFormationPastLabelledColumns(t *testing.T) {
	r := ugValid()
	r[ugFormType1+ugFormTypes+2] = "DRIL_Nope"
	rep := runUnitGlobal(t, ugRow(r))
	wantFinding(t, rep, 2, `formation 12 names drill "DRIL_Nope"`)
}

// A drills.csv that could not be read leaves the drill set short, so a name
// that fails to resolve against it proves nothing and must not be reported as
// though it did.
func TestUnitGlobalFormationsNotJudgedWhenDrillsUnread(t *testing.T) {
	sets := newUnitGlobalWorld()
	sets.MarkUnread("drills.csv", "Mods/Old/Logistics/drills.csv")

	r := ugValid()
	r[ugFormType1+3] = "DRIL_Nope"
	rep := runUnitGlobalIn(t, sets, ugRow(r))

	for _, f := range rep.Findings {
		if strings.Contains(f.Message, "DRIL_Nope") {
			t.Errorf("named a drill it had no way to judge: %s", f.Message)
		}
	}
	wantFinding(t, rep, 1, "1 formation name(s) in this file were not checked")
}

func TestUnitGlobalSoundsNotJudgedWhenSfxUnread(t *testing.T) {
	sets := newUnitGlobalWorld()
	sets.MarkUnread("sfx.csv", "Mods/Old/Logistics/sfx.csv")

	r := ugValid()
	r[ugSound1] = "SFX_Nope"
	rep := runUnitGlobalIn(t, sets, ugRow(r))

	for _, f := range rep.Findings {
		if strings.Contains(f.Message, "SFX_Nope") {
			t.Errorf("named a sound it had no way to judge: %s", f.Message)
		}
	}
	wantFinding(t, rep, 1, "1 sound name(s) in this file were not checked")
}

// The older layout carried two speeds rather than three, so everything from the
// uniforms rightward sits one column to the left of where the loader reads it.
// That is one finding about the file, not one about every value in it.
func TestUnitGlobalOlderLayout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "unitglobal.csv")
	body := "CLASS,TYPE,ALT CLASS,CAPTURED CLASS,Walk Speed (miles/hr),Run Speed (miles/hr),Uniform 1,Uniform 2\n" +
		"UGLB_Test,UT_Inf,,,3,6,USPR_Test,\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := datacsv.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	rep := report.New("")
	UnitGlobal(f, newUnitGlobalWorld(), rep)
	wantFinding(t, rep, 1, "puts Uniform 1 in column 6")
	wantCount(t, rep, 1)
}

// The loader's field splitter honours a quote where a field begins, so a name
// holding a comma is one field to it and must be one field here.
func TestUnitGlobalQuotedField(t *testing.T) {
	r := ugValid()
	r[ugMenu1] = `"Stand, at ease"`
	wantClean(t, runUnitGlobal(t, ugRow(r)))
}

// --- a class against the drills it is placed in ------------------------------
//
// A drill cell may pick which of the class's six uniforms one man wears, and
// the engine reads that value as 1-based into the class's own sprite slots. So
// the pairing can be wrong while neither file is wrong on its own, which is
// what these pin down.

// newUnitGlobalWorldWithDrill reads a real drills.csv into the name sets, so
// the drills a class names carry their actual per-slot sprite values rather
// than a hand-built stand-in.
func newUnitGlobalWorldWithDrill(t *testing.T, body string) *NameSets {
	t.Helper()

	p := filepath.Join(t.TempDir(), "drills.csv")
	if err := os.WriteFile(p, []byte(drillHeader+"\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := datacsv.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	sets := newUnitGlobalWorld()
	sets.AddDrills(f, "drills.csv")
	return sets
}

func TestUnitGlobalDrillWantsUniformTheClassLeavesBlank(t *testing.T) {
	sets := newUnitGlobalWorldWithDrill(t, "Test,DRIL_Test,1,2,10,10\n1,(0-0-2)2\n")
	rep := runUnitGlobalIn(t, sets, ugRow(ugValid()))
	wantFinding(t, rep, 2, "selects uniform 2, which this class does not fill")
}

func TestUnitGlobalDrillWantsUniformTheClassFills(t *testing.T) {
	sets := newUnitGlobalWorldWithDrill(t, "Test,DRIL_Test,1,2,10,10\n1,(0-0-2)2\n")
	r := ugValid()
	r[ugUniform1+1] = "USPR_Test"
	wantClean(t, runUnitGlobalIn(t, sets, ugRow(r)))
}

// A uniform named but unresolved leaves the slot NULL exactly as a blank one
// does, so a drill selecting it lands on nothing either way.
func TestUnitGlobalDrillWantsUniformThatDoesNotResolve(t *testing.T) {
	sets := newUnitGlobalWorldWithDrill(t, "Test,DRIL_Test,1,2,10,10\n1,(0-0-2)2\n")
	r := ugValid()
	r[ugUniform1+1] = "USPR_Nope"
	rep := runUnitGlobalIn(t, sets, ugRow(r))
	wantFinding(t, rep, 2, "selects uniform 2, which this class does not fill")
}

// Slot 1 is the flag bearer, and both places the engine assigns a man his
// sprite give him "sindex = -1" before ever reading Spr, so a sprite written
// on his cell is never read and is not the class's business.
func TestUnitGlobalDrillSpriteOnFlagBearerIsNotAsked(t *testing.T) {
	sets := newUnitGlobalWorldWithDrill(t, "Test,DRIL_Test,1,2,10,10\n(0-0-2)1,2\n")
	wantClean(t, runUnitGlobalIn(t, sets, ugRow(ugValid())))
}

// SForm::Sub is called only from unitbrig.cpp, where a brigade picks the
// formation of a subordinate unit -- which has its own class. Following the
// chain would blame this class for what another one's drill wants.
func TestUnitGlobalSubFormationIsNotFollowed(t *testing.T) {
	sets := newUnitGlobalWorldWithDrill(t,
		"Test,DRIL_Test,1,2,10,10,DRIL_Sub\n1,2\n"+
			"Sub,DRIL_Sub,1,2,10,10\n1,(0-0-3)2\n")
	wantClean(t, runUnitGlobalIn(t, sets, ugRow(ugValid())))
}

// Past the sixth there is no slot to fill at all. That is the drill's own
// fault and its own checks report it against the cell holding it, so it is not
// reported a second time against every class naming the drill.
func TestUnitGlobalDrillSpriteAboveTheLastUniform(t *testing.T) {
	sets := newUnitGlobalWorldWithDrill(t, "Test,DRIL_Test,1,2,10,10\n1,(0-0-7)2\n")
	wantClean(t, runUnitGlobalIn(t, sets, ugRow(ugValid())))
}

// A drill the class does not name cannot place its men, so what it selects is
// nothing to do with this class.
func TestUnitGlobalDrillTheClassDoesNotName(t *testing.T) {
	sets := newUnitGlobalWorldWithDrill(t,
		"Test,DRIL_Test,1,2,10,10\n1,2\n"+
			"Other,DRIL_Other,1,2,10,10\n1,(0-0-4)2\n")
	wantClean(t, runUnitGlobalIn(t, sets, ugRow(ugValid())))
}

// One missing uniform is one thing to fix however many drills select it.
func TestUnitGlobalManyDrillsOneMissingUniform(t *testing.T) {
	sets := newUnitGlobalWorldWithDrill(t,
		"Test,DRIL_Test,1,2,10,10\n1,(0-0-2)2\n"+
			"Two,DRIL_Two,1,2,10,10\n1,(0-0-2)2\n")
	r := ugValid()
	r[ugFormType1+1] = "DRIL_Two"
	rep := runUnitGlobalIn(t, sets, ugRow(r))
	wantFinding(t, rep, 2, "drills DRIL_TEST and DRIL_TWO select uniform 2")
}
