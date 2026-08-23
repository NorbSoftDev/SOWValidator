package checks

import (
	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// Context carries everything the per-file checks share: the name sets a
// reference resolves against, the sprite and loose-file indexes, and the
// report to write into.
type Context struct {
	Sets    *NameSets
	Sprites *SpriteSet
	Assets  *Assets
	Report  *report.Report
}

// DataFile is one data file read the way its own loader reads it.
//
// Collect runs for every layer's copy before any Check does, so a reference
// resolves against a name defined further down its own file or in another
// layer -- which is legal, because the engine resolves these only once every
// copy of every file has been read.
type DataFile struct {
	Name    string
	Collect func(c *Context, f *datacsv.File, short string)
	Check   func(c *Context, f *datacsv.File)
}

// DataFiles are the files that have a checker written against their loader.
// Anything here must not also appear in GenericFiles: the shape-inference
// checks there can only guess, and a file whose real format is known has no
// use for a guess.
//
// The order does not matter -- collection and checking are separate passes.
var DataFiles = []DataFile{
	{
		Name:    "unittype.csv",
		Collect: func(c *Context, f *datacsv.File, short string) { c.Sets.AddUnitType(f, short) },
		Check:   func(c *Context, f *datacsv.File) { UnitType(f, c.Assets, c.Report) },
	},
	{
		Name:    "sfx.csv",
		Collect: func(c *Context, f *datacsv.File, short string) { c.Sets.AddSfx(f, short, c.Report) },
		Check:   func(c *Context, f *datacsv.File) { Sfx(f, c.Sets, c.Assets, c.Report) },
	},
	{
		Name:    "drills.csv",
		Collect: func(c *Context, f *datacsv.File, short string) { c.Sets.AddDrills(f, short) },
		Check:   func(c *Context, f *datacsv.File) { Drills(f, c.Sets, c.Report) },
	},
	{
		Name:    "unitglobal.csv",
		Collect: func(c *Context, f *datacsv.File, short string) { c.Sets.AddUnitGlobalClasses(f, short) },
		Check:   func(c *Context, f *datacsv.File) { UnitGlobal(f, c.Sets, c.Report) },
	},
	{
		Name:    "munitions.csv",
		Collect: func(c *Context, f *datacsv.File, short string) { c.Sets.AddAmmo(f, short) },
		Check:   func(c *Context, f *datacsv.File) { Munitions(f, c.Sets, c.Sprites, c.Report) },
	},
	{
		Name:    "artytables.csv",
		Collect: func(c *Context, f *datacsv.File, short string) { c.Sets.AddArtyTables(f, short) },
		Check:   func(c *Context, f *datacsv.File) { ArtyTables(f, c.Report) },
	},
	{
		Name:    "rifles.csv",
		Collect: func(c *Context, f *datacsv.File, short string) { c.Sets.AddWeapons(f, short) },
		Check:   func(c *Context, f *datacsv.File) { Weapons(f, false, c.Sets, c.Report) },
	},
	{
		Name:    "artillery.csv",
		Collect: func(c *Context, f *datacsv.File, short string) { c.Sets.AddWeapons(f, short) },
		Check:   func(c *Context, f *datacsv.File) { Weapons(f, true, c.Sets, c.Report) },
	},
	{
		Name:    "efx.csv",
		Collect: func(c *Context, f *datacsv.File, short string) { c.Sets.AddEffects(f, short) },
		Check:   func(c *Context, f *datacsv.File) { Effects(f, c.Sprites, c.Report) },
	},
	{
		Name:    "gamefonts.csv",
		Collect: func(c *Context, f *datacsv.File, short string) { c.Sets.AddFonts(f, short) },
		Check:   func(c *Context, f *datacsv.File) { GameFonts(f, c.Assets, c.Report) },
	},
	{
		Name:  "replroster.csv",
		Check: func(c *Context, f *datacsv.File) { ReplRoster(f, c.Report) },
	},
	{
		Name:  "gscreens.csv",
		Check: func(c *Context, f *datacsv.File) { Screens(f, c.Sets, c.Report) },
	},
	{
		Name:  "mscreens.csv",
		Check: func(c *Context, f *datacsv.File) { Screens(f, c.Sets, c.Report) },
	},
	{
		Name:  "courier.csv",
		Check: func(c *Context, f *datacsv.File) { Courier(f, c.Report) },
	},
	{
		Name:  "unitattributes.csv",
		Check: func(c *Context, f *datacsv.File) { UnitAttributes(f, c.Report) },
	},
	{
		Name:  "statetables.csv",
		Check: func(c *Context, f *datacsv.File) { StateTables(f, c.Report) },
	},
}

// AssetDirs are the layer subdirectories indexed for the loose files a data
// row can name.
var AssetDirs = []string{DirSounds, DirFonts, DirModules, DirMaps, DirOOBs, DirScen}
