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
}

// CalcSetup.lua:1556-1557: a skill granted by an item keeps its
// ExtraSkill mod's triggered and triggerChance, and isTriggered sees it.
func TestGrantedTrigger(t *testing.T) {
	src := "Item:54:Lioneye's Paws, Bronzescale Boots"
	db := NewModDB()
	db.Add(Mod{Name: "ExtraSkill", Type: "LIST", Source: src,
		Value: map[string]any{"skillId": "RainOfArrows", "level": 5.0, "triggered": true, "triggerChance": 30.0}})
	g := &pobcode.SocketGroup{Source: src}
	if tr, ch := grantedTrigger(db, g, "RainOfArrows"); !tr || ch != 30 {
		t.Errorf("got %v %v, want true 30", tr, ch)
	}
	if tr, _ := grantedTrigger(db, &pobcode.SocketGroup{Source: "Item:1:Other"}, "RainOfArrows"); tr {
		t.Error("a mod of another source must not match")
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
