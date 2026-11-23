package engine

import (
	"math"
	"strings"
)

// MinionResult is one minion skill's minion: PoB's output.Minion with that
// skill as the main skill.
type MinionResult struct {
	Skill  string // the summoning skill
	Minion string // the minion's name
	Stats  map[string]float64
}

// minionBaseMods are the calcs.initModDB base mods (CalcSetup.lua) that the
// minion's defence reads; the player gets its own in Calc.
func minionBaseMods(mdb *ModDB) {
	for _, r := range []string{"Fire", "Cold", "Lightning", "Chaos"} {
		mdb.NewMod(r+"ResistMax", "BASE", baseMaxResist, "Base")
		mdb.NewMod("Totem"+r+"ResistMax", "BASE", baseMaxResist, "Base")
	}
	mdb.NewMod("BlockChanceMax", "BASE", 75, "Base")
	mdb.NewMod("SpellBlockChanceMax", "BASE", 75, "Base")
	chargeBaseMods(mdb)
	initConditionMods(mdb)
}

// initMinionDB is initMinionModDB (CalcPerform.lua): the minion's base
// stats from the monster tables and its minion data. Minion items (Animate
// Guardian, Bow and Quiver skills) are not ported.
func (c *Calculator) initMinionDB(db *ModDB, s *activeSkill, m *skillMinion, res *Result) *ModDB {
	mon := c.data.Monster
	table := mon.AllyLife
	switch {
	case m.data.LifeScaling == "AltLife1":
		table = mon.Life2
	case m.data.LifeScaling == "AltLife2":
		table = mon.Life3
	case s.flags["spectre"]:
		table = mon.Life
	}
	if m.level > len(table) || m.level > len(mon.Armour) || m.level > len(mon.Evasion) {
		return nil
	}
	mdb := NewModDB()
	mdb.enemy, mdb.owner = db.enemy, db
	mdb.multipliers["Level"] = float64(m.level)
	minionBaseMods(mdb)
	mdb.NewMod("Life", "BASE", math.Floor(table[m.level-1]*m.data.Life), "Base")
	if m.data.EnergyShield != 0 {
		mdb.NewMod("EnergyShield", "BASE", math.Floor(mon.AllyLife[m.level-1]*m.data.Life*m.data.EnergyShield), "Base")
	}
	// minionData.armour or 1: no minion has an explicit 0.
	armour, evasion := m.data.Armour, m.data.Evasion
	if armour == 0 {
		armour = 1
	}
	if evasion == 0 {
		evasion = 1
	}
	mdb.NewMod("Armour", "BASE", round(mon.Armour[m.level-1]*armour, 0), "Base")
	mdb.NewMod("Evasion", "BASE", round(mon.Evasion[m.level-1]*evasion, 0), "Base")
	mdb.NewMod("FireResist", "BASE", m.data.FireResist, "Base")
	mdb.NewMod("ColdResist", "BASE", m.data.ColdResist, "Base")
	mdb.NewMod("LightningResist", "BASE", m.data.LightningResist, "Base")
	mdb.NewMod("ChaosResist", "BASE", m.data.ChaosResist, "Base")
	endurance := map[string]any{"type": "Multiplier", "var": "EnduranceCharge"}
	mdb.NewMod("PhysicalDamageReduction", "BASE", 4, "Base", endurance)  // monsterConstants physical_damage_reduction_%_per_endurance_charge
	mdb.NewMod("ElementalDamageReduction", "BASE", 4, "Base", endurance) // elemental_damage_reduction_%_per_endurance_charge_if_player_minion
	mdb.NewMod("MaximumFortification", "BASE", 20, "Base")               // base_max_fortification
	for _, mm := range m.data.ModList {
		mdb.Add(modFromMap(mm, "Minion:"+m.data.Name))
	}
	if truthy(s.data["minionUseBowAndQuiver"]) || truthy(s.data["minionUseMainHandWeapon"]) {
		res.Incomplete = append(res.Incomplete, "minion weapons not applied: "+m.data.Name)
	}
	if db.Sum("BASE", "StrengthAddedToMinions") > 0 {
		mdb.NewMod("Str", "BASE", round(val(db, "Str")*db.Sum("BASE", "StrengthAddedToMinions")/100, 0), "Player")
	}
	return mdb
}

// addMinionModifiers is CalcPerform.lua addMinionModifiers: the
// MinionModifier mods of a skill or buff list that match the minion type.
func addMinionModifiers(mdb *ModDB, l []ListEntry, typ string) {
	for _, e := range l {
		v, _ := e.Value.(map[string]any)
		if t, ok := v["type"].(string); ok && t != typ {
			continue
		}
		if mm, ok := v["mod"].(map[string]any); ok {
			mdb.Add(modFromMap(mm, e.Mod.Source))
		}
	}
}

// buffLists returns the MinionModifier entries of each buff list.
func buffMinionModifiers(buffs map[string][]Mod, cfg *Cfg) [][]ListEntry {
	var out [][]ListEntry
	for _, l := range buffs {
		b := NewModDB()
		for _, x := range l {
			b.Add(x)
		}
		out = append(out, b.List(cfg, "MinionModifier"))
	}
	return out
}

// minionBuffs ports the minion side of the player's Buff and Aura branches
// in calcs.perform's buff loop: the buffs that reach env.minion.
func minionBuffs(db *ModDB, skills []*activeSkill, mdb *ModDB) map[string][]Mod {
	out := map[string][]Mod{}
	selfAurasStay := db.Flag("SelfAurasCannotAffectAllies") || db.Flag("SelfAurasOnlyAffectYou") || db.Flag("SelfAuraSkillsCannotAffectAllies")
	for _, s := range skills {
		if s.disabled {
			continue
		}
		list, cfg := s.mods, s.cfg
		for _, b := range s.buffs {
			if b.cond != "" && !list.getCondition(b.cond, cfg, false) {
				continue
			}
			if b.enemyCond != "" && (db.enemy == nil || !db.enemy.GetCondition(b.enemyCond)) {
				continue
			}
			name := strings.ReplaceAll(b.name, " ", "")
			switch b.typ {
			case "Buff":
				if s.flags["totem"] && !b.allowTotemBuff {
					continue
				}
				if !(b.applyMinions || b.applyAllies || list.Flag("BuffAppliesToAllies")) {
					continue
				}
				store, bcfg := db, (*Cfg)(nil)
				if b.activeSkillBuff {
					store, bcfg = list, cfg
				}
				mdb.conditions["AffectedBy"+name] = true
				inc := store.SumCfg(bcfg, "INC", "BuffEffect") + mdb.Sum("INC", "BuffEffectOnSelf")
				more := store.MoreCfg(bcfg, "BuffEffect") * mdb.More("BuffEffectOnSelf")
				mergeBuff(scaleMods(b.mods, (1+inc/100)*more), out, b.name)
			case "Aura":
				if selfAurasStay {
					continue
				}
				inc := list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect") + mdb.SumCfg(cfg, "INC", "BuffEffectOnSelf", "AuraEffectOnSelf")
				more := list.MoreCfg(cfg, "AuraEffect", "BuffEffect") * mdb.MoreCfg(cfg, "BuffEffectOnSelf", "AuraEffectOnSelf")
				mult := (1 + inc/100) * more
				mdb.conditions["AffectedBy"+name] = true
				mdb.conditions["AffectedByAura"] = true
				mergeBuff(append(scaleMods(b.mods, mult), scaleMods(extraAuraMods(db, cfg), mult)...), out, b.name)
			}
		}
	}
	return out
}

var minionResultStats = []string{
	"Life", "EnergyShield", "Armour", "Evasion", "Str", "Dex", "Int",
	"FireResist", "ColdResist", "LightningResist", "ChaosResist",
	"BlockChance", "SpellBlockChance",
}

// minionResults computes each minion skill's minion the way calcs.perform
// does for env.minion when that skill is the main skill: its ModDB, the
// buffs and auras that reach it, then calcs.defence. Minion items, flasks,
// curse and link buffs on minions are not ported.
func (c *Calculator) minionResults(db *ModDB, skills []*activeSkill, buffs map[string][]Mod, charLevel int, res *Result) []MinionResult {
	var out []MinionResult
	for _, s := range skills {
		if s.disabled {
			continue
		}
		m := c.minionOf(s, charLevel, db.enemyEnv.level)
		if m == nil || m.data.Hostile {
			continue
		}
		mdb := c.initMinionDB(db, s, m, res)
		if mdb == nil {
			continue
		}
		addMinionModifiers(mdb, s.mods.List(s.cfg, "MinionModifier"), m.typ)
		c.mergeKeystones(mdb, map[string]bool{}, res)
		c.attributes(mdb)
		c.lifeMana(mdb)
		mb := minionBuffs(db, skills, mdb)
		for _, l := range buffMinionModifiers(buffs, s.cfg) {
			addMinionModifiers(mdb, l, m.typ)
		}
		for _, l := range mb {
			for _, x := range l {
				mdb.Add(x)
			}
		}
		defenceForConditionals(mdb, nil)
		charges(mdb)
		c.resistances(mdb)
		c.defence(mdb, nil, map[string]bool{})
		stats := map[string]float64{}
		for _, k := range minionResultStats {
			stats[k] = mdb.output[k]
		}
		out = append(out, MinionResult{Skill: s.active.ge.Name, Minion: m.data.Name, Stats: stats})
	}
	return out
}
