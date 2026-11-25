package engine

import "math"

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
		for i := len(allyLifePoolList) - 1; i >= 0; i-- {
			life, m := out[allyLifePoolList[i].life], out[allyLifePoolList[i].mitigation]
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
			var hit float64
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

const (
	ehpCalcSpeedUp             = 8         // Data.lua data.misc.ehpCalcSpeedUp
	ehpCalcMaxDamage           = 100000000 // data.misc.ehpCalcMaxDamage
	ehpCalcMaxIterationsToCalc = 50        // data.misc.ehpCalcMaxIterationsToCalc
)
