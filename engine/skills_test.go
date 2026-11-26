package engine

import (
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

func TestTypeExpressionMatch(t *testing.T) {
	types := map[int]bool{stAura: true, stSpell: true}
	cases := []struct {
		expr []int
		want bool
	}{
		{[]int{stAura}, true},
		{[]int{stAttack}, false},
		{[]int{stAttack, stAura, stOR}, true},
		{[]int{stAttack, stAura, stAND}, false},
		{[]int{stAura, stNOT}, false},
		{[]int{stAttack, stNOT}, true},
		{[]int{stAttack, stAura}, true}, // any true value left on the stack
	}
	for _, c := range cases {
		if got := typeExpressionMatch(c.expr, types, nil); got != c.want {
			t.Errorf("%v: got %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestMatchKeywordFlags(t *testing.T) {
	if !matchKeywordFlags(kfAura|kfSpell, 0) || !matchKeywordFlags(kfAura|kfSpell, kfAura) {
		t.Error("any-match failed")
	}
	if matchKeywordFlags(kfAura, kfAura|kfCurse|keywordMatchAll) {
		t.Error("match-all must need every flag")
	}
	if matchKeywordFlags(0, kfAura) {
		t.Error("a query without a cfg must not select keyword mods")
	}
}

// Purity of Elements 20/20: 34% from level 20 plus 0.25 per quality.
func TestSkillStatsPurity(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	ge := c.data.Skills.Effects["PurityOfElements"]
	e := &skillEffect{id: "PurityOfElements", ge: ge, level: 20, quality: 20}
	if got := skillStats(e, 0)["base_resist_all_elements_%"]; got != 39 {
		t.Errorf("got %v, want 39", got)
	}
	list := NewModDB()
	c.mergeSkillInstanceMods(list, e, nil)
	ms := list.mods["ElementalResist"]
	if len(ms) != 1 || ms[0].Value != 39.0 || ms[0].Source != "Skill:PurityOfElements" {
		t.Fatalf("mods: %+v", ms)
	}
	s := &activeSkill{active: e, mods: list, cfg: &Cfg{}, data: &skillData{}}
	c.separateBuffs(s)
	if len(s.buffs) != 1 || s.buffs[0].typ != "Aura" || s.buffs[0].name != "Purity of Elements" || len(list.mods["ElementalResist"]) != 0 {
		t.Fatalf("buffs: %+v", s.buffs)
	}
}

// SkillsTab.lua LoadSkill/ProcessSocketGroup: a gemId with no known variant
// (Arctic Armour saved with its old metadata id) resolves by the saved name.
func TestResolveGemUnknownGemIDByName(t *testing.T) {
	c := newCalc(t)
	g := &pobcode.Gem{NameSpec: "Arctic Armour", SkillID: "ArcticArmour",
		GemID: "Metadata/Items/Gems/SkillGemArcticArmour", VariantID: "ArcticArmour"}
	gem, id, ok := c.resolveGem(g)
	if !ok || gem == nil || id != "ArcticArmour" {
		t.Fatalf("got gem %v id %q ok %v, want Arctic Armour", gem, id, ok)
	}
	g.NameSpec = "No Such Gem"
	if _, _, ok := c.resolveGem(g); ok {
		t.Error("an unknown gemId with an unknown name must stay unresolved")
	}
}
