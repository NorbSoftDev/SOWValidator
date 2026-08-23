package checks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// drillHeader is the column header of a current-layout drills.csv.
const drillHeader = "Name,ID,Rows,Columns,RowDist,ColDist,SubForm,KeepForm,CanWheel," +
	"CanFight,MoveRateMod,AboutFace,ArtyForm,MinEnemy,FireMod,MeleeMod,CantMove,CantCharge"

// runDrills writes body to a drills.csv and checks it, returning the findings.
// The body is given without the column header, which every case shares.
func runDrills(t *testing.T, body string) *report.Report {
	t.Helper()

	p := filepath.Join(t.TempDir(), "drills.csv")
	if err := os.WriteFile(p, []byte(drillHeader+"\n"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := datacsv.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	sets := NewNameSets()
	sets.AddDrills(f, "drills.csv")

	rep := report.New("")
	Drills(f, sets, rep)
	return rep
}

// wantFinding fails unless exactly one finding on the given line mentions want.
// Passing line 0 accepts a finding on any line.
func wantFinding(t *testing.T, rep *report.Report, line int, want string) {
	t.Helper()

	n := 0
	for _, f := range rep.Findings {
		if (line == 0 || f.Line == line) && strings.Contains(f.Message, want) {
			n++
		}
	}
	if n != 1 {
		t.Errorf("want 1 finding on line %d mentioning %q, got %d\n%s", line, want, n, dumpFindings(rep))
	}
}

func wantClean(t *testing.T, rep *report.Report) {
	t.Helper()
	if len(rep.Findings) != 0 {
		t.Errorf("want no findings, got %d\n%s", len(rep.Findings), dumpFindings(rep))
	}
}

func wantCount(t *testing.T, rep *report.Report, want int) {
	t.Helper()
	if len(rep.Findings) != want {
		t.Errorf("want %d finding(s), got %d\n%s", want, len(rep.Findings), dumpFindings(rep))
	}
}

func dumpFindings(rep *report.Report) string {
	if len(rep.Findings) == 0 {
		return "  (none)\n"
	}
	var b strings.Builder
	for _, f := range rep.Findings {
		fmt.Fprintf(&b, "  line %d: %s: %s\n", f.Line, f.Level, f.Message)
	}
	return b.String()
}

// A drill whose slot map matches its Rows and Columns, with every slot from 1
// up placed exactly once, is the shape everything else is measured against.
func TestDrillsClean(t *testing.T) {
	wantClean(t, runDrills(t, `Test Line,DRIL_Test,2,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
4,5,6
`))
}

// The blank and comma-led lines the row filter drops still count against Rows,
// because the loader reads them as slot-map lines like any other.
func TestDrillsBlankMapLinesCount(t *testing.T) {
	wantClean(t, runDrills(t, `Test Line,DRIL_Test,4,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3

,,
4,5,6
`))
}

// A name holding a comma has to be quoted, and the loader's own field splitter
// honours a quote where a field begins.
func TestDrillsQuotedName(t *testing.T) {
	wantClean(t, runDrills(t, `"Test Line, extended",DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
`))
}

// Too small a Rows leaves the tail of the slot map stranded: the loader reads
// those lines as definitions and the men on them are never placed.
func TestDrillsRowsTooSmall(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
4,5,6
`)
	wantFinding(t, rep, 4, "read as a drill definition")
}

// Too large a Rows swallows the next drill whole.
func TestDrillsRowsTooLarge(t *testing.T) {
	rep := runDrills(t, `First,DRIL_First,3,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
4,5,6
Second,DRIL_Second,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
`)
	wantFinding(t, rep, 5, "defines a drill but is read as slot-map data")
}

// A Rows reaching past the end of the file leaves the rest of the slots unset.
func TestDrillsRowsPastEndOfFile(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,4,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
4,5,6
`)
	wantFinding(t, rep, 2, "only 2 line(s) of slot map follow")
}

// Rows and Columns index a fixed grid the loader never bounds-checks.
func TestDrillsGridOverflow(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,250,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
`)
	wantFinding(t, rep, 2, "writes past the end of its grid")
}

func TestDrillsRowsNotANumber(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,two,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
`)
	wantFinding(t, rep, 2, `Rows is "two"`)
}

// A slot id above the engine's limit is thrown away, taking that man with it.
func TestDrillsSlotAboveLimit(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,900
`)
	wantFinding(t, rep, 3, "slot id is 900")
}

// The loader's shortcut for an empty cell only covers a cell that opens with
// the delimiter. Anything else with no slot number on it reaches the placement
// code, which indexes the drill's arrays one before their start.
func TestDrillsCellWithNoSlotNumber(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,(10)
`)
	wantFinding(t, rep, 3, "one element before the start")
}

func TestDrillsWhitespaceCellIsNotEmpty(t *testing.T) {
	rep := runDrills(t, "Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1\n1,2, \n")
	wantFinding(t, rep, 3, "one element before the start")
}

func TestDrillsUnclosedParen(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,(10-2,3
`)
	wantFinding(t, rep, 3, "with no ')'")
}

// SForm::Max never terminates when a drill has fewer than two men in it.
func TestDrillsTooFewMenToLookUp(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,,
`)
	wantFinding(t, rep, 2, "places only the flag bearer")
}

func TestDrillsNoMenAtAll(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,0,0,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
`)
	wantFinding(t, rep, 2, "places no men")
	wantCount(t, rep, 1)
}

func TestDrillsMissingSlots(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,2,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
4,,9
`)
	wantFinding(t, rep, 2, "slot(s) 5-8 are never placed")
}

func TestDrillsFlagBearerMissing(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
2,3,4
`)
	wantFinding(t, rep, 2, "the flag bearer, is never placed")
}

func TestDrillsDuplicateSlot(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,2
`)
	wantFinding(t, rep, 3, "slot 2 is placed twice")
}

func TestDrillsDuplicateID(t *testing.T) {
	rep := runDrills(t, `First,DRIL_Same,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
Second,DRIL_Same,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
`)
	wantFinding(t, rep, 4, `duplicate drill ID "DRIL_Same"`)
}

// Cells past Columns are written but never read, so those men go missing.
func TestDrillsCellsPastColumns(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3,4,5
`)
	wantFinding(t, rep, 3, "cells past column 3")
}

// A sub formation naming a drill that is not defined leaves the drill with
// none, exactly as a blank would.
func TestDrillsUnknownSubForm(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,DRIL_Nope,1,1,1,0.3,0,,100,,,,1
1,2,3
`)
	wantFinding(t, rep, 2, `SubForm names drill "DRIL_Nope"`)
}

// A sub formation defined further down the file is still legal: the engine
// resolves these only once every drills.csv has been read.
func TestDrillsForwardSubFormResolves(t *testing.T) {
	wantClean(t, runDrills(t, `First,DRIL_First,1,3,1.8,2.2,DRIL_Second,1,1,1,0.3,0,,100,,,,1
1,2,3
Second,DRIL_Second,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2,3
`))
}

func TestDrillsUnknownSlotSubForm(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,(0-0-0-0-DRIL_Nope)2,3
`)
	wantFinding(t, rep, 3, `subform names drill "DRIL_Nope"`)
}

// The loader applies a per-slot distance only when it is above zero, so a
// negative one is read and then dropped.
func TestDrillsNegativeSlotDistance(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,(-4)2,3
`)
	wantFinding(t, rep, 3, "row distance is -4")
}

// A positive one is normal and must stay quiet, including the CFixed parser's
// three decimal places and the '+' that scales a distance with unit size.
func TestDrillsSlotDistancesAccepted(t *testing.T) {
	wantClean(t, runDrills(t, `Test Line,DRIL_Test,1,4,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,(17.5)2,(0-17.5-1)3,(70-0-0-0-DRIL_Test-3)4
`))
}

func TestDrillsSpriteOutOfRange(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,(0-0-9)2,3
`)
	wantFinding(t, rep, 3, "sprite is 9")
}

func TestDrillsSubTypeOutOfRange(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,(0-0-0-0--7)2,3
`)
	wantFinding(t, rep, 3, "subtype is 7")
}

// The parentheses have to precede the man number; the file's own notes say so,
// and the loader reads the number and throws the rest away.
func TestDrillsParenAfterSlot(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,,,,1
1,2(10),3
`)
	wantFinding(t, rep, 3, `slot id "2(10)" is not a whole number`)
}

// A fraction in a column the loader reads as a whole number is truncated.
func TestDrillsFractionInWholeNumberColumn(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,0,,100,0.5,,,1
1,2,3
`)
	wantFinding(t, rep, 2, `FireMod is "0.5"`)
}

func TestDrillsNonNumericColumn(t *testing.T) {
	rep := runDrills(t, `Test Line,DRIL_Test,1,3,1.8,2.2,,1,1,1,0.3,yes,,100,,,,1
1,2,3
`)
	wantFinding(t, rep, 2, `AboutFace is "yes"`)
}

// The values the shipped data really carries sit outside the ranges the file's
// own type row states, and the loader has no such limits, so none of them are
// worth a finding.
func TestDrillsStaleTypeRowRangesNotEnforced(t *testing.T) {
	wantClean(t, runDrills(t, `Test Line,DRIL_Test,1,3,350+,13.2+,,1,1,1,0.3,2,,100,,,,1
1,2,3
`))
}

// The older layout opened on the drill ID with no Name column, so every value
// in it lands one column from where the loader looks. That is one finding
// about the file, not one about every line in it.
func TestDrillsLegacyLayout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "drills.csv")
	body := "Form #,Rows,Cols,Row Distance,Col Distance,Subformation,Keep Formation\n" +
		"Cav_Column,2,2,5,3,,0\n1,2\n3,4\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := datacsv.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	sets := NewNameSets()
	sets.AddDrills(f, "drills.csv")
	if len(sets.Drill) != 0 {
		t.Errorf("a file in the older layout has no ids to collect, got %v", sets.Drill)
	}

	rep := report.New("")
	Drills(f, sets, rep)
	wantFinding(t, rep, 1, "puts Rows in column 1")
	wantCount(t, rep, 1)
}

// Once the walk is out of step it picks itself up at the next real definition,
// so the rest of the file is still checked -- only the repetition is dropped.
func TestDrillsOutOfStepIsSummarisedAndRecovers(t *testing.T) {
	// Two columns, so that a stranded line -- which the loader reads as a
	// definition, Rows and all -- has no third field to be read as a Rows and
	// strands only itself.
	var b strings.Builder
	b.WriteString("Stranding,DRIL_Stranding,1,2,1.8,2.2,,1,1,1,0.3,0,,100,,,,1\n")
	for range maxOutOfStep + 3 {
		b.WriteString("1,2\n")
	}
	b.WriteString("Later,DRIL_Later,1,2,1.8,2.2,,1,1,1,0.3,0,,100,,,,1\n")
	b.WriteString("1,2\n")

	rep := runDrills(t, b.String())

	stranded := 0
	for _, f := range rep.Findings {
		if strings.Contains(f.Message, "read as a drill definition") {
			stranded++
		}
	}
	if stranded != maxOutOfStep {
		t.Errorf("want %d stranded lines listed one by one, got %d\n%s", maxOutOfStep, stranded, dumpFindings(rep))
	}
	// Eight lines follow the drill, the first of which is its one real slot-map
	// line, leaving seven stranded: five listed, two summed up.
	wantFinding(t, rep, 0, "2 further line(s)")

	// The drill after the stranded block is a good one, and nothing about it
	// should be reported.
	for _, f := range rep.Findings {
		if strings.Contains(f.Message, "DRIL_Later") {
			t.Errorf("the walk did not recover: %s", f.Message)
		}
	}
}

// WalkDrills is what everything else stands on, so its framing is checked on
// its own: a record covers its Rows lines and the next record starts after
// them, whatever those lines look like.
func TestWalkDrillsFraming(t *testing.T) {
	p := filepath.Join(t.TempDir(), "drills.csv")
	body := drillHeader + "\n" +
		",a comment row the filter drops\n" +
		"First,DRIL_First,3,2,1,1,,1,1,1,0,0,,100,,,,1\n" +
		"1,2\n" +
		"\n" +
		",,\n" +
		"Second,DRIL_Second,1,2,1,1,,1,1,1,0,0,,100,,,,1\n" +
		"1,2\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := datacsv.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	recs := WalkDrills(f)
	if len(recs) != 2 {
		t.Fatalf("want 2 drills, got %d", len(recs))
	}
	if recs[0].Line != 3 || len(recs[0].Map) != 3 || recs[0].MapAt != 4 {
		t.Errorf("first drill framed wrong: line %d, map at %d, %d line(s)", recs[0].Line, recs[0].MapAt, len(recs[0].Map))
	}
	if recs[1].Line != 7 {
		t.Errorf("second drill should start after the first drill's slot map, got line %d", recs[1].Line)
	}
}

// splitParams has to agree with the CFixed parser on where a '-' is a sign and
// where it is the separator.
func TestSplitParams(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"0-17.5-1", []string{"0", "17.5", "1"}},
		{"10", []string{"10"}},
		{"-5-10", []string{"-5", "10"}},
		{"0--10", []string{"0", "-10"}},
		{"0-0-0-0-DRIL_Sub-3", []string{"0", "0", "0", "0", "DRIL_Sub", "3"}},
		{"0-0-0-0--7", []string{"0", "0", "0", "0", "", "7"}},
		// The seventh read takes the whole remainder, so writing more than
		// seven values leaves the last one holding the overflow. That is what
		// the loader does with it, and what makes the cell report itself as an
		// unreadable whole number rather than as an extra value.
		{"1-2-3-4-5-6-7-8", []string{"1", "2", "3", "4", "5", "6", "7-8"}},
	}
	for _, c := range cases {
		got := splitParams(c.in)
		if len(got) != len(c.want) {
			t.Errorf("splitParams(%q) = %q, want %q", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("splitParams(%q) = %q, want %q", c.in, got, c.want)
				break
			}
		}
	}
}
