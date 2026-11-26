package engine

import (
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

	got := withTreeGrantedSkills(db, b, nil)
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
	if again := withTreeGrantedSkills(db, got, nil); again != got {
		t.Error("a build with the group gets another one")
	}

	// A saved group at the validated level matches (getNormalizedSkillLevel).
	db = NewModDB()
	db.Add(Mod{Name: "ExtraSkill", Type: "LIST", Source: "Tree:7",
		Value: map[string]any{"skillId": "S", "level": 25.0}})
	skills := map[string]*gamedata.Skill{"S": {Levels: map[int]*gamedata.SkillLevel{1: {}, 2: {}}}}
	b.SkillSets[0].Groups = append(b.SkillSets[0].Groups,
		pobcode.SocketGroup{Enabled: true, Source: "Tree:7", Gems: []pobcode.Gem{{SkillID: "S", Level: 2}}})
	if kept := withTreeGrantedSkills(db, b, skills); kept != b {
		t.Errorf("saved group at the validated level not matched: %d groups", len(kept.ActiveSkills().Groups))
	}
	// Another level makes a new group, as in PoB.
	b.SkillSets[0].Groups[1].Gems[0].Level = 1
	if more := withTreeGrantedSkills(db, b, skills); len(more.ActiveSkills().Groups) != 3 {
		t.Errorf("got %d groups, want a new group for another level", len(more.ActiveSkills().Groups))
	}
	// ... and the stale group no longer counts as granted (PoB removes it).
	if treeGrants(db, &b.SkillSets[0].Groups[1], skills) {
		t.Error("a saved group at another level counts as granted")
	}
}
