package engine

import (
	"math"
	"testing"
)

// ModDB.lua MoreInternal: a name in data.highPrecisionMods floors the
// product to its precision instead of rounding to 2 places
// (SupportManaMultiplier: 4).
func TestMoreHighPrecision(t *testing.T) {
	db := NewModDB()
	db.NewMod("SupportManaMultiplier", "MORE", 98, "Arrogance")
	db.NewMod("SupportManaMultiplier", "MORE", -12, "Enlighten")
	if got := db.More("SupportManaMultiplier"); math.Abs(got-1.7424) > 1e-12 {
		t.Errorf("SupportManaMultiplier more = %v, want 1.7424", got)
	}
	db.NewMod("Damage", "MORE", 98, "A")
	db.NewMod("Damage", "MORE", -12, "B")
	if got := db.More("Damage"); got != 1.74 {
		t.Errorf("Damage more = %v, want 1.74 (2 places)", got)
	}
}
