package engine

import (
	"math"
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
)

// tickRate is 1 / the cooldown v rounded up to server ticks.
func tickRate(v float64) float64 { return 1 / (math.Ceil(v*serverTickRate) / serverTickRate) }

// triggerSupport is a trigger support effect named name at level 20 with
// cooldown cd.
func triggerSupport(name string, cd float64) *skillEffect {
	return &skillEffect{level: 20, ge: &gamedata.Skill{Name: name, Support: true, IsTrigger: true,
		AddSkillTypes: []int{stTriggered}, Levels: map[int]*gamedata.SkillLevel{20: {Cooldown: cd}}}}
}

// CalcTriggers.lua:150-234: Focus triggers the spell once per Focus
// duration plus cooldown; the trigger rate cap is the support's cooldown.
func TestFocusTrigger(t *testing.T) {
	c := newCalc(t)
	s := triggerTestSkill("Arc")
	s.effects = append(s.effects, triggerSupport("Focus", 0.25))
	s.mods.NewMod("FocusCooldownRecovery", "INC", 60, "Tree")
	db := NewModDB()
	c.triggers(db, s, hitSource{}, nil)
	f := c.data.Skills.Effects["Focus"]
	want := 1 / (f.ConstantStats[0].Value/1000 + f.Levels[1].Cooldown/1.6)
	if got := db.output["SkillTriggerRate"]; math.Abs(got-want) > 1e-12 || s.infoTrigger != "Focus" ||
		!s.flags["globalTrigger"] || !truthy(s.data.val("triggered")) {
		t.Errorf("rate %v trigger %q, want %v", got, s.infoTrigger, want)
	}
	if got := db.output["TriggerRateCap"]; math.Abs(got-tickRate(0.25)) > 1e-12 {
		t.Errorf("TriggerRateCap %v, want %v", got, tickRate(0.25))
	}
}

// CalcTriggers.lua:946-960: Unseen Strike triggers twice a second while
// phasing and is disabled otherwise.
func TestHiddenBladeTrigger(t *testing.T) {
	c := &Calculator{}
	s := triggerTestSkill("Unseen Strike")
	s.data.set("triggerSource", "The Hidden Blade")
	s.data.set("triggeredByUnique", true)
	db := NewModDB()
	c.triggers(db, s, hitSource{}, nil)
	if !s.disabled || truthy(s.data.val("triggered")) {
		t.Errorf("not phasing: disabled %v triggered %v", s.disabled, s.data.val("triggered"))
	}
	s = triggerTestSkill("Unseen Strike")
	s.data.set("triggerSource", "The Hidden Blade")
	s.data.set("triggeredByUnique", true)
	db = NewModDB()
	db.Add(Mod{Name: "Condition:Phasing", Type: "FLAG", Value: true, Source: "Config"})
	c.triggers(db, s, hitSource{}, nil)
	if got := db.output["SkillTriggerRate"]; s.disabled || got != 2 || db.output["TriggerRateCap"] != 2 {
		t.Errorf("phasing: disabled %v rate %v cap %v, want 2", s.disabled, got, db.output["TriggerRateCap"])
	}
}

// CalcTriggers.lua:1574-1593: the shortest Hextoad interval caps the
// Bursting Toad rate; without one there is no config.
func TestBurstingToadTrigger(t *testing.T) {
	c := &Calculator{}
	toad := triggerTestSkill("Bursting Toad")
	toad.types[stTriggered] = true
	db := NewModDB()
	db.activeSkills = []*activeSkill{toad}
	c.triggers(db, toad, hitSource{}, nil)
	if truthy(toad.data.val("triggered")) {
		t.Error("triggered without a Hextoad interval")
	}
	hex := triggerTestSkill("Despair")
	hex.data.set("hextoadTriggerInterval", 0.9)
	other := triggerTestSkill("Punishment")
	other.data.set("hextoadTriggerInterval", 0.7)
	db = NewModDB()
	db.activeSkills = []*activeSkill{hex, other, toad}
	c.triggers(db, toad, hitSource{}, nil)
	if got := db.output["SkillTriggerRate"]; math.Abs(got-1/0.7) > 1e-12 {
		t.Errorf("rate %v, want %v", got, 1/0.7)
	}
}

// CalcTriggers.lua:1247-1266: Automation itself has no effective rate; a
// supported spell takes the active part's cooldown.
func TestAutomationTrigger(t *testing.T) {
	c := &Calculator{}
	auto := triggerTestSkill("Automation")
	auto.active.ge.Levels = map[int]*gamedata.SkillLevel{20: {Cooldown: 1.2}}
	auto.main = true
	db := NewModDB()
	db.activeSkills = []*activeSkill{auto}
	c.triggers(db, auto, hitSource{}, nil)
	if got := db.output["SkillTriggerRate"]; got != 0 || !auto.flags["skipEffectiveRate"] ||
		!auto.flags["globalTrigger"] {
		t.Errorf("Automation: rate %v flags %v", got, auto.flags)
	}
	auto = triggerTestSkill("Automation")
	auto.active.ge.Levels = map[int]*gamedata.SkillLevel{20: {Cooldown: 1.2}}
	spell := triggerTestSkill("Steelskin")
	spell.effects = append(spell.effects, triggerSupport("Automation", 0))
	spell.main = true
	spell.mods.NewMod("CooldownRecovery", "INC", 20, "Tree")
	db = NewModDB()
	db.activeSkills = []*activeSkill{auto, spell}
	c.triggers(db, spell, hitSource{}, nil)
	icdr := 1 + 20.0/100
	want := tickRate(1.2 / icdr)
	if got := db.output["SkillTriggerRate"]; math.Abs(got-want) > 1e-12 || spell.infoTrigger != "Automation" {
		t.Errorf("supported: rate %v trigger %q, want %v", got, spell.infoTrigger, want)
	}
}

// CalcTriggers.lua:1343-1361, 619-622, 637, 835: the Arcanist Brand
// activation rate is the source rate, and its activation cooldown is not
// rounded to server ticks nor changed by cooldown recovery.
func TestArcanistBrandTrigger(t *testing.T) {
	c := &Calculator{}
	brand := triggerTestSkill("Arcanist Brand")
	brand.data.set("repeatFrequency", 0.75)
	brand.data.set("attachedBrandCount", 2.0)
	brand.mods.NewMod("BrandActivationFrequency", "INC", 20, "Tree")
	brand.mods.NewMod("BrandActivationFrequency", "MORE", 50, "Tree")
	spell := triggerTestSkill("Fireball")
	spell.effects = append(spell.effects, triggerSupport("Arcanist Brand", 0))
	spell.data.set("triggeredByBrand", true)
	spell.main = true
	spell.mods.NewMod("CooldownRecovery", "INC", 30, "Tree")
	db := NewModDB()
	db.activeSkills = []*activeSkill{brand, spell}
	c.triggers(db, spell, hitSource{}, nil)
	rate := 0.75 * 1.2 * 1.5 * 2
	if got := db.output["EffectiveSourceRate"]; math.Abs(got-rate) > 1e-12 {
		t.Errorf("EffectiveSourceRate %v, want %v", got, rate)
	}
	if got, want := db.output["TriggerRateCap"], 1/(0.75/1.5/1.2); math.Abs(got-want) > 1e-12 {
		t.Errorf("TriggerRateCap %v, want %v", got, want)
	}
	if got := db.output["SkillTriggerRate"]; got <= 0 || got > rate {
		t.Errorf("SkillTriggerRate %v", got)
	}
}

// CalcTriggers.lua:1362-1366: the Cast on Death config returns nothing, so
// triggered is cleared again after it set it.
func TestCastOnDeathTrigger(t *testing.T) {
	s := triggerTestSkill("Fireball")
	s.effects = append(s.effects, triggerSupport("Cast on Death", 0))
	db := NewModDB()
	(&Calculator{}).triggers(db, s, hitSource{}, nil)
	if truthy(s.data.val("triggered")) || !s.flags["globalTrigger"] {
		t.Errorf("triggered %v globalTrigger %v", s.data.val("triggered"), s.flags["globalTrigger"])
	}
}

// findTriggerSkill: a source must pay the minimum cost per use
// (minimumCostPerUse), and source-weapon triggers only take attacks with
// the trigger's weapon (CalcTriggers.lua:97, 418-425).
func TestTriggerSourceFilters(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "HBcW_8Hvft-z")
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	db := res.DB
	w1, w2 := playerWeapons(db, db.gearMods, b.ActiveSpec().ClassID)
	var eq *activeSkill
	for _, s := range db.activeSkills {
		if s.active.ge.Name == "Earthquake of Amplification" {
			eq = s
		}
	}
	if eq == nil {
		t.Fatal("no Earthquake")
	}
	o := c.offenceOnCopy(db, eq, w1, w2)
	for _, tc := range []struct {
		min  float64
		want bool
	}{{o["ManaCost"], true}, {o["ManaCost"] + 1, false}} {
		cfg := &triggerConfig{minimumCost: &triggerCost{"ManaCost", tc.min}}
		c.findTriggerSkill(db, eq, cfg, w1, w2)
		if (cfg.source == eq) != tc.want || cfg.cached != tc.want {
			t.Errorf("minimum %v: source %v, want %v", tc.min, cfg.source != nil, tc.want)
		}
	}
	main := triggerTestSkill("Fiery Impact")
	main.group = eq.group
	for _, slot := range []string{"Weapon 1", "Weapon 2"} {
		g := *eq.group
		g.Slot = slot
		main.group = &g
		cfg := triggerConfigs["FieryImpactHeistMaceImplicit"](db, main)
		db.output = map[string]float64{}
		db.activeSkills = append(db.activeSkills[:0:0], eq, main)
		c.defaultTrigger(db, main, nil, cfg, w1, w2)
		found := cfg.source == eq
		if want := slot == "Weapon 1"; found != want || truthy(main.data.val("triggered")) != want {
			t.Errorf("%s: source found %v, want %v", slot, found, want)
		}
	}
}
