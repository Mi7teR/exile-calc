package engine

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// separateBuffs is the "Separate global effect modifiers" loop of
// buildActiveSkillModList: mods with a GlobalEffect tag leave the skill's
// list and become its buffs.
func (c *Calculator) separateBuffs(s *activeSkill) {
	list := s.mods
	names := make([]string, 0, len(list.mods))
	for n := range list.mods {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		var keep []Mod
		for _, m := range list.mods[name] {
			var tag map[string]any
			for _, t := range m.Tags {
				if tagStr(t, "type") == "GlobalEffect" {
					tag = t
					break
				}
			}
			if tag == nil {
				keep = append(keep, m)
				continue
			}
			if mc := tagStr(tag, "modCond"); mc != "" && !list.getCondition(mc, s.cfg, false) {
				continue
			}
			typ := tagStr(tag, "effectType")
			bname := tagStr(tag, "effectName")
			if bname == "" {
				bname = s.active.ge.Name
			}
			var buff *skillBuff
			for _, b := range s.buffs {
				if b.typ == typ && b.name == bname {
					buff = b
					break
				}
			}
			if buff == nil {
				buff = &skillBuff{typ: typ, name: bname, cond: tagStr(tag, "effectCond"),
					enemyCond: tagStr(tag, "effectEnemyCond"), applyNotPlayer: tagBool(tag, "applyNotPlayer"),
					applyMinions: tagBool(tag, "applyMinions"), allowTotemBuff: tagBool(tag, "allowTotemBuff"),
					stackVar: tagStr(tag, "effectStackVar"), stackLimitVar: tagStr(tag, "effectStackLimitVar")}
				buff.stackLimit, buff.hasStackLimit = tagNum(tag, "effectStackLimit")
				if m.Source == "Skill:"+s.active.id {
					buff.activeSkillBuff = true
					buff.applyNotPlayer = buff.applyNotPlayer || truthy(s.data.val("buffNotPlayer"))
					buff.applyMinions = buff.applyMinions || truthy(s.data.val("buffMinions"))
					buff.applyAllies = truthy(s.data.val("buffAllies"))
					buff.allowTotemBuff = truthy(s.data.val("allowTotemBuff"))
				}
				s.buffs = append(s.buffs, buff)
			}
			merged := false
			for i, d := range buff.mods {
				if sameParams(m, d) && (d.Type == "BASE" || d.Type == "INC") {
					a, _ := num(d.Value)
					b, _ := num(m.Value)
					d.Value = a + b
					buff.mods[i] = d
					merged = true
					break
				}
			}
			if !merged {
				buff.mods = append(buff.mods, m)
			}
		}
		if len(keep) == 0 {
			delete(list.mods, name)
		} else {
			list.mods[name] = keep
		}
	}
}

// mergeBuff is CalcPerform.lua mergeBuff: per comparable mod the highest value.
func mergeBuff(src []Mod, dest map[string][]Mod, key string) {
	d := dest[key]
	for _, m := range src {
		match := false
		if m.Type != "LIST" {
			for i, dm := range d {
				if sameParams(m, dm) {
					a, aok := num(dm.Value)
					b, bok := num(m.Value)
					if aok && bok && b > a {
						d[i] = m
					}
					match = true
					break
				}
			}
		}
		if !match {
			d = append(d, m)
		}
	}
	dest[key] = d
}

// applyBuffs ports the player's Buff and Aura branches of calcs.perform
// and adds the merged buffs to the ModDB.
func (c *Calculator) applyBuffs(db *ModDB, skills []*activeSkill, res *Result) ([]*enemyCurse, map[string][]Mod) {
	scorchingRayStages(skills)
	var curses []*enemyCurse
	buffs := map[string][]Mod{}
	warcries := map[string]bool{}
	guards := map[string][]Mod{}
	var guardOrder []string
	notBuff := map[string]bool{}
	var order []string
	debuffs := map[string][]Mod{}
	var debuffOrder []string
	appliedCombustion := false
	add := func(src []Mod, name string) {
		if _, ok := buffs[name]; !ok {
			order = append(order, name)
		}
		mergeBuff(src, buffs, name)
	}
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
			switch b.typ {
			case "GlobalDB":
				for _, m := range b.mods {
					db.Add(m)
				}
			case "Buff":
				if s.flags["totem"] && !b.allowTotemBuff || b.applyNotPlayer {
					continue
				}
				store, bcfg := db, (*Cfg)(nil)
				if b.activeSkillBuff {
					store, bcfg = list, cfg
				}
				db.conditions["AffectedBy"+strings.ReplaceAll(b.name, " ", "")] = true
				inc := store.SumCfg(bcfg, "INC", "BuffEffect", "BuffEffectOnSelf", "BuffEffectOnPlayer") +
					list.SumCfg(cfg, "INC", strings.ReplaceAll(b.name, " ", "")+"Effect")
				more := store.MoreCfg(bcfg, "BuffEffect", "BuffEffectOnSelf")
				add(scaleMods(b.mods, (1+inc/100)*more), b.name)
				if truthy(s.data.val("thisIsNotABuff")) {
					notBuff[b.name] = true
				}
			case "Aura":
				extra := extraAuraMods(db, cfg)
				mult := (1 + list.SumCfg(cfg, "INC", auraEffectNames...)/100) * list.MoreCfg(cfg, auraEffectNames...)
				// A stronger party aura of that name applies instead.
				if !truthy(s.data.val("auraCannotAffectSelf")) &&
					(db.Flag("AlliesAurasCannotAffectSelf") || !db.party.auraBeats("Aura", b.name, mult)) {
					db.conditions["AffectedByAura"] = true
					if strings.HasPrefix(b.name, "Vaal") && len(b.name) > 5 {
						db.conditions["AffectedBy"+strings.ReplaceAll(b.name[5:], " ", "")] = true
					}
					db.conditions["AffectedBy"+strings.ReplaceAll(b.name, " ", "")] = true
					add(append(scaleMods(b.mods, mult), scaleMods(extra, mult)...), b.name)
				}
				if db.Flag("AurasAffectEnemies") {
					res.Incomplete = append(res.Incomplete, "auras affecting enemies: "+b.name)
				}
			case "Guard":
				if s.flags["totem"] && !b.allowTotemBuff || b.applyNotPlayer {
					continue
				}
				store, bcfg := db, (*Cfg)(nil)
				if b.activeSkillBuff {
					store, bcfg = list, cfg
				}
				inc := store.SumCfg(bcfg, "INC", "BuffEffect", "BuffEffectOnSelf", "BuffEffectOnPlayer")
				more := store.MoreCfg(bcfg, "BuffEffect", "BuffEffectOnSelf")
				if _, ok := guards[b.name]; !ok {
					guardOrder = append(guardOrder, b.name)
				}
				mergeBuff(scaleMods(b.mods, (1+inc/100)*more), guards, b.name)
			case "Warcry":
				c.warcry(db, s, b, mainCfg(skills), warcries, add)
			case "Debuff", "AuraDebuff":
				stack := 1.0
				if b.stackVar != "" {
					stack = list.SumCfg(cfg, "BASE", "Multiplier:"+b.stackVar)
					if b.hasStackLimit {
						stack = math.Min(stack, b.stackLimit)
					} else if b.stackLimitVar != "" {
						stack = math.Min(stack, list.SumCfg(cfg, "BASE", "Multiplier:"+b.stackLimitVar))
					}
				} else if v, ok := num(s.data.val("stackCount")); ok {
					stack = v
				}
				if stack <= 0 {
					continue
				}
				name := strings.ReplaceAll(b.name, " ", "")
				db.enemy.conditions["AffectedBy"+name] = true
				db.conditions["AffectedBy"+name] = true
				var mult float64
				var extra []Mod
				if b.typ == "AuraDebuff" {
					extra = extraAuraDebuffMods(db, cfg)
					mult = 0
					if !db.Flag("SelfAurasOnlyAffectYou") {
						mult = (1 + list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect", "DebuffEffect")/100) * list.MoreCfg(cfg, "AuraEffect", "BuffEffect", "DebuffEffect")
						if db.party.auraBeats("AuraDebuff", b.name, mult) {
							mult = 0
						}
					}
				} else {
					mult = (1 + list.SumCfg(cfg, "INC", "DebuffEffect")/100) * list.MoreCfg(cfg, "DebuffEffect")
				}
				src := append(scaleMods(b.mods, mult*stack), scaleMods(extra, mult*stack)...)
				if s.data.val("stackCount") != nil || b.stackVar != "" {
					src = append(src, Mod{Name: "Multiplier:" + b.name + "Stack", Type: "BASE", Value: stack, Source: b.name})
				}
				if _, ok := debuffs[b.name]; !ok {
					debuffOrder = append(debuffOrder, b.name)
				}
				mergeBuff(src, debuffs, b.name)
			case "Curse", "CurseBuff":
				if cu := c.skillCurse(db, s, b); cu != nil {
					curses = append(curses, cu)
				}
			case "Link":
				// The player is never linked to itself: a link reaches the
				// main minion (minionBuffs) or the party (buffExports only).
			}
		}
		// Wither: the skill or its (main) minion can wither.
		if list.Flag("Condition:CanWither") || (s.main && minionCanWither(c, s)) {
			name := "WitherEffect"
			if s.main && minionCanWither(c, s) && !list.Flag("Condition:CanWither") {
				name = "MinionWitherEffect"
			}
			inc := db.Sum("INC", name)
			if name == "WitherEffect" && db.Flag("UnholyMight") {
				inc += db.Sum("INC", "UnholyMightAlsoWitherEffect")
			}
			db.NewMod("WitherEffectStack", "MAX", math.Floor(6*(1+inc/100)), s.active.ge.Name)
		}
		// Combustion: an ignited enemy gets the support's fire resistance.
		if !appliedCombustion && db.enemy.Flag("Condition:Ignited") && (s.types[stDamage] || s.types[stAttack]) {
			for _, sup := range s.supportList {
				if sup.ge.Name != "Combustion" {
					continue
				}
				if !list.FlagCfg(cfg, "CannotIgnite") {
					db.enemy.NewMod("FireResist", "BASE", list.SumCfg(cfg, "BASE", "CombustionFireResist"), "Combustion",
						map[string]any{"type": "GlobalEffect", "effectType": "Debuff", "effectName": "Combustion"},
						map[string]any{"type": "Condition", "var": "Ignited"})
					appliedCombustion = true
				}
				break
			}
		}
		c.minionAurasOnPlayer(db, s, add, res)
	}
	db.party.partyBuffsOnPlayer(db, add, func(src []Mod, name string) {
		if _, ok := debuffs[name]; !ok {
			debuffOrder = append(debuffOrder, name)
		}
		mergeBuff(src, debuffs, name)
	})
	// Guard buffs: a Vaal Molten Shell alone, else every Vaal guard and the
	// first other guard (in PoB's pairs order; here the order of skills).
	var slots []string
	nonVaal := false
	for _, name := range guardOrder {
		if name == "Vaal Molten Shell" {
			slots, nonVaal = []string{name}, false
			break
		}
		switch {
		case strings.HasPrefix(name, "Vaal"):
			slots = append(slots, name)
		case !nonVaal:
			slots, nonVaal = append(slots, name), true
		}
	}
	if nonVaal {
		db.conditions["AffectedByNonVaalGuardSkill"] = true
	}
	for _, name := range slots {
		db.conditions["AffectedByGuardSkill"] = true
		db.conditions["AffectedBy"+strings.ReplaceAll(name, " ", "")] = true
		add(guards[name], name)
	}
	for _, name := range order {
		for _, m := range buffs[name] {
			db.Add(m)
		}
		if !notBuff[name] {
			db.multipliers["BuffOnSelf"]++
		}
	}
	for _, name := range debuffOrder {
		for _, m := range debuffs[name] {
			db.enemy.Add(m)
		}
	}
	return curses, buffs
}

// minionCanWither is env.minion.modDB:Flag(nil, "Condition:CanWither") at
// the time of the buff loop: the minion's data mods and the minion
// modifiers of its summoning skill.
func minionCanWither(c *Calculator, s *activeSkill) bool {
	list, _ := minionList(s)
	if len(list) == 0 {
		return false
	}
	for _, e := range s.mods.List(s.cfg, "MinionModifier") {
		v, _ := e.Value.(map[string]any)
		if mm, ok := v["mod"].(map[string]any); ok && mm["name"] == "Condition:CanWither" {
			return true
		}
	}
	if m := c.data.Minions[list[0]]; m != nil {
		for _, mm := range m.ModList {
			if mm["name"] == "Condition:CanWither" {
				return true
			}
		}
	}
	return false
}

// extraAuraDebuffMods is the extraAuraModList of the AuraDebuff branch of
// calcs.perform: ExtraAuraDebuffEffect mods, merged by parameters.
func extraAuraDebuffMods(db *ModDB, cfg *Cfg) []Mod {
	var extra []Mod
	for _, e := range db.List(cfg, "ExtraAuraDebuffEffect") {
		v, _ := e.Value.(map[string]any)
		mm, ok := v["mod"].(map[string]any)
		if !ok {
			continue
		}
		m := modFromMap(mm, e.Mod.Source)
		merged := false
		for i, x := range extra {
			if sameParams(x, m) {
				a, _ := num(x.Value)
				bv, _ := num(m.Value)
				x.Value = a + bv
				extra[i] = x
				merged = true
				break
			}
		}
		if !merged {
			extra = append(extra, m)
		}
	}
	return extra
}

// extraAuraMods is the extraAuraModList of the Aura branch of
// calcs.perform: ExtraAuraEffect mods, merged by parameters.
func extraAuraMods(db *ModDB, cfg *Cfg) []Mod {
	return extraEffectMods(db, cfg, "ExtraAuraEffect")
}

// extraEffectMods is the extraAuraModList/extraLinkModList loop of
// calcs.perform: the mods of the actor's list entries of that name, with
// comparable mods summed.
func extraEffectMods(db *ModDB, cfg *Cfg, listName string) []Mod {
	var extra []Mod
	for _, e := range db.List(cfg, listName) {
		v, _ := e.Value.(map[string]any)
		mm, ok := v["mod"].(map[string]any)
		if !ok {
			continue
		}
		m := modFromMap(mm, e.Mod.Source)
		merged := false
		for i, x := range extra {
			if sameParams(x, m) {
				a, _ := num(x.Value)
				bv, _ := num(m.Value)
				x.Value = a + bv
				extra[i] = x
				merged = true
				break
			}
		}
		if !merged {
			extra = append(extra, m)
		}
	}
	return extra
}

var auraEffectNames = []string{"AuraEffect", "BuffEffect", "BuffEffectOnSelf", "AuraEffectOnSelf", "AuraBuffEffect", "SkillAuraEffectOnSelf"}

// warcryBonus is the warcry power bonus of each buff mod (mods tagged with
// a div) and the buff uptime from duration and cooldown, as the Warcry
// branch of calcs.perform computes them.
func warcryBonus(db *ModDB, s *activeSkill, b *skillBuff) ([]float64, float64) {
	list, cfg := s.mods, s.cfg
	power, ok := db.Override("WarcryPower")
	if !ok {
		power = math.Max(db.Sum("BASE", "WarcryPower")*(1+db.Sum("INC", "WarcryPower")/100), db.Sum("BASE", "MinimumWarcryPower"))
	}
	bonus := make([]float64, len(b.mods))
	for i, m := range b.mods {
		bonus[i] = 1
		if len(m.Tags) == 0 || tagStr(m.Tags[0], "effectType") != "Warcry" {
			continue
		}
		if div, hasDiv := tagNum(m.Tags[0], "div"); hasDiv && div != 0 {
			p := power
			if limit, hasLimit := tagNum(m.Tags[0], "limit"); hasLimit {
				p = math.Min(p, limit)
			}
			bonus[i] = math.Floor(p / div)
		}
	}
	duration := skillDuration(list, cfg, s.data, db.enemy)
	cooldown, ok := list.OverrideCfg(cfg, "CooldownRecovery")
	if !ok {
		cd, _ := s.data.cooldown.get()
		cooldown = (cd + list.SumCfg(cfg, "BASE", "CooldownRecovery")) / calcMod(list, cfg, "CooldownRecovery")
	}
	uptime := 1.0
	if !db.Flag("Condition:WarcryMaxHit") {
		uptime = math.Min(duration/cooldown, 1)
	}
	return bonus, uptime
}

// warcry ports the Warcry branch of calcs.perform: exerted attacks, warcry
// power bonuses on mods tagged with a div, uptime from duration and
// cooldown, and the buff on the player scaled by buff effect.
func (c *Calculator) warcry(db *ModDB, s *activeSkill, b *skillBuff, mainCfg *Cfg, seen map[string]bool, add func([]Mod, string)) {
	list, cfg := s.mods, s.cfg
	name := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(b.name, " Cry", ""), "'s", ""), " ", "")
	if base := list.SumCfg(mainCfg, "BASE", name+"ExertedAttacks"); base > 0 {
		extra := list.Sum("BASE", "ExtraExertedAttacks")
		db.NewMod("Num"+name+"Exerts", "BASE", math.Floor((base+extra)*list.More("ExtraExertedAttacks")), "")
		if !seen[b.name] {
			db.NewMod("Multiplier:ExertingWarcryCount", "BASE", 1, b.name)
			seen[b.name] = true
		}
	}
	if list.Flag("CannotShareWarcryBuffs") {
		return
	}
	bonus, uptime := warcryBonus(db, s, b)
	if db.Flag("CannotGainWarcryBuffs") || b.applyNotPlayer {
		return
	}
	db.conditions["AffectedBy"+name] = true
	inc := list.SumCfg(cfg, "INC", "BuffEffect", "BuffEffectOnSelf", "BuffEffectOnPlayer")
	more := list.MoreCfg(cfg, "BuffEffect", "BuffEffectOnSelf")
	var out []Mod
	for i, m := range b.mods {
		out = append(out, scaleMod(m, (1+inc/100)*more*bonus[i]*uptime))
	}
	add(out, b.name)
}

const buffExpirationSlowCap = 0.25 // Data.lua data.misc

// skillDuration is calcSkillDuration (CalcOffence.lua).
func skillDuration(list *ModDB, cfg *Cfg, data *skillData, enemy *ModDB) float64 {
	names := []string{"Duration", "PrimaryDuration"}
	if truthy(data.val("mineDurationAppliesToSkill")) {
		names = append(names, "MineDuration")
	}
	mod := math.Max(calcMod(list, cfg, names...), 0)
	base, _ := data.duration.get()
	d := (base + list.SumCfg(cfg, "BASE", "Duration", "PrimaryDuration")) * mod
	if truthy(data.val("debuff")) && enemy != nil {
		d /= math.Max(buffExpirationSlowCap, calcMod(enemy, cfg, "BuffExpireFaster"))
	}
	return d
}

// extraCurses ports the player part of "Check for extra curses"
// (CalcPerform.lua): ExtraCurse mods with applyToPlayer (config options,
// "You are Cursed with ...") put the curse gem's curse mods on the player,
// scaled by curse effect on self.
func (c *Calculator) extraCurses(db *ModDB, curses []*enemyCurse, res *Result) []*enemyCurse {
	if db.Flag("Condition:OnConsecratedGround") {
		effect := 1 + db.Sum("INC", "ConsecratedGroundEffect")/100
		db.NewMod("CurseEffectOnSelf", "INC", -50*effect, "Consecrated Ground")
	}
	for _, e := range db.List(nil, "ExtraCurse") {
		v, _ := e.Value.(map[string]any)
		id, _ := v["skillId"].(string)
		ge := c.data.Skills.Effects[id]
		if ge == nil {
			res.Incomplete = append(res.Incomplete, fmt.Sprintf("extra curse: unknown skill %q", id))
			continue
		}
		lvl, _ := num(v["level"])
		gem := NewModDB()
		c.mergeSkillInstanceMods(gem, &skillEffect{id: id, ge: ge, level: int(lvl)}, nil)
		var curse []Mod
		for _, ms := range gem.mods {
			for _, m := range ms {
				for _, t := range m.Tags {
					if tagStr(t, "type") == "GlobalEffect" && tagStr(t, "effectType") == "Curse" {
						curse = append(curse, m)
						break
					}
				}
			}
		}
		if !truthy(v["applyToPlayer"]) {
			if !db.enemy.Flag("Hexproof") || db.Flag("CursesIgnoreHexproof") {
				scale := (1 + db.enemy.Sum("INC", "CurseEffectOnSelf")/100) * db.enemy.More("CurseEffectOnSelf")
				curses = append(curses, &enemyCurse{name: ge.Name, priority: c.cursePriorityOf(ge.Name, nil), mods: scaleMods(curse, scale)})
			}
			continue
		}
		if db.Sum("BASE", "AvoidCurse") >= 100 {
			continue
		}
		db.conditions["Cursed"] = true
		db.multipliers["CurseOnSelf"]++
		db.conditions["AffectedBy"+strings.ReplaceAll(ge.Name, " ", "")] = true
		cfg := &Cfg{SkillName: ge.Name}
		inc := db.SumCfg(cfg, "INC", "CurseEffectOnSelf") + gem.Sum("INC", "CurseEffectAgainstPlayer")
		more := db.MoreCfg(cfg, "CurseEffectOnSelf") * gem.More("CurseEffectAgainstPlayer")
		for _, m := range scaleMods(curse, math.Max((1+inc/100)*more, 0)) {
			db.Add(m)
		}
	}
	return curses
}

// extraAuras ports the ExtraAura loop of calcs.perform: aura mods granted
// by other mods ("you and nearby allies have ...") apply to the player
// scaled by buff and aura effect on self.
func extraAuras(db *ModDB) {
	scale := (1 + db.Sum("INC", "BuffEffectOnSelf", "AuraEffectOnSelf")/100) * db.More("BuffEffectOnSelf", "AuraEffectOnSelf")
	for _, e := range db.List(nil, "ExtraAura") {
		v, _ := e.Value.(map[string]any)
		mm, ok := v["mod"].(map[string]any)
		if !ok || truthy(v["onlyAllies"]) || (truthy(v["fromAllies"]) && db.Flag("AlliesAurasCannotAffectSelf")) {
			continue
		}
		db.Add(scaleMod(modFromMap(mm, e.Mod.Source), scale))
		if !truthy(v["notBuff"]) {
			db.multipliers["BuffOnSelf"]++
		}
	}
}

func scaleMods(l []Mod, scale float64) []Mod {
	out := make([]Mod, len(l))
	for i, m := range l {
		out[i] = scaleMod(m, scale)
	}
	return out
}

// minionAurasOnPlayer is the player part of calcs.perform's minion skill
// buffs: the auras and ally buffs of a skill's minion's own skills (the
// Elemental Relic's Anger, Hatred and Wrath). The minion's ModDB is a bare
// one (bareMinionSkills): its own aura effect mods are not read.
func (c *Calculator) minionAurasOnPlayer(db *ModDB, s *activeSkill, add func([]Mod, string), res *Result) {
	if s.summon != nil || db.enemyEnv == nil {
		return
	}
	m := c.minionOf(s, int(db.multipliers["Level"]), db.enemyEnv.level)
	if m == nil || m.data.Hostile {
		return
	}
	casting, mskills := c.bareMinionSkills(db, s, m, res)
	for _, ms := range mskills {
		if ms.disabled || !truthy(ms.data.val("enable")) {
			continue
		}
		list, cfg := ms.mods, ms.cfg
		for _, b := range ms.buffs {
			name := strings.ReplaceAll(b.name, " ", "")
			switch b.typ {
			case "Buff":
				if !b.applyAllies {
					continue
				}
				store, bcfg := casting, (*Cfg)(nil)
				if b.activeSkillBuff {
					store, bcfg = list, cfg
				}
				db.conditions["AffectedBy"+name] = true
				inc := store.SumCfg(bcfg, "INC", "BuffEffect", "BuffEffectOnPlayer") + db.Sum("INC", "BuffEffectOnSelf")
				more := store.MoreCfg(bcfg, "BuffEffect", "BuffEffectOnPlayer") * db.More("BuffEffectOnSelf")
				add(scaleMods(b.mods, (1+inc/100)*more), b.name)
			case "Aura":
				if casting.Flag("SelfAurasCannotAffectAllies", "SelfAurasOnlyAffectYou", "SelfAuraSkillsCannotAffectAllies") ||
					list.FlagCfg(cfg, "SelfAurasAffectYouAndLinkedTarget") || db.Flag("AlliesAurasCannotAffectSelf") || db.conditions["AffectedBy"+name] {
					continue
				}
				inc := list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect", "BuffEffectOnPlayer", "AuraBuffEffect") + db.SumCfg(cfg, "INC", "BuffEffectOnSelf", "AuraEffectOnSelf")
				more := list.MoreCfg(cfg, "AuraEffect", "BuffEffect", "AuraBuffEffect") * db.MoreCfg(cfg, "BuffEffectOnSelf", "AuraEffectOnSelf")
				if db.party.auraBeats("Aura", b.name, (1+inc/100)*more) {
					continue
				}
				db.conditions["AffectedByAura"] = true
				if strings.HasPrefix(b.name, "Vaal") && len(b.name) > 5 {
					db.conditions["AffectedBy"+strings.ReplaceAll(b.name[5:], " ", "")] = true
				}
				db.conditions["AffectedBy"+name] = true
				add(scaleMods(b.mods, (1+inc/100)*more), b.name)
			}
		}
	}
}
