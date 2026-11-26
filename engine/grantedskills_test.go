package engine

import (
	"math"
	"os"
	"slices"
	"strings"
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
	got := applyGranted(b, grants, nil, nil)
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
	if again := applyGranted(got, grants, nil, nil); again != got {
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
	if kept := applyGranted(b, tree7, nil, skills); kept != b {
		t.Errorf("saved group at the validated level not matched: %+v", kept.ActiveSkills().Groups)
	}
	// Another level makes a new group, as in PoB, and the stale one goes.
	b.SkillSets[0].Groups[1].Gems[0].Level = 1
	more := applyGranted(b, tree7, nil, skills).ActiveSkills().Groups
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
	got := applyGranted(b, []grantedSkill{{source: "Item:1:Ring", slot: "Ring 1", id: "S", level: 1}}, nil, nil)
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

// CalcSetup.lua:1607-1619 removes a granted group nothing grants (tree,
// item, or Explode without explode sources). MainSocketGroup moves with
// its group, the game's choice (PoB keeps the index).
func TestStaleGrantedGroupRemoved(t *testing.T) {
	for _, tc := range []struct{ source, slot string }{{"Tree:1", ""}, {"Item:9:Ring", "Ring 1"}, {"Explode", ""}} {
		b := staleBuild(tc.source, tc.slot)
		b.MainSocketGroup = 2 // Arc
		got := applyGranted(b, nil, nil, nil)
		groups := got.ActiveSkills().Groups
		if len(groups) != 2 || groups[0].Gems[0].SkillID != "Arc" || groups[1].Gems[0].SkillID != "Fireball" {
			t.Fatalf("%s: groups %+v, want Arc and Fireball", tc.source, groups)
		}
		if got.MainSocketGroup != 1 {
			t.Errorf("%s: MainSocketGroup %d, want 1 (Arc's group)", tc.source, got.MainSocketGroup)
		}
		if len(b.ActiveSkills().Groups) != 3 || b.MainSocketGroup != 2 {
			t.Errorf("%s: the input build was changed", tc.source)
		}
	}
	// The main group itself removed: the index is kept, as in PoB, and
	// clamped later (setupSkills).
	b := staleBuild("Tree:1", "")
	b.MainSocketGroup = 1
	if got := applyGranted(b, nil, nil, nil); got.MainSocketGroup != 1 || len(got.ActiveSkills().Groups) != 2 {
		t.Errorf(
			"removed main group: MainSocketGroup %d, %d groups",
			got.MainSocketGroup,
			len(got.ActiveSkills().Groups),
		)
	}
	// A stale group after the main group: the index stays.
	b = staleBuild("Tree:1", "")
	g := b.SkillSets[0].Groups
	b.SkillSets[0].Groups = []pobcode.SocketGroup{g[1], g[2], g[0]}
	b.MainSocketGroup = 2
	if got := applyGranted(b, nil, nil, nil); got.MainSocketGroup != 2 {
		t.Errorf("stale group after main: MainSocketGroup %d, want 2", got.MainSocketGroup)
	}
}

// CalcSetup.lua:1564-1605: with explode sources, the first Explode group
// (or a new one, appended) holds one new EnemyExplode gem per source; a
// second Explode group is removed, and so is the group with no sources.
func TestExplodeGroup(t *testing.T) {
	newGem := pobcode.Gem{SkillID: "EnemyExplode", Level: 1, Enabled: true}
	src := []string{"Tree:5", "Item:3:Ring"}
	b := staleBuild("Tree:1", "")
	b.SkillSets[0].Groups = b.SkillSets[0].Groups[1:] // Arc, Fireball
	got := applyGranted(b, nil, src, nil)
	groups := got.ActiveSkills().Groups
	if len(groups) != 3 {
		t.Fatalf("groups %+v, want Arc, Fireball and Explode", groups)
	}
	g := groups[2]
	if g.Source != "Explode" || g.Label != "On Kill Monster Explosion" || !g.Enabled ||
		!slices.Equal(g.Gems, []pobcode.Gem{newGem, newGem}) {
		t.Errorf("explode group %+v", g)
	}
	if again := applyGranted(got, nil, src, nil); again != got {
		t.Error("an Explode group with its gems changed again")
	}

	// Saved groups: the first one's gems are rebuilt (a disabled gem, a
	// third gem), the second one goes.
	saved := pobcode.SocketGroup{Enabled: false, Label: "mine", Source: "Explode", Gems: []pobcode.Gem{
		{SkillID: "EnemyExplode", Level: 1}, newGem, newGem,
	}}
	arc := b.SkillSets[0].Groups[0]
	b.SkillSets[0].Groups = []pobcode.SocketGroup{saved, saved, arc}
	groups = applyGranted(b, nil, []string{"Tree:5"}, nil).ActiveSkills().Groups
	if len(groups) != 2 || groups[0].Label != "mine" || groups[0].Enabled ||
		!slices.Equal(groups[0].Gems, []pobcode.Gem{newGem}) || groups[1].Gems[0].SkillID != "Arc" {
		t.Errorf("groups %+v, want the first Explode group with one gem, then Arc", groups)
	}
	if none := applyGranted(b, nil, nil, nil).ActiveSkills().Groups; len(none) != 1 {
		t.Errorf("groups %+v, want only Arc without explode sources", none)
	}
}

// The Explode group of a corpus build holds one gem per explode source,
// and each gem reads its own source.
func TestExplodeGroupCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus builds")
	}
	c := newCalc(t)
	res, err := c.Calc(corpusBuild(t, "AtHqFK3vAk5P")) // a saved Explode group of three gems
	if err != nil {
		t.Fatal(err)
	}
	srcs := res.DB.explodeSources
	if len(srcs) < 2 {
		t.Fatalf("explode sources %v", srcs)
	}
	groups := res.Build().ActiveSkills().Groups
	i := slices.IndexFunc(groups, func(g pobcode.SocketGroup) bool { return g.Source == "Explode" })
	if i < 0 || len(groups[i].Gems) != len(srcs) {
		t.Fatalf("explode group %d, sources %v", i, srcs)
	}
	var got []string
	for _, s := range res.DB.activeSkills {
		if s.active.id == "EnemyExplode" {
			got = append(got, explodeSource(s))
		}
	}
	if !slices.Equal(got, srcs) {
		t.Errorf("gem sources %v, want %v", got, srcs)
	}
}

// withStaleGroup returns a copy of b with a stale tree group (no node
// grants it) first in the active skill set; MainSocketGroup still names
// the same group.
func withStaleGroup(b *pobcode.Build) *pobcode.Build {
	cp := *b
	cp.SkillSets = slices.Clone(b.SkillSets)
	set := cp.ActiveSkills()
	set.Groups = append([]pobcode.SocketGroup{{Enabled: true, Source: "Tree:1",
		Gems: []pobcode.Gem{{SkillID: "SummonSpectralTiger", Level: 20, Enabled: true}}}}, set.Groups...)
	cp.MainSocketGroup = min(max(len(set.Groups)-1, 1), max(b.MainSocketGroup, 1)) + 1
	return &cp
}

// A stale group before the main group: Calc removes it and the main skill
// stays the one chosen (the game's choice; PoB's moves to the next group),
// so the skills and Result.Build are the original build's, and FullDPS
// and CalcMinions, which calculate each skill on Result.Build, give the
// same skills.
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
		if r1.main == nil || r2.main == nil || r1.main.active.ge.Name != r2.main.active.ge.Name ||
			r2.Build().MainSocketGroup != r1.Build().MainSocketGroup {
			t.Errorf("%s: main skill %v, with a stale group %v", id, r1.main, r2.main)
		}
		if !slices.EqualFunc(r1.Skills, r2.Skills, func(a, b SkillInfo) bool {
			return a.Name == b.Name && a.Group+1 == b.Group && a.GroupSkill == b.GroupSkill
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
		if f1.FullDPS != f2.FullDPS || !slices.EqualFunc(f1.Skills, f2.Skills, func(a, b SkillDPS) bool {
			if a.Group != 0 {
				a.Group++ // the stale group is the input's group 1
			}
			return a == b
		}) {
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

// Result.WithMainSkill takes SkillInfo.Group (the input build's index)
// and makes that skill the main skill on Result.Build, also when a group
// before it was removed.
func TestResultWithMainSkill(t *testing.T) {
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
		if s.Disabled {
			continue
		}
		r, calcErr := c.Calc(res.WithMainSkill(s.Group, s.GroupSkill))
		if calcErr != nil {
			t.Fatal(calcErr)
		}
		if r.main == nil || r.main.active.ge.Name != s.Name {
			t.Errorf("group %d skill %d: main skill %v, want %s", s.Group, s.GroupSkill, r.main, s.Name)
		}
		tried++
	}
	if tried < 2 {
		t.Fatalf("only %d skills tried", tried)
	}
	// 0, the removed stale group 1 and a group past the input's.
	for _, g := range []int{0, 1, len(b.ActiveSkills().Groups) + 1} {
		if res.WithMainSkill(g, 1) != nil {
			t.Errorf("WithMainSkill(%d): want nil", g)
		}
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

// Comparer.Item runs the granted skill block, as the game counts skills
// granted by items: the compared item's skill gets its group and the
// replaced item's group goes (PoB's CALCULATOR-mode tooltip does neither).
func TestCompareItemGrantedSkill(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus builds")
	}
	c := newCalc(t)
	b := corpusBuild(t, "cl7sZKsLjQUB") // Oskarm's Assassin's Mark in Gloves
	oskarm := slotRaw(t, b, "Gloves")
	var lines []string
	for l := range strings.SplitSeq(oskarm, "\n") {
		if !strings.Contains(l, "Assassin's Mark") {
			lines = append(lines, l)
		}
	}
	plain := strings.Join(lines, "\n")
	if plain == oskarm {
		t.Fatal("no Assassin's Mark line")
	}
	has := func(r *Result) bool {
		return slices.ContainsFunc(r.Skills, func(s SkillInfo) bool { return s.Name == "Assassin's Mark" })
	}
	// Replacing Oskarm: Item's build loses the group.
	cp := newComparer(t, c, b)
	without, _, err := cp.withItem("Gloves", plain, "Gloves")
	if err != nil {
		t.Fatal(err)
	}
	if r, calcErr := c.Calc(without); calcErr != nil || has(r) {
		t.Errorf("replacing Oskarm keeps Assassin's Mark (%v)", calcErr)
	}
	removed, err := cp.Item("Gloves", plain)
	if err != nil {
		t.Fatal(err)
	}
	// Comparing Oskarm on the build without it: the group is added, and
	// the change is the opposite of removing it.
	cp = newComparer(t, c, without)
	with, _, err := cp.withItem("Gloves", oskarm, "Gloves")
	if err != nil {
		t.Fatal(err)
	}
	if r, calcErr := c.Calc(with); calcErr != nil || !has(r) {
		t.Errorf("comparing Oskarm adds no Assassin's Mark (%v)", calcErr)
	}
	added, err := cp.Item("Gloves", oskarm)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Delta) == 0 || len(added.Delta) != len(removed.Delta) {
		t.Fatalf("removing changes %d stats, adding %d", len(removed.Delta), len(added.Delta))
	}
	for k, d := range removed.Delta {
		if e := added.Delta[k]; math.Abs(e+d) > 1e-9*math.Max(1, math.Abs(d)) {
			t.Errorf("%s: removing %v, adding %v", k, d, e)
		}
	}
}

// The own item compared (Maw of Mischief grants the main skill, Death
// Wish): the main skill and the Full DPS skills stay.
func TestCompareOwnItemKeepsMain(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus builds")
	}
	c := newCalc(t)
	for _, tc := range []struct{ id, slot string }{{"Vq9VEs5haqZ-", "Helmet"}, {"cl7sZKsLjQUB", "Gloves"}} {
		b := corpusBuild(t, tc.id)
		r0, err := c.Calc(b)
		if err != nil {
			t.Fatal(err)
		}
		f0, err := c.FullDPS(b)
		if err != nil {
			t.Fatal(err)
		}
		nb, _, err := newComparer(t, c, b).compareBuild(tc.slot, slotRaw(t, b, tc.slot), "")
		if err != nil {
			t.Fatal(err)
		}
		r, err := c.calc(nb, calcOpts{compared: nb.Items[len(nb.Items)-1].ID})
		if err != nil {
			t.Fatal(err)
		}
		if r0.main == nil || r.main == nil || r.main.active.ge.Name != r0.main.active.ge.Name {
			t.Errorf("%s: main skill %v, compared %v", tc.id, r0.main, r.main)
		}
		f, err := c.fullDPSFrom(r)
		if err != nil {
			t.Fatal(err)
		}
		if f.FullDPS != f0.FullDPS || !slices.EqualFunc(f.Skills, f0.Skills, func(a, b SkillDPS) bool {
			return a.Name == b.Name && a.DPS == b.DPS && a.Count == b.Count
		}) {
			t.Errorf("%s: Full DPS %v %+v, compared %v %+v", tc.id, f0.FullDPS, f0.Skills, f.FullDPS, f.Skills)
		}
	}
}

// applyGranted is applyGrantedSkills without the group mapping.
func applyGranted(
	b *pobcode.Build,
	grants []grantedSkill,
	explode []string,
	skills map[string]*gamedata.Skill,
) *pobcode.Build {
	got, _ := applyGrantedSkills(b, grants, explode, skills)
	return got
}

// A MainSocketGroup past the input groups names an appended group (len+k)
// and moves with it; the mapping gives every group its input index.
func TestMainSocketGroupAppended(t *testing.T) {
	b := staleBuild("Tree:1", "") // stale, Arc, Fireball
	b.MainSocketGroup = 4         // the first appended group
	got, caller := applyGrantedSkills(b, []grantedSkill{{source: "Tree:2", id: "Frostbolt", level: 20}}, nil, nil)
	groups := got.ActiveSkills().Groups
	if len(groups) != 3 || groups[2].Gems[0].SkillID != "Frostbolt" || got.MainSocketGroup != 3 {
		t.Fatalf("groups %+v, MainSocketGroup %d; want Arc, Fireball, Frostbolt and 3", groups, got.MainSocketGroup)
	}
	if !slices.Equal(caller, []int{2, 3, 4}) {
		t.Errorf("caller groups %v, want [2 3 4]", caller)
	}
	b.MainSocketGroup = 9 // no such group: kept, clamped later as in PoB
	if kept := applyGranted(b, nil, nil, nil); kept.MainSocketGroup != 9 {
		t.Errorf("MainSocketGroup %d, want 9", kept.MainSocketGroup)
	}
}

// SkillInfo.Group is the input build's index: with groups [stale, X1, X2],
// setting MainSocketGroup to X2's Group makes X2 the main skill, and
// FullDPS selected that way gives the same Full DPS.
func TestCallerSpaceGroups(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus builds")
	}
	c := newCalc(t)
	orig := corpusBuild(t, "3uy_tEm_vqpB")
	b := withStaleGroup(orig)
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	r0, err := c.Calc(orig)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range res.Skills {
		if s.Group != r0.Skills[i].Group+1 {
			t.Fatalf("skill %s: Group %d, want %d (the stale group is 1)", s.Name, s.Group, r0.Skills[i].Group+1)
		}
	}
	f0, err := c.FullDPS(b)
	if err != nil {
		t.Fatal(err)
	}
	tried := 0
	for _, s := range res.Skills {
		if s.Disabled || s.GroupSkill != 1 || s.Group == b.MainSocketGroup {
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
		f, calcErr := c.FullDPS(&cp)
		if calcErr != nil {
			t.Fatal(calcErr)
		}
		if f.FullDPS != f0.FullDPS || !slices.Equal(f.Skills, f0.Skills) {
			t.Errorf(
				"MainSocketGroup %d: FullDPS %v %+v, want %v %+v",
				s.Group,
				f.FullDPS,
				f.Skills,
				f0.FullDPS,
				f0.Skills,
			)
		}
		tried++
	}
	if tried < 2 {
		t.Fatalf("only %d skills tried", tried)
	}
}

// Comparing a build's own item, granting the main or a Full DPS skill, in
// its own slot changes nothing: its granted groups carry over.
func TestCompareOwnGrantingItem(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus builds")
	}
	c := newCalc(t)
	ents, err := os.ReadDir("../testdata/golden/builds")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ents {
		id, ok := strings.CutSuffix(e.Name(), ".code")
		if !ok {
			continue
		}
		b := corpusBuild(t, id)
		set := b.ActiveSkills()
		if set == nil || b.ActiveItems() == nil {
			continue
		}
		var cp *Comparer
		for i, g := range set.Groups {
			if !strings.HasPrefix(g.Source, "Item:") || (i+1 != b.MainSocketGroup && !g.IncludeInFullDPS) {
				continue
			}
			it := b.Item(b.ActiveItems().SlotItem(g.Slot))
			if it == nil {
				continue
			}
			if cp == nil {
				cp = newComparer(t, c, b)
			}
			r, itemErr := cp.Item(g.Slot, it.Raw)
			if itemErr != nil {
				t.Fatalf("%s %s: %v", id, g.Slot, itemErr)
			}
			n++
			if len(r.Delta) != 0 {
				t.Errorf("%s group %d (%s in %s): delta %v", id, i+1, g.Gems[0].SkillID, g.Slot, r.Delta)
			}
		}
	}
	if n < 5 {
		t.Fatalf("only %d cases", n)
	}
}
