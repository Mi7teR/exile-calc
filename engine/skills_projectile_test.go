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

// Ball Lightning with no projectile speed or strike interval keeps its
// DPS multiplier instead of Inf or NaN, and notes it.
func TestBallLightningGuard(t *testing.T) {
	for _, data := range []map[string]any{
		{"strikeInterval": 0.15, "projectileSpeed": 0.0, "duration": 2.0},
		{"projectileSpeed": 40.0, "duration": 2.0},
	} {
		s := triggerTestSkill("Ball Lightning")
		s.part = 2
		for k, v := range data {
			s.data.set(k, v)
		}
		actor := NewModDB()
		out := map[string]float64{"AreaOfEffectRadius": 22}
		ballLightningPreDamage(actor, s, out)
		if m := s.data.dpsMultiplier.or(1); m != 1 || len(actor.incomplete) != 1 {
			t.Errorf("%v: dpsMultiplier %v, notes %v", data, m, actor.incomplete)
		}
		if _, ok := out["SkillDPSMultiplier"]; ok {
			t.Errorf("%v: SkillDPSMultiplier set", data)
		}
	}
}
