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
	all1H := playerAll1H(db)
	var types, names [2]string
	for i, it := range []*itemtext.Item{w1, w2} {
		if it != nil && it.Base != nil && it.Base.Weapon != nil {
			types[i], names[i] = it.Base.Type, it.Name
		}
	}
	db.all1HAdded = all1HAdded(types, all1H, names)
	switch {
	case w2 != nil && w2.Base != nil && w2.Base.Type == "Shield":
		cond["UsingShield"] = true
	case w2 == nil:
		cond["OffHandIsEmpty"] = true
	}
	weapon := func(it *itemtext.Item, all1H bool) {
		if it == nil || it.Base == nil || it.Base.Weapon == nil {
			return
		}
		info, ok := weaponTypeInfo[it.Base.Type]
		if !ok {
			return
		}
		cond["Using"+info.flag] = true
		// CalcPerform.lua:162-175, 200-213.
		if all1H {
			for _, f := range []string{"Axe", "Sword", "Dagger", "Mace", "Claw"} {
				cond["Using"+f] = true
			}
			cond["WieldingDifferentWeaponTypes"] = true
		}
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
		weapon(w1, all1H[0])
	}
	weapon(w2, all1H[1])
	for _, s := range []string{"Helmet", "Body Armour", "Gloves", "Boots"} {
		if bySlot[s] != nil {
			cond["Using"+s] = true
		}
	}
	if w1 != nil && w2 != nil && w1.Base != nil && w2.Base != nil && w1.Base.Weapon != nil && w2.Base.Weapon != nil {
		cond["DualWielding"] = true
		t1, t2 := w1.Base.Type, w2.Base.Type
		if (t1 == "Claw" || all1H[0]) && (t2 == "Claw" || all1H[1]) {
			cond["DualWieldingClaws"] = true
		}
		if (t1 == "Dagger" || all1H[0]) && (t2 == "Dagger" || all1H[1]) {
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

// playerAll1H is the countsAsAll1H of the player's weaponData1 and
// weaponData2: their items' WeaponData mods (Item.lua:2361-2363).
func playerAll1H(db *ModDB) [2]bool {
	var out [2]bool
	for i, slot := range []string{slotWeapon1, slotWeapon2} {
		if w, _, ok := itemWeapon(db.items[slot], db.gearMods[slot]); ok {
			out[i] = w.all1H
		}
	}
	return out
}

// buffMode sets CalcSetup.lua's mode conditions (132-134) for buffMode
// mode (578-599): "" is EFFECTIVE, the mode of every pass but the Calcs
// tab's, whose misc_buffMode may also be COMBAT, BUFFED or UNBUFFED.
func buffMode(db *ModDB, mode string) {
	buffs, combat, effective := false, false, false
	switch mode {
	case "", "EFFECTIVE":
		buffs, combat, effective = true, true, true
	case "COMBAT":
		buffs, combat = true, true
	case "BUFFED":
		buffs = true
	}
	db.conditions[condBuffed], db.conditions[condCombat], db.conditions[condEffective] = buffs, combat, effective
}

// The mode conditions (CalcSetup.lua:132-134).
const (
	condBuffed    = "Buffed"
	condCombat    = "Combat"
	condEffective = "Effective"
)

// copyModeConditions gives a minion's ModDB the env's mode conditions
// (calcs.initModDB, CalcSetup.lua:132-134).
func copyModeConditions(mdb, owner *ModDB) {
	for _, k := range []string{condBuffed, condCombat, condEffective} {
		mdb.conditions[k] = owner.conditions[k]
	}
}

// modeBuffs, modeCombat and modeEffective are env.mode_buffs, mode_combat
// and mode_effective, read from an actor's mode conditions, which
// CalcSetup.lua:132-134 sets to them for the player, the enemy and the
// minion alike.
func modeBuffs(actor *ModDB) bool     { return actor.conditions[condBuffed] }
func modeCombat(actor *ModDB) bool    { return actor.conditions[condCombat] }
func modeEffective(actor *ModDB) bool { return actor.conditions[condEffective] }

// minionEquipmentConditions ports the equipment conditions of
// doActorAttribsConditions (CalcPerform.lua:148-245) for env.minion
// (CalcPerform.lua:1998): from its itemList and the types of its
// weaponData1 (w1, "None" when unarmed) and weaponData2 (w2, "" when it
// has none). countsAsAll1H (Varunastra) is not ported, as for the player.
func minionEquipmentConditions(cond map[string]bool, items map[string]*itemtext.Item, w1, w2 string, all1H [2]bool) {
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
		weaponTypeConditions(cond, w1, all1H[0])
	}
	for _, s := range []string{"Helmet", "Body Armour", "Gloves", "Boots"} {
		if items[s] != nil {
			cond["Using"+s] = true
		}
	}
	if w2 != "" {
		weaponTypeConditions(cond, w2, all1H[1])
		dualWieldConditions(cond, items, w1, w2, all1H)
	}
}

// weaponTypeConditions are the Using<flag> and hand conditions of one
// weapon type (CalcPerform.lua:160-185, 198-221); a weapon that counts as
// all one handed melee types sets each type's condition and
// WieldingDifferentWeaponTypes (162-175, 200-213).
func weaponTypeConditions(cond map[string]bool, typ string, all1H bool) {
	info := weaponTypeInfo[typ]
	cond["Using"+info.flag] = true
	if all1H {
		for _, f := range []string{"Axe", "Sword", "Dagger", "Mace", "Claw"} {
			cond["Using"+f] = true
		}
		cond["WieldingDifferentWeaponTypes"] = true
	}
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
func dualWieldConditions(cond map[string]bool, items map[string]*itemtext.Item, w1, w2 string, all1H [2]bool) {
	cond["DualWielding"] = true
	if (w1 == "Claw" || all1H[0]) && (w2 == "Claw" || all1H[1]) {
		cond["DualWieldingClaws"] = true
	}
	if (w1 == "Dagger" || all1H[0]) && (w2 == "Dagger" || all1H[1]) {
		cond["DualWieldingDaggers"] = true
	}
	differ := weaponKind(items["Weapon 1"], w1) != weaponKind(items["Weapon 2"], w2)
	if differ && weaponTypeInfo[w1].oneHand && weaponTypeInfo[w2].oneHand {
		cond["WieldingDifferentWeaponTypes"] = true
	}
}

// oneHandMeleeTypes are the weapon types countsAsAll1H stands for
// (CalcActiveSkill.lua:220, CalcTools.lua:142-146).
var oneHandMeleeTypes = []string{ //nolint:gochecknoglobals // PoB data, as weaponTypeInfo
	"Claw", "Dagger", "One Handed Axe", "One Handed Mace", "One Handed Sword"}

// all1HAdded is the Added<condition> fields doActorAttribsConditions
// writes on a countsAsAll1H weapon (CalcPerform.lua:162-175, 200-213), of
// the weapon ModStore reads them from (ModStore.lua:635): the main hand's
// if it counts as all one handed types, else the off hand's; nil with
// neither. names are the weapons' item names (weaponData.name).
func all1HAdded(types [2]string, all1H [2]bool, names [2]string) map[string]bool {
	cond := map[string]bool{}
	var added [2]map[string]bool
	hand := func(i int) {
		cond["Using"+weaponTypeInfo[types[i]].flag] = true
		if !all1H[i] {
			return
		}
		a := map[string]bool{}
		for _, f := range []string{"Axe", "Sword", "Dagger", "Mace", "Claw"} {
			v := "Using" + f
			a[v] = !cond[v]
			if f == "Sword" {
				a[v] = strings.Contains(names[i], "Varunastra") || !cond[v] // Varunastra is a sword
			}
			cond[v] = true
		}
		added[i] = a
	}
	if types[0] != "" && types[0] != "None" {
		hand(0)
	}
	if types[1] != "" {
		hand(1)
	}
	if added[0] != nil {
		return added[0]
	}
	return added[1]
}

// addedUsingDagger is weaponData.AddedUsingDagger of each hand
// (CalcPerform.lua:167, 205): a countsAsAll1H weapon that made the actor
// count as using a dagger.
func addedUsingDagger(types [2]string, all1H [2]bool) [2]bool {
	dagger := func(i int) bool { return weaponTypeInfo[types[i]].flag == "Dagger" }
	var out [2]bool
	out[0] = all1H[0] && types[0] != "" && types[0] != "None" && !dagger(0)
	out[1] = all1H[1] && types[1] != "" && !dagger(0) && !all1H[0] && !dagger(1)
	return out
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
