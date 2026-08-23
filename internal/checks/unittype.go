package checks

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// unittype.csv columns, in the order CSoldCmn::Init (War3D/soldcmn.cpp:148)
// reads them.
//
// Note utWeight2 and utWeight3. The loader reads them as the cavalry weight
// and then the artillery weight; the file's header labels them the other way
// round. That disagreement is reported once per file rather than assumed away
// in either direction -- see checkUnitTypeWeightLabels.
const (
	utTypeName = iota // the key, upper-cased
	utType            // the unit type number
	utAiDll           // a .dll in Modules\, loaded at startup
	utAiFunc          // an exported symbol in that dll
	utWeight1         // read as the infantry weight
	utWeight2         // read as the cavalry weight
	utWeight3         // read as the artillery weight
	utAutoRoutePct
	utTicsReacquire
	utMarchTargYds
	utCanCharge
	utCanChargeRetreat
	utMeleeIndex
	utMeleeMntIndex
	utFatigueIndex
	utFatigueMntLimIndex
	utMountedStopOnDefTerrain
	utBaseVolleyYards
	utColumns
)

// utTypeNames are the unit type numbers the engine switches on, from
// EUnitType. Anything outside them is a unit the engine has no behaviour for.
var utTypeNames = map[int]string{
	1: "infantry",
	2: "cavalry",
	3: "artillery",
	4: "ammunition",
	5: "courier",
}

// UnitType validates unittype.csv.
func UnitType(f *datacsv.File, assets *Assets, rep *report.Report) {
	const check = "unittype"

	checkUnitTypeWeightLabels(f, rep)

	seen := map[string]int{}
	for _, row := range f.Rows {
		if isSubHeader(f, row) {
			continue
		}

		name := row.Field(utTypeName)
		if name == "" {
			rep.Errorf(check, f.Path, row.Line, "row: "+trim(row.Raw),
				"row has no TypeName -- it is keyed on the empty string, and every unitglobal.csv row naming a type that does not exist would resolve to it")
			continue
		}
		if prev, dup := seen[strings.ToUpper(name)]; dup {
			rep.Errorf(check, f.Path, row.Line, fmt.Sprintf("first defined at line %d", prev),
				"duplicate TypeName %q -- the loader deletes the earlier type and keeps this one", name)
		} else {
			seen[strings.ToUpper(name)] = row.Line
		}

		checkUnitTypeNumber(f, row, name, rep)
		checkUnitTypeAI(f, row, name, assets, rep)
	}
}

// checkUnitTypeNumber validates the type number the engine branches on.
func checkUnitTypeNumber(f *datacsv.File, row datacsv.Row, name string, rep *report.Report) {
	const check = "unittype"

	raw := row.Field(utType)
	label := colLabel(f, utType, "Type")

	if raw == "" {
		rep.Errorf(check, f.Path, row.Line, "",
			"%s: %s is blank -- the loader reads it as 0, which is no unit type at all", name, label)
		return
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		rep.Errorf(check, f.Path, row.Line, "",
			"%s: %s is %q, not a whole number -- the loader reads it as %d", name, label, raw, datacsv.Atoi(raw))
		return
	}
	if _, ok := utTypeNames[v]; !ok {
		rep.Errorf(check, f.Path, row.Line,
			"the engine knows 1 infantry, 2 cavalry, 3 artillery, 4 ammunition, 5 courier",
			"%s: %s is %d, which is not a unit type the engine has any behaviour for", name, label, v)
	}
}

// checkUnitTypeAI validates the two columns that reach outside the data
// entirely: the AI library and the symbol looked up inside it.
//
// Both are loaded at startup for every row, whether or not anything uses the
// type, and each failure is only a log line -- a type whose AI never resolved
// simply stands still.
func checkUnitTypeAI(f *datacsv.File, row datacsv.Row, name string, assets *Assets, rep *report.Report) {
	const check = "unittype"

	dll := row.Field(utAiDll)
	fn := row.Field(utAiFunc)

	if dll == "" {
		if fn != "" {
			rep.Errorf(check, f.Path, row.Line, "",
				"%s: %s names %q but %s is blank -- there is no library to look it up in",
				name, colLabel(f, utAiFunc, "AiFunc"), fn, colLabel(f, utAiDll, "AiDll"))
		}
		return
	}
	if !assets.Has(DirModules, dll, ".dll") {
		rep.Errorf(check, f.Path, row.Line,
			"the engine logs \"Could not find ai dll\" and the type gets no AI at all",
			"%s: %s %q is not in the Modules folder of any loaded layer", name, colLabel(f, utAiDll, "AiDll"), dll)
		return
	}
	if fn == "" {
		rep.Errorf(check, f.Path, row.Line,
			"the engine logs \"Could not find SowAIFunc\" and the type gets no AI at all",
			"%s: %s is blank -- the library is loaded but no function is looked up in it", name, colLabel(f, utAiFunc, "AiFunc"))
	}
}

// checkUnitTypeWeightLabels reports the file's header disagreeing with the
// loader about which arm the two weight columns belong to.
//
// CSoldCmn::Init reads columns 4, 5 and 6 into the infantry, cavalry and
// artillery weights in that order (War3D/soldcmn.cpp:222-224). The shipped
// header labels them InfWeight, ArtyWeight, CavWeight. One of the two is
// wrong, and the AI weights an enemy's strength by whichever the loader
// believes, so the column carrying the outlying value is applied to the wrong
// arm.
//
// This is one fact about the file, not one about each of its rows.
func checkUnitTypeWeightLabels(f *datacsv.File, rep *report.Report) {
	const check = "unittype"

	second := strings.ToLower(f.Header.Field(utWeight2))
	third := strings.ToLower(f.Header.Field(utWeight3))
	if second == "" || third == "" {
		return
	}

	secondSaysCav := strings.Contains(second, "cav")
	thirdSaysArt := strings.Contains(third, "art")
	if secondSaysCav && thirdSaysArt {
		return // header and loader agree
	}
	if !strings.Contains(second, "art") && !strings.Contains(third, "cav") {
		return // labelled as something else entirely; nothing to compare
	}

	rep.Errorf(check, f.Path, 1, "header: "+trim(f.Header.Raw),
		"the header labels column %d %q and column %d %q, but the loader reads them as the cavalry weight and then the artillery weight -- the AI weights an enemy's strength by the loader's order, so whichever of these two carries the outlying value is being applied to the other arm",
		utWeight2, f.Header.Field(utWeight2), utWeight3, f.Header.Field(utWeight3))
}
