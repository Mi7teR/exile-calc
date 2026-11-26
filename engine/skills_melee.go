package engine

import "math"

// Melee skill hooks.

func init() {
	registerSkill(
		[]string{"Molten Strike", "Molten Strike of the Zenith"},
		skillHooks{preDamage: moltenStrikePreDamage},
	)
	registerSkill([]string{"Static Strike"}, skillHooks{preDamage: staticStrikePreDamage})
	registerSkill([]string{"Rage Vortex"}, skillHooks{preSkillType: rageVortexPreSkillType})
}

// Data/Skills/act_str.lua Molten Strike preDamageFunc.
func moltenStrikePreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 1 {
		return
	}
	data := s.data
	proj := out["ProjectileCount"]
	inner, outer := out["AreaOfEffectRadiusSecondary"], out["AreaOfEffectRadiusTertiary"]
	overlap := math.Min(1, (enemyRadius(s)+out["AreaOfEffectRadius"]-inner)/(outer-inner))
	out["OverlapChance"] = overlap * 100
	mult := 1.0
	if s.part == 3 || s.part == 5 || s.part == 6 {
		mult = overlap * proj
		if s.part == 6 {
			// Zenith: the weighted average of normal and 5th attack balls.
			fifth := 1 + numOr(data.val("FifthStrikeDamage"), 0)/100
			mult = 0.8*mult + 0.2*fifth*overlap*(proj+numOr(data.val("FifthStrikeProjectiles"), 0))
		}
	}
	if mult != 1 {
		applySkillDPSMultiplier(s, out, mult)
	}
}

// Data/Skills/act_str.lua Static Strike preDamageFunc.
func staticStrikePreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	if s.part == 2 {
		f, _ := num(s.data.val("repeatFrequency"))
		s.data.hitTimeOverride = optF{f / (1 + s.mods.SumCfg(s.cfg, "INC", "StaticStrikeFrequency")/100), true}
	}
}

// Data/Skills/act_str.lua Rage Vortex preSkillTypeFunc.
func rageVortexPreSkillType(_ *ModDB, s *activeSkill) {
	list, cfg, data := s.mods, s.cfg, s.data
	if s.part == 2 {
		maxRage := list.SumCfg(cfg, "BASE", "MaximumRage")
		pct := numOr(data.val("MaxRageVortexSacrificePercentage"), 0) / 100
		override := list.SumCfg(cfg, "BASE", "Multiplier:RageSacrificedStacks")
		maxSac := math.Floor(pct * maxRage)
		stacks := maxSac
		if override > 0 {
			stacks = math.Min(override, maxSac)
		}
		list.NewMod("Multiplier:RageSacrificed", "BASE", stacks, "Skill:RageVortex")
	}
}
