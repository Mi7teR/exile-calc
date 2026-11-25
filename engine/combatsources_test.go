package engine

import (
	"math"
	"testing"
)

func TestSourcesMine(t *testing.T) {
	res := calcCorpus(t, "Fk9CO61s3Yaj") // Essence Drain traps
	src := sourcesOf(res.main, res.Stats)
	limit := res.Stats["ActiveTrapLimit"]
	if src.kind != "trap" || src.sustainMul != 1 || limit != 15 || src.burstHits != limit || src.active != limit {
		t.Errorf("trap sources %+v, ActiveTrapLimit %v (PoB 15)", src, limit)
	}
	res = calcCorpus(t, "Mwn6sSyz3fr5") // Spark of the Nova mines
	src = sourcesOf(res.main, res.Stats)
	limit = res.Stats["ActiveMineLimit"]
	if src.kind != "mine" || src.sustainMul != 1 || limit != 20 || src.burstHits != limit {
		t.Errorf("mine sources %+v, ActiveMineLimit %v (PoB 20)", src, limit)
	}
}

func TestSourcesTotem(t *testing.T) {
	res := calcCorpus(t, "fmxcrOoQ46rS") // Flameblast totems
	src := sourcesOf(res.main, res.Stats)
	n := res.Stats["TotemsSummoned"]
	if src.kind != "totem" || n != 6 || src.sustainMul != n || src.burstHits != n {
		t.Errorf("totem sources %+v, TotemsSummoned %v (PoB 6)", src, n)
	}
	s := res.main
	// Hits and stacking DoTs scale with the totems; PoB already counts the
	// totems in the bleed stacks, and ignite is the strongest one only.
	for comp, want := range map[string]float64{"TotalDPS": n, "PoisonDPS": n, "ImpaleDPS": n, "BleedDPS": 1, "IgniteDPS": 1, "TotalDot": 1} {
		if got := src.sustain(comp, s); got != want {
			t.Errorf("sustain(%s) = %v, want %v", comp, got, want)
		}
	}
}

func TestSourcesSustainedCappedByThrowRate(t *testing.T) {
	res := calcCorpus(t, "Mwn6sSyz3fr5") // Spark of the Nova mines, MineThrowCount 5
	out := res.Stats
	src := sourcesOf(res.main, out)
	// PoB's Speed for mines is the laying rate times the mines per throw
	// (skillData.timeOverride = MineLayingTime / MineThrowCount).
	rate := out["MineLayingSpeed"] * out["MineThrowCount"]
	if math.Abs(out["Speed"]-rate) > 1e-9*rate {
		t.Fatalf("Speed %v, MineLayingSpeed x MineThrowCount %v", out["Speed"], rate)
	}
	perCast := out["TotalDPS"] / out["Speed"]
	burst := perCast * src.burstHits
	sustained := out["TotalDPS"] * src.sustain("TotalDPS", res.main)
	if limit := burst * rate / src.burstHits; sustained > limit*(1+1e-9) {
		t.Errorf("sustained %v above the throw rate cap %v", sustained, limit)
	}
}

func TestDotScale(t *testing.T) {
	plain := &activeSkill{data: &skillData{}, mods: NewModDB(), flags: map[string]bool{}}
	crimson := &activeSkill{data: &skillData{}, mods: NewModDB(), flags: map[string]bool{}}
	crimson.mods.Add(Mod{Name: "Condition:HaveCrimsonDance", Type: "FLAG", Value: true})
	igniteStack := &activeSkill{data: &skillData{}, mods: NewModDB(), flags: map[string]bool{"igniteCanStack": true}}
	out := map[string]float64{"BleedStacksMax": 8, "BleedStacks": 1}
	cases := []struct {
		comp string
		hits float64
		s    *activeSkill
		out  map[string]float64
		want float64
	}{
		{"IgniteDPS", 3, plain, nil, 1},
		{"PoisonDPS", 3, plain, nil, 3},
		{"ImpaleDPS", 3, plain, nil, 3},
		{"TotalDot", 3, plain, nil, 1},
		{"BleedDPS", 10, crimson, out, 8},
		{"BleedDPS", 10, plain, map[string]float64{"BleedStacksMax": 1, "BleedStacks": 1}, 1},
		// PoB's BleedDPS already holds min(BleedStacks, max) stacks: four
		// stacks times three hits fill the eight, a factor of two.
		{"BleedDPS", 3, crimson, map[string]float64{"BleedStacksMax": 8, "BleedStacks": 4}, 2},
		// A target the cast misses gets no non-stacking DoT either.
		{"IgniteDPS", 0, plain, nil, 0},
		// Stacking ignite: PoB's value holds min(potential x max, max)
		// stacks; two stacks times three hits fill five, a factor of 2.5.
		{"IgniteDPS", 3, igniteStack, map[string]float64{"IgniteStacksMax": 5, "IgniteStackPotential": 0.4}, 2.5},
		{"IgniteDPS", 1, igniteStack, map[string]float64{"IgniteStacksMax": 5, "IgniteStackPotential": 0.4}, 1},
	}
	for _, c := range cases {
		if got := dotScale(c.comp, c.hits, c.s, c.out); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("dotScale(%s, %v) = %v, want %v", c.comp, c.hits, got, c.want)
		}
	}
	stack := &activeSkill{data: &skillData{}, mods: NewModDB(), flags: map[string]bool{"DotCanStack": true}}
	if got := dotScale("TotalDot", 3, stack, nil); got != 3 {
		t.Errorf("stacking skill DoT x3 = %v, want 3", got)
	}
}
