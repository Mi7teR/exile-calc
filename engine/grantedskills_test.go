package engine

import (
	"slices"
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// CalcSetup.lua:1523-1564: a skill granted by an allocated node gets a
// socket group appended to the active skill set when the build has none;
// a saved group with the same source, skill and level is kept.
func TestTreeGrantedSkillGroups(t *testing.T) {
	db := NewModDB()
	db.Add(Mod{Name: "ExtraSkill", Type: "LIST", Source: "Tree:200",
		Value: map[string]any{"skillId": "SummonSpectralTiger", "level": 20.0}})
	db.Add(Mod{Name: "ExtraSkill", Type: "LIST", Source: "Item:3:Ring",
		Value: map[string]any{"skillId": "Other", "level": 1.0}})
	saved := pobcode.SocketGroup{Enabled: true, Slot: "Helmet", Gems: []pobcode.Gem{{SkillID: "Arc", Level: 20}}}
	b := &pobcode.Build{ActiveSkillSetID: 1, MainSocketGroup: 1,
		SkillSets: []pobcode.SkillSet{{ID: 1, Groups: []pobcode.SocketGroup{saved}}}}

	grants := treeGrantedSkills(db)
	if len(grants) != 1 {
		t.Fatalf("tree grants %+v, want only the node's", grants)
	}
	got := applyGrantedSkills(b, grants, nil)
	groups := got.ActiveSkills().Groups
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want the saved one and the tree group", len(groups))
	}
	g := groups[1]
	if g.Source != "Tree:200" || g.Slot != "" || !g.Enabled || len(g.Gems) != 1 {
		t.Fatalf("tree group %+v", g)
	}
	gm := g.Gems[0]
	if gm.SkillID != "SummonSpectralTiger" || gm.Level != 20 || gm.Quality != 0 || !gm.Enabled || !gm.EnableGlobal1 {
		t.Errorf("tree gem %+v", gm)
	}
	if len(b.ActiveSkills().Groups) != 1 {
		t.Error("the input build was changed")
	}
	if again := applyGrantedSkills(got, grants, nil); again != got {
		t.Error("a build with the group gets another one")
	}

	// A saved group at the validated level matches (getNormalizedSkillLevel)
	// and keeps the validated level (ProcessSocketGroup).
	skills := map[string]*gamedata.Skill{"S": {Levels: map[int]*gamedata.SkillLevel{1: {}, 2: {}}}}
	tree7 := []grantedSkill{{source: "Tree:7", id: "S", level: 25}}
	b.SkillSets[0].Groups = append(
		b.SkillSets[0].Groups,
		pobcode.SocketGroup{
			Enabled: true,
			Source:  "Tree:7",
			Gems:    []pobcode.Gem{{SkillID: "S", Level: 2, EnableGlobal1: true}},
		},
	)
	if kept := applyGrantedSkills(b, tree7, skills); kept != b {
		t.Errorf("saved group at the validated level not matched: %+v", kept.ActiveSkills().Groups)
	}
	// Another level makes a new group, as in PoB, and the stale one goes.
	b.SkillSets[0].Groups[1].Gems[0].Level = 1
	more := applyGrantedSkills(b, tree7, skills).ActiveSkills().Groups
	if len(more) != 2 || more[1].Source != "Tree:7" || more[1].Gems[0].Level != 2 {
		t.Errorf("groups %+v, want the saved one and a new Tree:7 group at level 2", more)
	}
}

// CalcSetup.lua:1543-1561 updates a matched group: its first gem stays
// (quality, count), loses gemId, takes the granted level and
// enableGlobal1, and the other gems go; the group keeps enabled and label.
func TestGrantedGroupUpdate(t *testing.T) {
	g := pobcode.SocketGroup{Enabled: false, Label: "x", Source: "Item:1:Ring", Slot: "Ring 1", Gems: []pobcode.Gem{
		{SkillID: "S", GemID: "G", Level: 1, Quality: 7, Count: 2},
		{SkillID: "Support", Level: 20, Enabled: true},
	}}
	b := &pobcode.Build{ActiveSkillSetID: 1, MainSocketGroup: 1,
		SkillSets: []pobcode.SkillSet{{ID: 1, Groups: []pobcode.SocketGroup{g}}}}
	got := applyGrantedSkills(b, []grantedSkill{{source: "Item:1:Ring", slot: "Ring 1", id: "S", level: 1}}, nil)
	groups := got.ActiveSkills().Groups
	if len(groups) != 1 {
		t.Fatalf("groups %+v", groups)
	}
	want := []pobcode.Gem{{SkillID: "S", Level: 1, Quality: 7, Count: 2, EnableGlobal1: true}}
	if groups[0].Label != "x" || groups[0].Enabled || !slices.Equal(groups[0].Gems, want) {
		t.Errorf("group %+v, want gems %+v", groups[0], want)
	}
	if len(b.ActiveSkills().Groups[0].Gems) != 2 {
		t.Error("the input build was changed")
	}
}

// staleBuild is three groups: a stale granted group with source, Arc and
// Fireball; the main socket group is 3.
func staleBuild(source, slot string) *pobcode.Build {
	return &pobcode.Build{ActiveSkillSetID: 1, MainSocketGroup: 3,
		SkillSets: []pobcode.SkillSet{{ID: 1, Groups: []pobcode.SocketGroup{
			{Enabled: true, Source: source, Slot: slot, Gems: []pobcode.Gem{{SkillID: "Gone", Level: 1}}},
			{Enabled: true, Gems: []pobcode.Gem{{SkillID: "Arc", Level: 20}}},
			{Enabled: true, Gems: []pobcode.Gem{{SkillID: "Fireball", Level: 20}}},
		}}}}
}

// CalcSetup.lua:1607-1619 removes a granted group nothing grants, tree or
// item, and leaves build.mainSocketGroup alone (1652-1653 only clamp it):
// the saved index 3 now points past the list (setupSkills clamps it to 2).
func TestStaleGrantedGroupRemoved(t *testing.T) {
	for _, tc := range []struct{ source, slot string }{{"Tree:1", ""}, {"Item:9:Ring", "Ring 1"}} {
		b := staleBuild(tc.source, tc.slot)
		got := applyGrantedSkills(b, nil, nil)
		groups := got.ActiveSkills().Groups
		if len(groups) != 2 || groups[0].Gems[0].SkillID != "Arc" || groups[1].Gems[0].SkillID != "Fireball" {
			t.Fatalf("%s: groups %+v, want Arc and Fireball", tc.source, groups)
		}
		if got.MainSocketGroup != 3 {
			t.Errorf("%s: MainSocketGroup %d, want 3 (PoB does not adjust it)", tc.source, got.MainSocketGroup)
		}
		if len(b.ActiveSkills().Groups) != 3 {
			t.Errorf("%s: the input build was changed", tc.source)
		}
	}
	// An Explode group stays (its block is not ported).
	b := staleBuild("Explode", "")
	if got := applyGrantedSkills(b, nil, nil); got != b {
		t.Error("an Explode group was removed")
	}
}

// withStaleGroup returns a copy of b with a stale tree group (no node
// grants it) first in the active skill set. MainSocketGroup is not
// changed: once PoB removes that group, it names the same group again.
func withStaleGroup(b *pobcode.Build) *pobcode.Build {
	cp := *b
	cp.SkillSets = slices.Clone(b.SkillSets)
	set := cp.ActiveSkills()
	set.Groups = append([]pobcode.SocketGroup{{Enabled: true, Source: "Tree:1",
		Gems: []pobcode.Gem{{SkillID: "SummonSpectralTiger", Level: 20, Enabled: true}}}}, set.Groups...)
	return &cp
}

// A stale group before the main group: Calc removes it, so the skills and
// Result.Build are the original build's, and FullDPS and CalcMinions,
// which calculate each skill on Result.Build, give the same skills.
func TestStaleGroupBeforeMain(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus builds")
	}
	c := newCalc(t)
	minions, full := 0, 0
	for _, id := range []string{"3uy_tEm_vqpB", "5g9VlzQd4BVS"} {
		b := corpusBuild(t, id)
		stale := withStaleGroup(b)
		r1, err := c.Calc(b)
		if err != nil {
			t.Fatal(err)
		}
		r2, err := c.Calc(stale)
		if err != nil {
			t.Fatal(err)
		}
		if len(r2.Build().ActiveSkills().Groups) != len(r1.Build().ActiveSkills().Groups) {
			t.Fatalf("%s: stale group not removed", id)
		}
		if !slices.EqualFunc(r1.Skills, r2.Skills, func(a, b SkillInfo) bool {
			return a.Name == b.Name && a.Group == b.Group && a.GroupSkill == b.GroupSkill
		}) || r1.Stats["TotalDPS"] != r2.Stats["TotalDPS"] {
			t.Errorf("%s: skills or DPS differ with a stale group", id)
		}
		f1, err := c.FullDPS(b)
		if err != nil {
			t.Fatal(err)
		}
		f2, err := c.FullDPS(stale)
		if err != nil {
			t.Fatal(err)
		}
		if f1.FullDPS != f2.FullDPS || !slices.Equal(f1.Skills, f2.Skills) {
			t.Errorf("%s: FullDPS %v %+v, with a stale group %v %+v", id, f1.FullDPS, f1.Skills, f2.FullDPS, f2.Skills)
		}
		m1, err := c.CalcMinions(b)
		if err != nil {
			t.Fatal(err)
		}
		m2, err := c.CalcMinions(stale)
		if err != nil {
			t.Fatal(err)
		}
		if len(m1) != len(m2) {
			t.Fatalf("%s: %d minions, with a stale group %d", id, len(m1), len(m2))
		}
		minions, full = minions+len(m1), full+len(f1.Skills)
		for i := range m1 {
			if m1[i].Skill != m2[i].Skill || m1[i].Stats["TotalDPS"] != m2[i].Stats["TotalDPS"] {
				t.Errorf("%s: minion %d %s %v, with a stale group %s %v", id, i,
					m1[i].Skill, m1[i].Stats["TotalDPS"], m2[i].Skill, m2[i].Stats["TotalDPS"])
			}
		}
	}
	if minions == 0 || full == 0 {
		t.Fatalf("%d minions, %d Full DPS skills: the builds test nothing", minions, full)
	}
}

// SkillInfo.Group indexes Result.Build's groups, the space PoB reads
// mainSocketGroup in: setting the input build's MainSocketGroup to it
// makes that skill the main skill, also when a group before it is removed.
func TestSkillGroupSelectsMainOnInputBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus builds")
	}
	c := newCalc(t)
	b := withStaleGroup(corpusBuild(t, "3uy_tEm_vqpB"))
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	tried := 0
	for _, s := range res.Skills {
		if s.Disabled || s.GroupSkill != 1 {
			continue
		}
		cp := *b
		cp.MainSocketGroup = s.Group
		r, calcErr := c.Calc(&cp)
		if calcErr != nil {
			t.Fatal(calcErr)
		}
		if r.main == nil || r.main.active.ge.Name != s.Name {
			t.Errorf("MainSocketGroup %d: main skill %v, want %s", s.Group, r.main, s.Name)
		}
		tried++
	}
	if tried < 2 {
		t.Fatalf("only %d skills tried", tried)
	}
}

// slotGroup is the index of the granted group of skill in slot, or -1.
func slotGroup(b *pobcode.Build, slot, skill string) int {
	return slices.IndexFunc(b.ActiveSkills().Groups, func(g pobcode.SocketGroup) bool {
		return g.Source != "" && g.Slot == slot && len(g.Gems) > 0 && g.Gems[0].SkillID == skill
	})
}

// withoutGroup returns a copy of b without the active skill set's group i.
func withoutGroup(b *pobcode.Build, i int) *pobcode.Build {
	cp := *b
	cp.SkillSets = slices.Clone(b.SkillSets)
	set := cp.ActiveSkills()
	set.Groups = slices.Delete(slices.Clone(set.Groups), i, i+1)
	return &cp
}

// CalcSetup.lua:811-849 and 1536-1541: a skill granted by an item, in the
// active gear or the inactive weapon set, gets a group with the item's
// modSource and slot, appended when the build has none.
func TestItemGrantedSkillGroup(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus builds")
	}
	c := newCalc(t)
	for _, tc := range []struct{ id, slot, skill, source string }{
		{"cl7sZKsLjQUB", "Gloves", "AssassinsMark", "Item:26:Oskarm, Nubuck Gloves"},
		{"J_Uzb2TmaVdw", "Weapon 2 Swap", "VoidShot", "Item:7:Voidfletcher, Ornate Quiver"},
	} {
		b := corpusBuild(t, tc.id)
		i := slotGroup(b, tc.slot, tc.skill)
		if i < 0 {
			t.Fatalf("%s: no saved %s group", tc.id, tc.skill)
		}
		saved := b.ActiveSkills().Groups[i]
		res, err := c.Calc(withoutGroup(b, i))
		if err != nil {
			t.Fatal(err)
		}
		groups := res.Build().ActiveSkills().Groups
		if len(groups) != len(b.ActiveSkills().Groups) {
			t.Fatalf("%s: %d groups, want %d", tc.id, len(groups), len(b.ActiveSkills().Groups))
		}
		g := groups[len(groups)-1]
		if g.Source != tc.source || g.Slot != tc.slot || !g.Enabled || len(g.Gems) != 1 {
			t.Fatalf("%s: new group %+v", tc.id, g)
		}
		gm := g.Gems[0]
		if gm.SkillID != tc.skill || gm.Level != saved.Gems[0].Level || !gm.Enabled || !gm.EnableGlobal1 ||
			gm.Quality != 0 || gm.NameSpec == "" {
			t.Errorf("%s: new gem %+v, saved %+v", tc.id, gm, saved.Gems[0])
		}
		// The saved build has its group: no group is added or removed.
		r0, err := c.Calc(b)
		if err != nil {
			t.Fatal(err)
		}
		if len(r0.Build().ActiveSkills().Groups) != len(b.ActiveSkills().Groups) {
			t.Errorf("%s: saved build's groups changed", tc.id)
		}
	}
}

// getMiscCalculator's override pass (CALCULATOR mode) skips the granted
// skill block: a removed item's granted group stays, as in PoB's item
// comparison; a MAIN Calc removes it.
func TestCalculatorModeKeepsGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus builds")
	}
	c := newCalc(t)
	b := corpusBuild(t, "cl7sZKsLjQUB") // Oskarm's Assassin's Mark in Gloves
	nb := editSlots(b, map[string]string{"Gloves": ""})
	has := func(r *Result) bool {
		return slices.ContainsFunc(r.Skills, func(s SkillInfo) bool { return s.Name == "Assassin's Mark" })
	}
	main, err := c.Calc(nb)
	if err != nil {
		t.Fatal(err)
	}
	calc, err := c.calc(nb, true)
	if err != nil {
		t.Fatal(err)
	}
	if has(main) || !has(calc) {
		t.Errorf("Assassin's Mark in MAIN %v, in CALCULATOR %v; want false, true", has(main), has(calc))
	}
}
