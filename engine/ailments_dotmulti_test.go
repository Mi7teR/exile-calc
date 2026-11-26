package engine

import (
	"math"
	"testing"
)

// TestPoisonDotMultiWeightedInMainMode: with a breakdown (MAIN mode),
// CalcOffence.lua:4944-4951 replaces PoisonDotMulti with the non-crit and
// crit DoT multipliers weighted by the poisons each creates; CALCULATOR mode
// keeps the non-crit one. Seismic Trap as the main skill of Kh5YfbuwGaG0:
// the oracle's mainOutput has PoisonDotMulti 2.615 (2.58 x 0.93 + 3.08 x 0.07).
func TestPoisonDotMultiWeightedInMainMode(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "Kh5YfbuwGaG0")
	b.MainSocketGroup = 2 // Seismic Trap
	for _, x := range []struct {
		calculator bool
		want       float64
	}{{false, 2.615}, {true, 2.58}} {
		res, err := c.calc(b, calcOpts{calculator: x.calculator})
		if err != nil {
			t.Fatal(err)
		}
		if res.main == nil || res.main.active.ge.Name != "Seismic Trap" {
			t.Fatalf("main skill is not Seismic Trap")
		}
		if got := res.Stats["PoisonDotMulti"]; math.Abs(got-x.want) > 1e-6 {
			t.Errorf("calculator %v: PoisonDotMulti %v, want %v", x.calculator, got, x.want)
		}
		if got := res.Stats["CritPoisonDotMulti"]; math.Abs(got-3.08) > 1e-9 {
			t.Errorf("calculator %v: CritPoisonDotMulti %v, want 3.08", x.calculator, got)
		}
	}
}
