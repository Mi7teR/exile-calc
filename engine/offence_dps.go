package engine

import "math"

// skillDot is "Calculate skill DOT components": the skill's own damage
// over time (TotalDotInstance, TotalDot). preDotFunc is not ported.
func skillDot(actor *ModDB, s *activeSkill, out map[string]float64, dpsMult, quantity float64) {
	list, data, enemy, sc := s.mods, &s.data, actor.enemy, s.cfg
	flags := sc.Flags | mfDot
	for _, x := range []struct {
		flag uint64
		key  string
	}{{mfArea, "dotIsArea"}, {mfProjectile, "dotIsProjectile"}, {mfSpell, "dotIsSpell"}, {mfAttack, "dotIsAttack"}, {mfHit, "dotIsHit"}} {
		if !truthy(data.val(x.key)) {
			flags &^= x.flag
		}
	}
	base := *sc
	base.Flags, base.KeywordFlags = flags, sc.KeywordFlags&^kfHit
	taken := base
	if truthy(data.val("dotIsSpell")) {
		taken.Flags &^= mfSpell
	}
	out["TotalDotInstance"] = 0
	for _, t := range dmgTypeList {
		tc := base
		tc.KeywordFlags |= kfDot[t]
		baseVal := 0.0
		if canDeal(s, t) {
			baseVal, _ = num(data.val(t + "Dot"))
		}
		if !(baseVal > 0 || out[t+"Dot"] > 0) {
			continue
		}
		s.flags["dot"] = true
		names := []string{"DamageTaken", "DamageTakenOverTime", t + "DamageTaken", t + "DamageTakenOverTime"}
		inc := enemy.SumCfg(&taken, "INC", names...)
		more := enemy.MoreCfg(&taken, names...)
		if isElemental[t] {
			inc += enemy.SumCfg(&taken, "INC", "ElementalDamageTaken")
			more *= enemy.MoreCfg(&taken, "ElementalDamageTaken")
		}
		var resist float64
		if t == "Physical" {
			resist = math.Max(0, math.Min(enemy.Sum("BASE", "PhysicalDamageReduction"), enemyPhysicalDamageReductionCap))
		} else {
			resist = enemyResist(actor, &tc, t)
		}
		effMult := (1 - resist/100) * (1 + inc/100) * more
		out[t+"DotEffMult"] = effMult
		dmgNames := []string{"Damage", t + "Damage"}
		if isElemental[t] {
			dmgNames = append(dmgNames, "ElementalDamage")
		}
		dinc := list.SumCfg(&tc, "INC", dmgNames...)
		if list.Flag("dotIsHeraldOfAsh") {
			dinc = math.Max(dinc-list.SumCfg(sc, "INC", dmgNames...), 0)
		}
		dmore := list.MoreCfg(&tc, dmgNames...)
		mult, ok := list.OverrideCfg(&tc, "DotMultiplier")
		if !ok {
			mult = list.SumCfg(&tc, "BASE", "DotMultiplier") + list.SumCfg(&tc, "BASE", t+"DotMultiplier")
		}
		aura := 1.0
		if s.types[stAura] && !s.types[stRemoteMined] && !s.types[stBanner] {
			aura = calcMod(list, &tc, "AuraEffect")
		}
		total := baseVal * (1 + dinc/100) * dmore * (1 + mult/100) * aura * effMult
		if out[t+"Dot"] == 0 {
			out[t+"Dot"] = total
			out["TotalDotInstance"] = math.Min(out["TotalDotInstance"]+total, dotDpsCap)
		} else {
			out["TotalDotInstance"] = math.Min(out["TotalDotInstance"]+total+out[t+"Dot"], dotDpsCap)
		}
	}
	switch {
	case list.Flag("DotCanStack"):
		s.flags["DotCanStack"] = true
		speed := out["Speed"]
		if base.KeywordFlags&kfMine != 0 {
			speed = out["MineLayingSpeed"]
		} else if base.KeywordFlags&kfTrap != 0 {
			speed = out["TrapThrowingSpeed"]
		}
		out["TotalDot"] = math.Min(out["TotalDotInstance"]*speed*out["Duration"]*dpsMult*quantity, dotDpsCap)
		out["TotalDotCalcSection"] = out["TotalDot"]
	case list.Flag("dotIsBurningGround"):
		out["TotalDot"] = 0
		out["TotalDotCalcSection"] = out["TotalDotInstance"]
		if v, ok := out["BurningGroundDPS"]; !ok || v < out["TotalDotInstance"] {
			out["BurningGroundDPS"] = math.Max(v, out["TotalDotInstance"])
			out["BurningGroundFromIgnite"] = 0
		}
	case list.Flag("dotIsCausticGround"):
		out["TotalDot"] = 0
		out["TotalDotCalcSection"] = out["TotalDotInstance"]
		if v, ok := out["CausticGroundDPS"]; !ok || v < out["TotalDotInstance"] {
			out["CausticGroundDPS"] = math.Max(v, out["TotalDotInstance"])
			out["CausticGroundFromPoison"] = 0
		}
	case list.Flag("dotIsCorruptingBlood"):
		out["TotalDot"] = 0
		out["TotalDotCalcSection"] = out["TotalDotInstance"]
		if v, ok := out["CorruptingBloodDPS"]; !ok || v < out["TotalDotInstance"] {
			out["CorruptingBloodDPS"] = math.Max(v, out["TotalDotInstance"])
		}
	default:
		if list.Flag("DotCanStackAsTotems") && s.flags["totem"] {
			s.flags["DotCanStack"] = true
		}
		brands := 1.0
		if s.types[stBrand] && !truthy(data.val("countsAttachedBrandsInDamage")) {
			if v, ok := out["AttachedBrandCount"]; ok {
				brands = v
			}
		}
		out["TotalDot"] = out["TotalDotInstance"]
		if brands > 1 {
			out["TotalDot"] = math.Min(out["TotalDotInstance"]*brands, dotDpsCap)
		}
		out["TotalDotCalcSection"] = out["TotalDot"]
	}
}

// decay is Essence of Delirium's Decay (CalcOffence "Calculate DPS for
// Essence of Delirium's Decay effect"): skillData.decay chaos DoT per second.
func decay(actor *ModDB, s *activeSkill, out map[string]float64) {
	base, ok := num(s.data.val("decay"))
	if !ok || !s.flags["hit"] || !canDeal(s, "Chaos") {
		return
	}
	s.flags["decay"] = true
	list, enemy := s.mods, actor.enemy
	dotCfg := *s.cfg
	dotCfg.Flags = mfDot
	dotCfg.KeywordFlags = s.cfg.KeywordFlags&^kfHit | kfDot["Chaos"]
	names := []string{"DamageTaken", "DamageTakenOverTime", "ChaosDamageTaken", "ChaosDamageTakenOverTime"}
	effMult := (1 - enemyResist(actor, &dotCfg, "Chaos")/100) * (1 + enemy.Sum("INC", names...)/100) * enemy.More(names...)
	out["DecayEffMult"] = effMult
	inc := list.SumCfg(&dotCfg, "INC", "Damage", "ChaosDamage")
	more := list.MoreCfg(&dotCfg, "Damage", "ChaosDamage")
	mult, ok := list.OverrideCfg(&dotCfg, "DotMultiplier")
	if !ok {
		mult = list.SumCfg(&dotCfg, "BASE", "DotMultiplier") + list.SumCfg(&dotCfg, "BASE", "ChaosDotMultiplier")
	}
	out["DecayDPS"] = base * (1 + inc/100) * more * (1 + mult/100) * effMult
	out["DecayDuration"] = 8 / math.Max(buffExpirationSlowCap, calcMod(enemy, s.cfg, "BuffExpireFaster"))
}

// cullAndReservation is the culling and reservation DPS part of the damage
// pass (CullMultiplier, ReservationDpsMultiplier).
func cullAndReservation(actor *ModDB, s *activeSkill, p *hitPass, global map[string]float64, dpsMult float64) {
	list, cfg, out := s.mods, p.cfg, p.out
	speed := global["Speed"]
	if v, ok := global["HitSpeed"]; ok {
		speed = v
	}
	hitRate := out["HitChance"] / 100 * speed * dpsMult
	critical, _ := list.MaxCfg(cfg, "CriticalCullPercent")
	if critical > 0 {
		critical = math.Min(critical, critical*(1-math.Pow(1-out["CritChance"]/100, hitRate)))
	}
	regular, _ := list.MaxCfg(cfg, "CullPercent")
	global["CullPercent"] = math.Max(critical, regular)
	global["CullMultiplier"] = 100 / (100 - global["CullPercent"])
	global["ReservationDpsMultiplier"] = 100 / (100 - actor.enemy.Sum("BASE", "LifeReservationPercent"))
}

// combinedDPS is "Calculate combined DPS estimate, including DoTs".
func combinedDPS(s *activeSkill, out map[string]float64, passes []*hitPass, dpsMult, quantity float64) {
	baseDPS := out["TotalDPS"]
	showAverage := truthy(s.data.val("showAverage"))
	if showAverage {
		baseDPS = out["AverageDamage"]
	}
	out["CombinedDPS"], out["CombinedAvg"] = baseDPS, baseDPS
	if s.flags["dot"] {
		out["WithDotDPS"] = baseDPS + out["TotalDot"]
	}
	if quantity > 1 {
		if v, ok := out["TotalPoisonDPS"]; ok {
			out["TotalPoisonDPS"] = math.Min(v*quantity, dotDpsCap)
		}
	}
	if showAverage {
		out["WithPoisonDPS"] = baseDPS + out["TotalPoisonAverageDamage"]
	} else {
		out["WithPoisonDPS"] = baseDPS + out["TotalPoisonDPS"]
	}
	out["WithIgniteDPS"] = baseDPS
	if s.flags["ignite"] {
		switch {
		case s.flags["igniteCanStack"]:
			if showAverage {
				out["CombinedAvg"] = out["CombinedDPS"] + out["IgniteDamage"]
			} else {
				out["WithIgniteDPS"] = baseDPS + out["TotalIgniteDPS"]
			}
		case showAverage:
			out["WithIgniteDPS"] = baseDPS + out["IgniteDamage"]
			out["CombinedAvg"] += out["IgniteDamage"]
		default:
			out["WithIgniteDPS"] = baseDPS + out["IgniteDPS"]
		}
	}
	if s.flags["impale"] {
		impaleDPS := func(o map[string]float64) float64 {
			mod, ok := o["ImpaleModifier"]
			if !ok {
				mod = 1
			}
			return o["impaleStoredHitAvg"] * (mod - 1) * o["HitChance"] / 100 * dpsMult
		}
		if s.flags["attack"] && truthy(s.data.val("doubleHitsWhenDualWielding")) && len(passes) == 2 {
			out["ImpaleDPS"] = impaleDPS(passes[0].out) + impaleDPS(passes[1].out)
		} else {
			out["ImpaleDPS"] = impaleDPS(out)
		}
		if out["ImpaleDuration"] <= 0 {
			out["ImpaleDPS"] = 0
		}
		if showAverage {
			out["WithImpaleDPS"] = out["AverageDamage"] + out["ImpaleDPS"]
			out["CombinedAvg"] += out["ImpaleDPS"]
		} else {
			out["ImpaleDPS"] *= hitSpeed(out)
			out["WithImpaleDPS"] = out["TotalDPS"] + out["ImpaleDPS"]
		}
		if quantity > 1 {
			out["ImpaleDPS"] *= quantity
		}
		out["CombinedDPS"] += out["ImpaleDPS"]
	}
	out["WithBleedDPS"] = baseDPS
	if s.flags["bleed"] {
		if showAverage {
			out["WithBleedDPS"] = baseDPS + out["BleedDamage"]
			out["CombinedAvg"] += out["BleedDamage"]
		} else {
			out["WithBleedDPS"] = baseDPS + out["BleedDPS"]
		}
	}
	bestCull := addMirageDPS(s, out)
	ignite, ok := out["TotalIgniteDPS"]
	if !ok {
		ignite = out["IgniteDPS"]
	}
	total := out["TotalDot"] + out["TotalPoisonDPS"] + math.Max(out["CausticGroundDPS"], out["MirageCausticGroundDPS"]) + ignite +
		math.Max(out["BurningGroundDPS"], out["MirageBurningGroundDPS"]) +
		out["BleedDPS"] + out["CorruptingBloodDPS"] + out["DecayDPS"]
	out["TotalDotDPS"] = math.Min(total, dotDpsCap)
	if !showAverage {
		out["CombinedDPS"] += out["TotalDotDPS"]
	}
	cull := math.Max(bestCull, out["CullMultiplier"])
	resMult, ok := out["ReservationDpsMultiplier"]
	if !ok {
		resMult = 1
	}
	out["CullingDPS"] = out["CombinedDPS"] * (cull - 1)
	out["ReservationDPS"] = out["CombinedDPS"] * (resMult - 1)
	out["CombinedDPS"] *= cull * resMult
}
