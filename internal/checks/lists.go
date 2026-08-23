package checks

// builtInLists are the courier lists the engine builds itself rather than
// reading from courier.csv. CVars::GetList (War3D/vars.cpp) answers each of
// these by name -- the compass points, the officers under a commander, the
// saved games -- so a menu opening one is not naming anything the file has to
// define.
//
// Taken from the names that function tests for. A list added to the engine
// without being added here reads as undefined, so check this when that
// function changes.
var builtInLists = map[string]bool{
	"listaddons": true, "listallcamplocs": true, "listallmaps": true,
	"listallmods": true, "listallofficers": true, "listallopenoob": true,
	"listallpeers": true, "listallscenofficers": true, "listallsubs": true,
	"listaohighscore": true, "listaoscenarios": true, "listcas": true,
	"listcompass": true, "listcusthighscore": true, "listdebugtxt": true,
	"listdetach": true, "listfortunits": true, "listhighscore": true,
	"listlanguages": true, "listlanservers": true, "listmaplocs": true,
	"listmpscenarios": true, "listmpservers": true, "listmpstatus": true,
	"listmultihighscore": true, "listobjectives": true, "listoffsubs": true,
	"listordertimes": true, "listpeers": true, "listpeersub": true,
	"listreceivedmessages": true, "listrepgames": true, "listsavedcampgames": true,
	"listsavedgames": true, "listsavescengames": true, "listsavethisscengames": true,
	"listsbcbattles": true, "listsbcoff": true, "listsbctowns": true,
	"listscenarios": true, "listscenofficers": true, "listscrres": true,
	"listsentmessages": true, "listspscenarios": true, "liststats": true,
	"listsubs": true, "listtarg": true, "listtownunits": true,
	"treealloob": true, "treesbcseloff": true, "treeseloff": true,
}
