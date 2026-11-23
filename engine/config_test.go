package engine

import (
	"strings"
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

func TestApplyConfig(t *testing.T) {
	calc, err := New()
	if err != nil {
		t.Fatal(err)
	}
	set := &pobcode.ConfigSet{Inputs: []pobcode.Input{
		{Name: "buffMassiveShrine", Kind: pobcode.KindBool, Bool: true},
		{Name: "conditionShockEffect", Kind: pobcode.KindNumber, Number: 35},
		{Name: "conditionStationary", Kind: pobcode.KindNumber, Number: 3},
		{Name: "conditionLowLife", Kind: pobcode.KindBool, Bool: false},
	}}
	db := NewModDB()
	db.enemy = NewModDB()
	res := &Result{}
	calc.applyConfig(db, res, set)
	for _, s := range res.Incomplete {
		if !strings.HasPrefix(s, "config enemyIsBoss") { // needs build.configTab
			t.Errorf("incomplete: %s", s)
		}
	}
	buffMode(db)
	if !db.GetCondition("MassiveShrine") {
		t.Error("Condition:MassiveShrine not set")
	}
	if db.GetCondition("LowLife") {
		t.Error("an unchecked option must not apply")
	}
	if got := db.GetMultiplier("StationarySeconds"); got != 3 {
		t.Errorf("StationarySeconds = %v", got)
	}
	if got := db.enemy.Sum("BASE", "ShockVal"); got != 0 { // needs Condition:ShockedConfig
		t.Errorf("ShockVal = %v", got)
	}
	if got := db.enemy.Sum("BASE", "DesiredShockVal"); got != 35 {
		t.Errorf("DesiredShockVal = %v", got)
	}
	shrineBaseMods(db)
	shrineBuffs(db)
	if got := db.Sum("INC", "Life"); got != 40 {
		t.Errorf("Massive Shrine Life INC = %v", got)
	}
}
