package engine

import (
	"math"
	"testing"
)

// TestBifurcateNeedsModeEffective: CalcOffence.lua:3162 and 3261 apply
// BifurcateCrit only with env.mode_effective, which the actor's Effective
// condition mirrors (CalcSetup.lua:134).
func TestBifurcateNeedsModeEffective(t *testing.T) {
	for _, effective := range []bool{true, false} {
		list := NewModDB()
		list.Add(Mod{Name: "BifurcateCrit", Type: "FLAG", Value: true, Source: "Test"})
		list.Add(Mod{Name: "CritChanceCap", Type: "BASE", Value: 100.0, Source: "Base"})
		list.Add(Mod{Name: "CritMultiplier", Type: "BASE", Value: 50.0, Source: "Base"})
		actor := NewModDB()
		actor.conditions["Effective"] = effective
		p := &hitPass{out: map[string]float64{"AccuracyHitChance": 100}, src: hitSource{critChance: 50}}
		(&Calculator{}).crit(&activeSkill{mods: list}, p, NewModDB(), actor)

		wantCrit, wantBif := 50.0, 0.0
		if effective {
			wantCrit, wantBif = 75, 1+25.0/75 // 1-(1-0.5)^2; 0.5^2 / 0.75
		}
		if got := p.out["CritChance"]; math.Abs(got-wantCrit) > 1e-9 {
			t.Errorf("effective %v: CritChance %v, want %v", effective, got, wantCrit)
		}
		if got := p.out["CritBifurcates"]; math.Abs(got-wantBif) > 1e-9 {
			t.Errorf("effective %v: CritBifurcates %v, want %v", effective, got, wantBif)
		}
	}
}
