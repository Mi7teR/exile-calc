package engine

import "testing"

// CalcSetup.lua:62-70 and CalcDefence.lua:1243-1272 for env.minion: the
// base caps, and the parent's maximum life leech rate with
// MaximumLifeLeechIsEqualToParent.
func TestMinionLeechCaps(t *testing.T) {
	owner := NewModDB()
	owner.output["MaxLifeLeechRatePercent"] = 25
	mdb := NewModDB()
	mdb.owner = owner
	minionBaseMods(mdb)
	mdb.output["Life"], mdb.output["EnergyShield"], mdb.output["Mana"] = 1000, 200, 0
	mdb.NewMod("MaxLifeLeechRate", "INC", 50, "Tree")
	minionLeechCaps(mdb)
	for k, want := range map[string]float64{
		"MaxLifeLeechInstance": 100, "MaxLifeLeechRatePercent": 30, "MaxLifeLeechRate": 300,
		"MaxEnergyShieldLeechInstance": 20, "MaxEnergyShieldLeechRate": 20, "MaxManaLeechRate": 0,
	} {
		if got := mdb.output[k]; got != want {
			t.Errorf("%s %v, want %v", k, got, want)
		}
	}
	mdb.Add(Mod{Name: "MaximumLifeLeechIsEqualToParent", Type: "FLAG", Value: true})
	minionLeechCaps(mdb)
	if mdb.output["MaxLifeLeechRatePercent"] != 25 || mdb.output["MaxLifeLeechRate"] != 250 {
		t.Errorf("parent: %v %v", mdb.output["MaxLifeLeechRatePercent"], mdb.output["MaxLifeLeechRate"])
	}
}

// CalcPerform.lua:3837-3839: with MinionsGainYourSpellSuppressionChance
// the minion gets the player's spell suppression chance as a mod before
// its calcs.defence. (The minion's defence does not run the avoidance part
// of calcs.defence on this branch, so its SpellSuppressionChance output is
// not computed; the mod is what is checked.)
func TestMinionSpellSuppressionFromPlayer(t *testing.T) {
	c := newCalc(t)
	for _, share := range []bool{false, true} {
		db := NewModDB()
		db.enemy = NewModDB()
		db.output["SpellSuppressionChance"] = 40
		if share {
			db.Add(Mod{Name: "MinionsGainYourSpellSuppressionChance", Type: "FLAG", Value: true})
		}
		mdb := NewModDB()
		mdb.owner, mdb.enemy = db, db.enemy
		minionBaseMods(mdb)
		mc := &minionCalc{mdb: mdb, res: &MinionResult{Stats: map[string]float64{}}}
		c.minionDefence(db, mc)
		want := 0.0
		if share {
			want = 40
		}
		if got := mdb.Sum("BASE", "SpellSuppressionChance"); got != want {
			t.Errorf("share %v: SpellSuppressionChance %v, want %v", share, got, want)
		}
	}
}
