// Command sowvalidator validates Scourge of War data files.
//
// It reproduces how the engine reads its CSVs and packed-sprite dictionaries,
// then applies the checks the engine does not: correct column counts, sane
// numeric ranges, resolvable cross-file references, and packed sprites that
// actually contain every frame they declare.
//
// Content is layered the way the game layers it -- base, then at most one DLC,
// then any number of mods -- so a combination can be validated exactly as it
// will be loaded.
//
// Usage:
//
//	sowvalidator -root DIR [-dlc NAME] [-mod NAME]... [-json] [-q]
//	sowvalidator -root DIR -list
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/checks"
	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/plist"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// printHelp writes the full option list. It goes to stdout so it can be piped
// or redirected like any other output.
func printHelp(w io.Writer) {
	fmt.Fprint(w, `SowValidator - data file checker for Scourge of War

Checks the game's data files for the kinds of error the engine cannot report:
missing sprite frames, dangling references between files, malformed rows, and
base/overlay animations whose frame counts disagree.

USAGE
  sowvalidator -root DIR [-dlc NAME] [-mod NAME]... [options]
  sowvalidator -root DIR -list

REQUIRED
  -root DIR     The game folder to check. This is how you choose the game:
                  ...\Base     for Waterloo
                  ...\BaseGB   for Gettysburg

CONTENT TO LOAD
  Mods and DLC stack on top of the base game, so check the combination you
  actually play. Anything you do not list is not loaded.

  -dlc NAME     DLC to load, or omit for none. Only one may be loaded at a
                time, exactly as in the game.
  -mod NAME     Mod to load. Repeat the flag once per mod; any number may be
                loaded together, applied in the order given.
  -list         List the DLC and mods installed under -root, then exit.

  Names may be given in full or as any unambiguous fragment, so
  -dlc Ligny matches "Scourge Of War - Ligny".

OUTPUT
  -json         Emit findings as JSON instead of text.
  -q            Show errors only, hiding warnings and the header, so the
                output pipes or redirects cleanly.

EXAMPLES
  See what is installed:
    sowvalidator -root "C:\Games\SowWL\Base" -list

  Check the base game on its own:
    sowvalidator -root "C:\Games\SowWL\Base"

  Check a DLC plus two mods, as you would play them:
    sowvalidator -root "C:\Games\SowWL\Base" -dlc Ligny -mod "Sprite Test" -mod "New Menus"

  Save a report to a file:
    sowvalidator -root "C:\Games\SowWL\Base" -q > report.txt

EXIT CODES
  0  no errors found
  1  errors found
  2  sowvalidator could not run (bad folder, unknown mod name, ...)
`)
}

// spriteNameCSVs define sprite names. Angles/Frames sit in different columns
// per file: unitpack.csv and gfxpack.csv are
// Name,File,First,Scale,Angles,Frames,... while gfx.csv is
// Name,File,Width,Height,Source x,Source y,Scale,Angles,Frames,...
var spriteNameCSVs = []struct {
	Name      string
	AnglesCol int
	FramesCol int
}{
	{"unitpack.csv", 4, 5},
	{"gfxpack.csv", 4, 5},
	{"gfx.csv", 7, 8},
}

// modList collects a repeatable -mod flag.
type modList []string

func (m *modList) String() string { return strings.Join(*m, ", ") }
func (m *modList) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func main() {
	var mods modList

	root := flag.String("root", "", "game folder to check (the Base or BaseGB directory)")
	dlc := flag.String("dlc", "", "DLC to load, or empty for none (only one may be loaded at a time)")
	flag.Var(&mods, "mod", "mod to load; repeat the flag for each mod (any number may be loaded together)")
	list := flag.Bool("list", false, "list the DLC and mods available under -root, then exit")
	asJSON := flag.Bool("json", false, "emit findings as JSON")
	quiet := flag.Bool("q", false, "only print errors, not warnings")

	// Send -h and flag-parse errors to stdout alongside the help text, so a
	// user redirecting output captures the whole thing.
	flag.CommandLine.SetOutput(os.Stdout)
	flag.Usage = func() { printHelp(os.Stdout) }

	// Run with no arguments at all: show the help rather than acting on
	// defaults the user never chose.
	if len(os.Args) < 2 {
		printHelp(os.Stdout)
		return
	}

	flag.Parse()

	if strings.TrimSpace(*root) == "" {
		fmt.Fprintln(os.Stderr, "sowvalidator: -root is required -- point it at your Base or BaseGB folder.")
		fmt.Fprintln(os.Stderr, "Run sowvalidator with no arguments to see the full help.")
		os.Exit(2)
	}

	if err := run(*root, *dlc, mods, *list, *asJSON, *quiet); err != nil {
		fmt.Fprintf(os.Stderr, "sowvalidator: %v\n", err)
		os.Exit(2)
	}
}

func run(root, dlc string, mods []string, list, asJSON, quiet bool) error {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("game folder %q is not a directory (pass -root)", root)
	}

	if list {
		PrintAvailable(root)
		return nil
	}

	layers, err := BuildLayers(root, dlc, mods)
	if err != nil {
		return err
	}

	rep := report.New(root)

	// ---- packed sprite dictionaries, in layer order ----------------------
	ix := plist.NewIndex()
	for _, l := range layers {
		if l.PackDir == "" {
			continue
		}
		bad, err := ix.LoadDir(l.PackDir)
		if err != nil {
			return fmt.Errorf("scanning %s: %w", l.PackDir, err)
		}
		for _, b := range bad {
			if b.Key == "" {
				rep.Errorf("plist", b.File, 0, "", "could not parse .plist: %v", b.Err)
				continue
			}
			rep.Warnf("plist", b.File, 0, "", "unusable frame key: %v", b.Err)
		}
	}
	if len(ix.Files) == 0 {
		return fmt.Errorf("found no .plist files under %q -- is this a game folder?", root)
	}

	// ---- data files, in layer order --------------------------------------
	// Later layers override earlier ones, matching the engine's walk from index
	// 0 upward, so name sets and sprite geometry built in
	// this order end up with the values the engine would use.
	sets := checks.NewNameSets()
	sprites := checks.NewSpriteSet()

	type loaded struct {
		spec *checks.PackSpec
		file *datacsv.File
	}
	type refFile struct {
		base string
		file *datacsv.File
	}

	var (
		packFiles    []loaded
		modelFiles   []*datacsv.File
		genericFiles []*datacsv.File
		refFiles     []refFile
		dataLayers   int
	)

	// Files with whole-file replacement semantics: only the winning layer.
	replaced := map[string]*datacsv.File{}

	for _, l := range layers {
		if l.DataDir == "" {
			continue
		}
		dataLayers++

		for _, spec := range spriteNameCSVs {
			p := filepath.Join(l.DataDir, spec.Name)
			f, err := datacsv.Load(p)
			if err != nil {
				continue
			}
			short, _ := filepath.Rel(root, p)
			sprites.AddFrom(f, short, spec.AnglesCol, spec.FramesCol, rep)

			for i := range checks.PackSpecs {
				if strings.EqualFold(checks.PackSpecs[i].Name, spec.Name) {
					packFiles = append(packFiles, loaded{spec: &checks.PackSpecs[i], file: f})
				}
			}
		}

		p := filepath.Join(l.DataDir, "unitmodel.csv")
		if f, err := datacsv.Load(p); err == nil {
			short, _ := filepath.Rel(root, p)
			sets.AddUnitModel(f, short)
			modelFiles = append(modelFiles, f)
		}

		for _, base := range checks.GenericFiles {
			p := filepath.Join(l.DataDir, base)
			f, err := datacsv.Load(p)
			if err != nil {
				continue
			}
			if checks.ReplaceFiles[strings.ToLower(base)] {
				// Whole-file replacement: remember only the highest layer,
				// which is the only copy the engine will read.
				replaced[strings.ToLower(base)] = f
			} else {
				genericFiles = append(genericFiles, f)
				refFiles = append(refFiles, refFile{base: base, file: f})
			}

			short, _ := filepath.Rel(root, p)
			switch strings.ToLower(base) {
			case "unittype.csv":
				sets.AddUnitType(f, short)
			case "sfx.csv":
				sets.AddSfx(f, short)
			}
		}
	}

	// Fold in the winning copy of each whole-file-replacement file.
	for base, f := range replaced {
		genericFiles = append(genericFiles, f)
		refFiles = append(refFiles, refFile{base: base, file: f})
	}

	// ---- checks ----------------------------------------------------------
	for _, l := range packFiles {
		checks.PackFrames(l.file, *l.spec, ix, rep)
	}
	for _, f := range modelFiles {
		checks.ModelRefs(f, sprites, rep)
		checks.ModGeometry(f, sprites, rep)
	}
	for _, f := range genericFiles {
		checks.Generic(f, rep)
	}
	for _, r := range refFiles {
		checks.Refs(r.file, r.base, sets, sprites, rep)
	}

	// ---- output ----------------------------------------------------------
	if !asJSON && !quiet {
		fmt.Printf("Checking %s\n", root)
		for _, l := range layers {
			note := ""
			if l.DataDir == "" && l.PackDir == "" {
				note = "   (no data or graphics -- nothing to check)"
			} else if l.DataDir == "" {
				note = "   (graphics only)"
			}
			fmt.Printf("  %-4s %s%s\n", stackMark(l), l.Label(), note)
		}
		fmt.Printf("\n%d .plist file(s), %d sprite pack(s), %d layer(s) with data\n\n",
			len(ix.Files), len(ix.Packs), dataLayers)
	}

	var failed bool
	if asJSON {
		failed = rep.WriteJSON(os.Stdout)
	} else {
		failed = rep.WriteText(os.Stdout, quiet)
	}
	if failed {
		os.Exit(1)
	}
	return nil
}

func stackMark(l Layer) string {
	switch l.Kind {
	case "base":
		return "[1]"
	case "dlc":
		return "[2]"
	default:
		return "[3]"
	}
}
