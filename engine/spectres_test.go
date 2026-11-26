package engine

import (
	"slices"
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
)

func TestSpectreModList(t *testing.T) {
	listMod := func(name, inner string) map[string]any {
		return map[string]any{"name": name, "type": "LIST", "value": map[string]any{
			"mod": map[string]any{"name": inner, "type": "BASE", "value": 5.0},
		}}
	}
	e := spectreEffects{curseLimit: 1}
	e.addModList([]map[string]any{
		listMod("AllyModifier", "Ally"),
		listMod("MinionModifier", "Minion"),
		listMod("PlayerModifier", "Player"),
		{"name": "EnemyCurseLimit", "type": "BASE", "value": 3.0},
		listMod("PlayerModifier", "AfterLimit"), // CalcPerform.lua breaks out at EnemyCurseLimit
	}, "Spectre:X")
	names := func(l []Mod) []string {
		var out []string
		for _, m := range l {
			out = append(out, m.Name+"/"+m.Source)
		}
		return out
	}
	if got, want := names(e.player), []string{"Ally/Spectre:X", "Player/Spectre:X"}; !slices.Equal(got, want) {
		t.Errorf("player mods %v, want %v", got, want)
	}
	if got, want := names(e.minion), []string{"Ally/Spectre:X", "Minion/Spectre:X"}; !slices.Equal(got, want) {
		t.Errorf("minion mods %v, want %v", got, want)
	}
	if e.curseLimit != 4 {
		t.Errorf("curse limit %d, want 4", e.curseLimit)
	}
}

func TestMonsterTagMatches(t *testing.T) {
	md := &gamedata.Minion{MonsterTags: []string{"construct", "undead"}}
	for _, c := range []struct {
		md   *gamedata.Minion
		tag  map[string]any
		want bool
	}{
		{md, map[string]any{"monsterTag": "Construct"}, true},
		{md, map[string]any{"monsterTag": "Beast"}, false},
		{md, map[string]any{"monsterTag": "Construct", "neg": true}, false},
		{md, map[string]any{"monsterTagList": []any{"beast", "Undead"}}, true},
		{nil, map[string]any{"monsterTag": "Beast", "neg": true}, false}, // not a minion: never applies
	} {
		if got := monsterTagMatches(c.md, c.tag); got != c.want {
			t.Errorf("%v on %v: %v, want %v", c.tag, c.md, got, c.want)
		}
	}
}

// TestRaiseSpectreUsesSpectreList checks Raise Spectre's minion and the
// curses its spectre puts on the enemy (Perfect Blasphemer's Vulnerability),
// and that without the build's spectre list it has no minion.
func TestRaiseSpectreUsesSpectreList(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "Wpo4TuYfRXFT")
	ms, err := c.CalcMinions(b)
	if err != nil {
		t.Fatal(err)
	}
	var spectres []string
	for _, m := range ms {
		if m.Skill == "Raise Spectre" {
			spectres = append(spectres, m.Minion)
		}
	}
	if want := []string{"Perfect Blasphemer", "Perfect Forest Warrior"}; !slices.Equal(spectres, want) {
		t.Errorf("spectres %v, want %v", spectres, want)
	}
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	cursed := false
	for _, r := range res.DB.Enemy().Tabulate("PhysicalDamageTaken") {
		cursed = cursed || r.Source == "Skill:TalismanT1Vulnerability"
	}
	if !cursed {
		t.Error("the spectre's Vulnerability is not on the enemy")
	}

	b.SpectreList = nil
	res, err = c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Skills {
		if s.Name == "Raise Spectre" && s.HasMinion {
			t.Error("Raise Spectre has a minion without a spectre list")
		}
	}
}
