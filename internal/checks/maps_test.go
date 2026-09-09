package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// runMap checks one map file, given its sections as written.
func runMap(t *testing.T, body string) *report.Report {
	t.Helper()

	p := filepath.Join(t.TempDir(), "TestMap.csv")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := datacsv.Load(p)
	if err != nil {
		t.Fatal(err)
	}

	rep := report.New("")
	Maps(f, NewNameSets(), NewSpriteSet(), rep)
	return rep
}

// mapWith builds a map file holding one terrain, one fort, and whatever sound
// rows a case wants. The sub-header under each section opener is written out
// because the engine skips that line by position rather than by content, which
// is what the reader has to match.
func mapWith(soundRows ...string) string {
	return strings.Join(append([]string{
		"TERRAIN TABLE BRUSH,,,,,,,,,,,,",
		"Terrain Type Name,Grayscale (RGB),Movement Rate Modifier,Density,Visibility,Height,Defensive Bonus,Fatigue,Unit Can't Halt,No Fade,No Thin,Toggle",
		"IDS_MAP_GRASS,125,0,8,1000,1,0,0,0,,,",
		",,,,,,,,,,,,",
		"TERRAIN TABLE FORTS,,,,,,,,,,,,",
		"Name,Grayscale,Off,Def,CenterX,CenterY,,,,,,",
		"IDS_FORT_TEST,20,20,90,1000,2000,,,,,,",
		",,,,,,,,,,,,",
		"TERRAIN TABLE SOUNDS,,,,,,,,,,,,",
		"loc x,loc z,dir x,dir z,Sprite,Sound File,Terrain,,,,,",
	}, soundRows...), "\n") + "\n"
}

// A fort's greyscale is a ground value like any other: the brush rows and the
// fort rows write into the same gLand.m_ground[val] (War3D/world.cpp:311 and
// :428). Fort smoke names the fort's own value, which is the whole point of
// the column -- "ground value for fort smoke" (War3D/trees.cpp:144) -- so
// reading the forts after the sounds reported every fort as undefined terrain.
func TestMapFortSmokeNamesItsFort(t *testing.T) {
	wantClean(t, runMap(t, mapWith("1100,2100,0,1,,,20,100,600,,,")))
}

func TestMapSoundTerrainNamesABrushTerrain(t *testing.T) {
	wantClean(t, runMap(t, mapWith("1100,2100,0,1,,,125,100,600,,,")))
}

func TestMapSoundTerrainNamesNothing(t *testing.T) {
	rep := runMap(t, mapWith("1100,2100,0,1,,,77,100,600,,,"))
	wantFinding(t, rep, 11, "terrain 77")
}

// Zero is how the row says it is not fort smoke at all (War3D/trees.cpp:183),
// and the engine claims that entry for impassable ground regardless.
func TestMapSoundTerrainZeroIsNotFortSmoke(t *testing.T) {
	wantClean(t, runMap(t, mapWith("1100,2100,0,1,,,0,100,600,,,")))
}

// A fort taking a greyscale the brush section already used replaces that
// terrain in the one table both write into.
func TestMapFortTakesATerrainGreyscale(t *testing.T) {
	body := strings.Replace(mapWith(), "IDS_FORT_TEST,20,", "IDS_FORT_TEST,125,", 1)
	rep := runMap(t, body)
	wantFinding(t, rep, 7, "already names a terrain")
}
