package engine

import (
	"math"
	"testing"
)

// Seismic Trap, "Average active traps, average waves": 3 pulses of 0.9s in
// a 3.5s duration, 4s trap cooldown (0.675 active traps), 5 waves landing
// with chance (9 + 5 - 1)^2 / 18^2.
func TestSeismicTrapPreDamage(t *testing.T) {
	s := triggerTestSkill("Seismic Trap")
	s.part = 5
	s.data.set("repeatInterval", 0.9)
	s.data.duration = optF{3.5, true}
	s.mods.NewMod("MaximumWaves", "BASE", 5, "Gem")
	s.mods.NewMod("EnemyRadius", "BASE", 5, "Config")
	out := map[string]float64{"TrapCooldown": 4, "AreaOfEffectRadius": 18, "AreaOfEffectRadiusSecondary": 9}
	seismicTrapPreDamage(nil, s, out)
	overlap := 13.0 * 13 / (18 * 18)
	want := 0.675 * 5 * overlap
	if got := s.data.dpsMultiplier.or(1); math.Abs(got-want) > 1e-9 {
		t.Errorf("dpsMultiplier %v, want %v", got, want)
	}
	if got := s.data.hitTimeOverride.or(0); math.Abs(got-0.9) > 1e-9 || out["PulsesPerTrap"] != 3 {
		t.Errorf("hitTimeOverride %v pulses %v, want 0.9 3", got, out["PulsesPerTrap"])
	}
}
