package engine

import (
	"slices"
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
		"passiveTree",
		"items",
		"jewelSockets",
		"customMods",
		"grantedPassives",
		"itemConditions",
		"config",
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
