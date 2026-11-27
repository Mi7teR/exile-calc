package engine

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/Mi7teR/exile-calc/internal/luadata"
)

// TestBossStatsMatchBossesLua recomputes Data.lua:975-993 data.bossStats from
// PoB's Data/Bosses.lua and checks the hand-copied means in enemy.go. It skips
// when the PoB source is absent; POB_SRC overrides its directory.
func TestBossStatsMatchBossesLua(t *testing.T) {
	src := os.Getenv("POB_SRC")
	if src == "" {
		home, _ := os.UserHomeDir()
		src = filepath.Join(home, "GoProjects/exile-oracle/.pob/v2.68.1/src")
	}
	raw, err := os.ReadFile(filepath.Join(src, "Data/Bosses.lua"))
	if err != nil {
		t.Skipf("PoB source not available: %v", err)
	}
	v, err := luadata.Eval(string(raw), nil)
	if err != nil {
		t.Fatalf("eval Bosses.lua: %v", err)
	}
	var bosses map[string]struct {
		Armour  float64 `lua:"armourMult"`
		Evasion float64 `lua:"evasionMult"`
		IsUber  bool    `lua:"isUber"`
	}
	if err = luadata.Decode(v, &bosses); err != nil {
		t.Fatalf("decode Bosses.lua: %v", err)
	}
	var count, uberCount int
	var armour, evasion, uberArmour, uberEvasion float64
	for _, b := range bosses {
		if b.IsUber {
			uberCount++
			uberArmour += b.Armour
			uberEvasion += b.Evasion
		}
		count++
		armour += b.Armour
		evasion += b.Evasion
	}
	if count == 0 || uberCount == 0 {
		t.Fatalf("no bosses parsed: %d total, %d uber", count, uberCount)
	}
	t.Logf("%d bosses, %d uber", count, uberCount)
	for _, c := range []struct {
		name      string
		lua, goes float64
	}{
		{"PinnacleArmourMean", 100 + armour/float64(count), pinnacleArmourMean},
		{"PinnacleEvasionMean", 100 + evasion/float64(count), pinnacleEvasionMean},
		{"UberArmourMean", 100 + uberArmour/float64(uberCount), uberArmourMean},
		{"UberEvasionMean", 100 + uberEvasion/float64(uberCount), uberEvasionMean},
	} {
		if math.Abs(c.lua-c.goes) > 1e-9 {
			t.Errorf("%s: Bosses.lua gives %v, enemy.go has %v", c.name, c.lua, c.goes)
		}
	}
}
