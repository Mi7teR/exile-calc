package engine

import (
	"testing"

	"github.com/Mi7teR/exile-calc/pobcode"
)

// TestMapMods: the MapPrefix/MapSuffix options run their Data/ModMap.lua
// affix (mapAffixDropDownFunction, ConfigOptions.lua:116-131).
// zKMTzbWQ2h1C picks Armoured twice on a red map: 40% PDR each, as PoB's
// enemy mods. A yellow map with 50% increased map mod effect gives Fecund
// (count: the top of its 30-39 roll) and of Venom (check).
func TestMapMods(t *testing.T) {
	c := newCalc(t)
	res, err := c.Calc(corpusBuild(t, "zKMTzbWQ2h1C"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range res.DB.enemy.mods["PhysicalDamageReduction"] {
		if m.Source == "Map mod Armoured" && m.Value == 40.0 {
			n++
		}
	}
	if n != 2 {
		t.Errorf("Armoured mods %d, want 2", n)
	}

	b := corpusBuild(t, "zKMTzbWQ2h1C")
	set := b.ActiveConfig()
	set.Inputs = append(set.Inputs,
		pobcode.Input{Name: "MapPrefix2", Kind: pobcode.KindString, String: "Fecund"},
		pobcode.Input{Name: "MapSuffix4", Kind: pobcode.KindString, String: "of Venom"},
		pobcode.Input{Name: "multiplierMapModEffect", Kind: pobcode.KindNumber, Number: 50},
		pobcode.Input{Name: "multiplierMapModTier", Kind: pobcode.KindString, String: "MED"})
	if res, err = c.Calc(b); err != nil {
		t.Fatal(err)
	}
	fecund := 0.0
	for _, m := range res.DB.enemy.mods["Life"] {
		if m.Source == "Map mod Fecund" && m.Type == "MORE" {
			fecund, _ = num(m.Value)
		}
	}
	if fecund != 58.5 {
		t.Errorf("Fecund Life MORE %v, want 58.5 ((30 + 9) * 1.5)", fecund)
	}
	if got := res.DB.enemy.Sum("BASE", "PoisonChance"); got != 100 {
		t.Errorf("enemy PoisonChance %v, want 100", got)
	}
	if got := res.DB.enemy.Sum("BASE", "PhysicalDamageReduction"); got < 60 {
		t.Errorf("enemy PDR %v: Armoured on a yellow map is 30 * 1.5 each", got)
	}
}
