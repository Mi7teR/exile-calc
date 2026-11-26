package engine

import (
	"reflect"
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
		"hexDoom",
		"minionCounts",
		"lootBonuses",
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
		"offence",
		"minionOffence",
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

// TestTreeSpanAppendOnly: stepMergeTree moves st.treeSpan's index ranges,
// which only hold if the steps from passiveTree up to mergeTree never remove,
// reorder or replace a db.mods entry. Over the corpus, in MAIN and
// CALCULATOR mode, every list must still start with what it held after
// passiveTree when mergeTree runs.
func TestTreeSpanAppendOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus-heavy; run without -short")
	}
	from := slices.IndexFunc(calcSteps, func(s calcStep) bool { return s.name == "passiveTree" })
	to := slices.IndexFunc(calcSteps, func(s calcStep) bool { return s.name == "mergeTree" })
	if from < 0 || to <= from {
		t.Fatalf("passiveTree (%d) must come before mergeTree (%d)", from, to)
	}
	c := newCalc(t)
	for _, id := range corpusIDs(t) {
		b := corpusBuild(t, id)
		for _, calculator := range []bool{false, true} {
			st := &calcState{b: b, calculator: calculator}
			var snap map[string][]Mod
			for i, step := range calcSteps[:to] {
				if err := step.run(c, st); err != nil {
					t.Fatalf("%s: %s: %v", id, step.name, err)
				}
				if i == from {
					snap = make(map[string][]Mod, len(st.db.mods))
					for name, l := range st.db.mods {
						snap[name] = slices.Clone(l)
					}
				}
			}
			for name, before := range snap {
				now := st.db.mods[name]
				if len(now) < len(before) {
					t.Errorf("%s (calculator %v): %s shrank from %d to %d mods before mergeTree",
						id, calculator, name, len(before), len(now))
					continue
				}
				for i := range before {
					if !reflect.DeepEqual(before[i], now[i]) {
						t.Errorf("%s (calculator %v): %s[%d] changed before mergeTree: %+v -> %+v",
							id, calculator, name, i, before[i], now[i])
						break
					}
				}
			}
		}
	}
}
