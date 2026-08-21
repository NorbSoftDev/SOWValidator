package checks

import (
	"fmt"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// Geom is a sprite's animation geometry. The product Frames*Angles is the
// number of atlas frames the sprite occupies, and is the value that must match
// between a base sprite and its mod overlays (see ModGeometry).
type Geom struct {
	Frames int
	Angles int
	Where  string // "file:line" where defined
}

func (g Geom) Total() int { return g.Frames * g.Angles }

// SpriteSet is the set of sprite names defined across the sprite-defining CSVs.
// Names are compared upper-cased, matching the engine's buff.MakeUpper()
// .
type SpriteSet struct {
	Defined map[string]string // UPPER name -> "file:line" where defined
	Geom    map[string]Geom   // UPPER name -> animation geometry
}

func NewSpriteSet() *SpriteSet {
	return &SpriteSet{Defined: map[string]string{}, Geom: map[string]Geom{}}
}

// AddFrom records every Name in the first column of f as a defined sprite,
// reporting duplicates the way the engine does. anglesCol and framesCol give
// this file's column positions for Angles and Frames, which differ between
// unitpack/gfxpack and gfx.
// A name redefined by a later layer is a deliberate override -- that is what
// mods and DLC are for -- so duplicates are only reported within a single file.
func (s *SpriteSet) AddFrom(f *datacsv.File, short string, anglesCol, framesCol int, rep *report.Report) {
	const check = "sprite-duplicate"

	inThisFile := map[string]int{}

	for _, row := range f.Rows {
		name := strings.ToUpper(row.Field(colName))
		if name == "" {
			continue
		}
		if prev, ok := inThisFile[name]; ok {
			rep.Warnf(check, f.Path, row.Line,
				fmt.Sprintf("first defined at line %d of this file", prev),
				"sprite %q is defined twice in the same file; the later definition wins and the earlier one is deleted", name)
		}
		inThisFile[name] = row.Line

		where := fmt.Sprintf("%s:%d", short, row.Line)
		s.Defined[name] = where

		angles, aok := row.Int(anglesCol)
		frames, fok := row.Int(framesCol)
		if aok && fok && angles > 0 && frames > 0 {
			s.Geom[name] = Geom{Frames: frames, Angles: angles, Where: where}
		}
	}
}

func (s *SpriteSet) Has(name string) bool {
	_, ok := s.Defined[strings.ToUpper(name)]
	return ok
}

// unitmodel.csv column layout, from its header row:
// Name, Low Res, Walk, Stand, Load, Ready, Fire, Run, Charge, Melee, Prone, Death, Death_2
var modelActionCols = []struct {
	Index int
	Label string
}{
	{2, "Walk"}, {3, "Stand"}, {4, "Load"}, {5, "Ready"}, {6, "Fire"},
	{7, "Run"}, {8, "Charge"}, {9, "Melee"}, {10, "Prone"},
	{11, "Death"}, {12, "Death_2"},
}

const unitmodelCols = 13

// ModelRefs checks that every action column in unitmodel.csv names a sprite
// that actually exists. A dangling name resolves to nothing at load and the
// unit silently has no animation for that action.
func ModelRefs(f *datacsv.File, sprites *SpriteSet, rep *report.Report) {
	const check = "model-refs"

	models := map[string]bool{}
	for _, row := range f.Rows {
		if n := strings.ToUpper(row.Field(colName)); n != "" {
			models[n] = true
		}
	}

	for _, row := range f.Rows {
		name := row.Field(colName)
		if name == "" {
			continue
		}

		if row.Len() < unitmodelCols {
			rep.Errorf(check, f.Path, row.Line,
				fmt.Sprintf("row: %s", row.Raw),
				"%s: row has %d columns, expected %d -- Death/Death_2 may be missing entirely",
				name, row.Len(), unitmodelCols)
			// Keep going: the columns that are present are still worth checking.
		}

		for _, c := range modelActionCols {
			if c.Index >= row.Len() {
				continue
			}
			ref := row.Field(c.Index)
			if ref == "" {
				continue
			}
			if !sprites.Has(ref) {
				rep.Errorf(check, f.Path, row.Line, "",
					"%s: %s references sprite %q, which is not defined in unitpack.csv, gfxpack.csv or gfx.csv",
					name, c.Label, ref)
			}
		}

		// "Low Res" names an alternate sprite; it may point at either a sprite
		// or another model entry, so accept both.
		if lowRes := row.Field(1); lowRes != "" {
			if !sprites.Has(lowRes) && !models[strings.ToUpper(lowRes)] {
				rep.Warnf(check, f.Path, row.Line, "",
					"%s: Low Res references %q, which is neither a defined sprite nor another unitmodel entry", name, lowRes)
			}
		}
	}
}
