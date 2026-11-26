package engine

import (
	"math"
	"strings"
	"testing"

	"github.com/Mi7teR/exile-calc/pobcode"
)

// TestPoisonDotMultiWeightedInMainMode: with a breakdown (MAIN mode),
// CalcOffence.lua:4944-4951 replaces PoisonDotMulti with the non-crit and
// crit DoT multipliers weighted by the poisons each creates; CALCULATOR mode
// keeps the non-crit one. Seismic Trap as the main skill of Kh5YfbuwGaG0:
// the oracle's mainOutput has PoisonDotMulti 2.615 (2.58 x 0.93 + 3.08 x 0.07).
func TestPoisonDotMultiWeightedInMainMode(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "Kh5YfbuwGaG0")
	b.MainSocketGroup = 2 // Seismic Trap
	for _, x := range []struct {
		calculator bool
		want       float64
	}{{false, 2.615}, {true, 2.58}} {
		res, err := c.calc(b, calcOpts{calculator: x.calculator})
		if err != nil {
			t.Fatal(err)
		}
		if res.main == nil || res.main.active.ge.Name != "Seismic Trap" {
			t.Fatalf("main skill is not Seismic Trap")
		}
		if got := res.Stats["PoisonDotMulti"]; math.Abs(got-x.want) > 1e-6 {
			t.Errorf("calculator %v: PoisonDotMulti %v, want %v", x.calculator, got, x.want)
		}
		if got := res.Stats["CritPoisonDotMulti"]; math.Abs(got-3.08) > 1e-9 {
			t.Errorf("calculator %v: CritPoisonDotMulti %v, want 3.08", x.calculator, got)
		}
	}
}

// TestBleedStacksMaxZero: a custom "up to 0 times" sets BleedStacksMax to 0
// and CalcOffence.lua divides by it (4479, BleedStackPotential). PoB's
// output then has a non-finite BleedStackPotential and BleedDPS 0 (oracle,
// yvENgQrpk-61 with that custom mod); the engine gives the same.
func TestBleedStacksMaxZero(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "yvENgQrpk-61")
	cfg := b.ActiveConfig()
	cfg.CustomMods = append(cfg.CustomMods,
		pobcode.CustomModBlock{Enabled: true, Text: "You can inflict Bleeding on an Enemy up to 0 times"})
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Stats["BleedStacksMax"]; got != 0 {
		t.Fatalf("BleedStacksMax %v, want 0", got)
	}
	if got := res.Stats["BleedStackPotential"]; !math.IsInf(got, 1) {
		t.Errorf("BleedStackPotential %v, want +Inf", got)
	}
	if got := res.Stats["BleedDPS"]; got != 0 {
		t.Errorf("BleedDPS %v, want 0", got)
	}
}

// TestDualWieldAilmentCritChance pins which crit chance the ailment section
// reads for a dual-wield attack whose hands crit differently. Inside the
// per-pass ailment loop (CalcOffence.lua:4208) PoB reads the pass's
// output.CritChance for the ailment chance, ailmentCritChance and
// calcAilmentDamage (4488, 4614, 4761, 4887-4889), but the combined
// globalOutput.CritChance for the non-crit share of the DotMulti weighting
// (4648 bleed, 4945-4946 poison, 5341 ignite). Synthetic variant of
// Ne3GCl-EkKEi (Viper Strike, two Wasp Nests): the off hand's Wasp Nest has 250%
// instead of 20% increased Critical Strike Chance, plus custom bleed mods.
// The want values are the oracle's output.MainHand/OffHand (PoB v2.68.1).
func TestDualWieldAilmentCritChance(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "Ne3GCl-EkKEi")
	it := b.Item(b.ActiveItems().SlotItem("Weapon 2"))
	raw := strings.Replace(it.Raw, "20% increased Critical Strike Chance", "250% increased Critical Strike Chance", 1)
	if raw == it.Raw {
		t.Fatal("off hand crit line not found")
	}
	it.Raw = raw
	cfg := b.ActiveConfig()
	for _, m := range []string{
		"+40% to Damage over Time Multiplier for Bleeding from Critical Strikes",
		"25% chance to cause Bleeding on Hit",
	} {
		cfg.CustomMods = append(cfg.CustomMods, pobcode.CustomModBlock{Enabled: true, Text: m})
	}
	var passes []*hitPass
	debugPasses = func(ps []*hitPass) {
		if len(ps) == 2 {
			passes = ps
		}
	}
	defer func() { debugPasses = nil }()
	res, err := c.calc(b, calcOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if res.main == nil || res.main.active.ge.Name != "Viper Strike" || len(passes) != 2 {
		t.Fatalf("main skill is not a dual-wield Viper Strike")
	}
	if got := res.Stats["CritChance"]; math.Abs(got-25.775) > 1e-9 {
		t.Errorf("CritChance %v, want 25.775", got)
	}
	for i, want := range []map[string]float64{{
		"CritChance":     13.49,
		"BleedDotMulti":  1.4663125226682, // 1.5 x (1 - 0.25775) share vs 1.9 x per-pass ailmentCritChance
		"BleedDPS":       6614.5407783632,
		"PoisonDotMulti": 1.738875, // both hands: globalOutput.CritChance only
		"PoisonDPS":      113311.86145924,
	}, {
		"CritChance":     38.06,
		"BleedDotMulti":  1.5154590145978,
		"BleedDPS":       7172.9984992811,
		"PoisonDotMulti": 1.738875,
		"PoisonDPS":      120853.9397896,
	}} {
		for stat, w := range want {
			if got := passes[i].out[stat]; math.Abs(got-w) > 1e-6*math.Max(1, math.Abs(w)) {
				t.Errorf("%s %s: %v, want %v", passes[i].label, stat, got, w)
			}
		}
	}
}

// TestLegacyPerfectAgony: CalcOffence.lua:4219-4227 adds half of every
// non-"Base" crit multiplier mod as ailment DoT multiplier on crits. The
// want values are the oracle's (PoB v2.68.1) with the legacy Perfect Agony
// line and "+31% to Global Critical Strike Multiplier" as custom mods. The
// spells (-fxcKCnOf7D2, 8YoGqmeyF3HU, EdNBLqyOUdTp) take the nil
// handCondition path; Ne3GCl-EkKEi is a dual-wield attack with hand tags.
func TestLegacyPerfectAgony(t *testing.T) {
	const legacy = "Modifiers to Critical Strike Multiplier also apply to Damage over Time " +
		"Multiplier for Ailments from Critical Strikes at 50% of their value"
	const crit = "+31% to Global Critical Strike Multiplier"
	c := newCalc(t)
	run := func(id string, text string) map[string]float64 {
		b := corpusBuild(t, id)
		cfg := b.ActiveConfig()
		cfg.CustomMods = append(cfg.CustomMods, pobcode.CustomModBlock{Enabled: true, Text: text})
		res, err := c.Calc(b)
		if err != nil {
			t.Fatal(err)
		}
		return res.Stats
	}
	for _, x := range []struct {
		id   string
		want map[string]float64
	}{
		{"-fxcKCnOf7D2", map[string]float64{"CritIgniteDotMulti": 2.9, "IgniteDPS": 980.54459286322}},
		{"8YoGqmeyF3HU", map[string]float64{
			"CritIgniteDotMulti": 2.17, "CritPoisonDotMulti": 2.59,
			"IgniteDPS": 14102.902454202, "PoisonDPS": 81907.58373525,
		}},
		{"EdNBLqyOUdTp", map[string]float64{"CritIgniteDotMulti": 2.94}},
		{"Ne3GCl-EkKEi", map[string]float64{"PoisonDPS": 228452.15179476}},
	} {
		without := run(x.id, crit)
		got := run(x.id, legacy+"\n"+crit)
		for k, want := range x.want {
			if !near(got[k], want, 1e-9) {
				t.Errorf("%s %s %v, want %v", x.id, k, got[k], want)
			}
			if near(without[k], want, 1e-9) {
				t.Errorf("%s %s is %v without the legacy line too", x.id, k, want)
			}
		}
	}
}
