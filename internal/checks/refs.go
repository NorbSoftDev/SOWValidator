package checks

import (
	"fmt"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// NameSets holds the key sets that cross-file references resolve against.
// Each is the union across every data directory, mirroring how the engine
// layers base, DLC and mod content.
type NameSets struct {
	UnitType  map[string]string // UPPER TypeName   -> where
	UnitModel map[string]string // exact-case Name  -> where (see below)
	Sfx       map[string]string // UPPER sfx ID     -> where
}

func NewNameSets() *NameSets {
	return &NameSets{
		UnitType:  map[string]string{},
		UnitModel: map[string]string{},
		Sfx:       map[string]string{},
	}
}

// AddKeys records column col of every row in f into set.
func addKeys(set map[string]string, f *datacsv.File, col int, short string, upper bool) {
	for _, row := range f.Rows {
		k := row.Field(col)
		if k == "" {
			continue
		}
		if upper {
			k = strings.ToUpper(k)
		}
		if _, exists := set[k]; !exists {
			set[k] = fmt.Sprintf("%s:%d", short, row.Line)
		}
	}
}

func (n *NameSets) AddUnitType(f *datacsv.File, short string) {
	addKeys(n.UnitType, f, 0, short, true)
}

// AddUnitModel stores names in their original case. The engine looks these up
// with an exact string match where neither the stored key
// nor the lookup string is upper-cased, so the match is
// case-sensitive and a case typo silently yields no sprite.
func (n *NameSets) AddUnitModel(f *datacsv.File, short string) {
	addKeys(n.UnitModel, f, 0, short, false)
}

func (n *NameSets) AddSfx(f *datacsv.File, short string) {
	addKeys(n.Sfx, f, 1, short, true)
}

// unitglobal.csv column layout. The engine reads Class and TypeID up front and
// defers the rest of the row, parsing it from "Alt Class" onward when the unit
// class is actually loaded.
const (
	ugClass     = 0
	ugTypeID    = 1
	ugAltClass  = 2
	ugCapClass  = 3
	ugUniform1  = 7  // six uniform columns, 7..12
	ugSoundBase = 13 // nine state-sound columns, 13..21
	ugFlagBear  = 22
)

// munitions.csv column layout, from the ammunition loader.
const (
	munSprite = 1
	munSound  = 13
)

// Refs validates cross-file references whose resolution was confirmed in the
// loaders. Only verified relationships are checked -- guessing column meanings
// is how false positives get reintroduced.
func Refs(f *datacsv.File, base string, sets *NameSets, sprites *SpriteSet, rep *report.Report) {
	switch strings.ToLower(base) {
	case "unitglobal.csv":
		refsUnitGlobal(f, sets, rep)
	case "munitions.csv":
		refsMunitions(f, sets, sprites, rep)
	}
}

func refsUnitGlobal(f *datacsv.File, sets *NameSets, rep *report.Report) {
	const check = "xref"

	for _, row := range f.Rows {
		class := row.Field(ugClass)
		if class == "" {
			continue
		}

		// TypeID -> unittype.csv. The engine treats this as fatal: it logs and

		if t := row.Field(ugTypeID); t != "" {
			if _, ok := sets.UnitType[strings.ToUpper(t)]; !ok {
				rep.Errorf(check, f.Path, row.Line,
					"the engine logs \"Unit type not found\" and refuses to start",
					"%s: TypeID %q is not defined in unittype.csv", class, t)
			}
		}

		// Uniform 1-6 -> unitmodel.csv, matched case-sensitively.
		for j := 0; j < 6; j++ {
			ref := row.Field(ugUniform1 + j)
			if ref == "" {
				continue
			}
			checkModelRef(f, row, class, fmt.Sprintf("Uniform %d", j+1), ref, sets, rep)
		}

		// Flag bearer sprite -> unitmodel.csv.
		if ref := row.Field(ugFlagBear); ref != "" {
			checkModelRef(f, row, class, "flag bearer", ref, sets, rep)
		}

		// State sounds -> sfx.csv.
		for j := 0; j < 9; j++ {
			ref := row.Field(ugSoundBase + j)
			if ref == "" {
				continue
			}
			if _, ok := sets.Sfx[strings.ToUpper(ref)]; !ok {
				rep.Warnf(check, f.Path, row.Line, "",
					"%s: sound %q is not defined in sfx.csv", class, ref)
			}
		}
	}
}

// checkModelRef resolves a unitmodel reference, distinguishing a name that does
// not exist at all from one that exists in a different case -- the latter still
// fails at runtime but is far easier to fix.
func checkModelRef(f *datacsv.File, row datacsv.Row, class, label, ref string, sets *NameSets, rep *report.Report) {
	const check = "xref"

	if _, ok := sets.UnitModel[ref]; ok {
		return
	}
	for known := range sets.UnitModel {
		if strings.EqualFold(known, ref) {
			rep.Errorf(check, f.Path, row.Line,
				fmt.Sprintf("unitmodel.csv defines %q; the engine matches these names case-sensitively", known),
				"%s: %s %q differs only in case from a defined unitmodel entry", class, label, ref)
			return
		}
	}
	rep.Errorf(check, f.Path, row.Line,
		"the engine logs \"Unitglobal sprite set not found\" and leaves the slot null",
		"%s: %s %q is not defined in unitmodel.csv", class, label, ref)
}

func refsMunitions(f *datacsv.File, sets *NameSets, sprites *SpriteSet, rep *report.Report) {
	const check = "xref"

	for _, row := range f.Rows {
		name := row.Field(colName)
		if name == "" {
			continue
		}

		// Sprite Graphic -> a defined sprite.
		if ref := row.Field(munSprite); ref != "" {
			if !sprites.Has(ref) {
				rep.Errorf(check, f.Path, row.Line, "",
					"%s: sprite %q is not defined in unitpack.csv, gfxpack.csv or gfx.csv", name, ref)
			}
		}

		// Sound -> sfx.csv.
		if ref := row.Field(munSound); ref != "" {
			if _, ok := sets.Sfx[strings.ToUpper(ref)]; !ok {
				rep.Warnf(check, f.Path, row.Line, "",
					"%s: sound %q is not defined in sfx.csv", name, ref)
			}
		}
	}
}
