package engine

import "testing"

// CalcSetup.lua 1286-1302: gloves and boots add each mod's scaled
// (scale - 1) part, so "+5" at 70% effect is 5 + trunc(-1.5) = 4, not
// trunc(3.5) = 3; LIST mods are left as they are.
func TestAddScaledPart(t *testing.T) {
	got := addScaledPart(Mod{Name: "Str", Type: "BASE", Value: 5.0}, 0.7-1)
	if got.Value != 4.0 {
		t.Errorf("+5 at 0.7: %v, want 4", got.Value)
	}
	if v := scaleMod(Mod{Name: "Str", Type: "BASE", Value: 5.0}, 0.7).Value; v != 3.0 {
		t.Errorf("scaleMod +5 at 0.7: %v, want 3 (the other branches)", v)
	}
	list := Mod{Name: "ExtraSkill", Type: "LIST", Value: map[string]any{"skillId": "X"}}
	if l := addScaledPart(list, -0.3); l.Type != "LIST" || l.Value.(map[string]any)["skillId"] != "X" {
		t.Errorf("LIST mod changed: %+v", l)
	}
	if v := addScaledPart(Mod{Name: "Life", Type: "INC", Value: 20.0}, 0.5).Value; v != 30.0 {
		t.Errorf("20%% at 1.5: %v, want 30", v)
	}
}
