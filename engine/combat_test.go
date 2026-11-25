package engine

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// corpusBuild decodes a golden corpus build.
func corpusBuild(t testing.TB, id string) *pobcode.Build {
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

func newCalc(t testing.TB) *Calculator {
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

// corpusCombat runs Calc and Combat on every golden corpus build with a
// main skill and calls f; a Calc or Combat error fails the test.
func corpusCombat(t *testing.T, f func(id string, res *Result, cr *CombatResult)) {
	t.Helper()
	c := newCalc(t)
	paths, _ := filepath.Glob("../testdata/golden/builds/*.code")
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), ".code")
		b := corpusBuild(t, id)
		res, err := c.Calc(b)
		if err != nil {
			t.Fatalf("%s: Calc: %v", id, err)
		}
		if res.main == nil {
			continue
		}
		cr, err := c.Combat(b, CombatOptions{})
		if err != nil {
			t.Fatalf("%s: Combat: %v", id, err)
		}
		f(id, res, cr)
	}
}

// TestCombatSingleTargetInvariant: with no sources, where one use lands one
// hit on the boss (any shape), combat DPS on the boss is PoB's CombinedDPS,
// and so are its hit parts (the DoT parts share PoB's DoT cap, so only
// their sum is checked, through Sustained).
func TestCombatSingleTargetInvariant(t *testing.T) {
	checked := 0
	corpusCombat(t, func(id string, res *Result, cr *CombatResult) {
		if sourcesOf(res.main, res.Stats).kind != "" || cr.Boss.HitsPerCast != 1 {
			return
		}
		if truthy(res.main.data["showAverage"]) {
			return // CombinedDPS is an average hit, not a rate
		}
		out := res.Stats
		mult := combinedMult(res.main, out)
		for comp, want := range map[string]float64{"Hit": out["TotalDPS"], "Mirage": out["MirageDPS"], "Impale": out["ImpaleDPS"]} {
			if got := cr.Boss.Components[comp]; !near(got, want*mult, 0.005) {
				t.Errorf("%s (%s): Boss %s = %v, PoB %v x %v", id, cr.Skill, comp, got, want, mult)
			}
		}
		// Contagion's own DoT is another skill's damage, on top of PoB's.
		if !near(cr.Boss.Sustained-cr.Boss.Components["Contagion"], out["CombinedDPS"], 0.005) {
			t.Errorf("%s (%s): Boss.Sustained %v, PoB CombinedDPS %v (%v)", id, cr.Skill, cr.Boss.Sustained, out["CombinedDPS"], cr.Boss.Components)
		}
		checked++
	})
	if checked == 0 {
		t.Fatal("no single-target build in the corpus")
	}
	t.Logf("%d one-hit builds checked", checked)
}

// TestCombatPackSanity: a skill with no sources that hits the boss at least
// once per use deals at least about PoB's single-target DPS to the pack
// (the boss hits, not the pack's, select: a collapsed shape hits the pack
// less than once). Flags a
// shape that collapses on the pack (area radius 0, explosions as rays).
// pobOverlap shapes are included: the most-hit mob keeps PoB's
// single-target overlap, so they must not fall below PoB either.
func TestCombatPackSanity(t *testing.T) {
	checked := 0
	corpusCombat(t, func(id string, res *Result, cr *CombatResult) {
		if sourcesOf(res.main, res.Stats).kind != "" || cr.Boss.HitsPerCast < 1 {
			return
		}
		if cr.Pack.Sustained < 0.9*cr.Pack.PoB {
			t.Errorf("%s (%s): Pack.Sustained %v < 0.9 x PoB %v (hits %.2f)", id, cr.Skill, cr.Pack.Sustained, cr.Pack.PoB, cr.Pack.HitsPerCast)
		}
		checked++
	})
	t.Logf("%d builds checked", checked)
}

// TestCombatNoDoubleOverlap: PoB's dpsMultiplier already counts the overlap
// of a pobOverlap shape, so geometry adds nothing on the boss.
func TestCombatNoDoubleOverlap(t *testing.T) {
	checked := 0
	corpusCombat(t, func(id string, res *Result, cr *CombatResult) {
		if !shapeOf(res.main, res.Stats).pobOverlap {
			return
		}
		want := res.Stats["TotalDPS"] * sourcesOf(res.main, res.Stats).sustain("TotalDPS", res.main) * combinedMult(res.main, res.Stats)
		if cr.Boss.HitsPerCast != 1 || !near(cr.Boss.Components["Hit"], want, 0.005) {
			t.Errorf("%s (%s): HitsPerCast %v, Hit %v, PoB TotalDPS x sources %v", id, cr.Skill, cr.Boss.HitsPerCast, cr.Boss.Components["Hit"], want)
		}
		checked++
	})
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
	if diff := cmp.Diff(before.Stats, after.Stats); diff != "" {
		t.Errorf("Stats changed by Combat (-before +after):\n%s", diff)
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
	if diff := cmp.Diff(x, y); diff != "" {
		t.Errorf("two runs differ (-first +second):\n%s", diff)
	}
}

// assumed reports whether one of the assumptions contains sub.
func assumed(cr *CombatResult, sub string) bool {
	for _, a := range cr.Assumptions {
		if strings.Contains(a, sub) {
			return true
		}
	}
	return false
}

func TestCombatZeroDPSSkills(t *testing.T) {
	c := newCalc(t)
	for id, note := range map[string]string{
		"rfm48XKZzz4v": "главный скилл — миньоны", // Summon Skeletons
		"LuQygQN4SdUj": "CombinedDPS PoB = 0",     // Purity of Fire
	} {
		cr, err := c.Combat(corpusBuild(t, id), CombatOptions{})
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if cr.Boss.Sustained != 0 || cr.Pack.Sustained != 0 || !assumed(cr, note) {
			t.Errorf("%s (%s): boss %v, pack %v, assumptions %q; want 0 and %q", id, cr.Skill, cr.Boss.Sustained, cr.Pack.Sustained, cr.Assumptions, note)
		}
	}
}

func TestCombatAreaWithoutRadius(t *testing.T) {
	// Moonfall: AreaOfEffectRadius 0, counted as a single hit, not an area
	// that touches nothing.
	cr, err := newCalc(t).Combat(corpusBuild(t, "OmGuvxN4Nw7A"), CombatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cr.Pack.HitsPerCast < 1 || !assumed(cr, "радиус области неизвестен") {
		t.Errorf("%s: pack hits %v, assumptions %q", cr.Skill, cr.Pack.HitsPerCast, cr.Assumptions)
	}
}

func TestCombatNoProjectileShotgun(t *testing.T) {
	// Rolling Magma with GMP: one hit on the boss per use.
	cr, err := newCalc(t).Combat(corpusBuild(t, "8YoGqmeyF3HU"), CombatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cr.Boss.HitsPerCast != 1 {
		t.Errorf("%s: boss hits %v, want 1", cr.Skill, cr.Boss.HitsPerCast)
	}
}

// TestCombatBurst: the burst is one use's hit damage times the hits and the
// sources, with culling and reservation like Sustained.
func TestCombatBurst(t *testing.T) {
	checked := 0
	corpusCombat(t, func(id string, res *Result, cr *CombatResult) {
		src := sourcesOf(res.main, res.Stats)
		rate := hitSpeed(res.Stats)
		if src.kind == "" || rate <= 0 || shapeOf(res.main, res.Stats).pobOverlap {
			return
		}
		mult := combinedMult(res.main, res.Stats)
		want := res.Stats["TotalDPS"] / rate * cr.Boss.HitsPerCast * src.burstHits * mult
		if dot, dur := math.Min(res.Stats["TotalDot"], dotDpsCap), res.Stats["Duration"]; dot > 0 && dur > 0 {
			n := math.Min(cr.Boss.HitsPerCast, 1) // one target, one debuff
			if stackingDot("TotalDot", res.main) {
				n = cr.Boss.HitsPerCast * src.burstHits
			}
			want += dot * dur * n * mult
		}
		if !near(cr.Boss.Burst, want, 1e-9) {
			t.Errorf("%s (%s): Boss.Burst %v, want %v", id, cr.Skill, cr.Boss.Burst, want)
		}
		checked++
	})
	if checked == 0 {
		t.Fatal("no build with sources")
	}
}

func TestCombatOptionsAndPerMob(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "hlasMvXnooQc")
	def, err := c.Combat(b, CombatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	odd, err := c.Combat(b, CombatOptions{Boss: BossSize(9), Pack: PackParams{Count: 500}})
	if err != nil {
		t.Fatal(err)
	}
	if odd.Boss.Sustained != def.Boss.Sustained || !assumed(odd, "неизвестный размер босса") || !assumed(odd, "ограничено 50") {
		t.Errorf("unknown boss size: %v (medium %v), assumptions %q", odd.Boss.Sustained, def.Boss.Sustained, odd.Assumptions)
	}
	for _, r := range []struct {
		cr *CombatResult
		n  float64
	}{{def, defaultPackCount}, {odd, maxPackCount}} {
		pm := r.cr.PackPerMob
		if !near(pm.Sustained, r.cr.Pack.Sustained/r.n, 1e-12) || !near(pm.Burst, r.cr.Pack.Burst/r.n, 1e-12) || pm.PoB != r.cr.Pack.PoB {
			t.Errorf("PackPerMob %+v, Pack %+v / %v", pm, r.cr.Pack, r.n)
		}
	}
	if !assumed(def, "в среднем на моба = сумма / 10") {
		t.Errorf("no per-mob assumption: %q", def.Assumptions)
	}
}

// TestCombatNoAreaNoExplosion: Essence Drain has a radius in its data but no
// area flag, so its projectile does not explode on the pack.
func TestCombatNoAreaNoExplosion(t *testing.T) {
	c := newCalc(t)
	cr, err := c.Combat(corpusBuild(t, "Fk9CO61s3Yaj"), CombatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cr.Pack.HitsPerCast > 1.0001 {
		t.Errorf("%s: pack hits per cast %.2f, want <= 1 (no explosion)", cr.Skill, cr.Pack.HitsPerCast)
	}
}

// TestCombatContagionSpread: Contagion spreads Essence Drain's debuff over
// the pack, so the pack's skill DoT is several mobs' worth; without the
// spread it is one mob's (the projectile hits one target).
func TestCombatContagionSpread(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "maxrollEDBigStaff")
	cr, err := c.Combat(b, CombatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	boss, pack := cr.Boss.Components["Dot"], cr.Pack.Components["Dot"]
	if boss <= 0 || pack < 5*boss {
		t.Errorf("%s: pack Dot %v, want >= 5 x boss Dot %v (Contagion spread)", cr.Skill, pack, boss)
	}
	found := false
	for _, a := range cr.Assumptions {
		if strings.Contains(a, "разносит DoT") {
			found = true
		}
	}
	if !found {
		t.Errorf("no Contagion spread assumption in %q", cr.Assumptions)
	}
	if sp := c.contagionOf(b); sp.radius <= 0 || !strings.HasPrefix(sp.name, "Contagion") {
		t.Errorf("contagionOf = %+v, want a Contagion with a radius", sp)
	}
}
