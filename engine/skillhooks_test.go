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
		"Tornado of Elemental Turbulence",
		"Storm Burst",
		"Spark", "Spark of the Nova",
		"Ice Spear", "Ice Spear of Splitting",
		"Toxic Rain", "Toxic Rain of Sporeburst", "Toxic Rain of Withering",
		"Shrapnel Ballista", "Shrapnel Ballista of Steel",
		"Bodyswap", "Bodyswap of Sacrifice",
		"Ball Lightning of Orbiting",
		"Arcanist Brand", "Armageddon Brand", "Armageddon Brand of Recall", "Penance Brand",
		"Penance Brand of Dissipation", "Penance Brand of Conduction", "Storm Brand", "Storm Brand of Indecision",
		"Wintertide Brand",
		"Flameblast", "Flameblast of Celerity", "Flameblast of Contraction",
		"Static Strike",
		"Righteous Fire", "Righteous Fire of Arcane Devotion", "Vaal Righteous Fire",
		"Divine Ire", "Divine Ire of Holy Lightning", "Divine Ire of Disintegration",
	},
	"preSkillType": {"Rage Vortex"},
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
	noop := func(*activeSkill) {}
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
