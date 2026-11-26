package engine

import "math"

// allyLifePoolList is CalcDefence.lua allyLifePoolList: life output,
// mitigation output, redirect mod and fallback life mod, in drain order.
var allyLifePoolList = []struct {
	life, mitigation, redirect, fallback string
	meleeOnly                            bool
}{
	{"TotalMinionLife", "MinionAllyDamageMitigation", "takenFromMinionBeforeYou", "Multiplier:MinionLife", false},
	{"TotalRadianceSentinelLife", "RadianceSentinelAllyDamageMitigation", "takenFromRadianceSentinelBeforeYou", "", false},
	{"TotalSpectreLife", "SpectreAllyDamageMitigation", "takenFromSpectresBeforeYou", "", false},
	{"TotalTotemLife", "TotemAllyDamageMitigation", "takenFromTotemsBeforeYou", "", false},
	{"TotalVaalRejuvenationTotemLife", "VaalRejuvenationTotemAllyDamageMitigation", "takenFromVaalRejuvenationTotemsBeforeYou", "", false},
	{"TotalVoidSpawnLife", "VoidSpawnAllyDamageMitigation", "takenFromVoidSpawnBeforeYou", "", false},
	{"TotalStoneGolemLife", "StoneGolemAllyDamageMitigation", "takenFromStoneGolemBeforeYou", "", true},
	{"AlliedEnergyShield", "SoulLinkMitigation", "", "", false},
	{"FrostShieldLife", "FrostShieldDamageMitigation", "", "", false},
}

// allyPool is buildAllyLifePools' entry.
type allyPool struct{ remaining, percent float64 }

func buildAllyPools(out map[string]float64) map[string]*allyPool {
	pools := map[string]*allyPool{}
	for _, a := range allyLifePoolList {
		if life, m := out[a.life], out[a.mitigation]; life > 0 && m > 0 {
			pools[a.life] = &allyPool{remaining: life, percent: math.Min(m, 100) / 100}
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
		if out["LifeRecoverable"] < out["LifeUnreserved"] {
			out["CappingLife"] = 1 // CalcDefence.lua:2293
		}
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
	out["AnyBypass"] = 0 // CalcDefence.lua:2353-2370
	for _, t := range dmgTypes {
		if db.Flag("UnblockedDamageDoesBypassES") {
			out[t+"EnergyShieldBypass"] = 100
			out["AnyBypass"] = 1
		} else {
			v, ok := db.Override(t + "EnergyShieldBypass")
			if !ok {
				v = db.Sum("BASE", t+"EnergyShieldBypass")
			}
			if v != 0 {
				out["AnyBypass"] = 1
			}
			if t == "Chaos" {
				if !db.Flag("ChaosNotBypassEnergyShield") {
					v += 100
				} else {
					out["AnyBypass"] = 1
				}
			}
			out[t+"EnergyShieldBypass"] = v
		}
		out[t+"EnergyShieldBypass"] = math.Max(math.Min(out[t+"EnergyShieldBypass"], 100), 0)
		out["MinimumBypass"] = math.Min(out["MinimumBypass"], out[t+"EnergyShieldBypass"])
	}

	// Mind over Matter.
	out["ehpSectionAnySpecificTypes"] = 0
	out["OnlySharedMindOverMatter"], out["AnySpecificMindOverMatter"] = 0, 0
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
			out["ehpSectionAnySpecificTypes"], out["AnySpecificMindOverMatter"] = 1, 1
			out["OnlySharedMindOverMatter"] = 0
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
	out["AnyGuard"] = 0
	out["sharedGuardAbsorbRate"] = math.Min(db.Sum("BASE", "GuardAbsorbRate"), 100)
	if out["sharedGuardAbsorbRate"] > 0 {
		out["OnlySharedGuard"] = 1
		out["sharedGuardAbsorb"] = calcVal(db, "GuardAbsorbLimit")
	}
	for _, t := range dmgTypes {
		out[t+"GuardAbsorbRate"] = math.Min(db.Sum("BASE", t+"GuardAbsorbRate"), 100)
		if out[t+"GuardAbsorbRate"] > 0 {
			out["ehpSectionAnySpecificTypes"], out["AnyGuard"] = 1, 1
			out["OnlySharedGuard"] = 0
			out[t+"GuardAbsorb"] = calcVal(db, t+"GuardAbsorbLimit")
		}
	}

	// Aegis.
	out["sharedAegis"], _ = maxSource(db, "", "AegisValue")
	out["sharedElementalAegis"], _ = maxSource(db, "", "ElementalAegisValue")
	out["AnyAegis"] = b2f(out["sharedAegis"] > 0 || out["sharedElementalAegis"] > 0)
	if out["sharedElementalAegis"] > 0 {
		out["ehpSectionAnySpecificTypes"] = 1
	}
	for _, t := range dmgTypes {
		out[t+"Aegis"], _ = maxSource(db, "", t+"AegisValue")
		if out[t+"Aegis"] > 0 {
			out["ehpSectionAnySpecificTypes"], out["AnyAegis"] = 1, 1
		}
		if isElemental[t] {
			out[t+"AegisDisplay"] = out[t+"Aegis"] + out["sharedElementalAegis"]
		}
	}

	// Taken from allies before you: Frost Shield, then every ally redirect
	// (the life comes from allyLifeMods; minion life is not ported).
	out["FrostShieldLife"] = db.Sum("BASE", "FrostGlobeHealth")
	out["FrostShieldDamageMitigation"] = db.Sum("BASE", "FrostGlobeDamageMitigation")
	for _, a := range allyLifePoolList {
		if a.redirect == "" {
			continue
		}
		m := db.Sum("BASE", a.redirect)
		if a.meleeOnly {
			switch env.damageType {
			case "Melee":
			case "Average":
				m /= 4
			default:
				m = 0
			}
		}
		out[a.mitigation] = m
		if m != 0 {
			life := db.Sum("BASE", a.life)
			if life == 0 && a.fallback != "" {
				life = db.Sum("BASE", a.fallback)
			}
			if v, ok := db.Override(a.life); ok {
				life = v
			}
			out[a.life] = life
		}
	}
	// Companionship sharing a minion's life with another redirect
	// (MinionLifeShares...) needs minions, which are not ported.
	if out["TotemAllyDamageMitigation"] > 0 && out["TotalTotemLife"] == 0 && out["TotalVaalRejuvenationTotemLife"] > 0 {
		out["VaalRejuvenationTotemAllyDamageMitigation"] += out["TotemAllyDamageMitigation"]
		out["TotemAllyDamageMitigation"] = 0
		delete(out, "TotalTotemLife")
	}
	out["SoulLinkMitigation"] = db.Sum("BASE", "TakenFromParentESBeforeYou")
	if out["SoulLinkMitigation"] == 0 {
		out["SoulLinkMitigation"] = db.Sum("BASE", "TakenFromPartyMemberESBeforeYou")
		if out["SoulLinkMitigation"] != 0 {
			// CalcDefence.lua: a Soul Link from the party.
			out["AlliedEnergyShield"], _ = db.partyStat("EnergyShieldRecoveryCap")
		}
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
		for _, a := range allyLifePoolList {
			if ap := in.allies[a.life]; ap != nil && ap.remaining > 0 {
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
