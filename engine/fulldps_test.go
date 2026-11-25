package engine

import (
	"strings"
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// withoutFullDPS returns a copy of b with includeInFullDPS cleared on
// every group of the active skill set.
func withoutFullDPS(b *pobcode.Build) *pobcode.Build {
	cp := *b
	cp.SkillSets = append([]pobcode.SkillSet(nil), b.SkillSets...)
	if set := cp.ActiveSkills(); set != nil {
		set.Groups = append([]pobcode.SocketGroup(nil), set.Groups...)
		for i := range set.Groups {
			set.Groups[i].IncludeInFullDPS = false
		}
	}
	return &cp
}

// calcs.calcFullDPS with no skill flagged: combinedDPS 0, no skills.
func TestFullDPSNoSkillsFlagged(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "3uy_tEm_vqpB") // four groups in Full DPS
	r, err := c.FullDPS(withoutFullDPS(b))
	if err != nil {
		t.Fatal(err)
	}
	if r.FullDPS != 0 || r.FullDotDPS != 0 || len(r.Skills) != 0 {
		t.Errorf("FullDPS %v FullDotDPS %v skills %+v, want 0 and none", r.FullDPS, r.FullDotDPS, r.Skills)
	}
}

// withGroupCount returns a copy of b whose Full DPS groups have groupCount
// n, and the 1-based index of the last of them.
func withGroupCount(b *pobcode.Build, n int) (*pobcode.Build, int) {
	cp := withoutFullDPS(b)
	set, orig := cp.ActiveSkills(), b.ActiveSkills()
	g := -1
	for i := range orig.Groups {
		if orig.Groups[i].IncludeInFullDPS {
			set.Groups[i].IncludeInFullDPS, set.Groups[i].GroupCount = true, &n
			g = i + 1
		}
	}
	return cp, g
}

// firstOfGroup is the first Skills entry of socket group g.
func firstOfGroup(r *FullDPSResult, g int) *SkillDPS {
	for i := range r.Skills {
		if r.Skills[i].Group == g {
			return &r.Skills[i]
		}
	}
	return nil
}

// getActiveSkillCount: a group's groupCount overrides the gem count, and
// the skill's DPS is counted that many times.
func TestFullDPSGroupCount(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "EdNBLqyOUdTp") // one group in Full DPS, gem count 5
	one, err := c.FullDPS(b)
	if err != nil {
		t.Fatal(err)
	}
	cp, g := withGroupCount(b, 2)
	two, err := c.FullDPS(cp)
	if err != nil {
		t.Fatal(err)
	}
	first, second := firstOfGroup(one, g), firstOfGroup(two, g)
	if first == nil || second == nil {
		t.Fatalf("no skill of group %d: %+v / %+v", g, one.Skills, two.Skills)
	}
	if first.Count != 5 || second.Count != 2 || second.DPS != first.DPS {
		t.Errorf("counts %v/%v DPS %v/%v, want 5/2 and equal DPS", first.Count, second.Count, first.DPS, second.DPS)
	}
}

// groupCount 0 is a count (Calcs.lua getActiveSkillCount returns it with
// enabled true): the hits count zero times, the best ignite still counts.
func TestFullDPSGroupCountZero(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "eI3KTLED5mcJ") // Static Strike group in Full DPS, its ignite the best
	full, err := c.FullDPS(b)
	if err != nil {
		t.Fatal(err)
	}
	cp, g := withGroupCount(b, 0)
	zero, err := c.FullDPS(cp)
	if err != nil {
		t.Fatal(err)
	}
	if full.FullDotDPS <= 0 || full.FullDPS <= full.FullDotDPS {
		t.Fatalf("fixture: FullDPS %v FullDotDPS %v, want hits and an ignite", full.FullDPS, full.FullDotDPS)
	}
	e := firstOfGroup(zero, g)
	if e == nil || e.Count != 0 || e.DPS <= 0 {
		t.Errorf("group %d entry %+v, want count 0 with its DPS", g, e)
	}
	if zero.FullDotDPS != full.FullDotDPS || zero.FullDPS != full.FullDotDPS {
		t.Errorf("FullDPS %v FullDotDPS %v, want both %v (the ignite only)",
			zero.FullDPS, zero.FullDotDPS, full.FullDotDPS)
	}
}

// FullDPS leaves the build as it was: Calc gives the same stats after.
func TestFullDPSDoesNotMutateBuild(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "tyLAO6f9GS0_") // seven groups in Full DPS
	before, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.FullDPS(b); err != nil {
		t.Fatal(err)
	}
	after, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range before.Stats {
		if after.Stats[k] != v && v == v { // NaN-safe
			t.Errorf("%s %v after FullDPS, %v before", k, after.Stats[k], v)
		}
	}
}

// getActiveSkillCount: a Vaal gem's second skill (the base skill) counts
// only with enableGlobal2, though it is still an active skill.
func TestFullDPSVaalEnableGlobal2(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "gzVTJCkWXFan") // group 2: Vaal Caustic Arrow, Caustic Arrow
	cp := withoutFullDPS(b)
	g := &cp.ActiveSkills().Groups[1]
	g.IncludeInFullDPS = true
	g.Gems = append([]pobcode.Gem(nil), g.Gems...)
	for i := range g.Gems {
		if g.Gems[i].SkillID == "VaalCausticArrow" {
			g.Gems[i].EnableGlobal2 = false
		}
	}
	res, err := c.Calc(cp)
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, s := range res.Skills {
		listed = listed || (s.Group == 2 && s.Name == "Caustic Arrow")
	}
	if !listed {
		t.Fatal("fixture: Caustic Arrow is not an active skill")
	}
	r, err := c.FullDPS(cp)
	if err != nil {
		t.Fatal(err)
	}
	vaal := false
	for _, s := range r.Skills {
		if s.Group == 2 && s.GroupSkill == 2 {
			t.Errorf("Caustic Arrow counted with enableGlobal2 false: %+v", s)
		}
		vaal = vaal || (s.Group == 2 && s.GroupSkill == 1)
	}
	if !vaal {
		t.Errorf("Vaal Caustic Arrow missing: %+v", r.Skills)
	}
}

// A disabled main group stays in the active skill list of every skill's
// calculation, as in PoB's Full DPS env: its auras still apply.
func TestFullDPSKeepsDisabledMainGroup(t *testing.T) {
	c := newCalc(t)
	b := withoutFullDPS(corpusBuild(t, "5g9VlzQd4BVS")) // group 2: Wrath; group 4: Spark
	b.MainSocketGroup = 2
	set := b.ActiveSkills()
	set.Groups[1].Enabled = false
	set.Groups[3].IncludeInFullDPS = true
	var spark *SkillDPS
	r, err := c.FullDPS(b)
	if err != nil {
		t.Fatal(err)
	}
	for i := range r.Skills {
		if r.Skills[i].Group == 4 && strings.HasPrefix(r.Skills[i].Name, "Spark") {
			spark = &r.Skills[i]
		}
	}
	if spark == nil {
		t.Fatalf("no Spark entry: %+v", r.Skills)
	}
	with, err := c.Calc(withMainSkill(b, 4, spark.GroupSkill))
	if err != nil {
		t.Fatal(err)
	}
	enabled := withMainSkill(b, 4, spark.GroupSkill)
	enabled.ActiveSkills().Groups[1].Enabled = true
	want, err := c.Calc(enabled)
	if err != nil {
		t.Fatal(err)
	}
	if with.Stats["TotalDPS"] >= want.Stats["TotalDPS"] {
		t.Fatalf("fixture: Wrath does not raise Spark (%v without, %v with)",
			with.Stats["TotalDPS"], want.Stats["TotalDPS"])
	}
	if spark.DPS != want.Stats["TotalDPS"] {
		t.Errorf("Spark DPS %v, want %v (with the disabled main group's Wrath)", spark.DPS, want.Stats["TotalDPS"])
	}
}
