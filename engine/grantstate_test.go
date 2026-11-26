package engine

import (
	"slices"
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// groupBySource is the 0-based index of b's active group with source src.
func groupBySource(t *testing.T, b *pobcode.Build, src string) int {
	t.Helper()
	i := slices.IndexFunc(b.ActiveSkills().Groups, func(g pobcode.SocketGroup) bool { return g.Source == src })
	if i < 0 {
		t.Fatalf("no group with source %q", src)
	}
	return i
}

// TestExplodeGroupGrant: CalcSetup.lua:1564-1605. The Explode group's
// gems are new instances with triggered = true; the group has noSupports
// only when the block creates it (Dd_SPzNY9-I5 saved none), not when the
// saved build has it (2HB8-0xwYEA6). The EnemyExplode skills see
// srcInstance.triggered (isTriggered, CalcTriggers.lua:42).
func TestExplodeGroupGrant(t *testing.T) {
	c := newCalc(t)
	for _, tc := range []struct {
		id         string
		noSupports bool
	}{{"Dd_SPzNY9-I5", true}, {"2HB8-0xwYEA6", false}} {
		res, err := c.Calc(corpusBuild(t, tc.id))
		if err != nil {
			t.Fatal(err)
		}
		i := groupBySource(t, res.build, "Explode")
		g := grantOf(res.grants, i)
		if g.noSupports != tc.noSupports {
			t.Errorf("%s: Explode group noSupports %v, want %v", tc.id, g.noSupports, tc.noSupports)
		}
		gems := res.build.ActiveSkills().Groups[i].Gems
		for gi := range gems {
			if src := g.gem(gi); !src.triggered || src.level != 1 {
				t.Errorf("%s: EnemyExplode gem %d: %+v, want triggered at level 1", tc.id, gi+1, src)
			}
		}
		n := 0
		for _, s := range res.DB.activeSkills {
			if s.group == &res.build.ActiveSkills().Groups[i] {
				n++
				if !s.active.srcTriggered || !isTriggered(s) {
					t.Errorf("%s: EnemyExplode skill not triggered", tc.id)
				}
			}
		}
		if n != len(gems) {
			t.Errorf("%s: %d EnemyExplode skills for %d gems", tc.id, n, len(gems))
		}
		// A calculation of the calculated build (the minion passes,
		// withMainSkill, and the Comparer) keeps the created group's
		// noSupports, as PoB's session group does; the saved build does not
		// hold it.
		again, err := c.Calc(withMainSkill(res.build, 1, 1))
		if err != nil {
			t.Fatal(err)
		}
		if got := grantOf(again.grants, groupBySource(t, again.build, "Explode")).noSupports; got != tc.noSupports {
			t.Errorf("%s: recalculated Explode group noSupports %v, want %v", tc.id, got, tc.noSupports)
		}
		code, err := res.build.Code()
		if err != nil {
			t.Fatal(err)
		}
		saved, err := pobcode.FromCode(code)
		if err != nil {
			t.Fatal(err)
		}
		if g := saved.ActiveSkills().Groups[i]; g.NoSupports {
			t.Errorf("%s: the saved build keeps NoSupports", tc.id)
		}
	}
}

// TestGrantedNoSupports: a group a noSupports grant takes gets no
// supports (CalcSetup.lua:1556, 1866-1867). MuBiMZ8WiDZn's ring grants
// Despair (curse on hit, noSupports); an Arcane Surge in the ring's slot
// supports it only when the grant leaves noSupports off.
func TestGrantedNoSupports(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "MuBiMZ8WiDZn")
	set := b.ActiveSkills()
	set.Groups = append(slices.Clone(set.Groups), pobcode.SocketGroup{Enabled: true, Slot: "Ring 2",
		Gems: []pobcode.Gem{{SkillID: "SupportArcaneSurge", Level: 20, Enabled: true}}})
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	ring := groupBySource(t, res.build, "Item:13:New Item, Amethyst Ring")
	if !grantOf(res.grants, ring).noSupports {
		t.Fatal("Despair's group has no noSupports")
	}
	supported := func(skills []*activeSkill) []string {
		var out []string
		for _, s := range skills {
			if s.group == &res.build.ActiveSkills().Groups[ring] {
				for _, e := range s.effects[1:] {
					out = append(out, e.id)
				}
			}
		}
		return out
	}
	if got := supported(res.DB.activeSkills); len(got) != 0 {
		t.Errorf("Despair with noSupports supported by %v", got)
	}
	grants := slices.Clone(res.grants)
	grants[ring].noSupports = false
	got := supported(c.setupSkills(res.DB, res.build, false, &Result{}, grants))
	if !slices.Contains(got, "SupportArcaneSurge") {
		t.Errorf("Despair without noSupports supported by %v, want Arcane Surge", got)
	}
}

// TestCurseApplicationSkill: CalcActiveSkill.lua:521-526. "Your spells
// are disabled" (DisableSkill for spells and ForceEnableCurseApplication)
// leaves an item's triggered curse with noSupports enabled; DisableSkill
// alone disables it.
func TestCurseApplicationSkill(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "MuBiMZ8WiDZn")
	b.MainSocketGroup = groupBySource(t, b, "Item:13:New Item, Amethyst Ring") + 1
	disable := testMod{m: Mod{Name: "DisableSkill", Type: "FLAG", Value: true,
		Tags: []map[string]any{{"type": "SkillType", "skillType": float64(stSpell)}}}}
	force := testMod{m: flagMod("ForceEnableCurseApplication")}
	for _, tc := range []struct {
		mods     []testMod
		disabled bool
	}{{nil, false}, {[]testMod{disable}, true}, {[]testMod{disable, force}, false}} {
		st := calcWithMods(t, c, b, true, tc.mods...)
		s := st.res.main
		if s == nil || s.active.ge.Name != "Despair" {
			t.Fatalf("main skill %v, want Despair", s)
		}
		if s.disabled != tc.disabled {
			t.Errorf("mods %d: Despair disabled %v, want %v", len(tc.mods), s.disabled, tc.disabled)
		}
	}
}

// TestTriggeredGrantCost: ProcessSocketGroup empties the level's cost of
// a triggered granted gem (SkillsTab.lua:1198-1202). KR_vSVUJvzBw's
// Lioneye's Paws trigger level 5 Rain of Arrows: ManaCost 0, as the
// oracle gives with that group as the main skill (8 before the port).
func TestTriggeredGrantCost(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "KR_vSVUJvzBw")
	b.MainSocketGroup = groupBySource(t, b, "Item:54:Lioneye's Paws, Bronzescale Boots") + 1
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if res.main.active.ge.Name != "Rain of Arrows" || res.Stats["ManaCost"] != 0 {
		t.Errorf("%s ManaCost %v, want Rain of Arrows at 0", res.main.active.ge.Name, res.Stats["ManaCost"])
	}
	// The emptied level is shared: a Rain of Arrows gem at level 5
	// elsewhere costs nothing either; another level keeps its cost.
	groups := []pobcode.SocketGroup{
		{Gems: []pobcode.Gem{{SkillID: "RainOfArrows", Level: 5}}},
		{Gems: []pobcode.Gem{{SkillID: "Despair", Level: 1}}},
	}
	grants := []groupGrant{{gems: []gemGrant{{triggered: true, level: 5}}}, {gems: []gemGrant{{level: 1}}}}
	wiped := wipedCosts(groups, grants)
	ge := c.data.Skills.Effects["RainOfArrows"]
	for _, x := range []struct {
		level int
		want  bool
	}{{5, true}, {6, false}} {
		e := &skillEffect{id: "RainOfArrows", ge: ge, level: x.level, wipedCosts: wiped}
		if got := e.levelCost() == nil; got != x.want {
			t.Errorf("level %d: cost emptied %v, want %v", x.level, got, x.want)
		}
	}
	if wiped[costKey{"Despair", 1}] {
		t.Error("an untriggered gem emptied its cost")
	}
}

// TestItemGrantTags: item.grantedSkills is baseList:List(nil, "ExtraSkill")
// and CanExplode baseModList:Flag(nil, ...) (Item.lua:2666-2679,
// CalcSetup.lua:851): tags are evaluated against the item's own list,
// which has no conditions, so a conditional grant does not count.
func TestItemGrantTags(t *testing.T) {
	c := newCalc(t)
	cond := []map[string]any{{"type": "Condition", "var": "Phasing"}}
	neg := []map[string]any{{"type": "Condition", "var": "Phasing", "neg": true}}
	grant := func(id string, tags []map[string]any) Mod {
		return Mod{Name: extraSkill, Type: "LIST", Value: map[string]any{"skillId": id, "level": 1.0,
			"noSupports": true, "triggered": true}, Tags: tags}
	}
	info := c.grantInfo("Item:1:Test", []Mod{
		grant("Despair", nil), grant("Vulnerability", cond), grant("Enfeeble", neg),
		{Name: "CanExplode", Type: "FLAG", Value: true, Tags: cond},
	})
	var ids []string
	for _, g := range info.grants {
		ids = append(ids, g.id)
		if !g.fromItem || !g.noSupports || !g.triggered || g.nameSpec == "" {
			t.Errorf("%s: %+v, want fromItem, noSupports, triggered and a name", g.id, g)
		}
	}
	if !slices.Equal(ids, []string{"Despair", "Enfeeble"}) {
		t.Errorf("grants %v, want [Despair Enfeeble]", ids)
	}
	if info.canExplode {
		t.Error("a conditional CanExplode counts")
	}
	if !c.grantInfo("Item:1:Test", []Mod{{Name: "CanExplode", Type: "FLAG", Value: true}}).canExplode {
		t.Error("CanExplode does not count")
	}
	// The parser's grants keep their fields.
	r := c.parser.Parse("Curse Enemies with Despair on Hit")
	var mods []Mod
	for _, m := range r.Mods {
		mods = append(mods, fromParsed(m, "Item:2:Ring"))
	}
	if g := c.grantInfo("Item:2:Ring", mods).grants; len(g) != 1 || !g[0].triggered || !g[0].noSupports {
		t.Errorf("Curse Enemies with Despair on Hit: %+v", g)
	}
}

// TestItemMultiplier: the influence requirement reads
// env.itemModDB:GetMultiplier (CalcSetup.lua:1500-1506, ModStore.lua:
// 301-303): the item count plus the items' Multiplier BASE mods, or their
// OVERRIDE; other sources are not in itemModDB.
func TestItemMultiplier(t *testing.T) {
	db := NewModDB()
	db.multipliers["ShaperItem"] = 2
	db.NewMod("Multiplier:ShaperItem", "BASE", 1, "Item:3:Ring")
	db.NewMod("Multiplier:ShaperItem", "BASE", 5, "Tree:100")
	if got := itemMultiplier(db, "ShaperItem"); got != 3 {
		t.Errorf("ShaperItem %v, want 3", got)
	}
	db.NewMod("Multiplier:ShaperItem", "OVERRIDE", 6, "Item:4:Amulet")
	if got := itemMultiplier(db, "ShaperItem"); got != 6 {
		t.Errorf("ShaperItem with an override %v, want 6", got)
	}
	g := grantedSkill{id: "Despair", reqMult: "ElderItem", reqCount: 2}
	keep := slices.DeleteFunc([]grantedSkill{g}, func(g grantedSkill) bool {
		return g.reqMult != "" && itemMultiplier(db, g.reqMult) < g.reqCount
	})
	if len(keep) != 0 {
		t.Error("a grant needing 2 Elder items holds with none")
	}
}

// TestLuaTableHoles: tags and lists with holes as PoB's ModParser leaves
// them. "Non-Instant Warcries ..." has skillTypeList { 64, nil, 37 }
// (SkillType.Totem does not exist); EvalMod walks it with pairs
// (ModStore.lua:866), so both Instant and Triggered skills are excluded.
// Sacrificial Zeal's overwhelm has its tag at mod[2] behind a nil, which
// EvalMod's ipairs (ModStore.lua:368) never reaches: it always applies.
func TestLuaTableHoles(t *testing.T) {
	c := newCalc(t)
	parse := func(line string) Mod {
		r := c.parser.Parse(line)
		if !r.Parsed() || len(r.Mods) != 1 {
			t.Fatalf("%q: %+v", line, r)
		}
		return fromParsed(r.Mods[0], "Test")
	}
	db := NewModDB()
	db.Add(parse("Non-Instant Warcries you use yourself have no Cooldown"))
	for _, tc := range []struct {
		types map[int]bool
		want  bool
	}{{map[int]bool{1: true}, true}, {map[int]bool{64: true}, false}, {map[int]bool{37: true}, false}} {
		cfg := &Cfg{KeywordFlags: 0x4, SkillTypes: tc.types}
		if _, got := db.OverrideCfg(cfg, "CooldownRecovery"); got != tc.want {
			t.Errorf("skill types %v: override %v, want %v", tc.types, got, tc.want)
		}
	}
	zeal := parse("Hits Overwhelm 15% of Physical Damage Reduction while you have Sacrificial Zeal")
	if len(zeal.Tags) != 0 {
		t.Errorf("Sacrificial Zeal overwhelm tags %v, want none", zeal.Tags)
	}
}
