package engine

import (
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

func TestItemEnergyShield(t *testing.T) {
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		text string
		want float64
	}{
		// Base 75-88, percentile 1, +50 local, 100% increased, quality 20.
		{"Rarity: RARE\nTest\nCabalist Regalia\nQuality: 20\nImplicits: 0\n+50 to maximum Energy Shield\n100% increased Energy Shield", 331},
		// Percentile from the text: 0.5 of the 13 variance.
		{"Rarity: RARE\nTest\nCabalist Regalia\nEnergyShieldBasePercentile: 0.5\nImplicits: 0\n+10 to Strength", 82},
		// "Quality does not increase Defences".
		{"Rarity: RARE\nTest\nCabalist Regalia\nQuality: 20\nImplicits: 1\nQuality does not increase Defences\n+10 to Strength", 88},
		// ItemClass:BuildModList removes The Apostate's energy shield.
		{"Rarity: UNIQUE\nThe Apostate\nCabalist Regalia\nImplicits: 0\n+40 to Strength", 0},
	}
	for _, tc := range cases {
		it, mods := c.itemMods(&pobcode.Item{Raw: tc.text}, &Result{})
		if it == nil {
			t.Fatalf("%q: not parsed", tc.text)
		}
		if got := itemEnergyShield(it, mods); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.text, got, tc.want)
		}
	}
}
