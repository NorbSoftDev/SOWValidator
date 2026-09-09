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
//
// All but UnitModel are keyed upper-cased, because their loaders upper-case
// both the key they store and the name they look up. UnitModel is the
// exception and the reason the distinction is worth keeping.
type NameSets struct {
	UnitType  map[string]string // UPPER TypeName          -> where
	UnitModel map[string]string // exact-case Name         -> where (see below)
	Sfx       map[string]string // UPPER sfx ID            -> where
	Drill     map[string]string // UPPER drill ID          -> where
	Class     map[string]string // UPPER unitglobal Class  -> where
	Ammo      map[string]string // UPPER munitions ID      -> where
	ArtyTable map[string]string // UPPER ArtyTableID       -> where
	Weapon    map[string]string // UPPER rifle/artillery ID -> where
	Effect    map[string]string // UPPER efx ID            -> where
	Font      map[string]string // UPPER gamefonts ID      -> where

	// DrillForm is what each drill asks of the class using it, which is a
	// property of the pair rather than of either file. Keyed UPPER like Drill.
	DrillForm map[string]*DrillForm

	// unread records, per file name, a copy of that file whose layout its
	// loader no longer reads. A rejected file leaves its name set short of
	// whatever it defines, so a reference that fails to resolve against that
	// set proves nothing. The checks that resolve those names report how many
	// they had to leave alone rather than name one they cannot judge.
	unread map[string]string
}

func NewNameSets() *NameSets {
	return &NameSets{
		UnitType:  map[string]string{},
		UnitModel: map[string]string{},
		Sfx:       map[string]string{},
		Drill:     map[string]string{},
		Class:     map[string]string{},
		Ammo:      map[string]string{},
		ArtyTable: map[string]string{},
		Weapon:    map[string]string{},
		Effect:    map[string]string{},
		Font:      map[string]string{},
		DrillForm: map[string]*DrillForm{},
		unread:    map[string]string{},
	}
}

// DrillForm is what one drill asks of whichever class is placed in it: the
// uniform slots its cells select, and where each is selected from.
type DrillForm struct {
	Sprites map[int]string // 1-based uniform slot -> "file:line" of a cell selecting it
}

// MarkUnread records that a copy of base, at where, is in a layout its loader
// no longer reads, so nothing it defines reached the name sets.
func (n *NameSets) MarkUnread(base, where string) { n.unread[strings.ToLower(base)] = where }

// Unread returns where a rejected copy of base was found, or "" if every copy
// of it was read. A check resolving names that come from base must treat a
// miss as unprovable while this is set.
func (n *NameSets) Unread(base string) string { return n.unread[strings.ToLower(base)] }

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

// AddSfx records the sound ids. CSounds::Init (War3D/sounds.cpp:150) reads
// past the Name column and keys on the ID after it, so a file written before
// that column existed has its .wav filenames registered as ids and none of its
// real ones.
func (n *NameSets) AddSfx(f *datacsv.File, short string, rep *report.Report) {
	if !checkLayout(f, sfxLayout, "sfx", rep) {
		n.MarkUnread("sfx.csv", short)
		return
	}
	addKeys(n.Sfx, f, 1, short, true)
}

// labelColumn finds the column carrying a given header label, ignoring spaces,
// underscores and case, or -1 if the header names none. Where a known label
// lands is what says whether a file is in the layout its loader reads.
func labelColumn(f *datacsv.File, label string) int {
	strip := strings.NewReplacer(" ", "", "_", "", "#", "")
	for i := 0; i < f.Header.Len(); i++ {
		if strings.EqualFold(strip.Replace(f.Header.Field(i)), label) {
			return i
		}
	}
	return -1
}
