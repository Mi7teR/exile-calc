package engine

import (
	"math"
	"testing"
)

// Data/Skills/act_int.lua:5519-5521: Eye of Winter's more damage ramps
// from 0 at the caster to full at 60 units times the projectile speed.
func TestEyeOfWinterRamp(t *testing.T) {
	s := triggerTestSkill("Eye of Winter")
	s.mods.NewMod("EyeOfWinterRamp", "BASE", 200, "Gem")
	eyeOfWinterPreDamage(nil, s, map[string]float64{"ProjectileSpeedMod": 0.5})
	for _, c := range []struct{ dist, want float64 }{{0, 1}, {15, 2}, {30, 3}, {80, 3}} {
		cfg := &Cfg{SkillDist: c.dist, HasSkillDist: true}
		if got := s.mods.MoreCfg(cfg, "Damage"); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("distance %v: more %v, want %v", c.dist, got, c.want)
		}
	}
}
