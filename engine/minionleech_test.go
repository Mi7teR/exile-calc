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
