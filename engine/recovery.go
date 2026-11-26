package engine

import (
	"math"
	"strings"
)

// Data.lua data.misc values used by the defence outputs below.
const (
	suppressionChanceCap     = 100
	suppressionEffect        = 40
	dodgeChanceCap           = 75
	manaRegenBase            = 105.0 / 60 / 100  // mana_regeneration_rate_per_minute_%
	energyShieldRechargeBase = 2000.0 / 60 / 100 // energy_shield_recharge_rate_per_minute_%
	energyShieldRechargeWait = 2                 // EnergyShieldRechargeDelay
	damageReductionCap       = 90                // maximum_physical_damage_reduction_%
	avoidChanceCap           = 75                // Data.lua data.misc.AvoidChanceCap
	temporalChainsEffectCap  = 75
)

// avoidanceAndRecovery ports the parts of calcs.defence after the primary
// defences: spell suppression, dodge, recovery rate modifiers,
// regeneration, energy shield recharge, base damage reduction and
// movement speed. Evasion chances need the enemy's accuracy table and are
// not ported.
func avoidanceAndRecovery(db *ModDB) {
	out := db.output
	enemy := db.enemy

	// Spell suppression.
	suppression := db.Sum("BASE", "SpellSuppressionChance")
	total := suppression
	if o, ok := db.Override("SpellSuppressionChance"); ok {
		total = o
	}
	if db.Flag("ConvertSpellSuppressionToSpellDodge") {
		db.NewMod("SpellDodgeChance", "BASE", suppression/2, "Acrobatics")
	}
	out["SpellSuppressionChance"] = math.Min(total, suppressionChanceCap)
	out["SpellSuppressionEffect"] = math.Max(suppressionEffect+db.Sum("BASE", "SpellSuppressionEffect"), 0)
	out["EffectiveSpellSuppressionChance"] = out["SpellSuppressionChance"]
	if enemy != nil && enemy.Flag("CannotBeSuppressed") {
		out["EffectiveSpellSuppressionChance"] = 0
	}
	// CalcDefence.lua:1166-1177: lucky or unlucky suppression only with
	// env.mode_effective.
	rolls := 0
	if modeEffective(db) {
		if db.Flag("SpellSuppressionChanceIsLucky") {
			rolls++
		}
		if db.Flag("SpellSuppressionChanceIsUnlucky") {
			rolls--
		}
		if db.Flag("ExtremeLuck") {
			rolls *= 2
		}
	}
	out["EffectiveSpellSuppressionChance"] = luckyRolls(db, out["EffectiveSpellSuppressionChance"], float64(rolls))
	out["SpellSuppressionChanceOverCap"] = math.Max(0, total-suppressionChanceCap)

	// Dodge.
	attackDodge := db.Sum("BASE", "AttackDodgeChance")
	spellDodge := db.Sum("BASE", "SpellDodgeChance")
	spellDodgeMax, ok := db.Override("SpellDodgeChanceMax")
	if !ok {
		spellDodgeMax = db.Sum("BASE", "SpellDodgeChanceMax")
	}
	out["AttackDodgeChance"] = math.Min(attackDodge, dodgeChanceCap)
	out["SpellDodgeChance"] = math.Min(spellDodge, spellDodgeMax)
	reduce := 0.0
	cannot := false
	if enemy != nil {
		reduce, cannot = enemy.Sum("BASE", "reduceEnemyDodge"), enemy.Flag("CannotBeDodged")
	}
	out["EffectiveAttackDodgeChance"], out["EffectiveSpellDodgeChance"] = 0, 0
	if !cannot {
		out["EffectiveAttackDodgeChance"] = math.Min(math.Max(attackDodge-reduce, 0), dodgeChanceCap)
		out["EffectiveSpellDodgeChance"] = math.Min(math.Max(spellDodge-reduce, 0), spellDodgeMax)
	}
	// CalcDefence.lua:1207: unlucky dodge only with env.mode_effective.
	if modeEffective(db) && db.Flag("DodgeChanceIsUnlucky") {
		out["EffectiveAttackDodgeChance"] *= out["EffectiveAttackDodgeChance"] / 100
		out["EffectiveSpellDodgeChance"] *= out["EffectiveSpellDodgeChance"] / 100
	}
	out["AttackDodgeChanceOverCap"] = math.Max(0, attackDodge-dodgeChanceCap)
	out["SpellDodgeChanceOverCap"] = math.Max(0, spellDodge-spellDodgeMax)

	missingUnreservedLife(db)

	// Recovery rate modifiers.
	out["LifeRecoveryRateMod"] = 1
	if !db.Flag("CannotRecoverLifeOutsideLeech") {
		out["LifeRecoveryRateMod"] = calcMod(db, nil, "LifeRecoveryRate")
	}
	out["ManaRecoveryRateMod"] = calcMod(db, nil, "ManaRecoveryRate")
	out["EnergyShieldRecoveryRateMod"] = calcMod(db, nil, "EnergyShieldRecoveryRate")
	leechCaps(db)

	// Regeneration.
	resources := []string{"Mana", "Life", "Energy Shield", "Rage"}
	for i, display := range resources {
		res := strings.ReplaceAll(display, " ", "")
		pool := out[res]
		inc := db.Sum("INC", res+"Regen")
		more := db.More(res + "Regen")
		rateMod, ok := out[res+"RecoveryRateMod"]
		if !ok {
			rateMod = 1
		}
		regenRate := 0.0
		switch {
		case db.Flag("No"+res+"Regen") || db.Flag("CannotGain"+res):
			out[res+"Regen"] = 0
		case res == "Life" && db.Flag("ZealotsOath"):
			out["LifeRegen"] = 0
			if b := db.Sum("BASE", "LifeRegen"); b > 0 {
				db.NewMod("EnergyShieldRegen", "BASE", b, "Zealot's Oath")
			}
			if p := db.Sum("BASE", "LifeRegenPercent"); p > 0 {
				db.NewMod("EnergyShieldRegenPercent", "BASE", p, "Zealot's Oath")
			}
		default:
			if inc != 0 {
				for _, next := range resources[i+1:] {
					n := strings.ReplaceAll(next, " ", "")
					if db.Flag(res + "RegenTo" + n + "Regen") {
						db.NewMod(n+"Regen", "INC", inc, display+" instead applies to "+next)
						inc = 0
					}
				}
			}
			if res == "Life" {
				if conv := db.Sum("BASE", "LifeRegenAppliesToEnergyShield"); conv > 0 {
					f := math.Min(conv, 100) / 100
					db.NewMod("EnergyShieldRegen", "BASE", floorTo(db.Sum("BASE", "LifeRegen")*f, 2), "Life Regen to ES Regen")
					db.NewMod("EnergyShieldRegenPercent", "BASE", floorTo(db.Sum("BASE", "LifeRegenPercent")*f, 2), "Life Regen to ES Regen")
				}
			}
			base := db.Sum("BASE", res+"Regen") + pool*db.Sum("BASE", res+"RegenPercent")/100
			regen := base * (1 + inc/100) * more
			if regen != 0 {
				for _, next := range resources[i+1:] {
					n := strings.ReplaceAll(next, " ", "")
					if db.Flag(res + "RegenerationRecovers" + n) {
						db.NewMod(n+"Recovery", "BASE", regen, display+" Regeneration Recovers "+next)
					}
				}
			}
			regenRate = round(regen*rateMod, 1)
			out[res+"Regen"] = regenRate
		}
		out[res+"RegenInc"] = inc
		degen := db.Sum("BASE", res+"Degen") + pool*db.Sum("BASE", res+"DegenPercent")/100
		tincture := db.Sum("BASE", res+"DegenPercentTincture")
		degen += math.Max(pool*tincture/100, tincture)
		degenRate := 0.0
		if degen > 0 {
			degenRate = degen * calcMod(db, nil, res+"Degen")
		}
		out[res+"Degen"] = degenRate
		recovery := db.Sum("BASE", res+"Recovery") * rateMod
		out[res+"Recovery"] = recovery
		regenPart := regenRate
		if db.Flag("UnaffectedBy" + res + "Regen") {
			regenPart = 0
		}
		out[res+"RegenRecovery"] = regenPart - degenRate + recovery
		if out[res+"RegenRecovery"] > 0 {
			db.Add(Mod{Name: "Condition:CanGain" + res, Type: "FLAG", Value: true, Source: display + "Regen"})
		}
		out[res+"RegenPercent"] = 0
		if pool > 0 {
			out[res+"RegenPercent"] = round(out[res+"RegenRecovery"]/pool*100, 1)
		}
	}

	// Energy shield recharge.
	toLife := db.Flag("EnergyShieldRechargeAppliesToLife") && !db.Flag("CannotRecoverLifeOutsideLeech")
	toES := !(db.Flag("NoEnergyShieldRecharge") || db.Flag("CannotGainEnergyShield") || toLife)
	// CalcDefence.lua:1367-1368: Flag(...) and ... is nil (no key) when the
	// first flag is unset.
	if db.Flag("EnergyShieldRechargeAppliesToLife") {
		out["EnergyShieldRechargeAppliesToLife"] = b2f(toLife)
	}
	out["EnergyShieldRechargeAppliesToEnergyShield"] = b2f(toES)
	if toLife || toES {
		inc, more := db.Sum("INC", "EnergyShieldRecharge"), db.More("EnergyShieldRecharge")
		base, ok := db.Override("EnergyShieldRecharge")
		if !ok {
			base = energyShieldRechargeBase
		}
		if toLife {
			out["LifeRecharge"] = round(out["Life"]*base*(1+inc/100)*more*out["LifeRecoveryRateMod"], 0)
		} else {
			out["EnergyShieldRecharge"] = round(out["EnergyShield"]*base*(1+inc/100)*more*out["EnergyShieldRecoveryRateMod"], 0)
		}
		out["EnergyShieldRechargeDelay"] = energyShieldRechargeWait / (1 + db.Sum("INC", "EnergyShieldRechargeFaster")/100)
	} else {
		out["EnergyShieldRecharge"] = 0
	}

	// Base damage reduction.
	drMax, ok := db.Override("DamageReductionMax")
	if !ok {
		drMax = damageReductionCap
	}
	out["DamageReductionMax"] = drMax
	db.NewMod("ArmourAppliesToPhysicalDamageTaken", "BASE", 100, "")
	for _, t := range []string{"Physical", "Lightning", "Cold", "Fire", "Chaos"} {
		names := []string{t + "DamageReduction"}
		if t == "Lightning" || t == "Cold" || t == "Fire" {
			names = append(names, "ElementalDamageReduction")
		}
		v, ok := db.Override(t + "DamageReduction")
		if !ok {
			v = math.Min(math.Max(0, db.Sum("BASE", names...)), drMax)
		}
		out["Base"+t+"DamageReduction"] = v
		w, ok := db.Override(t + "DamageReduction")
		if !ok {
			w = math.Min(math.Max(0, v+db.Sum("BASE", t+"DamageReductionWhenHit")), drMax)
		}
		out["Base"+t+"DamageReductionWhenHit"] = w
	}

	// Movement speed.
	if o, ok := db.Override("MovementSpeed"); ok {
		out["MovementSpeedMod"] = o
	} else if v, has := db.partyStat("MovementSpeedMod"); has && db.Flag("MovementSpeedEqualHighestLinkedPlayers") {
		// CalcDefence.lua: the party member's MovementSpeedMod (PartyMemberStats).
		out["MovementSpeedMod"] = v
	} else {
		out["MovementSpeedMod"] = calcMod(db, nil, "MovementSpeed")
	}
	if db.Flag("MovementSpeedCannotBeBelowBase") {
		out["MovementSpeedMod"] = math.Max(out["MovementSpeedMod"], 1)
	}
	out["EffectiveMovementSpeedMod"] = out["MovementSpeedMod"] * out["ActionSpeedMod"]

	wardRechargeAndBlind(db)

	// Recovery on block.
	out["LifeOnBlock"], out["LifeOnSuppress"] = 0, 0
	if !db.Flag("CannotRecoverLifeOutsideLeech") {
		out["LifeOnBlock"] = db.Sum("BASE", "LifeOnBlock")
		out["LifeOnSuppress"] = db.Sum("BASE", "LifeOnSuppress")
	}
	out["ManaOnBlock"] = db.Sum("BASE", "ManaOnBlock")
	out["EnergyShieldOnBlock"] = db.Sum("BASE", "EnergyShieldOnBlock")
	out["EnergyShieldOnSpellBlock"] = db.Sum("BASE", "EnergyShieldOnSpellBlock")
	out["EnergyShieldOnSuppress"] = db.Sum("BASE", "EnergyShieldOnSuppress")

	// Damage and hit avoidance.
	specific := false
	for _, t := range dmgTypes {
		out["Avoid"+t+"DamageChance"] = math.Min(db.Sum("BASE", "Avoid"+t+"DamageChance"), avoidChanceCap)
		specific = specific || out["Avoid"+t+"DamageChance"] > 0
	}
	out["specificTypeAvoidance"] = 0 // a boolean in PoB
	if specific {
		out["specificTypeAvoidance"] = 1
	}
	out["AvoidProjectilesChance"] = math.Min(db.Sum("BASE", "AvoidProjectilesChance"), avoidChanceCap)
	out["AvoidAllDamageFromHitsChance"] = math.Min(db.Sum("BASE", "AvoidAllDamageFromHitsChance"), avoidChanceCap)
	immune := func(flag ...string) bool { return db.Flag(flag...) }
	capped := func(v float64) float64 { return math.Min(v, 100) }
	out["BlindAvoidChance"] = capped(db.Sum("BASE", "AvoidBlind"))
	if immune("BlindImmune") {
		out["BlindAvoidChance"] = 100
	}
	out["ImpaleAvoidChance"] = capped(db.Sum("BASE", "AvoidImpale"))
	if immune("ImpaleImmune") {
		out["ImpaleAvoidChance"] = 100
	}
	// CalcDefence.lua:1618-1621.
	for _, x := range []string{"CorruptedBlood", "Maim", "Hinder", "Knockback"} {
		if db.Flag(x + "Immune") { // a Flag: nil when unset
			out[x+"Immunity"] = 1
		}
	}
	if db.Flag("SpellSuppressionAppliesToAilmentAvoidance") {
		pct := db.Sum("BASE", "SpellSuppressionAppliesToAilmentAvoidancePercent") / 100
		// The uncapped sum (CalcDefence's local spellSuppressionChance).
		db.NewMod("AvoidElementalAilments", "BASE", math.Floor(pct*db.Sum("BASE", "SpellSuppressionChance")), "Ancestral Vision")
	}
	out["LightRadiusMod"] = calcMod(db, nil, "LightRadius")
	out["LightRadiusInc"] = math.Max(db.Sum("INC", "LightRadius"), 0)
	for _, a := range []string{"Bleed", "Poison"} {
		out[a+"AvoidChance"] = math.Floor(capped(db.Sum("BASE", "Avoid"+a, "AvoidAilments")))
		if immune(a + "Immune") {
			out[a+"AvoidChance"] = 100
		}
	}
	for _, a := range []string{"Ignite", "Chill", "Freeze", "Shock", "Scorch", "Brittle", "Sap"} {
		v := db.Sum("BASE", "Avoid"+a, "AvoidAilments", "AvoidElementalAilments")
		if db.Flag("ShockAvoidAppliesToElementalAilments") && a != "Shock" {
			v += db.Sum("BASE", "AvoidShock")
		}
		out[a+"AvoidChance"] = math.Floor(capped(v))
		if immune(a+"Immune", "ElementalAilmentImmune") {
			out[a+"AvoidChance"] = 100
		}
	}
	out["CurseAvoidChance"] = capped(db.Sum("BASE", "AvoidCurse"))
	if immune("CurseImmune") {
		out["CurseAvoidChance"] = 100
	}
	out["SilenceAvoidChance"] = out["CurseAvoidChance"]
	if immune("SilenceImmune") {
		out["SilenceAvoidChance"] = 100
	}
	ad, _ := maxSource(db, "", "ArmourDefense")
	out["ArmourDefense"] = ad / 100
	rawArmourDefense(out)
	out["CritExtraDamageReduction"] = capped(db.Sum("BASE", "ReduceCritExtraDamage"))
	out["CurseEffectOnSelf"] = math.Max(db.More("CurseEffectOnSelf")*(100+db.Sum("INC", "CurseEffectOnSelf")), 0)
	out["ExposureEffectOnSelf"] = db.More("ExposureEffectOnSelf") * (100 + db.Sum("INC", "ExposureEffectOnSelf"))
	out["WitherEffectOnSelf"] = db.More("WitherEffectOnSelf") * (100 + db.Sum("INC", "WitherEffectOnSelf"))
	selfDebuffs(db)
}

// luckyRolls applies CalcDefence.lua's lucky/unlucky rolls to a chance in
// percent, with the EHPUnluckyWorstOf config overriding the rolls.
func luckyRolls(db *ModDB, chance, rolls float64) float64 {
	if e := db.enemyEnv; e != nil && e.worstOf != 1 {
		rolls = -e.worstOf / 2
	}
	if rolls == 0 {
		return chance
	}
	p := chance / 100
	switch {
	case db.Flag("Unexciting"):
		return (3*p*p - 2*p*p*p) * 100
	case rolls > 0:
		return (1 - math.Pow(1-p, rolls+1)) * 100
	}
	return math.Pow(p, -rolls) * chance
}

// recoup is the recoup part of calcs.defence: the Recoup outputs and
// anyRecoup, which the EHP estimation reads.
func recoup(db *ModDB) {
	out := db.output
	out["anyRecoup"] = 0
	types := []string{"Life", "Mana", "EnergyShield"}
	inc := 1 + db.Sum("INC", "RecoupRecoveryAmount")/100
	more := db.More("RecoupRecoveryAmount")
	for _, a := range types {
		for _, b := range types {
			if a == b {
				continue
			}
			if flag := "Add" + a + "RecoupTo" + b + "Recoup"; db.Flag(flag) {
				source := ""
				for _, r := range db.tabulateType(flag, "FLAG") {
					source = r.Source
					break
				}
				db.NewMod(b+"Recoup", "BASE", db.Sum("BASE", a+"Recoup"), source)
			}
		}
	}
	lifeBlocked := func(t string) bool {
		return t == "Life" && (db.Flag("CannotRecoupLife") || db.Flag("EnergyShieldRecoupInsteadOfLife"))
	}
	for _, t := range types {
		switch {
		case t == "Life" && db.Flag("CannotRecoupLife"):
			out["LifeRecoup"] = 0
		case t == "Life" && db.Flag("EnergyShieldRecoupInsteadOfLife"):
			out["LifeRecoup"] = 0
			db.NewMod("EnergyShieldRecoup", "BASE", db.Sum("BASE", "LifeRecoup"), "Life Recoup Conversion")
		default:
			out[t+"Recoup"] = db.Sum("BASE", t+"Recoup") * out[t+"RecoveryRateMod"] * inc * more
			out["anyRecoup"] += out[t+"Recoup"]
		}
	}
	if db.Flag("UsePowerCharges") && db.Flag("PowerChargesConvertToAbsorptionCharges") {
		v := db.Sum("BASE", "PerAbsorptionElementalEnergyShieldRecoup")
		for _, e := range []string{"Cold", "Fire", "Lightning"} {
			db.NewMod(e+"EnergyShieldRecoup", "BASE", v, "Absorption Charges", map[string]any{"type": "Multiplier", "var": "AbsorptionCharge"})
		}
	}
	for _, t := range types {
		for _, d := range dmgTypes {
			name := d + t + "Recoup"
			v := db.Sum("BASE", name)
			if lifeBlocked(t) {
				out[name] = 0
				if db.Flag("EnergyShieldRecoupInsteadOfLife") && !db.Flag("CannotRecoupLife") {
					db.NewMod(d+"EnergyShieldRecoup", "BASE", v, "Life Recoup Conversion")
				}
				continue
			}
			out[name] = v * out[t+"RecoveryRateMod"] * inc * more
			out["anyRecoup"] += out[name]
		}
	}
}

var dmgTypes = []string{"Physical", "Lightning", "Cold", "Fire", "Chaos"} // CalcDefence.lua dmgTypeList

// actionSpeedMod is calcs.actionSpeedMod (CalcPerform.lua).
func actionSpeedMod(db *ModDB) float64 {
	minimum, _ := maxSource(db, "", "MinimumActionSpeed")
	mod := 1 + (math.Max(-temporalChainsEffectCap, db.Sum("INC", "TemporalChainsActionSpeed"))+db.Sum("INC", "ActionSpeed"))/100
	mod = math.Max(minimum/100, mod)
	if r, ok := maxSource(db, "", "MaximumActionSpeedReduction"); ok {
		mod = math.Min((100-r)/100, mod)
	}
	return mod
}
