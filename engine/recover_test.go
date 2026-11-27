package engine

import (
	"errors"
	"testing"
)

// TestEntryPointsRecoverPanics: a build that makes the engine panic (here
// a nil build) gives an ErrInternal error from every exported entry point
// instead of crashing the caller.
func TestEntryPointsRecoverPanics(t *testing.T) {
	c := newCalc(t)
	calls := map[string]func() error{
		"Calc":         func() error { _, err := c.Calc(nil); return err },
		"FullDPS":      func() error { _, err := c.FullDPS(nil); return err },
		"NewComparer":  func() error { _, err := c.NewComparer(nil); return err },
		"CalcMinions":  func() error { _, err := c.CalcMinions(nil); return err },
		"CostWarnings": func() error { _, err := c.CostWarnings(nil); return err },
		"Combat":       func() error { _, err := c.Combat(nil, CombatOptions{}); return err },
		"Comparer.Item": func() error {
			_, err := (&Comparer{c: c}).Item("Ring 1", gameRing)
			return err
		},
	}
	for name, call := range calls {
		if err := call(); !errors.Is(err, ErrInternal) {
			t.Errorf("%s: err = %v, want ErrInternal", name, err)
		}
	}
	// The Calculator still works after the recovered panics.
	if _, err := c.Calc(corpusBuild(t, "3uy_tEm_vqpB")); err != nil {
		t.Fatal(err)
	}
}
