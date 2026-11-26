package engine

import (
	"strings"
	"testing"

	"github.com/Mi7teR/exile-calc/pobcode"
)

// CalcSetup.lua:578-599, 132-134: each Calcs tab buff mode sets the
// Buffed, Combat and Effective conditions.
func TestBuffModeConditions(t *testing.T) {
	for _, tc := range []struct {
		mode                  string
		buffs, combat, effect bool
	}{
		{"", true, true, true}, {"EFFECTIVE", true, true, true}, {"COMBAT", true, true, false},
		{"BUFFED", true, false, false}, {"UNBUFFED", false, false, false},
	} {
		db := NewModDB()
		buffMode(db, tc.mode)
		if modeBuffs(db) != tc.buffs || modeCombat(db) != tc.combat || modeEffective(db) != tc.effect {
			t.Errorf("%q: %v %v %v", tc.mode, modeBuffs(db), modeCombat(db), modeEffective(db))
		}
	}
}

// The CALCS pass of the Party tab export runs in the Calcs tab's buff mode
// (misc_buffMode): without mode_buffs no aura is exported, and without
// mode_effective no curse but marks.
func TestCalcsBuffModeExports(t *testing.T) {
	c := newCalc(t)
	export := func(mode string) map[string]string {
		b := corpusBuild(t, "OhQ2HoOzO2Z0")
		if b.Party == nil {
			b.Party = &pobcode.Party{}
		}
		b.Party.ExportBuffs = true
		if mode != "" {
			in := pobcode.Input{Name: "misc_buffMode", Kind: pobcode.KindString, String: mode}
			b.CalcsInputs = append(b.CalcsInputs, in)
		}
		res, err := c.Calc(b)
		if err != nil {
			t.Fatal(err)
		}
		return map[string]string{"Aura": res.PartyExport("Aura"), "Curse": res.PartyExport("Curse"),
			"Warcry": res.PartyExport("Warcry")}
	}
	eff, buffed, unbuffed := export(""), export("BUFFED"), export("UNBUFFED")
	if eff["Aura"] == "" || eff["Warcry"] == "" || !strings.Contains(eff["Curse"], "\n---") {
		t.Fatalf("EFFECTIVE exports %q", eff)
	}
	if buffed["Aura"] != eff["Aura"] || buffed["Warcry"] != eff["Warcry"] ||
		strings.Contains(buffed["Curse"], "\n---") {
		t.Errorf("BUFFED exports %q", buffed)
	}
	if unbuffed["Aura"] != "" || unbuffed["Warcry"] != "" {
		t.Errorf("UNBUFFED exports %q", unbuffed)
	}
}

// A CALCS pass in a non-EFFECTIVE buff mode (the party export's) clears
// mode_effective on the player and the enemy, so the mode_effective guards
// hold while the buffs and combat modes stay on:
//   - curses: none lands on the enemy (CalcPerform.lua:2696)
//   - offence: no impale chance (CalcOffence.lua:4312)
//   - defence: no lucky block, EffectiveBlockChance is BlockChance
//     (CalcDefence.lua:760-772)
func TestCalcsPassModeEffectiveGuards(t *testing.T) {
	c := newCalc(t)
	run := func(id, mode string) *Result {
		res, err := c.calc(corpusBuild(t, id), calcOpts{buffMode: mode})
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	eff, combat := run("OhQ2HoOzO2Z0", "EFFECTIVE"), run("OhQ2HoOzO2Z0", "COMBAT")
	if !modeEffective(eff.DB) || !eff.DB.enemy.conditions["Cursed"] {
		t.Fatalf("EFFECTIVE: effective %v, enemy cursed %v", modeEffective(eff.DB), eff.DB.enemy.conditions["Cursed"])
	}
	db := combat.DB
	if modeEffective(db) || modeEffective(db.enemy) || !modeCombat(db) || !modeBuffs(db) {
		t.Errorf("COMBAT modes: effective %v/%v combat %v buffs %v",
			modeEffective(db), modeEffective(db.enemy), modeCombat(db), modeBuffs(db))
	}
	if db.enemy.conditions["Cursed"] {
		t.Error("COMBAT: curse applied without mode_effective")
	}

	// HBcW_8Hvft-z impales on every hit.
	eff, combat = run("HBcW_8Hvft-z", "EFFECTIVE"), run("HBcW_8Hvft-z", "COMBAT")
	if eff.Stats["ImpaleChance"] != 100 || combat.Stats["ImpaleChance"] != 0 {
		t.Errorf("ImpaleChance: EFFECTIVE %v, COMBAT %v; want 100, 0",
			eff.Stats["ImpaleChance"], combat.Stats["ImpaleChance"])
	}

	// 1U1EWET8OeON has lucky block: 76% blocks as 94.24%.
	eff, combat = run("1U1EWET8OeON", "EFFECTIVE"), run("1U1EWET8OeON", "COMBAT")
	block := combat.Stats["BlockChance"]
	if eff.Stats["EffectiveBlockChance"] != 94.24 || block != 76 || combat.Stats["EffectiveBlockChance"] != block {
		t.Errorf("EffectiveBlockChance: EFFECTIVE %v, COMBAT %v (BlockChance %v); want 94.24, 76, 76",
			eff.Stats["EffectiveBlockChance"], combat.Stats["EffectiveBlockChance"], block)
	}
}
