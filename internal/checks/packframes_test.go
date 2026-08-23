package checks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/plist"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// runPack builds a one-pack .plist holding exactly the given 1-based key
// numbers, runs PackFrames over a single unitpack row, and returns the
// rendered report.
func runPack(t *testing.T, keyNums []int, row string) string {
	t.Helper()
	dir := t.TempDir()

	var sb strings.Builder
	sb.WriteString(`<?xml version="1.0"?><plist><dict><key>frames</key><dict>`)
	for _, n := range keyNums {
		fmt.Fprintf(&sb, "<key>IWOW%04d.png</key><dict><key>x</key><integer>0</integer></dict>", n)
	}
	sb.WriteString(`</dict></dict></plist>`)
	if err := os.WriteFile(filepath.Join(dir, "iwow.plist"), []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	ix := plist.NewIndex()
	if _, err := ix.LoadDir(dir); err != nil {
		t.Fatal(err)
	}

	csvPath := filepath.Join(dir, "unitpack.csv")
	csv := "Name,File,First,Scale,Angles,Frames,Timing,Levitate\n" + row + "\n"
	if err := os.WriteFile(csvPath, []byte(csv), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := datacsv.Load(csvPath)
	if err != nil {
		t.Fatal(err)
	}

	rep := report.New(dir)
	PackFrames(f, PackSpec{Name: "unitpack.csv", MinCols: unitpackCols}, ix, rep)
	var out strings.Builder
	rep.WriteText(&out, false)
	return out.String()
}

func seq(lo, hi int) []int {
	var out []int
	for i := lo; i <= hi; i++ {
		out = append(out, i)
	}
	return out
}

// A pack whose keys start at 0065 leaves slots 0-63 empty. The row that reads
// slots 64-127 is itself complete, so this is a warning about the pack, and
// the message has to say so rather than reading as a fault in the row.
func TestPackFramesLeadingGap(t *testing.T) {
	got := runPack(t, seq(65, 128), "GFX_NatW_Walk,IWOW,65,1,8,8,100,0")

	for _, want := range []string{
		"warning:",
		"pack IWOW defines no sprites for slots 0-63, so its first sprite is slot 64",
		"defined by: iwow.plist",
		"keys IWOW0001.png through IWOW0064.png",
		"This row itself is fine -- it reads slots 64-127",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "error:") {
		t.Errorf("a leading gap must not be an error:\n%s", got)
	}
}

// Holes punched into the middle of a pack are described against its last slot,
// not reported as a late start.
func TestPackFramesInteriorGap(t *testing.T) {
	keys := append(seq(1, 8), seq(13, 24)...) // slots 8-11 empty, 0-7 and 12-23 present
	got := runPack(t, keys, "GFX_NatW_Walk,IWOW,13,1,4,3,100,0")

	for _, want := range []string{
		"pack IWOW defines no sprites for slots 8-11, which sit below its last slot 23",
		"empty slot\n    8 is the key IWOW0009.png",
		"This row itself is fine -- it reads slots 12-23",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// A row that genuinely lacks frames stays an error, not a gap warning.
func TestPackFramesShortIsError(t *testing.T) {
	got := runPack(t, seq(1, 8), "GFX_NatW_Walk,IWOW,1,1,4,4,100,0")
	if !strings.Contains(got, "is short 8 frame(s)") {
		t.Errorf("expected a short-pack error:\n%s", got)
	}
}
