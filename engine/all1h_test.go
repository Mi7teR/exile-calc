package engine

import (
	"testing"
)

// varunastraRaw is a Varunastra with two lines that read the weapon types
// it counts as: claw damage and attack speed while wielding a dagger.
const varunastraRaw = `Rarity: UNIQUE
Varunastra
Vaal Blade
Implicits: 1
+460 to Accuracy Rating
90% increased Physical Damage
Adds 40 to 90 Physical Damage
Grants 3 Mana per Enemy Hit
Counts as all One Handed Melee Weapon Types
100% increased Damage with Claws
50% increased Attack Speed while wielding a Dagger`

// TestCountsAsAll1H: Varunastra's countsAsAll1H in weapon flags
// (CalcActiveSkill.lua:212-244), support filtering (CalcTools.lua:119-147),
// conditions (CalcPerform.lua:162-229) and negated weapon conditions
// (ModStore.lua:635-657), for the player and a minion. The builds are
// corpus builds with Varunastra in the main hand; the numbers are PoB's.
// 0BL0JzvGlFo_'s Spectral Throw keeps Nightblade's crit chance, whose
// "not while wielding a sword, axe or mace" condition Varunastra's added
// conditions do not break.
func TestCountsAsAll1H(t *testing.T) {
	c := newCalc(t)
	for _, x := range []struct {
		id                    string
		totalDPS, crit, speed float64
	}{
		{"0BL0JzvGlFo_", 481804.08895893, 75.96, 6.0528},
		{"8EreRMfyXz8n", 4556867.7075695, 73.07, 19.2582},
	} {
		b := corpusBuild(t, x.id)
		b.Item(b.ActiveItems().SlotItem("Weapon 1")).Raw = varunastraRaw
		res, err := c.Calc(b)
		if err != nil {
			t.Fatal(err)
		}
		for k, want := range map[string]float64{"TotalDPS": x.totalDPS, "CritChance": x.crit, "Speed": x.speed} {
			if got := res.Stats[k]; !near(got, want, 1e-9) {
				t.Errorf("%s %s %v, want %v", x.id, k, got, want)
			}
		}
		if res.main.all1H != [2]bool{true, false} || !res.DB.conditions["UsingDagger"] ||
			!res.DB.conditions["WieldingDifferentWeaponTypes"] {
			t.Errorf("%s: all1H %v conditions %v", x.id, res.main.all1H, res.DB.conditions)
		}
	}

	// The Animated Guardian wielding it.
	b := corpusBuild(t, "a2nRZLW9ZVuu")
	for i := range b.ItemSets {
		if id := b.ItemSets[i].SlotItem("Weapon 1"); id != 0 {
			b.Item(id).Raw = varunastraRaw
		}
	}
	mins, err := c.CalcMinions(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mins {
		if m.Minion == "Animated Guardian" {
			if !near(m.Stats["Speed"], 3.172, 1e-9) || !near(m.Stats["TotalDPS"], 35152.53972835, 1e-9) {
				t.Errorf("Animated Guardian Speed %v TotalDPS %v, want 3.172 35152.53972835",
					m.Stats["Speed"], m.Stats["TotalDPS"])
			}
			return
		}
	}
	t.Fatal("no Animated Guardian")
}

// TestAll1HAdded: the Added<condition> flags of a countsAsAll1H weapon
// (CalcPerform.lua:162-175, 200-213) and their effect on negated
// conditions (ModStore.lua:635-657).
func TestAll1HAdded(t *testing.T) {
	a := all1HAdded([2]string{"One Handed Sword", "Dagger"}, [2]bool{true, false},
		[2]string{"Varunastra, Vaal Blade", ""})
	want := map[string]bool{
		"UsingAxe": true, "UsingSword": true, "UsingDagger": true, "UsingMace": true, "UsingClaw": true,
	}
	for k, v := range want {
		if a[k] != v {
			t.Errorf("main hand %s %v, want %v", k, a[k], v)
		}
	}
	// In the off hand after a dagger: the dagger was already there.
	a = all1HAdded([2]string{"Dagger", "One Handed Sword"}, [2]bool{false, true}, [2]string{"", "Other"})
	if a["UsingDagger"] || a["UsingSword"] || !a["UsingAxe"] {
		t.Errorf("off hand %v", a)
	}
	if all1HAdded([2]string{"Dagger", ""}, [2]bool{}, [2]string{}) != nil {
		t.Error("Added flags without a countsAsAll1H weapon")
	}
	db := NewModDB()
	db.conditions["UsingSword"], db.conditions["UsingDagger"] = true, true
	db.all1HAdded = map[string]bool{"UsingSword": true, "UsingDagger": false}
	not := func(v string, value float64) Mod {
		return Mod{Name: "Life", Type: "BASE", Value: value,
			Tags: []map[string]any{{"type": "Condition", "var": v, "neg": true}}}
	}
	notSword, notDagger := not("UsingSword", 10), not("UsingDagger", 5)
	db.Add(notSword)
	db.Add(notDagger)
	if got := db.Sum("BASE", "Life"); got != 10 {
		t.Errorf("Life %v, want 10 (the added sword is ignored, the dagger was already set)", got)
	}
}
