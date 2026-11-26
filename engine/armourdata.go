package engine

import (
	"math"
	"strings"

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

// calcLocalFlags is Item.lua calcLocal with mod flags (local attack speed).
func calcLocalFlags(mods []modparser.Mod, name, typ string, flags uint64) float64 {
	total := 0.0
	for _, m := range mods {
		if m.Name != name || m.Type != typ || m.Flags != flags || m.KeywordFlags != 0 {
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

// itemWeapon ports the weapon branch of ItemClass:BuildModListForSlotNum:
// the weapon's attack rate, crit chance and physical damage with its local
// mods and quality (elemental base damage and WeaponData mods are not
// ported). It returns false for items that are not weapons.
func itemWeapon(it *itemtext.Item, mods []modparser.Mod) (hitSource, string, bool) {
	if it == nil || it.Base == nil || it.Base.Weapon == nil {
		return hitSource{}, "", false
	}
	w := it.Base.Weapon
	local := func(name, typ string) float64 { return calcLocal(mods, name, typ) }
	quality := float64(it.Quality)
	speedInc := calcLocalFlags(mods, "Speed", "INC", mfAttack) + math.Floor(quality/8*local("AlternateQualityLocalAttackSpeedPer8Quality", "INC"))
	out := hitSource{min: map[string]float64{}, max: map[string]float64{}}
	out.attackRate = round(w.AttackRateBase*(1+speedInc/100), 2)
	// Item.lua: weaponData.range = base range + rangeBonus.
	out.rangeBonus = local("WeaponRange", "BASE") + 10*local("WeaponRangeMetre", "BASE") +
		math.Floor(quality/10*local("AlternateQualityLocalWeaponRangePer10Quality", "BASE"))
	out.rng = float64(w.Range) + out.rangeBonus
	out.hasRange = true
	localEle := local("LocalElementalDamage", "INC")
	for _, t := range dmgTypeList {
		mn, mx := local(t+"Min", "BASE"), local(t+"Max", "BASE")
		if t == "Physical" {
			mn += w.PhysicalMin
			mx += w.PhysicalMax
			inc := local("PhysicalDamage", "INC")
			q := quality
			if local("AlternateQualityWeapon", "BASE") > 0 {
				q = 0
			}
			mn = round(mn*(1+inc/100)*(1+q/100), 0)
			mx = round(mx*(1+inc/100)*(1+q/100), 0)
		} else if t != "Chaos" {
			inc := local("Local"+t+"Damage", "INC") + localEle
			mn = round(mn*(1+inc/100), 0)
			mx = round(mx*(1+inc/100), 0)
		}
		if mn > 0 && mx > 0 {
			out.min[t], out.max[t] = mn, mx
		}
	}
	out.critChance = round((w.CritChanceBase+local("CritChance", "BASE"))*
		(1+(local("CritChance", "INC")+math.Floor(quality/4*local("AlternateQualityLocalCritChancePer4Quality", "INC")))/100), 2)
	// Item.lua: WeaponData mods set weaponData fields ("This Weapon's
	// Critical Strike Chance is 100%").
	for _, m := range mods {
		if m.Name != "WeaponData" || m.Type != "LIST" || len(m.Tags) != 0 {
			continue
		}
		v, _ := m.Value.(map[string]any)
		key, _ := v["key"].(string)
		if key == "countsAsAll1H" {
			out.all1H = truthy(v["value"])
			continue
		}
		f, ok := num(v["value"])
		if !ok {
			continue
		}
		switch {
		case key == "CritChance":
			out.critChance = f
		case key == "AttackRate":
			out.attackRate = f
		case strings.HasSuffix(key, "Min") && contains(dmgTypeList, strings.TrimSuffix(key, "Min")):
			out.min[strings.TrimSuffix(key, "Min")] = f
		case strings.HasSuffix(key, "Max") && contains(dmgTypeList, strings.TrimSuffix(key, "Max")):
			out.max[strings.TrimSuffix(key, "Max")] = f
		}
	}
	return out, it.Base.Type, true
}
