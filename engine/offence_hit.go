package engine

import "math"

// calcDamage is CalcOffence.lua calcDamage.
func calcDamage(list *ModDB, p *hitPass, t string, typeFlags int, convDst bool) (float64, float64) {
	typeFlags |= dmgTypeFlags[t]
	var addMin, addMax float64
	for _, o := range dmgTypeList {
		if o == t {
			break
		}
		if m := p.conv[o].to[t]; m > 0 {
			mn, mx := calcDamage(list, p, o, typeFlags, true)
			addMin += mn * m
			addMax += mx * m
		}
	}
	b := p.base[t]
	if b[0] == 0 && b[1] == 0 {
		if convDst {
			return addMin, addMax
		}
		return round(addMin, 0), round(addMax, 0)
	}
	names := damageStatsForTypes(typeFlags)
	inc := 1 + list.SumCfg(p.cfg, "INC", names...)/100
	more := list.MoreCfg(p.cfg, names...)
	if debugCalcDamage != nil {
		debugCalcDamage(list, p, t, inc, more, names)
	}
	mn := ((b[0]*inc*more)*list.MoreCfg(p.cfg, "MinDamage") + addMin) *
		list.MoreCfg(p.cfg, "Min"+t+"Damage") * (1 + list.SumCfg(p.cfg, "INC", "Min"+t+"Damage")/100)
	mx := ((b[1]*inc*more)*list.MoreCfg(p.cfg, "MaxDamage") + addMax) *
		list.MoreCfg(p.cfg, "Max"+t+"Damage") * (1 + list.SumCfg(p.cfg, "INC", "Max"+t+"Damage")/100)
	if convDst {
		return mn, mx
	}
	return round(mn, 0), round(mx, 0)
}

// calcResistForType is CalcOffence.lua calcResistForType without the
// "resist equal to yours" mods.
func calcResistForType(enemy *ModDB, cfg *Cfg, t string, configMax float64) float64 {
	if r, ok := enemy.OverrideCfg(cfg, t+"Resist"); ok {
		return math.Max(math.Min(r, enemyMaxResistFor(configMax)), resistFloor)
	}
	names := []string{t + "Resist"}
	if isElemental[t] {
		names = append(names, "ElementalResist")
	}
	r := enemy.SumCfg(cfg, "BASE", names...) * math.Max(calcMod(enemy, cfg, names...), 0)
	return math.Max(math.Min(r, enemyMaxResistFor(configMax)), resistFloor)
}

// enemyResist is calcs.offence's calcResistForType: the enemy's resist of
// type t, or the actor's own with "resist equal to yours".
func enemyResist(actor *ModDB, cfg *Cfg, t string) float64 {
	enemy := actor.enemy
	cm := 0.0
	if actor.enemyEnv != nil {
		cm, _ = actor.enemyEnv.num("enemy" + t + "Resist")
	}
	if _, ok := enemy.OverrideCfg(cfg, t+"Resist"); !ok && actor.Flag("Enemy"+t+"ResistEqualToYours") {
		return math.Max(math.Min(actor.output[t+"Resist"], enemyMaxResistFor(cm)), resistFloor)
	}
	return calcResistForType(enemy, cfg, t, cm)
}

func enemyMaxResistFor(configMax float64) float64 {
	if configMax == 0 {
		return enemyMaxResist
	}
	return math.Min(math.Max(configMax, enemyMaxResist), enemyResistCap)
}

// hitDamage is the crit and hit damage part of calcs.offence for one pass:
// AverageHit and AverageDamage.
func (c *Calculator) hitDamage(actor *ModDB, s *activeSkill, p *hitPass) {
	list, cfg, data, out := s.mods, s.cfg, s.data, p.out
	enemy := actor.enemy
	ge := s.active.ge

	c.crit(s, p, enemy, actor)

	// Double and triple damage. PoB differs: its
	// Sum(TripleDamageChance) or 0 + self + onCrit parses as
	// Sum or (0 + ...), so it drops the enemy's SelfTripleDamageChance and
	// triple damage on crit; both count here.
	triple := math.Min(list.SumCfg(p.cfg, "BASE", "TripleDamageChance")+enemy.SumCfg(p.cfg, "BASE", "SelfTripleDamageChance")+
		math.Min(list.SumCfg(p.cfg, "BASE", "TripleDamageChanceOnCrit"), 100)*out["CritChance"]/100, 100)
	double := math.Min(list.SumCfg(p.cfg, "BASE", "DoubleDamageChance")+enemy.SumCfg(p.cfg, "BASE", "SelfDoubleDamageChance")+
		math.Min(list.SumCfg(p.cfg, "BASE", "DoubleDamageChanceOnCrit"), 100)*out["CritChance"]/100, 100)
	if r, ok := p.global["IntimidatingUpTimeRatio"]; ok {
		if list.Flag("Condition:WarcryMaxHit") {
			double = 100
		} else {
			double = math.Min(double+r, 100)
		}
	}
	if triple > 0 {
		double = math.Max(double-triple*double/100, 0)
	}
	out["DoubleDamageChance"], out["TripleDamageChance"] = double, triple
	scaled := 1 + double/100 + 2*triple/100
	warcry := p.global["OffensiveWarcryEffect"]
	if list.Flag("Condition:WarcryMaxHit") {
		warcry = p.global["MaxOffensiveWarcryEffect"]
	}
	if warcry == 0 {
		warcry = 1
	}
	scaled *= out["RuthlessBlowHitEffect"] * out["FistOfWarDamageEffect"] * warcry
	postCritFunc(s, out, p.global)
	redirectAddedDamage(list, p.cfg)

	// Base hit damage.
	baseMult, eff := 1.0, 1.0
	if lv := ge.Levels[s.active.level]; lv != nil && lv.BaseMultiplier != 0 {
		baseMult = lv.BaseMultiplier
	} else if v, ok := num(data.val("baseMultiplier")); ok {
		baseMult = v
	}
	if lv := ge.Levels[s.active.level]; lv != nil && lv.DamageEffectiveness != 0 {
		eff = lv.DamageEffectiveness
	} else if v, ok := num(data.val("damageEffectiveness")); ok {
		eff = v
	}
	p.base = map[string][2]float64{}
	for _, t := range dmgTypeList {
		addedMin := list.SumCfg(p.cfg, "BASE", t+"Min") + enemy.SumCfg(p.cfg, "BASE", "Self"+t+"Min")
		addedMax := list.SumCfg(p.cfg, "BASE", t+"Max") + enemy.SumCfg(p.cfg, "BASE", "Self"+t+"Max")
		addedMult := calcMod(list, p.cfg, "Added"+t+"Damage", "AddedDamage")
		p.base[t] = [2]float64{
			p.src.min[t]*baseMult + addedMin*eff*addedMult,
			p.src.max[t]*baseMult + addedMax*eff*addedMult,
		}
		out[t+"MinBase"], out[t+"MaxBase"] = p.base[t][0], p.base[t][1]
	}

	// Hit damage per type: pass 1 crits, pass 2 non-crits.
	var totalHitAvg, totalCritAvg float64
	out["impaleStoredHitAvg"] = 0
	skillCond := p.cfg.SkillCond
	for pass := 1; pass <= 2; pass++ {
		cond := map[string]bool{}
		for k, v := range skillCond {
			cond[k] = v
		}
		cond["CriticalStrike"] = pass == 1
		p.cfg.SkillCond = cond
		for _, t := range dmgTypeList {
			if !s.flags["hit"] || list.FlagCfg(cfg, "DealNo"+t, "DealNoDamage") {
				continue
			}
			mn, mx := calcDamage(list, p, t, 0, false)
			allMult := p.conv[t].mult * scaled
			if pass == 2 {
				out["allMult"] = allMult
			}
			if pass == 1 {
				allMult *= out["CritMultiplier"]
			}
			mn, mx = mn*allMult, mx*allMult
			var lucky float64
			ele := t == "Lightning" || t == "Cold" || t == "Fire"
			if list.FlagCfg(cfg, "LuckyHits") ||
				(pass == 2 && t == "Lightning" && list.FlagCfg(cfg, "LightningNoCritLucky")) ||
				(pass == 1 && list.FlagCfg(cfg, "CritLucky")) ||
				(t == "Lightning" && actor.Flag("LightningLuckHits")) ||
				(t == "Chaos" && actor.Flag("ChaosLuckyHits")) ||
				(t == "Fire" && actor.Flag("FireLuckyHits")) ||
				(t == "Cold" && actor.Flag("ColdLuckyHits")) ||
				(ele && list.FlagCfg(cfg, "ElementalLuckHits")) {
				lucky = 1
			} else {
				lucky = math.Min(list.SumCfg(cfg, "BASE", "LuckyHitsChance"), 100) / 100
			}
			if list.FlagCfg(cfg, "UnluckyHits") {
				lucky--
			}
			rolls := 3.0
			if list.FlagCfg(cfg, "ExtremeLuck") {
				rolls = 4
			}
			if list.FlagCfg(cfg, "Unexciting") {
				rolls, lucky = 2, 0
			}
			avg := (mn + mx) / 2
			if lucky >= 0 {
				avg = avg*(1-lucky) + (mn+(rolls-1)*mx)/rolls*lucky
			} else {
				avg = avg*(1+lucky) + ((rolls-1)*mn+mx)/rolls*(-lucky)
			}
			if mn != 0 || mx != 0 {
				if t == "Physical" {
					// Pre-armour physical damage for impale.
					if pass == 1 {
						out["impaleStoredHitAvg"] += avg * out["CritChance"] / 100
					} else {
						out["impaleStoredHitAvg"] += avg * (1 - out["CritChance"]/100)
					}
				}
				effMult := c.enemyEffMult(actor, list, p.cfg, s, t, avg)
				if pass == 2 {
					out[t+"EffMult"] = effMult
				}
				avg *= effMult
			}
			if pass == 1 {
				totalCritAvg += avg
				out[t+"CritAverage"] = avg
			} else {
				totalHitAvg += avg
				out[t+"HitAverage"] = avg
			}
		}
	}
	p.cfg.SkillCond = skillCond

	out["AverageHit"] = totalHitAvg*(1-out["CritChance"]/100) + totalCritAvg*out["CritChance"]/100
	if s.flags["monsterExplode"] && truthy(s.data.val("hitChanceIsExplodeChance")) {
		out["HitChance"] = out["ExplodeChance"]
	}
	out["AverageDamage"] = out["AverageHit"] * out["HitChance"] / 100
}

// enemyEffMult is the effective damage multiplier of a hit of type t on
// the enemy: damage taken, resistance and penetration, armour.
func (c *Calculator) enemyEffMult(actor, list *ModDB, cfg *Cfg, s *activeSkill, t string, avg float64) float64 {
	enemy := actor.enemy
	takenInc := enemy.SumCfg(cfg, "INC", "DamageTaken", t+"DamageTaken")
	takenMore := enemy.MoreCfg(cfg, "DamageTaken", t+"DamageTaken")
	var useResChance float64
	resNames := []string{"Ignore" + t + "Resistance"}
	chanceNames := []string{"ChanceToIgnore" + t + "Resistance"}
	if isElemental[t] {
		resNames = append(resNames, "IgnoreElementalResistances")
		chanceNames = append(chanceNames, "ChanceToIgnoreElementalResistances")
	}
	if list.FlagCfg(cfg, resNames...) || enemy.Flag("SelfIgnore"+t+"Resistance") {
		useResChance = 0
	} else {
		useResChance = 1 - math.Min(math.Max(list.SumCfg(cfg, "BASE", chanceNames...), 0), 100)/100
	}
	var resist, pen float64
	if t == "Physical" {
		armour := math.Max(val(enemy, "Armour"), 0)
		red := armourReductionF(armour, avg*list.MoreCfg(cfg, "CalcArmourAsThoughDealing"))
		ignore := math.Min(list.SumCfg(cfg, "BASE", "ChanceToIgnoreEnemyPhysicalDamageReduction"), 100)
		if list.FlagCfg(cfg, "IgnoreEnemyPhysicalDamageReduction") || ignore >= 100 {
			resist = 0
		} else {
			resist = math.Min(math.Max(0, enemy.Sum("BASE", "PhysicalDamageReduction")+list.SumCfg(cfg, "BASE", "EnemyPhysicalDamageReduction")+red), enemyPhysicalDamageReductionCap)
			if resist > 0 {
				resist *= 1 - (list.Sum("BASE", "PartialIgnoreEnemyPhysicalDamageReduction")/100 + ignore/100)
			}
		}
	} else {
		resistOf := func(t string) float64 { return enemyResist(actor, cfg, t) }
		resist = resistOf(t)
		ignores := func(e string) bool {
			names := []string{"Ignore" + e + "Resistance"}
			if isElemental[e] {
				names = append(names, "IgnoreElementalResistances")
			}
			if list.FlagCfg(cfg, names...) || enemy.Flag("SelfIgnore"+e+"Resistance") {
				return true
			}
			ch := []string{"ChanceToIgnore" + e + "Resistance"}
			if isElemental[e] {
				ch = append(ch, "ChanceToIgnoreElementalResistances")
			}
			return math.Min(math.Max(list.SumCfg(cfg, "BASE", ch...), 0), 100) >= 100
		}
		lowestChaos := t == "Chaos" && (list.FlagCfg(cfg, "ChaosDamageUsesLowestResistance") || list.FlagCfg(cfg, "ChaosDamageUsesHighestResistance"))
		if lowestChaos || (isElemental[t] && list.FlagCfg(cfg, "ElementalDamageUsesLowestResistance")) {
			used := t
			if isElemental[t] {
				takenInc += enemy.SumCfg(cfg, "INC", "ElementalDamageTaken")
			}
			highest := list.FlagCfg(cfg, "ChaosDamageUsesHighestResistance")
			for _, e := range dmgTypeList {
				if !isElemental[e] || e == t || ignores(e) {
					continue
				}
				r := resistOf(e)
				if (highest && resist < r) || (!highest && resist > r) {
					resist, used = r, e
				}
			}
			if isElemental[used] {
				pen = list.SumCfg(cfg, "BASE", used+"Penetration", "ElementalPenetration")
			} else if used == "Chaos" {
				pen = list.SumCfg(cfg, "BASE", "ChaosPenetration")
			}
		} else {
			switch {
			case isElemental[t]:
				pen = list.SumCfg(cfg, "BASE", t+"Penetration", "ElementalPenetration")
				takenInc += enemy.SumCfg(cfg, "INC", "ElementalDamageTaken")
			case t == "Chaos":
				pen = list.SumCfg(cfg, "BASE", "ChaosPenetration")
			}
		}
	}
	invert := 0.0
	if isElemental[t] {
		invert = math.Max(math.Min(list.SumCfg(cfg, "CHANCE", "HitsInvertEleResChance", "HitsInvert"+t+"ResChance"), 1), 0)
	} else if t == "Chaos" {
		invert = math.Max(math.Min(list.SumCfg(cfg, "CHANCE", "HitsInvertChaosResChance"), 1), 0)
	}
	if s.flags["projectile"] {
		takenInc += enemy.Sum("INC", "ProjectileDamageTaken")
		if s.flags["attack"] {
			takenInc += enemy.Sum("INC", "ProjectileAttackDamageTaken")
		}
	}
	if s.flags["trap"] || s.flags["mine"] {
		takenInc += enemy.Sum("INC", "TrapMineDamageTaken")
	}
	eff := (1 + takenInc/100) * takenMore
	switch {
	case isElemental[t] && list.FlagCfg(cfg, "CannotElePenIgnore"):
		r := resist
		if invert > 0 {
			r = resist - 2*invert*resist
		}
		eff *= 1 - r/100
	case useResChance > 0:
		r := (resist - pen) * useResChance
		if invert > 0 {
			r = ((resist-pen)*(1-invert) + (-resist-pen)*invert) * useResChance
		}
		eff *= 1 - r/100
	}
	return eff
}
