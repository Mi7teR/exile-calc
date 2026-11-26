package engine

import "math"

// ignite is the "Calculate ignite chance and damage" part of the ailment
// loop for one pass.
func ignite(actor *ModDB, s *activeSkill, p *hitPass, global map[string]float64, dpsMult float64) {
	list, cfg, out, enemy := s.mods, p.cfg, p.out, actor.enemy
	if !canDeal(s, "Fire") || out["IgniteChanceOnHit"]+out["IgniteChanceOnCrit"] <= 0 {
		return
	}
	dotCfg := ailmentDotCfg(s, p, kfIgnite|kfDot["Fire"])
	critChance := out["CritChance"]
	global["IgniteChancePerHit"] = out["IgniteChanceOnHit"]*(1-critChance/100) + out["IgniteChanceOnCrit"]*critChance/100
	maxStacks := 1.0
	if s.flags["igniteCanStack"] {
		if v, ok := list.OverrideCfg(cfg, "IgniteStacks"); ok {
			maxStacks = v
		} else {
			maxStacks += list.SumCfg(cfg, "BASE", "IgniteStacks")
		}
	}
	global["IgniteStacksMax"] = maxStacks
	rateMod := (calcMod(list, cfg, "IgniteBurnFaster") + enemy.Sum("INC", "SelfIgniteBurnFaster")/100) / calcMod(list, cfg, "IgniteBurnSlower")
	durationBase := float64(igniteDurationBase)
	if v, ok := list.OverrideCfg(dotCfg, "IgniteDurationBase"); ok {
		durationBase = v
	}
	durationMod := math.Max(calcMod(list, dotCfg, "EnemyIgniteDuration", "EnemyAilmentDuration", "EnemyElementalAilmentDuration", "DamagingAilmentDuration")*
		calcMod(enemy, nil, "SelfIgniteDuration", "SelfAilmentDuration", "SelfElementalAilmentDuration"), 0)
	debuffDurationMult := 1 / math.Max(buffExpirationSlowCap, calcMod(enemy, s.cfg, "BuffExpireFaster"))
	global["IgniteDuration"] = durationBase * durationMod / rateMod * debuffDurationMult
	chance := out["IgniteChanceOnHit"]/100*(1-critChance/100) + out["IgniteChanceOnCrit"]/100*critChance/100
	stacks := out["HitChance"] / 100 * chance * dpsMult
	if !truthy(s.data.val("triggeredOnDeath")) {
		speed := global["Speed"]
		if v, ok := global["HitSpeed"]; ok {
			speed = v
		}
		if cd, ok := out["Cooldown"]; ok {
			t, hasHitTime := out["HitTime"]
			if !hasHitTime {
				t = out["Time"]
			}
			stacks *= global["IgniteDuration"] / math.Max(cd, t)
		} else if speed > 0 {
			stacks *= global["IgniteDuration"] * speed
		}
	}
	if v, ok := list.OverrideCfg(nil, "IgniteStackPotentialOverride"); ok {
		global["IgniteStackPotential"] = v / maxStacks
	} else {
		global["IgniteStackPotential"] = stacks / maxStacks
	}
	rollAvg := 50.0
	if global["IgniteStackPotential"] > 1 {
		rollAvg = (stacks - (maxStacks-1)/2) / (stacks + 1) * 100
	}
	global["IgniteRollAverage"] = rollAvg

	dotMulti := func() float64 {
		if v, ok := list.OverrideCfg(dotCfg, "DotMultiplier"); ok {
			return 1 + v/100
		}
		return 1 + (list.SumCfg(dotCfg, "BASE", "DotMultiplier")+list.SumCfg(dotCfg, "BASE", "FireDotMultiplier"))/100
	}
	var hitDmg, critDmg, maxCritDmg, minHitDmg, maxHitDmg float64
	for sub := 1; sub <= 2; sub++ {
		dotCfg.SkillCond["CriticalStrike"] = sub != 1
		var totalMin, totalMax float64
		add := func(t string, ok bool, typeFlags int) {
			if !ok || !canDeal(s, t) {
				return
			}
			mn, mx := ailmentSourceDamage(list, p, dotCfg, t, typeFlags)
			out["Ignite"+t+"Min"], out["Ignite"+t+"Max"] = mn, mx
			totalMin += mn
			totalMax += mx
		}
		fire := dmgTypeFlags["Fire"]
		add("Physical", list.FlagCfg(cfg, "PhysicalCanIgnite"), fire)
		add("Lightning", list.FlagCfg(cfg, "LightningCanIgnite"), fire)
		add("Cold", list.FlagCfg(cfg, "ColdCanIgnite"), fire)
		add("Fire", !list.FlagCfg(cfg, "FireCannotIgnite"), 0)
		add("Chaos", list.FlagCfg(cfg, "ChaosCanIgnite"), fire)
		if sub == 2 {
			if actor.Flag("AilmentsAreNeverFromCrit") {
				dotCfg.SkillCond["CriticalStrike"] = false
				out["CritIgniteDotMulti"] = dotMulti()
				dotCfg.SkillCond["CriticalStrike"] = true
			} else {
				out["CritIgniteDotMulti"] = dotMulti()
			}
			critDmg = (totalMin + (totalMax-totalMin)*rollAvg/100) * out["CritIgniteDotMulti"]
			maxCritDmg = totalMax * out["CritIgniteDotMulti"]
		} else {
			out["IgniteDotMulti"] = dotMulti()
			hitDmg = (totalMin + (totalMax-totalMin)*rollAvg/100) * out["IgniteDotMulti"]
			minHitDmg = totalMin * out["IgniteDotMulti"]
			maxHitDmg = totalMax * out["IgniteDotMulti"]
		}
		out["IgniteTotalMin"], out["IgniteTotalMax"] = totalMin, totalMax
	}
	ailmentCrit := 100 * (1 - math.Pow(1-critChance/100, math.Max(1, stacks)))
	effects := out["RuthlessBlowAilmentEffect"] * out["FistOfWarDamageEffect"] * global["AilmentWarcryEffect"]
	ailmentDamage(out, "Ignite", ailmentCrit, minHitDmg, 0)
	ailmentDamage(out, "Ignite", 100, maxHitDmg, maxCritDmg)
	baseVal := ailmentDamage(out, "Ignite", ailmentCrit, hitDmg, critDmg) * ignitePercentBase * effects
	if !(baseVal > 0) {
		return
	}
	s.flags["ignite"] = true
	fireEff := func() float64 {
		r := enemyResist(actor, dotCfg, "Fire")
		inc := enemy.SumCfg(dotCfg, "INC", "DamageTaken", "DamageTakenOverTime", "FireDamageTaken", "FireDamageTakenOverTime", "ElementalDamageTaken")
		more := enemy.MoreCfg(dotCfg, "DamageTaken", "DamageTakenOverTime", "FireDamageTaken", "FireDamageTakenOverTime", "ElementalDamageTaken")
		return (1 - r/100) * (1 + inc/100) * more
	}
	var effMult float64
	if list.FlagCfg(cfg, "IgniteToChaos") {
		r := enemyResist(actor, dotCfg, "Chaos")
		inc := enemy.SumCfg(dotCfg, "INC", "DamageTaken", "DamageTakenOverTime", "ChaosDamageTaken", "ChaosDamageTakenOverTime")
		more := enemy.MoreCfg(dotCfg, "DamageTaken", "DamageTakenOverTime", "ChaosDamageTaken", "ChaosDamageTakenOverTime")
		effMult = (1 - r/100) * (1 + inc/100) * more
	} else {
		effMult = fireEff()
	}
	global["IgniteEffMult"] = effMult
	if debugAilmentCfg != nil {
		debugAilmentCfg(actor, s, dotCfg)
	}
	effectMod := calcMod(list, dotCfg, "AilmentEffect")
	active := math.Min(stacks, maxStacks)
	out["IgniteDPS"] = math.Min(baseVal*effectMod*rateMod*active*effMult, dotDpsCap)
	g1, _ := list.MaxCfg(nil, "IgniteDpsAsBurningGround")
	g2, _ := enemy.MaxCfg(nil, "IgniteDpsAsBurningGround")
	if ground := math.Max(g1, g2); ground > 0 {
		global["BurningGroundDPS"] = math.Min(baseVal*effectMod*rateMod*fireEff()*ground/100, dotDpsCap)
		global["BurningGroundFromIgnite"] = 1
	}
	global["IgniteDamage"] = out["IgniteDPS"] * global["IgniteDuration"]
	if s.flags["igniteCanStack"] {
		out["IgniteDamage"] = out["IgniteDPS"] * global["IgniteDuration"]
		out["IgniteStacksMax"] = maxStacks
		out["TotalIgniteDPS"] = out["IgniteDPS"]
	}
	critDotMulti(actor, out, "Ignite", global["CritChance"], ailmentCrit)
}

// bleed is the "Calculate bleeding chance and damage" part of the ailment
// loop for one pass.
func bleed(actor *ModDB, s *activeSkill, p *hitPass, global map[string]float64, dpsMult float64) {
	list, cfg, out, enemy, data := s.mods, p.cfg, p.out, actor.enemy, s.data
	if !canDeal(s, "Physical") || out["BleedChanceOnHit"]+out["BleedChanceOnCrit"] <= 0 {
		return
	}
	dotCfg := ailmentDotCfg(s, p, kfBleed|kfDot["Physical"])
	configStacks := enemy.Sum("BASE", "Multiplier:BleedStacks")
	maxStacks, ok := list.OverrideCfg(cfg, "BleedStacksMax")
	if !ok {
		maxStacks = list.SumCfg(cfg, "BASE", "BleedStacksMax")
	}
	override, hasOverride := list.OverrideCfg(nil, "BleedStackPotentialOverride")
	if hasOverride {
		override /= maxStacks
	}
	global["BleedStacksMax"] = maxStacks
	durationBase := float64(bleedDurationBase)
	if v, isOverride := list.OverrideCfg(dotCfg, "BleedDurationBase"); isOverride {
		durationBase = v
	} else if d, hasDuration := data.duration.get(); hasDuration && truthy(data.val("bleedDurationIsSkillDuration")) {
		durationBase = d
	}
	durNames := []string{"EnemyBleedDuration", "EnemyAilmentDuration", "DamagingAilmentDuration"}
	if truthy(data.val("bleedIsSkillEffect")) {
		durNames = append(durNames, "Duration")
	}
	durationMod := math.Max(calcMod(list, dotCfg, durNames...)*calcMod(enemy, nil, "SelfBleedDuration", "SelfAilmentDuration")/calcMod(enemy, dotCfg, "BleedExpireRate"), 0)
	rateMod := calcMod(list, cfg, "BleedFaster") + enemy.Sum("INC", "SelfBleedFaster")/100
	debuffDurationMult := 1 / math.Max(buffExpirationSlowCap, calcMod(enemy, s.cfg, "BuffExpireFaster"))
	global["BleedDuration"] = durationBase * durationMod / rateMod * debuffDurationMult
	critChance := out["CritChance"]
	chance := out["BleedChanceOnHit"]/100*(1-critChance/100) + out["BleedChanceOnCrit"]/100*critChance/100
	stacks := out["HitChance"] / 100 * chance * dpsMult
	if speed := hitSpeed(global); speed > 0 {
		stacks *= global["BleedDuration"] * speed
	}
	if s.flags["totem"] {
		totems, isOverride := actor.Override("TotemsSummoned")
		if !isOverride {
			totems = list.SumCfg(s.cfg, "BASE", "ActiveTotemLimit", "ActiveBallistaLimit")
		}
		stacks *= totems
	}
	if configStacks > 0 {
		stacks = configStacks
	}
	if stacks < 1 && (!hasOverride || override <= 1) {
		list.Add(Mod{Name: "Condition:SingleBleed", Type: "FLAG", Value: true, Source: "bleed"})
	}
	if hasOverride {
		global["BleedStackPotential"] = override
	} else {
		global["BleedStackPotential"] = stacks / maxStacks
	}
	rollAvg := 50.0
	if global["BleedStackPotential"] > 1 {
		rollAvg = (stacks - (maxStacks-1)/2) / (stacks + 1) * 100
	}
	global["BleedRollAverage"] = rollAvg
	var avgCrit, maxCrit, avgHit, minHit, maxHit float64
	for sub := 1; sub <= 2; sub++ {
		dotCfg.SkillCond["CriticalStrike"] = sub != 1
		mn, mx := ailmentSourceDamage(list, p, dotCfg, "Physical", 0)
		out["BleedPhysicalMin"], out["BleedPhysicalMax"] = mn, mx
		if sub == 2 {
			if actor.Flag("AilmentsAreNeverFromCrit") {
				dotCfg.SkillCond["CriticalStrike"] = false
				out["CritBleedDotMulti"] = dotMultiFor(list, dotCfg, "Physical")
				dotCfg.SkillCond["CriticalStrike"] = true
			} else {
				out["CritBleedDotMulti"] = dotMultiFor(list, dotCfg, "Physical")
			}
			minCrit := mn * out["CritBleedDotMulti"]
			maxCrit = mx * out["CritBleedDotMulti"]
			avgCrit = minCrit + (maxCrit-minCrit)*rollAvg/100
		} else {
			out["BleedDotMulti"] = dotMultiFor(list, dotCfg, "Physical")
			minHit = mn * out["BleedDotMulti"]
			maxHit = mx * out["BleedDotMulti"]
			avgHit = minHit + (maxHit-minHit)*rollAvg/100
		}
	}
	basePercent := numOr(data.val("bleedBasePercent"), bleedPercentBase)
	ailmentCrit := 100 * (1 - math.Pow(1-critChance/100, math.Max(global["BleedStackPotential"], 1)))
	effects := out["RuthlessBlowAilmentEffect"] * out["FistOfWarDamageEffect"] * global["AilmentWarcryEffect"]
	ailmentDamage(out, "Bleed", ailmentCrit, minHit, 0)
	ailmentDamage(out, "Bleed", 100, maxHit, maxCrit)
	baseDps := ailmentDamage(out, "Bleed", ailmentCrit, avgHit, avgCrit) * basePercent / 100 * effects
	if !(baseDps > 0) {
		return
	}
	s.flags["bleed"] = true
	effMult := dotTakenMult(actor, dotCfg, "Physical")
	global["BleedEffMult"] = effMult
	effectMod := calcMod(list, dotCfg, "AilmentEffect")
	out["BaseBleedDPS"] = baseDps * effectMod * rateMod * math.Min(stacks, maxStacks) * effMult
	out["BleedDPS"] = math.Min(out["BaseBleedDPS"], dotDpsCap)
	global["BleedStacks"] = stacks
	global["BleedDamage"] = out["BaseBleedDPS"] * global["BleedDuration"]
	critDotMulti(actor, out, "Bleed", global["CritChance"], ailmentCrit)
}

// poison is the "Calculate poison chance and damage" part of the ailment
// loop for one pass.
func poison(actor *ModDB, s *activeSkill, p *hitPass, global map[string]float64, dpsMult, quantity float64) {
	list, cfg, out, enemy, data := s.mods, p.cfg, p.out, actor.enemy, s.data
	if !canDeal(s, "Chaos") || out["PoisonChanceOnHit"]+out["PoisonChanceOnCrit"]+out["ChaosPoisonChance"] <= 0 {
		return
	}
	dotCfg := ailmentDotCfg(s, p, kfPoison|kfDot["Chaos"])
	rateMod := calcMod(list, cfg, "PoisonFaster") + enemy.Sum("INC", "SelfPoisonFaster")/100
	durationBase := float64(poisonDurationBase)
	if v, ok := list.OverrideCfg(dotCfg, "PoisonDurationBase"); ok {
		durationBase = v
	} else if d, hasDuration := data.duration.get(); hasDuration && truthy(data.val("poisonDurationIsSkillDuration")) {
		durationBase = d
	}
	durNames := []string{"EnemyPoisonDuration", "EnemyAilmentDuration", "DamagingAilmentDuration"}
	if truthy(data.val("poisonIsSkillEffect")) {
		durNames = append(durNames, "Duration")
	}
	durationMod := math.Max(calcMod(list, dotCfg, durNames...)*calcMod(enemy, nil, "SelfPoisonDuration", "SelfAilmentDuration"), 0)
	debuffDurationMult := 1 / math.Max(buffExpirationSlowCap, calcMod(enemy, s.cfg, "BuffExpireFaster"))
	global["PoisonDuration"] = durationBase * durationMod / rateMod * debuffDurationMult
	critChance := out["CritChance"]
	chaosChance := 0.0
	if out["ChaosHitAverage"] > 0 {
		chaosChance = out["ChaosPoisonChance"]
	}
	onHit := math.Min(100, out["PoisonChanceOnHit"]+chaosChance)
	onCrit := math.Min(100, out["PoisonChanceOnCrit"]+chaosChance)
	chance := onHit/100*(1-critChance/100) + onCrit/100*critChance/100
	additional := 1.0
	if !list.Flag("CannotMultiplePoison") {
		additional = 1 + math.Min(list.SumCfg(cfg, "BASE", "AdditionalPoisonChance")/100, 1) + list.SumCfg(cfg, "BASE", "AdditionalPoisonStacks")
	}
	limit, hasLimit := minCfg(list, cfg, "PoisonStackLimit")
	stacks := out["HitChance"] / 100 * chance * additional * dpsMult * numOr(data.val("stackMultiplier"), 1) * quantity
	if speed := hitSpeed(global); speed > 0 {
		stacks *= global["PoisonDuration"] * speed
		if hasLimit && limit > 0 && stacks > limit {
			stacks = math.Min(stacks, math.Ceil(limit/additional)*additional)
		}
	}
	if stacks < additional && configNum(actor, "multiplierPoisonOnEnemy") == 0 {
		list.Add(Mod{Name: "Condition:NonPoisonedOnly", Type: "FLAG", Value: true, Source: "Calculation"})
	}
	var hitDmg, critDmg, maxCritDmg, minHitDmg, maxHitDmg float64
	for sub := 1; sub <= 2; sub++ {
		dotCfg.SkillCond["CriticalStrike"] = sub != 1
		totalMin, totalMax := ailmentSourceDamage(list, p, dotCfg, "Chaos", 0)
		out["PoisonChaosMin"], out["PoisonChaosMax"] = totalMin, totalMax
		nonChaosMult := 1.0
		if out["ChaosPoisonChance"] > 0 && out["PoisonChaosMax"] > 0 {
			key := "PoisonChanceOnHit"
			if sub == 2 {
				key = "PoisonChanceOnCrit"
			}
			cc := math.Min(100, out[key]+out["ChaosPoisonChance"])
			nonChaosMult = out[key] / cc
			out[key] = cc
		}
		chaosFlag := dmgTypeFlags["Chaos"]
		for _, x := range []struct {
			t  string
			ok bool
		}{
			{"Lightning", list.FlagCfg(cfg, "LightningCanPoison")},
			{"Cold", list.FlagCfg(cfg, "ColdCanPoison")},
			{"Fire", list.FlagCfg(cfg, "FireCanPoison")},
			{"Physical", true},
		} {
			if !x.ok || !canDeal(s, x.t) {
				continue
			}
			mn, mx := ailmentSourceDamage(list, p, dotCfg, x.t, chaosFlag)
			out["Poison"+x.t+"Min"], out["Poison"+x.t+"Max"] = mn, mx
			totalMin += mn * nonChaosMult
			totalMax += mx * nonChaosMult
		}
		if sub == 2 {
			if actor.Flag("AilmentsAreNeverFromCrit") {
				dotCfg.SkillCond["CriticalStrike"] = false
				out["CritPoisonDotMulti"] = dotMultiFor(list, dotCfg, "Chaos")
				dotCfg.SkillCond["CriticalStrike"] = true
			} else {
				out["CritPoisonDotMulti"] = dotMultiFor(list, dotCfg, "Chaos")
			}
			critDmg = (totalMin + totalMax) / 2 * out["CritPoisonDotMulti"]
			maxCritDmg = totalMax * out["CritPoisonDotMulti"]
		} else {
			out["PoisonDotMulti"] = dotMultiFor(list, dotCfg, "Chaos")
			hitDmg = (totalMin + totalMax) / 2 * out["PoisonDotMulti"]
			minHitDmg = totalMin * out["PoisonDotMulti"]
			maxHitDmg = totalMax * out["PoisonDotMulti"]
		}
	}
	effects := out["RuthlessBlowAilmentEffect"] * out["FistOfWarDamageEffect"] * global["AilmentWarcryEffect"]
	ailmentDamage(out, "Poison", critChance, minHitDmg, 0)
	ailmentDamage(out, "Poison", 100, maxHitDmg, maxCritDmg)
	baseVal := ailmentDamage(out, "Poison", critChance, hitDmg, critDmg) * poisonPercentBase * effects
	if !(baseVal > 0) {
		return
	}
	s.flags["poison"] = true
	if debugAilmentCfg != nil {
		debugAilmentCfg(actor, s, dotCfg)
	}
	effMult := dotTakenMult(actor, dotCfg, "Chaos")
	global["PoisonEffMult"] = effMult
	if list.Flag("Condition:NonPoisonedOnly") {
		stacks = math.Min(additional, stacks)
	}
	global["PoisonStacks"] = stacks
	effectMod := calcMod(list, dotCfg, "AilmentEffect")
	single := math.Min(baseVal*effectMod*rateMod*effMult, dotDpsCap)
	out["PoisonDPS"] = single
	out["PoisonDamage"] = single * global["PoisonDuration"]
	g1, _ := list.MaxCfg(nil, "PoisonDpsAsCausticGround")
	g2, _ := enemy.MaxCfg(nil, "PoisonDpsAsCausticGround")
	if ground := math.Max(g1, g2); ground > 0 {
		out["CausticGroundDPS"] = math.Min(baseVal*effectMod*rateMod*effMult*ground/100, dotDpsCap)
		global["CausticGroundFromPoison"] = 1
	}
	if truthy(data.val("showAverage")) {
		out["TotalPoisonAverageDamage"] = out["PoisonDamage"]
		out["TotalPoisonDPS"] = out["PoisonDPS"]
	} else {
		out["TotalPoisonDPS"] = math.Min(single*stacks, dotDpsCap)
	}
	critDotMulti(actor, out, "Poison", global["CritChance"], global["CritChance"])
}

// impale is the "Calculate impale chance and modifiers" part of the
// ailment loop for one pass.
func impale(actor *ModDB, s *activeSkill, p *hitPass, global map[string]float64) {
	list, cfg, out, enemy := s.mods, p.cfg, p.out, actor.enemy
	if !canDeal(s, "Physical") || out["ImpaleChance"]+out["ImpaleChanceOnCrit"] <= 0 {
		return
	}
	s.flags["impale"] = true
	crit := out["CritChance"] / 100
	chance := math.Min(out["ImpaleChance"]/100, 1)*(1-crit) + math.Min(out["ImpaleChanceOnCrit"]/100, 1)*crit
	maxStacks := list.SumCfg(cfg, "BASE", "ImpaleStacksMax") * (1 + list.SumCfg(cfg, "BASE", "ImpaleAdditionalDurationChance")/100)
	stacks := math.Min(maxStacks, enemy.SumCfg(cfg, "BASE", "Multiplier:ImpaleStacks"))
	const impaleStoredDamageBase = 0.1
	onBleed := list.SumCfg(cfg, "INC", "ImpaleEffectOnBleed") * math.Min(list.SumCfg(cfg, "BASE", "BleedChance")/100, 1)
	inc := (list.SumCfg(cfg, "INC", "ImpaleEffect") + onBleed) / 100
	more := round(list.MoreCfg(cfg, "ImpaleEffect"), 2)
	stored := impaleStoredDamageBase * (1 + inc) * more
	hitMod := stored * stacks
	armour := math.Max(val(enemy, "Armour"), 0)
	resist := math.Min(math.Max(0, enemy.Sum("BASE", "PhysicalDamageReduction")+list.SumCfg(cfg, "BASE", "EnemyImpalePhysicalDamageReduction")+
		armourReductionF(armour, hitMod*out["impaleStoredHitAvg"])), enemyPhysicalDamageReductionCap)
	if list.FlagCfg(cfg, "IgnoreEnemyImpalePhysicalDamageReduction") {
		resist = 0
	}
	takenCfg := &Cfg{Flags: mfHit}
	names := []string{"DamageTaken", "PhysicalDamageTaken", "ReflectedDamageTaken"}
	taken := (1 + enemy.SumCfg(takenCfg, "INC", names...)/100) * enemy.MoreCfg(takenCfg, names...)
	global["ImpaleStacksMax"] = maxStacks
	global["ImpaleStacks"] = stacks
	out["ImpaleStoredDamage"] = stored * 100
	out["ImpaleModifier"] = 1 + hitMod*(1-resist/100)*chance*taken
}
