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
	// out is the minion's whole output (env.minion.output) and mainSkill
	// its main skill's name, for calcFullDPS.
	out       map[string]float64
	mainSkill string
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
	mdb.NewMod("ImpaleStacksMax", "BASE", 5, "Base")
	mdb.NewMod("SoulEaterMax", "BASE", soulEaterMaxStacks, "Base")
	mdb.NewMod("ShockStacksMax", "BASE", 1, "Base")
	mdb.NewMod("ScorchStacksMax", "BASE", 1, "Base")
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
	mdb.enemy, mdb.owner, mdb.minionData = db.enemy, db, m.data
	mdb.multipliers["Level"] = float64(m.level)
	minionBaseMods(mdb)
	copyModeConditions(mdb, db)
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
	for _, em := range s.extraSkillMods {
		mdb.Add(em)
	}
	// CalcPerform.lua:1203-1206: Necromantic Aegis gives every minion the
	// player's shield, as its "Weapon 3".
	if a := db.aegis; a != nil {
		mdb.items["Weapon 3"] = a.item
		for _, x := range a.mods {
			mdb.Add(x)
		}
	}
	c.minionPlayerGear(db, mdb, s, m.typ, res)
	gear := c.minionItems(db, mdb, s, res)
	if a := db.aegis; a != nil {
		if gear == nil {
			gear = map[string][]modparser.Mod{}
		}
		gear["Weapon 3"] = a.gear
	}
	if db.Sum("BASE", "StrengthAddedToMinions") > 0 {
		mdb.NewMod("Str", "BASE", round(val(db, "Str")*db.Sum("BASE", "StrengthAddedToMinions")/100, 0), "Player")
	}
	return mdb, gear
}

// minionPlayerGear is the part of initMinionModDB that gives a minion the
// player's gear: the bow and quiver (minionUseBowAndQuiver, the quiver
// scaled by WidowHailMultiplier), the gloves (BlinkAndMirrorUseGloves) and
// the main hand weapon (minionUseMainHandWeapon), and The Iron Mass in the
// main hand for Raised Skeletons (env.theIronMass).
func (c *Calculator) minionPlayerGear(db, mdb *ModDB, s *activeSkill, typ string, res *Result) {
	b := db.theBuild()
	if b == nil || b.ActiveItems() == nil {
		return
	}
	add := func(slot string, scale float64) {
		c.addItem(mdb, res, b.Item(b.ActiveItems().SlotItem(slot)), scale, slot, scaleEach)
	}
	baseType := func(slot string) string {
		if it := db.items[slot]; it != nil && it.Base != nil {
			return it.Base.Type
		}
		return ""
	}
	if truthy(s.data.val("minionUseBowAndQuiver")) {
		if baseType("Weapon 1") == "Bow" {
			add("Weapon 1", 1)
		}
		if baseType("Weapon 2") == "Quiver" {
			add("Weapon 2", math.Max(db.Sum("BASE", "WidowHailMultiplier"), 1))
		}
		if db.Flag("BlinkAndMirrorUseGloves") && db.items["Gloves"] != nil {
			add("Gloves", 1)
		}
	}
	if truthy(s.data.val("minionUseMainHandWeapon")) || (typ == "RaisedSkeleton" && wieldsIronMass(db)) {
		add("Weapon 1", 1)
	}
}

// wieldsIronMass: The Iron Mass, Gladius is in the player's main hand.
func wieldsIronMass(db *ModDB) bool {
	it := db.items["Weapon 1"]
	return it != nil && it.Name == "The Iron Mass" && it.BaseName == "Gladius"
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
		if it, mods := c.addItem(mdb, res, b.Item(id), 1, slot, scaleEach); it != nil {
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

// minionBuffsOf ports the minion side of the player's Buff and Aura
// branches in calcs.perform's buff loop for the player's skill s: its buffs
// that reach env.minion, into out. linkToMinion is linksApplyToMinions
// while no Link has linked yet (#linkSkills < 1).
//
//nolint:gocognit,funlen // one case per buff type, as the Lua branches
func minionBuffsOf(db *ModDB, s *activeSkill, mdb *ModDB, mainCfg *Cfg, out map[string][]Mod, linkToMinion *bool) {
	selfAurasStay := db.Flag("SelfAurasCannotAffectAllies") || db.Flag("SelfAurasOnlyAffectYou") || db.Flag("SelfAuraSkillsCannotAffectAllies")
	{
		if s.disabled {
			return
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
				if db.party.auraBeats("Aura", b.name, mult) {
					continue
				}
				mdb.conditions["AffectedBy"+name] = true
				mdb.conditions["AffectedByAura"] = true
				mergeBuff(append(scaleMods(b.mods, mult), scaleMods(extraAuraMods(db, cfg), mult)...), out, b.name)
			case "Link": //nolint:goconst // PoB buff type, as the other cases
				if *linkToMinion {
					// Only the first Link skill links (#linkSkills < 1).
					*linkToMinion = false
					mergeBuff(linkToMinionMods(db, mdb, s, b), out, b.name)
				}
			}
		}
	}
}

// linksApplyToMinion is calcs.perform's linksApplyToMinions: Link skills
// can target damageable minions, the config links the main minion, and
// that minion can be damaged.
func linksApplyToMinion(db, mdb *ModDB, summon *activeSkill) bool {
	return summon != nil && db.Flag("Condition:CanLinkToMinions") && db.Flag("Condition:LinkedToMinion") &&
		!mdb.Flag("Condition:CannotBeDamaged") && !summon.types[stMinionsAreUndamagable]
}

// linkToMinionMods is the linksApplyToMinions part of calcs.perform's Link
// branch: the link's mods and ExtraLinkEffect mods, scaled by link and buff
// effect and the minion's effect on self.
func linkToMinionMods(db, mdb *ModDB, s *activeSkill, b *skillBuff) []Mod {
	list, cfg := s.mods, s.cfg
	extra := extraEffectMods(db, cfg, "ExtraLinkEffect")
	inc := list.SumCfg(cfg, "INC", "LinkEffect", "BuffEffect") + mdb.Sum("INC", "BuffEffectOnSelf", "LinkEffectOnSelf")
	more := list.MoreCfg(cfg, "LinkEffect", "BuffEffect") * mdb.More("BuffEffectOnSelf", "LinkEffectOnSelf")
	mult := (1 + inc/100) * more
	mdb.conditions["AffectedBy"+strings.ReplaceAll(b.name, " ", "")] = true
	mdb.conditions["AffectedByLink"] = true
	return append(scaleMods(b.mods, mult), scaleMods(extra, mult)...)
}

// clawCritToMinion is the Law of the Wilds part of calcs.offence's stat
// conversions for the main skill: claw crit chance and multiplier mods
// also apply to the minion.
func clawCritToMinion(s *activeSkill, mdb *ModDB) {
	cfg := &Cfg{Flags: mfClaw | mfHit}
	for _, x := range []struct{ flag, name, typ string }{
		{"ClawCritChanceAppliesToMinions", "CritChance", "INC"},
		{"ClawCritMultiplierAppliesToMinions", "CritMultiplier", "BASE"},
	} {
		if !s.mods.Flag(x.flag) {
			continue
		}
		for _, r := range s.mods.TabulateCfg(cfg, x.typ, x.name) {
			if r.Flags&mfClaw != 0 {
				if f, ok := num(r.Value); ok {
					mdb.NewMod(x.name, x.typ, f, r.Source)
				}
			}
		}
	}
}

// rallyingCryWeaponDamage is the minion side's "flat damage bonus" of
// Rallying Cry in calcs.perform: a share of the player's main hand weapon
// damage per 5 warcry power (up to 30). An unarmed player is not ported.
func rallyingCryWeaponDamage(db *ModDB, s *activeSkill, mainCfg *Cfg, scale float64) []Mod {
	more := 1 + s.mods.SumCfg(mainCfg, "BASE", "RallyingCryMinionDamageBonusMultiplier")
	out := rallyingCryWeaponMods(db, s, mainCfg)
	for i, m := range out {
		out[i] = scaleMod(m, scale*more)
	}
	return out
}

// rallyingCryWeaponMods is Rallying Cry's extraWarcryModList
// (CalcPerform.lua:2449-2463) before it is scaled.
func rallyingCryWeaponMods(db *ModDB, s *activeSkill, mainCfg *Cfg) []Mod {
	w, _, ok := itemWeapon(db.items["Weapon 1"], db.gearMods["Weapon 1"])
	if !ok {
		return nil
	}
	power, ok := db.Override("WarcryPower")
	if !ok {
		power = math.Max(db.Sum("BASE", "WarcryPower")*(1+db.Sum("INC", "WarcryPower")/100), db.Sum("BASE", "MinimumWarcryPower"))
	}
	effect := math.Floor(s.mods.SumCfg(mainCfg, "BASE", "RallyingCryAllyDamageBonusPer5Power") * math.Floor(math.Min(power, 30)/5))
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
			out = append(out, Mod{Name: x.name, Type: "BASE", Value: x.v * effect / 100, //nolint:mnd // percent
				KeywordFlags: kfAttack, Source: "Rallying Cry", Tags: []map[string]any{tag}})
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
	debugSkills        func([]*activeSkill)
	debugPlayerSkill   func(*activeSkill)
	debugPasses        func([]*hitPass) // tests: per hand outputs
	debugCalcDamage    func(list *ModDB, p *hitPass, t string, inc, more float64, names []string)
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
	w := hitSource{min: map[string]float64{}, max: map[string]float64{}, critChance: 5,
		rng: m.data.AttackRange, hasRange: m.data.AttackRange != 0}
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
// earlyMinionEnemyMods is applyEnemyModifiers(env.minion, true) of
// calcs.perform: env.minion's modDB (base mods and items, set up by
// initMinionModDB at the start of perform) gives its EnemyModifier mods to
// the enemy before curses are assigned, e.g. the Animated Guardian's Dying
// Breath "Nearby Enemies have X% increased Effect of Curses on them".
// PoB differs from the game only in that env.minion exists only while the
// minion's skill is the main skill; that is kept, as the corpus is built
// the same way.
func (c *Calculator) earlyMinionEnemyMods(db *ModDB, skills []*activeSkill, charLevel int) {
	s := mainSkill(skills)
	if s == nil || s.disabled || db.enemyEnv == nil {
		return
	}
	m := c.minionOf(s, charLevel, db.enemyEnv.level)
	if m == nil || m.data.Hostile {
		return
	}
	if mdb, _ := c.initMinionDB(db, s, m, &Result{}); mdb != nil {
		enemyModifiers{}.apply(mdb)
	}
}

// minionInit is env.minion as calcs.perform builds it up to the curse
// slots: initMinionModDB (CalcPerform.lua:1318-1322), its MinionModifier
// mods, keystones and doActorAttribsConditions (1990-1998), and its side
// of the buff loop (2232-3100). Its buffs are added after the curse slots
// (minionApplyBuffs, 3383-3410), its charges and misc buffs in
// stepActorMisc (3527-3528), and its defence, triggers and offence after
// the player's offence (minionOffence, 3836-3845).
//
//nolint:gocognit // calcs.perform's env.minion steps in their order
func (c *Calculator) minionInit(
	db *ModDB, skills []*activeSkill, curses []*enemyCurse, charLevel int, res *Result,
) *minionCalc {
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
	// applyEnemyModifiers(env.minion, true) (CalcPerform.lua:1354) took the
	// EnemyModifier mods initMinionModDB gave (earlyMinionEnemyMods): the
	// second pass in doActorMisc adds only the others.
	minionEnemyMods := enemyModifiers{}
	minionEnemyMods.mark(mdb)
	weapon, weaponType, weapon2, weapon2Type := c.minionWeapon(s, m, mdb, gear)
	// minionUseBowAndQuiver (with a bow) and minionUseMainHandWeapon: the
	// minion's weaponData1 is the player's.
	if pw, ptyp, ok := itemWeapon(db.items["Weapon 1"], db.gearMods["Weapon 1"]); ok &&
		((truthy(s.data.val("minionUseBowAndQuiver")) && ptyp == "Bow") ||
			truthy(s.data.val("minionUseMainHandWeapon")) ||
			(m.typ == "RaisedSkeleton" && wieldsIronMass(db))) {
		weapon, weaponType = pw, ptyp
	}
	if !s.flags["spectre"] && s.mods.FlagCfg(s.cfg, "NonSpectreMinionsUseParentMainHandAttackTime") {
		if pw, _ := playerWeapons(db, db.gearMods, -1); pw.attackRate > 0 {
			weapon.attackRate = pw.attackRate
		}
	}
	all1H := [2]bool{weapon.all1H}
	if weapon2 != nil {
		all1H[1] = weapon2.all1H
	}
	mskills := c.createMinionSkills(mdb, s, m, [2]string{weaponType, weapon2Type}, all1H, res)
	ms := mskills[minionSkillIndex(s, len(mskills))]
	mdb.activeSkills = mskills
	addMinionModifiers(mdb, s.minionModifiers, m.typ)
	c.mergeKeystones(mdb, map[string]bool{}, res)
	minionEquipmentConditions(mdb.conditions, mdb.items, weaponType, weapon2Type, all1H)
	var names [2]string
	for i, slot := range []string{"Weapon 1", "Weapon 2"} {
		if it := mdb.items[slot]; it != nil {
			names[i] = it.Name
		}
	}
	mdb.all1HAdded = all1HAdded([2]string{weaponType, weapon2Type}, all1H, names)
	// CalcPerform.lua:246-296, 400-410 for env.minion: its main skill's
	// conditions, with env.player.mainSkill (the summoning skill s) for
	// the ailment and exposure checks.
	combatConditions(mdb, ms, s)
	c.attributes(mdb)
	shrineBuffs(mdb)
	c.lifeMana(mdb)
	// The buff loop: the spectres' modList buff (CalcPerform.lua:2282)
	// before it, then per player skill its buffs that reach env.minion and
	// its minions' skill buffs (2342-3030), then the party's (3031-3089).
	mb := map[string][]Mod{}
	if sp := c.spectreEffectsOf(db, skills); len(sp.minion) > 0 {
		mb["Spectre"] = append(mb["Spectre"], sp.minion...)
	}
	link := linksApplyToMinion(db, mdb, s)
	for _, x := range skills {
		minionBuffsOf(db, x, mdb, s.cfg, mb, &link)
		c.minionSkillBuffsOf(db, mdb, x, s, mskills, charLevel, mb, res)
	}
	db.party.partyBuffsOnMinion(mdb, mb)
	// CalcPerform.lua:2733-2738: a curse that grants a buff also grants it
	// to env.minion, scaled by the minion's buff effect on self.
	for _, cu := range curses {
		if cu.curseBuffMods != nil {
			scale := (1 + mdb.Sum("INC", "BuffEffectOnSelf")/100) * mdb.More("BuffEffectOnSelf")
			cu.minionBuffMods = scaleMods(cu.curseBuffMods, scale)
		}
	}
	r := &MinionResult{Skill: s.active.ge.Name, Minion: m.data.Name, Stats: map[string]float64{}}
	return &minionCalc{summon: s, ms: ms, m: m, mdb: mdb, gear: gear, weapon: weapon, weapon2: weapon2,
		buffs: mb, enemyMods: minionEnemyMods, res: r}
}

// minionApplyBuffs is "Apply buff/debuff modifiers" for env.minion
// (CalcPerform.lua:3383-3410): the MinionModifier mods of the player's
// buffs, the minion buffs, then the minion buff of each slotted curse.
func (mc *minionCalc) applyBuffs(db *ModDB, buffs map[string][]Mod, slots []*enemyCurse, res *Result) {
	mdb, s := mc.mdb, mc.summon
	for _, l := range buffMinionModifiers(buffs, s.cfg) {
		addMinionModifiers(mdb, l, mc.m.typ)
	}
	for _, l := range mc.buffs {
		for _, x := range l {
			mdb.Add(x)
		}
	}
	for _, cu := range slots {
		for _, x := range cu.minionBuffMods {
			mdb.Add(x)
		}
	}
	// mergeFlasks' flaskBuffsNonPlayer (CalcPerform.lua) is not ported: flasks
	// that apply to the minion (FlasksApplyToMinion, QuickSilverAppliesToAllies,
	// a link's ParentNonUniqueFlasksAppliedToYou) do not reach it.
	fl := db.minionFlasks
	if (fl.any && db.FlagCfg(s.cfg, "FlasksApplyToMinion")) ||
		(fl.nonUnique && mdb.Flag("ParentNonUniqueFlasksAppliedToYou")) ||
		(fl.quicksilver && db.FlagCfg(s.cfg, "QuickSilverAppliesToAllies")) {
		res.Incomplete = append(res.Incomplete, "flask effects on the minion are not applied")
	}
}

// misc is doActorCharges and doActorMisc for env.minion (CalcPerform.lua
// 3526-3529): its charges, misc buffs, rage (skillCfg there is an
// undefined global, so nil) and second "Process enemy modifiers" pass.
func (mc *minionCalc) misc(db *ModDB) {
	mdb := mc.mdb
	charges(mdb)
	silver, silverInc := silverOnslaught(mdb, db.silverFlasks)
	actorMisc(mdb, mc.ms, mc.summon, silver, silverInc)
	rage(mdb, nil)
	mc.enemyMods.apply(mdb)
}

// defence is calcs.defence(env, env.minion) (CalcPerform.lua:3836-3840),
// after the player's offence.
func (c *Calculator) minionDefence(db *ModDB, mc *minionCalc) {
	mdb := mc.mdb
	// CalcDefence.lua: the player's ResistanceAddedToMinions (Bonemeld)
	// gives the minion that share of the player's final resistances.
	if db.Sum("BASE", "ResistanceAddedToMinions") > 0 {
		for _, r := range db.TabulateCfg(nil, "BASE", "ResistanceAddedToMinions") {
			v, _ := num(r.Value)
			for _, t := range resistTypes {
				share := math.Floor(db.output[t+"Resist"] * v / 100) //nolint:mnd // percent
				mdb.NewMod(t+"Resist", "BASE", share, r.Source)
			}
		}
	}
	c.resistances(mdb)
	c.defence(mdb, mc.gear, map[string]bool{})
	for _, k := range minionResultStats {
		mc.res.Stats[k] = mdb.output[k]
	}
}

// minionCalc is env.minion through the steps of calcs.perform.
type minionCalc struct {
	summon, ms *activeSkill
	m          *skillMinion
	mdb        *ModDB
	gear       map[string][]modparser.Mod
	weapon     hitSource
	weapon2    *hitSource
	buffs      map[string][]Mod // minionBuffs
	enemyMods  enemyModifiers
	res        *MinionResult
}

// minionOffence is calcs.triggers(env, env.minion) and calcs.offence(env,
// env.minion, env.minion.mainSkill) (CalcPerform.lua:3844-3845), with the
// Law of the Wilds conversion of the player's calcs.offence
// (CalcOffence.lua:713-730) before them.
func (c *Calculator) minionOffence(db *ModDB, mc *minionCalc, res *Result) {
	if mc == nil {
		return
	}
	c.minionDefence(db, mc)
	mdb, ms := mc.mdb, mc.ms
	clawCritToMinion(mc.summon, mdb)
	if ms == nil {
		return
	}
	if debugMinionSkill != nil {
		debugMinionSkill(ms)
	}
	if debugEnemy != nil && debugMinionOffence {
		debugEnemy(mdb)
	}
	c.minionTriggers(db, mdb, mc.summon, ms, res)
	out := c.offence(mdb, ms, mc.weapon, mc.weapon2)
	for k, v := range out {
		if contains(minionOffenceStats, k) || debugMinionOffence {
			mc.res.Stats[k] = v
		}
	}
	mc.res.out, mc.res.mainSkill = out, ms.active.ge.Name
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
		r, calcErr := c.Calc(withMainSkill(res.build, res.buildGroup(s.Group), s.GroupSkill))
		if calcErr != nil {
			return nil, calcErr
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
// skill's supports, with mod lists on the minion's ModDB. The main skill
// is minionSkillIndex.
//
//nolint:gocognit // the Lua function's loops
func (c *Calculator) createMinionSkills(
	mdb *ModDB, s *activeSkill, m *skillMinion, weapons [2]string, all1H [2]bool, res *Result,
) []*activeSkill {
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
		ms := c.createActiveSkill(eff, s.supportList, nil, weapons, all1H, s)
		ms.cfg = &Cfg{}
		c.buildSkillMods(mdb, ms, res)
		ms.flags["minion"], ms.flags["minionSkill"], ms.flags["haveMinion"] = true, true, true
		ms.flags["spectre"] = s.flags["spectre"]
		eff2, _ := num(s.data.val("minionDamageEffectiveness"))
		ms.data.set("damageEffectiveness", 1+eff2/100)
		list = append(list, ms)
	}
	return list
}

// minionSkillIndex is the end of calcs.createMinionSkills: the minion's
// main skill is the gem's skillMinionSkill (1-based), clamped to the
// skill list; returned 0-based.
func minionSkillIndex(s *activeSkill, n int) int {
	i := 1
	if s.gem != nil && s.gem.SkillMinionSkill != 0 {
		i = s.gem.SkillMinionSkill
	}
	return max(min(i, n), 1) - 1
}

// minionSkillBuffsOf is the env.minion side of calcs.perform's loop over
// the minions' skills of the player's skill s (CalcPerform.lua:2811-3029):
// their enabled Buff and Aura buffs that reach the main minion, into out,
// and the AffectedBy condition the main minion's own debuffs set on it
// (3012-3014), in the loop's order. Other minions' skills read their own
// minion's mods (castingMinionSkills).
//
//nolint:gocognit // one case per buff type, as the Lua branches
func (c *Calculator) minionSkillBuffsOf(
	db, mdb *ModDB, s, main *activeSkill, mainSkills []*activeSkill, charLevel int, out map[string][]Mod, res *Result,
) {
	{
		if s.disabled {
			return
		}
		casting := mdb
		list := mainSkills
		if s != main {
			m := c.minionOf(s, charLevel, db.enemyEnv.level)
			if m == nil {
				return
			}
			casting, list = c.castingMinionSkills(db, s, m, res)
		}
		for _, ms := range list {
			enabled := truthy(ms.data.val("enable"))
			skillList, cfg := ms.mods, ms.cfg
			for _, b := range ms.buffs {
				name := strings.ReplaceAll(b.name, " ", "")
				if b.typ == "Debuff" || b.typ == "AuraDebuff" { //nolint:goconst // PoB buff types
					if casting == mdb && modeEffective(db) && debuffStack(ms, b) > 0 {
						mdb.conditions["AffectedBy"+name] = true // CalcPerform.lua:2991, 3012-3014
					}
					continue
				}
				// CalcPerform.lua:2831, 2873: Buff and Aura are mode_buffs.
				if !enabled || !modeBuffs(db) {
					continue
				}
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
					if mdb.conditions["AffectedBy"+name] ||
						(casting == mdb && truthy(s.data.val("auraCannotAffectSelf"))) {
						continue
					}
					inc := skillList.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect") + mdb.SumCfg(cfg, "INC", "BuffEffectOnSelf", "AuraEffectOnSelf")
					more := skillList.MoreCfg(cfg, "AuraEffect", "BuffEffect") * mdb.MoreCfg(cfg, "BuffEffectOnSelf", "AuraEffectOnSelf")
					mult := (1 + inc/100) * more
					if db.party.auraBeats("Aura", b.name, mult) {
						continue
					}
					mdb.conditions["AffectedBy"+name] = true
					mdb.conditions["AffectedByAura"] = true
					src := append(scaleMods(b.mods, mult), scaleMods(extraAuraMods(casting, cfg), mult)...)
					setSpectreSource(src, s, casting, b.name) // CalcPerform.lua:2919
					mergeBuff(src, out, b.name)
				}
			}
		}
	}
}

// castingMinionSkills is a minion's skills as calcs.perform's minion
// skill loop sees them (activeSkill.minion.activeSkillList), cached per
// summoning skill for the calculation. Their mod lists read the minion's
// own ModDB: its base and monster data mods (initMinionModDB) and the
// summoning skill's MinionModifier mods.
//
// For the main skill's minion (env.minion) that is PoB: initMinionModDB
// and addMinionModifiers (CalcPerform.lua:1322, 1991) fill the ModDB its
// skills were created on (CalcPerform.lua:1298-1302). PoB never fills the
// other minions' ModDB, so their own CurseEffect, AuraEffect and
// DebuffEffect mods do nothing there; in the game they apply, so the
// engine builds the other minions the same way (user decision; pinned by
// TestOtherMinionCurseEffect). Their skills therefore also see their own
// items, attributes and MinionModifier mods, which can enable skills PoB
// disables (a Guardian's Smite beside a shield) and change their buffs
// on the player. The game also applies the player's
// "minions have ..." mods and the summoning gem's supports to every
// minion, which is what the skill's MinionModifier list holds.
//
// The MinionModifier mods are those of the player before the buffs
// (CalcPerform.lua:1991): s.minionModifiers for the main skill, read then,
// and for the others the list at the first call, which is the buff loop
// before the buffs are added.
func (c *Calculator) castingMinionSkills(
	db *ModDB, s *activeSkill, m *skillMinion, res *Result,
) (*ModDB, []*activeSkill) {
	if db.minionSkillCache == nil {
		db.minionSkillCache = map[*activeSkill][]*activeSkill{}
	}
	if l, ok := db.minionSkillCache[s]; ok && len(l) > 0 {
		return l[0].mods.root(), l
	}
	// A throwaway Result: the minion's items are parsed again for the
	// main minion, and their notes belong to that pass.
	casting, _ := c.initMinionDB(db, s, m, &Result{})
	if casting == nil {
		casting = NewModDB()
		casting.enemy, casting.owner, casting.minionData = db.enemy, db, m.data
		copyModeConditions(casting, db)
	}
	casting.build, casting.enemyEnv = db.build, db.enemyEnv
	mods := s.minionModifiers
	if !s.main {
		mods = s.mods.List(s.cfg, "MinionModifier")
	}
	addMinionModifiers(casting, mods, m.typ)
	l := c.createMinionSkills(casting, s, m, [2]string{m.data.WeaponType1}, [2]bool{}, res)
	db.minionSkillCache[s] = l
	return casting, l
}
