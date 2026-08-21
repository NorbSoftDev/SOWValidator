package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Layer is one level of the content stack: the base game, an optional DLC, or
// a mod. The engine builds the same stack
// in this order:
//
//	.sow mappacks -> base folder -> Work -> DLC -> mods
//
// and walks it from the bottom up, so later
// layers win. Most data files are read from every layer and
// merged row-by-row; a few are read only from the highest layer,
// replacing the file wholesale.
type Layer struct {
	Kind    string // "base", "dlc", "mod"
	Name    string // display name
	Dir     string // layer root
	DataDir string // Logistics or "Data Files", "" if absent
	PackDir string // Graphics\Packed, "" if absent
}

func (l Layer) Label() string {
	if l.Kind == "base" {
		return "base"
	}
	return fmt.Sprintf("%s: %s", l.Kind, l.Name)
}

// findSub returns the child of dir matching want case-insensitively. The data
// folders are inconsistent about capitalisation ("Packed" vs "packed").
func findSub(dir, want string) string {
	if dir == "" {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() && strings.EqualFold(e.Name(), want) {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}

// newLayer fills in a layer's data and packed directories. Mods and DLC place
// their data in "Data Files" or, in older content, "Logistics".
func newLayer(kind, name, dir string) Layer {
	l := Layer{Kind: kind, Name: name, Dir: dir}
	if d := findSub(dir, "Logistics"); d != "" {
		l.DataDir = d
	} else if d := findSub(dir, "Data Files"); d != "" {
		l.DataDir = d
	}
	l.PackDir = findSub(findSub(dir, "Graphics"), "Packed")
	return l
}

// listDir returns the immediate subdirectories of root\sub, sorted.
func listDir(root, sub string) []string {
	base := findSub(root, sub)
	if base == "" {
		return nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// resolve matches a user-supplied name against available entries, accepting an
// exact case-insensitive match or an unambiguous substring.
func resolve(kind, want string, available []string) (string, error) {
	for _, a := range available {
		if strings.EqualFold(a, want) {
			return a, nil
		}
	}
	var hits []string
	for _, a := range available {
		if strings.Contains(strings.ToLower(a), strings.ToLower(want)) {
			hits = append(hits, a)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return "", fmt.Errorf("no %s named %q (use -list to see what is available)", kind, want)
	default:
		return "", fmt.Errorf("%s %q is ambiguous: matches %s", kind, want, strings.Join(hits, ", "))
	}
}

// BuildLayers assembles the content stack in engine load order: base, then at
// most one DLC, then every requested mod in the order given.
func BuildLayers(root, dlc string, mods []string) ([]Layer, error) {
	layers := []Layer{newLayer("base", filepath.Base(root), root)}

	if dlc != "" {
		avail := listDir(root, "DLC")
		name, err := resolve("DLC", dlc, avail)
		if err != nil {
			return nil, err
		}
		layers = append(layers, newLayer("dlc", name, filepath.Join(findSub(root, "DLC"), name)))
	}

	availMods := listDir(root, "Mods")
	for _, m := range mods {
		name, err := resolve("mod", m, availMods)
		if err != nil {
			return nil, err
		}
		layers = append(layers, newLayer("mod", name, filepath.Join(findSub(root, "Mods"), name)))
	}
	return layers, nil
}

// PrintAvailable lists the DLC and mods installed under root.
func PrintAvailable(root string) {
	fmt.Printf("Game folder: %s\n\n", root)

	dlcs := listDir(root, "DLC")
	fmt.Printf("DLC (%d) -- at most one may be loaded at a time:\n", len(dlcs))
	if len(dlcs) == 0 {
		fmt.Println("  (none)")
	}
	for _, d := range dlcs {
		fmt.Printf("  %s\n", d)
	}

	mods := listDir(root, "Mods")
	fmt.Printf("\nMods (%d) -- any number may be loaded together:\n", len(mods))
	if len(mods) == 0 {
		fmt.Println("  (none)")
	}
	for _, m := range mods {
		fmt.Printf("  %s\n", m)
	}

	fmt.Printf("\nExample:\n  sowvalidator -root \"%s\" -dlc \"%s\" -mod \"%s\"\n",
		root, first(dlcs, "<dlc>"), first(mods, "<mod>"))
}

func first(v []string, fallback string) string {
	if len(v) > 0 {
		return v[0]
	}
	return fallback
}
