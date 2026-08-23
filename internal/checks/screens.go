package checks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// gscreens.csv and mscreens.csv are one format read by one loader
// (CScreens::Init, War3D/screen.cpp:93): a row reading NEW opens a screen, and
// every row after it is one control on that screen until the next NEW.
//
// The first column picks which kind of control to build. CScrObj::Create
// (War3D/scrobj.cpp:320) is a list of thirty-odd names, and anything not on it
// is logged and dropped -- the control simply is not on the screen. Every kind
// then reads the same columns through CScrObj::InitCtrl, buttons included:
// they look like an exception, because the loader puts their type back on the
// front of the row, but their own InitCtrl takes it off again.

// Screen control columns, in the order CScrObj::InitCtrl reads them. The
// position is a rectangle, which costs four columns, and the source a point,
// which costs two.
const (
	srType = iota
	srID
	srGraphic // read into a string; what it names depends on the control
	srFont    // a gamefonts.csv ID
	srTooltip
	srIndex
	srX
	srY
	srWidth
	srHeight
	srSrcX
	srSrcY
	srDrawCond
	srExecCond
	srFunction
	srDepends1
	srColumns
)

// screenTypes are the control names CScrObj::Create knows.
var screenTypes = map[string]bool{
	"new": true, "graphic": true, "setvar": true, "text": true, "edit": true,
	"hedit": true, "pass": true, "spin": true, "list": true, "listmenu": true,
	"checklist": true, "listcol": true, "frame": true, "listtext": true,
	"progbar": true, "map": true, "cmdmap": true, "courmap": true,
	"basefont": true, "highfont": true, "varfont": true, "mapicons": true,
	"format": true, "radio": true, "check": true, "button": true,
	"defbutton": true, "canbutton": true, "scroll": true, "thumb": true,
	"pict": true, "command": true,
}

// Screens validates gscreens.csv or mscreens.csv.
func Screens(f *datacsv.File, sets *NameSets, rep *report.Report) {
	const check = "screens"

	if !checkLayout(f, screensLayout, check, rep) {
		return
	}

	screens := map[string]int{}
	open := ""

	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		kind := strings.ToLower(row.Field(srType))
		if kind == "" {
			continue
		}

		if !screenTypes[kind] {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"the engine logs \"unknow obj %s\" and leaves this control off the screen", row.Field(srType))
			continue
		}

		if kind == "new" {
			name := row.Field(srID)
			if name == "" {
				rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
					"screen has no ID -- it is keyed on the empty string, and nothing can open it")
				open = ""
				continue
			}
			if prev, dup := screens[strings.ToUpper(name)]; dup {
				rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
					"duplicate screen %q -- the loader deletes the earlier screen and keeps this one, controls and all", name)
			} else {
				screens[strings.ToUpper(name)] = row.Line
			}
			open = name
			continue
		}

		// Every other row belongs to the screen above it. With none open the
		// loader still builds the control and hangs it on whatever screen it
		// last had, which is nothing at all on the first row of a file.
		if open == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"this control comes before any NEW row, so there is no screen for it to belong to")
			continue
		}

		checkScreenRefs(f, row, open, sets, rep)
		checkScreenRect(f, row, open, rep)
	}
}

// checkScreenRefs validates the font.
//
// The Graphic column beside it is deliberately left alone. CScrObj::InitCtrl
// only reads it into a string, and what each kind of control then does with it
// differs: a MAPICONS names a texture file, a TEXT names a display tag, others
// name a sprite. Checking them all against the sprite set reported the first
// two as missing, which they are not.
func checkScreenRefs(f *datacsv.File, row datacsv.Row, screen string, sets *NameSets, rep *report.Report) {
	const check = "screens-ref"

	id := row.Field(srID)
	if id == "" {
		id = row.Field(srType)
	}

	// The font column is not a bare id: CFonts::FontStr (War3D/fonts.cpp:222)
	// splits it on dashes into a name, an alignment and three colour values,
	// and only the first of those is looked up. A control with no font at all
	// takes the screen's, so blank is normal here.
	if ref := row.Field(srFont); ref != "" {
		name := strings.TrimSpace(strings.SplitN(ref, "-", 2)[0])
		if name == "" {
			return
		}
		if _, ok := sets.Font[strings.ToUpper(name)]; !ok {
			rep.Warnf(check, f.Path, row.Line,
				"the engine leaves the control with no font, and it is drawn in whatever the screen last used",
				"%s: %s: %s names font %q, which is not defined in gamefonts.csv",
				screen, id, colLabel(f, srFont, "Font"), name)
		}
	}
}

// checkScreenRect validates the four position columns, which are read as a
// rectangle. A control whose width or height reads as nothing is a control
// nobody can see or click.
func checkScreenRect(f *datacsv.File, row datacsv.Row, screen string, rep *report.Report) {
	const check = "screens"

	for _, c := range []struct {
		col      int
		fallback string
	}{
		{srX, "X Coord"}, {srY, "Y Coord"}, {srWidth, "Width"}, {srHeight, "Height"},
		{srSrcX, "X Source"}, {srSrcY, "Y Source"},
	} {
		raw := row.Field(c.col)
		if raw == "" {
			continue
		}
		if _, err := strconv.Atoi(raw); err != nil {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s: %s is %q, not a whole number -- the loader reads it as %d",
				screen, row.Field(srID), colLabel(f, c.col, c.fallback), raw, datacsv.Atoi(raw))
		}
	}
}
