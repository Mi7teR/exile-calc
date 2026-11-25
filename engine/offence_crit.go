package engine

import "math"

// crit is the crit chance, crit multiplier and crit effect part of
// calcs.offence's damage pass (mode_effective). Parent and party member
// crit links are not ported.
func (c *Calculator) crit(s *activeSkill, p *hitPass, enemy *ModDB, actor *ModDB) {
	list := s.mods
	out, cfg := p.out, p.cfg
	zero := func() {
		out["PreEffectiveCritChance"], out["CritChance"], out["CritMultiplier"], out["BonusCritDotMultiplier"], out["CritEffect"] = 0, 0, 0, 0, 1
	}
	done := false
	switch {
	case list.FlagCfg(cfg, "NeverCrit"):
		zero()
		done = true
	case list.FlagCfg(cfg, "SpellSkillsCannotDealCriticalStrikesExceptOnFinalRepeat"):
		if p.repeats <= 1 {
			zero()
			done = true
		} else if list.FlagCfg(cfg, "SpellSkillsAlwaysDealCriticalStrikesOnFinalRepeat") {
			mode := "AVERAGE"
			if b := actor.theBuild(); b != nil {
				mode = configString(b.ActiveConfig(), "repeatMode", "AVERAGE")
			}
			switch mode {
			case "None":
				out["PreEffectiveCritChance"], out["CritChance"] = 0, 0
			case "AVERAGE":
				out["PreEffectiveCritChance"], out["CritChance"] = 100/p.repeats, 100/p.repeats
			default:
				out["PreEffectiveCritChance"], out["CritChance"] = 100, 100
			}
		}
	default:
		override, hasOverride := list.OverrideCfg(cfg, "CritChance")
		baseCrit := p.src.critChance
		if hasOverride {
			baseCrit = override
		}
		if list.FlagCfg(cfg, "BaseCritFromMainHand") {
			baseCrit = p.mainHandCrit
		}
		if hasOverride && override == 100 {
			out["PreEffectiveCritChance"], out["PreBifurcateCritChance"], out["CritChance"] = 100, 100, 100
		} else {
			var base, inc, more = 0.0, 0.0, 1.0
			if !hasOverride {
				base = list.SumCfg(cfg, "BASE", "CritChance") + enemy.Sum("BASE", "SelfCritChance")
				inc = list.SumCfg(cfg, "INC", "CritChance") + enemy.Sum("INC", "SelfCritChance")
				more = list.MoreCfg(cfg, "CritChance") * enemy.More("SelfCritChance")
			}
			cc := round((baseCrit+base)*(1+inc/100)*more*100, 0) / 100
			capv, ok := list.Override("CritChanceCap")
			if !ok {
				capv = list.SumCfg(cfg, "BASE", "CritChanceCap")
			}
			cc = math.Min(cc, capv)
			if baseCrit+base > 0 {
				cc = math.Max(cc, 0)
			}
			out["PreEffectiveCritChance"] = cc
			rolls := 0.0
			if list.FlagCfg(cfg, "CritChanceLucky") {
				rolls++
			}
			if list.FlagCfg(s.cfg, "ExtremeLuck") {
				rolls *= 2
			}
			if rolls != 0 {
				if actor.Flag("Unexciting") {
					cc = (3*math.Pow(cc/100, 2) - 2*math.Pow(cc/100, 3)) * 100
				} else {
					cc = (1 - math.Pow(1-cc/100, rolls+1)) * 100
				}
			}
			out["PreBifurcateCritChance"] = cc
			if list.FlagCfg(cfg, "BifurcateCrit") {
				cc = (1 - math.Pow(1-cc/100, 2)) * 100
			}
			for _, x := range []struct {
				flag string
				n    float64
			}{{"Every3UseCrit", 3}, {"Every5UseCrit", 5}, {"Every6UseCrit", 6}} {
				if list.FlagCfg(s.cfg, x.flag) {
					cc = ((x.n-1)*cc + 100) / x.n
				}
			}
			out["CritChance"] = cc * out["AccuracyHitChance"] / 100
		}
	}
	if !done {
		if list.FlagCfg(cfg, "NoCritMultiplier") {
			out["CritMultiplier"] = 1
		} else {
			extra := list.SumCfg(cfg, "BASE", "CritMultiplier") / 100
			if o, ok := list.OverrideCfg(s.cfg, "CritMultiplier"); ok {
				extra = (o - 100) / 100
			}
			enemyInc := 1 + enemy.Sum("INC", "SelfCritMultiplier")/100
			extra += enemy.Sum("BASE", "SelfCritMultiplier") / 100
			extra = round(extra*enemyInc, 2)
			override, hasOverride := list.OverrideCfg(cfg, "CritChance")
			if list.FlagCfg(cfg, "BifurcateCrit") && !(hasOverride && override == 100) {
				pre := out["PreBifurcateCritChance"]
				multiChance := pre * pre / 100
				use := 1.0
				for _, x := range []struct {
					flag string
					f    float64
				}{{"Every3UseCrit", 2.0 / 3}, {"Every5UseCrit", 4.0 / 5}, {"Every6UseCrit", 5.0 / 6}} {
					if list.FlagCfg(s.cfg, x.flag) {
						use *= x.f
					}
				}
				multiChance *= use
				cond := 0.0
				if out["CritChance"] > 0 {
					cond = multiChance / out["CritChance"]
				}
				out["CritBifurcates"] = 1 + cond
				extra += cond * extra
				list.Add(Mod{Name: "CritMultiplier", Type: "MORE", Value: floorTo(cond*100, 2), Flags: mfHit, Source: "Bifurcated Crit Damage Bonus"})
			}
			out["CritMultiplier"] = 1 + math.Max(0, extra)
		}
		cc := out["CritChance"] / 100
		out["CritEffect"] = 1 - cc + cc*out["CritMultiplier"]
		out["BonusCritDotMultiplier"] = (list.SumCfg(cfg, "BASE", "CritMultiplier") - 50) * list.SumCfg(cfg, "BASE", "CritMultiplierAppliesToDegen") / 10000
	}
	if out["CritChance"] != 0 {
		list.conditions["CritInPast8Sec"] = true
	}
}

// postCritFunc ports the skills' postCritFunc (Data/Skills/*.lua), run
// after the crit chance of each damage pass. PoB passes the global output
// (runSkillFunc's upvalue); out is the pass's, which is the same for spells.
func postCritFunc(s *activeSkill, out, global map[string]float64) {
	if h := hooksOf(s); h != nil && h.postCrit != nil {
		h.postCrit(s, out, global)
	}
}
