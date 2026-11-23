package engine

import (
	"math"
	"strings"

	"github.com/Mi7teR/poe-lib/modparser"
	"github.com/Mi7teR/poe-lib/pobcode"
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
	mdb.NewMod("CritChanceCap", "BASE", 100, "Base")
	mdb.NewMod("ImpaleStacksMax", "BASE", 5, "Base")
	mdb.conditions["Buffed"], mdb.conditions["Combat"], mdb.conditions["Effective"] = true, true, true
	chargeBaseMods(mdb)
	initConditionMods(mdb)
	shrineBaseMods(mdb)
}

// initMinionDB is initMinionModDB (CalcPerform.lua): the minion's base
// stats from the monster tables and its minion data. Minion items (Animate
// Guardian, Bow and Quiver skills) are not ported.
func (c *Calculator) initMinionDB(db *ModDB, s *activeSkill, m *skillMinion, res *Result) (*ModDB, map[string][]modparser.Mod) {
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
		return nil, nil
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
	if db.Flag("MinionAccuracyEqualsAccuracy") {
		mdb.NewMod("Accuracy", "BASE", val(db, "Accuracy")+val(db, "Dex")*2, "Player") // data.misc.AccuracyPerDexBase
	} else {
		mdb.Add(Mod{Name: "CannotBeEvaded", Type: "FLAG", Value: true, Source: "Minion Attacks always hit"})
	}
	// monsterConstants: base_critical_strike_multiplier 130, critical_ailment_dot_multiplier_+ 30,
	// critical_strike_chance_+%_per_power_charge 50, frenzy charge speed 4, inherent damage 4.
	mdb.NewMod("CritMultiplier", "BASE", 30, "Base")
	mdb.NewMod("DotMultiplier", "BASE", 30, "Base", map[string]any{"type": "Condition", "var": "CriticalStrike"})
	mdb.NewMod("CritChance", "INC", 50, "Base", map[string]any{"type": "Multiplier", "var": "PowerCharge"})
	frenzy := map[string]any{"type": "Multiplier", "var": "FrenzyCharge"}
	mdb.Add(Mod{Name: "Speed", Type: "INC", Value: 4.0, Flags: mfAttack, Source: "Base", Tags: []map[string]any{frenzy}})
	mdb.Add(Mod{Name: "Speed", Type: "INC", Value: 4.0, Flags: mfCast, Source: "Base", Tags: []map[string]any{frenzy}})
	mdb.NewMod("Damage", "MORE", 4, "Base", frenzy)
	mdb.NewMod("ProjectileCount", "BASE", 1, "Base")
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
	gear := c.minionItems(db, mdb, s, res)
	if db.Sum("BASE", "StrengthAddedToMinions") > 0 {
		mdb.NewMod("Str", "BASE", round(val(db, "Str")*db.Sum("BASE", "StrengthAddedToMinions")/100, 0), "Player")
	}
	return mdb, gear
}

// minionItems is the minion.uses part of initMinionModDB: the items in the
// slots the minion uses, from its item set (Animate Guardian: the gem's
// skillMinionItemSet, else the first item set) or else the player's.
func (c *Calculator) minionItems(db, mdb *ModDB, s *activeSkill, res *Result) map[string][]modparser.Mod {
	b := db.build
	uses := s.active.ge.MinionUses
	if b == nil || len(uses) == 0 {
		return nil
	}
	var set *pobcode.ItemSet
	if s.active.ge.MinionHasItemSet {
		id := 0
		if s.gem != nil {
			id = s.gem.SkillMinionItemSet
		}
		for i := range b.ItemSets {
			if b.ItemSets[i].ID == id {
				set = &b.ItemSets[i]
			}
		}
		if set == nil && len(b.ItemSets) > 0 {
			set = &b.ItemSets[0] // itemSetOrderList[1]
		}
	}
	gear := map[string][]modparser.Mod{}
	for _, slot := range uses {
		var id int
		switch {
		case set != nil:
			name := slot
			if set.UseSecondWeaponSet && (slot == "Weapon 1" || slot == "Weapon 2") {
				name += " Swap"
			}
			id = set.SlotItem(name)
		case b.ActiveItems() != nil:
			id = b.ActiveItems().SlotItem(slot)
		}
		if it, mods := c.addItem(mdb, res, b.Item(id), 1, slot); it != nil {
			mdb.items[slot] = it
			gear[slot] = mods
		}
	}
	return gear
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
func minionBuffs(db *ModDB, skills []*activeSkill, mdb *ModDB, mainCfg *Cfg, res *Result) map[string][]Mod {
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
			case "Warcry":
				if list.Flag("CannotShareWarcryBuffs") {
					continue
				}
				bonus, uptime := warcryBonus(db, s, b)
				warcry := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(b.name, " Cry", ""), "'s", ""), " ", "")
				mdb.conditions["AffectedBy"+warcry] = true
				inc := list.SumCfg(cfg, "INC", "BuffEffect") + mdb.SumCfg(cfg, "INC", "BuffEffectOnSelf")
				more := list.MoreCfg(cfg, "BuffEffect") * mdb.MoreCfg(cfg, "BuffEffectOnSelf")
				var src []Mod
				for i, m := range b.mods {
					src = append(src, scaleMod(m, (1+inc/100)*more*bonus[i]*uptime))
				}
				if s.active.ge.Name == "Rallying Cry" {
					src = append(src, rallyingCryWeaponDamage(db, s, mainCfg, (1+inc/100)*uptime)...)
				}
				mergeBuff(src, out, b.name)
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

// rallyingCryWeaponDamage is the minion side's "flat damage bonus" of
// Rallying Cry in calcs.perform: a share of the player's main hand weapon
// damage per 5 warcry power (up to 30). An unarmed player is not ported.
func rallyingCryWeaponDamage(db *ModDB, s *activeSkill, mainCfg *Cfg, scale float64) []Mod {
	w, _, ok := itemWeapon(db.items["Weapon 1"], db.gearMods["Weapon 1"])
	if !ok {
		return nil
	}
	power, ok := db.Override("WarcryPower")
	if !ok {
		power = math.Max(db.Sum("BASE", "WarcryPower")*(1+db.Sum("INC", "WarcryPower")/100), db.Sum("BASE", "MinimumWarcryPower"))
	}
	effect := math.Floor(s.mods.SumCfg(mainCfg, "BASE", "RallyingCryAllyDamageBonusPer5Power") * math.Floor(math.Min(power, 30)/5))
	more := 1 + s.mods.SumCfg(mainCfg, "BASE", "RallyingCryMinionDamageBonusMultiplier")
	tag := map[string]any{"type": "GlobalEffect", "effectType": "Warcry", "div": 5.0, "limit": 30.0}
	var out []Mod
	for _, t := range dmgTypeList {
		if w.min[t] == 0 && w.max[t] == 0 {
			continue
		}
		for _, x := range []struct {
			name string
			v    float64
		}{{t + "Min", w.min[t]}, {t + "Max", w.max[t]}} {
			out = append(out, scaleMod(Mod{Name: x.name, Type: "BASE", Value: x.v * effect / 100,
				KeywordFlags: kfAttack, Source: "Rallying Cry", Tags: []map[string]any{tag}}, scale*more))
		}
	}
	return out
}

// extraAurasToMinion is the minion side of calcs.perform's ExtraAura loop:
// "you and allies within 6 metres" mods reach env.minion, scaled by its
// buff and aura effect on self.
func extraAurasToMinion(db, mdb *ModDB) {
	scale := (1 + mdb.Sum("INC", "BuffEffectOnSelf", "AuraEffectOnSelf")/100) * mdb.More("BuffEffectOnSelf", "AuraEffectOnSelf")
	for _, e := range db.List(nil, "ExtraAura") {
		v, _ := e.Value.(map[string]any)
		mm, ok := v["mod"].(map[string]any)
		if !ok || !(truthy(v["fromAllies"]) || !db.Flag("SelfAurasCannotAffectAllies")) {
			continue
		}
		mdb.Add(scaleMod(modFromMap(mm, e.Mod.Source), scale))
	}
}

var (
	debugMinionOffence = false
	debugMinionSkill   func(*activeSkill)
	debugEnemy         func(*ModDB)
)

var minionOffenceStats = []string{"Speed", "HitChance", "CritChance", "CritMultiplier", "AverageDamage", "TotalDPS"}

// minionWeapon is the minion's weaponData1 of CalcActiveSkill.lua and its
// type: the main hand item of a minion that uses one (Animate Guardian's
// item set), else damage from the ally (or monster, for spectres) damage
// table and its attack time; and its off hand weapon (weaponData2), if
// any. Minions using the player's weapons are not ported.
func (c *Calculator) minionWeapon(s *activeSkill, m *skillMinion, mdb *ModDB, gear map[string][]modparser.Mod) (hitSource, string, *hitSource, string) {
	var w2 *hitSource
	w2Type := ""
	if contains(s.active.ge.MinionUses, "Weapon 2") {
		if w, typ, ok := itemWeapon(mdb.items["Weapon 2"], gear["Weapon 2"]); ok {
			w2, w2Type = &w, typ
		}
	}
	if contains(s.active.ge.MinionUses, "Weapon 1") {
		if w, typ, ok := itemWeapon(mdb.items["Weapon 1"], gear["Weapon 1"]); ok {
			return w, typ, w2, w2Type
		}
	}
	typ := m.data.WeaponType1
	if typ == "" {
		typ = "None"
	}
	table := c.data.Monster.AllyDamage
	if s.flags["spectre"] || m.data.Hostile {
		table = c.data.Monster.Damage
	}
	w := hitSource{min: map[string]float64{}, max: map[string]float64{}, critChance: 5}
	if m.level > len(table) || m.data.AttackTime == 0 {
		return w, typ, w2, w2Type
	}
	damage := table[m.level-1] * m.data.Damage
	if !m.data.BaseDamageIgnoresAttackSpeed {
		damage *= m.data.AttackTime
	}
	w.attackRate = 1 / m.data.AttackTime
	w.min["Physical"] = round(damage*(1-m.data.DamageSpread), 0)
	w.max["Physical"] = round(damage*(1+m.data.DamageSpread), 0)
	return w, typ, w2, w2Type
}

var minionResultStats = []string{
	"Life", "EnergyShield", "Armour", "Evasion", "Str", "Dex", "Int",
	"FireResist", "ColdResist", "LightningResist", "ChaosResist",
	"BlockChance", "SpellBlockChance",
}

// mainMinion computes env.minion, the main skill's minion, the way
// calcs.perform does: its ModDB, the buffs and auras that reach it, then
// calcs.defence. Minion items, flasks, curse, warcry and link buffs on
// minions are not ported.
func (c *Calculator) mainMinion(db *ModDB, skills []*activeSkill, buffs map[string][]Mod, charLevel int, res *Result) *MinionResult {
	s := mainSkill(skills)
	if s == nil || s.disabled {
		return nil
	}
	m := c.minionOf(s, charLevel, db.enemyEnv.level)
	if m == nil || m.data.Hostile {
		return nil
	}
	mdb, gear := c.initMinionDB(db, s, m, res)
	if mdb == nil {
		return nil
	}
	weapon, weaponType, weapon2, weapon2Type := c.minionWeapon(s, m, mdb, gear)
	mskills := c.createMinionSkills(mdb, s, m, [2]string{weaponType, weapon2Type}, res)
	ms := mskills[0]
	addMinionModifiers(mdb, s.mods.List(s.cfg, "MinionModifier"), m.typ)
	c.mergeKeystones(mdb, map[string]bool{}, res)
	c.attributes(mdb)
	shrineBuffs(mdb)
	c.lifeMana(mdb)
	mb := minionBuffs(db, skills, mdb, s.cfg, res)
	for k, l := range c.minionSkillBuffs(db, mdb, skills, s, mskills, charLevel, res) {
		mergeBuff(l, mb, k)
	}
	for _, l := range buffMinionModifiers(buffs, s.cfg) {
		addMinionModifiers(mdb, l, m.typ)
	}
	for _, l := range mb {
		for _, x := range l {
			mdb.Add(x)
		}
	}
	extraAurasToMinion(db, mdb)
	defenceForConditionals(mdb, gear)
	charges(mdb)
	actorMisc(mdb, ms, false, 0)
	c.resistances(mdb)
	c.defence(mdb, gear, map[string]bool{})
	stats := map[string]float64{}
	for _, k := range minionResultStats {
		stats[k] = mdb.output[k]
	}
	if ms != nil {
		if debugMinionSkill != nil {
			debugMinionSkill(ms)
		}
		if debugEnemy != nil && debugMinionOffence {
			debugEnemy(mdb)
		}
		for k, v := range c.offence(mdb, ms, weapon, weapon2) {
			if contains(minionOffenceStats, k) || debugMinionOffence {
				stats[k] = v
			}
		}
	}
	return &MinionResult{Skill: s.active.ge.Name, Minion: m.data.Name, Stats: stats}
}

// CalcMinions calculates the build once per enabled minion skill with that
// skill as the main skill, as PoB does when you select it, and returns
// each skill's minion.
func (c *Calculator) CalcMinions(b *pobcode.Build) ([]MinionResult, error) {
	res, err := c.Calc(b)
	if err != nil {
		return nil, err
	}
	var out []MinionResult
	for _, s := range res.Skills {
		if !s.HasMinion || s.Disabled {
			continue
		}
		r, err := c.Calc(withMainSkill(b, s.Group, s.GroupSkill))
		if err != nil {
			return nil, err
		}
		if r.Minion != nil {
			out = append(out, *r.Minion)
		}
	}
	return out, nil
}

// withMainSkill returns a copy of b whose main skill is the n-th active
// skill of socket group g (both 1-based) of the active skill set.
func withMainSkill(b *pobcode.Build, g, n int) *pobcode.Build {
	cp := *b
	cp.SkillSets = append([]pobcode.SkillSet(nil), b.SkillSets...)
	set := cp.ActiveSkills()
	if set == nil {
		return &cp
	}
	set.Groups = append([]pobcode.SocketGroup(nil), set.Groups...)
	set.Groups[g-1].MainActiveSkill = n
	cp.MainSocketGroup = g
	return &cp
}

// createMinionSkills is calcs.createMinionSkills (CalcActiveSkill.lua):
// the minion's skills (its minion data's skill list and ExtraMinionSkill
// mods) at the highest level its level allows, supported by the summoning
// skill's supports, with mod lists on the minion's ModDB. The first is the
// minion's main skill (skillMinionSkill is not stored).
func (c *Calculator) createMinionSkills(mdb *ModDB, s *activeSkill, m *skillMinion, weapons [2]string, res *Result) []*activeSkill {
	skills := c.data.Skills.Effects
	var ids []string
	for _, id := range m.data.SkillList {
		if skills[id] != nil {
			ids = append(ids, id)
		}
	}
	for _, e := range s.mods.List(s.cfg, "ExtraMinionSkill") {
		v, _ := e.Value.(map[string]any)
		id, _ := v["skillId"].(string)
		if l, ok := v["minionList"].([]any); ok {
			found := false
			for _, t := range l {
				found = found || t == m.typ
			}
			if !found {
				continue
			}
		}
		if skills[id] != nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		ids = []string{"Melee"}
	}
	var list []*activeSkill
	for _, id := range ids {
		ge := skills[id]
		level := 1
		if len(ge.Levels) > 1 {
			for l := 1; ge.Levels[l] != nil; l++ {
				if ge.Levels[l].LevelRequirement > m.level {
					break
				}
				level = l
			}
		}
		eff := &skillEffect{id: id, ge: ge, level: level, actorLevel: float64(m.level)}
		ms := c.createActiveSkill(eff, s.supportList, nil, weapons, s)
		ms.cfg = &Cfg{}
		c.buildSkillMods(mdb, ms, res)
		ms.flags["minion"], ms.flags["minionSkill"], ms.flags["haveMinion"] = true, true, true
		ms.flags["spectre"] = s.flags["spectre"]
		eff2, _ := num(s.data["minionDamageEffectiveness"])
		ms.data["damageEffectiveness"] = 1 + eff2/100
		list = append(list, ms)
	}
	return list
}

// minionSkillBuffs is the env.minion side of calcs.perform's loop over the
// minions' skills: their enabled Buff and Aura buffs that reach the main
// minion. Other minions' skills are built on a bare minion ModDB, as PoB
// never initialises those. The player side (allies' auras) is not ported.
func (c *Calculator) minionSkillBuffs(db, mdb *ModDB, skills []*activeSkill, main *activeSkill, mainSkills []*activeSkill, charLevel int, res *Result) map[string][]Mod {
	out := map[string][]Mod{}
	for _, s := range skills {
		if s.disabled {
			continue
		}
		casting := mdb
		list := mainSkills
		if s != main {
			m := c.minionOf(s, charLevel, db.enemyEnv.level)
			if m == nil {
				continue
			}
			casting = NewModDB()
			casting.enemy, casting.owner, casting.build, casting.enemyEnv = db.enemy, db, db.build, db.enemyEnv
			list = c.createMinionSkills(casting, s, m, [2]string{m.data.WeaponType1}, res)
		}
		for _, ms := range list {
			if !truthy(ms.data["enable"]) {
				continue
			}
			skillList, cfg := ms.mods, ms.cfg
			for _, b := range ms.buffs {
				name := strings.ReplaceAll(b.name, " ", "")
				switch b.typ {
				case "Buff":
					isCasting := casting == mdb
					if !(b.applyMinions || isCasting || b.applyAllies) {
						continue
					}
					store, bcfg := casting, (*Cfg)(nil)
					if b.activeSkillBuff {
						store, bcfg = skillList, cfg
					}
					mdb.conditions["AffectedBy"+name] = true
					names := []string{"BuffEffect"}
					if isCasting {
						names = append(names, "BuffEffectOnSelf")
					}
					mult := (1 + store.SumCfg(bcfg, "INC", names...)/100) * store.MoreCfg(bcfg, names...)
					mergeBuff(scaleMods(b.mods, mult), out, b.name)
					mergeBuff(b.mods, out, b.name)
				case "Aura":
					if casting.Flag("SelfAurasCannotAffectAllies") || casting.Flag("SelfAurasOnlyAffectYou") ||
						casting.Flag("SelfAuraSkillsCannotAffectAllies") || skillList.FlagCfg(cfg, "SelfAurasAffectYouAndLinkedTarget") {
						continue
					}
					if mdb.conditions["AffectedBy"+name] || (casting == mdb && truthy(s.data["auraCannotAffectSelf"])) {
						continue
					}
					inc := skillList.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect") + mdb.SumCfg(cfg, "INC", "BuffEffectOnSelf", "AuraEffectOnSelf")
					more := skillList.MoreCfg(cfg, "AuraEffect", "BuffEffect") * mdb.MoreCfg(cfg, "BuffEffectOnSelf", "AuraEffectOnSelf")
					mult := (1 + inc/100) * more
					mdb.conditions["AffectedBy"+name] = true
					mdb.conditions["AffectedByAura"] = true
					mergeBuff(append(scaleMods(b.mods, mult), scaleMods(extraAuraMods(casting, cfg), mult)...), out, b.name)
				}
			}
		}
	}
	return out
}
