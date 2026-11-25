package engine

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// corpusBuild decodes a golden corpus build.
func corpusBuild(t *testing.T, id string) *pobcode.Build {
	t.Helper()
	raw, err := os.ReadFile("../testdata/golden/builds/" + id + ".code")
	if err != nil {
		t.Fatal(err)
	}
	b, err := pobcode.FromCode(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newCalc(t *testing.T) *Calculator {
	t.Helper()
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func near(got, want, rel float64) bool {
	if want == 0 {
		return math.Abs(got) < 1e-9
	}
	return math.Abs(got-want) <= rel*math.Abs(want)
}

// TestCombatSingleTargetInvariant: where the shape adds nothing (a melee
// strike or a single hit, no sources), combat DPS on the boss is PoB's,
// component by component.
func TestCombatSingleTargetInvariant(t *testing.T) {
	c := newCalc(t)
	paths, _ := filepath.Glob("../testdata/golden/builds/*.code")
	checked := 0
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), ".code")
		b := corpusBuild(t, id)
		res, err := c.Calc(b)
		if err != nil || res.main == nil {
			continue
		}
		sh := shapeOf(res.main, res.Stats)
		if sh.pobOverlap || (sh.kind != meleeStrike && sh.kind != singleHit) || sourcesOf(res.main, res.Stats).kind != "" {
			continue
		}
		if truthy(res.main.data["showAverage"]) {
			continue // CombinedDPS is an average hit, not a rate
		}
		cr, err := c.Combat(b, CombatOptions{})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		out := res.Stats
		mult := combinedMult(res.main, out)
		ignite, ok := out["TotalIgniteDPS"]
		if !ok {
			ignite = out["IgniteDPS"]
		}
		for comp, want := range map[string]float64{"Hit": out["TotalDPS"], "Ignite": ignite, "Poison": out["TotalPoisonDPS"], "Mirage": out["MirageDPS"]} {
			if got := cr.Boss.Components[comp]; !near(got, want*mult, 0.005) {
				t.Errorf("%s (%s): Boss %s = %v, PoB %v x %v", id, cr.Skill, comp, got, want, mult)
			}
		}
		if !near(cr.Boss.Sustained, out["CombinedDPS"], 0.005) {
			t.Errorf("%s (%s): Boss.Sustained %v, PoB CombinedDPS %v (%v)", id, cr.Skill, cr.Boss.Sustained, out["CombinedDPS"], cr.Boss.Components)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no single-target build in the corpus")
	}
	t.Logf("%d single-target builds checked", checked)
}

// TestCombatNoDoubleOverlap: PoB's dpsMultiplier already counts the overlap
// of a pobOverlap shape, so geometry adds nothing on the boss.
func TestCombatNoDoubleOverlap(t *testing.T) {
	c := newCalc(t)
	paths, _ := filepath.Glob("../testdata/golden/builds/*.code")
	checked := 0
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), ".code")
		b := corpusBuild(t, id)
		res, err := c.Calc(b)
		if err != nil || res.main == nil || !shapeOf(res.main, res.Stats).pobOverlap {
			continue
		}
		cr, err := c.Combat(b, CombatOptions{})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		want := res.Stats["TotalDPS"] * sourcesOf(res.main, res.Stats).sustain("TotalDPS", res.main) * combinedMult(res.main, res.Stats)
		if cr.Boss.HitsPerCast != 1 || !near(cr.Boss.Components["Hit"], want, 0.005) {
			t.Errorf("%s (%s): HitsPerCast %v, Hit %v, PoB TotalDPS x sources %v", id, cr.Skill, cr.Boss.HitsPerCast, cr.Boss.Components["Hit"], want)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no pobOverlap build in the corpus")
	}
	t.Logf("%d pobOverlap builds checked", checked)
}

func TestCombatNoMainSkill(t *testing.T) {
	b := corpusBuild(t, "hlasMvXnooQc")
	b.SkillSets = nil
	cr, err := newCalc(t).Combat(b, CombatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cr.Boss.Sustained != 0 || cr.Pack.Sustained != 0 || cr.Boss.Burst != 0 || cr.Skill != "" {
		t.Errorf("no main skill: %+v", cr)
	}
	found := false
	for _, a := range cr.Assumptions {
		found = found || strings.Contains(a, "главного скилла")
	}
	if !found {
		t.Errorf("no assumption about the missing main skill: %q", cr.Assumptions)
	}
}

func TestCombatDoesNotMutateBuild(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "hlasMvXnooQc")
	before, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	cfg := b.ActiveConfig()
	inputs := len(cfg.Inputs)
	if _, err := c.Combat(b, CombatOptions{}); err != nil {
		t.Fatal(err)
	}
	after, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.ActiveConfig().Inputs) != inputs {
		t.Errorf("config inputs %d, were %d", len(b.ActiveConfig().Inputs), inputs)
	}
	if !reflect.DeepEqual(before.Stats, after.Stats) {
		for k, v := range before.Stats {
			if after.Stats[k] != v {
				t.Errorf("%s: %v before Combat, %v after", k, v, after.Stats[k])
			}
		}
	}
}

func TestCombatKineticBlast(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "hlasMvXnooQc")
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	cr, err := c.Combat(b, CombatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pob := res.Stats["TotalDPS"]
	t.Logf("%s: PoB TotalDPS %.0f; Boss %+v; Pack %+v", cr.Skill, pob, cr.Boss, cr.Pack)
	found := false
	for _, a := range cr.Assumptions {
		t.Log(a)
		found = found || strings.Contains(a, "по боссу попадают")
	}
	if !found {
		t.Error("no assumption on the explosions that hit the boss")
	}
	if cr.Boss.Sustained <= 2*pob {
		t.Errorf("Boss.Sustained %v, want above 2 x PoB TotalDPS %v", cr.Boss.Sustained, pob)
	}
	if cr.Pack.Sustained <= cr.Boss.Sustained {
		t.Errorf("Pack.Sustained %v, want above Boss.Sustained %v", cr.Pack.Sustained, cr.Boss.Sustained)
	}
}

func TestCombatDeterministic(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "hlasMvXnooQc")
	opts := CombatOptions{Boss: BossLarge, Pack: PackParams{Count: 6}, Seed: 7}
	x, err := c.Combat(b, opts)
	if err != nil {
		t.Fatal(err)
	}
	y, err := c.Combat(b, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(x, y) {
		t.Errorf("two runs differ:\n%+v\n%+v", x, y)
	}
}
