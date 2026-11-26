package engine

import (
	"slices"
	"testing"
)

// switchCases lists the case names of the per-skill switches the hook
// registry replaced (offence.go preDamageFunc, preSkillTypeFunc,
// postCritFunc, initialFunc; minions.go warcryExertMods), by hook kind.
var switchCases = map[string][]string{
	"preDamage": {
		"On Kill Monster Explosion",
		"Barrage", "Barrage of Volley Fire", "Blast Rain", "Vaal Lightning Arrow",
		"Blazing Salvo",
		"Forbidden Rite", "Forbidden Rite of Soul Sacrifice",
		"Freezing Pulse",
		"Lightning Tendrils of Eccentricity",
		"Kinetic Fusillade",
		"Molten Strike", "Molten Strike of the Zenith",
		"Avenging Flame",
		"Bladefall of Trarthus",
		"Tornado of Elemental Turbulence", "Tornado",
		"Storm Burst",
		"Spark", "Spark of the Nova",
		"Ice Spear", "Ice Spear of Splitting",
		"Toxic Rain", "Toxic Rain of Sporeburst", "Toxic Rain of Withering",
		"Shrapnel Ballista", "Shrapnel Ballista of Steel",
		"Bodyswap", "Bodyswap of Sacrifice",
		"Ball Lightning of Orbiting", "Ball Lightning", "Eye of Winter",
		"Seismic Trap", "Seismic Trap of Swells", "Blade Blast", "Blade Blast of Unloading",
		"Arcanist Brand", "Armageddon Brand", "Armageddon Brand of Recall", "Penance Brand",
		"Penance Brand of Dissipation", "Penance Brand of Conduction", "Storm Brand", "Storm Brand of Indecision",
		"Wintertide Brand",
		"Flameblast", "Flameblast of Celerity", "Flameblast of Contraction",
		"Static Strike",
		"Righteous Fire", "Righteous Fire of Arcane Devotion", "Vaal Righteous Fire",
		"Divine Ire", "Divine Ire of Holy Lightning", "Divine Ire of Disintegration",
	},
	"preSkillType": {"Rage Vortex", "Bane", "Bane of Condemnation"},
	"postCrit":     {"Kinetic Fusillade", "Lightning Tendrils of Eccentricity"},
	"initial":      {"Blood Sacrament", "Cyclone", "Vaal Cyclone", "Cyclone of Tumult"},
	"warcryShare":  {"Rallying Cry", "Seismic Cry"},
}

// TestSkillHookRegistryCoversSwitch checks that the names with a hook of
// each kind are exactly the case names of the switch it replaced.
func TestSkillHookRegistryCoversSwitch(t *testing.T) {
	has := map[string]func(h *skillHooks) bool{
		"preDamage":    func(h *skillHooks) bool { return h.preDamage != nil },
		"preSkillType": func(h *skillHooks) bool { return h.preSkillType != nil },
		"postCrit":     func(h *skillHooks) bool { return h.postCrit != nil },
		"initial":      func(h *skillHooks) bool { return h.initial != nil },
		"warcryShare":  func(h *skillHooks) bool { return h.warcryShare != nil },
	}
	for kind, want := range switchCases {
		var got []string
		for name, h := range skillHookRegistry {
			if has[kind](h) {
				got = append(got, name)
			}
		}
		slices.Sort(got)
		want = slices.Sorted(slices.Values(want))
		if !slices.Equal(got, want) {
			t.Errorf("%s hooks:\n got %q\nwant %q", kind, got, want)
		}
	}
}

// TestSkillHookRegistryDuplicatePanics checks registerSkill's guard
// against a second hook of the same kind for one name, and that hooks of
// different kinds merge.
func TestSkillHookRegistryDuplicatePanics(t *testing.T) {
	// Swapping the global registry is safe only because engine tests do not use t.Parallel.
	saved := skillHookRegistry
	t.Cleanup(func() { skillHookRegistry = saved })
	skillHookRegistry = map[string]*skillHooks{}
	noop := func(*ModDB, *activeSkill) {}
	registerSkill([]string{"X"}, skillHooks{preSkillType: noop})
	registerSkill([]string{"X"}, skillHooks{postCrit: func(*activeSkill, map[string]float64, map[string]float64) {}})
	if h := skillHookRegistry["X"]; h.preSkillType == nil || h.postCrit == nil {
		t.Fatal("hooks of different kinds did not merge")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate preSkillType hook did not panic")
		}
	}()
	registerSkill([]string{"X"}, skillHooks{preSkillType: noop})
}

// TestMinionSkillHooks: a minion skill has only its own granted effect's
// functions (Data/Skills/minion.lua, spectre.lua), not those of a player
// skill of the same name.
func TestMinionSkillHooks(t *testing.T) {
	summon := triggerTestSkill("Raise Spectre")
	minionSkill := func(id, name string) *activeSkill {
		s := triggerTestSkill(name)
		s.active.id, s.summon = id, summon
		return s
	}
	if hooksOf(triggerTestSkill("Cyclone")) == nil {
		t.Fatal("the player's Cyclone has no hooks")
	}
	if h := hooksOf(minionSkill("AzmeriVikingCyclone", "Cyclone")); h != nil {
		t.Errorf("a spectre's Cyclone has the player's hooks: %+v", h)
	}
	// spectre.lua:7277: Barrage hits once per projectile, without the
	// player Barrage's skill part check.
	hydra := minionSkill("AzmeriHydraBarrage", "Barrage")
	preDamageFunc(nil, hydra, map[string]float64{"ProjectileCount": 10})
	if got := hydra.data.dpsMultiplier.or(1); got != 10 {
		t.Errorf("Azmeri Hydra Barrage dpsMultiplier %v, want 10", got)
	}
	// minion.lua:756 and 1756: one hit per summoned minion.
	for _, id := range []string{"GAZombieCorpseGroundImpact", "MinionInstability"} {
		s := minionSkill(id, "x")
		preDamageFunc(nil, s, map[string]float64{})
		if got, _ := s.data.hitTimeOverride.get(); got != 1 {
			t.Errorf("%s without summonSpeed: hitTimeOverride %v, want 1", id, got)
		}
		summon.data.set("summonSpeed", 2.5)
		preDamageFunc(nil, s, map[string]float64{})
		if got, _ := s.data.hitTimeOverride.get(); got != 0.4 {
			t.Errorf("%s: hitTimeOverride %v, want 0.4", id, got)
		}
		summon.data.del("summonSpeed")
	}
}

// TestFallingSlamSummonSpeed: with Falling Slam as the Raised Zombie's main
// skill it hits at Raise Zombie's summoning rate (skillData.summonSpeed,
// CalcOffence.lua:4141), which needs the player's offence before the
// minion's. JEhRArCR8lqN with skillMinionSkill 3: PoB 1.6153846153846 and
// 21588.366993529.
func TestFallingSlamSummonSpeed(t *testing.T) {
	b := corpusBuild(t, "JEhRArCR8lqN")
	set := b.ActiveSkills()
	for gi := range set.Groups {
		for k := range set.Groups[gi].Gems {
			if g := &set.Groups[gi].Gems[k]; g.NameSpec == "Raise Zombie" {
				g.SkillMinionSkill = 3
			}
		}
	}
	mins, err := newCalc(t).CalcMinions(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mins {
		if m.Minion == "Raised Zombie" {
			if got := m.Stats["Speed"]; !near(got, 1.6153846153846, 1e-9) {
				t.Errorf("Falling Slam speed %v, want 1.6153846153846", got)
			}
			if got := m.Stats["TotalDPS"]; !near(got, 21588.366993529, 1e-9) {
				t.Errorf("Falling Slam TotalDPS %v, want 21588.366993529", got)
			}
			return
		}
	}
	t.Fatal("no Raised Zombie")
}
