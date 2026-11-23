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

// itemEnergyShield is armourData.EnergyShield of ItemClass:BuildModListForSlotNum.
func itemEnergyShield(it *itemtext.Item, mods []modparser.Mod) float64 {
	if it.Base == nil || it.Base.Armour == nil || zeroEnergyShield[it.Name] == it.BaseName {
		return 0
	}
	a := it.Base.Armour
	base := calcLocal(mods, "EnergyShield", "BASE") + a.EnergyShieldBaseMin
	variance := a.EnergyShieldBaseMax - a.EnergyShieldBaseMin
	extra := calcLocal(mods, "EvasionAndEnergyShield", "BASE") + calcLocal(mods, "ArmourAndEnergyShield", "BASE")
	inc := calcLocal(mods, "EnergyShield", "INC") + calcLocal(mods, "ArmourAndEnergyShield", "INC") +
		calcLocal(mods, "EvasionAndEnergyShield", "INC") + calcLocal(mods, "Defences", "INC")
	quality := float64(it.Quality)
	if calcLocal(mods, "AlternateQualityArmour", "BASE") > 0 {
		quality = 0
	}
	scale := (1 + inc/100) * (1 + quality/100)
	pct, ok := it.ArmourData["EnergyShieldBasePercentile"]
	if !ok {
		pct = 1
		if es := it.ArmourData["EnergyShield"]; es > 0 && variance != 0 {
			pct = round(math.Max(math.Min((es/scale-base)/variance, 1), 0), 4)
		}
	}
	return round((base+extra+variance*pct)*scale, 0)
}

// zeroEnergyShield: ItemClass:BuildModList's hack "to remove the energy
// shield and base int requirement" of these uniques.
var zeroEnergyShield = map[string]string{
	"Tabula Rasa": "Simple Robe", "Skin of the Loyal": "Simple Robe", "Skin of the Lords": "Simple Robe",
	"The Apostate": "Cabalist Regalia",
}

// esToLifeSlots are the slots CalcPerform.lua converts for ConvertArmourESToLife.
var esToLifeSlots = []string{"Helmet", "Gloves", "Boots", "Body Armour", "Weapon 2", "Weapon 3"}
