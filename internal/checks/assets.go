package checks

import (
	"os"
	"path/filepath"
	"strings"
)

// Assets indexes the loose files a data row can name -- a .wav, a font, a map
// bitmap -- so a name that points at nothing can be reported.
//
// CW3DFind (Shared/w3dfind.cpp:451) resolves these by walking the content
// layers and looking for the name in one named subdirectory of each. The
// search is flat: it does not descend, so a file one folder deeper is a file
// the engine will not find. This mirrors that.
//
// It also reads .sow catalog archives, which this cannot. An install using
// them can hold a file this index has never seen, so finding a catalog
// anywhere switches the whole index off rather than let it report files that
// are present as missing.
type Assets struct {
	// byDir maps a subdirectory -- "Sounds\", "Layout\" -- to the lower-cased
	// names found in it across every layer.
	byDir map[string]map[string]bool
	// sealed records that a catalog was found, so absence proves nothing.
	sealed bool
}

func NewAssets() *Assets {
	return &Assets{byDir: map[string]map[string]bool{}}
}

// Add indexes one layer's copy of a subdirectory. A layer that does not have
// it contributes nothing, which is normal: most layers carry only some.
func (a *Assets) Add(kind, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	set := a.byDir[kind]
	if set == nil {
		set = map[string]bool{}
		a.byDir[kind] = set
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		set[name] = true
		if strings.HasSuffix(name, ".sow") {
			a.sealed = true
		}
	}
}

// Seal switches the index off, for an install whose files may be inside a
// catalog this cannot read.
func (a *Assets) Seal() { a.sealed = true }

// Usable reports whether absence from the index means anything.
func (a *Assets) Usable() bool { return !a.sealed }

// Has reports whether a file is where the engine would look for it. It answers
// true for anything while the index is sealed, so a caller need not repeat the
// test. An empty name is nobody's file and answers true as well; whether a
// blank column is a fault is the caller's question, not this one's.
//
// exts lets a caller accept a name written without its extension. The engine's
// own callers vary: some store the name exactly as the file is called, others
// append the extension themselves.
func (a *Assets) Has(kind, name string, exts ...string) bool {
	if a.sealed || strings.TrimSpace(name) == "" {
		return true
	}
	set := a.byDir[kind]
	if len(set) == 0 {
		// Nothing indexed for this kind at all. Reporting every name against
		// an empty index would be one finding per row for a folder this
		// simply did not read.
		return true
	}

	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "/", `\`)
	// A name carrying a path is looked for by its last element, since the
	// engine's search is flat.
	if i := strings.LastIndexByte(n, '\\'); i >= 0 {
		n = n[i+1:]
	}
	if set[n] {
		return true
	}
	for _, ext := range exts {
		if set[n+strings.ToLower(ext)] {
			return true
		}
	}
	return false
}

// AssetDirs are the subdirectories the checks look in, as the engine names
// them in Shared/w3dfind.h.
const (
	DirSounds  = "Sounds"
	DirFonts   = `Graphics\Fonts`
	DirMaps    = "Maps"
	DirModules = "Modules"
	DirOOBs    = "OOBs"
	DirScen    = "Scenarios"
)

// LayerDir joins a layer root and one of the subdirectories above.
func LayerDir(root, kind string) string {
	return filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(kind, `\`, "/")))
}
