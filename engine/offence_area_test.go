package engine

import "testing"

// CalcOffence.lua:1150: PiercedCount is the configured count, capped by
// the pierce count.
func TestPiercedCount(t *testing.T) {
	actor := NewModDB()
	actor.enemy = NewModDB()
	s := triggerTestSkill("Frostbolt")
	s.flags["projectile"] = true
	s.mods.NewMod("PierceCount", "BASE", 2, "Tree")
	s.mods.NewMod("PiercedCount", "BASE", 5, "Config")
	out := map[string]float64{}
	pierceForkStats(actor, s, out)
	if out["PiercedCount"] != 2 || actor.output["PiercedCount"] != 2 || !s.flags["piercing"] {
		t.Errorf(
			"got %v (actor %v, piercing %v), want 2",
			out["PiercedCount"],
			actor.output["PiercedCount"],
			s.flags["piercing"],
		)
	}
}
