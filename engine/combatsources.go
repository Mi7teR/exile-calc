package engine

import "math"

// sources describes where a skill's casts come from in combat: the player
// directly, or mines, traps, totems or ballistas.
type sources struct {
	kind       string  // "", "mine", "trap", "totem", "ballista"
	active     float64 // ActiveMineLimit / ActiveTrapLimit / TotemsSummoned
	sustainMul float64 // multiplier on per-cast DPS for sustained combat DPS
	burstHits  float64 // casts that go off together in one burst
}

// sourcesOf reads the main skill's source counts from its outputs.
//
// Mines and traps: PoB's Speed is already the throwing or laying rate
// (CalcOffence sets skillData.timeOverride to TrapThrowingTime /
// TrapThrowCount or MineLayingTime / MineThrowCount), so TotalDPS is the
// sustained rate and the active limit only sizes the burst.
//
// Totems and ballistas: PoB's TotalDPS is one totem's (Speed is the totem's
// own cast or attack rate), so the summoned totems multiply it.
func sourcesOf(s *activeSkill, out map[string]float64) sources {
	var src sources
	switch {
	case s.flags["totem"]:
		src.kind = "totem"
		if s.types[stTotemsAreBallistae] {
			src.kind = "ballista"
		}
		// CalcOffence: output.TotemsSummoned = Override or ActiveTotemLimit.
		src.active = out["TotemsSummoned"]
		if src.active <= 0 {
			src.active = out["ActiveTotemLimit"]
		}
		src.sustainMul = src.active
	case s.flags["mine"]:
		src.kind = "mine"
		src.active = out["ActiveMineLimit"]
		src.sustainMul = 1
	case s.flags["trap"]:
		src.kind = "trap"
		src.active = out["ActiveTrapLimit"]
		src.sustainMul = 1
	default:
		src.active, src.sustainMul = 1, 1
	}
	if src.active <= 0 {
		src.active = 1
	}
	if src.sustainMul <= 0 {
		src.sustainMul = 1
	}
	src.burstHits = src.active
	return src
}

// sustain is sustainMul for one DPS component. Hits and DoTs that stack
// take every totem; PoB already multiplies the bleed stacks by the active
// totems (CalcOffence bleed: bleedStacks * activeTotems), and a DoT that
// does not stack keeps only the strongest instance whatever the totems.
func (src sources) sustain(component string, s *activeSkill) float64 {
	if component == "TotalDPS" || stackingDot(component, s) {
		return src.sustainMul
	}
	return 1
}

// stackingDot reports whether every application of a DoT component adds
// an instance: poison, impale, and skill DoT with DotCanStack (PoB's
// TotalDot then already counts applications per second).
func stackingDot(component string, s *activeSkill) bool {
	switch component {
	case "PoisonDPS", "TotalPoisonDPS", "ImpaleDPS":
		return true
	case "TotalDot":
		return s.flags["DotCanStack"]
	}
	return false
}

// dotScale scales each DoT component by hits-per-cast (stacking rules).
//
// Stacking DoTs scale with the hits. Bleed stacks only with Crimson Dance,
// and PoB's BleedDPS already holds min(BleedStacks, BleedStacksMax) stacks,
// so the hits raise it only up to the stack limit; stacking ignite
// (igniteCanStack, PoB's TotalIgniteDPS) the same with IgniteStackPotential
// × IgniteStacksMax stacks. Everything else keeps the strongest instance:
// one hit is enough, fewer than one is the chance the target is hit at all.
func dotScale(component string, hitsPerCast float64, s *activeSkill, out map[string]float64) float64 {
	if stackingDot(component, s) {
		return hitsPerCast
	}
	if maxStacks := out["IgniteStacksMax"]; component == "IgniteDPS" && s.flags["igniteCanStack"] && maxStacks > 0 {
		return stackScale(out["IgniteStackPotential"]*maxStacks, maxStacks, hitsPerCast)
	}
	if component == "BleedDPS" && s.mods.FlagCfg(s.cfg, "Condition:HaveCrimsonDance") {
		return stackScale(out["BleedStacks"], out["BleedStacksMax"], hitsPerCast)
	}
	return math.Min(hitsPerCast, 1)
}

// stackScale raises a DoT holding min(stacks, maxStacks) stacks by the
// hits, up to maxStacks.
func stackScale(stacks, maxStacks, hitsPerCast float64) float64 {
	if stacks <= 0 {
		return math.Min(hitsPerCast, maxStacks)
	}
	return math.Min(stacks*hitsPerCast, maxStacks) / math.Min(stacks, maxStacks)
}
