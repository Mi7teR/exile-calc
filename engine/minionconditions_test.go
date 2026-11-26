package engine

import (
	"maps"
	"slices"
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/itemtext"
)

func TestMinionEquipmentConditions(t *testing.T) {
	item := func(typ, sub string, weapon bool) *itemtext.Item {
		b := &gamedata.Base{Type: typ, SubType: sub}
		if weapon {
			b.Weapon = &gamedata.WeaponStats{}
		}
		return &itemtext.Item{Base: b}
	}
	for _, c := range []struct {
		name   string
		items  map[string]*itemtext.Item
		w1, w2 string
		want   []string
	}{
		{"monster weapon", nil, "One Handed Sword", "",
			[]string{"OffHandIsEmpty", "UsingMeleeWeapon", "UsingOneHandedWeapon", "UsingSword"}},
		{"unarmed", nil, "None", "",
			[]string{"OffHandIsEmpty", "Unarmed", "Unencumbered"}},
		{"guardian staff and armour", map[string]*itemtext.Item{
			"Weapon 1": item("Staff", "Warstaff", true), "Body Armour": item("Body Armour", "", false),
		}, "Staff", "",
			[]string{"OffHandIsEmpty", "UsingBody Armour", "UsingMeleeWeapon", "UsingStaff", "UsingTwoHandedWeapon"}},
		{"sword and shield", map[string]*itemtext.Item{
			"Weapon 1": item("One Handed Sword", "", true), "Weapon 2": item("Shield", "", false),
		}, "One Handed Sword", "",
			[]string{"UsingMeleeWeapon", "UsingOneHandedWeapon", "UsingShield", "UsingSword"}},
		{"thrusting and one handed sword", map[string]*itemtext.Item{
			"Weapon 1": item("One Handed Sword", "Thrusting", true), "Weapon 2": item("One Handed Sword", "", true),
		}, "One Handed Sword", "One Handed Sword",
			[]string{"DualWielding", "UsingMeleeWeapon", "UsingOneHandedWeapon", "UsingSword"}},
		{"claw and dagger", map[string]*itemtext.Item{
			"Weapon 1": item("Claw", "", true), "Weapon 2": item("Dagger", "", true),
		}, "Claw", "Dagger",
			[]string{"DualWielding", "UsingClaw", "UsingDagger", "UsingMeleeWeapon", "UsingOneHandedWeapon", "WieldingDifferentWeaponTypes"}},
	} {
		cond := map[string]bool{}
		minionEquipmentConditions(cond, c.items, c.w1, c.w2)
		if got := slices.Sorted(maps.Keys(cond)); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestAnimatedGuardianStaffBlock: the Animated Guardian's staff sets
// UsingStaff on the minion, so the staff's block chance applies
// (a2nRZLW9ZVuu: 36, PoB's value).
func TestAnimatedGuardianStaffBlock(t *testing.T) {
	ms, err := newCalc(t).CalcMinions(corpusBuild(t, "a2nRZLW9ZVuu"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.Minion == "Animated Guardian" {
			if got := m.Stats["BlockChance"]; got != 36 {
				t.Errorf("Animated Guardian BlockChance %v, want 36", got)
			}
			return
		}
	}
	t.Fatal("no Animated Guardian")
}
