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
