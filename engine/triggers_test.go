package engine

import (
	"math"
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// triggerTestSkill is a bare active skill named name for calcs.triggers.
func triggerTestSkill(name string) *activeSkill {
	e := &skillEffect{ge: &gamedata.Skill{Name: name}}
	return &activeSkill{active: e, effects: []*skillEffect{e}, mods: NewModDB(), cfg: &Cfg{},
		data: &skillData{}, types: map[int]bool{}, flags: map[string]bool{}}
}

// Global triggers (CalcTriggers.lua:1196-1210, 1311-1314) use the trigger
// rate cap as the source rate: one trigger per (tick-rounded) cooldown.
func TestTriggersGlobalSelfSource(t *testing.T) {
	c := &Calculator{}
	cooldown := 0.25
	wantRate := 1 / (math.Ceil(cooldown*serverTickRate) / serverTickRate)

	ts := triggerTestSkill("Tempest Shield")
	ts.data.cooldown = optF{cooldown, true}
	db := NewModDB()
	c.triggers(db, ts, hitSource{}, nil)
	if got := db.output["SkillTriggerRate"]; math.Abs(got-wantRate) > 1e-9 || ts.infoTrigger != "Tempest Shield" {
		t.Errorf("Tempest Shield: rate %v trigger %q, want %v %q", got, ts.infoTrigger, wantRate, "Tempest Shield")
	}

	// A spell supported by Cast when Damage Taken: the threshold scales
	// with CWDTThreshold mods.
	cwdt := &skillEffect{ge: &gamedata.Skill{Name: "Cast when Damage Taken", Support: true, IsTrigger: true,
		AddSkillTypes: []int{stTriggered}}}
	sp := triggerTestSkill("Frostbolt")
	sp.effects = append(sp.effects, cwdt)
	sp.data.cooldown = optF{cooldown, true}
	sp.data.set("triggeredByDamageTaken", 528.0)
	sp.mods.NewMod("CWDTThreshold", "INC", 50, "Tree")
	db = NewModDB()
	c.triggers(db, sp, hitSource{}, nil)
	if got := db.output["SkillTriggerRate"]; math.Abs(got-wantRate) > 1e-9 ||
		sp.infoTrigger != "Cast when Damage Taken" {
		t.Errorf("CWDT: rate %v trigger %q, want %v", got, sp.infoTrigger, wantRate)
	}
	if got := db.output["CWDTThreshold"]; got != 792 {
		t.Errorf("CWDTThreshold %v, want 792", got)
	}

	// Without a damage threshold the CWDT config is nil and no other
	// config matches: the skill is not triggered.
	sp = triggerTestSkill("Frostbolt")
	sp.effects = append(sp.effects, cwdt)
	sp.data.set("triggered", true)
	db = NewModDB()
	c.triggers(db, sp, hitSource{}, nil)
	if _, ok := db.output["SkillTriggerRate"]; ok || truthy(sp.data.val("triggered")) {
		t.Errorf("CWDT without threshold: output %v triggered %v", db.output, sp.data.val("triggered"))
	}

	// A threshold of 0 is set (truthy in Lua): the skill is triggered.
	sp = triggerTestSkill("Frostbolt")
	sp.effects = append(sp.effects, cwdt)
	sp.data.set("triggeredByDamageTaken", 0.0)
	db = NewModDB()
	c.triggers(db, sp, hitSource{}, nil)
	if !truthy(sp.data.val("triggered")) {
		t.Error("CWDT with threshold 0 is not triggered")
	}
}

// withTestTriggerConfig registers a config for one test.
func withTestTriggerConfig(t *testing.T, key string, f func(*ModDB, *activeSkill) *triggerConfig) {
	t.Helper()
	// Safe only because engine tests do not use t.Parallel.
	triggerConfigs[key] = f
	t.Cleanup(func() { delete(triggerConfigs, key) })
}

// config.triggerChance = config.triggerChance or srcInstance.triggerChance
// keeps an explicit 0 (truthy in Lua), which gives no triggers; without a
// config chance the granted skill's chance applies.
func TestTriggerChanceExplicitZero(t *testing.T) {
	c := &Calculator{}
	src := triggerTestSkill("Source")
	src.types[stSpell] = true
	for _, tc := range []struct {
		name    string
		has     bool
		wantOff bool
	}{{"zero", true, true}, {"unset", false, false}} {
		withTestTriggerConfig(t, "test chance "+tc.name, func(*ModDB, *activeSkill) *triggerConfig {
			return &triggerConfig{source: src, sourceOut: map[string]float64{}, trigRate: 2, hasTriggerChance: tc.has}
		})
		main := triggerTestSkill("Test Chance " + tc.name)
		main.active.srcTriggerChance, main.active.srcHasTriggerChance = 50, true
		db := NewModDB()
		c.triggers(db, main, hitSource{}, nil)
		rate := db.output["SkillTriggerRate"]
		if (rate == 0) != tc.wantOff || math.IsNaN(rate) || math.IsInf(rate, 0) {
			t.Errorf("%s: SkillTriggerRate %v", tc.name, rate)
		}
	}
}

// CalcTriggers.lua:1629: configTable[skillId] is tried first, with the id
// as it is; no game skill id is also a name key of the Go table.
func TestTriggerConfigBySkillID(t *testing.T) {
	withTestTriggerConfig(t, "TestSkillId", func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{selfSource: true, triggerName: "by id"}
	})
	main := triggerTestSkill("Tempest Shield")
	main.active.id = "TestSkillId"
	db := NewModDB()
	(&Calculator{}).triggers(db, main, hitSource{}, nil)
	if main.infoTrigger != "by id" {
		t.Errorf("trigger %q, want the id's config", main.infoTrigger)
	}
	calc := newCalc(t)
	for id := range calc.data.Skills.Effects {
		if triggerConfigs[id] != nil {
			t.Errorf("skill id %q is also a trigger config name key", id)
		}
	}
}

// The triggered skill's own mana cost is computed once per Calc: a cached
// value is used without another offence pass.
func TestManaforgedCostCached(t *testing.T) {
	db := NewModDB()
	main := triggerTestSkill("Toxic Rain")
	main.data.set("triggeredByManaforged", true)
	main.data.set("ManaForgedArrowsPercentThreshold", 3.0)
	db.triggeredCostCache = map[*activeSkill]float64{main: 10}
	// &Calculator{} has no data: an offence pass would panic.
	src := map[string]float64{"ManaCostRaw": 7}
	got := (&Calculator{}).manaforgedRate(db, main, &triggerConfig{}, src, 2, hitSource{}, nil)
	if want := 2 / math.Ceil(30.0/7); got != want {
		t.Errorf("rate %v, want %v", got, want)
	}
	got = (&Calculator{}).manaforgedRate(db, main, &triggerConfig{}, map[string]float64{}, 2, hitSource{}, nil)
	if got != 0 {
		t.Errorf("source without mana cost: rate %v, want 0", got)
	}
}

// CalcSetup.lua:1556-1557: a skill granted by an item keeps its
// ExtraSkill mod's triggered and triggerChance, and isTriggered sees it.
func TestGrantedTrigger(t *testing.T) {
	src := "Item:54:Lioneye's Paws, Bronzescale Boots"
	db := NewModDB()
	db.Add(Mod{Name: "ExtraSkill", Type: "LIST", Source: src,
		Value: map[string]any{"skillId": "RainOfArrows", "level": 5.0, "triggered": true, "triggerChance": 30.0}})
	g := &pobcode.SocketGroup{Source: src}
	if tr, ch, has := grantedTrigger(db, g, "RainOfArrows"); !tr || ch != 30 || !has {
		t.Errorf("got %v %v %v, want true 30 true", tr, ch, has)
	}
	if tr, _, _ := grantedTrigger(db, &pobcode.SocketGroup{Source: "Item:1:Other"}, "RainOfArrows"); tr {
		t.Error("a mod of another source must not match")
	}
	db.Add(Mod{Name: "ExtraSkill", Type: "LIST", Source: "Item:2:Other",
		Value: map[string]any{"skillId": "RainOfArrows", "level": 5.0, "triggered": true}})
	if tr, _, has := grantedTrigger(db, &pobcode.SocketGroup{Source: "Item:2:Other"}, "RainOfArrows"); !tr || has {
		t.Errorf("no triggerChance: got triggered %v set %v, want true false", tr, has)
	}
	s := triggerTestSkill("Rain of Arrows")
	if isTriggered(s) {
		t.Fatal("plain skill counts as triggered")
	}
	s.active.srcTriggered = true
	if !isTriggered(s) {
		t.Error("srcInstance.triggered does not count")
	}
}

// TestMinionTriggers: a minion's main skill with no trigger config is not
// triggered (CalcTriggers.lua:1640-1642), and the Holy Relic's Nova takes
// its rate from the player's attacks (configTable["nova"]): a2nRZLW9ZVuu's
// Holy Relic, 2.8876 as in PoB.
func TestMinionTriggers(t *testing.T) {
	c := &Calculator{}
	ms := triggerTestSkill("Minion Melee")
	ms.data.set("triggered", true)
	c.minionTriggers(NewModDB(), NewModDB(), triggerTestSkill("Raise Zombie"), ms, &Result{})
	if _, ok := ms.data.get("triggered"); ok {
		t.Error("a minion skill without a trigger config stays triggered")
	}

	mins, err := newCalc(t).CalcMinions(corpusBuild(t, "a2nRZLW9ZVuu"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mins {
		if m.Minion == "Holy Relic" {
			if got := m.Stats["Speed"]; !near(got, 2.88760368, 1e-6) {
				t.Errorf("Holy Relic Nova speed %v, want 2.88760368", got)
			}
			return
		}
	}
	t.Fatal("no Holy Relic")
}

// TestMinionSnipeTrigger: configTable["snipe"] for calcs.triggers(env,
// env.minion) reads and changes env.player.mainSkill (CalcTriggers.lua
// 529-604). A summoning skill that Snipe does not trigger loses
// skillData.triggered, and so does the minion's Snipe; one that Snipe
// triggers gets the stage mods, and with no Snipe to channel the minion's
// skill is self-cast.
func TestMinionSnipeTrigger(t *testing.T) {
	c := &Calculator{}
	g := &pobcode.SocketGroup{Slot: "Weapon 1"}
	pm := triggerTestSkill("Raise Spectre")
	pm.group = g
	pm.data.set("triggered", true)
	ms := triggerTestSkill("Snipe")
	ms.summon = pm
	ms.data.set("triggered", true)
	db, mdb := NewModDB(), NewModDB()
	db.activeSkills = []*activeSkill{pm}
	c.minionTriggers(db, mdb, pm, ms, &Result{})
	if _, ok := pm.data.get("triggered"); ok {
		t.Error("the summoning skill keeps skillData.triggered")
	}
	if _, ok := ms.data.get("triggered"); ok {
		t.Error("the minion's Snipe stays triggered")
	}

	pm.data.set("triggeredBySnipe", true)
	db.NewMod("Multiplier:SnipeStage", "BASE", 2, "Config")
	db.NewMod("Multiplier:SnipeStagesMax", "BASE", 6, "Base")
	ms.data.set("triggered", true)
	c.minionTriggers(db, mdb, pm, ms, &Result{})
	if got := pm.mods.Sum("BASE", "Multiplier:SnipeStages"); got != 2 {
		t.Errorf("Snipe stages on the summoning skill %v, want 2", got)
	}
	if ms.mods.Sum("BASE", "Multiplier:SnipeStages") != 0 {
		t.Error("the stage mods went to the minion's skill")
	}
	if _, ok := ms.data.get("triggered"); ok {
		t.Error("the minion's Snipe is triggered without a Snipe source")
	}
	if _, ok := mdb.output["SkillTriggerRate"]; ok {
		t.Errorf("minion trigger rate without a source: %v", mdb.output)
	}
}

// TestAddedCastTime: processAddedCastTime (CalcTriggers.lua:45-62) adds the
// cast time at the rounded cast speed to the trigger cooldown, for
// env.player.mainSkill in the default handler (634-637).
func TestAddedCastTime(t *testing.T) {
	s := triggerTestSkill("Arc")
	s.active.ge.CastTime = 0.75
	if _, ok := processAddedCastTime(s); ok {
		t.Fatal("added cast time without the flag")
	}
	s.mods.Add(Mod{Name: "SpellCastTimeAddedToCooldownIfTriggered", Type: "FLAG", Value: true})
	s.mods.NewMod("Speed", "INC", 50, "Tree")
	if got, ok := processAddedCastTime(s); !ok || got != 0.5 || !s.flags["addsCastTime"] {
		t.Fatalf("added cast time %v %v", got, ok)
	}
	c := &Calculator{}
	s.main = true
	s.data.cooldown = optF{0.25, true}
	db := NewModDB()
	db.activeSkills = []*activeSkill{s}
	c.defaultTrigger(db, s, nil, &triggerConfig{selfSource: true}, hitSource{}, nil)
	want := 1 / (math.Ceil(0.5*serverTickRate) / serverTickRate)
	if got := db.output["TriggerRateCap"]; math.Abs(got-want) > 1e-9 || db.output["addsCastTime"] != 0.5 {
		t.Errorf("TriggerRateCap %v addsCastTime %v, want %v 0.5", got, db.output["addsCastTime"], want)
	}
}

// TestSkipEffectiveRate: the Spellslinger skill itself skips the effective
// rate (CalcTriggers.lua:1221-1224, 747): its trigger rate is 0.
func TestSkipEffectiveRate(t *testing.T) {
	c := &Calculator{}
	for _, name := range []string{"Spellslinger", "Arc"} {
		s := triggerTestSkill(name)
		s.main = true
		db := NewModDB()
		db.activeSkills = []*activeSkill{s}
		cfg := triggerConfigs["spellslinger"](db, s)
		src := triggerTestSkill("Power Siphon")
		cfg.source, cfg.sourceOut, cfg.trigRate = src, map[string]float64{}, 2
		c.defaultTrigger(db, s, nil, cfg, hitSource{}, nil)
		got := db.output["SkillTriggerRate"]
		if skip := name == "Spellslinger"; skip != (got == 0) || s.flags["skipEffectiveRate"] != skip {
			t.Errorf("%s: SkillTriggerRate %v skipEffectiveRate %v", name, got, s.flags["skipEffectiveRate"])
		}
	}
}
