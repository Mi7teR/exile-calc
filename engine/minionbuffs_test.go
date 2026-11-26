package engine

import (
	"strings"
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
)

// TestMinionExtraCurses: "Check for extra curses" for env.minion
// (CalcPerform.lua:3178-3224): an ExtraCurse without applyToPlayer joins
// minionCurses, one with it curses the minion itself; each pass takes only
// its own part.
func TestMinionExtraCurses(t *testing.T) {
	c := newCalc(t)
	extra := func(id string, toSelf bool) Mod {
		v := map[string]any{"skillId": id, "level": 1.0}
		if toSelf {
			v["applyToPlayer"] = true
		}
		return Mod{Name: "ExtraCurse", Type: "LIST", Value: v, Source: "Item"}
	}
	mdb := NewModDB()
	mdb.enemy = NewModDB()
	mdb.Add(extra("Enfeeble", false))
	mdb.Add(extra("Vulnerability", true))
	curses := c.actorExtraCurses(mdb, extraCursePass{enemy: true, minion: true}, &Result{})
	if len(curses) != 1 || curses[0].name != "Enfeeble" || !curses[0].minion || len(curses[0].mods) == 0 {
		t.Fatalf("enemy pass: %+v", curses)
	}
	if mdb.conditions["Cursed"] {
		t.Error("the enemy pass cursed the minion")
	}
	if got := c.actorExtraCurses(mdb, extraCursePass{self: true}, &Result{}); len(got) != 0 {
		t.Errorf("the self pass returned enemy curses: %+v", got)
	}
	if !mdb.conditions["Cursed"] || !mdb.conditions["AffectedByVulnerability"] || mdb.multipliers["CurseOnSelf"] != 1 {
		t.Errorf("self pass: conditions %v multipliers %v", mdb.conditions, mdb.multipliers)
	}

	// The main minion's ExtraCurse reaches the enemy through the curse
	// slots: wYVdkskvi67I with an ExtraCurse on the casting minion.
	b := corpusBuild(t, "wYVdkskvi67I")
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	db := res.DB
	main := mainSkill(db.activeSkills)
	m := c.minionOf(main, b.Level, db.enemyEnv.level)
	if m == nil {
		t.Fatal("the main skill has no minion")
	}
	casting, _ := c.castingMinionSkills(db, main, m, &Result{})
	casting.Add(extra("Punishment", false))
	got := c.minionExtraCurses(db, db.activeSkills, &Result{})
	if len(got) != 1 || got[0].name != "Punishment" || !got[0].minion {
		t.Errorf("minionExtraCurses: %+v", got)
	}
}

// TestMinionAuraTotem: while the player's main skill is a totem, a
// minion's aura gives the totem its energy shield, armour, evasion and
// resistance mods as Totem<name> (CalcPerform.lua:2934-2964), with the
// spectre's name as their source.
func TestMinionAuraTotem(t *testing.T) {
	mods := totemAuraMods(1.25,
		[]Mod{{Name: "FireResistMax", Type: "BASE", Value: 1.0}},
		[]Mod{{Name: "Armour", Type: "INC", Value: 15.0}, {Name: "Evasion", Type: "BASE", Value: 10.5},
			{Name: "Life", Type: "INC", Value: 10.0}, {Name: "ChaosResist", Type: "BASE", Value: 7.0}})
	want := []struct {
		name string
		v    float64
	}{{"TotemFireResistMax", 1}, {"TotemArmour", 18}, {"TotemEvasion", 13.125}, {"TotemChaosResist", 8}}
	if len(mods) != len(want) {
		t.Fatalf("totem mods %+v", mods)
	}
	for i, w := range want {
		if mods[i].Name != w.name || mods[i].Value != w.v {
			t.Errorf("mod %d: %s %v, want %s %v", i, mods[i].Name, mods[i].Value, w.name, w.v)
		}
	}

	c := newCalc(t)
	res, err := c.Calc(corpusBuild(t, "wYVdkskvi67I"))
	if err != nil {
		t.Fatal(err)
	}
	db := res.DB
	totem := triggerTestSkill("Shockwave Totem")
	totem.flags["totem"] = true
	added := map[string][]Mod{}
	for _, s := range db.activeSkills {
		if s.summon == nil && s.flags["spectre"] {
			c.minionAurasOnPlayer(db, s, totem, func(l []Mod, name string) { added[name] = append(added[name], l...) },
				func([]Mod, string) {}, &Result{})
		}
	}
	l, ok := added["Totem Determination"]
	if !ok || !totem.mods.conditions["AffectedByDetermination"] || !totem.mods.conditions["AffectedByAura"] {
		t.Fatalf("no Totem Determination: %v %v", added, totem.mods.conditions)
	}
	found := false
	for _, m := range l {
		found = found || m.Name == "TotemArmour"
		if !strings.HasPrefix(m.Source, "Spectre:") || strings.Contains(m.Source, " - ") {
			t.Errorf("totem mod source %q, want Spectre:<minion>", m.Source)
		}
	}
	if !found {
		t.Errorf("Totem Determination without TotemArmour: %+v", l)
	}
}

// TestSetSpectreSource: a spectre's aura mods on the player are sourced
// "Spectre:<aura> - <minion>" (CalcPerform.lua:2814-2826, 2904).
func TestSetSpectreSource(t *testing.T) {
	res, err := newCalc(t).Calc(corpusBuild(t, "wYVdkskvi67I"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range res.DB.mods {
		for _, m := range l {
			if strings.HasPrefix(m.Source, "Spectre:Determination - ") {
				found = true
			}
		}
	}
	if !found {
		t.Error("no player mod from the spectre's Determination with its spectre source")
	}
	mods := []Mod{{Name: "Armour", Source: "Skill:Determination"}}
	plain := triggerTestSkill("Summon Elemental Relic")
	casting := NewModDB()
	casting.minionData = &gamedata.Minion{Name: "Elemental Relic"}
	setSpectreSource(mods, plain, casting, "Determination")
	if mods[0].Source != "Skill:Determination" {
		t.Errorf("a non-spectre minion's mod got source %q", mods[0].Source)
	}
}

// TestMinionDebuffCondition: a debuff of env.minion's own skills sets
// AffectedBy<debuff> on env.minion (CalcPerform.lua:3012-3014):
// NCgd_ieVLUw7's Dancing Dervish Maim, with Manifest Dancing Dervishes as
// the main skill.
func TestMinionDebuffCondition(t *testing.T) {
	c := newCalc(t)
	res, err := c.Calc(corpusBuild(t, "NCgd_ieVLUw7"))
	if err != nil {
		t.Fatal(err)
	}
	var affected, seen bool
	debugMinionSkill = func(ms *activeSkill) {
		seen = true
		affected = ms.mods.root().conditions["AffectedByMaim"]
	}
	defer func() { debugMinionSkill = nil }()
	for _, s := range res.Skills {
		if s.Name == "Manifest Dancing Dervishes" {
			if _, calcErr := c.Calc(withMainSkill(res.build, res.buildGroup(s.Group), s.GroupSkill)); calcErr != nil {
				t.Fatal(calcErr)
			}
			break
		}
	}
	if !seen || !affected {
		t.Errorf("minion offence ran %v, AffectedByMaim %v", seen, affected)
	}
}
