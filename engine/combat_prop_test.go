package engine

import (
	"flag"
	"testing"

	"github.com/google/go-cmp/cmp"
	"pgregory.net/rapid"
)

// Property tests for the combat DPS geometry and DoT scaling. The ones that
// run Calc/Combat on corpus builds are slow (up to a second per Combat), so
// they draw from a small fixed sample of the corpus and limit the checks.

// limitChecks lowers rapid's check count to n for the test unless the run
// asked for a count with -rapid.checks.
func limitChecks(t *testing.T, n string) {
	t.Helper()
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	set := false
	flag.Visit(func(v *flag.Flag) { set = set || v.Name == "rapid.checks" })
	if set {
		return
	}
	old := f.Value.String()
	if err := f.Value.Set(n); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Value.Set(old) })
}

// TestPropHitsBounded: one cast lands at most projectiles x sub-hits hits on
// any target. Every kind caps a target below that: a projectile marks a
// target it hit (one hit per projectile, without shotgun one per cast), sub-hit
// areas add at most their count (fractions sum to subHits), and the other
// kinds hit a target once. pobOverlap caps a target at one hit.
func TestPropHitsBounded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		sh := skillShape{
			// selfArea..singleHit are the contiguous deliveryKind values.
			kind:       deliveryKind(rapid.IntRange(0, int(singleHit)).Draw(rt, "kind")),
			subHits:    float64(rapid.IntRange(0, 10).Draw(rt, "subHits2")) / 2, // 0..5 in halves
			subRadius:  rapid.Float64Range(0.5, 40).Draw(rt, "subRadius"),
			scatter:    rapid.Float64Range(0, 60).Draw(rt, "scatter"),
			spreadDeg:  rapid.Float64Range(0, 90).Draw(rt, "spreadDeg"),
			canShotgun: rapid.Bool().Draw(rt, "shotgun"),
			pobOverlap: rapid.Bool().Draw(rt, "pobOverlap"),
		}
		projectiles := rapid.IntRange(1, 20).Draw(rt, "projectiles")
		pierce := rapid.IntRange(0, 4).Draw(rt, "pierce")
		chain := rapid.IntRange(0, 4).Draw(rt, "chain")
		fork := rapid.IntRange(0, 3).Draw(rt, "fork")
		radius := rapid.Float64Range(0.5, 80).Draw(rt, "radius")
		strikes := rapid.IntRange(1, 5).Draw(rt, "strikes")
		seed := rapid.Uint64().Draw(rt, "seed")
		var m targetModel
		if rapid.Bool().Draw(rt, "pack") {
			m = packModel(PackParams{
				Count:     rapid.IntRange(1, 20).Draw(rt, "count"),
				Radius:    rapid.Float64Range(1, 100).Draw(rt, "packRadius"),
				MobRadius: rapid.Float64Range(1, 30).Draw(rt, "mobRadius"),
				Distance:  rapid.Float64Range(1, 150).Draw(rt, "distance"),
			})
		} else {
			m = bossModel(rapid.SampledFrom([]BossSize{BossSmall, BossMedium, BossLarge}).Draw(rt, "boss"))
		}

		bound := float64(projectiles) * max(1, sh.subHits)
		if sh.pobOverlap {
			bound = 1
		}
		hits := expectedHits(
			sh, float64(projectiles), float64(pierce), float64(chain), float64(fork),
			radius, float64(strikes), m, seed,
		)
		if len(hits) != len(m.targets) {
			rt.Fatalf("%d results for %d targets", len(hits), len(m.targets))
		}
		for i, h := range hits {
			if h < 0 || h > bound*(1+1e-9) {
				rt.Fatalf(
					"target %d: %v hits per cast, want within [0, %v] (%+v, projectiles %d)",
					i, h, bound, sh, projectiles,
				)
			}
		}
	})
}

// propCorpusIDs is a fixed spread sample of the corpus for the slow properties.
func propCorpusIDs(t *testing.T) []string {
	t.Helper()
	all := corpusIDs(t)
	var ids []string
	for i := 0; i < len(all); i += 8 {
		ids = append(ids, all[i])
	}
	return ids
}

// TestPropPackVsPoB: for builds the combat model treats as single-target
// PoB-comparable (no source multiplier, at least one hit on the boss per use;
// the preconditions of TestCombatPackSanity) the pack run deals at least 0.9
// x PoB's CombinedDPS: the most-hit mob keeps PoB's single-target value.
func TestPropPackVsPoB(t *testing.T) {
	limitChecks(t, "8")
	c := newCalc(t)
	ids := propCorpusIDs(t)
	rapid.Check(t, func(rt *rapid.T) {
		id := rapid.SampledFrom(ids).Draw(rt, "build")
		opts := CombatOptions{Seed: rapid.Uint64().Draw(rt, "seed")}
		b := corpusBuild(t, id)
		res, err := c.Calc(b)
		if err != nil {
			rt.Fatal(err)
		}
		if res.main == nil || sourcesOf(res.main, res.Stats).kind != "" {
			rt.Skip("no main skill, or a source skill")
		}
		cr, err := c.Combat(b, opts)
		if err != nil {
			rt.Fatal(err)
		}
		if cr.Boss.HitsPerCast < 1 {
			rt.Skip("boss hit less than once per use")
		}
		if cr.Pack.Sustained < 0.9*cr.Pack.PoB {
			rt.Fatalf(
				"%s (%s) seed %d: Pack.Sustained %v < 0.9 x PoB %v",
				id, cr.Skill, opts.Seed, cr.Pack.Sustained, cr.Pack.PoB,
			)
		}
	})
}

// TestPropSeedDeterministic: the same build and options give the same
// result, whatever the seed, boss size and pack.
func TestPropSeedDeterministic(t *testing.T) {
	limitChecks(t, "6")
	c := newCalc(t)
	ids := propCorpusIDs(t)
	rapid.Check(t, func(rt *rapid.T) {
		id := rapid.SampledFrom(ids).Draw(rt, "build")
		opts := CombatOptions{
			Boss: rapid.SampledFrom([]BossSize{BossSmall, BossMedium, BossLarge}).Draw(rt, "boss"),
			Pack: PackParams{Count: rapid.IntRange(0, 20).Draw(rt, "count")},
			Seed: rapid.Uint64().Draw(rt, "seed"),
		}
		b := corpusBuild(t, id)
		x, err := c.Combat(b, opts)
		if err != nil {
			rt.Fatal(err)
		}
		y, err := c.Combat(b, opts)
		if err != nil {
			rt.Fatal(err)
		}
		if diff := cmp.Diff(x, y); diff != "" {
			rt.Fatalf("%s %+v: two runs differ (-first +second):\n%s", id, opts, diff)
		}
	})
}

// TestPropDotScaleMonotonic: more hits never lower a DoT's scale, and the
// scale respects the component's limit: stacking DoTs have none (the scale
// is the hits), stacking ignite and Crimson Dance bleed cap at their stack
// maximum (scale x the stacks PoB already holds <= max), the rest keep one
// instance (scale <= 1).
func TestPropDotScaleMonotonic(t *testing.T) {
	plain := &activeSkill{data: &skillData{}, mods: NewModDB(), flags: map[string]bool{}}
	crimson := &activeSkill{data: &skillData{}, mods: NewModDB(), flags: map[string]bool{}}
	crimson.mods.Add(Mod{Name: "Condition:HaveCrimsonDance", Type: "FLAG", Value: true})
	igniteStack := &activeSkill{data: &skillData{}, mods: NewModDB(), flags: map[string]bool{"igniteCanStack": true}}
	dotStack := &activeSkill{data: &skillData{}, mods: NewModDB(), flags: map[string]bool{"DotCanStack": true}}
	skills := []*activeSkill{plain, crimson, igniteStack, dotStack}
	comps := []string{
		"Hit", "PoisonDPS", "TotalPoisonDPS", "ImpaleDPS", "TotalDot",
		"IgniteDPS", "BleedDPS", "GroundDPS", "DecayDPS",
	}
	rapid.Check(t, func(rt *rapid.T) {
		s := rapid.SampledFrom(skills).Draw(rt, "skill")
		comp := rapid.SampledFrom(comps).Draw(rt, "component")
		// Stack maxima are at least 1 (a limit of 0 is not a PoB output).
		out := map[string]float64{
			"BleedStacksMax":       rapid.Float64Range(1, 20).Draw(rt, "bleedMax"),
			"BleedStacks":          rapid.Float64Range(0, 25).Draw(rt, "bleedStacks"),
			"IgniteStacksMax":      rapid.Float64Range(1, 20).Draw(rt, "igniteMax"),
			"IgniteStackPotential": rapid.Float64Range(0, 3).Draw(rt, "ignitePotential"),
		}
		lo := rapid.Float64Range(0, 30).Draw(rt, "hits")
		hi := lo + rapid.Float64Range(0, 30).Draw(rt, "extraHits")
		a, b := dotScale(comp, lo, s, out), dotScale(comp, hi, s, out)
		if a < 0 || b < a-1e-12 {
			rt.Fatalf("%s: scale(%v) = %v, scale(%v) = %v: not monotonic", comp, lo, a, hi, b)
		}
		switch {
		case stackingDot(comp, s):
			if b != hi {
				rt.Fatalf("%s: stacking scale %v, want the hits %v", comp, b, hi)
			}
		case comp == "IgniteDPS" && s.flags["igniteCanStack"]:
			maxStacks := out["IgniteStacksMax"]
			if held := min(out["IgniteStackPotential"]*maxStacks, maxStacks); b*held > maxStacks*(1+1e-9) {
				rt.Fatalf("ignite: scale %v x %v held stacks above the %v limit", b, held, maxStacks)
			}
		case comp == "BleedDPS" && s == crimson:
			maxStacks := out["BleedStacksMax"]
			if held := min(out["BleedStacks"], maxStacks); b*held > maxStacks*(1+1e-9) {
				rt.Fatalf("bleed: scale %v x %v held stacks above the %v limit", b, held, maxStacks)
			}
		default:
			if b > 1 {
				rt.Fatalf("%s: scale %v above 1 for a non-stacking DoT", comp, b)
			}
		}
	})
}
