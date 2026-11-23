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
	armour := map[string]armourData{}
	for _, slot := range defenceSlots {
		if it := db.items[slot]; it != nil && it.Base != nil && it.Base.Armour != nil {
			armour[slot] = itemArmour(it, gearMods[slot])
		}
	}

	// Block.
	out["BlockChanceMax"] = math.Min(db.Sum("BASE", "BlockChanceMax"), blockChanceCap)
	baseBlock := armour["Weapon 2"].BlockChance + armour["Weapon 3"].BlockChance
	out["ShieldBlockChance"] = baseBlock
	if !keystones["Necromantic Aegis"] {
		if o, ok := db.Override("ReplaceShieldBlock"); ok {
			baseBlock = o
		}
	}
	blockInc, blockMore := db.Sum("INC", "BlockChance"), db.More("BlockChance")
	if db.Flag("MaxBlockIfNotBlockedRecently") {
		out["BlockChance"] = out["BlockChanceMax"]
	} else {
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
	if db.Flag("CannotBlockAttacks") {
		out["BlockChance"] = 0
	}
	if db.Flag("CannotBlockSpells") {
		out["SpellBlockChance"] = 0
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
	out["Ward"] = math.Max(math.Floor(ward), 0)
}
