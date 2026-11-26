package engine

import "math"

// lootBonuses is CalcPerform.lua:1511-1514, Bisco's Collar's quantity and
// rarity from normal and magic enemies.
func lootBonuses(db *ModDB) {
	out := db.output
	out["LootQuantityNormalEnemies"], out["LootRarityMagicEnemies"] = 0, 0
	if q := db.Sum("INC", "LootQuantityNormalEnemies"); q > 0 {
		out["LootQuantityNormalEnemies"] = q + db.Sum("INC", "LootQuantity")
	}
	if r := db.Sum("INC", "LootRarityMagicEnemies"); r > 0 {
		out["LootRarityMagicEnemies"] = r + db.Sum("INC", "LootRarity")
	}
}

// gemLevelQuality is CalcPerform.lua:3972-4013: the main skill gem's
// level and quality (GemLevel is activeEffect.level; GemQuality sums the
// base, support, item, global and socket quality mods).
func gemLevelQuality(db *ModDB, s *activeSkill) {
	if !s.active.hasSrc {
		return
	}
	out, list, cfg := db.output, s.mods, s.cfg
	out["GemHasLevel"], out["GemLevel"] = 1, float64(s.active.level)
	out["GemHasQuality"] = 1
	out["GemQuality"] = list.SumCfg(cfg, "BASE", "GemQuality") + list.SumCfg(cfg, "BASE", "GemSupportQuality") +
		list.SumCfg(cfg, "BASE", "GemItemQuality") + list.SumCfg(cfg, "BASE", "GemGlobalQuality") +
		list.SumCfg(cfg, "BASE", "GemSocketQuality")
}

// hexDoom is CalcPerform.lua:1380-1388: every hex curse (not Blasphemy's
// TotemCastsWhenNotDetached) raises HexDoomLimit to its MaxDoom and gets
// its configured doom as increased curse effect. Sum always returns a
// number, so the MaxDoom test in PoB's condition is always true.
func hexDoom(db *ModDB, skills []*activeSkill) {
	out := db.output
	for _, s := range skills {
		if !s.flags["hex"] || !s.flags["curse"] || s.types[stTotemCastsWhenNotDetached] {
			continue
		}
		stacks := db.Sum("BASE", "Multiplier:HexDoomStack")
		maxDoom := s.mods.Sum("BASE", "MaxDoom")
		effect := s.mods.More("DoomEffect")
		out["HexDoomLimit"] = math.Max(maxDoom, out["HexDoomLimit"])
		s.mods.NewMod("CurseEffect", "INC", math.Min(stacks, maxDoom)*effect, "Doom")
		db.multipliers["HexDoom"] = math.Min(math.Max(stacks, db.multipliers["HexDoom"]), out["HexDoomLimit"])
	}
}

// inherentRageLoss is CalcPerform.lua:934-935.
func inherentRageLoss(db *ModDB) {
	out := db.output
	out["InherentRageLossDelay"] = 2 + db.Sum("BASE", "InherentRageLossDelay")
	out["InherentRageLoss"] = 0
	if !db.Flag("InherentRageLossIsPrevented") {
		out["InherentRageLoss"] = 10 * (1 + db.Sum("INC", "InherentRageLoss")/100)
	}
}

// missingUnreservedLife is CalcDefence.lua:1228-1233.
func missingUnreservedLife(db *ModDB) {
	out := db.output
	missingLife := 0.0
	if db.GetCondition("AffectedByPetrifiedBlood") && out["LifeUnreserved"] > 0 {
		current := math.Min(out["LifeUnreserved"], out["Life"]*out["LowLifePercentage"]/100)
		missingLife = 100 * (out["LifeUnreserved"] - current) / out["LifeUnreserved"]
	}
	if o, set := db.Override("MissingUnreservedLifePercent"); set {
		missingLife = o
	}
	out["MissingUnreservedLifePercent"] = missingLife
}

// wardRechargeAndBlind is CalcDefence.lua:1551 (data.misc.WardRechargeDelay
// 2) and 1585-1587.
func wardRechargeAndBlind(db *ModDB) {
	out := db.output
	out["WardRechargeDelay"] = 2 / (1 + db.Sum("INC", "WardRechargeFaster")/100)
	if db.enemy.Flag("Blind") {
		out["BlindEffectMod"] = calcMod(db.enemy, nil, "BlindEffect", "BuffEffectOnSelf") * 100
	}
}

// rawArmourDefense is CalcDefence.lua:1637: nil when ArmourDefense is 0.
func rawArmourDefense(out map[string]float64) {
	if out["ArmourDefense"] > 0 {
		out["RawArmourDefense"] = (1 + out["ArmourDefense"]) * 100
	}
}

// selfDebuffs is CalcDefence.lua:1668-1699: debuff expiration, the
// durations and effects of ailments on the player, and Firesong's
// SelfIgniteDurationToElementalAilments.
func selfDebuffs(db *ModDB) {
	out, enemy := db.output, db.enemy
	out["DebuffExpirationRate"] = db.Sum("BASE", "SelfDebuffExpirationRate")
	out["DebuffExpirationModifier"] = 10000 / (100 + out["DebuffExpirationRate"])
	out["showDebuffExpirationModifier"] = b2f(out["DebuffExpirationModifier"] != 100)
	out["SelfBlindDuration"] = db.More("SelfBlindDuration") * (100 + db.Sum("INC", "SelfBlindDuration")) *
		out["DebuffExpirationModifier"] / 100
	igniteToAll := db.Flag("IgniteDurationAppliesToElementalAilments")
	if igniteToAll {
		if inc := db.Sum("INC", "SelfIgniteDuration"); inc != 0 {
			db.NewMod("SelfIgniteDurationToElementalAilments", "INC", inc, "Firesong")
		}
		if more := db.More("SelfIgniteDuration"); more != 1 {
			db.NewMod("SelfIgniteDurationToElementalAilments", "MORE", more, "Firesong")
		}
	}
	expiry := func(a string) float64 {
		return 100 + out["DebuffExpirationRate"] + db.Sum("BASE", "Self"+a+"DebuffExpirationRate")
	}
	for _, a := range []string{"Bleed", "Poison"} { // data.nonElementalAilmentTypeList
		more := db.More("Self"+a+"Duration", "SelfAilmentDuration")
		inc := (100 + db.Sum("INC", "Self"+a+"Duration", "SelfAilmentDuration")) * 100
		out["Self"+a+"Duration"] = inc * more / expiry(a)
	}
	for _, a := range elementalAilmentTypeList {
		all := igniteToAll && a != "Ignite"
		more := db.More("Self"+a+"Duration", "SelfAilmentDuration", "SelfElementalAilmentDuration")
		inc := 100 + db.Sum("INC", "Self"+a+"Duration", "SelfAilmentDuration", "SelfElementalAilmentDuration")
		if all {
			more *= db.More("SelfIgniteDuration")
			inc += db.Sum("INC", "SelfIgniteDuration")
		}
		out["Self"+a+"Duration"] = more * (inc * 100) / expiry(a)
	}
	for _, a := range ailmentTypeList {
		src := enemy
		if db.Flag("Condition:" + a + "edSelf") {
			src = db
		}
		out["Self"+a+"Effect"] = calcMod(db, nil, "Self"+a+"Effect") * calcMod(src, nil, "Enemy"+a+"Effect") * 100
	}
}

// totemResist is the totem half of the resistance loop (CalcDefence.lua:
// 600-611, 633-637): the Totem<Elem>Resist outputs from the player's
// Totem*Resist mods.
func totemResist(db *ModDB, elem string) {
	out := db.output
	maxNames, names := []string{"Totem" + elem + "ResistMax"}, []string{"Totem" + elem + "Resist"}
	if elem != "Chaos" {
		maxNames, names = append(maxNames, "TotemElementalResistMax"), append(names, "TotemElementalResist")
	}
	maxRes, ok := db.Override("Totem" + elem + "ResistMax")
	if !ok {
		maxRes = math.Min(maxResistCap, db.Sum("BASE", maxNames...))
	}
	total, ok := db.Override("Totem" + elem + "Resist")
	if !ok {
		total = db.Sum("BASE", names...) * math.Max(calcMod(db, nil, names...), 0)
	}
	total, maxRes = modfInt(total), modfInt(maxRes)
	final := math.Max(math.Min(total, maxRes), resistFloor)
	out["Totem"+elem+"Resist"] = final
	out["Totem"+elem+"ResistTotal"] = total
	out["Totem"+elem+"ResistOverCap"] = math.Max(0, total-maxRes)
	out["MissingTotem"+elem+"Resist"] = math.Max(0, maxRes-final)
}

// The stun constants of data.misc (Data.lua:275-278).
const (
	minStunChanceNeeded    = 20
	stunBaseMult           = 200
	stunBaseDuration       = 0.35
	stunNotMeleeDamageMult = 0.75
)

// stun is the "stun" block of buildDefenceEstimations (CalcDefence.lua:
// 2183-2276).
func stun(db *ModDB, category string) {
	out := db.output
	var base float64
	switch {
	case db.Flag("StunThresholdBasedOnEnergyShieldInsteadOfLife"):
		base = out["EnergyShield"] * db.Sum("BASE", "StunThresholdEnergyShieldPercent") / 100
	case db.Flag("StunThresholdBasedOnManaInsteadOfLife"):
		base = out["Mana"] * db.Sum("BASE", "StunThresholdManaPercent") / 100
	case db.Flag("ChaosInoculation"):
		base = db.Sum("BASE", "Life")
	default:
		base = out["Life"]
	}
	if db.Flag("AddESToStunThreshold") {
		base += out["EnergyShield"] * db.Sum("BASE", "ESToStunThresholdPercent") / 100
	}
	out["StunThreshold"] = base * (1 + db.Sum("INC", "StunThreshold")/100)
	notAvoid := 100 - math.Min(db.Sum("BASE", "AvoidStun"), 100)
	if db.Flag("StunImmune") {
		notAvoid = 0
	}
	if out["EnergyShield"] > out["totalTakenHit"] && !db.Flag("EnergyShieldProtectsMana") {
		notAvoid *= 0.5
	}
	out["StunAvoidChance"] = 100 - notAvoid
	if out["StunAvoidChance"] >= 100 {
		out["StunDuration"], out["BlockDuration"] = 0, 0
	} else {
		duration := 1 + db.Sum("INC", "StunDuration")/100
		recovery := 1 + db.Sum("INC", "StunRecovery")/100
		blockRecovery := 1 + db.Sum("INC", "StunRecovery", "BlockRecovery")/100
		out["StunDuration"] = math.Ceil(stunBaseDuration*duration/recovery*serverTickRate) / serverTickRate
		out["BlockDuration"] = math.Ceil(stunBaseDuration*duration/blockRecovery*serverTickRate) / serverTickRate
	}
	out["InterruptStunAvoidChance"] = math.Min(db.Sum("BASE", "AvoidInterruptStun"), 100)
	damage := out["totalTakenHit"] + out["PhysicalTakenHit"]*0.25
	// As PoB: "~= Average" first, so a Melee hit also takes the mixed
	// factor and only Average reaches the second branch.
	if category != "Average" {
		damage *= (1 + stunNotMeleeDamageMult*3) / 4
	} else if category != "Melee" {
		damage *= stunNotMeleeDamageMult
	}
	chance := math.Min(stunBaseMult*damage/out["StunThreshold"], 100)
	if chance <= minStunChanceNeeded {
		chance = 0
	}
	out["SelfStunChance"] = chance * notAvoid / 100
}

// pseudoRecoup is CalcDefence.lua:1519-1533: physical damage mitigated
// from hits regenerated as life, mana or energy shield.
func pseudoRecoup(db *ModDB) {
	out := db.output
	for _, r := range leechPools {
		if db.Flag("No"+r+"Regen") || db.Flag("CannotGain"+r) {
			continue
		}
		v := db.Sum("BASE", "PhysicalDamageMitigated"+r+"PseudoRecoup")
		if v <= 0 {
			continue
		}
		dur := db.Sum("BASE", "PhysicalDamageMitigated"+r+"PseudoRecoupDuration")
		if dur == 0 {
			dur = 4
		}
		out["PhysicalDamageMitigated"+r+"PseudoRecoupDuration"] = dur
		out["PhysicalDamageMitigated"+r+"PseudoRecoup"] = v * (1 + db.Sum("INC", r+"Regen")/100) * db.More(r+"Regen") *
			out[r+"RecoveryRateMod"]
		out["anyRecoup"] += out["PhysicalDamageMitigated"+r+"PseudoRecoup"]
	}
}

// recoupTypes is recoupTypeList (CalcDefence.lua:3019).
var recoupTypes = []string{"Life", "Mana", "EnergyShield"}

// recoupRecovery is CalcDefence.lua:3008-3134: recoup and pseudo recoup
// over the survival time, Petrified Blood's lost life and the net of the
// two.
func recoupRecovery(db *ModDB, category string) {
	out := db.output
	if out["anyRecoup"] > 0 && category != "DamageOverTime" {
		recoupRates(db)
	}
	// Petrified Blood "degen".
	_, lost := out["LifeLossLostOverTime"]
	_, lostHalf := out["LifeBelowHalfLossLostOverTime"]
	if out["preventedLifeLossTotal"] > 0 && lost && lostHalf {
		below := db.Sum("BASE", "LifeLossBelowHalfLost") / 100
		v := out["LifeLossLostOverTime"] + out["LifeBelowHalfLossLostOverTime"]*below
		out["LifeLossLostMax"] = v / 4
		out["LifeLossLostAvg"] = v / (out["EHPSurvivalTime"] + 4)
	}
	// Net recovery over time from enemy hits.
	if out["LifeRecoupRecoveryAvg"] > 0 || out["preventedLifeLossTotal"] > 0 {
		out["netLifeRecoupAndLossLostOverTimeMax"] = out["LifeRecoupRecoveryMax"] - out["LifeLossLostMax"]
		out["netLifeRecoupAndLossLostOverTimeAvg"] = out["LifeRecoupRecoveryAvg"] - out["LifeLossLostAvg"]
		if out["LifeRecoupRecoveryAvg"] > 0 && out["preventedLifeLossTotal"] > 0 {
			out["showNetRecoup"] = 1
		}
	}
}

// recoupRates is the "recoup" block (CalcDefence.lua:3008-3094).
func recoupRates(db *ModDB) {
	out := db.output
	var total, totalEle float64
	mitigated := out["NumberOfMitigatedDamagingHits"] * (out["PhysicalTakenDamage"] - out["PhysicalTakenHit"])
	for _, t := range dmgTypes {
		total += out[t+"RecoupableDamageTaken"]
		if isElemental[t] {
			totalEle += out[t+"RecoupableDamageTaken"]
		}
	}
	extra := map[string][2]float64{}
	for i, r := range recoupTypes {
		recoupTime := 4.0
		if db.Flag("3Second"+r+"Recoup") || db.Flag("3SecondRecoup") {
			recoupTime = 3
		}
		rec := recoupTotal(out, r, total, totalEle)
		out["Total"+r+"RecoupRecovery"] = rec
		pseudo := out["PhysicalDamageMitigated"+r+"PseudoRecoup"] / 100 * mitigated
		duration := outOr(out, "PhysicalDamageMitigated"+r+"PseudoRecoupDuration", 4)
		if pseudo != 0 {
			piousPath(db, recoupTypes[i:], pseudo, duration, extra)
		}
		if db.Flag("UnaffectedBy" + r + "Regen") {
			pseudo = 0
		}
		out["Total"+r+"PseudoRecoup"] = pseudo
		surv := out["EHPSurvivalTime"]
		maxRate := rec/recoupTime + pseudo/duration
		avgRate := rec/(surv+recoupTime) + pseudo/(surv+duration)
		if e, ok := extra[r]; ok {
			maxRate += e[0] / e[1]
			avgRate += e[0] / (surv + e[1])
		}
		out[r+"RecoupRecoveryMax"], out[r+"RecoupRecoveryAvg"] = maxRate, avgRate
	}
}

// recoupTotal is Total<r>RecoupRecovery (CalcDefence.lua:3022-3030).
func recoupTotal(out map[string]float64, r string, total, totalEle float64) float64 {
	rec := out[r+"Recoup"] / 100 * total
	if out["Elemental"+r+"Recoup"] > 0 && totalEle > 0 {
		rec += out["Elemental"+r+"Recoup"] / 100 * totalEle
	}
	for _, t := range dmgTypes {
		if out[t+r+"Recoup"] > 0 && out[t+"RecoupableDamageTaken"] > 0 {
			rec += out[t+r+"Recoup"] / 100 * out[t+"RecoupableDamageTaken"]
		}
	}
	return rec
}

// dotEHPAndDegens is CalcDefence.lua:3401-3546: the EHP against each
// damage-over-time type, the build's own degens and the net regeneration.
func dotEHPAndDegens(db *ModDB, est *defenceEstimations) {
	out := db.output
	for _, t := range dmgTypes {
		out[t+"DotEHP"] = out[t+"TotalPool"] / out[t+"TakenDotMult"]
	}
	totalDegen := 0.0
	degen := map[string]float64{}
	for _, t := range dmgTypes {
		base := db.Sum("BASE", t+"Degen")
		if base <= 0 {
			continue
		}
		// pairs over damageOverTimeShiftTable; the types in list order.
		for _, to := range dmgTypes {
			if pct := est.dotShift[t][to]; pct > 0 {
				v := base * (pct / 100) * out[to+"TakenDotMult"]
				degen[to] += v
				out[to+"BuildDegen"] = degen[to]
				totalDegen += v
			}
		}
	}
	if totalDegen == 0 {
		return // TotalBuildDegen = nil
	}
	out["TotalBuildDegen"] = totalDegen
	lifeNet, manaNet, esNet := out["LifeRegenRecovery"], out["ManaRegenRecovery"], out["EnergyShieldRegenRecovery"]
	var lifeD, manaD, esD float64
	for _, t := range dmgTypes {
		d, ok := degen[t]
		if !ok {
			continue
		}
		fromMana := (out[t+"MindOverMatter"] + out["sharedMindOverMatter"]) / 100
		bypass := out[t+"EnergyShieldBypass"] / 100
		var l, m, e float64
		if out["EnergyShieldRegenRecovery"] > 0 {
			if db.Flag("EnergyShieldProtectsMana") {
				l = d * (1 - fromMana)
				e = d * (1 - bypass) * fromMana
			} else {
				l = d * bypass * (1 - fromMana)
				e = d * (1 - bypass)
			}
			m = d * bypass * fromMana
		} else {
			l = d * (1 - fromMana)
			m = d * fromMana
		}
		lifeD, manaD, esD = lifeD+l, manaD+m, esD+e
	}
	out["NetLifeRegen"], out["NetManaRegen"], out["NetEnergyShieldRegen"] = lifeNet-lifeD, manaNet-manaD, esNet-esD
	out["TotalNetRegen"] = out["NetLifeRegen"] + out["NetManaRegen"] + out["NetEnergyShieldRegen"]
}

// piousPath is CalcDefence.lua:3034-3040: a pseudo recoup of rs[0] that
// regeneration of a later type also recovers.
func piousPath(db *ModDB, rs []string, pseudo, duration float64, extra map[string][2]float64) {
	out, r := db.output, rs[0]
	for _, r2 := range rs[1:] {
		if db.Flag(r+"RegenerationRecovers"+r2) && !db.Flag("UnaffectedBy"+r2+"Regen") &&
			!db.Flag("No"+r2+"Regen") && !db.Flag("CannotGain"+r2) {
			extra[r2] = [2]float64{pseudo * out[r2+"RecoveryRateMod"] / out[r+"RecoveryRateMod"], duration}
		}
	}
}

// The trap and mine radii of data.misc (Data.lua:269-271).
const (
	trapTriggerRadiusBase    = 10
	mineDetonationRadiusBase = 60
	mineAuraRadiusBase       = 35
)

// skillTypeOutputs are the skill type outputs of calcs.offence that the
// engine does not compute elsewhere: CalcOffence.lua:1190-1191, 1216-1217, 1249-1250 (effect mods), 1314-1317,
// 1398-1408 (trap and mine radii), 1336-1340 (stored uses), 1441-1445
// (totem defences), 1527-1531, 1611-1617, 1626-1629 (durations).
func skillTypeOutputs(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, cfg, data := s.mods, s.cfg, s.data
	if s.types[stAura] {
		names := []string{"AuraEffect"}
		if !truthy(data.val("auraCannotAffectSelf")) && !s.types[stAuraAffectsEnemies] {
			names = append(names, "SkillAuraEffectOnSelf")
		}
		out["AuraEffectMod"] = calcMod(list, cfg, names...)
	}
	if s.types[stHex] || s.types[stMark] {
		out["CurseEffectMod"] = calcMod(list, cfg, "CurseEffect")
	}
	if s.types[stBuff] && s.types[stHerald] {
		out["HeraldBuffEffectMod"] = calcMod(list, cfg, "BuffEffect", "BuffEffectOnSelf")
	}
	if s.flags["trap"] {
		r := calcRadius(trapTriggerRadiusBase, areaModOf(list, cfg, "TrapTriggerAreaOfEffect"), 1)
		out["TrapTriggerRadius"], out["TrapTriggerRadiusMetre"] = r, r/10
	}
	if v, ok := data.storedUses.get(); ok {
		out["StoredUses"] = v + list.SumCfg(cfg, "BASE", "AdditionalCooldownUses", "AdditionalUses")
	}
	if s.flags["mine"] {
		r := calcRadius(mineDetonationRadiusBase, areaModOf(list, cfg, "MineDetonationAreaOfEffect"), 1)
		out["MineDetonationRadius"], out["MineDetonationRadiusMetre"] = r, r/10
		if s.types[stAura] {
			r = calcRadius(mineAuraRadiusBase, out["AreaOfEffectMod"], 1)
			out["MineAuraRadius"], out["MineAuraRadiusMetre"] = r, r/10
		}
	}
	if s.flags["totem"] {
		out["TotemLifeMod"] = calcMod(list, cfg, "TotemLife")
		out["TotemEnergyShield"] = list.SumCfg(cfg, "BASE", "TotemEnergyShield")
		out["TotemBlockChance"] = list.SumCfg(cfg, "BASE", "TotemBlockChance")
		out["TotemArmour"] = list.SumCfg(cfg, "BASE", "TotemArmour")
	}
	skillDurations(actor, s, out)
}

// areaModOf is calcAreaMod(calcLib.mods(list, cfg, name)): calcLib.mods
// gives 1 + INC/100 and MORE.
func areaModOf(list *ModDB, cfg *Cfg, name string) float64 {
	return calcAreaMod(1+list.SumCfg(cfg, "INC", name)/100, list.MoreCfg(cfg, name))
}

// skillDurations is the secondary, soul gain prevention and totem
// durations of the "Skill duration" block (CalcOffence.lua:1527-1629).
func skillDurations(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, cfg, data := s.mods, s.cfg, s.data
	mine := truthy(data.val("mineDurationAppliesToSkill"))
	with := func(names []string, extra string, ok bool) []string {
		if ok {
			return append(names, extra)
		}
		return names
	}
	sec, _ := num(data.val("durationSecondary"))
	if base := sec + list.SumCfg(cfg, "BASE", "Duration", "SecondaryDuration"); base > 0 {
		mod := math.Max(calcMod(list, cfg, with([]string{"Duration", "SecondaryDuration"}, "MineDuration", mine)...), 0)
		out["DurationSecondary"] = base * mod
		if truthy(data.val("debuffSecondary")) && modeEffective(actor) {
			out["DurationSecondary"] /= math.Max(0.25, calcMod(actor.enemy, cfg, "BuffExpireFaster"))
		}
		out["DurationSecondary"] = math.Ceil(out["DurationSecondary"]*serverTickRate) / serverTickRate
	}
	if base, _ := num(data.val("soulPreventionDuration")); base > 0 {
		names := with([]string{"SoulGainPreventionDuration"}, "Duration",
			truthy(data.val("skillEffectAppliesToSoulGainPrevention")))
		mod := math.Max(calcMod(list, cfg, with(names, "MineDuration", mine)...), 0)
		out["SoulGainPreventionDuration"] = math.Max(math.Ceil(base*mod*serverTickRate), 1) / serverTickRate
	}
	out["TotemDurationMod"] = math.Max(calcMod(list, cfg, "TotemDuration"), 0)
	out["TotemDuration"] = math.Ceil(list.SumCfg(cfg, "BASE", "TotemDuration")*out["TotemDurationMod"]*
		serverTickRate) / serverTickRate
}
