package engine

import "math"

const impaleStoredDamageBase = 0.1 // Data.lua data.misc.ImpaleStoredDamageBase

var isElemental = map[string]bool{"Fire": true, "Cold": true, "Lightning": true}

// defenceEstimations holds what calcs.buildDefenceEstimations keeps on
// the actor between its parts.
type defenceEstimations struct {
	shift    map[string]map[string]float64 // damageShiftTable[from][to] percent
	dotShift map[string]map[string]float64
}

// buildDefenceEstimations ports calcs.buildDefenceEstimations
// (CalcDefence.lua) for the player.
func buildDefenceEstimations(db *ModDB) *defenceEstimations {
	out := db.output
	env := db.enemyEnv
	enemy := db.enemy
	category := env.damageType
	est := &defenceEstimations{shift: map[string]map[string]float64{}, dotShift: map[string]map[string]float64{}}

	// Chance to not be hit.
	if category != "DamageOverTime" {
		projAvoid := out["AvoidProjectilesChance"]
		if out["specificTypeAvoidance"] != 0 {
			projAvoid = 0
		}
		all := 1 - out["AvoidAllDamageFromHitsChance"]/100
		out["MeleeNotHitChance"] = 100 - (1-out["MeleeEvadeChance"]/100)*(1-out["EffectiveAttackDodgeChance"]/100)*all*100
		out["ProjectileNotHitChance"] = 100 - (1-out["ProjectileEvadeChance"]/100)*(1-out["EffectiveAttackDodgeChance"]/100)*all*(1-projAvoid/100)*100
		out["SpellNotHitChance"] = 100 - (1-out["EffectiveSpellDodgeChance"]/100)*all*100
		out["SpellProjectileNotHitChance"] = 100 - (1-out["EffectiveSpellDodgeChance"]/100)*all*(1-projAvoid/100)*100
		out["UntypedNotHitChance"] = 100 - all*100
		out["AverageNotHitChance"] = (out["MeleeNotHitChance"] + out["ProjectileNotHitChance"] + out["SpellNotHitChance"] + out["SpellProjectileNotHitChance"]) / 4
		out["AverageEvadeChance"] = (out["MeleeEvadeChance"] + out["ProjectileEvadeChance"]) / 4
		out["ConfiguredNotHitChance"] = out[category+"NotHitChance"]
		out["ConfiguredEvadeChance"] = out[category+"EvadeChance"]
		if env.worstOf > 1 {
			out["ConfiguredNotHitChance"] *= out["ConfiguredNotHitChance"] / 100
			out["ConfiguredEvadeChance"] *= out["ConfiguredEvadeChance"] / 100
			if env.worstOf == 4 {
				out["ConfiguredNotHitChance"] *= out["ConfiguredNotHitChance"] / 100
				out["ConfiguredEvadeChance"] *= out["ConfiguredEvadeChance"] / 100
			}
		}
	}

	// Enemy damage.
	elemental := func(t string, names ...string) []string {
		if isElemental[t] {
			return append(names, "ElementalDamage")
		}
		return names
	}
	out["totalEnemyDamage"], out["totalEnemyDamageIn"] = 0, 0
	if category == "DamageOverTime" {
		for _, t := range dmgTypes {
			out[t+"EnemyPen"] = 0
			out[t+"EnemyDamageMult"] = calcMod(enemy, nil, elemental(t, "Damage", t+"Damage")...)
			out[t+"EnemyOverwhelm"] = 0
			out[t+"EnemyDamage"] = 0
		}
	} else {
		crit := 0.0
		switch {
		case enemy.Flag("NeverCrit"):
		case enemy.Flag("AlwaysCrit"):
			crit = 100
		default:
			base, ok := db.Override("enemyCritChance")
			if !ok {
				base = env.value("enemyCritChance")
			}
			crit = math.Max(math.Min(base*(1+db.Sum("INC", "EnemyCritChance")/100+enemy.Sum("INC", "CritChance")/100)*(1-out["ConfiguredEvadeChance"]/100), 100), 0)
		}
		out["EnemyCritChance"] = crit
		critDamage := math.Max(env.value("enemyCritDamage")+enemy.Sum("BASE", "CritMultiplier"), 0)
		out["EnemyCritEffect"] = 1 + crit/100*(critDamage/100)*(1-out["CritExtraDamageReduction"]/100)
		allKeywords := &Cfg{KeywordFlags: ^uint64(0)} // keywordFlags = bnot(KeywordFlag.MatchAll)
		conversion := map[string]map[string]float64{}
		for _, t := range dmgTypes {
			mult := calcMod(enemy, nil, elemental(t, "Damage", t+"Damage")...)
			dmg := env.value("enemy" + t + "Damage")
			pen := env.value("enemy" + t + "Pen")
			overwhelm := env.value("enemy" + t + "Overwhelm")
			dmg += (enemy.SumCfg(allKeywords, "BASE", t+"Min") + enemy.SumCfg(allKeywords, "BASE", t+"Max")) / 2
			conversionTotal := 0.0
			if t == "Physical" {
				conv, skill := map[string]float64{}, map[string]float64{}
				total, totalSkill := 0.0, 0.0
				for _, to := range dmgTypes {
					skill[to] = enemy.SumCfg(allKeywords, "BASE", t+"DamageSkillConvertTo"+to)
					conv[to] = enemy.SumCfg(allKeywords, "BASE", t+"DamageConvertTo"+to)
					totalSkill += skill[to]
					total += conv[to]
				}
				if totalSkill > 100 {
					m := 100 / totalSkill
					totalSkill *= m
					total = 0
					for _, to := range dmgTypes {
						skill[to] *= m
						conv[to] = 0
					}
				} else if total+totalSkill > 100 {
					m := (100 - totalSkill) / total
					total *= m
					for _, to := range dmgTypes {
						conv[to] *= m
					}
				}
				conversionTotal = total + totalSkill
				for _, to := range dmgTypes {
					gainAs := enemy.SumCfg(allKeywords, "BASE", t+"DamageGainAs"+to) / 100
					pct := conv[to] / 100
					if sp := skill[to] / 100; sp > 0 {
						if to != "Chaos" {
							sp *= 1 + env.physConversion/100
						}
						pct += sp
					}
					if gainAs > 0 || pct > 0 {
						if conversion[to] == nil {
							conversion[to] = map[string]float64{}
						}
						conversion[to][t] = dmg*gainAs + dmg*pct
					}
				}
			}
			overwhelm += enemy.Sum("BASE", "PhysicalOverwhelm") + db.Sum("BASE", "EnemyPhysicalOverwhelm")
			out[t+"EnemyPen"] = pen
			out[t+"EnemyDamageMult"] = mult
			out[t+"EnemyOverwhelm"] = overwhelm
			out["totalEnemyDamageIn"] += dmg
			out[t+"EnemyDamage"] = dmg * (1 - conversionTotal/100) * mult * out["EnemyCritEffect"]
			for from, d := range conversion[t] {
				names := []string{"Damage", t + "Damage", from + "Damage"}
				if isElemental[t] {
					names = append(names, "ElementalDamage")
				}
				if isElemental[from] {
					names = append(names, "ElementalDamage")
				}
				out[t+"EnemyDamage"] += d * calcMod(enemy, nil, names...) * out["EnemyCritEffect"]
			}
			out["totalEnemyDamage"] += out[t+"EnemyDamage"]
		}
	}

	// Damage taken as.
	for _, t := range dmgTypes {
		shift, dot := map[string]float64{}, map[string]float64{}
		destTotal, dotTotal := 0.0, 0.0
		for _, to := range dmgTypes {
			if to == t {
				continue
			}
			takenAs := db.Sum("BASE", elementalTaken(t, t+"DamageTakenAs"+to, "ElementalDamageTakenAs"+to)...)
			dot[to] = takenAs + db.Sum("BASE", elementalTaken(t, t+"DamageTakenOverTimeAs"+to, "ElementalDamageTakenOverTimeAs"+to)...)
			dotTotal += dot[to]
			shift[to] = takenAs + db.Sum("BASE", elementalTaken(t, t+"DamageFromHitsTakenAs"+to, "ElementalDamageFromHitsTakenAs"+to)...)
			destTotal += shift[to]
		}
		dot[t] = math.Max(100-dotTotal, 0)
		shift[t] = math.Max(100-destTotal, 0)
		est.shift[t], est.dotShift[t] = shift, dot
		out[t+"TakenDamage"] = out[t+"EnemyDamage"] * shift[t] / 100
	}
	for _, t := range dmgTypes {
		for _, to := range dmgTypes {
			if t != to {
				out[to+"TakenDamage"] += out[t+"EnemyDamage"] * est.shift[t][to] / 100
			}
		}
	}
	out["totalTakenDamage"] = 0
	for _, t := range dmgTypes {
		out["totalTakenDamage"] += out[t+"TakenDamage"]
	}

	takenMultipliers(db)
	incomingHitMultipliers(db, category)
	pools(db)
	if category != "DamageOverTime" {
		est.totalEHP(db, category)
	}
	est.maximumHitTaken(db)
	return est
}

// elementalTaken is the "name, isElemental[t] and elementalName" pair.
func elementalTaken(t, name, elementalName string) []string {
	if isElemental[t] {
		return []string{name, elementalName}
	}
	return []string{name}
}

// takenMultipliers is "Damage taken multipliers/Degen calculations".
func takenMultipliers(db *ModDB) {
	out := db.output
	for _, t := range dmgTypes {
		baseInc := db.Sum("INC", "DamageTaken", t+"DamageTaken")
		baseMore := db.More("DamageTaken", t+"DamageTaken")
		if isElemental[t] {
			baseInc += db.Sum("INC", "ElementalDamageTaken")
			baseMore *= db.More("ElementalDamageTaken")
		}
		inc := baseInc + db.Sum("INC", "DamageTakenWhenHit", t+"DamageTakenWhenHit")
		more := baseMore * db.More("DamageTakenWhenHit", t+"DamageTakenWhenHit")
		if isElemental[t] {
			inc += db.Sum("INC", "ElementalDamageTakenWhenHit")
			more *= db.More("ElementalDamageTakenWhenHit")
		}
		out[t+"TakenHitMult"] = math.Max((1+inc/100)*more, 0)
		for _, h := range []string{"Attack", "Spell"} {
			out[h+"TakenHitMult"] = math.Max((1+(inc+db.Sum("INC", h+"DamageTaken"))/100)*more*db.More(h+"DamageTaken"), 0)
			out[t+h+"TakenHitMult"] = out[h+"TakenHitMult"]
		}
		rInc := inc + db.Sum("INC", "ReflectedDamageTaken", t+"ReflectedDamageTaken")
		rMore := more * db.More("ReflectedDamageTaken", t+"ReflectedDamageTaken")
		if isElemental[t] {
			rInc += db.Sum("INC", "ElementalReflectedDamageTaken")
			rMore *= db.More("ElementalReflectedDamageTaken")
		}
		out[t+"TakenReflect"] = math.Max((1+rInc/100)*rMore, 0)

		dInc := baseInc + db.Sum("INC", "DamageTakenOverTime", t+"DamageTakenOverTime")
		dMore := baseMore * db.More("DamageTakenOverTime", t+"DamageTakenOverTime")
		if isElemental[t] {
			dInc += db.Sum("INC", "ElementalDamageTakenOverTime")
			dMore *= db.More("ElementalDamageTakenOverTime")
		}
		resist := out[t+"ResistOverTime"]
		if t == "Physical" {
			resist = out[t+"Resist"]
		}
		if db.Flag("SelfIgnore" + t + "Resistance") {
			resist = 0
		}
		reduction := out["Base"+t+"DamageReduction"]
		if db.Flag("SelfIgnoreBase" + t + "DamageReduction") {
			reduction = 0
		}
		out[t+"TakenDotMult"] = math.Max((1-resist/100)*(1-reduction/100)*(1+dInc/100)*dMore, 0)
	}
}

// armourReduction is calcs.armourReduction.
func armourReduction(armour, raw float64) float64 {
	if armour == 0 && raw == 0 {
		return 0
	}
	return round(armour/(armour+raw*5)*100, 0)
}

// incomingHitMultipliers is "Incoming hit damage multipliers".
func incomingHitMultipliers(db *ModDB, category string) {
	out := db.output
	enemy := db.enemy
	out["totalTakenHit"] = 0
	var impaleFlags uint64
	if category == "Melee" || category == "Projectile" || category == "Average" {
		impaleFlags = mfAttack
	}
	impaleChance := enemy.SumCfg(&Cfg{Flags: impaleFlags}, "BASE", "ImpaleChance")
	if category == "Average" {
		impaleChance *= 0.5
	}
	impaleChance *= 1 - out["ImpaleAvoidChance"] // as PoB: the avoid chance is in percent here
	for _, t := range dmgTypes {
		resist := out[t+"Resist"]
		if v, ok := out[t+"ResistWhenHit"]; ok {
			resist = v
		}
		if db.Flag("SelfIgnore" + t + "Resistance") {
			resist = 0
		}
		reduction := out["Base"+t+"DamageReductionWhenHit"]
		if db.Flag("SelfIgnoreBase" + t + "DamageReduction") {
			reduction = 0
		}
		pen := out[t+"EnemyPen"]
		if db.Flag("SelfIgnore"+t+"Resistance", "EnemyCannotPen"+t+"Resistance") {
			pen = 0
		}
		overwhelm := out[t+"EnemyOverwhelm"]
		if db.Flag("SelfIgnore" + t + "DamageReduction") {
			overwhelm = 0
		}
		damage := out[t+"TakenDamage"]
		impale := 0.0
		if impaleChance > 0 && t == "Physical" {
			impale = damage * impaleStoredDamageBase
		}
		armourReduct, impaleArmourReduct := 0.0, 0.0
		pct := 0.0
		if !db.Flag("ArmourDoesNotApplyTo" + t + "DamageTaken") {
			pct = db.Sum("BASE", "ArmourAppliesTo"+t+"DamageTaken")
		}
		pct = math.Min(pct, 100)
		applied := out["Armour"] * pct / 100 * (1 + out["ArmourDefense"])
		wardBased := t == "Physical" && db.Flag("PhysicalReductionBasedOnWard")
		if wardBased {
			m, _ := db.Override("PhysicalReductionBasedOnWardPercent")
			applied = out["Ward"] * m / 100
		}
		resMult := 1 - (resist-pen)/100
		flat := db.Sum("BASE", "DamageTaken", t+"DamageTaken", "DamageTakenWhenHit", t+"DamageTakenWhenHit")
		switch category {
		case "Melee", "Projectile":
			flat += db.Sum("BASE", "DamageTakenFromAttacks", t+"DamageTakenFromAttacks", t+"DamageTakenFrom"+category+"Attacks")
		case "Spell", "SpellProjectile":
			flat += db.Sum("BASE", "DamageTakenFromSpells", t+"DamageTakenFromSpells", t+"DamageTakenFromSpellProjectiles")
		case "Average":
			flat += db.Sum("BASE", "DamageTakenFromAttacks", t+"DamageTakenFromAttacks")/2 + db.Sum("BASE", t+"DamageTakenFromProjectileAttacks")/4 +
				db.Sum("BASE", "DamageTakenFromSpells", t+"DamageTakenFromSpells")/2 + db.Sum("BASE", "DamageTakenFromSpellProjectiles", t+"DamageTakenFromSpellProjectiles")/4
		}
		out[t+"takenFlat"] = flat
		drMax := out["DamageReductionMax"]
		if pct > 0 || wardBased {
			armourReduct = math.Min(drMax, armourReduction(applied, damage*resMult))
			if impale > 0 {
				impaleArmourReduct = math.Min(drMax, armourReduction(applied, impale*resMult))
			}
		}
		total := math.Min(drMax, armourReduct+reduction)
		reductMult := 1 - math.Max(math.Min(drMax, total-overwhelm), 0)/100
		out[t+"DamageReduction"] = 100 - reductMult*100
		if impale > 0 {
			impale *= resMult * (1 - math.Max(math.Min(drMax, math.Min(drMax, impaleArmourReduct+reduction)-overwhelm), 0)/100)
			impale *= impaleChance / 100 * 5 * out[t+"TakenReflect"]
		}
		takenMult := out[t+"TakenHitMult"]
		suppress := 1.0
		switch category {
		case "Melee", "Projectile":
			takenMult = out[t+"AttackTakenHitMult"]
		case "Spell", "SpellProjectile":
			takenMult = out[t+"SpellTakenHitMult"]
			if out["EffectiveSpellSuppressionChance"] == 100 {
				suppress = 1 - out["SpellSuppressionEffect"]/100
			}
		case "Average":
			takenMult = (out[t+"SpellTakenHitMult"] + out[t+"AttackTakenHitMult"]) / 2
			if out["EffectiveSpellSuppressionChance"] == 100 {
				suppress = 1 - out["SpellSuppressionEffect"]/100/2
			}
		}
		out[t+"EffectiveAppliedArmour"] = applied
		out[t+"ResistTakenHitMulti"] = resMult
		after := takenMult * suppress
		out[t+"AfterReductionTakenHitMulti"] = after
		baseMult := resMult * reductMult
		out[t+"BaseTakenHitMult"] = baseMult * after
		out[t+"TakenHit"] = math.Max(damage*baseMult+flat, 0)*takenMult*suppress + impale
		out[t+"TakenHitMult"] = 0
		if damage > 0 {
			out[t+"TakenHitMult"] = out[t+"TakenHit"] / damage
		}
		out["totalTakenHit"] += out[t+"TakenHit"]
	}
}

// allyLifePools is CalcDefence.lua's allyLifePoolList: the allies that take
// damage before you, in drain order (life output, mitigation output).
// Minion, sentinel, spectre, totem, void spawn and golem life are not
// computed yet, so only Soul Link and Frost Shield can apply.
var allyLifePools = [][2]string{
	{"TotalMinionLife", "MinionAllyDamageMitigation"}, {"TotalRadianceSentinelLife", "RadianceSentinelAllyDamageMitigation"},
	{"TotalSpectreLife", "SpectreAllyDamageMitigation"}, {"TotalTotemLife", "TotemAllyDamageMitigation"},
	{"TotalVaalRejuvenationTotemLife", "VaalRejuvenationTotemAllyDamageMitigation"}, {"TotalVoidSpawnLife", "VoidSpawnAllyDamageMitigation"},
	{"TotalStoneGolemLife", "StoneGolemAllyDamageMitigation"}, {"AlliedEnergyShield", "SoulLinkMitigation"},
	{"FrostShieldLife", "FrostShieldDamageMitigation"},
}

// allyPool is buildAllyLifePools' entry.
type allyPool struct{ remaining, percent float64 }

func buildAllyPools(out map[string]float64) map[string]*allyPool {
	pools := map[string]*allyPool{}
	for _, a := range allyLifePools {
		if life, m := out[a[0]], out[a[1]]; life > 0 && m > 0 {
			pools[a[0]] = &allyPool{remaining: life, percent: math.Min(m, 100) / 100}
		}
	}
	return pools
}

// lifeHitPoolWithLossPrevention is calcLifeHitPoolWithLossPrevention.
func lifeHitPoolWithLossPrevention(life, maxLife, prevented, belowHalfPrevented float64) float64 {
	half := maxLife * 0.5
	above := math.Max(life-half, 0)
	return above/(1-prevented/100) + math.Min(life, half)/(1-belowHalfPrevented/100)/(1-prevented/100)
}

// pools ports the pool parts of buildDefenceEstimations: recoverable life,
// life loss prevention, energy shield bypass, Mind over Matter, Guard,
// Aegis, allies' mitigation and the total pools per damage type.
func pools(db *ModDB) {
	out := db.output
	env := db.enemyEnv
	// Life recoverable.
	out["LifeRecoverable"] = out["LifeUnreserved"]
	if env.checks["conditionLowLife"] {
		pct, ok := out["LowLifePercentage"]
		if !ok {
			pct = lowPoolThreshold
		}
		out["LifeRecoverable"] = math.Min(out["Life"]*pct/100, out["LifeUnreserved"])
	}
	if db.Flag("DamageInsteadReservesLife") {
		out["LifeRecoverable"] = out["LifeCancellableReservation"] / 100 * out["Life"]
	}
	out["LifeRecoverable"] = math.Max(out["LifeRecoverable"], 1)

	// Prevented life loss.
	recoverable := out["LifeRecoverable"]
	prevented := math.Min(db.Sum("BASE", "LifeLossPrevented"), 100)
	out["preventedLifeLoss"] = prevented
	belowHalf := db.Sum("BASE", "LifeLossBelowHalfPrevented")
	out["preventedLifeLossBelowHalf"] = (1 - prevented/100) * belowHalf
	if !env.checks["conditionLowLife"] {
		portion := math.Min(out["Life"]*0.5/recoverable, 1)
		out["preventedLifeLossTotal"] = prevented + out["preventedLifeLossBelowHalf"]*portion
	} else {
		out["preventedLifeLossTotal"] = prevented + out["preventedLifeLossBelowHalf"]
	}
	out["LifeHitPool"] = lifeHitPoolWithLossPrevention(recoverable, out["Life"], prevented, belowHalf)

	// Energy shield bypass.
	out["MinimumBypass"] = 100
	for _, t := range dmgTypes {
		if db.Flag("UnblockedDamageDoesBypassES") {
			out[t+"EnergyShieldBypass"] = 100
		} else {
			v, ok := db.Override(t + "EnergyShieldBypass")
			if !ok {
				v = db.Sum("BASE", t+"EnergyShieldBypass")
			}
			if t == "Chaos" && !db.Flag("ChaosNotBypassEnergyShield") {
				v += 100
			}
			out[t+"EnergyShieldBypass"] = v
		}
		out[t+"EnergyShieldBypass"] = math.Max(math.Min(out[t+"EnergyShieldBypass"], 100), 0)
		out["MinimumBypass"] = math.Min(out["MinimumBypass"], out[t+"EnergyShieldBypass"])
	}

	// Mind over Matter.
	out["sharedMindOverMatter"] = math.Min(db.Sum("BASE", "DamageTakenFromManaBeforeLife"), 100)
	momPool := func(lifePool, effect, bypass float64) float64 {
		mana := math.Max(out["ManaUnreserved"], 0)
		maxPool := math.Inf(1)
		if effect < 1 {
			maxPool = lifePool/(1-effect) - lifePool
		}
		maxMana := math.Floor(math.Min(mana, maxPool))
		maxES := 0.0
		if db.Flag("EnergyShieldProtectsMana") && bypass < 1 {
			maxES = math.Floor(math.Min(math.Min(out["EnergyShieldRecoveryCap"], maxPool*(1-bypass)),
				(lifePool+maxMana)/(1-(1-bypass)*effect)-(lifePool+maxMana)))
		}
		used := math.Floor(math.Min(maxPool-maxES, maxMana))
		return lifePool + used + maxES
	}
	if out["sharedMindOverMatter"] > 0 {
		m, bypass := out["sharedMindOverMatter"]/100, out["MinimumBypass"]/100
		out["sharedManaEffectiveLife"] = momPool(out["LifeRecoverable"], m, bypass)
		out["sharedMoMHitPool"] = momPool(out["LifeHitPool"], m, bypass)
	} else {
		out["sharedManaEffectiveLife"] = out["LifeRecoverable"]
		out["sharedMoMHitPool"] = out["LifeHitPool"]
	}
	for _, t := range dmgTypes {
		out[t+"MindOverMatter"] = math.Min(db.Sum("BASE", t+"DamageTakenFromManaBeforeLife"), 100-out["sharedMindOverMatter"])
		if out[t+"MindOverMatter"] > 0 || (out[t+"EnergyShieldBypass"] > out["MinimumBypass"] && out["sharedMindOverMatter"] > 0) {
			m := (out[t+"MindOverMatter"] + out["sharedMindOverMatter"]) / 100
			bypass := out[t+"EnergyShieldBypass"] / 100
			out[t+"ManaEffectiveLife"] = momPool(out["LifeRecoverable"], m, bypass)
			out[t+"MoMHitPool"] = momPool(out["LifeHitPool"], m, bypass)
		} else {
			out[t+"ManaEffectiveLife"] = out["sharedManaEffectiveLife"]
			out[t+"MoMHitPool"] = out["sharedMoMHitPool"]
		}
	}

	// Guard.
	out["sharedGuardAbsorbRate"] = math.Min(db.Sum("BASE", "GuardAbsorbRate"), 100)
	if out["sharedGuardAbsorbRate"] > 0 {
		out["sharedGuardAbsorb"] = calcVal(db, "GuardAbsorbLimit")
	}
	for _, t := range dmgTypes {
		out[t+"GuardAbsorbRate"] = math.Min(db.Sum("BASE", t+"GuardAbsorbRate"), 100)
		if out[t+"GuardAbsorbRate"] > 0 {
			out[t+"GuardAbsorb"] = calcVal(db, t+"GuardAbsorbLimit")
		}
	}

	// Aegis.
	out["sharedAegis"], _ = maxSource(db, "", "AegisValue")
	out["sharedElementalAegis"], _ = maxSource(db, "", "ElementalAegisValue")
	for _, t := range dmgTypes {
		out[t+"Aegis"], _ = maxSource(db, "", t+"AegisValue")
		if isElemental[t] {
			out[t+"AegisDisplay"] = out[t+"Aegis"] + out["sharedElementalAegis"]
		}
	}

	// Taken from allies before you: Frost Shield; ally life pools
	// (minions, totems, golems) are not ported.
	out["FrostShieldLife"] = db.Sum("BASE", "FrostGlobeHealth")
	out["FrostShieldDamageMitigation"] = db.Sum("BASE", "FrostGlobeDamageMitigation")
	out["SoulLinkMitigation"] = db.Sum("BASE", "TakenFromParentESBeforeYou")
	if out["SoulLinkMitigation"] == 0 {
		out["SoulLinkMitigation"] = db.Sum("BASE", "TakenFromPartyMemberESBeforeYou")
	}

	// Vaal Arctic Armour.
	out["VaalArcticArmourLife"] = db.Sum("BASE", "VaalArcticArmourMaxHits")
	out["VaalArcticArmourMitigation"] = math.Min(-db.Sum("MORE", "VaalArcticArmourMitigation")/100, 1)

	// Total pools.
	for _, t := range dmgTypes {
		out[t+"TotalPool"] = out[t+"ManaEffectiveLife"]
		out[t+"TotalHitPool"] = out[t+"MoMHitPool"]
		if out[t+"EnergyShieldBypass"] < 100 && !db.Flag("EnergyShieldProtectsMana") {
			if b := out[t+"EnergyShieldBypass"]; b > 0 {
				protected := out["EnergyShieldRecoveryCap"] / (1 - b/100) * (b / 100)
				for _, k := range []string{"TotalPool", "TotalHitPool"} {
					out[t+k] = math.Max(out[t+k]-protected, 0) + math.Min(out[t+k], protected)/(b/100)
				}
			} else {
				out[t+"TotalPool"] += out["EnergyShieldRecoveryCap"]
				out[t+"TotalHitPool"] += out["EnergyShieldRecoveryCap"]
			}
		}
	}
}

// armourReductionF is calcs.armourReductionF.
func armourReductionF(armour, raw float64) float64 {
	if armour == 0 && raw == 0 {
		return 0
	}
	return armour / (armour + raw*5) * 100
}

// takenHitFromDamage is calcs.takenHitFromDamage: the damage each type
// takes from a raw hit of one type, after conversion and mitigation.
func (est *defenceEstimations) takenHitFromDamage(db *ModDB, raw float64, t string) (float64, map[string]float64) {
	out := db.output
	sum := 0.0
	damages := map[string]float64{}
	vaal := math.Min(-db.Sum("MORE", "VaalArcticArmourMitigation")/100, 1)
	for _, to := range dmgTypes {
		pct := est.shift[t][to]
		flat := out[to+"takenFlat"]
		if pct <= 0 && flat == 0 {
			continue
		}
		converted := raw * pct / 100
		resist := out[to+"ResistTakenHitMulti"]
		armourDR := armourReductionF(out[to+"EffectiveAppliedArmour"], converted*resist)
		flatDR := out["Base"+to+"DamageReductionWhenHit"]
		if db.Flag("SelfIgnoreBase" + to + "DamageReduction") {
			flatDR = 0
		}
		total := math.Min(out["DamageReductionMax"], armourDR+flatDR)
		overwhelm := out[to+"EnemyOverwhelm"]
		if db.Flag("SelfIgnore" + to + "DamageReduction") {
			overwhelm = 0
		}
		drMult := 1 - math.Max(math.Min(out["DamageReductionMax"], total-overwhelm), 0)/100
		reduced := round(math.Max(converted*resist*drMult+flat, 0)*out[to+"AfterReductionTakenHitMulti"], 0) * (1 - vaal)
		sum += reduced
		if reduced > 0 || pct > 0 {
			damages[to] = reduced
		}
	}
	return sum, damages
}

// poolState is the pool table of calcs.reducePoolsByDamage.
type poolState struct {
	allies                          map[string]*allyPool
	aegis, guard                    map[string]float64
	ward, wardActiveChance          float64
	energyShield, mana, life        float64
	lifeLossLost, lifeBelowHalfLost float64
	recoupable                      map[string]float64 // damageTakenThatCanBeRecouped
	overkill, hitPoolRemaining      float64
	hasWardActiveChance             bool
}

// newPoolState is the defaults reducePoolsByDamage takes from the output.
func newPoolState(out map[string]float64) *poolState {
	p := &poolState{aegis: map[string]float64{"shared": out["sharedAegis"], "sharedElemental": out["sharedElementalAegis"]},
		guard: map[string]float64{"shared": out["sharedGuardAbsorb"]}, ward: out["Ward"],
		energyShield: out["EnergyShieldRecoveryCap"], mana: out["ManaUnreserved"], life: out["LifeRecoverable"],
		recoupable: map[string]float64{}, allies: buildAllyPools(out)}
	for _, t := range dmgTypes {
		p.aegis[t] = out[t+"Aegis"]
		p.guard[t] = out[t+"GuardAbsorb"]
	}
	return p
}

// reducePoolsByDamage is calcs.reducePoolsByDamage without the breakdown
// and with no allies' pools (not ported). in may be nil.
func reducePoolsByDamage(db *ModDB, in *poolState, damage map[string]float64) *poolState {
	out := db.output
	if in == nil {
		in = newPoolState(out)
	}
	total := 0.0
	dmg := map[string]float64{}
	for t, d := range damage {
		if d > 0 {
			dmg[t] = math.Ceil(d)
			total += dmg[t]
		}
	}
	aegis := map[string]float64{}
	for k, v := range in.aegis {
		aegis[k] = v
	}
	guard := map[string]float64{}
	for k, v := range in.guard {
		guard[k] = v
	}
	recoup := in.recoupable
	ward := in.ward
	wardActive := in.wardActiveChance
	if !in.hasWardActiveChance {
		wardActive = 0
		if ward > 0 {
			wardActive = 1
		}
	}
	wardAvoidBreak := math.Min(db.Sum("BASE", "WardAvoidBreakChance")/100, 1)
	if db.Flag("Condition:WardNotBreak") {
		wardAvoidBreak = 1
	}
	wardBypassBelow := db.Sum("BASE", "WardBypassBelowPercent") / 100
	es, mana, life := in.energyShield, in.mana, in.life
	belowHalfPrevented := db.Sum("BASE", "LifeLossBelowHalfPrevented")
	lost, belowLost := in.lifeLossLost, in.lifeBelowHalfLost
	overkill := 0.0
	ward = math.Max(ward, 0)
	wardBefore := ward
	es, mana, life = math.Max(es, 0), math.Max(mana, 0), math.Max(life, 0)

	hitPoolInitial := lifeHitPoolWithLossPrevention(life, out["Life"], out["preventedLifeLoss"], belowHalfPrevented)
	momLeft, esLeft := map[string]float64{}, map[string]float64{}
	protectsMana := db.Flag("EnergyShieldProtectsMana")
	for t := range dmg {
		mom := math.Min(out["sharedMindOverMatter"]+out[t+"MindOverMatter"], 100) / 100
		maxMoM := hitPoolInitial/(1-mom) - hitPoolInitial
		momPool := mana
		if mom < 1 {
			momPool = math.Min(maxMoM, mana)
		}
		lifePlus := hitPoolInitial + momPool
		bypass := out[t+"EnergyShieldBypass"] / 100
		esPool := es
		if bypass > 0 {
			esPool = math.Min(lifePlus/bypass-lifePlus, es)
		}
		if protectsMana && bypass < 1 {
			esLeft[t] = math.Floor(math.Min(math.Min(maxMoM*(1-bypass), es), lifePlus/(1-(1-bypass)*mom)-lifePlus))
			momLeft[t] = math.Floor(math.Min(maxMoM-esLeft[t], momPool))
		} else {
			esLeft[t] = math.Floor(esPool)
			momLeft[t] = math.Floor(momPool)
		}
	}

	for _, t := range dmgTypes {
		rem, ok := dmg[t]
		if !ok {
			continue
		}
		for _, a := range allyLifePools {
			if ap := in.allies[a[0]]; ap != nil && ap.remaining > 0 {
				d := math.Min(rem*ap.percent, ap.remaining)
				ap.remaining = math.Floor(ap.remaining - d)
				rem -= d
			}
		}
		recoup[t] += rem
		take := func(pool *float64, amount float64) {
			d := math.Min(amount, *pool)
			*pool -= d
			rem -= d
		}
		if v := aegis[t]; v > 0 {
			d := math.Min(rem, v)
			aegis[t] -= d
			rem -= d
		}
		if isElemental[t] && aegis["sharedElemental"] > 0 {
			v := aegis["sharedElemental"]
			take(&v, rem)
			aegis["sharedElemental"] = v
		}
		if aegis["shared"] > 0 {
			v := aegis["shared"]
			take(&v, rem)
			aegis["shared"] = v
		}
		if guard[t] > 0 {
			d := math.Min(rem*out[t+"GuardAbsorbRate"]/100, guard[t])
			guard[t] = math.Floor(guard[t] - d)
			rem -= d
		}
		if guard["shared"] > 0 {
			d := math.Min(rem*out["sharedGuardAbsorbRate"]/100, guard["shared"])
			guard["shared"] = math.Floor(guard["shared"] - d)
			rem -= d
		}
		if ward > 0 && (wardBypassBelow == 0 || total >= wardBefore*wardBypassBelow) {
			d := math.Min(rem*(1-db.Sum("BASE", "WardBypass")/100), ward)
			ward -= d
			rem -= d * wardActive
		}
		mom := math.Min(out["sharedMindOverMatter"]+out[t+"MindOverMatter"], 100) / 100
		bypass := out[t+"EnergyShieldBypass"] / 100
		esPool := esLeft[t]
		if es > 0 && !protectsMana && bypass < 1 {
			d := math.Min(rem*(1-bypass), esPool)
			for t2 := range dmg {
				esLeft[t2] = math.Max(esLeft[t2]-d, 0)
			}
			es -= d
			rem -= d
		}
		if mom > 0 {
			momDamage := math.Ceil(rem * mom)
			if protectsMana && es > 0 && bypass < 1 {
				d := math.Ceil(math.Min(momDamage*(1-bypass), esPool))
				for t2 := range dmg {
					esLeft[t2] = math.Floor(math.Max(esLeft[t2]-d, 0))
				}
				es -= d
				rem -= d
				momDamage -= d
			}
			d := math.Ceil(math.Min(momDamage, momLeft[t]))
			for t2 := range dmg {
				momLeft[t2] = math.Floor(math.Max(momLeft[t2]-d, 0))
			}
			mana -= d
			rem -= d
		}
		if out["preventedLifeLossTotal"] > 0 {
			half := out["Life"] * 0.5
			prevent := out["preventedLifeLoss"] / 100
			aboveLow := math.Max(life-half, 0) / (1 - prevent)
			preventBelow := belowHalfPrevented / 100
			canTake := aboveLow + math.Max(math.Min(life, half), 0)/(1-preventBelow)/(1-prevent)
			if canTake < rem {
				overkill += rem - canTake
				rem = canTake
			}
			if out["preventedLifeLossBelowHalf"] != 0 {
				split := math.Min(rem, aboveLow)
				rem -= split
				lost += split * prevent
				life -= split * (1 - prevent)
				if life <= half {
					u := rem * prevent
					lost += u
					rem -= u
					s := rem * preventBelow
					belowLost += s
					rem -= s
				}
			} else {
				d := rem * prevent
				lost += d
				rem -= d
			}
		}
		if life > 0 {
			d := math.Min(rem, life)
			life -= d
			rem -= d
		}
		overkill += rem
	}

	momRemaining, esRemaining := math.Inf(1), math.Inf(1)
	for t := range dmg {
		momRemaining = math.Min(momRemaining, momLeft[t])
		esRemaining = math.Min(esRemaining, esLeft[t])
	}
	hitPool := 0.0
	if life >= 1 {
		hitPool = lifeHitPoolWithLossPrevention(life, out["Life"], out["preventedLifeLoss"], belowHalfPrevented)
	}
	if !math.IsInf(momRemaining, 1) {
		hitPool += momRemaining
	}
	if !math.IsInf(esRemaining, 1) {
		hitPool += esRemaining
	}
	res := &poolState{allies: in.allies, aegis: aegis, guard: guard, energyShield: math.Floor(es), mana: math.Floor(mana), life: math.Floor(life),
		recoupable: recoup, lifeLossLost: math.Ceil(lost), lifeBelowHalfLost: math.Ceil(belowLost),
		overkill: math.Ceil(overkill), hitPoolRemaining: math.Floor(hitPool), hasWardActiveChance: true}
	res.ward = math.Floor(ward)
	res.wardActiveChance = wardActive
	if ward < wardBefore {
		res.ward = 0
		if wardAvoidBreak > 0 {
			res.ward = math.Floor(wardBefore)
		}
		res.wardActiveChance = wardActive * wardAvoidBreak
	}
	return res
}

const maxHitSmoothingPasses = 8 // Data.lua data.misc.maxHitSmoothingPasses

// maximumHitTaken is the "maximum hit taken" part of
// buildDefenceEstimations (no allies' pools).
func (est *defenceEstimations) maximumHitTaken(db *ModDB) {
	out := db.output
	for _, t := range dmgTypes {
		k := t + "TotalHitPool"
		if bypass := db.Sum("BASE", "WardBypass"); bypass > 0 {
			protected := out["Ward"] / (1 - bypass/100) * (bypass / 100)
			out[k] = math.Max(out[k]-protected, 0) + math.Min(out[k], protected)/(bypass/100)
		} else {
			out[k] += out["Ward"]
		}
		aegis := math.Max(out[t+"Aegis"], out["sharedAegis"])
		if isElemental[t] {
			aegis = math.Max(aegis, out[t+"AegisDisplay"])
		}
		out[k] += aegis
		// "output.sharedGuardAbsorbRate or 0 + ..." is the shared rate alone in Lua.
		if rate := out["sharedGuardAbsorbRate"]; rate > 0 {
			absorb := out["sharedGuardAbsorb"]
			if rate >= 100 {
				out[k] += absorb
			} else {
				protected := absorb / (rate / 100) * (1 - rate/100)
				out[k] = math.Max(out[k]-protected, 0) + math.Min(out[k], protected)/(1-rate/100)
			}
		}
		for i := len(allyLifePools) - 1; i >= 0; i-- {
			life, m := out[allyLifePools[i][0]], out[allyLifePools[i][1]]
			if life <= 0 || m <= 0 {
				continue
			}
			m = math.Min(m, 100)
			if m == 100 {
				out[k] += life
			} else {
				protected := life / (m / 100) * (1 - m/100)
				out[k] = math.Max(out[k]-protected, 0) + math.Min(out[k], protected)/(1-m/100)
			}
		}
	}
	for _, t := range dmgTypes {
		partMin := math.Inf(1)
		smoothing := false
		for _, to := range dmgTypes {
			pct := est.shift[t][to]
			flat := out[to+"takenFlat"]
			if pct <= 0 && flat == 0 {
				continue
			}
			hit := 0.0
			applied := out[to+"EffectiveAppliedArmour"]
			conv := pct / 100
			pool := out[to+"TotalHitPool"]
			takenMulti := out[to+"AfterReductionTakenHitMulti"] * (1 - out["VaalArcticArmourMitigation"])
			switch {
			case conv <= 0:
				if math.Max(flat, 0)*takenMulti >= pool {
					hit = 0
				} else {
					hit = math.Inf(1)
				}
			case applied == 0 && pct == 100:
				dr := out[to+"ResistTakenHitMulti"] * (1 - out[to+"DamageReduction"]/100)
				hit = math.Max(pool/conv/dr-flat, 0) / takenMulti
			default:
				resist := out[to+"ResistTakenHitMulti"]
				flatDR := out["Base"+to+"DamageReductionWhenHit"] / 100
				if db.Flag("SelfIgnoreBase" + to + "DamageReduction") {
					flatDR = 0
				}
				overwhelm := out[to+"EnemyOverwhelm"]
				if db.Flag("SelfIgnore" + to + "DamageReduction") {
					overwhelm = 0
				}
				rc := resist * conv
				a := 5 * (1 - flatDR + overwhelm/100) * takenMulti * rc * rc
				b := ((overwhelm/100-flatDR)*applied*takenMulti - 5*(pool-flat*takenMulti)) * rc
				c := -applied * (pool - flat*takenMulti)
				raw := math.Inf(1)
				if a != 0 {
					raw = (math.Sqrt(math.Max(b*b-4*a*c, 0)) - b) / (2 * a)
				}
				noDR := pool / conv / resist / takenMulti * (1 - flat*takenMulti/pool)
				maxDR := noDR / (1 - (out["DamageReductionMax"]-overwhelm)/100)
				hit = math.Floor(math.Max(math.Min(raw, maxDR), noDR))
				smoothing = smoothing || pct != 100
			}
			partMin = math.Min(partMin, hit)
		}
		mult := out[t+"EnemyDamageMult"]
		var final float64
		switch {
		case math.IsInf(partMin, 1):
			final = partMin
		case smoothing:
			incoming := partMin
			var previous float64
			havePrevious := false
			for n := 1; n <= maxHitSmoothingPasses; n++ {
				_, damages := est.takenHitFromDamage(db, incoming, t)
				pools := reducePoolsByDamage(db, nil, damages)
				overkill := pools.overkill - pools.hitPoolRemaining
				ratio := 0.0
				for pt := range damages {
					if pp := out[pt+"TotalHitPool"]; pp > 0 {
						ratio = math.Max(ratio, (overkill+pp)/pp)
					}
				}
				if ratio <= 0 {
					ratio = 1
				}
				step := 1.0
				if n > 1 && havePrevious && previous != 0 {
					step = math.Min(math.Abs((overkill-previous)/previous), 2)
				}
				adjust := 0.0
				switch {
				case step > 1:
					adjust = -overkill / step
				case n > 1:
					adjust = -overkill * step
				}
				previous, havePrevious = overkill, true
				incoming = (incoming + adjust) / math.Sqrt(ratio)
				if overkill < 1 && overkill > -1 {
					break
				}
			}
			final = round(incoming/mult, 0)
		default:
			final = round(partMin/mult, 0)
		}
		out[t+"MaximumHitTaken"] = final
	}
	minimum, second := math.Inf(1), math.Inf(1)
	for _, t := range dmgTypes {
		v := out[t+"MaximumHitTaken"]
		if v < minimum {
			second, minimum = minimum, v
		} else if v < second {
			second = v
		}
	}
	out["SecondMinimalMaximumHitTaken"] = second
}

const (
	ehpCalcSpeedUp             = 8         // Data.lua data.misc.ehpCalcSpeedUp
	ehpCalcMaxDamage           = 100000000 // data.misc.ehpCalcMaxDamage
	ehpCalcMaxIterationsToCalc = 50        // data.misc.ehpCalcMaxIterationsToCalc
)

// damageIn is numberOfHitsToDie's DamageIn table.
type damageIn struct {
	damage                               map[string]float64
	cycles                               float64
	iterations                           int
	limitSpeedup, cyclesRan              bool
	gainWhenHit                          bool
	lifeWhenHit, manaWhenHit, esWhenHit  float64
	missingLifeBefore, missingManaBefore float64
	trackRecoupable, trackLifeLoss       bool
}

// numberOfHitsToDie is buildDefenceEstimations' helper of that name.
func (est *defenceEstimations) numberOfHitsToDie(db *ModDB, in *damageIn) float64 {
	out := db.output
	if in.cycles == 0 {
		in.cycles = 1
	}
	total := 0.0
	for _, t := range dmgTypes {
		total += in.damage[t]
	}
	if total == 0 {
		return math.Inf(1)
	}
	wardAvoidBreak := math.Min(db.Sum("BASE", "WardAvoidBreakChance")/100, 1)
	if db.Flag("Condition:WardNotBreak") {
		wardAvoidBreak = 1
	}
	if wardAvoidBreak == 1 && out["Ward"] > 0 && total < out["Ward"] {
		return math.Inf(1)
	}
	numHits := 0.0
	ward := out["Ward"]
	if wardAvoidBreak < 1 && in.cycles > 1 {
		ward = 0
	}
	pool := newPoolState(out)
	pool.ward = ward
	pool.lifeLossLost, pool.lifeBelowHalfLost = out["LifeLossLostOverTime"], out["LifeBelowHalfLossLostOverTime"]
	if in.cycles != 1 {
		in.trackRecoupable, in.trackLifeLoss = false, false
	}

	vaalLeft := out["VaalArcticArmourLife"]
	if in.cycles > 1 {
		vaalLeft = 0
	}
	iterMult := 1.0
	damageTotal := 0.0
	for pool.life > 0 && in.iterations < ehpCalcMaxIterationsToCalc {
		in.iterations++
		dmg := map[string]float64{}
		damageTotal = 0
		vaalMult := 1.0
		if vaalLeft > 0 {
			vaalMult = 1 - out["VaalArcticArmourMitigation"]*math.Min(vaalLeft/iterMult, 1)
		}
		vaalLeft -= iterMult
		for _, t := range dmgTypes {
			d := in.damage[t]
			if d > 0 {
				dmg[t] = d * iterMult * vaalMult
			}
			damageTotal += d
		}
		if (in.gainWhenHit || in.missingLifeBefore != 0 || in.missingManaBefore != 0) && (iterMult > 1 || in.cycles > 1) {
			gain := iterMult * in.cycles
			if in.missingLifeBefore != 0 {
				pool.life = math.Min(pool.life+in.missingLifeBefore*(out["LifeUnreserved"]-pool.life)*(gain-1)/100, gain*out["LifeRecoverable"])
			}
			if in.missingManaBefore != 0 {
				pool.mana = math.Min(pool.mana+in.missingManaBefore*(out["ManaUnreserved"]-pool.mana)*(gain-1)/100, gain*out["ManaUnreserved"])
			}
			if in.gainWhenHit {
				pool.life = math.Min(pool.life+in.lifeWhenHit*(gain-1), gain*out["LifeRecoverable"])
				pool.mana = math.Min(pool.mana+in.manaWhenHit*(gain-1), gain*out["ManaUnreserved"])
				pool.energyShield = math.Min(pool.energyShield+in.esWhenHit*(gain-1), gain*out["EnergyShieldRecoveryCap"])
			}
		}
		if in.missingLifeBefore != 0 && pool.life > 0 {
			pool.life = math.Min(pool.life+in.missingLifeBefore*(out["LifeUnreserved"]-pool.life)/100, out["LifeRecoverable"])
		}
		if in.missingManaBefore != 0 && pool.mana > 0 {
			pool.mana = math.Min(pool.mana+in.missingManaBefore*(out["ManaUnreserved"]-pool.mana)/100, out["ManaUnreserved"])
		}
		pool = reducePoolsByDamage(db, pool, dmg)
		if pool.life > 0 && damageTotal >= ehpCalcMaxDamage {
			return math.Inf(1)
		}
		if in.gainWhenHit && pool.life > 0 {
			pool.life = math.Min(pool.life+in.lifeWhenHit, out["LifeRecoverable"])
			pool.mana = math.Min(pool.mana+in.manaWhenHit, out["ManaUnreserved"])
			pool.energyShield = math.Min(pool.energyShield+in.esWhenHit, out["EnergyShieldRecoveryCap"])
		}
		iterMult = 1
		speedUp := float64(ehpCalcSpeedUp)
		if in.limitSpeedup {
			speedUp = 4
		}
		wardBreakActive := wardAvoidBreak < 1 && pool.wardActiveChance > 0.01
		if !in.cyclesRan && !wardBreakActive && pool.life > 0 && in.iterations < ehpCalcMaxIterationsToCalc {
			next := &damageIn{damage: map[string]float64{}, limitSpeedup: in.limitSpeedup, cycles: in.cycles * speedUp, iterations: in.iterations}
			for _, t := range dmgTypes {
				next.damage[t] = in.damage[t] * speedUp
			}
			if in.gainWhenHit {
				next.gainWhenHit = true
				next.lifeWhenHit, next.manaWhenHit, next.esWhenHit = in.lifeWhenHit, in.manaWhenHit, in.esWhenHit
			}
			iterMult = math.Max((est.numberOfHitsToDie(db, next)-1)*speedUp-1, 1)
			if math.IsInf(iterMult, 1) {
				return math.Inf(1)
			}
			in.iterations = next.iterations
			in.cyclesRan = true
		}
		numHits += iterMult
	}
	if in.trackRecoupable {
		for t, v := range pool.recoupable {
			out[t+"RecoupableDamageTaken"] += v
		}
	}
	if in.trackLifeLoss {
		out["LifeLossLostOverTime"] += pool.lifeLossLost
		out["LifeBelowHalfLossLostOverTime"] += pool.lifeBelowHalfLost
	}
	if pool.life == 0 && in.cycles == 1 {
		numHits -= pool.overkill / damageTotal
	}
	damageTotal = 0
	for _, t := range dmgTypes {
		damageTotal += in.damage[t] * numHits
	}
	if pool.life >= 0 && damageTotal >= ehpCalcMaxDamage {
		return math.Inf(1)
	}
	if math.IsNaN(numHits) {
		return 0
	}
	return math.Max(numHits, 0)
}

// totalEHP is the effective health pool part of buildDefenceEstimations.
func (est *defenceEstimations) totalEHP(db *ModDB, category string) {
	out := db.output
	env := db.enemyEnv
	plain := &damageIn{damage: map[string]float64{}}
	for _, t := range dmgTypes {
		plain.damage[t] = out[t+"TakenHit"]
	}
	plain.limitSpeedup = out["preventedLifeLossTotal"] > 0
	out["NumberOfDamagingHits"] = est.numberOfHitsToDie(db, plain)

	in := &damageIn{damage: map[string]float64{}}
	block := out["EffectiveBlockChance"] / 100
	if category != "Melee" && category != "Untyped" {
		block = out["Effective"+category+"BlockChance"] / 100
	}
	if db.enemy.Flag("CannotBeBlocked") {
		block = 0
	}
	blockEffect := 1 - block*out["BlockEffect"]/100
	suppress, suppressEffect, extraAvoid, avgAvoid := 0.0, 1.0, 0.0, 0.0
	gainOnBlock := !env.checks["DisableEHPGainOnBlock"] && out["NumberOfDamagingHits"] > 1
	if gainOnBlock {
		in.lifeWhenHit = out["LifeOnBlock"] * block
		in.manaWhenHit = out["ManaOnBlock"] * block
		in.esWhenHit = out["EnergyShieldOnBlock"] * block
		switch category {
		case "Spell", "SpellProjectile":
			in.esWhenHit += out["EnergyShieldOnSpellBlock"] * block
		case "Average":
			in.esWhenHit += out["EnergyShieldOnSpellBlock"] / 2 * block
		}
	}
	if category == "Spell" || category == "SpellProjectile" || category == "Average" {
		suppress = out["EffectiveSpellSuppressionChance"] / 100
	}
	if suppress < 1 {
		if category == "Average" {
			suppress /= 2
		}
		in.esWhenHit += out["EnergyShieldOnSuppress"] * suppress
		in.lifeWhenHit += out["LifeOnSuppress"] * suppress
		suppressEffect = 1 - suppress*out["SpellSuppressionEffect"]/100
	} else {
		half := 1.0
		if category == "Average" {
			half = 0.5
		}
		in.esWhenHit += out["EnergyShieldOnSuppress"] * half
		in.lifeWhenHit += out["LifeOnSuppress"] * half
	}
	switch category {
	case "Projectile", "SpellProjectile":
		extraAvoid += out["AvoidProjectilesChance"]
	case "Average":
		extraAvoid += out["AvoidProjectilesChance"] / 2
	}
	if gainOnBlock {
		in.missingLifeBefore = db.Sum("BASE", "MissingLifeBeforeEnemyHit")
		in.missingManaBefore = db.Sum("BASE", "MissingManaBeforeEnemyHit")
		if in.lifeWhenHit != 0 || in.manaWhenHit != 0 || in.esWhenHit != 0 || in.missingLifeBefore != 0 || in.missingManaBefore != 0 {
			in.gainWhenHit = true
		}
	} else {
		in.lifeWhenHit, in.manaWhenHit, in.esWhenHit = 0, 0, 0
	}
	for _, t := range dmgTypes {
		avoid := 0.0
		if out["specificTypeAvoidance"] != 0 {
			avoid = math.Min(out["Avoid"+t+"DamageChance"]+extraAvoid, avoidChanceCap)
			if env.worstOf > 1 {
				avoid *= avoid / 100
				if env.worstOf == 4 {
					avoid *= avoid / 100
				}
			}
			avgAvoid += avoid
		}
		in.damage[t] = out[t+"TakenHit"] * (blockEffect * suppressEffect * (1 - avoid/100))
	}
	if out["anyRecoup"] > 0 {
		in.trackRecoupable = true
		for _, t := range dmgTypes {
			out[t+"RecoupableDamageTaken"] = 0
		}
	}
	if out["preventedLifeLossTotal"] > 0 {
		in.trackLifeLoss = true
		out["LifeLossLostOverTime"], out["LifeBelowHalfLossLostOverTime"] = 0, 0
	}
	in.limitSpeedup = in.trackRecoupable || in.trackLifeLoss || in.gainWhenHit
	avgAvoid /= 5
	out["ConfiguredDamageChance"] = 100 * (blockEffect * suppressEffect * (1 - avgAvoid/100))
	if out["ConfiguredDamageChance"] != 100 || in.trackRecoupable || in.trackLifeLoss || in.gainWhenHit {
		out["NumberOfMitigatedDamagingHits"] = est.numberOfHitsToDie(db, in)
	} else {
		out["NumberOfMitigatedDamagingHits"] = out["NumberOfDamagingHits"]
	}
	out["TotalNumberOfHits"] = out["NumberOfMitigatedDamagingHits"] / (1 - out["ConfiguredNotHitChance"]/100)
	out["TotalEHP"] = out["TotalNumberOfHits"] * out["totalEnemyDamageIn"]

	speed := env.value("enemySpeed")
	if speed == 0 {
		speed = 700
	}
	skillTime := speed / (1 + db.enemy.Sum("INC", "Speed")/100)
	out["enemySkillTime"] = skillTime / 1000 / actionSpeedMod(db.enemy)
	out["EHPSurvivalTime"] = out["TotalNumberOfHits"] * out["enemySkillTime"]
}
