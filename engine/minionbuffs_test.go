package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/Mi7teR/exile-calc/gamedata"
	"github.com/Mi7teR/exile-calc/pobcode"
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
				func([]Mod, string) {}, map[string]bool{}, &Result{})
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

// TestMinionPartyExports: the Party tab exports of the minions' skills
// (buffExports, CalcPerform.lua:2923-2933, 3002) as PartyTab.lua
// exportBuffs writes them. a2nRZLW9ZVuu exporting: the spectre's Smite and
// its Wrath (moved to Wrath_Debuff by the Holy Relic's Wrath) carry
// setSpectreSource's names. The lines are PoB's; PoB orders a buff's mods
// by its stat processing, the engine by mod name, so they compare as sets.
func TestMinionPartyExports(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "a2nRZLW9ZVuu")
	if res, err := c.Calc(b); err != nil || res.PartyExport("Aura") != "" {
		t.Fatalf("exports without the Party tab's export: %v", err)
	}
	if b.Party == nil {
		b.Party = &pobcode.Party{}
	}
	b.Party.ExportBuffs = true
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	text := res.PartyExport("Aura")
	got := map[string][]string{}
	name := ""
	for l := range strings.SplitSeq(text, "\n") {
		switch {
		case name == "":
			name = l
		case l == "---":
			name = ""
		default:
			got[name] = append(got[name], l)
		}
	}
	want := map[string][]string{
		"Smite": {
			"100",
			"33|Spectre:Smite - Perfect Blasphemer|LightningMin|BASE|-|-|type=GlobalEffect/effectType=Aura,type=Condition/neg=true/var=AffectedByVaalSmite",
			"621|Spectre:Smite - Perfect Blasphemer|LightningMax|BASE|-|-|type=GlobalEffect/effectType=Aura,type=Condition/neg=true/var=AffectedByVaalSmite",
		},
		"Wrath": {"100",
			"26|Skill:Wrath|LightningMin|BASE|-|Attack|type=GlobalEffect/effectType=Aura",
			"415|Skill:Wrath|LightningMax|BASE|-|Attack|type=GlobalEffect/effectType=Aura",
			"23|Skill:Wrath|LightningDamage|MORE|Spell|-|type=GlobalEffect/effectType=Aura"},
		"Wrath_Debuff": {
			"100",
			"9|Spectre:Wrath - Perfect Spirit of Fortune|LightningMin|BASE|-|Attack|type=GlobalEffect/effectType=Aura",
			"126|Spectre:Wrath - Perfect Spirit of Fortune|LightningMax|BASE|-|Attack|type=GlobalEffect/effectType=Aura",
			"18|Spectre:Wrath - Perfect Spirit of Fortune|LightningDamage|MORE|Spell|-|type=GlobalEffect/effectType=Aura",
		},
	}
	for k, w := range want {
		g := got[k]
		if len(g) == 0 || g[0] != w[0] || !sameStrings(g[1:], w[1:]) {
			t.Errorf("%s: %q, want %q", k, g, w)
		}
	}
	// The Party tab of a party member reads it back.
	list := map[string]*partyBuffList{}
	parsePartyBuffs(list, text, "Aura", nil)
	if smite := list["Aura"].get("Smite"); smite == nil || smite.effect != 100 || len(smite.mods) != 2 {
		t.Errorf("parsed Smite %+v", smite)
	}
}

func sameStrings(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// TestPlayerAuraOthers: the rest of the player's Aura branch
// (CalcPerform.lua:2512-2618): the totem copy while the main skill is a
// totem, the debuff with AurasAffectEnemies, and the unscaled
// ExtraAuraDebuffEffect debuff of an aura without an AuraDebuff.
func TestPlayerAuraOthers(t *testing.T) {
	c := &Calculator{}
	db := NewModDB()
	buffMode(db, "")
	aura := triggerTestSkill("Determination")
	aura.mods.NewMod("AuraEffect", "INC", 50, "Gem")
	b := &skillBuff{typ: "Aura", name: "Determination", mods: []Mod{{Name: "Armour", Type: "MORE", Value: 20.0}}}
	aura.buffs = []*skillBuff{b}
	totem := triggerTestSkill("Shockwave Totem")
	totem.flags["totem"] = true
	db.Add(Mod{Name: "AurasAffectEnemies", Type: "FLAG", Value: true})
	db.Add(Mod{Name: "ExtraAuraDebuffEffect", Type: "LIST", Source: "Tree",
		Value: map[string]any{"mod": map[string]any{"name": "PhysicalDamageTaken", "type": "INC", "value": 5.0}}})
	buffs, debuffs := map[string][]Mod{}, map[string][]Mod{}
	add := func(l []Mod, name string) { buffs[name] = append(buffs[name], l...) }
	addDebuff := func(l []Mod, name string) { debuffs[name] = append(debuffs[name], l...) }
	c.playerAuraOthers(db, aura, totem, b, nil, add, addDebuff)
	if l := buffs["Totem Determination"]; len(l) != 1 || l[0].Name != "TotemArmour" || l[0].Value != 30.0 {
		t.Errorf("totem buff %+v", l)
	}
	if !totem.mods.conditions["AffectedByDetermination"] || !totem.mods.conditions["AffectedByAura"] {
		t.Errorf("totem conditions %v", totem.mods.conditions)
	}
	// The AurasAffectEnemies debuff (scaled 1.5), then the extra debuff mod
	// unscaled, as PoB merges it (CalcPerform.lua:2615).
	want := []Mod{
		{Name: "Armour", Type: "MORE", Value: 30.0},
		{Name: "PhysicalDamageTaken", Type: "INC", Value: 5.0, Source: "Tree"},
	}
	got := debuffs["Determination"]
	if len(got) != 2 || got[0].Value != want[0].Value || got[1].Name != want[1].Name || got[1].Value != want[1].Value {
		t.Errorf("debuffs %+v", got)
	}
}

// TestMinionSkillBuffOrder: env.minion's own debuff sets its AffectedBy
// condition where the minion skill loop reaches it (CalcPerform.lua
// 3012-3014), so an aura of that name listed after it no longer reaches
// env.minion (2908), and one listed before it still does.
func TestMinionSkillBuffOrder(t *testing.T) {
	c := &Calculator{}
	for _, debuffFirst := range []bool{true, false} {
		db, mdb := NewModDB(), NewModDB()
		buffMode(db, "")
		buffMode(mdb, "")
		summon := triggerTestSkill("Raise Spectre")
		ms := triggerTestSkill("Minion Wrath")
		ms.summon = summon
		ms.data.set("enable", true)
		aura := &skillBuff{typ: "Aura", name: "Wrath",
			mods: []Mod{{Name: "LightningMin", Type: "BASE", Value: 10.0}}}
		debuff := &skillBuff{typ: "AuraDebuff", name: "Wrath",
			mods: []Mod{{Name: "LightningResist", Type: "BASE", Value: -5.0}}}
		ms.buffs = []*skillBuff{aura, debuff}
		if debuffFirst {
			ms.buffs = []*skillBuff{debuff, aura}
		}
		out := map[string][]Mod{}
		c.minionSkillBuffsOf(db, mdb, summon, summon, []*activeSkill{ms}, 1, out, &Result{})
		if !mdb.conditions["AffectedByWrath"] {
			t.Errorf("debuff first %v: no AffectedByWrath", debuffFirst)
		}
		if _, got := out["Wrath"]; got == debuffFirst {
			t.Errorf("debuff first %v: aura on env.minion %v", debuffFirst, got)
		}
	}
}

// TestMinionPipelineOrder: env.minion's ModDB exists from the buff loop
// on, so its extra curses read it (CalcPerform.lua:3177-3178) and its
// second enemy modifier pass reaches the enemy before the player's
// calcs.defence (3528 before 3826).
func TestMinionPipelineOrder(t *testing.T) {
	c := newCalc(t)
	st := &calcState{b: corpusBuild(t, "a2nRZLW9ZVuu")}
	for _, step := range calcSteps {
		if step.name == "curses" {
			if st.minion == nil {
				t.Fatal("no env.minion after the buff loop")
			}
			mdb := st.minion.mdb
			// Tagged with a condition only env.minion's own conditions set.
			mdb.Add(Mod{Name: "ExtraCurse", Type: "LIST", Source: "Test",
				Value: map[string]any{"skillId": "Punishment", "level": 1.0},
				Tags:  []map[string]any{{"type": "Condition", "var": "UsedMinionSkillRecently"}}})
			mdb.Add(Mod{Name: "EnemyModifier", Type: "LIST", Source: "Test",
				Value: map[string]any{"mod": map[string]any{"name": "TestEnemyMarker", "type": "BASE", "value": 1.0}}})
		}
		if step.name == "defence" && st.db.enemy.Sum("BASE", "TestEnemyMarker") != 1 {
			t.Error("the minion's enemy modifiers are not on the enemy before the player's defence")
		}
		if err := step.run(c, st); err != nil {
			t.Fatal(err)
		}
		if step.name == "curses" &&
			!slices.ContainsFunc(st.curses, func(cu *enemyCurse) bool { return cu.name == "Punishment" && cu.minion }) {
			t.Error("the minion's ExtraCurse did not join minionCurses")
		}
	}
}
