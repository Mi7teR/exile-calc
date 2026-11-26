package engine

import (
	"strings"
)

// cursePriority is Data.lua data.cursePriority.
var cursePriority = map[string]float64{
	"Temporal Chains": 1, "Enfeeble": 2, "Vulnerability": 3, "Elemental Weakness": 4,
	"Flammability": 5, "Frostbite": 6, "Conductivity": 7, "Despair": 8, "Punishment": 9,
	"Warlord's Mark": 10, "Assassin's Mark": 11, "Sniper's Mark": 12, "Poacher's Mark": 13,
	"SocketPriorityBase": 100,
	"Weapon 1":           1000, "Amulet": 2000, "Helmet": 3000, "Weapon 2": 4000,
	"Body Armour": 5000, "Gloves": 6000, "Boots": 7000,
	"Ring 1": 8000, "Ring 2": 9000, "Ring 3": 10000,
	"CurseFromEquipment": 11000, "CurseFromAura": 20000,
}

// enemyCurse is a curse of CalcPerform.lua's curses list.
type enemyCurse struct {
	name           string
	priority       float64
	isMark         bool
	ignoreHexLimit bool
	socketedLimit  bool // socketedCursesHexLimit
	// minion: the curse is in minionCurses (a minion's skill), slotted
	// after the player's curses within minionCurses.limit.
	minion         bool
	mods, buffMods []Mod
	// curseBuffMods is a CurseBuff's mods scaled by curse effect (temp,
	// CalcPerform.lua:2727-2728), minionBuffMods those for env.minion
	// (curse.minionBuffModList, 2733-2738).
	curseBuffMods, minionBuffMods []Mod
}

// cursePriorityOf is CalcPerform.lua determineCursePriority; s is nil for
// curses not from an active skill.
func (c *Calculator) cursePriorityOf(name string, s *activeSkill) float64 {
	source, slot, socket := "", "", 1
	if s != nil && s.group != nil {
		source, slot = s.group.Source, s.group.Slot
		for k := range s.group.Gems {
			if gem, _, ok := c.resolveGem(&s.group.Gems[k]); ok && gem != nil && gem.Name == name {
				socket = min(k+1, 8)
				break
			}
		}
	}
	// slot:gsub(" (Swap)", ""): the parentheses are a capture, so " Swap" goes.
	slotPriority := cursePriority[strings.ReplaceAll(slot, " Swap", "")]
	sourcePriority := 0.0
	if s != nil && s.types[stAura] {
		sourcePriority = cursePriority["CurseFromAura"]
	} else if source != "" {
		sourcePriority = cursePriority["CurseFromEquipment"]
	}
	if source != "" && (slotPriority == cursePriority["Ring 2"] || slotPriority == cursePriority["Ring 3"]) {
		slotPriority = cursePriority["Ring 1"]
	}
	return cursePriority[name] + float64(
		float64(socket)*cursePriority["SocketPriorityBase"],
	) + slotPriority + sourcePriority
}

// skillCurse is the Curse/CurseBuff branch of the buff loop in
// calcs.perform: the curse an active skill puts on the enemy, or nil.
func (c *Calculator) skillCurse(db *ModDB, s *activeSkill, b *skillBuff) *enemyCurse {
	list, cfg, enemy := s.mods, s.cfg, db.enemy
	mark := s.types[stMark]
	db.conditions["SelfCast"+strings.ReplaceAll(b.name, " ", "")] = !(s.types[stTriggered] || s.types[stAura])
	if !mark && db.enemyEnv != nil &&
		db.enemyEnv.checks["balanceOfTerrorSelfCast"+strings.ReplaceAll(b.name, " ", "")] {
		return nil
	}
	// CalcPerform.lua:2696: a mark applies in every buff mode.
	hexproof := enemy.Flag("Hexproof") && !db.Flag("CursesIgnoreHexproof") &&
		!truthy(s.data.val("ignoreHexLimit")) && !truthy(s.data.val("ignoreHexproof"))
	if !mark && (!modeEffective(db) || hexproof) {
		return nil
	}
	cu := &enemyCurse{
		name:           b.name,
		priority:       c.cursePriorityOf(b.name, s),
		isMark:         mark,
		ignoreHexLimit: (list.FlagCfg(cfg, "CursesIgnoreHexLimit") || truthy(s.data.val("ignoreHexLimit"))) && !mark,
		socketedLimit:  db.FlagCfg(cfg, "SocketedCursesAdditionalLimit"),
	}
	inc := list.SumCfg(cfg, "INC", "CurseEffect") + enemy.Sum("INC", "CurseEffectOnSelf")
	if s.types[stAura] && !s.types[stRemoteMined] {
		inc += list.SumCfg(cfg, "INC", "AuraEffect")
	}
	more := list.MoreCfg(cfg, "CurseEffect")
	moreMark := more
	if !mark {
		more *= enemy.More("CurseEffectOnSelf")
	}
	mult := 0.0
	if !((db.Flag("SelfAurasOnlyAffectYou") || list.FlagCfg(cfg, "SelfAurasAffectYouAndLinkedTarget")) && s.types[stAura]) {
		mult = (1 + inc/100) * more
	}
	if b.typ == "Curse" {
		cu.mods = scaleMods(b.mods, mult)
		if e := db.partyExports; e != nil {
			// CalcPerform.lua:2722-2724.
			effect := (1 + inc/100) * moreMark
			if mark {
				effect = mult
			}
			e.curse[b.name] = &partyExport{isMark: mark, effectMult: effect, mods: b.mods}
		}
	} else {
		// The curse grants a buff: scaled by curse effect, then buff effect.
		buffMult := (1 + db.SumCfg(cfg, "INC", "BuffEffectOnSelf")/100) * db.MoreCfg(cfg, "BuffEffectOnSelf")
		cu.curseBuffMods = scaleMods(b.mods, mult)
		cu.buffMods = scaleMods(cu.curseBuffMods, buffMult)
	}
	return cu
}

// applyCurses ports "Assign curses to slots" of calcs.perform for the
// player's, the minions' (minionCurses) and the party's curses
// (allyCurses) and puts the slotted curses on the enemy.
func (c *Calculator) applyCurses(db *ModDB, skills []*activeSkill, curses []*enemyCurse, res *Result) []*enemyCurse {
	limit := int(db.Sum("BASE", "EnemyCurseLimit"))
	if db.Flag("CurseLimitIsMaximumPowerCharges") {
		limit = int(db.Sum("BASE", "PowerChargesMax"))
		res.Incomplete = append(res.Incomplete, "curse limit from maximum power charges uses the base maximum")
	}
	db.output["EnemyCurseLimit"] = float64(limit) // CalcPerform.lua:3250
	if e := db.partyExports; e != nil {
		e.curseLimit = float64(limit) // CalcPerform.lua:3252
	}
	allyCurses, allyLimit := db.party.partyCurses(db.enemy)
	var own, minion []*enemyCurse
	for _, cu := range curses {
		if cu.minion {
			minion = append(minion, cu)
		} else {
			own = append(own, cu)
		}
	}
	sources := []struct {
		curses []*enemyCurse
		limit  int
	}{{own, limit}, {minion, c.spectreEffectsOf(db, skills).curseLimit}, {allyCurses, allyLimit}}
	var slots []*enemyCurse
	markSlotted := false
	for _, src := range sources {
		slots, markSlotted = slotCurses(db, skills, src.curses, src.limit, slots, markSlotted)
	}
	// Lua's #curseSlots stops at the first hole.
	for i, s := range slots {
		if s == nil {
			slots = slots[:i]
			break
		}
	}
	slots = placeUnlimitedCurses(db, curses, slots)
	db.multipliers["CurseOnEnemy"] = float64(len(slots))
	for _, s := range slots {
		db.enemy.conditions["Cursed"] = true
		if s.isMark {
			db.enemy.conditions["Marked"] = true
		}
		for _, m := range s.mods {
			db.enemy.Add(m)
		}
		for _, m := range s.buffMods {
			db.Add(m)
		}
	}
	return slots
}

// slotCurses is one source of the first slot loop of "Assign curses to
// slots": curses within the hex limit, by priority, up to the source's
// limit. Slots and the mark flag are shared between sources.
func slotCurses(
	db *ModDB,
	skills []*activeSkill,
	curses []*enemyCurse,
	limit int,
	slots []*enemyCurse,
	markSlotted bool,
) ([]*enemyCurse, bool) {
	for _, cu := range curses {
		if cu.ignoreHexLimit || cu.socketedLimit {
			continue
		}
		skip := false
		for _, s := range skills {
			if len(s.buffs) > 0 && cu.name == s.buffs[0].name && s.types[stAura] {
				if db.Flag("SelfAurasOnlyAffectYou") ||
					s.mods.FlagCfg(mainCfg(skills), "SelfAurasAffectYouAndLinkedTarget") {
					skip = true
				}
				break
			}
		}
		slot := -1
		for i := 0; i < limit; i++ {
			if cu.isMark && markSlotted {
				slot = -1
				break
			}
			if i >= len(slots) || slots[i] == nil {
				slot = i
				break
			} else if slots[i].name == cu.name {
				if slots[i].priority < cu.priority {
					slot = i
				} else {
					slot = -1
				}
				break
			} else if slots[i].priority < cu.priority {
				slot = i
			}
		}
		if slot < 0 {
			continue
		}
		for len(slots) <= slot {
			slots = append(slots, nil)
		}
		if slots[slot] != nil && slots[slot].isMark {
			markSlotted = false
		}
		if !skip {
			slots[slot] = cu
		}
		if cu.isMark {
			markSlotted = true
		}
	}
	return slots, markSlotted
}

// placeUnlimitedCurses is the second slot loop: the player's curses that
// ignore the hex limit or have the socketed curses' own limit.
func placeUnlimitedCurses(db *ModDB, curses []*enemyCurse, slots []*enemyCurse) []*enemyCurse {
	for _, cu := range curses {
		if cu.ignoreHexLimit {
			found := false
			for i, s := range slots {
				if s.name == cu.name {
					if s.priority < cu.priority {
						slots[i] = cu
					}
					found = true
					break
				}
			}
			if !found {
				slots = append(slots, cu)
			}
		}
		if cu.socketedLimit {
			limitValue := int(db.Sum("BASE", "SocketedCursesHexLimitValue"))
			skip := false
			for i, s := range slots {
				if s.name == cu.name {
					if s.priority < cu.priority {
						slots[i] = cu
					}
					skip = true
					break
				}
				if i+1 >= limitValue {
					skip = true
				}
			}
			if !skip {
				slots = append(slots, cu)
			}
		}
	}
	return slots
}
