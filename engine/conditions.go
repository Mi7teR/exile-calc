package engine

import (
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/itemtext"
)

// weaponTypeInfo is data.weaponTypeInfo (Modules/Data.lua).
var weaponTypeInfo = map[string]struct {
	oneHand, melee bool
	flag           string
}{
	"None": {true, true, "Unarmed"}, "Bow": {false, false, "Bow"}, "Claw": {true, true, "Claw"},
	"Dagger": {true, true, "Dagger"}, "Staff": {false, true, "Staff"}, "Wand": {true, false, "Wand"},
	"One Handed Axe": {true, true, "Axe"}, "One Handed Mace": {true, true, "Mace"},
	"One Handed Sword": {true, true, "Sword"}, "Sceptre": {true, true, "Mace"},
	"Thrusting One Handed Sword": {true, true, "Sword"}, "Fishing Rod": {false, true, "Fishing"},
	"Two Handed Axe": {false, true, "Axe"}, "Two Handed Mace": {false, true, "Mace"},
	"Two Handed Sword": {false, true, "Sword"},
}

// equipped is one item in a gear slot.
type equipped struct {
	slot string
	it   *itemtext.Item
}

var influenceMultipliers = map[string]string{
	"ShaperItem": "shaper", "ElderItem": "elder", "WarlordItem": "adjudicator",
	"HunterItem": "basilisk", "CrusaderItem": "crusader", "RedeemerItem": "eyrie",
}

func hasInfluence(it *itemtext.Item, key string) bool {
	for _, i := range it.Influences {
		if i == key {
			return true
		}
	}
	return false
}

// itemConditions ports the item counts of CalcSetup.lua (rarity, influence
// and type multipliers, <Rarity>In<Slot> conditions) and the equipment
// conditions of doActorAttribsConditions (CalcPerform.lua). Weapon data
// for countsAsAll1H (Varunastra) is not ported.
func itemConditions(db *ModDB, gear []equipped) {
	cond, mult := db.conditions, db.multipliers
	bySlot := map[string]*itemtext.Item{}
	for _, g := range gear {
		it := g.it
		bySlot[g.slot] = it
		if it.Base == nil {
			continue
		}
		switch it.Base.Type {
		case "Jewel", "Flask", "Tincture", "Graft", "Viscera":
			continue
		}
		key := "NormalItem"
		switch it.Rarity {
		case itemtext.Unique, itemtext.Relic:
			key = "UniqueItem"
		case itemtext.Rare:
			key = "RareItem"
		case itemtext.Magic:
			key = "MagicItem"
		}
		mult[key]++
		cond[key+"In"+g.slot] = true
		if it.Corrupted {
			mult["CorruptedItem"]++
		} else {
			mult["NonCorruptedItem"]++
		}
		for m, infl := range influenceMultipliers {
			if hasInfluence(it, infl) {
				mult[m]++
			} else {
				mult["Non"+m]++
			}
		}
		if hasInfluence(it, "shaper") || hasInfluence(it, "elder") {
			mult["ShaperOrElderItem"]++
		}
		// item.type:gsub(" ", ""):gsub(".+Handed", "") .. "Item"
		t := strings.ReplaceAll(it.Base.Type, " ", "")
		if i := strings.LastIndex(t, "Handed"); i >= 0 {
			t = t[i+len("Handed"):]
		}
		mult[t+"Item"]++
		if it.Base.Type == "Ring" {
			mult[strings.ReplaceAll(it.BaseName, " ", "")+"Equipped"]++
		}
	}

	// doActorAttribsConditions
	w1, w2 := bySlot["Weapon 1"], bySlot["Weapon 2"]
	switch {
	case w2 != nil && w2.Base != nil && w2.Base.Type == "Shield":
		cond["UsingShield"] = true
	case w2 == nil:
		cond["OffHandIsEmpty"] = true
	}
	weapon := func(it *itemtext.Item) {
		if it == nil || it.Base == nil || it.Base.Weapon == nil {
			return
		}
		info, ok := weaponTypeInfo[it.Base.Type]
		if !ok {
			return
		}
		cond["Using"+info.flag] = true
		if info.melee {
			cond["UsingMeleeWeapon"] = true
		}
		if info.oneHand {
			cond["UsingOneHandedWeapon"] = true
		} else {
			cond["UsingTwoHandedWeapon"] = true
		}
	}
	if w1 == nil || w1.Base == nil || w1.Base.Weapon == nil {
		cond["Unarmed"] = true
		if w2 == nil && bySlot["Gloves"] == nil {
			cond["Unencumbered"] = true
		}
	} else {
		weapon(w1)
	}
	weapon(w2)
	for _, s := range []string{"Helmet", "Body Armour", "Gloves", "Boots"} {
		if bySlot[s] != nil {
			cond["Using"+s] = true
		}
	}
	if w1 != nil && w2 != nil && w1.Base != nil && w2.Base != nil && w1.Base.Weapon != nil && w2.Base.Weapon != nil {
		cond["DualWielding"] = true
		t1, t2 := w1.Base.Type, w2.Base.Type
		if t1 == "Claw" && t2 == "Claw" {
			cond["DualWieldingClaws"] = true
		}
		if t1 == "Dagger" && t2 == "Dagger" {
			cond["DualWieldingDaggers"] = true
		}
		// getWeaponType: thrusting swords count as one handed swords.
		kind := func(b *gamedata.Base) string {
			if b.Type == "One Handed Sword" && b.SubType == "Thrusting" || b.Type == "Thrusting One Handed Sword" {
				return "One Handed Sword"
			}
			if b.Type == "Sceptre" {
				return "Sceptre"
			}
			if b.SubType != "" {
				return b.SubType
			}
			return b.Type
		}
		if kind(w1.Base) != kind(w2.Base) && weaponTypeInfo[t1].oneHand && weaponTypeInfo[t2].oneHand {
			cond["WieldingDifferentWeaponTypes"] = true
		}
	}
}

// buffMode sets CalcSetup.lua's mode conditions for the main output
// (buffMode "EFFECTIVE").
func buffMode(db *ModDB) {
	db.conditions["Buffed"] = true
	db.conditions["Combat"] = true
	db.conditions["Effective"] = true
}

// minionEquipmentConditions ports the equipment conditions of
// doActorAttribsConditions (CalcPerform.lua:148-245) for env.minion
// (CalcPerform.lua:1998): from its itemList and the types of its
// weaponData1 (w1, "None" when unarmed) and weaponData2 (w2, "" when it
// has none). countsAsAll1H (Varunastra) is not ported, as for the player.
func minionEquipmentConditions(cond map[string]bool, items map[string]*itemtext.Item, w1, w2 string) {
	// CalcPerform.lua:149: actor == env.player is false for a minion; the
	// minion's aegis item is not ported.
	if it := items["Weapon 2"]; it != nil && it.Base != nil && it.Base.Type == "Shield" {
		cond["UsingShield"] = true
	} else if items["Weapon 2"] == nil {
		cond["OffHandIsEmpty"] = true
	}
	if w1 == "None" {
		cond["Unarmed"] = true
		if items["Weapon 2"] == nil && items["Gloves"] == nil {
			cond["Unencumbered"] = true
		}
	} else {
		weaponTypeConditions(cond, w1)
	}
	for _, s := range []string{"Helmet", "Body Armour", "Gloves", "Boots"} {
		if items[s] != nil {
			cond["Using"+s] = true
		}
	}
	if w2 != "" {
		weaponTypeConditions(cond, w2)
		dualWieldConditions(cond, items, w1, w2)
	}
}

// weaponTypeConditions are the Using<flag> and hand conditions of one
// weapon type (CalcPerform.lua:160-185, 198-221).
func weaponTypeConditions(cond map[string]bool, typ string) {
	info := weaponTypeInfo[typ]
	cond["Using"+info.flag] = true
	if info.melee {
		cond["UsingMeleeWeapon"] = true
	}
	if info.oneHand {
		cond["UsingOneHandedWeapon"] = true
	} else {
		cond["UsingTwoHandedWeapon"] = true
	}
}

// dualWieldConditions is CalcPerform.lua:223-245 for weapon types w1 and w2.
func dualWieldConditions(cond map[string]bool, items map[string]*itemtext.Item, w1, w2 string) {
	cond["DualWielding"] = true
	if w1 == "Claw" && w2 == "Claw" {
		cond["DualWieldingClaws"] = true
	}
	if w1 == "Dagger" && w2 == "Dagger" {
		cond["DualWieldingDaggers"] = true
	}
	differ := weaponKind(items["Weapon 1"], w1) != weaponKind(items["Weapon 2"], w2)
	if differ && weaponTypeInfo[w1].oneHand && weaponTypeInfo[w2].oneHand {
		cond["WieldingDifferentWeaponTypes"] = true
	}
}

// weaponKind is getWeaponType (CalcPerform.lua:231-237): thrusting swords
// count as one handed swords; then the weaponTypeInfo label, the item
// base's subType (Item.lua:2327) or the type.
func weaponKind(it *itemtext.Item, typ string) string {
	var sub string
	if it != nil && it.Base != nil && it.Base.Weapon != nil {
		sub = it.Base.SubType
	}
	switch {
	case typ == "One Handed Sword" && sub == "Thrusting":
		return "One Handed Sword"
	case typ == "Sceptre":
		return "Sceptre"
	case typ == "Thrusting One Handed Sword":
		return "One Handed Sword"
	case sub != "":
		return sub
	}
	return typ
}
