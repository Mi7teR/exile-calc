package engine

import (
	"fmt"
	"maps"
	"math"
	"regexp"
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
	addDebuff := func(src []Mod, name string) {
		if _, ok := debuffs[name]; !ok {
			debuffOrder = append(debuffOrder, name)
		}
		mergeBuff(src, debuffs, name)
	}
	pm := mainSkill(skills)
	// env.minion, and linksApplyToMinions (CalcPerform.lua:2743-2744) on its
	// ModDB as the buff loop sees it.
	envMinion, linkMinion := false, false
	if pm != nil && db.enemyEnv != nil {
		if m := c.minionOf(pm, int(db.multipliers["Level"]), db.enemyEnv.level); m != nil {
			envMinion = true
			if !m.data.Hostile {
				casting, _ := c.castingMinionSkills(db, pm, m, res)
				linkMinion = linksApplyToMinion(db, casting, pm)
			}
		}
	}
	linked := false // #linkSkills >= 1
	// CalcPerform.lua:2242: the active spectres' modLists, before the
	// skills' buffs; their mods are inserted, not merged.
	sp := c.spectreEffectsOf(db, skills)
	if sp.beast {
		db.conditions["HaveBeastSpectre"] = true
	}
	if len(sp.player) > 0 {
		order = append(order, "Spectre")
		buffs["Spectre"] = append(buffs["Spectre"], sp.player...)
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
				if s.flags["totem"] && !b.allowTotemBuff {
					continue
				}
				store, bcfg := db, (*Cfg)(nil)
				if b.activeSkillBuff {
					store, bcfg = list, cfg
				}
				effectName := strings.ReplaceAll(b.name, " ", "") + "Effect"
				if !b.applyNotPlayer {
					db.conditions["AffectedBy"+strings.ReplaceAll(b.name, " ", "")] = true
					inc := store.SumCfg(bcfg, "INC", "BuffEffect", "BuffEffectOnSelf", "BuffEffectOnPlayer") +
						list.SumCfg(cfg, "INC", effectName)
					more := store.MoreCfg(bcfg, "BuffEffect", "BuffEffectOnSelf")
					add(scaleMods(b.mods, (1+inc/100)*more), b.name)
					if truthy(s.data.val("thisIsNotABuff")) {
						notBuff[b.name] = true
					}
				}
				// CalcPerform.lua:2377-2382.
				if e := db.partyExports; e != nil &&
					(b.applyAllies || list.Flag("BuffAppliesToAllies") || list.Flag("BuffAppliesToPartyMembers")) {
					inc := store.SumCfg(bcfg, "INC", "BuffEffect") + list.SumCfg(cfg, "INC", effectName)
					e.other[b.name] = &partyExport{effectMult: (1 + inc/100) * store.MoreCfg(bcfg, "BuffEffect"), mods: b.mods}
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
				c.playerAuraOthers(db, s, pm, b, extra, add, addDebuff)
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
				c.warcry(db, s, b, mainCfg(skills), warcries, envMinion, add)
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
						if e := db.partyExports; e != nil {
							// CalcPerform.lua:2658-2669.
							e.setAuraDebuff(b.name, &partyExport{effectMult: mult, mods: append(append([]Mod(nil), b.mods...), extra...)})
						}
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
				// main minion (minionBuffs) or the party (CalcPerform.lua
				// 2745-2773), while no link has reached the minion.
				if linked {
					continue
				}
				if e := db.partyExports; e != nil {
					extra := extraEffectMods(db, cfg, "ExtraLinkEffect")
					mult := (1 + list.SumCfg(cfg, "INC", "LinkEffect", "BuffEffect")/100) * list.MoreCfg(cfg, "LinkEffect", "BuffEffect")
					e.link[b.name] = &partyExport{effectMult: mult, mods: append(append([]Mod(nil), b.mods...), extra...)}
				}
				linked = linkMinion
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
		curses = append(curses, c.minionAurasOnPlayer(db, s, pm, add, addDebuff, notBuff, res)...)
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
func (c *Calculator) warcry(
	db *ModDB, s *activeSkill, b *skillBuff, mainCfg *Cfg, seen map[string]bool, envMinion bool, add func([]Mod, string),
) {
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
	if e := db.partyExports; e != nil {
		// CalcPerform.lua:2468-2475: buff.modList with the warcryPowerBonus
		// written on its tags (2414-2418), and Rallying Cry's weapon damage,
		// which exists only with env.minion (2449-2465).
		mods := make([]Mod, 0, len(b.mods))
		for i, m := range b.mods {
			if len(m.Tags) > 0 && tagStr(m.Tags[0], "effectType") == "Warcry" {
				if _, hasDiv := tagNum(m.Tags[0], "div"); hasDiv {
					m.Tags = append([]map[string]any{maps.Clone(m.Tags[0])}, m.Tags[1:]...)
					m.Tags[0]["warcryPowerBonus"] = bonus[i]
				}
			}
			mods = append(mods, m)
		}
		if envMinion && s.active.ge.Name == "Rallying Cry" {
			mods = append(mods, rallyingCryWeaponMods(db, s, mainCfg)...)
		}
		mult := (1 + list.SumCfg(cfg, "INC", "BuffEffect")/100) * list.MoreCfg(cfg, "BuffEffect") * uptime
		e.warcry[b.name] = &partyExport{effectMult: mult, mods: mods}
	}
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
// (CalcPerform.lua:3177-3224): ExtraCurse mods with applyToPlayer (config
// options, "You are Cursed with ...") put the curse gem's curse mods on
// the player, scaled by curse effect on self; the others are curses on
// the enemy.
func (c *Calculator) extraCurses(db *ModDB, curses []*enemyCurse, res *Result) []*enemyCurse {
	return append(curses, c.actorExtraCurses(db, extraCursePass{self: true, enemy: true}, res)...)
}

// extraCursePass is which part of an actor's "Check for extra curses"
// pass runs: self, the Consecrated Ground mod and the applyToPlayer curses
// on the actor; enemy, the curses on the enemy (in minionCurses when
// minion).
type extraCursePass struct {
	self, enemy, minion bool
}

// actorExtraCurses is one iteration of "Check for extra curses"
// (CalcPerform.lua:3178-3224) for actor (env.player's modDB, or
// env.minion's into minionCurses).
func (c *Calculator) actorExtraCurses(actor *ModDB, pass extraCursePass, res *Result) []*enemyCurse {
	if pass.self && actor.Flag("Condition:OnConsecratedGround") {
		effect := 1 + actor.Sum("INC", "ConsecratedGroundEffect")/100
		actor.NewMod("CurseEffectOnSelf", "INC", -50*effect, "Consecrated Ground")
	}
	var curses []*enemyCurse
	for _, e := range actor.List(nil, "ExtraCurse") {
		v, _ := e.Value.(map[string]any)
		toSelf := truthy(v["applyToPlayer"])
		if toSelf && !pass.self || !toSelf && !pass.enemy {
			continue
		}
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
		if !toSelf {
			if !actor.enemy.Flag("Hexproof") || actor.Flag("CursesIgnoreHexproof") {
				scale := (1 + actor.enemy.Sum("INC", "CurseEffectOnSelf")/100) * actor.enemy.More("CurseEffectOnSelf")
				curses = append(curses, &enemyCurse{name: ge.Name, priority: c.cursePriorityOf(ge.Name, nil),
					minion: pass.minion, mods: scaleMods(curse, scale)})
			}
			continue
		}
		if actor.Sum("BASE", "AvoidCurse") >= 100 {
			continue
		}
		actor.conditions["Cursed"] = true
		actor.multipliers["CurseOnSelf"]++
		actor.conditions["AffectedBy"+strings.ReplaceAll(ge.Name, " ", "")] = true
		cfg := &Cfg{SkillName: ge.Name}
		inc := actor.SumCfg(cfg, "INC", "CurseEffectOnSelf") + gem.Sum("INC", "CurseEffectAgainstPlayer")
		more := actor.MoreCfg(cfg, "CurseEffectOnSelf") * gem.More("CurseEffectAgainstPlayer")
		for _, m := range scaleMods(curse, math.Max((1+inc/100)*more, 0)) {
			actor.Add(m)
		}
	}
	return curses
}

// extraAuras ports the ExtraAura loop of calcs.perform: aura mods granted
// by other mods ("you and nearby allies have ...") apply to the player
// scaled by buff and aura effect on self, and, while the main skill is a
// totem, to its totems: an unscaled copy named Condition:Totem<X> for a
// condition, Totem<name> otherwise (Speed, CritMultiplier and CritChance
// excepted), e.g. Condition:TotemTailwind.
func extraAuras(db *ModDB, main *activeSkill) {
	scale := (1 + db.Sum("INC", "BuffEffectOnSelf", "AuraEffectOnSelf")/100) * db.More("BuffEffectOnSelf", "AuraEffectOnSelf")
	totem := main != nil && main.flags["totem"]
	for _, e := range db.List(nil, "ExtraAura") {
		v, _ := e.Value.(map[string]any)
		mm, ok := v["mod"].(map[string]any)
		if !ok {
			continue
		}
		if !truthy(v["onlyAllies"]) && (!truthy(v["fromAllies"]) || !db.Flag("AlliesAurasCannotAffectSelf")) {
			db.Add(scaleMod(modFromMap(mm, e.Mod.Source), scale))
			if !truthy(v["notBuff"]) {
				db.multipliers["BuffOnSelf"]++
			}
		}
		if x := db.partyExports; x != nil && (truthy(v["fromAllies"]) || !db.Flag("SelfAurasCannotAffectAllies")) {
			x.extraAura = append(x.extraAura, modFromMap(mm, e.Mod.Source)) // CalcPerform.lua:3469
		}
		if (truthy(v["fromAllies"]) || !db.Flag("SelfAurasCannotAffectAllies")) && totem {
			m := modFromMap(mm, e.Mod.Source)
			if m.Name == "Speed" || m.Name == "CritMultiplier" || m.Name == "CritChance" {
				continue
			}
			if strings.Contains(m.Name, "Condition:") {
				m.Name = strings.ReplaceAll(m.Name, "Condition:", "Condition:Totem")
			} else {
				m.Name = "Totem" + m.Name
			}
			db.Add(m)
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

// minionAurasOnPlayer is the player and enemy part of calcs.perform's
// minion skill buffs (CalcPerform.lua:2811): the auras and ally buffs of a
// skill's minion's own skills (the Elemental Relic's Anger, Hatred and
// Wrath), their debuffs on the enemy, and their curses, returned as
// minionCurses. The minion skills read the minion's own mods
// (castingMinionSkills).
//
// pm is env.player.mainSkill: while it is a totem, the minion's auras also
// reach its totems (CalcPerform.lua:2934-2964).
func (c *Calculator) minionAurasOnPlayer(
	db *ModDB, s, pm *activeSkill, add, debuff func([]Mod, string), notBuff map[string]bool, res *Result,
) []*enemyCurse {
	if s.summon != nil || db.enemyEnv == nil {
		return nil
	}
	m := c.minionOf(s, int(db.multipliers["Level"]), db.enemyEnv.level)
	if m == nil || m.data.Hostile {
		return nil
	}
	var curses []*enemyCurse
	casting, mskills := c.castingMinionSkills(db, s, m, res)
	for _, ms := range mskills {
		if ms.disabled {
			continue
		}
		list, cfg := ms.mods, ms.cfg
		enabled := truthy(ms.data.val("enable"))
		for _, b := range ms.buffs {
			name := strings.ReplaceAll(b.name, " ", "")
			if b.typ == "Debuff" || b.typ == "AuraDebuff" {
				// Debuffs need no "enable" (CalcPerform.lua:2979).
				// env.minion's AffectedBy condition (CalcPerform.lua:3012-3014)
				// is set by minionSkillBuffsOf.
				minionDebuff(db, ms, b, debuff)
				continue
			}
			if !enabled {
				continue
			}
			switch b.typ {
			case "Curse":
				// CalcPerform.lua:2967.
				if db.enemy.Flag("Hexproof") && !ms.types[stMark] {
					continue
				}
				inc := list.SumCfg(cfg, "INC", "CurseEffect") + db.enemy.Sum("INC", "CurseEffectOnSelf")
				more := list.MoreCfg(cfg, "CurseEffect") * db.enemy.More("CurseEffectOnSelf")
				curses = append(curses, &enemyCurse{name: b.name, priority: c.cursePriorityOf(b.name, ms),
					minion: true, mods: scaleMods(b.mods, (1+inc/100)*more)})
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
				// CalcPerform.lua:2840-2845: the scaled mods, then the
				// unscaled ones merged over them.
				add(scaleMods(b.mods, (1+inc/100)*more), b.name)
				add(b.mods, b.name)
				if truthy(ms.data.val("thisIsNotABuff")) {
					notBuff[b.name] = true
				}
				if e := db.partyExports; e != nil {
					// CalcPerform.lua:2846-2851.
					e.other[b.name] = &partyExport{
						effectMult: (1 + store.SumCfg(bcfg, "INC", "BuffEffect")/100) * store.MoreCfg(bcfg, "BuffEffect"),
						mods:       b.mods,
					}
				}
			case "Aura":
				if casting.Flag("SelfAurasCannotAffectAllies", "SelfAurasOnlyAffectYou", "SelfAuraSkillsCannotAffectAllies") ||
					list.FlagCfg(cfg, "SelfAurasAffectYouAndLinkedTarget") {
					continue
				}
				extra := extraAuraMods(casting, cfg)
				if !db.Flag("AlliesAurasCannotAffectSelf") && !db.conditions["AffectedBy"+name] {
					inc := list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect", "BuffEffectOnPlayer", "AuraBuffEffect") + db.SumCfg(cfg, "INC", "BuffEffectOnSelf", "AuraEffectOnSelf")
					more := list.MoreCfg(cfg, "AuraEffect", "BuffEffect", "AuraBuffEffect") * db.MoreCfg(cfg, "BuffEffectOnSelf", "AuraEffectOnSelf")
					mult := (1 + inc/100) * more
					if !db.party.auraBeats("Aura", b.name, mult) {
						db.conditions["AffectedByAura"] = true
						if strings.HasPrefix(b.name, "Vaal") && len(b.name) > 5 {
							db.conditions["AffectedBy"+strings.ReplaceAll(b.name[5:], " ", "")] = true
						}
						db.conditions["AffectedBy"+name] = true
						src := append(scaleMods(b.mods, mult), scaleMods(extra, mult)...)
						setSpectreSource(src, s, casting, b.name)
						add(src, b.name)
					}
				}
				if e := db.partyExports; e != nil {
					// CalcPerform.lua:2923-2933.
					mods := append(append([]Mod(nil), b.mods...), extra...)
					setSpectreSource(mods, s, casting, b.name)
					e.setAura(b.name, &partyExport{
						effectMult: (1 + list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect")/100) * list.MoreCfg(cfg, "AuraEffect", "BuffEffect"),
						mods:       mods,
					})
				}
				// CalcPerform.lua:2934-2964: the player's main skill is a
				// totem, which the aura's defences reach.
				if pm != nil && pm.flags["totem"] && pm.mods != nil && !pm.mods.conditions["AffectedBy"+name] {
					pm.mods.conditions["AffectedBy"+name] = true
					pm.mods.conditions["AffectedByAura"] = true
					scale := math.Max((1+list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect", "AuraBuffEffect")/100)*
						list.MoreCfg(cfg, "AuraEffect", "BuffEffect", "AuraBuffEffect"), 0)
					src := totemAuraMods(scale, extra, b.mods)
					setSpectreSource(src, s, casting, "")
					add(src, "Totem "+b.name)
				}
			}
		}
	}
	return curses
}

// playerAuraOthers is the rest of calcs.perform's Aura branch for the
// player's aura b of skill s (CalcPerform.lua:2512-2618), after its effect
// on the player; env.minion's part is minionBuffs. The aura's Party tab
// export, its debuff on the enemy with AurasAffectEnemies, its totem copy
// while env.player.mainSkill pm is a totem, and the ExtraAuraDebuffEffect
// debuff of an aura without an AuraDebuff.
func (c *Calculator) playerAuraOthers(db *ModDB, s, pm *activeSkill, b *skillBuff, extra []Mod, add, addDebuff func([]Mod, string)) {
	list, cfg := s.mods, s.cfg
	name := strings.ReplaceAll(b.name, " ", "")
	both := func() []Mod { return append(append([]Mod(nil), b.mods...), extra...) }
	if !db.Flag("SelfAurasCannotAffectAllies") && !db.Flag("SelfAurasOnlyAffectYou") && !db.Flag("SelfAuraSkillsCannotAffectAllies") {
		mult := (1 + list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect")/100) * list.MoreCfg(cfg, "AuraEffect", "BuffEffect")
		if e := db.partyExports; e != nil {
			e.setAura(b.name, &partyExport{effectMult: mult, mods: both()}) // CalcPerform.lua:2527-2536
		}
		// CalcPerform.lua:2537-2550.
		if db.Flag("AurasAffectEnemies") && !list.FlagCfg(cfg, "SelfAurasAffectYouAndLinkedTarget") {
			if e := db.partyExports; e != nil {
				e.setAuraDebuff(b.name, &partyExport{effectMult: mult, mods: both()})
			}
			addDebuff(append(scaleMods(b.mods, mult), scaleMods(extra, mult)...), b.name)
		}
	}
	// CalcPerform.lua:2552-2580.
	if pm != nil && pm.flags["totem"] && pm.mods != nil &&
		!db.Flag("SelfAurasCannotAffectAllies") && !db.Flag("SelfAuraSkillsCannotAffectAllies") {
		pm.mods.conditions["AffectedBy"+name] = true
		pm.mods.conditions["AffectedByAura"] = true
		scale := math.Max((1+list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect", "AuraBuffEffect")/100)*
			list.MoreCfg(cfg, "AuraEffect", "BuffEffect", "AuraBuffEffect"), 0)
		add(totemAuraMods(scale, extra, b.mods), "Totem "+b.name)
	}
	// CalcPerform.lua:2582-2618: PoB merges the extra debuff mods without
	// the multiplier it computes.
	if len(db.List(cfg, "ExtraAuraDebuffEffect")) == 0 || db.Flag("SelfAurasOnlyAffectYou") ||
		list.FlagCfg(cfg, "SelfAurasAffectYouAndLinkedTarget") {
		return
	}
	for _, x := range s.buffs {
		if x.typ == "AuraDebuff" {
			return
		}
	}
	mods := extraAuraDebuffMods(db, cfg)
	if e := db.partyExports; e != nil {
		mult := (1 + list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect", "DebuffEffect")/100) *
			list.MoreCfg(cfg, "AuraEffect", "BuffEffect", "DebuffEffect")
		e.setAuraDebuff(b.name, &partyExport{effectMult: mult, mods: mods})
	}
	addDebuff(mods, b.name)
}

// totemResistName is the mod.name:match("Resist?M?a?x?$") of the totem
// aura branches.
var totemResistName = regexp.MustCompile(`Resist?M?a?x?$`)

// totemAuraMods is the totem part of calcs.perform's aura branches
// (CalcPerform.lua:2940-2961 for a minion's aura): the energy shield,
// armour, evasion and resistance mods of the lists, renamed Totem<name>
// and scaled; a whole number stays whole (math.modf of the value rounded
// to two places).
func totemAuraMods(scale float64, lists ...[]Mod) []Mod {
	var out []Mod
	sc := func(v float64) float64 {
		if math.Floor(v) == v {
			i, _ := math.Modf(round(v*scale, 2))
			return i
		}
		return v * scale
	}
	for _, l := range lists {
		for _, m := range l {
			if m.Name != "EnergyShield" && m.Name != "Armour" && m.Name != "Evasion" && !totemResistName.MatchString(m.Name) {
				continue
			}
			m.Name = "Totem" + m.Name
			if scale != 1 {
				if f, isNum := m.Value.(float64); isNum {
					m.Value = sc(f)
				} else if v, isMap := m.Value.(map[string]any); isMap {
					if mm, hasMod := v["mod"].(map[string]any); hasMod {
						if iv, isInner := mm["value"].(float64); isInner {
							inner := maps.Clone(mm)
							inner["value"] = sc(iv)
							nv := maps.Clone(v)
							nv["mod"] = inner
							m.Value = nv
						}
					}
				}
			}
			out = append(out, m)
		}
	}
	return out
}

// setSpectreSource is calcs.perform's setSpectreSource
// (CalcPerform.lua:2814-2826) for the minion of summoning skill s, with
// casting its ModDB: a spectre's mods get the source "Spectre:<buff> -
// <minion>", or "Spectre:<minion>" without a buff name.
func setSpectreSource(mods []Mod, s *activeSkill, casting *ModDB, buffName string) {
	if !s.flags["spectre"] || casting.minionData == nil {
		return
	}
	source := "Spectre:" + casting.minionData.Name
	if buffName != "" {
		source = "Spectre:" + buffName + " - " + casting.minionData.Name
	}
	for i := range mods {
		mods[i].Source = source
	}
}

// debuffStack is the stackCount of calcs.perform's Debuff and AuraDebuff
// branches (CalcPerform.lua:2980-2990): the stack variable within its
// limit, else skillData.stackCount, else 1.
func debuffStack(ms *activeSkill, b *skillBuff) float64 {
	list, cfg := ms.mods, ms.cfg
	stack := 1.0
	if b.stackVar != "" {
		stack = list.SumCfg(cfg, "BASE", "Multiplier:"+b.stackVar)
		if b.hasStackLimit {
			stack = math.Min(stack, b.stackLimit)
		} else if b.stackLimitVar != "" {
			stack = math.Min(stack, list.SumCfg(cfg, "BASE", "Multiplier:"+b.stackLimitVar))
		}
	} else if v, ok := num(ms.data.val("stackCount")); ok {
		stack = v
	}
	return stack
}

// minionDebuff is the Debuff and AuraDebuff branch of calcs.perform's
// minion skill buffs (CalcPerform.lua:2979): the debuff on the enemy,
// scaled by the minion skill's debuff (and aura) effect and its stacks.
// The party's buff exports are not kept. It reports whether the debuff
// applied (a positive stack count).
func minionDebuff(db *ModDB, ms *activeSkill, b *skillBuff, debuff func([]Mod, string)) bool {
	list, cfg := ms.mods, ms.cfg
	stack := debuffStack(ms, b)
	if stack <= 0 {
		return false
	}
	name := strings.ReplaceAll(b.name, " ", "")
	mult := 1.0
	if b.typ == "AuraDebuff" {
		mult = 0
		if !list.Flag("SelfAurasOnlyAffectYou") || list.FlagCfg(cfg, "SelfAurasAffectYouAndLinkedTarget") {
			mult = (1 + list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect", "DebuffEffect")/100) * list.MoreCfg(cfg, "AuraEffect", "BuffEffect", "DebuffEffect")
			switch {
			case db.enemy.conditions["AffectedBy"+name]:
				mult = 0
			default:
				if e := db.partyExports; e != nil {
					e.setAuraDebuff(b.name, &partyExport{effectMult: mult, mods: b.mods}) // CalcPerform.lua:3002
				}
				if db.party.auraBeats("AuraDebuff", b.name, mult) {
					mult = 0
				}
			}
		}
	}
	db.enemy.conditions["AffectedBy"+name] = true
	if b.typ == "Debuff" {
		mult = (1 + list.SumCfg(cfg, "INC", "DebuffEffect")/100) * list.MoreCfg(cfg, "DebuffEffect")
	}
	src := scaleMods(b.mods, mult*stack)
	if ms.data.val("stackCount") != nil || b.stackVar != "" {
		v, _ := num(ms.data.val("stackCount"))
		src = append(src, Mod{Name: "Multiplier:" + b.name + "Stack", Type: "BASE", Value: v, Source: b.name})
	}
	debuff(src, b.name)
	return true
}
