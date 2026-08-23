package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorbSoftDev/SOWValidator/internal/datacsv"
	"github.com/NorbSoftDev/SOWValidator/internal/report"
)

// load writes a file and reads it back the way the validator does. The name
// matters: several checks look at it, and all of them look at the header.
func load(t *testing.T, name, body string) *datacsv.File {
	t.Helper()

	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := datacsv.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func newReport() *report.Report { return report.New("") }

// ---- rifles.csv and artillery.csv -----------------------------------------

const artilleryHeader = "Name,ID,Type ID,Min Range (yds),Optimal (yds),Typical (yds),Long (yds)," +
	"Max Range (yds),Misfires,Rate of Fire Factor,MROF,Can Pct,Shell Pct,Shrap Pct,Solid Pct," +
	"ArtyTableID,Can ID,Shell ID,Shrap ID,Solid ID"

func artillerySets() *NameSets {
	sets := NewNameSets()
	sets.Ammo["AM_CAN"] = "munitions.csv:2"
	sets.ArtyTable["AT_TEST"] = "artytables.csv:2"
	return sets
}

func TestWeaponsClean(t *testing.T) {
	f := load(t, "artillery.csv", artilleryHeader+"\n"+
		"Gun,W_Test,ammo,100,200,300,400,500,1,20,15,25,25,25,25,AT_Test,AM_Can,AM_Can,AM_Can,AM_Can\n")
	rep := newReport()
	Weapons(f, true, artillerySets(), rep)
	wantClean(t, rep)
}

// The engine takes the first band whose range covers the distance, so a band
// that does not rise above the one before it is never reached.
func TestWeaponsRangesOutOfOrder(t *testing.T) {
	f := load(t, "artillery.csv", artilleryHeader+"\n"+
		"Gun,W_Test,ammo,100,200,150,400,500,1,20,15,25,25,25,25,AT_Test,AM_Can,AM_Can,AM_Can,AM_Can\n")
	rep := newReport()
	Weapons(f, true, artillerySets(), rep)
	wantFinding(t, rep, 2, "below the range before it")
}

func TestWeaponsUnknownRound(t *testing.T) {
	f := load(t, "artillery.csv", artilleryHeader+"\n"+
		"Gun,W_Test,ammo,100,200,300,400,500,1,20,15,25,25,25,25,AT_Test,AM_Nope,AM_Can,AM_Can,AM_Can\n")
	rep := newReport()
	Weapons(f, true, artillerySets(), rep)
	wantFinding(t, rep, 2, `canister round "AM_Nope"`)
}

func TestWeaponsNoAmmunitionAtAll(t *testing.T) {
	f := load(t, "artillery.csv", artilleryHeader+"\n"+
		"Gun,W_Test,ammo,100,200,300,400,500,1,20,15,0,0,0,0,AT_Test,AM_Can,AM_Can,AM_Can,AM_Can\n")
	rep := newReport()
	Weapons(f, true, artillerySets(), rep)
	wantFinding(t, rep, 2, "issued no rounds")
}

// The older artillery.csv has no MROF column, so everything after the rate of
// fire is one place out. That is one finding about the file.
func TestWeaponsOlderLayout(t *testing.T) {
	f := load(t, "artillery.csv",
		"Name,ID,Type ID,Min Range (yds),Optimal (yds),Typical (yds),Long (yds),Max Range (yds),"+
			"Misfires,Rate of Fire Factor,Can Pct,Shell Pct,Shrap Pct,Solid Pct,ArtyTableID,Can ID,Shell ID,Shrap ID,Solid ID\n"+
			"Gun,W_Test,ammo,100,200,300,400,500,1,20,25,25,25,25,AT_Test,AM_Can,AM_Can,AM_Can,AM_Can\n")
	rep := newReport()
	Weapons(f, true, artillerySets(), rep)
	wantFinding(t, rep, 1, "puts ArtyTableID in column 14")
	wantCount(t, rep, 1)
}

// ---- artytables.csv -------------------------------------------------------

const artyTableHeader = "Name,ArtyTableID,Experience,Calculated,Stress,Adjusted"

// The experience column is an offset into a fixed run of slots, so one at or
// above the stride lands in the next table's space.
func TestArtyTablesExperienceOffTheEnd(t *testing.T) {
	f := load(t, "artytables.csv", artyTableHeader+"\nTable,AT_Test,120,1,1,1\n")
	rep := newReport()
	ArtyTables(f, rep)
	wantFinding(t, rep, 2, "lands in the next table's slots")
}

// Only the last of the three value columns reaches the table.
func TestArtyTablesBlankAdjusted(t *testing.T) {
	f := load(t, "artytables.csv", artyTableHeader+"\nTable,AT_Test,0,5,5,\n")
	rep := newReport()
	ArtyTables(f, rep)
	wantFinding(t, rep, 2, "is 0 whatever the columns before it say")
}

func TestArtyTablesRepeatedExperience(t *testing.T) {
	f := load(t, "artytables.csv", artyTableHeader+"\nTable,AT_Test,0,1,1,1\nTable,AT_Test,0,1,1,2\n")
	rep := newReport()
	ArtyTables(f, rep)
	wantFinding(t, rep, 3, "experience 0 is set twice")
}

// ---- sfx.csv --------------------------------------------------------------

const sfxHeader = "Name,ID,File,MinDist,MaxDist,Loop,Volume,Music"

func sfxAssets() *Assets {
	a := NewAssets()
	a.byDir[DirSounds] = map[string]bool{"there.wav": true}
	return a
}

func TestSfxMissingFile(t *testing.T) {
	f := load(t, "sfx.csv", sfxHeader+"\nName,SFX_Test,gone.wav,10,200,0,0,0\n")
	rep := newReport()
	Sfx(f, NewNameSets(), sfxAssets(), rep)
	wantFinding(t, rep, 2, "is not in the Sounds folder")
}

func TestSfxFileFoundWithoutExtension(t *testing.T) {
	f := load(t, "sfx.csv", sfxHeader+"\nName,SFX_Test,there,10,200,0,0,0\n")
	rep := newReport()
	Sfx(f, NewNameSets(), sfxAssets(), rep)
	wantClean(t, rep)
}

func TestSfxInaudible(t *testing.T) {
	f := load(t, "sfx.csv", sfxHeader+"\nName,SFX_Test,there.wav,300,200,0,0,0\n")
	rep := newReport()
	Sfx(f, NewNameSets(), sfxAssets(), rep)
	wantFinding(t, rep, 2, "the sound fades between the two")
}

// ---- unittype.csv ---------------------------------------------------------

const unitTypeHeader = "TypeName,Type,AiDll,AiFunc,InfWeight,CavWeight,ArtyWeight,AutoRoutePct," +
	"TicsReaquire,MarchTargYds,CanCharge,CanChargeRetreat,MeleeIndex,MeleeMntIndex," +
	"FatigueIndex,FatigueMntLimIndex,MountedStopOnDefTerrain,BaseVolleyYards"

func unitTypeAssets() *Assets {
	a := NewAssets()
	a.byDir[DirModules] = map[string]bool{"sowai.dll": true}
	return a
}

func TestUnitTypeClean(t *testing.T) {
	f := load(t, "unittype.csv", unitTypeHeader+"\nTN_INF,1,SowAI.dll,SowAIFunc,1,1,1,0,0,0,1,1,0,0,0,0,0,0\n")
	rep := newReport()
	UnitType(f, unitTypeAssets(), rep)
	wantClean(t, rep)
}

func TestUnitTypeUnknownTypeNumber(t *testing.T) {
	f := load(t, "unittype.csv", unitTypeHeader+"\nTN_INF,9,SowAI.dll,SowAIFunc,1,1,1,0,0,0,1,1,0,0,0,0,0,0\n")
	rep := newReport()
	UnitType(f, unitTypeAssets(), rep)
	wantFinding(t, rep, 2, "not a unit type the engine has any behaviour for")
}

func TestUnitTypeMissingAiDll(t *testing.T) {
	f := load(t, "unittype.csv", unitTypeHeader+"\nTN_INF,1,Gone.dll,SowAIFunc,1,1,1,0,0,0,1,1,0,0,0,0,0,0\n")
	rep := newReport()
	UnitType(f, unitTypeAssets(), rep)
	wantFinding(t, rep, 2, "is not in the Modules folder")
}

// The shipped header labels the two weight columns the other way round from
// the order the loader reads them.
func TestUnitTypeWeightLabelsDisagree(t *testing.T) {
	header := strings.Replace(unitTypeHeader, "InfWeight,CavWeight,ArtyWeight", "InfWeight,ArtyWeight,CavWeight", 1)
	f := load(t, "unittype.csv", header+"\nTN_INF,1,SowAI.dll,SowAIFunc,1,100,1,0,0,0,1,1,0,0,0,0,0,0\n")
	rep := newReport()
	UnitType(f, unitTypeAssets(), rep)
	wantFinding(t, rep, 1, "the loader reads them as the cavalry weight")
}

// ---- map .csv -------------------------------------------------------------

const mapBrushHeader = "TERRAIN TABLE BRUSH,,,,,,,,,,,,,,,,,\n" +
	"Terrain Type Name,Grayscale,Movement,Density,Visibility,Height,Defensive,Fatigue,Wall,NoFade,NoThin,Level,S1,S2,S3,S4,S5,S6\n"

func mapSprites() *SpriteSet {
	s := NewSpriteSet()
	s.Defined["GFX_TREE"] = "gfx.csv:2"
	return s
}

func TestMapsClean(t *testing.T) {
	f := load(t, "Gburg.csv", mapBrushHeader+"IDS_MAP_OPEN,255,0,100,0,0,0,0,,,,0,GFX_Tree,,,,,\n")
	rep := newReport()
	Maps(f, NewNameSets(), mapSprites(), rep)
	wantClean(t, rep)
}

// The ground table has one slot per greyscale value and the loader never
// checks the one it is given.
func TestMapsGreyscaleOffTheEnd(t *testing.T) {
	f := load(t, "Gburg.csv", mapBrushHeader+"IDS_MAP_OPEN,300,0,100,0,0,0,0,,,,0,,,,,,\n")
	rep := newReport()
	Maps(f, NewNameSets(), mapSprites(), rep)
	wantFinding(t, rep, 3, "writing past the end of the table")
}

func TestMapsUnknownFillSprite(t *testing.T) {
	f := load(t, "Gburg.csv", mapBrushHeader+"IDS_MAP_OPEN,255,0,100,0,0,0,0,,,,0,GFX_Nope,,,,,\n")
	rep := newReport()
	Maps(f, NewNameSets(), mapSprites(), rep)
	wantFinding(t, rep, 3, `fill sprite 1 "GFX_Nope"`)
}

// A firing line reads through the ground table entry it names, and every entry
// starts empty.
func TestMapsFiringLineWithNoTerrain(t *testing.T) {
	f := load(t, "Gburg.csv", mapBrushHeader+
		"IDS_MAP_OPEN,255,0,100,0,0,0,0,,,,0,,,,,,\n"+
		"TERRAIN TABLE FORTS,,,,,,,,,,,,,,,,,\n"+
		"Name,Grayscale,Off,Bonus,Mid X,Mid Y\n"+
		"FIRING LINE,77,10,0,0,0,0,0,0,5\n")
	rep := newReport()
	Maps(f, NewNameSets(), mapSprites(), rep)
	wantFinding(t, rep, 6, "the game follows a null pointer")
}

func TestMapsNoBrushSection(t *testing.T) {
	f := load(t, "Gburg.csv", "TERRAIN TABLE SOUNDS,,,,\nloc x,loc z,dir x,dir z,Sprite\n0,0,0,0,GFX_Tree\n")
	rep := newReport()
	Maps(f, NewNameSets(), mapSprites(), rep)
	wantFinding(t, rep, 1, "has no terrain table at all")
}

// ---- OOBs and scenarios ---------------------------------------------------

const oobHeader = "Name,ID,NAME1,NAME2,SIDE,ARMY,CORPS,DIV,BGDE,REG,CLASS,PORTRAIT,Weapon,AMMO," +
	"FLAGS,FLAG2,Formation,Head Count,Ability,Command,Control,Leadership,Style,Experience,Fatigue,Morale"

const scenarioHeader = "Name,ID,SIDE,ARMY,CORPS,DIV,BGDE,REG,BTN,AMMO,dir x,dir z,loc x,loc z,Formation,Head Count,Fatigue,Morale"

func oobSets() *NameSets {
	sets := NewNameSets()
	sets.Class["UGLB_TEST"] = "unitglobal.csv:2"
	sets.Weapon["W_TEST"] = "rifles.csv:2"
	sets.Drill["DRIL_TEST"] = "drills.csv:2"
	return sets
}

func oobFile(t *testing.T, rows ...string) *datacsv.File {
	t.Helper()
	return load(t, "oob_test.csv", oobHeader+"\n"+strings.Join(rows, "\n")+"\n")
}

func TestOOBClean(t *testing.T) {
	f := oobFile(t, "Meade,OOB_U_Test,George,Meade,0,1,1,0,0,0,UGLB_Test,(0-0),W_Test,,,,DRIL_Test,100,0,0,0,0,0,0,0,0")
	rep := newReport()
	OOB(f, oobSets(), rep)
	wantClean(t, rep)
}

func TestOOBUnknownClass(t *testing.T) {
	f := oobFile(t, "Meade,OOB_U_Test,George,Meade,0,1,1,0,0,0,UGLB_Nope,(0-0),W_Test,,,,DRIL_Test,100,0,0,0,0,0,0,0,0")
	rep := newReport()
	OOB(f, oobSets(), rep)
	wantFinding(t, rep, 2, `CLASS "UGLB_Nope"`)
}

func TestOOBUnknownWeaponAndFormation(t *testing.T) {
	f := oobFile(t, "Meade,OOB_U_Test,George,Meade,0,1,1,0,0,0,UGLB_Test,(0-0),W_Nope,,,,DRIL_Nope,100,0,0,0,0,0,0,0,0")
	rep := newReport()
	OOB(f, oobSets(), rep)
	wantFinding(t, rep, 2, `Weapon "W_Nope"`)
	wantFinding(t, rep, 2, `Formation "DRIL_Nope"`)
}

// The OOBMOD column many mods add is not one the engine reads.
func TestOOBOlderLayout(t *testing.T) {
	header := strings.Replace(oobHeader, "CLASS,PORTRAIT,Weapon", "CLASS,OOBMOD,PORTRAIT,Weapon", 1)
	f := load(t, "oob_test.csv", header+"\n"+
		"Meade,OOB_U_Test,George,Meade,0,1,1,0,0,0,UGLB_Test,0,(0-0),W_Test,,,,DRIL_Test,100,0,0,0,0,0,0,0,0\n")
	rep := newReport()
	OOB(f, oobSets(), rep)
	wantFinding(t, rep, 1, "puts Weapon in column 13")
	wantCount(t, rep, 1)
}

func scenarioWorld(t *testing.T) *OOBSet {
	t.Helper()
	oobs := NewOOBSet()
	oobs.Add(oobFile(t, "Meade,OOB_U_Test,George,Meade,0,1,1,0,0,0,UGLB_Test,(0-0),W_Test,,,,DRIL_Test,100,0,0,0,0,0,0,0,0"))
	return oobs
}

func TestScenarioClean(t *testing.T) {
	f := load(t, "scenario.csv", scenarioHeader+"\n"+
		"MASTER,oob_test.csv,,,,,,,,,,,,,,,,\n"+
		"Meade,OOB_U_Test,0,1,1,0,0,0,0,100,0,1,500,500,DRIL_Test,100,0,0\n")
	rep := newReport()
	Scenario(f, scenarioWorld(t), oobSets(), rep)
	wantClean(t, rep)
}

func TestScenarioNoMasterRow(t *testing.T) {
	f := load(t, "scenario.csv", scenarioHeader+"\n"+
		"Meade,OOB_U_Test,0,1,1,0,0,0,0,100,0,1,500,500,DRIL_Test,100,0,0\n")
	rep := newReport()
	Scenario(f, scenarioWorld(t), oobSets(), rep)
	wantFinding(t, rep, 1, "has no MASTER row")
}

func TestScenarioUnitNotInMaster(t *testing.T) {
	f := load(t, "scenario.csv", scenarioHeader+"\n"+
		"MASTER,oob_test.csv,,,,,,,,,,,,,,,,\n"+
		"Nobody,OOB_U_Nope,0,1,1,0,0,0,0,100,0,1,500,500,DRIL_Test,100,0,0\n")
	rep := newReport()
	Scenario(f, scenarioWorld(t), oobSets(), rep)
	wantFinding(t, rep, 3, "leaves this unit out of the battle")
}

func TestScenarioUnknownMasterFile(t *testing.T) {
	f := load(t, "scenario.csv", scenarioHeader+"\n"+
		"MASTER,oob_gone.csv,,,,,,,,,,,,,,,,\n"+
		"Meade,OOB_U_Test,0,1,1,0,0,0,0,100,0,1,500,500,DRIL_Test,100,0,0\n")
	rep := newReport()
	Scenario(f, scenarioWorld(t), oobSets(), rep)
	wantFinding(t, rep, 2, "which is not there")
}

// ---- battlescript.csv -----------------------------------------------------

const battleScriptHeader = ",ID NAME,Command,FromId,X Coord,Z Coord,time var"

func TestBattleScriptClean(t *testing.T) {
	f := load(t, "battlescript.csv", battleScriptHeader+"\n"+
		"12:30:00 PM,OOB_U_Test,moveto,OOB_U_Test,100,100,0\n"+
		"evtobjdone,OBJ_TEST,logmsg,,,,0\n")
	rep := newReport()
	BattleScript(f, map[string]int{"OBJ_TEST": 2}, map[string]int{"OOB_U_TEST": 2}, rep)
	wantClean(t, rep)
}

func TestBattleScriptUnknownEventType(t *testing.T) {
	f := load(t, "battlescript.csv", battleScriptHeader+"\nevtnosuch,OOB_U_Test,moveto,,100,100,0\n")
	rep := newReport()
	BattleScript(f, nil, map[string]int{"OOB_U_TEST": 2}, rep)
	wantFinding(t, rep, 2, "throws the whole event away")
}

func TestBattleScriptUnknownCommand(t *testing.T) {
	f := load(t, "battlescript.csv", battleScriptHeader+"\n12:30:00 PM,OOB_U_Test,notacommand,,100,100,0\n")
	rep := newReport()
	BattleScript(f, nil, map[string]int{"OOB_U_TEST": 2}, rep)
	wantFinding(t, rep, 2, "is not one the engine knows")
}

// A command carries its arguments after a colon, and may be queued behind
// another command.
func TestBattleScriptCommandArguments(t *testing.T) {
	f := load(t, "battlescript.csv", battleScriptHeader+"\n"+
		"12:30:00 PM,OOB_U_Test,loadcour:Courier:Tut1_01,,100,100,0\n"+
		"12:31:00 PM,OOB_U_Test,addcommqueue:moveto:5,,100,100,0\n")
	rep := newReport()
	BattleScript(f, nil, map[string]int{"OOB_U_TEST": 2}, rep)
	wantClean(t, rep)
}

func TestBattleScriptUnknownUnit(t *testing.T) {
	f := load(t, "battlescript.csv", battleScriptHeader+"\n12:30:00 PM,OOB_U_Nope,moveto,,100,100,0\n")
	rep := newReport()
	BattleScript(f, nil, map[string]int{"OOB_U_TEST": 2}, rep)
	wantFinding(t, rep, 2, "is not a unit this scenario places")
}

func TestBattleScriptUnknownObjective(t *testing.T) {
	f := load(t, "battlescript.csv", battleScriptHeader+"\nevtobjdone,OBJ_NOPE,logmsg,,,,0\n")
	rep := newReport()
	BattleScript(f, map[string]int{"OBJ_TEST": 2}, map[string]int{}, rep)
	wantFinding(t, rep, 2, "which this scenario does not define")
}

// With no order of battle to check against, the unit columns are left alone.
func TestBattleScriptUnitsUnjudged(t *testing.T) {
	f := load(t, "battlescript.csv", battleScriptHeader+"\n12:30:00 PM,OOB_U_Nope,moveto,,100,100,0\n")
	rep := newReport()
	BattleScript(f, nil, nil, rep)
	wantClean(t, rep)
}

// ---- courier.csv ----------------------------------------------------------

const courierHeader = "TYPE,ID,Sub List,Command,Text,Notes"

func TestCourierClean(t *testing.T) {
	f := load(t, "courier.csv", courierHeader+"\n"+
		"NEW,listorders,,,,\n"+
		"MENU,Face in Direction,listcompass,Awheelspec:%s,text,\n"+
		"ITEM,Move,,moveto,text,\n")
	rep := newReport()
	Courier(f, rep)
	wantClean(t, rep)
}

func TestCourierUnknownEntryType(t *testing.T) {
	f := load(t, "courier.csv", courierHeader+"\nNEW,listorders,,,,\nNOPE,Something,,,,\n")
	rep := newReport()
	Courier(f, rep)
	wantFinding(t, rep, 3, "is not NEW, MENU, ITEM or BUTT")
}

func TestCourierEntryBeforeAnyList(t *testing.T) {
	f := load(t, "courier.csv", courierHeader+"\nMENU,Something,,,,\n")
	rep := newReport()
	Courier(f, rep)
	wantFinding(t, rep, 2, "Bad list definitions")
}

func TestCourierUnknownSubList(t *testing.T) {
	f := load(t, "courier.csv", courierHeader+"\nNEW,listorders,,,,\nMENU,Something,listnope,,,\n")
	rep := newReport()
	Courier(f, rep)
	wantFinding(t, rep, 3, `"listnope" is not a list`)
}

// A sub list defined further down the file resolves, because the engine
// resolves these only once the whole file is read.
func TestCourierForwardSubList(t *testing.T) {
	f := load(t, "courier.csv", courierHeader+"\n"+
		"NEW,listorders,,,,\nMENU,Something,listlater,,,\nNEW,listlater,,,,\nITEM,Thing,,moveto,,\n")
	rep := newReport()
	Courier(f, rep)
	wantClean(t, rep)
}

// ---- gscreens.csv and mscreens.csv ----------------------------------------

const screensHeader = "Type,ID,Graphic,Font,Tooltip,Index,X Coord,Y Coord,Width,Height," +
	"X Source,Y Source,Draw Condition,Exec Condition,Function,Depends1,Depends2,Depends3"

func screensSets() *NameSets {
	sets := NewNameSets()
	sets.Font["DEFAULTSMALL"] = "gamefonts.csv:2"
	return sets
}

func TestScreensClean(t *testing.T) {
	f := load(t, "gscreens.csv", screensHeader+"\n"+
		"NEW,MainScreen,back.tga,,,,0,0,1024,768,0,0,,,,,,\n"+
		"TEXT,Title,IDS_Title,DefaultSmall-C-255-255-255,,,10,10,100,20,0,0,,,,,,\n")
	rep := newReport()
	Screens(f, screensSets(), rep)
	wantClean(t, rep)
}

func TestScreensUnknownControlType(t *testing.T) {
	f := load(t, "gscreens.csv", screensHeader+"\nNEW,MainScreen,,,,,0,0,0,0,0,0,,,,,,\nWIDGET,Thing,,,,,0,0,0,0,0,0,,,,,,\n")
	rep := newReport()
	Screens(f, screensSets(), rep)
	wantFinding(t, rep, 3, "unknow obj")
}

func TestScreensControlBeforeAnyScreen(t *testing.T) {
	f := load(t, "gscreens.csv", screensHeader+"\nTEXT,Thing,,,,,0,0,0,0,0,0,,,,,,\n")
	rep := newReport()
	Screens(f, screensSets(), rep)
	wantFinding(t, rep, 2, "comes before any NEW row")
}

// The font column is a name with an alignment and a colour after it, and only
// the name is looked up.
func TestScreensUnknownFont(t *testing.T) {
	f := load(t, "gscreens.csv", screensHeader+"\n"+
		"NEW,MainScreen,,,,,0,0,0,0,0,0,,,,,,\n"+
		"TEXT,Title,,NoSuchFont-C-255-255-255,,,10,10,100,20,0,0,,,,,,\n")
	rep := newReport()
	Screens(f, screensSets(), rep)
	wantFinding(t, rep, 3, `names font "NoSuchFont"`)
}

// ---- unitattributes.csv ---------------------------------------------------

func TestUnitAttributesNoTables(t *testing.T) {
	f := load(t, "unitattributes.csv", ",INTEGERS ONLY,Level Name\nWeather,Visibility,Label\n0,300,IDS_Fog\n")
	rep := newReport()
	UnitAttributes(f, rep)
	wantFinding(t, rep, 1, "has no NEW row")
	wantCount(t, rep, 1)
}

func TestUnitAttributesEmptyTable(t *testing.T) {
	f := load(t, "unitattributes.csv", ",INTEGERS ONLY,Level Name\nNEW,Weather,\nNEW,Morale,\n0,300,IDS_Fog\n")
	rep := newReport()
	UnitAttributes(f, rep)
	wantFinding(t, rep, 2, "has no experience levels under it")
}
