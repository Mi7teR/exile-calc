package engine

import (
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// Bane counts the curses of its own group it applies, up to the curse
// limit; curses of other groups do not count.
func TestBanePreSkillType(t *testing.T) {
	g, other := &pobcode.SocketGroup{}, &pobcode.SocketGroup{}
	actor := NewModDB()
	bane := triggerTestSkill("Bane of Condemnation")
	bane.group = g
	actor.activeSkills = []*activeSkill{bane}
	for _, grp := range []*pobcode.SocketGroup{g, g, g, other} {
		c := triggerTestSkill("Despair")
		c.group = grp
		c.mods.Add(Mod{Name: "Condition:AppliedByBane", Type: "FLAG", Value: true, Source: "Skill:SupportDarkRitual"})
		actor.activeSkills = append(actor.activeSkills, c)
	}
	for _, c := range []struct {
		limit float64
		want  float64
	}{{0, 3}, {2, 2}} {
		bane.mods = NewModDB()
		delete(actor.output, "EnemyCurseLimit")
		if c.limit > 0 {
			actor.output["EnemyCurseLimit"] = c.limit
		}
		banePreSkillType(actor, bane)
		if got := bane.mods.Sum("BASE", "Multiplier:CurseApplied"); got != c.want {
			t.Errorf("limit %v: CurseApplied %v, want %v", c.limit, got, c.want)
		}
	}
}
