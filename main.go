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
//	sowvalidator
//
// Run with no arguments it looks for a Base or BaseGB folder beside itself,
// checks that, and writes the report to sowvalidator.txt inside it, so the
// program can simply be dropped into a game install and started.
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
  sowvalidator

NO ARGUMENTS
  Started with no arguments -- by double-clicking it, say -- sowvalidator
  looks for a Base or BaseGB folder next to itself or in the folder it was
  started from, checks the base game there, and writes the report to
  sowvalidator.txt inside that folder. With no such folder to find, it
  prints this help instead.

  To check DLC or mods, pass the options below.

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
  Check the game sowvalidator is sitting next to, report to sowvalidator.txt:
    sowvalidator

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

	// Run with no arguments at all: check the game folder we are sitting in
	// or beside, writing the report to a file there. That is the whole
	// interface for someone who drops the program into their install and
	// double-clicks it. With no game folder to find there is nothing to act
	// on, so show the help.
	if len(os.Args) < 2 {
		root := findGameFolder()
		if root == "" {
			printHelp(os.Stdout)
			return
		}
		failed, err := runToFile(root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "sowvalidator: %v\n", err)
			os.Exit(2)
		}
		if failed {
			os.Exit(1)
		}
		return
	}

	flag.Parse()

	if strings.TrimSpace(*root) == "" {
		fmt.Fprintln(os.Stderr, "sowvalidator: -root is required -- point it at your Base or BaseGB folder.")
		fmt.Fprintln(os.Stderr, "Run sowvalidator -h to see the full help.")
		os.Exit(2)
	}

	rep, err := run(os.Stdout, *root, *dlc, mods, *list, *asJSON, *quiet)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sowvalidator: %v\n", err)
		os.Exit(2)
	}
	if rep.Count(report.Error) > 0 {
		os.Exit(1)
	}
}

// reportName is the file a no-argument run writes into the game folder.
const reportName = "sowvalidator.txt"

// gameFolderNames are the two game roots: Waterloo installs to Base,
// Gettysburg to BaseGB.
var gameFolderNames = []string{"Base", "BaseGB"}

// findGameFolder picks the game folder for a run with no arguments: a Base or
// BaseGB folder beside the program or in the working directory, or either of
// those directories itself if the program was put inside the game folder. It
// returns "" when there is nothing that looks like a game folder.
func findGameFolder() string {
	var dirs []string
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, wd)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe))
	}

	// A folder holding the game wins over a folder that is the game: sitting
	// beside the install is the ordinary case, sitting inside it the fallback.
	for _, d := range dirs {
		for _, name := range gameFolderNames {
			if sub := findSub(d, name); sub != "" {
				return sub
			}
		}
	}
	for _, d := range dirs {
		for _, name := range gameFolderNames {
			if strings.EqualFold(filepath.Base(d), name) {
				return d
			}
		}
	}
	return ""
}

// runToFile checks the base game in root and writes the full report to
// sowvalidator.txt there. The console gets only where the report went and how
// it came out, which is all there is to read when the window closes on exit.
func runToFile(root string) (bool, error) {
	out := filepath.Join(root, reportName)
	f, err := os.Create(out)
	if err != nil {
		return false, fmt.Errorf("cannot write %s: %w", out, err)
	}

	fmt.Printf("Checking %s\n", root)
	rep, runErr := run(f, root, "", nil, false, false, false)
	if runErr != nil {
		// The console window is gone the moment this exits, so leave the
		// reason in the file the user was told to look in.
		fmt.Fprintf(f, "sowvalidator: %v\n", runErr)
	}
	if cerr := f.Close(); cerr != nil && runErr == nil {
		runErr = fmt.Errorf("writing %s: %w", out, cerr)
	}
	if runErr != nil {
		return false, runErr
	}

	errs, warns := rep.Count(report.Error), rep.Count(report.Warn)
	fmt.Printf("%d error(s), %d warning(s) -- full report in %s\n", errs, warns, out)
	return errs > 0, nil
}

// run performs the checks and renders the findings to out. It returns the
// report so the caller can decide what to do about the counts, and an error
// only when the check could not be made at all.
func run(out io.Writer, root, dlc string, mods []string, list, asJSON, quiet bool) (*report.Report, error) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("game folder %q is not a directory (pass -root)", root)
	}

	if list {
		PrintAvailable(root)
		return report.New(root), nil
	}

	layers, err := BuildLayers(root, dlc, mods)
	if err != nil {
		return nil, err
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
			return nil, fmt.Errorf("scanning %s: %w", l.PackDir, err)
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
		return nil, fmt.Errorf("found no .plist files under %q -- is this a game folder?", root)
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
		fmt.Fprintf(out, "Checking %s\n", root)
		for _, l := range layers {
			note := ""
			if l.DataDir == "" && l.PackDir == "" {
				note = "   (no data or graphics -- nothing to check)"
			} else if l.DataDir == "" {
				note = "   (graphics only)"
			}
			fmt.Fprintf(out, "  %-4s %s%s\n", stackMark(l), l.Label(), note)
		}
		fmt.Fprintf(out, "\n%d .plist file(s), %d sprite pack(s), %d layer(s) with data\n\n",
			len(ix.Files), len(ix.Packs), dataLayers)
	}

	if asJSON {
		rep.WriteJSON(out)
	} else {
		rep.WriteText(out, quiet)
	}
	return rep, nil
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
