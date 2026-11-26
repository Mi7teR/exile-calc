package engine

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/modparser"
	"github.com/Mi7teR/poe-lib/pobcode"
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
			if bv, isBool := exp[k].(bool); isBool {
				want, ok = b2f(bv), true
			}
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

// TestPerSecondCosts: CalcOffence.lua:5961-6010, the upfront costs per
// second, and the raw costs a cost conversion leaves on the converted
// tables (1754, 1780, 1784), match the oracle on every corpus build.
func TestPerSecondCosts(t *testing.T) {
	oracleStats(
		t,
		false,
		"ManaPerSecondCost",
		"LifePerSecondCost",
		"ESPerSecondCost",
		"SoulPerSecondCost",
		"ManaPercentPerSecondCost",
		"LifePercentPerSecondCost",
		"ESPercentPerSecondCost",
		"ManaPerSecondHasCost",
		"LifePerSecondHasCost",
		"ESPerSecondHasCost",
		"SoulPerSecondHasCost",
		"ManaPercentPerSecondHasCost",
		"LifePercentPerSecondHasCost",
		"ESPercentPerSecondHasCost",
		"ManaPerSecondCostRaw",
		"ManaPercentPerSecondCostRaw",
		"LifePerSecondCostRaw",
		"LifePercentPerSecondCostRaw",
		"ManaCostRaw",
		"LifeCostRaw",
		"ESCostRaw",
		"ManaPercentCostRaw",
		"LifePercentCostRaw",
	)
}

// TestManaCostPayablePool: CalcOffence.lua:1903-1904, unreserved mana plus
// energy shield with Eldritch Battery.
func TestManaCostPayablePool(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "-fxcKCnOf7D2")
	st := calcWithMods(t, c, b, true)
	out := st.res.Stats
	if out["ManaCostPayablePool"] != out["ManaUnreserved"] || out["ManaCostPayablePool"] != 89 {
		t.Errorf("pool %v, unreserved %v; oracle 89", out["ManaCostPayablePool"], out["ManaUnreserved"])
	}
	eb := calcWithMods(t, c, b, true, testMod{m: flagMod("EnergyShieldProtectsMana")}).res.Stats
	if eb["ManaCostPayablePool"] != eb["ManaUnreserved"]+eb["EnergyShield"] {
		t.Errorf(
			"with Eldritch Battery: pool %v, want %v + %v",
			eb["ManaCostPayablePool"],
			eb["ManaUnreserved"],
			eb["EnergyShield"],
		)
	}
}

// TestAttributeRequirements: calcs.perform's attribute requirements
// (CalcPerform.lua:2131-2196) over the items and gems match the oracle on
// every corpus build.
func TestAttributeRequirements(t *testing.T) {
	oracleStats(t, false, "ReqStr", "ReqDex", "ReqInt", "ReqOmni")
}

// TestAttributeRequirementParts: the gem formula (CalcTools.lua:179-189),
// item requirements (Item.lua:2652-2665), the GemProperty "req" override
// (CalcSetup.lua:1925-1927) and Omniscience (CalcPerform.lua:2153-2158).
func TestAttributeRequirementParts(t *testing.T) {
	// Level 20 requirement 70, multi 100: round((20 + 67 * 3) * 0.7) = 155.
	if got := gemStatRequirement(70, false, 100); got != 155 {
		t.Errorf("active gem: %v, want 155", got)
	}
	if got := gemStatRequirement(1, true, 40); got != 0 {
		t.Errorf("below 14: %v, want 0", got)
	}
	c := newCalc(t)
	base := c.data.Bases["Vaal Regalia"]
	it := &itemtext.Item{Base: base, Req: itemtext.Requirements{Int: 999}}
	if got := itemRequirements(it, nil); got[2] != 999 {
		t.Errorf("item Int %v, want the item's own 999", got[2])
	}
	it.Sockets = []itemtext.Socket{{}}
	inc := []modparser.Mod{
		{Name: "IntRequirement", Type: "INC", Value: -50.0},
		{Name: "IntRequirement", Type: "BASE", Value: 10.0},
	}
	if got, want := itemRequirements(it, inc)[2], math.Floor(float64(base.Req.Int+10)*0.5); got != want {
		t.Errorf("socketed item Int %v, want base %d + 10, halved = %v", got, base.Req.Int, want)
	}
	noReq := []modparser.Mod{{Name: "NoAttributeRequirements", Type: "FLAG", Value: true}}
	if got := itemRequirements(it, noReq); got != (requirements{}) {
		t.Errorf("NoAttributeRequirements: %v", got)
	}
	zero := 0.0
	if got := c.gemRequirements(gemRequirement{override: &zero}); got != (requirements{}) {
		t.Errorf("req override: %v", got)
	}
	// Omniscience: floor(floor(req * mult) / (omni - 1)) into ReqOmni.
	db := NewModDB()
	db.items = map[string]*itemtext.Item{"Body Armour": {Base: base, Req: itemtext.Requirements{Int: 194}}}
	db.Add(Mod{Name: "OmniscienceRequirements", Type: "FLAG", Value: true, Source: "Test"})
	db.NewMod("OmniAttributeRequirements", "INC", 100, "Test")
	c.attributeRequirements(db, &pobcode.Build{})
	if got := db.output["ReqOmni"]; got != 194 {
		t.Errorf("ReqOmni %v, want 194", got)
	}
	if _, ok := db.output["ReqInt"]; ok {
		t.Error("ReqInt set with Omniscience")
	}
}
