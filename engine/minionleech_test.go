package engine

import (
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

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
// the minion gets the player's spell suppression chance before its
// calcs.defence.
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
		if got := mdb.output["SpellSuppressionChance"]; got != want {
			t.Errorf("share %v: SpellSuppressionChance %v, want %v", share, got, want)
		}
	}
}

// The real mod line, "Your Minions gain your chance to Suppress Spell
// Damage" (ModParser.lua:4518), as a Config tab custom modifier: the
// player's spell suppression reaches the main minion's defence.
func TestMinionSpellSuppressionModLine(t *testing.T) {
	c := newCalc(t)
	for _, share := range []bool{false, true} {
		b := corpusBuild(t, "0BL0JzvGlFo_")
		text := "+30% chance to Suppress Spell Damage"
		if share {
			text += "\nYour Minions gain your chance to Suppress Spell Damage"
		}
		cfg := b.ActiveConfig()
		cfg.CustomMods = append(cfg.CustomMods, pobcode.CustomModBlock{Enabled: true, Text: text})
		res, err := c.Calc(b)
		if err != nil {
			t.Fatal(err)
		}
		player := res.Stats["SpellSuppressionChance"]
		mins, err := c.CalcMinions(b)
		if err != nil || len(mins) == 0 {
			t.Fatalf("minions %v %v", mins, err)
		}
		want := 0.0
		if share {
			want = player
		}
		if player < 30 {
			t.Fatalf("player suppression %v", player)
		}
		if got := mins[0].Stats["SpellSuppressionChance"]; got != want {
			t.Errorf("share %v: %s suppression %v, want %v", share, mins[0].Minion, got, want)
		}
	}
}
