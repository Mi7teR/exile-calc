package engine

import (
	"testing"

	"github.com/Mi7teR/exile-calc/pobcode"
)

// A configured ailment on the player without a value takes the ailment's
// default effect (PoB never reaches its default and gives 0); the enemy's
// ailments come from enemyAilments instead.
func TestSelfAilmentDefault(t *testing.T) {
	player := NewModDB()
	player.build = &pobcode.Build{}
	for name, want := range map[string]float64{"Chill": 10, "Shock": 15, "Scorch": 10} {
		if got, ok := selfAilment(player, name, false); !ok || got != want {
			t.Errorf("player %s = %v, %v; want %v", name, got, ok, want)
		}
	}
	player.NewMod("ShockVal", "OVERRIDE", 30.0, "Config")
	if got, _ := selfAilment(player, "Shock", false); got != 30 {
		t.Errorf("player Shock with value = %v; want 30", got)
	}
	enemy := NewModDB()
	if got, _ := selfAilment(enemy, "Chill", false); got != 0 {
		t.Errorf("enemy Chill = %v; want 0", got)
	}
}
