package checks

import (
	"fmt"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// MaxSprMods is how many mod overlays a unitmodel row may carry.
const MaxSprMods = 2

// firstModCol is the first mod-overlay column in unitmodel.csv, immediately
// after Death_2. The engine reads nine state sprites, then Death, then
// Death_2, then the overlay references.
const firstModCol = 13

// modSlots are the animation slots compared between a base row and its overlay.
// Each maps to the same index in the mod's own row, because the engine swaps in
// the overlay's sprite set and then indexes the identical state
// .
var modSlots = []struct {
	Index int
	Label string
}{
	{2, "Walk"}, {3, "Stand"}, {4, "Load"}, {5, "Ready"}, {6, "Fire"},
	{7, "Run"}, {8, "Charge"}, {9, "Melee"}, {10, "Prone"},
	{11, "Death"}, {12, "Death_2"},
}

const (
	colDeath  = 11
	colDeath2 = 12
)

// slotRef returns the sprite a row uses for a slot, applying the engine's
// Death_2 -> Death fallback: when no Death_2 sprite is set,
// the engine silently uses Death instead.
func slotRef(row datacsv.Row, slot int) string {
	ref := row.Field(slot)
	if ref == "" && slot == colDeath2 {
		ref = row.Field(colDeath)
	}
	return ref
}

// ModGeometry enforces the invariant that a base sprite and its mod overlays
// must have identical Frames*Angles.
//
// When drawing a figure the engine computes ONE frame index and applies it to
// the base sprite and to both mod overlays, without re-deriving it per sprite.
//
// The engine originally validated this and the check was commented out
// . With it disabled, a base
// and overlay of different geometry share an index that is only in range for
// one of them: the smaller array is indexed with the larger array's frame
// number, reading past the end of its packed-sprite coordinate vectors.
func ModGeometry(f *datacsv.File, sprites *SpriteSet, rep *report.Report) {
	const check = "mod-geometry"

	rows := map[string]datacsv.Row{}
	for _, row := range f.Rows {
		if n := strings.ToUpper(row.Field(colName)); n != "" {
			rows[n] = row
		}
	}

	for _, row := range f.Rows {
		name := row.Field(colName)
		if name == "" {
			continue
		}

		for m := 0; m < MaxSprMods; m++ {
			modName := strings.ToUpper(row.Field(firstModCol + m))
			if modName == "" {
				continue
			}

			modRow, ok := rows[modName]
			if !ok {
				rep.Errorf(check, f.Path, row.Line, "",
					"%s: mod%d references unitmodel entry %q, which is not defined", name, m+1, modName)
				continue
			}

			for _, slot := range modSlots {
				baseRef := slotRef(row, slot.Index)
				modRef := slotRef(modRow, slot.Index)
				if baseRef == "" || modRef == "" {
					continue
				}

				baseGeom, bok := sprites.Geom[strings.ToUpper(baseRef)]
				modGeom, mok := sprites.Geom[strings.ToUpper(modRef)]
				if !bok || !mok {
					// Undefined sprites are reported by the model-refs check.
					continue
				}
				if baseGeom.Total() == modGeom.Total() {
					continue
				}

				detail := fmt.Sprintf(
					"base    %-32s %2d angles x %2d frames = %3d  (%s)\n"+
						"overlay %-32s %2d angles x %2d frames = %3d  (%s)\n"+
						"the engine shares one frame index across both;\n"+
						"the smaller array is indexed with the larger one's frame number.",
					baseRef, baseGeom.Angles, baseGeom.Frames, baseGeom.Total(), baseGeom.Where,
					modRef, modGeom.Angles, modGeom.Frames, modGeom.Total(), modGeom.Where)

				rep.Errorf(check, f.Path, row.Line, detail,
					"%s: %s geometry differs from mod%d overlay %q (%d vs %d frames)",
					name, slot.Label, m+1, modName, baseGeom.Total(), modGeom.Total())
			}
		}
	}
}
