# SowValidator

A command-line validator for Scourge of War data files.

The engine's loaders are built never to fail. When its parser runs out of
commas it hands back the remainder of the line, and the number conversion turns
whatever it is given into a value — so a malformed row produces a
plausible-looking wrong value instead of an error. `sowvalidator` reads
the same files the same way, then applies the checks the engine does not.

## Download

**[Download sowvalidator.exe](https://github.com/NorbSoftDev/SOWValidator/releases/latest/download/sowvalidator.exe)**
— that link always serves the newest release, and there is a new one for every
change pushed.

A single self-contained binary: no runtime, no DLLs, nothing to install. Put it
anywhere and run it from a command prompt. `sowvalidator -version` says which
build it is, and so does the first line of every report it writes.

Every release also ships `sowvalidator.exe.sha256` if you want to verify the
download:

```
certutil -hashfile sowvalidator.exe SHA256
```

### "Windows protected your PC"

The binary is not code-signed, so Windows SmartScreen will warn the first time
you run it. Click **More info**, then **Run anyway**. If you would rather not,
build it yourself — it takes one command and no dependencies.

## Build from source

```
go build -o sowvalidator.exe .
```

Nothing beyond the Go standard library. Go 1.25+.

## Releasing

Nothing has to be done. Every push to `main` runs `.github/workflows/ci.yml`,
which checks formatting, vets, runs the tests, cross-compiles for
windows/amd64, and publishes the result as the next patch version — `v1.0.3`
becomes `v1.0.4`. The version is stamped into the binary as it is built, so
`sowvalidator -version` and every report it writes name the build they came
from. A pull request is built and tested the same way and publishes nothing.

To start a new minor or major line, tag it by hand:

```
git tag v1.1.0
git push origin v1.1.0
```

`.github/workflows/release.yml` builds and publishes that one. The automatic
numbering then carries on from it, because it always reads the highest existing
tag rather than counting its own releases. The two never both publish the same
commit: the automatic one stands aside when the commit it is building is
already tagged.

Both write a SHA-256 alongside the binary. Binaries are never committed to the
repository.

## Usage

Put it in your game install and start it with no arguments — by double-clicking
it, say — and it finds the `Base` (Waterloo) or `BaseGB` (Gettysburg) folder
beside it, checks the base game there, and writes the report to
`sowvalidator.txt` inside that folder:

```
sowvalidator
```

It looks in the folder it was started from as well as the one it lives in, and
accepts sitting inside the game folder itself. With no game folder to find —
and with `-h` — it prints the full option list with examples instead.

Checking DLC or mods, or sending the report somewhere other than
`sowvalidator.txt`, means passing the options:

```
sowvalidator -root DIR [-dlc NAME] [-mod NAME]... [-json] [-q]
sowvalidator -root DIR -list

  -root DIR   REQUIRED. Game folder to check -- the Base (Waterloo) or
              BaseGB (Gettysburg) directory. This is how you pick the game.
  -dlc NAME   DLC to load, or omit for none. Only one may be loaded at a
              time, matching the game.
  -mod NAME   mod to load. Repeat the flag for each mod; any number may be
              loaded together, and they are applied in the order given.
  -list       list the DLC and mods installed under -root, then exit.
  -version    print the version and exit.
  -json       emit findings as JSON.
  -q          only print errors, not warnings (also suppresses the header,
              so output pipes cleanly).
```

Exit codes: `0` clean, `1` errors found, `2` sowvalidator itself failed. Everything
goes to stdout, so it redirects to a file as-is; diagnostics go to stderr.

```
sowvalidator -root "E:\norbsoftdev\SOWSteam\Base" -list
sowvalidator -root "E:\norbsoftdev\SOWSteam\Base"
sowvalidator -root "E:\norbsoftdev\SOWSteam\Base" -dlc "Ligny" -mod "Sprite Test"
sowvalidator -root "E:\norbsoftdev\SOWSteam\Base" -mod "Grog Gameplay x64" -mod "New Menus" -q > report.txt
```

`-dlc` and `-mod` accept an exact name or any unambiguous substring, so
`-dlc Ligny` resolves `Scourge Of War - Ligny`. An ambiguous fragment is an
error listing the candidates.

## Content layering

Mods and DLC do not replace the base data — they stack on top of it, and a
combination has to be validated as the combination. `sowvalidator` builds the same
stack the engine does:

```
base  ->  DLC (at most one)  ->  mods (any number, in order)
```

The engine walks that stack from the bottom up, so later layers win. Most data
files are read from *every* layer and merged row by row, with a later row
replacing an earlier one of the same key. A few — `courier.csv` and
`statetables.csv` — are read only from the highest layer and replace the file
wholesale.

Two consequences for validation:

- A name defined in a later layer **is not a duplicate** — it is the override
  doing its job. Duplicate reporting is therefore scoped to within a single
  file, never across layers.
- A base file referencing a name that only a mod defines is only valid **with
  that mod loaded**. Checking the base alone reports it as dangling, correctly.
- For the whole-file-replacement files, **only the winning layer is validated**.
  Checking a lower layer's copy would report problems in a file the engine will
  never read.

Each layer's data comes from its `Logistics` folder, or `Data Files` in newer
content; packed dictionaries come from each layer's `Graphics\Packed`.

## Checks

### `pack-frames`

For every row of `unitpack.csv` and `gfxpack.csv`, confirms the packed sprite
actually contains every frame the row declares: frames `First-1` through
`First-1 + (Frames × Angles) - 1` must all be present in the `.plist`
dictionaries.

This is the check the engine gets wrong twice:

1. **Off-by-one.** The engine requires the pack to hold at least
   `begin + Frames × Angles - 1` frames, but then reads up to and including
   frame `begin + Frames × Angles - 1`. A pack holding exactly that many frames
   passes the check and the engine reads one frame past the end. The correct
   bound is `begin + Frames × Angles`, which is what sowvalidator enforces.

2. **Empty slots are invisible.** The engine sizes its frame table from slot 0
   and zero-fills it, then marks only the slots it actually loaded. A slot no
   `.plist` defines therefore reads back as a valid-looking texture rather than
   a missing one, so its own "frame not found" check can never fire for it.
   sowvalidator reports these as warnings.

   Two shapes turn up. A pack whose keys start late — `IWOW0065.png` onwards,
   with `First,65` in the CSV to match — leaves slots 0-63 empty:

   ```
   GFX_NatW_Walk: pack IWOW defines no sprites for slots 0-63, so its first
   sprite is slot 64
   ```

   That is usually harmless, just an allocated-and-unused run of slots, and the
   warning says so: the row that triggered it reads slots 64 and up, which are
   all defined. It matters only if some other row's `First` points into the
   empty range, where it will silently draw the wrong sprite instead of
   failing. Holes punched into the middle of a pack are reported against its
   last slot instead, and are far more likely to be a real mistake.

Also flags `Angles <= 0` (the engine divides 360 by it), `Frames <= 0`,
`First <= 0` (the engine subtracts 1 from it, making it negative), short rows,
and rows containing quote characters.

### `model-refs`

Every action column in `unitmodel.csv` — Walk, Stand, Load, Ready, Fire, Run,
Charge, Melee, Prone, **Death, Death_2** — must name a sprite defined in
`unitpack.csv`, `gfxpack.csv` or `gfx.csv`. A dangling name resolves to nothing
at load and the unit silently has no animation for that action.

`Low Res` is checked more loosely, as a warning, since it may name either a
sprite or another `unitmodel` entry.

### `mod-geometry`

A base sprite and its mod overlays must have identical `Frames × Angles`.

When drawing a figure, the engine computes **one** frame index and applies it
to the base sprite and to both overlays, without re-deriving it per sprite. All
three must therefore share the same frame geometry.

The engine once validated this, but the check is disabled. With it off, a base
and overlay of differing geometry share an index that is only in range for one
of them — the smaller set gets indexed with the larger one's frame number,
reading past the end of its frame data.

This matters most for mounted figures, where the **base is the horse** and the
**overlay is the rider**. A 16×12 horse animation (192 frames) driving a 16×4
rider overlay (64 frames) can index ~128 entries past the end of the rider's
arrays.

Mod columns in `unitmodel.csv` (13 and 14) name *other `unitmodel` entries*, not
sprites — the engine swaps in the overlay's whole sprite set and then indexes
the same state slot. The check therefore compares each
action slot against the same slot in the referenced row, applying the engine's
`Death_2 → Death` fallback.

### `row-shape` and `numeric`

**Nothing is checked this way any more.** Every data file the engine loads now
has a checker written against its own loader; these two remain only for a file
added to `GenericFiles` before its format has been read.

They worked by inference: a column whose values are 98% numeric over 20+
samples was called numeric, and the stragglers reported as typos. That is a
guess, and it can only ever be a guess. It cried wolf on `drills.csv` -- whose
slot-map lines are not rows at all, so a lone `(0-17.5-1)89` sitting in what
the header calls the `AboutFace` column looked like a typo -- while being blind
to the wrong `Rows` values in the same file that strand whole blocks of men.

Where a file's real format is known, the guess is replaced. That is what the
rest of this section is.

### `duplicate-key`

Reports two rows claiming the same key, for the sprite files that still go
through it: `unitpack.csv`, `gfxpack.csv`, `gfx.csv` and `unitmodel.csv`, which
all key on column 0. Every loader resolves a repeat the same way -- delete the
existing entry, keep the new one -- so the earlier definition is silently
discarded.

Every other file now checks its own keys, in the column its own loader reads
them from, which is not column 0 everywhere: a loader that reads past a display
name keys on column 1.

### `drill`, `drill-map` and `drill-ref`

`drills.csv` is not a table, so nothing that treats it as one can say anything
true about it. Each drill is a **definition line** followed by exactly `Rows`
more lines holding its **slot map**, which the loader reads inside a nested
loop, cell by cell, and never considers definitions. Those cells have a grammar
of their own, documented in the file's own notes rows:

```
(rowdist-coldist-sprite-facing-subform-subtype-lock)slot
```

Only as many values as are needed have to be written, so `(10)87` sets a row
distance and nothing else. The parentheses must *precede* the man number.

`Rows` is what holds the file together: it decides where the next drill starts,
so a wrong one shifts every drill below it. These checks therefore walk the file
the way `CForm::Init` does, and then apply that loader's own rules.

- **Record framing.** A line read as a definition that holds slot-map data means
  the drill above it declared too few `Rows`, and the men on that line are never
  placed. A slot-map line that defines a drill means it declared too many, and
  the drill it swallowed is never loaded. A file in the older layout — which
  opened on the drill ID with no `Name` column, so every value lands one column
  from where the loader looks — is reported once, against its header.
- **Slot placement.** Slots placed twice, slots never placed, and slot ids past
  the engine's limit of 200 (`MAXMEN`, `War3D/defines.h:62`), which the loader
  silently drops. That limit is the whole of the rule: the file's own notes
  rows still give the men as `2-125`, which no longer describes this loader —
  the shipped drills place men right up to slot 200 and the engine draws every
  one of them. A drill with fewer
  than two men in it is reported as a hang: `SForm::Max` loops
  `while (i >= imaxmen) i = i - imaxmen + 1`, which never terminates for an
  `imaxmen` below 2, and every slot lookup goes through it.
- **Cell grammar.** An unclosed `(`; a cell carrying no slot number, which the
  loader writes one element *before* the start of the drill's arrays; values
  that are not numbers; and sprite or subtype values outside what the notes rows
  document.
- **Values read and then ignored.** A per-slot row or column distance below zero
  is parsed and then dropped, because the loader applies one only when it is
  above zero. The file says one thing and the game does another.
- **References.** `SubForm`, `ArtyForm` and a cell's `subform` must name a drill
  that exists. Blank is normal and means no sub formation; a name that resolves
  to nothing gets the same result, silently. Forward references are legal, since
  the engine resolves these only once every `drills.csv` has been read.

The ranges on the file's own type row are deliberately **not** enforced. They no
longer describe the loader: it keeps these in a 64-bit fixed-point type with no
clamp anywhere, and the shipped data itself sits outside them — brigade drills
carry a `RowDist` of `350+`, and several carry an `AboutFace` of `2` where the
type row says `[0/1]` and the loader only ever asks whether it is above zero.
Reporting a value the loader is perfectly happy with is how a validator teaches
people to ignore it.

### `unitglobal` and `unitglobal-ref`

`unitglobal.csv` is a table, unlike `drills.csv`, but it is not read like one.
`CSoldCmn::Init` takes only the first two fields and puts the rest of the line
away in a buffer; `CSoldCmn::Load` reads that buffer much later, the first time
something asks for the class. The column meanings therefore live in two
functions, and a fault in the tail of a row surfaces mid-battle rather than at
load.

The row is also mostly runs of same-typed columns — six uniforms, nine sounds,
four menus, then a list of drill ids that runs to the end of the line —
pointing at four different files. Nothing about the shape of a column here says
what belongs in it.

| Column(s) | Checked against | Severity |
|---|---|---|
| 0 `Class` | must be present and unique; the loader keeps the last of a repeat | error |
| 1 `Type` | `unittype.csv` — **blank counts**, the loader looks it up anyway | error, and the game refuses to start |
| 2–3 `Alt Class`, `Captured Class` | another `Class` in this file, case-insensitively | error |
| 4–6 speeds | must be numbers above zero, or the unit cannot move at that speed | error |
| 7–12 `Uniform 1-6` | `unitmodel.csv`, **case-sensitively** | error |
| 13–21 state sounds | `sfx.csv` | warning |
| 22 flag bearer | `unitmodel.csv`; blank counts | error, see below |
| 27+ formations | `drills.csv`; blank means the class has no such formation | error |

Three of those are worth spelling out.

**The flag bearer is a crash, not a missing sprite.** `Lookup`
(`Shared/vechelp.inl:44`) leaves its out parameter untouched when the name
misses, and `SSoldCmn` has no constructor, so `m_fsprite` keeps whatever was on
the heap. `FindClass` then dereferences it with no null check. The six uniform
slots do not have this problem — the loader sets each to `NULL` before looking
it up — which is why a blank or wrong flag bearer is reported as an error in
its own right rather than as one more dangling reference.

**`unitglobal` → `unitmodel` is case-sensitive.** Neither the stored key nor
the lookup string is upper-cased (`War3D/soldcmn.cpp:137`), so a case-only typo
silently yields no sprite. That case is reported separately from a name that
does not exist at all, since it is a much easier fix — and it is the one that
looks right on the page.

**The formation columns are a list, not a fixed set.** The loader reads at
least ten drill ids and then keeps reading for as long as the line has fields
left, so the check follows the row to its end rather than stopping at the last
labelled column. Most rows are short, which is how the file says a class has no
such formation, so only a name that fails to resolve is reported.

Ranges are not enforced anywhere here, for the same reason as in `drills`: the
loader imposes none, and what it really does with a bad value — reads it as
zero, drops it, or dereferences a pointer it never set — is the thing worth
reporting.

### Files written before the format changed

Three files carry a layout the current loader no longer reads. In each case
every value lands some columns from where it is looked for, so the file is
misread end to end and nothing said about its rows would be true:

| File | Older layout | Detected by |
|---|---|---|
| `drills.csv` | opened on the drill ID, with no `Name` column | `Rows` not in column 2 |
| `unitglobal.csv` | two speeds rather than three, no `Mid Speed` | `Uniform 1` not in column 7 |
| `sfx.csv` | no `ID` column, so sounds key on their `.wav` filename | `File` not in column 2 |
| `artillery.csv` | no `MROF` column after the rate of fire | `ArtyTableID` not in column 15 |
| `replroster.csv` | a list of ranks and names, not a side/army index | no `Side` column |
| `gscreens/mscreens.csv` | one fewer column between the font and the position | `X Coord` not in column 6 |
| OOB files | an `OOBMOD` column after `CLASS` that nothing reads | `Weapon` not in column 12 |
| `scenario.csv` | no `BTN` column after `REG` | `Formation` not in column 14 |
| `battlescript.csv` | no `FromId` column after `Command` | `X Coord` not in column 4 |
| `unitattributes.csv` | tables opened by name, with no `NEW` row | no `NEW` row anywhere |

The `OOBMOD` one is worth singling out, because a great many community mods
carry it. Nothing in the engine reads that column: `COOB::Init` takes three
columns after the six rank ones and calls them class, portrait and weapon, so
in such a file it reads `OOBMOD` as each unit's portrait and the portrait as
its weapon, and every column to the end of the row is one place out.

Each is reported once, against the file's header, and the rest of that file is
left alone. Where such a file would have contributed names that other files
reference, the checks that resolve those names report **how many they had to
leave unjudged** rather than calling them undefined — a name set short of a
whole file cannot prove anything is missing from it.

### `sprite-duplicate`

Reports sprites defined more than once. The engine logs this too
 but the message scrolls past at startup; the later
definition wins and the earlier is deleted.

## How it reads the data

- **Row filter** matches the engine: line 1 is the header, and any line that is
  empty or begins with a comma is skipped (,).
  That is how the CSVs carry their documentation rows.
- **One quoting rule**, matching the engine's and no more. `CUtil::ParseFind`
  honours a quote only where a field begins, and then looks for the delimiter
  after the closing quote; a quote anywhere else is an ordinary character, and
  every quote is stripped from the value. Splitting any other way would
  validate a file the engine never sees.
- **`.plist` keys** are split exactly as does: the last 8
  characters are assumed to be `NNNN.png`, so `UO01D0001.png` yields pack
  `UO01D`, frame `0` (the engine subtracts 1 to make it 0-based).
- **Packed dictionaries** are collected from each selected layer's
  `Graphics\Packed`, in load order.

## Layout

```
main.go                     CLI, data-directory discovery, orchestration
internal/datacsv            engine-compatible CSV reader with line numbers
internal/plist              .plist parser -> pack name -> frame set
internal/checks             the checks themselves
internal/report             findings, severity, text and JSON output
```

## What is checked

Every CSV the engine loads, each against its own loader.

**Logistics** — `artillery`, `artytables`, `courier`, `drills`, `efx`,
`gamefonts`, `gfx`, `gfxpack`, `gscreens`, `mscreens`, `munitions`,
`replroster`, `rifles`, `sfx`, `statetables`, `unitattributes`, `unitglobal`,
`unitmodel`, `unitpack`, `unittype`.

**Maps** — every `.csv` in each layer's `Maps` folder, section by section.

**Scenarios** — every scenario folder, because every one of them is a scenario
the player can pick: its `scenario.csv` joined to the master order of battle it
names, its `maplocations.csv`, its `battlescript.csv` checked against both, and
its `casfx.csv`.

**OOBs** — every `.csv` in each layer's `OOBs` folder.

Loose files a data row names are checked too, where the engine finds them by
walking one folder of each layer: a sound's `.wav` in `Sounds`, a font's `.pft`
in `Graphics\Fonts`, a unit type's AI library in `Modules`. An install whose
content lives in `.sow` catalogue archives switches that off rather than report
files it cannot see as missing.

## Not yet covered

- `.layout` files (MyGUI) referencing names in the CSVs
- `.ini` files: `battledef.ini`, `defines.ini`, and each map's and scenario's
- The Campaign folder's own `battlescript.csv` and `maplocations.csv`, which are
  read by a different loader from the scenario ones
- What each screen control does with its `Graphic` column, which differs by
  control type -- a texture file for one, a display tag for another, a sprite
  for the rest
- Bounds tied to engine constants (`LEN_NAME` 256, `MAXMEN` 200), which should
  be generated from the C++ headers rather than hardcoded
- `commands.go` and `lists.go` are transcribed from the engine's own tables. If
  a command or a built-in courier list is added to the engine, add it there too
  or a script using it reads as unknown.
