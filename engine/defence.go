package engine

import (
	"math"

	"github.com/Mi7teR/poe-lib/modparser"
)

const blockChanceCap = 90 // Data.lua data.misc.BlockChanceCap

// calcMod is calcLib.mod: (1 + increased/100) * more.
func calcMod(db *ModDB, cfg *Cfg, names ...string) float64 {
	return (1 + db.SumCfg(cfg, "INC", names...)/100) * db.MoreCfg(cfg, names...)
}

// defenceSlots are the slots whose armour data counts (calcs.defence).
var defenceSlots = []string{"Helmet", "Gloves", "Boots", "Body Armour", "Weapon 2", "Weapon 3"}

// defenceForConditionals is calcs.defenceForConditionals: each armour
// slot's defences as <Defence>On<Slot> outputs, read by PerStat tags.
func defenceForConditionals(db *ModDB, gearMods map[string][]modparser.Mod) {
	for _, slot := range defenceSlots {
		it := db.items[slot]
		if it == nil || it.Base == nil || it.Base.Armour == nil {
			continue
		}
		d := itemArmour(it, gearMods[slot])
		for _, x := range []struct {
			name string
			v    float64
		}{{"Ward", d.Ward}, {"EnergyShield", d.EnergyShield}, {"Armour", d.Armour}, {"Evasion", d.Evasion}} {
			if !db.Flag("GainNo"+x.name+"From"+slot) && x.v > 0 {
				db.output[x.name+"On"+slot] = x.v
			}
		}
	}
}

// defence ports the block and primary defence parts of calcs.defence
// (Modules/CalcDefence.lua): block chances, then armour, evasion, energy
// shield and ward from gear (per slot, with the slot's cfg), global bases
// and conversions.
func (c *Calculator) defence(db *ModDB, gearMods map[string][]modparser.Mod, keystones map[string]bool) {
	out := db.output
	out["ActionSpeedMod"] = actionSpeedMod(db)
	armour := map[string]armourData{}
	for _, slot := range defenceSlots {
		if it := db.items[slot]; it != nil && it.Base != nil && it.Base.Armour != nil {
			armour[slot] = itemArmour(it, gearMods[slot])
		}
	}

	// Block.
	out["BlockChanceMax"] = math.Min(db.Sum("BASE", "BlockChanceMax"), blockChanceCap)
	if db.Flag("MaximumBlockAttackChanceIsEqualToPartyMember") {
		// CalcDefence.lua: a Protective Link from the party.
		out["BlockChanceMax"], _ = db.partyStat("BlockChanceMax")
	}
	baseBlock := armour["Weapon 2"].BlockChance + armour["Weapon 3"].BlockChance
	out["ShieldBlockChance"] = baseBlock
	if !keystones["Necromantic Aegis"] {
		if o, ok := db.Override("ReplaceShieldBlock"); ok {
			baseBlock = o
		}
	}
	blockInc, blockMore := db.Sum("INC", "BlockChance"), db.More("BlockChance")
	switch {
	case db.Flag("BlockAttackChanceIsEqualToPartyMember"):
		v, _ := db.partyStat("BlockChance")
		out["BlockChance"] = math.Min(v, out["BlockChanceMax"])
	case db.Flag("MaxBlockIfNotBlockedRecently"):
		out["BlockChance"] = out["BlockChanceMax"]
	default:
		total := round((baseBlock+db.Sum("BASE", "BlockChance"))*(1+blockInc/100)*blockMore, 0)
		out["BlockChance"] = math.Min(total, out["BlockChanceMax"])
		out["BlockChanceOverCap"] = math.Max(0, total-out["BlockChanceMax"])
	}
	if db.Flag("SpellBlockChanceMaxIsBlockChanceMax") {
		out["SpellBlockChanceMax"] = out["BlockChanceMax"]
	} else {
		out["SpellBlockChanceMax"] = math.Min(db.Sum("BASE", "SpellBlockChanceMax"), blockChanceCap)
	}
	switch {
	case db.Flag("MaxSpellBlockIfNotBlockedRecently"):
		out["SpellBlockChance"] = out["SpellBlockChanceMax"]
	case db.Flag("SpellBlockChanceIsBlockChance"):
		out["SpellBlockChance"] = out["BlockChance"]
	default:
		total := round(db.Sum("BASE", "SpellBlockChance")*(1+blockInc/100)*blockMore, 0)
		out["SpellBlockChance"] = math.Min(total, out["SpellBlockChanceMax"])
		out["SpellBlockChanceOverCap"] = math.Max(0, total-out["SpellBlockChanceMax"])
	}
	out["ProjectileBlockChance"] = math.Min(out["BlockChance"]+db.Sum("BASE", "ProjectileBlockChance")*calcMod(db, nil, "BlockChance"), out["BlockChanceMax"])
	switch {
	case db.Flag("MaxSpellBlockIfNotBlockedRecently"):
		out["SpellProjectileBlockChance"] = out["SpellBlockChanceMax"]
	case db.Flag("SpellBlockChanceIsBlockChance"):
		out["SpellProjectileBlockChance"] = out["ProjectileBlockChance"]
	default:
		out["SpellProjectileBlockChance"] = math.Max(math.Min(out["SpellBlockChance"]+db.Sum("BASE", "ProjectileSpellBlockChance")*calcMod(db, nil, "SpellBlockChance"), out["SpellBlockChanceMax"]), 0)
	}
	if db.Flag("CannotBlockAttacks") {
		out["BlockChance"], out["ProjectileBlockChance"] = 0, 0
	}
	if db.Flag("CannotBlockSpells") {
		out["SpellBlockChance"], out["SpellProjectileBlockChance"] = 0, 0
	}
	for _, t := range []string{"BlockChance", "ProjectileBlockChance", "SpellBlockChance", "SpellProjectileBlockChance"} {
		// CalcDefence.lua:760-772: the enemy's reduction and the luck are
		// env.mode_effective.
		out["Effective"+t] = out[t]
		rolls := 0.0
		if modeEffective(db) {
			out["Effective"+t] = math.Max(out[t]-db.enemy.Sum("BASE", "reduceEnemyBlock"), 0)
			if db.Flag(t + "IsLucky") {
				rolls++
			}
			if db.Flag(t + "IsUnlucky") {
				rolls--
			}
			if db.Flag("ExtremeLuck") {
				rolls *= 2
			}
		}
		out["Effective"+t] = luckyRolls(db, out["Effective"+t], rolls)
	}
	out["EffectiveAverageBlockChance"] = (out["EffectiveBlockChance"] + out["EffectiveProjectileBlockChance"] +
		out["EffectiveSpellBlockChance"] + out["EffectiveSpellProjectileBlockChance"]) / 4
	out["BlockEffect"] = 100 - db.Sum("BASE", "BlockEffect")
	if out["BlockEffect"] != 0 {
		out["DamageTakenOnBlock"] = 100 - out["BlockEffect"]
	}

	// Light Radius increases to Life Regeneration and Mana Regeneration
	// increases to ES Recharge (regenerationConversions), then Armour
	// increases to ES Recharge (Armour and Energy Shield Mastery).
	for _, x := range [][3]string{{"LightRadius", "LifeRegen", "LightRadiusAppliesToLifeRegen"},
		{"ManaRegen", "EnergyShieldRecharge", "ManaRegenAppliesToEnergyShieldRecharge"}} {
		if v, ok := db.MaxCfg(nil, x[2]); ok && v != 0 {
			convertIncMods(db, []string{x[0]}, x[1], v/100)
		}
	}
	if db.Flag("ArmourAppliesToEnergyShieldRecharge") {
		mult := 100.0
		if v, ok := db.MaxCfg(nil, "ImprovedArmourAppliesToEnergyShieldRecharge"); ok {
			mult = v
		}
		convertIncMods(db, []string{"Armour", "ArmourAndEvasion", "Defences"}, "EnergyShieldRecharge", mult/100)
	}

	// Defences increased by resistances and block.
	for _, x := range []struct{ flag, name, stat string }{
		{"ArmourIncreasedByUncappedFireRes", "Armour", "FireResistTotal"},
		{"ArmourIncreasedByOvercappedFireRes", "Armour", "FireResistOverCap"},
		{"EvasionRatingIncreasedByUncappedColdRes", "Evasion", "ColdResistTotal"},
		{"EvasionRatingIncreasedByOvercappedColdRes", "Evasion", "ColdResistOverCap"},
		{"EnergyShieldIncreasedByChanceToBlockSpellDamage", "EnergyShield", "SpellBlockChance"},
		{"EnergyShieldIncreasedByChaosResistance", "EnergyShield", "ChaosResist"},
	} {
		for i := range db.mods[x.flag] {
			m := &db.mods[x.flag][i]
			if m.Type != "FLAG" || !eligible(m, "FLAG", nil) {
				continue
			}
			if v, ok := db.eval(m, nil); ok && truthy(v) {
				db.NewMod(x.name, "INC", out[x.stat], m.Source)
				break
			}
		}
	}

	// Primary defences.
	ironReflexes := db.Flag("IronReflexes")
	esToWard := db.Flag("EnergyShieldToWard")
	var ward, es, arm, eva, gearEvasion float64
	for _, slot := range defenceSlots {
		d, ok := armour[slot]
		if !ok {
			continue
		}
		cfg := &Cfg{SlotName: slot}
		esBase, armBase, evaBase, wardBase := d.EnergyShield, d.Armour, d.Evasion, d.Ward
		if db.Flag("GainNoEnergyShieldFrom" + slot) {
			esBase = 0
		}
		if db.Flag("GainNoArmourFrom" + slot) {
			armBase = 0
		}
		if db.Flag("GainNoEvasionFrom"+slot) || (db.Flag("GainNoArmourFrom"+slot) && ironReflexes) {
			evaBase = 0
		}
		if db.Flag("GainNoWardFrom" + slot) {
			wardBase = 0
		}
		if slot == "Body Armour" && db.Flag("ConvertBodyArmourArmourEvasionToWard") {
			conv := math.Min(db.Sum("BASE", "BodyArmourArmourEvasionToWardPercent")/100, 1)
			ca, ce := armBase*conv, evaBase*conv
			armBase, evaBase, wardBase = armBase-ca, evaBase-ce, wardBase+ca+ce
		}
		if wardBase > 0 {
			if esToWard {
				ward += wardBase * (1 + db.SumCfg(cfg, "INC", "Ward", "Defences", "EnergyShield")/100) * db.MoreCfg(cfg, "Ward", "Defences")
			} else {
				ward += wardBase * calcMod(db, cfg, "Ward", "Defences")
			}
		}
		if esBase > 0 {
			if esToWard {
				es += esBase * db.MoreCfg(cfg, "EnergyShield", "Defences")
			} else if !db.Flag("ConvertArmourESToLife") {
				es += esBase * calcMod(db, cfg, "EnergyShield", "Defences", slot+"ESAndArmour")
			}
		}
		if armBase > 0 {
			arm += armBase * calcMod(db, cfg, "Armour", "ArmourAndEvasion", "Defences", slot+"ESAndArmour")
		}
		if evaBase > 0 {
			gearEvasion += evaBase
			if ironReflexes {
				arm += evaBase * calcMod(db, cfg, "Armour", "Evasion", "ArmourAndEvasion", "Defences")
			} else {
				eva += evaBase * calcMod(db, cfg, "Evasion", "ArmourAndEvasion", "Defences")
			}
		}
	}
	if b := db.Sum("BASE", "Ward"); b > 0 {
		if esToWard {
			ward += b * (1 + db.Sum("INC", "Ward", "Defences", "EnergyShield")/100) * db.More("Ward", "Defences")
		} else {
			ward += b * calcMod(db, nil, "Ward", "Defences")
		}
	}
	if b := db.Sum("BASE", "EnergyShield"); b > 0 {
		if esToWard {
			es += b * db.More("EnergyShield", "Defences")
		} else {
			es += b * calcMod(db, nil, "EnergyShield", "Defences")
		}
	}
	if b := db.Sum("BASE", "Armour", "ArmourAndEvasion"); b > 0 {
		arm += b * calcMod(db, nil, "Armour", "ArmourAndEvasion", "Defences")
	}
	if b := db.Sum("BASE", "Evasion", "ArmourAndEvasion"); b > 0 {
		if ironReflexes {
			arm += b * calcMod(db, nil, "Armour", "Evasion", "ArmourAndEvasion", "Defences")
		} else {
			eva += b * calcMod(db, nil, "Evasion", "ArmourAndEvasion", "Defences")
		}
	}
	if conv := db.Sum("BASE", "ManaConvertToArmour"); conv > 0 {
		arm += 2 * db.Sum("BASE", "Mana") * conv / 100 * calcMod(db, nil, "Mana", "Armour", "ArmourAndEvasion", "Defences")
	}
	if conv := db.Sum("BASE", "ManaGainAsEnergyShield"); conv > 0 {
		es += db.Sum("BASE", "Mana") * conv / 100 * calcMod(db, nil, "Mana", "EnergyShield", "Defences")
	}
	ci := db.Flag("ChaosInoculation")
	if conv := db.Sum("BASE", "LifeGainAsArmour"); conv > 0 {
		if ci {
			arm++
		} else {
			arm += db.Sum("BASE", "Life") * conv / 100 * calcMod(db, nil, "Life", "Armour", "ArmourAndEvasion", "Defences")
		}
	}
	if conv := db.Sum("BASE", "LifeConvertToEnergyShield", "LifeGainAsEnergyShield"); conv > 0 {
		if ci {
			es++
		} else {
			es += db.Sum("BASE", "Life") * conv / 100 * calcMod(db, nil, "Life", "EnergyShield", "Defences")
		}
	}
	if conv := db.Sum("BASE", "EvasionGainAsArmour"); conv > 0 {
		b := (db.Sum("BASE", "Evasion", "ArmourAndEvasion") + gearEvasion) * conv / 100
		arm += b * calcMod(db, nil, "Evasion", "Armour", "ArmourAndEvasion", "Defences")
	}
	if o, ok := db.Override("EnergyShield"); ok {
		out["EnergyShield"] = o
	} else {
		out["EnergyShield"] = math.Max(round(es, 0), 0)
	}
	if o, ok := db.Override("Armour"); ok {
		out["Armour"] = o
	} else {
		out["Armour"] = math.Max(round(arm, 0), 0)
	}
	out["Evasion"] = math.Max(round(eva, 0), 0)
	out["MeleeEvasion"] = math.Max(round(eva*calcMod(db, nil, "MeleeEvasion"), 0), 0)
	out["ProjectileEvasion"] = math.Max(round(eva*calcMod(db, nil, "ProjectileEvasion"), 0), 0)
	out["LowestOfArmourAndEvasion"] = math.Min(out["Armour"], out["Evasion"])
	out["Ward"] = math.Max(math.Floor(ward), 0)
	c.evadeChance(db)
}

const evadeChanceCap = 95 // Data.lua data.misc.EvadeChanceCap

// hitChance is calcs.hitChance.
func hitChance(evasion, accuracy float64) float64 {
	if accuracy < 0 {
		return 5
	}
	raw := accuracy / (accuracy + math.Pow(evasion/5, 0.9)) * 125
	return math.Max(math.Min(round(raw, 0), 100), 5)
}

// calcVal is calcLib.val: the BASE sum scaled by increased and more.
func calcVal(db *ModDB, name string) float64 {
	if b := db.Sum("BASE", name); b != 0 {
		return b * calcMod(db, nil, name)
	}
	return 0
}

// evadeChance is the evasion part of calcs.defence: energy shield
// recovery cap and the chance to evade the enemy's attacks.
func (c *Calculator) evadeChance(db *ModDB) {
	out := db.output
	capping := (db.Flag("ArmourESRecoveryCap") && out["Armour"] < out["EnergyShield"]) ||
		(db.Flag("EvasionESRecoveryCap") && out["Evasion"] < out["EnergyShield"]) || db.conditions["LowEnergyShield"]
	out["EnergyShieldRecoveryCap"] = out["EnergyShield"]
	if capping {
		switch {
		case db.Flag("ArmourESRecoveryCap") && db.Flag("EvasionESRecoveryCap"):
			out["EnergyShieldRecoveryCap"] = math.Min(out["Armour"], out["Evasion"])
		case db.Flag("ArmourESRecoveryCap"):
			out["EnergyShieldRecoveryCap"] = out["Armour"]
		case db.Flag("EvasionESRecoveryCap"):
			out["EnergyShieldRecoveryCap"] = out["Evasion"]
		}
		if db.conditions["LowEnergyShield"] {
			out["EnergyShieldRecoveryCap"] = math.Min(out["EnergyShield"]*lowPoolThreshold, out["EnergyShieldRecoveryCap"])
		}
	}
	enemy := db.enemy
	switch {
	case db.Flag("CannotEvade") || enemy.Flag("CannotBeEvaded"):
		out["EvadeChance"], out["MeleeEvadeChance"], out["ProjectileEvadeChance"] = 0, 0, 0
	case db.Flag("AlwaysEvade"):
		out["EvadeChance"], out["MeleeEvadeChance"], out["ProjectileEvadeChance"] = 100, 100, 100
	default:
		acc := round(calcVal(enemy, "Accuracy"), 0)
		evade := db.Sum("BASE", "EvadeChance")
		hit := calcMod(enemy, nil, "HitChance")
		stat, melee, proj := out["Evasion"], out["MeleeEvasion"], out["ProjectileEvasion"]
		if db.Flag("EvadeChanceBasedOnWard") {
			o, _ := db.Override("EvadeChanceBasedOnWardPercent")
			stat = out["Ward"] * o / 100
			melee, proj = stat, stat
		}
		out["EvadeChance"] = 100 - (hitChance(stat, acc)-evade)*hit
		chance := func(s float64, name string) float64 {
			return math.Max(0, math.Min(evadeChanceCap, (100-(hitChance(s, acc)-evade)*hit)*calcMod(db, nil, "EvadeChance", name)))
		}
		out["MeleeEvadeChance"] = chance(melee, "MeleeEvadeChance")
		out["ProjectileEvadeChance"] = chance(proj, "ProjectileEvadeChance")
		if out["MeleeEvadeChance"] == out["ProjectileEvadeChance"] {
			out["EvadeChance"] = out["MeleeEvadeChance"]
		}
	}
}
