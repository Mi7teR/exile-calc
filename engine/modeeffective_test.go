package engine

import (
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// testMod is a mod a test adds to the player (enemy false) or the enemy.
type testMod struct {
	enemy bool
	m     Mod
}

// calcEffective is Calc with env.mode_effective set to effective and the
// given mods added after the config step (CalcSetup.lua:134 mirrors
// mode_effective in each actor's Effective condition; the Calcs tab's
// COMBAT, BUFFED and UNBUFFED buff modes clear it).
func calcEffective(t *testing.T, c *Calculator, b *pobcode.Build, effective bool, mods ...testMod) map[string]float64 {
	t.Helper()
	return calcWithMods(t, c, b, effective, mods...).res.Stats
}

// calcWithMods is calcEffective's calculation state.
func calcWithMods(t *testing.T, c *Calculator, b *pobcode.Build, effective bool, mods ...testMod) *calcState {
	t.Helper()
	st := &calcState{b: b}
	for _, step := range calcSteps {
		if err := step.run(c, st); err != nil {
			t.Fatal(err)
		}
		if step.name != "config" {
			continue
		}
		st.db.conditions["Effective"], st.db.enemy.conditions["Effective"] = effective, effective
		for _, tm := range mods {
			m := tm.m
			m.Source = "Test"
			if tm.enemy {
				st.db.enemy.Add(m)
			} else {
				st.db.Add(m)
			}
		}
	}
	return st
}

func flagMod(name string) Mod { return Mod{Name: name, Type: "FLAG", Value: true} }

func numMod(name, typ string, v float64) Mod { return Mod{Name: name, Type: typ, Value: v} }

// TestModeEffectiveGuards: each part of calcs.offence and calcs.defence
// that PoB runs only with env.mode_effective. For every guard the probe
// mods change the stat with mode_effective and leave it as it is without.
func TestModeEffectiveGuards(t *testing.T) {
	c := newCalc(t)
	enemy := func(m Mod) testMod { return testMod{true, m} }
	player := func(m Mod) testMod { return testMod{false, m} }
	for _, tc := range []struct {
		name   string
		build  string
		stat   string
		common []testMod // added in every run
		probe  []testMod // the mods the guard ignores
	}{
		// CalcOffence.lua:3131-3133.
		{"enemy SelfCritChance", "MuBiMZ8WiDZn", "CritChance", nil,
			[]testMod{enemy(numMod("SelfCritChance", "BASE", 10))}},
		// 3146.
		{"lucky crit", "MuBiMZ8WiDZn", "CritChance", nil, []testMod{player(flagMod("CritChanceLucky"))}},
		// 3162.
		{"bifurcate crit", "MuBiMZ8WiDZn", "CritChance", nil, []testMod{player(flagMod("BifurcateCrit"))}},
		// 3167-3179: every 3rd use, and the confirmation roll (accuracy 96%).
		{"crit every 3rd use", "MuBiMZ8WiDZn", "CritChance", nil, []testMod{player(flagMod("Every3UseCrit"))}},
		{"crit confirmation roll", "MuBiMZ8WiDZn", "CritChance", nil, []testMod{player(numMod("Accuracy", "MORE", -50))}},
		// 3247-3250.
		{"enemy SelfCritMultiplier", "MuBiMZ8WiDZn", "CritMultiplier", nil,
			[]testMod{enemy(numMod("SelfCritMultiplier", "BASE", 50))}},
		// 2253.
		{"enemy CannotEvade", "MuBiMZ8WiDZn", "AccuracyHitChance", nil, []testMod{enemy(flagMod("CannotEvade"))}},
		// 3319, 3324.
		{"enemy SelfDoubleDamageChance", "MuBiMZ8WiDZn", "AverageHit", nil,
			[]testMod{enemy(numMod("SelfDoubleDamageChance", "BASE", 30))}},
		// 2729: Intimidating Cry's double damage.
		{"intimidating cry double damage", "D37AzBTWIbhf", "IntimidatingAvgDmg", nil,
			[]testMod{enemy(numMod("SelfDoubleDamageChance", "BASE", 30))}},
		// 3529: enemy resistances and damage taken on hits.
		{"enemy damage taken from hits", "MuBiMZ8WiDZn", "AverageHit", nil,
			[]testMod{enemy(numMod("DamageTaken", "INC", 50))}},
		// 3529: the impale's stored physical hit.
		{"impale stored hit", "MuBiMZ8WiDZn", "ImpaleDPS", []testMod{player(numMod("ImpaleChance", "BASE", 100))},
			[]testMod{player(numMod("PhysicalDamage", "MORE", 100))}},
		// 4250-4254, 4312.
		{"impale chance", "MuBiMZ8WiDZn", "ImpaleChance", nil, []testMod{player(numMod("ImpaleChance", "BASE", 40))}},
		// 4322-4331.
		{"enemy ailment avoidance", "MuBiMZ8WiDZn", "BleedChanceOnHit", nil,
			[]testMod{enemy(numMod("AvoidBleed", "BASE", 50))}},
		// 1493-1496: debuff durations.
		{"debuff duration", "MuBiMZ8WiDZn", "BleedDuration", nil,
			[]testMod{enemy(numMod("BuffExpireFaster", "MORE", -50))}},
		{"impale duration", "MuBiMZ8WiDZn", "ImpaleDuration", nil,
			[]testMod{enemy(numMod("BuffExpireFaster", "MORE", -50))}},
		// 4623-4629.
		{"bleed effective multiplier", "MuBiMZ8WiDZn", "BleedDPS", nil,
			[]testMod{enemy(numMod("DamageTakenOverTime", "INC", 50))}},
		// 4893-4899.
		{"poison effective multiplier", "MuBiMZ8WiDZn", "PoisonDPS", nil,
			[]testMod{enemy(numMod("DamageTakenOverTime", "INC", 50))}},
		// 5233-5256.
		{"ignite effective multiplier", "OhQ2HoOzO2Z0", "IgniteDPS", nil,
			[]testMod{enemy(numMod("DamageTakenOverTime", "INC", 50))}},
		// 5756-5762.
		{"decay effective multiplier", "WOpwgBt54cCP", "DecayDPS", nil,
			[]testMod{enemy(numMod("DamageTakenOverTime", "INC", 50))}},
		// 5772.
		{"decay duration", "WOpwgBt54cCP", "DecayDuration", nil,
			[]testMod{enemy(numMod("BuffExpireFaster", "MORE", -50))}},
		// 5798-5803: burning ground the player drops.
		{"dropped burning ground", "OhQ2HoOzO2Z0", "BurningGroundDPS",
			[]testMod{player(numMod("DropsBurningGround", "BASE", 1e8))},
			[]testMod{enemy(numMod("DamageTakenOverTime", "INC", 50))}},
		// 5867-5883.
		{"skill DoT effective multiplier", "1U1EWET8OeON", "TotalDot", nil,
			[]testMod{enemy(numMod("DamageTakenOverTime", "INC", 50))}},
		// CalcDefence.lua:760.
		{"enemy reduceEnemyBlock", "MuBiMZ8WiDZn", "EffectiveBlockChance",
			[]testMod{player(numMod("BlockChance", "BASE", 40))}, []testMod{enemy(numMod("reduceEnemyBlock", "BASE", 10))}},
		// 762-772.
		{"lucky block", "MuBiMZ8WiDZn", "EffectiveBlockChance",
			[]testMod{player(numMod("BlockChance", "BASE", 40))}, []testMod{player(flagMod("BlockChanceIsLucky"))}},
		// 1166-1177 (this build's suppression is lucky already).
		{"unlucky suppression", "MuBiMZ8WiDZn", "EffectiveSpellSuppressionChance", nil,
			[]testMod{player(flagMod("SpellSuppressionChanceIsUnlucky"))}},
		// 1207.
		{"unlucky dodge", "MuBiMZ8WiDZn", "EffectiveAttackDodgeChance",
			[]testMod{player(numMod("AttackDodgeChance", "BASE", 30))}, []testMod{player(flagMod("DodgeChanceIsUnlucky"))}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := corpusBuild(t, tc.build)
			with := append(append([]testMod(nil), tc.common...), tc.probe...)
			eff := calcEffective(t, c, b, true, tc.common...)[tc.stat]
			effProbe := calcEffective(t, c, b, true, with...)[tc.stat]
			not := calcEffective(t, c, b, false, tc.common...)[tc.stat]
			notProbe := calcEffective(t, c, b, false, with...)[tc.stat]
			if eff == effProbe {
				t.Errorf(
					"%s with mode_effective: %v with and without the probe mods; the test needs a stat they change",
					tc.stat,
					eff,
				)
			}
			if not != notProbe {
				t.Errorf(
					"%s without mode_effective: %v, with the probe mods %v; want them ignored",
					tc.stat,
					not,
					notProbe,
				)
			}
		})
	}
}

// TestDropsBurningGround: CalcOffence.lua:5788-5813. -fxcKCnOf7D2 with the
// custom mods "Drops Burning Ground while moving, dealing 2500 Fire Damage
// per second for 4 seconds" and "25% increased Fire Damage": the oracle
// gives BurningGroundDPS 10901.6875, not from ignite.
func TestDropsBurningGround(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "-fxcKCnOf7D2")
	cfg := b.ActiveConfig()
	text := "Drops Burning Ground while moving, dealing 2500 Fire Damage per second for 4 seconds\n" +
		"25% increased Fire Damage"
	cfg.CustomMods = append(cfg.CustomMods, pobcode.CustomModBlock{Title: "test", Enabled: true, Text: text})
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Stats["BurningGroundDPS"]; !near(got, 10901.6875, 1e-9) || res.Stats["BurningGroundFromIgnite"] != 0 {
		t.Errorf(
			"BurningGroundDPS %v (from ignite %v), want 10901.6875 not from ignite",
			got,
			res.Stats["BurningGroundFromIgnite"],
		)
	}
}
