package engine

import (
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

// getActiveSkillCount: a group's groupCount overrides the gem count, and
// the skill's DPS is counted that many times.
func TestFullDPSGroupCount(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "EdNBLqyOUdTp") // one group in Full DPS, gem count 5
	one, err := c.FullDPS(b)
	if err != nil {
		t.Fatal(err)
	}
	cp := withoutFullDPS(b)
	set, orig := cp.ActiveSkills(), b.ActiveSkills()
	g := -1
	for i := range orig.Groups {
		if orig.Groups[i].IncludeInFullDPS {
			set.Groups[i].IncludeInFullDPS, set.Groups[i].GroupCount = true, 2
			g = i + 1
		}
	}
	two, err := c.FullDPS(cp)
	if err != nil {
		t.Fatal(err)
	}
	var first, second *SkillDPS
	for i := range one.Skills {
		if one.Skills[i].Group == g {
			first = &one.Skills[i]
			break
		}
	}
	for i := range two.Skills {
		if two.Skills[i].Group == g {
			second = &two.Skills[i]
			break
		}
	}
	if first == nil || second == nil {
		t.Fatalf("no skill of group %d: %+v / %+v", g, one.Skills, two.Skills)
	}
	if first.Count != 5 || second.Count != 2 || second.DPS != first.DPS {
		t.Errorf("counts %v/%v DPS %v/%v, want 5/2 and equal DPS", first.Count, second.Count, first.DPS, second.DPS)
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
