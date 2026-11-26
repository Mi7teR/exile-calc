package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBreakdownOnlyOutputs: outputs PoB sets only inside "if breakdown"
// blocks, which MAIN mode always has: the flask breakdown
// (CalcPerform.lua:1617-1637) and the tincture breakdown (1876-1880).
// Every corpus build matches the oracle.
func TestBreakdownOnlyOutputs(t *testing.T) {
	oracleStats(t, true, "FlaskEffect", "FlaskChargeGen", "LifeFlaskChargeGen", "ManaFlaskChargeGen",
		"UtilityFlaskChargeGen", "FlaskChargeOnCritChance", "TinctureEffect", "TinctureLimit")
}

// TestNonDamagingAilmentOutputs: the non-damaging ailment, knockback and
// stun part of calcs.offence (CalcOffence.lua:5376-5605, combined for
// attacks at 5722-5737) matches the oracle on every corpus build, also
// in which outputs are set at all.
func TestNonDamagingAilmentOutputs(t *testing.T) {
	oracleStats(t, false, "ChillEffectMod", "ChillDurationMod", "ChillSourceEffect", "FreezeDurationMod",
		"ChillDuration", "ShockDuration", "ScorchDuration", "BrittleDuration", "SapDuration", "ShockEffectMod",
		"ScorchEffectMod", "BrittleEffectMod", "SapEffectMod", "ShockChance", "FreezeChance", "ScorchChance",
		"BrittleChance", "SapChance", "ChillChance", "KnockbackChance", "KnockbackDistance", "KnockbackChanceOnHit",
		"KnockbackChanceOnCrit", "EnemyStunThresholdMod", "EnemyStunDuration")
}

// oracleStats checks keys of every corpus build against the oracle; with
// always set, every build must have each key.
func oracleStats(t *testing.T, always bool, keys ...string) {
	t.Helper()
	if testing.Short() {
		t.Skip("corpus")
	}
	c := newCalc(t)
	files, err := filepath.Glob("../testdata/golden/expected/*.json")
	if err != nil || len(files) == 0 {
		t.Fatal("no expected results", err)
	}
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".json")
		raw, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		var exp map[string]any
		if jerr := json.Unmarshal(raw, &exp); jerr != nil {
			t.Fatal(jerr)
		}
		res, cerr := c.Calc(corpusBuild(t, id))
		if cerr != nil {
			t.Fatal(cerr)
		}
		for _, k := range keys {
			want, ok := exp[k].(float64)
			got, has := res.Stats[k]
			if !always && !ok && !has {
				continue
			}
			if !ok || !has || !near(got, want, 1e-9) {
				t.Errorf("%s %s = %v (set %v), oracle %v", id, k, got, has, exp[k])
			}
		}
	}
}
