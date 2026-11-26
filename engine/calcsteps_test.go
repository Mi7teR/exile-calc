package engine

import (
	"slices"
	"strings"
	"testing"
)

// TestCalcStepOrder pins the order of Calc's steps: MORE mods round per
// layer, so swapping two steps changes numbers.
func TestCalcStepOrder(t *testing.T) {
	want := []string{
		"initEnv",
		"baseMods",
		"bandit",
		"pantheon",
		"config",
		"customMods",
		"passiveTree",
		"items",
		"jewelSockets",
		"mergeTree",
		"grantedPassives",
		"itemConditions",
		"skills",
		"flasks",
		"skillModLayer",
		"earlyEnemyMods",
		"armourESToLife",
		"attributesLifeMana",
		"reservations",
		"tinctures",
		"skillCounts",
		"chillSkills",
		"earlyCharges",
		"buffs",
		"curses",
		"extraAuras",
		"disableWeapons",
		"defenceForConditionals",
		"charges",
		"actorMisc",
		"skillInfo",
		"allyLife",
		"defence",
		"minion",
		"offence",
		"unsupportedTags",
	}
	got := make([]string, len(calcSteps))
	for i, s := range calcSteps {
		got[i] = s.name
	}
	if !slices.Equal(got, want) {
		t.Errorf("calcSteps order changed:\n got %q\nwant %q", got, want)
	}
}

// CalcSetup.lua:1426/1463: the item mods are merged into the ModDB before
// the allocated nodes' mods.
func TestItemModsBeforeTreeMods(t *testing.T) {
	c := newCalc(t)
	r, err := c.Calc(corpusBuild(t, "3ryNFCmR6SLS"))
	if err != nil {
		t.Fatal(err)
	}
	seenTree, checked := false, false
	for _, m := range r.DB.mods["Life"] {
		switch {
		case strings.HasPrefix(m.Source, "Tree:"):
			seenTree = true
		case strings.HasPrefix(m.Source, "Item:"):
			if seenTree {
				t.Fatalf("item mod %s after a tree mod", m.Source)
			}
			checked = true
		}
	}
	if !checked || !seenTree {
		t.Fatalf("build has no item and tree Life mods to compare")
	}
}
