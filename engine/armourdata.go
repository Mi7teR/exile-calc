package engine

import (
	"math"

	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/modparser"
)

// calcLocal is Item.lua calcLocal for BASE and INC: the item's mods of
// that name and type with the given flags, no keyword flags and no tag
// other than a leading InSlot.
func calcLocal(mods []modparser.Mod, name, typ string) float64 {
	total := 0.0
	for _, m := range mods {
		if m.Name != name || m.Type != typ || m.Flags != 0 || m.KeywordFlags != 0 {
			continue
		}
		if len(m.Tags) > 0 && tagStr(m.Tags[0], "type") != "InSlot" {
			continue
		}
		if f, ok := num(m.Value); ok {
			total += f
		}
	}
	return total
}

// armourData is an armour item's own defences (Item.lua armourData).
type armourData struct {
	Armour, Evasion, EnergyShield, Ward, BlockChance float64
}

// itemArmour ports the armour branch of ItemClass:BuildModListForSlotNum:
// each defence is (base minimum + local flat + variance * base percentile)
// scaled by local increases and quality. A missing percentile is derived
// from the defence value in the item text, else 1.
func itemArmour(it *itemtext.Item, mods []modparser.Mod) armourData {
	var d armourData
	if it.Base == nil || it.Base.Armour == nil {
		return d
	}
	a := it.Base.Armour
	local := func(name, typ string) float64 { return calcLocal(mods, name, typ) }
	quality := float64(it.Quality)
	if local("AlternateQualityArmour", "BASE") > 0 {
		quality = 0
	}
	defences := local("Defences", "INC")
	compute := func(name string, min, max, flat, inc float64) float64 {
		base := local(name, "BASE") + min
		variance := max - min
		scale := (1 + (local(name, "INC")+inc+defences)/100) * (1 + quality/100)
		pct, ok := it.ArmourData[name+"BasePercentile"]
		if !ok {
			pct = 1
			if v := it.ArmourData[name]; v > 0 && variance != 0 {
				pct = round(math.Max(math.Min((v/scale-base)/variance, 1), 0), 4)
			}
		}
		return round((base+flat+variance*pct)*scale, 0)
	}
	ae, aes, ees := local("ArmourAndEvasion", "BASE"), local("ArmourAndEnergyShield", "BASE"), local("EvasionAndEnergyShield", "BASE")
	aeInc, aesInc, eesInc := local("ArmourAndEvasion", "INC"), local("ArmourAndEnergyShield", "INC"), local("EvasionAndEnergyShield", "INC")
	d.Armour = compute("Armour", a.ArmourBaseMin, a.ArmourBaseMax, ae+aes, aeInc+aesInc)
	d.Evasion = compute("Evasion", a.EvasionBaseMin, a.EvasionBaseMax, ae+ees, aeInc+eesInc)
	d.EnergyShield = compute("EnergyShield", a.EnergyShieldBaseMin, a.EnergyShieldBaseMax, ees+aes, aesInc+eesInc)
	// Ward has no hybrid stats: wardBase + variance, scaled by Ward and Defences.
	d.Ward = compute("Ward", a.WardBaseMin, a.WardBaseMax, 0, 0)
	if a.BlockChance != 0 {
		d.BlockChance = math.Floor((a.BlockChance + local("BlockChance", "BASE")) * (1 + local("BlockChance", "INC")/100))
	}
	if base := zeroEnergyShield[it.Name]; base != "" && base == it.BaseName {
		d.EnergyShield = 0
	}
	return d
}

// zeroEnergyShield: ItemClass:BuildModList's hack "to remove the energy
// shield and base int requirement" of these uniques.
var zeroEnergyShield = map[string]string{
	"Tabula Rasa": "Simple Robe", "Skin of the Loyal": "Simple Robe", "Skin of the Lords": "Simple Robe",
	"The Apostate": "Cabalist Regalia",
}

// esToLifeSlots are the slots CalcPerform.lua converts for ConvertArmourESToLife.
var esToLifeSlots = []string{"Helmet", "Gloves", "Boots", "Body Armour", "Weapon 2", "Weapon 3"}
