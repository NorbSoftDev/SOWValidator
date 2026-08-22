# SowValidator

A command-line validator for Scourge of War data files.

The engine's loaders are built never to fail. When its parser runs out of
commas it hands back the remainder of the line, and the number conversion turns
whatever it is given into a value — so a malformed row produces a
plausible-looking wrong value instead of an error. `sowvalidator` reads
the same files the same way, then applies the checks the engine does not.

## Download

**[Download sowvalidator.exe](https://github.com/NorbSoftDev/SOWValidator/releases/latest/download/sowvalidator.exe)**
— that link always serves the newest release.

A single self-contained binary: no runtime, no DLLs, nothing to install. Put it
anywhere and run it from a command prompt.

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

Releases are built by GitHub Actions from the tagged commit, so the published
binary always matches the source at that tag:

```
git tag v1.0.0
git push origin v1.0.0
```

`.github/workflows/release.yml` cross-compiles for windows/amd64, writes a
SHA-256, and attaches both to the release. Binaries are never committed to the
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

2. **Gaps are invisible.** The engine sizes its frame table with zeroes and only
   marks the frames it actually loaded. A frame missing from the middle of a
   pack therefore reads as a valid-looking texture slot rather than a missing
   one, so its own "frame not found" check can never fire for it. sowvalidator
   reports these as warnings.

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

Structural validation for the remaining core data CSVs (`artillery`,
`artytables`, `courier`, `drills`, `efx`, `gamefonts`, `gscreens`, `mscreens`,
`munitions`, `replroster`, `rifles`, `sfx`, `statetables`, `unitattributes`,
`unitglobal`, `unittype`). None of these have a documented schema, so the shape
is inferred from each file's own contents rather than hardcoded.

- **`row-shape`** reports rows with *fewer* fields than the file's modal width.
  Extra trailing fields are ignored — trailing commas are endemic in these files
  and the loader never reads them. Short rows are the dangerous case.
- **`numeric`** reports isolated non-numeric values in an otherwise numeric
  column.

These files are messy by nature: they carry documentation rows, stack several
tables in one file with repeated sub-headers, and use deliberately non-numeric
syntax such as `(0-47)87` in `drills.csv`. The thresholds are set accordingly —
a column needs 20+ samples and 98% numeric agreement, and at most 2 outliers
before the column is assumed to simply permit that syntax. Rows matching 3+
header labels are treated as sub-headers and skipped. Untuned, these checks
produced over 1,200 false positives.

### `duplicate-key`

Reports two rows claiming the same key. Every loader resolves this the same
way — delete the existing entry, keep the new one — so the earlier definition
is silently discarded.

The key column is **not** column 0 everywhere. It is whichever field the loader
last reads before using it as a lookup key, so a file whose loader skips past
the first field keys on column 1. Determined per file:

| Key column 0 | Key column 1 |
|---|---|
| `unitpack.csv` | `artillery.csv` |
| `gfxpack.csv` | `rifles.csv` |
| `gfx.csv` | `sfx.csv` |
| `unitmodel.csv` | `efx.csv` |
| `unitglobal.csv` | |
| `unittype.csv` | |
| `munitions.csv` | |

Only files whose loader performs **one map-insert per row** are checked — that
is the only shape where a repeated key is unambiguously a mistake. Excluded,
because a repeated key is normal in them:

- `artytables.csv` — one row per experience level within a table, so
  `ArtyTableID` repeats by design.
- `drills.csv` — rows carry sub-slot data past the formation header, so
  column 1 is not a per-row unique id in the actual file.
- `statetables.csv` — sectioned state driven by blank separator rows
.
- `replroster.csv` — a 2D side/army index, not a name table
.
- `gscreens.csv`, `mscreens.csv`, `courier.csv`, `unitattributes.csv` —
  record-type files where column 0 is a record type and only `NEW` rows open a
  record; rows between belong to the record above them.

Those need per-file record-structure modelling rather than a key column.

### `xref`

Cross-file references, checked only where the resolution was confirmed in a
loader. Guessing what a column points at is how false positives get
reintroduced, so unverified relationships are left out.

From `unitglobal.csv`:

| Column(s) | References | Severity |
|---|---|---|
| 1 `TypeID` | `unittype.csv` column 0 | error — engine sets a fatal error |
| 7–12 `Uniform 1-6` | `unitmodel.csv` column 0 | error |
| 13–21 state sounds | `sfx.csv` column 1 | warning |
| 22 flag bearer | `unitmodel.csv` column 0 | error |

From `munitions.csv` (`the ammunition loader`):

| Column | References | Severity |
|---|---|---|
| 1 `Sprite Graphic` | any defined sprite | error |
| 13 sound | `sfx.csv` column 1 | warning |

**`unitglobal` → `unitmodel` is case-sensitive.** The engine calls
an exact string comparison, and neither the stored key
 nor the lookup string is upper-cased, so a case-only
typo silently yields no sprite. The check reports that case separately from a
name that does not exist at all, since it is a much easier fix.

Name sets are the union across every selected layer, mirroring how the engine
layers base, DLC and mod content.

### `sprite-duplicate`

Reports sprites defined more than once. The engine logs this too
 but the message scrolls past at startup; the later
definition wins and the earlier is deleted.

## How it reads the data

- **Row filter** matches the engine: line 1 is the header, and any line that is
  empty or begins with a comma is skipped (,).
  That is how the CSVs carry their documentation rows.
- **No quote handling**, deliberately. The engine splits on raw commas and has
  no concept of quoting, so honouring quotes here would validate something the
  engine never sees. Quotes in data rows are reported instead.
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

## Not yet covered

- `.layout` files (MyGUI) referencing names in the CSVs
- The other core data files
- Scenarios, OOBs, Maps, Campaign
- Bounds tied to engine constants (`LEN_NAME` 256, `MAXMEN` 200), which should
  be generated from the C++ headers rather than hardcoded
