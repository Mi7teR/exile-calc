package engine

import (
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/Mi7teR/exile-calc/gamedata"
	"github.com/Mi7teR/exile-calc/pobcode"
)

// skillStats is calcLib.buildSkillInstanceStats. Effectiveness and linear
// interpolation (statInterpolation 3 and 2) need the actor level.
func skillStats(e *skillEffect, actorLevel float64) map[string]float64 {
	stats := map[string]float64{}
	ge := e.ge
	if e.quality > 0 {
		for _, q := range ge.QualityStats {
			v, _ := math.Modf(q.Value * float64(e.quality))
			stats[q.Stat] += v
		}
	}
	level := ge.Levels[e.level]
	if level == nil {
		level = &gamedata.SkillLevel{}
	}
	if actorLevel == 0 {
		// CalcTools.lua: skillInstance.actorLevel or level.levelRequirement
		// or 1. Lua's 0 is true, so a levelRequirement of 0 (item-granted
		// skills such as Blood Sacrament) is used as is. A level without the
		// field also reads 0 here (PoB: 1); the only such levels (minion
		// levels with cost only) have no stats to interpolate.
		actorLevel = float64(level.LevelRequirement)
	}
	for i, stat := range ge.Stats {
		v := 1.0
		if i < len(level.Values) {
			v = level.Values[i]
		}
		if i < len(level.StatInterpolation) {
			switch level.StatInterpolation[i] {
			case 3:
				// SkillDamageBaseEffectiveness 3.885209, SkillDamageIncrementalEffectiveness 0.360246
				be := ge.BaseEffectiveness
				if be == 0 {
					be = 1
				}
				eff := (3.885209 + float64(0.360246*(actorLevel-1))) * be * math.Pow(1+ge.IncrementalEffectiveness, actorLevel-1)
				v = round(eff*v, 0)
			case 2:
				v = linearStat(ge, e.level, i, actorLevel)
			}
		}
		stats[stat] += v
	}
	for _, cs := range ge.ConstantStats {
		stats[cs.Stat] += cs.Value
	}
	return stats
}

func linearStat(ge *gamedata.Skill, lvl, index int, actorLevel float64) float64 {
	keys := make([]int, 0, len(ge.Levels))
	for k := range ge.Levels {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	cur := 0
	for i, k := range keys {
		if k == lvl {
			cur = i
		}
	}
	at := func(i int) float64 {
		l := ge.Levels[keys[i]]
		if index < len(l.Values) {
			return l.Values[index]
		}
		return 0
	}
	if len(keys) < 2 {
		return round(at(cur), 0)
	}
	next := min(cur+1, len(keys)-1)
	nextReq := float64(ge.Levels[keys[next]].LevelRequirement)
	prevReq := float64(ge.Levels[keys[next-1]].LevelRequirement)
	return round(at(next-1)+(at(next)-at(next-1))*(actorLevel-prevReq)/(nextReq-prevReq), 0)
}

// modFromMap decodes a mod of the generated skill data.
func modFromMap(m map[string]any, source string) Mod {
	mod := Mod{Value: m["value"], Source: source}
	mod.Name, _ = m["name"].(string)
	mod.Type, _ = m["type"].(string)
	if f, ok := num(m["flags"]); ok {
		mod.Flags = uint64(f)
	}
	if f, ok := num(m["keywordFlags"]); ok {
		mod.KeywordFlags = uint64(f)
	}
	if l, ok := m["@list"].([]any); ok {
		for _, t := range l {
			if tm, isMap := t.(map[string]any); isMap {
				mod.Tags = append(mod.Tags, tm)
			}
		}
	}
	return mod
}

// processMod is Data.lua processMod for one skill: source, nested mod
// source and the notMinionStat tag.
func processMod(ge *gamedata.Skill, id string, m map[string]any, stat string) Mod {
	mod := modFromMap(m, "Skill:"+id)
	if v, ok := mod.Value.(map[string]any); ok {
		if sub, isMod := v["mod"].(map[string]any); isMod {
			v = copyMap(v)
			sub = copyMap(sub)
			sub["source"] = "Skill:" + id
			v["mod"] = sub
			mod.Value = v
		}
	}
	if stat != "" && (ge.Support || slices.Contains(ge.SkillTypes, stBuff)) {
		for _, s := range ge.NotMinionStat {
			if s == stat {
				mod.Tags = append(append([]map[string]any(nil), mod.Tags...),
					map[string]any{"type": "ActorCondition", "actor": "parent", "neg": true})
				break
			}
		}
	}
	return mod
}

// statMap returns a stat's map: the skill's own, else data.skillStatMap.
func (c *Calculator) statMap(ge *gamedata.Skill, stat string) *gamedata.StatMap {
	if m := ge.StatMap[stat]; m != nil {
		return m
	}
	return c.data.Skills.StatMap[stat]
}

// levelMod is mergeLevelMod: the mod with its value replaced.
func levelMod(m Mod, value float64) Mod {
	switch v := m.Value.(type) {
	case map[string]any:
		v = copyMap(v)
		if sub, ok := v["mod"].(map[string]any); ok {
			sub = copyMap(sub)
			sub["value"] = value
			v["mod"] = sub
		} else {
			v["value"] = value
		}
		m.Value = v
	default:
		m.Value = value
	}
	return m
}

func scalar(p *float64, def float64) float64 {
	if p == nil {
		return def
	}
	return *p
}

// mergeSkillInstanceMods is calcs.mergeSkillInstanceMods.
func (c *Calculator) mergeSkillInstanceMods(list *ModDB, e *skillEffect, extra []ListEntry) {
	e.level = validateLevel(e.level, e.ge, e.gem)
	stats := skillStats(e, e.actorLevel)
	for _, x := range extra {
		v, _ := x.Value.(map[string]any)
		k, _ := v["key"].(string)
		f, _ := num(v["value"])
		stats[k] += f
	}
	names := make([]string, 0, len(stats))
	for k := range stats {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, stat := range names {
		sm := c.statMap(e.ge, stat)
		if sm == nil {
			continue
		}
		val := stats[stat]
		mapValue := func(g *gamedata.StatMap) float64 {
			if g.Value != nil {
				return *g.Value
			}
			return val*scalar(g.Mult, 1)/scalar(g.Div, 1) + scalar(g.Base, 0)
		}
		for _, it := range sm.Items {
			if it.Mod != nil {
				list.Add(levelMod(processMod(e.ge, e.id, it.Mod, stat), mapValue(sm)))
				continue
			}
			for _, sub := range it.Group.Items {
				if sub.Mod != nil {
					list.Add(levelMod(processMod(e.ge, e.id, sub.Mod, stat), mapValue(it.Group)))
				}
			}
		}
	}
	for _, m := range e.ge.BaseMods {
		list.Add(processMod(e.ge, e.id, m, ""))
	}
}

// buildSkillMods ports calcs.buildActiveSkillModList up to the buff list.
func (c *Calculator) buildSkillMods(db *ModDB, s *activeSkill, res *Result) {
	ge := s.active.ge
	flags, types := s.flags, s.types
	// CalcActiveSkill.lua:254-263: the mode flags.
	flags["buffs"], flags["combat"], flags["effective"] = modeBuffs(db), modeCombat(db), modeEffective(db)
	// Multipart skills: the selected part's flags.
	if parts := ge.Parts; len(parts) > 1 {
		s.part = min(len(parts), max(s.active.srcPart, 1))
		for k, v := range parts[s.part-1].Flags {
			if v {
				flags[k] = true
			} else {
				delete(flags, k)
			}
		}
		flags["multiPart"] = true
	}
	w2, w1 := db.items["Weapon 2"], db.items["Weapon 1"]
	if types[stRequiresShield] && (w2 == nil || w2.Base == nil || w2.Base.Type != "Shield") {
		s.disabled = true
	}
	if types[stRequiresStaff] && (w1 == nil || w1.Base == nil || w1.Base.Type != "Staff") {
		s.disabled = true
	}
	for stat, v := range skillStats(s.active, 0) {
		if sm := c.statMap(ge, stat); v != 0 && sm != nil && sm.SkillFlag != "" {
			flags[sm.SkillFlag] = true
		}
	}
	// Weapon flags (CalcActiveSkill.lua "Set weapon flags"): each hand's
	// weapon type must be in the active skill's and its supports'
	// weaponTypes; an empty main hand is unarmed ("None"); minion skills
	// ignore weapon types. Shield attacks, forced source weapons and "as
	// though" weapon types are not ported.
	var w1Flags, w2Flags uint64
	w1Attack, w2Attack := false, false
	weaponTypes := [][]string{}
	if len(ge.WeaponTypes) > 0 {
		weaponTypes = append(weaponTypes, ge.WeaponTypes)
	}
	for _, e := range s.effects {
		if e.ge.Support && len(e.ge.WeaponTypes) > 0 {
			weaponTypes = append(weaponTypes, e.ge.WeaponTypes)
		}
	}
	// getWeaponFlags (CalcActiveSkill.lua:212-244): a countsAsAll1H weapon
	// fits weapon types that hold a one handed melee type, and has all
	// their flags.
	handFlags := func(typ string, all1H bool) (uint64, bool, bool) {
		if _, known := weaponTypeInfo[typ]; !known {
			return 0, false, false
		}
		for _, l := range weaponTypes {
			if !contains(l, typ) && !(all1H && slices.ContainsFunc(oneHandMeleeTypes, func(t string) bool { return contains(l, t) })) {
				return 0, false, true
			}
		}
		f, _ := weaponFlags(typ)
		if all1H {
			f = f&^modFlagByName[weaponTypeInfo[typ].flag] | modFlagByName["Axe"] | modFlagByName["Claw"] |
				modFlagByName["Dagger"] | modFlagByName["Mace"] | modFlagByName["Sword"]
		}
		return f, true, true
	}
	attackLike := flags["attack"] || flags["dotFromAttack"]
	// Skills that require a shield (or shield attacks) are disabled without one.
	if (types[stRequiresShield] || flags["shieldAttack"]) && s.summon == nil {
		if it := db.items["Weapon 2"]; it == nil || it.Base == nil || it.Base.Type != "Shield" {
			s.disabled = true
		}
	}
	if flags["shieldAttack"] {
		// Shield attacks hit with the off hand only, without weapon flags.
		w2Attack = true
		flags["weapon2Attack"] = true
	}
	main := s.weapons[0]
	if main == "" {
		main = "None"
	}
	f, ok, known := handFlags(main, s.all1H[0])
	if flags["shieldAttack"] {
		ok, known = false, false
	}
	if !ok && s.summon != nil {
		f, ok = weaponFlags("None")
		main = "None"
	}
	if ok {
		if attackLike {
			w1Flags, w1Attack = f, true
			flags["weapon1Attack"] = true
			if weaponTypeInfo[main].melee && flags["melee"] {
				delete(flags, "projectile")
			} else if !weaponTypeInfo[main].melee && flags["projectile"] {
				delete(flags, "melee")
			}
		}
	} else if (types[stDualWieldOnly] || known) && s.summon == nil {
		s.disabled = true
	}
	if flags["shieldAttack"] {
		// weapon2Flags = 0
	} else if f2, ok2, known2 := handFlags(s.weapons[1], s.all1H[1]); ok2 && s.weapons[1] != "" {
		if types[stDualWieldRequiresDifferentTypes] && s.weapons[0] == s.weapons[1] && !s.all1H[0] && !s.all1H[1] {
			s.disabled = true
		} else if attackLike {
			w2Flags, w2Attack = f2, true
			flags["weapon2Attack"] = true
		}
	} else if (types[stDualWieldOnly] || known2 && s.weapons[1] != "") && s.summon == nil {
		s.disabled = true
	}
	var modFlags uint64
	if flags["hit"] {
		modFlags |= mfHit
	}
	if flags["attack"] {
		modFlags |= mfAttack
	} else {
		modFlags |= mfCast
		if flags["spell"] {
			modFlags |= mfSpell
		}
	}
	if flags["melee"] {
		modFlags |= mfMelee
	} else if flags["projectile"] {
		modFlags |= mfProjectile
		flags["chaining"] = true
	}
	if flags["area"] {
		modFlags |= mfArea
	}
	var kw uint64
	for _, x := range []struct {
		on bool
		kf uint64
	}{
		{flags["hit"], kfHit}, {types[stAura], kfAura}, {types[stAppliesCurse], kfCurse}, {types[stWarcry], kfWarcry},
		{types[stMovement], kfMovement}, {types[stVaal], kfVaal}, {types[stLightning], kfLightning},
		{types[stCold], kfCold}, {types[stFire], kfFire}, {types[stChaos], kfChaos}, {types[stPhysical], kfPhysical},
		{flags["brand"], kfBrand}, {flags["arrow"], kfArrow},
	} {
		if x.on {
			kw |= x.kf
		}
	}
	switch {
	case flags["totem"]:
		kw |= kfTotem
	case flags["trap"]:
		kw |= kfTrap
	case flags["mine"]:
		kw |= kfMine
	case !types[stTriggered]:
		flags["selfCast"] = true
	}
	if w1Attack && w1Flags&modFlagByName["Bow"] != 0 {
		kw |= kfBow
	}
	if types[stAttack] {
		kw |= kfAttack
	}
	if types[stSpell] && !flags["cast"] {
		kw |= kfSpell
	}
	baseFlags := map[string]bool{}
	for _, f := range ge.BaseFlags {
		baseFlags[f] = true
	}
	summonName := ""
	if s.summon != nil {
		summonName = s.summon.active.ge.Name
	}
	// Distance to the enemy (CalcActiveSkill.lua effectiveRange), for
	// DistanceRamp and MeleeProximity tags.
	dist, distVar := 40.0, "projectileDistance"
	if flags["melee"] {
		dist, distVar = 15, "meleeDistance"
	}
	if b := db.theBuild(); b != nil {
		if in, found := configInput(b.ActiveConfig(), distVar); found && in.Kind == pobcode.KindNumber {
			dist = in.Number
		}
	}
	// skillDist = env.mode_effective and effectiveRange (CalcActiveSkill.lua:492).
	if !modeEffective(db) {
		dist = 0
	}
	s.cfg = &Cfg{
		SkillDist: dist, HasSkillDist: modeEffective(db),
		Flags: modFlags | w1Flags, KeywordFlags: kw, SkillName: strings.TrimPrefix(ge.Name, "Vaal "),
		SkillID: s.active.id, BaseFlags: baseFlags, SkillTypes: types, SkillGem: s.active.gem,
		SlotName: s.cfg.SlotName, SummonSkillName: summonName, SkillPart: s.part,
	}
	if w1Attack {
		w := *s.cfg
		w.SkillCond = map[string]bool{"MainHandAttack": true}
		s.weapon1Cfg = &w
	}
	if w2Attack {
		w := *s.cfg
		w.Flags = modFlags | w2Flags
		w.SkillCond = map[string]bool{"OffHandAttack": true}
		s.weapon2Cfg = &w
	}
	if s.active.cfg != nil {
		s.cfg.SlotName, s.cfg.SocketColor, s.cfg.SocketNum = s.active.cfg.SlotName, s.active.cfg.SocketColor, s.active.cfg.SocketNum
	}
	list := db.child()
	s.mods = list
	// CalcActiveSkill.lua:516: the minion's damage fixup, less attack
	// damage and more attack speed.
	if md := db.minionData; md != nil && md.DamageFixup != 0 {
		list.Add(Mod{Name: "Damage", Type: "MORE", Value: -100 * md.DamageFixup, Flags: mfAttack, Source: "Damage Fixup"})
		list.Add(Mod{Name: "Speed", Type: "MORE", Value: 100 * md.DamageFixup, Flags: mfAttack, Source: "Damage Fixup"})
	}
	if ge.SpectreMinionList {
		var spectres []string
		if b := db.theBuild(); b != nil {
			for _, id := range b.SpectreList {
				if c.data.Minions[id] != nil {
					spectres = append(spectres, id)
				}
			}
		}
		s.spectres = spectres // a fresh list on every build of the skill
	}
	// CalcActiveSkill.lua:521-526: mods that apply curses (an item's
	// triggered curse with noSupports) stay enabled with
	// ForceEnableCurseApplication.
	curseApplication := s.active.groupSourceItem && s.flags["curse"] && s.active.hasSrc &&
		s.active.srcNoSupports && s.active.srcTriggered
	enabled := list.FlagCfg(s.cfg, "EnableSkill") || (curseApplication && list.Flag("ForceEnableCurseApplication"))
	if list.FlagCfg(s.cfg, "DisableSkill") && !enabled {
		s.disabled = true
	}
	if s.disabled {
		return
	}
	for _, e := range s.effects[1:] {
		c.mergeSkillInstanceMods(list, e, nil)
		if lv := e.ge.Levels[e.level]; lv != nil {
			if lv.ManaMultiplier != 0 {
				list.NewMod("SupportManaMultiplier", "MORE", lv.ManaMultiplier, "Skill:"+e.id)
			}
			if lv.ManaReservationPercent != 0 {
				s.data.set("manaReservationPercent", lv.ManaReservationPercent)
			}
			// CalcActiveSkill.lua:557-565.
			if lv.PvPDamageMultiplier != 0 {
				list.NewMod("PvpDamageMultiplier", "MORE", lv.PvPDamageMultiplier, "Skill:"+e.id)
			}
			levelStoredUses(s, lv)
		}
	}
	a := s.active
	lvl, q := a.level, a.quality
	if a.hasSrc {
		lvl, q = a.srcLevel, a.srcQuality
	}
	list.NewMod("GemLevel", "BASE", float64(lvl), "Max Level")
	list.NewMod("GemQuality", "BASE", float64(q), "Max Quality")
	if a.socketQuality > 0 { // CalcActiveSkill.lua:573-575
		list.NewMod("GemSocketQuality", "BASE", float64(a.socketQuality), "Socket Quality")
	}
	for _, p := range list.List(s.cfg, "SupportedGemProperty") {
		v, _ := p.Value.(map[string]any)
		if v["keyword"] != "grants_active_skill" || a.gem == nil || hasTag(a.gem, "support") {
			continue
		}
		key, _ := v["key"].(string)
		val, _ := num(v["value"])
		switch key {
		case "level":
			a.level += int(val)
		case "quality":
			a.quality += int(val)
		}
		var tags []map[string]any
		if len(p.Mod.Tags) > 0 {
			tags = p.Mod.Tags[:1]
		}
		list.Add(Mod{Name: "GemSupport" + upperFirst(key), Type: "BASE", Value: val, Source: p.Mod.Source, Tags: tags})
	}
	for _, p := range a.props {
		var tags []map[string]any
		if len(p.mod.Tags) > 0 {
			tags = p.mod.Tags[:1]
		}
		list.Add(Mod{Name: "Gem" + p.sourceType + upperFirst(p.key), Type: "BASE", Value: p.value, Source: p.mod.Source, Tags: tags})
	}
	c.mergeSkillInstanceMods(list, a, list.List(s.cfg, "ExtraSkillStat"))
	// Extra modifiers from the granted effect level.
	if lv := ge.Levels[a.level]; lv != nil {
		if lv.CritChance != 0 {
			s.data.set("CritChance", lv.CritChance)
		}
		if lv.DamageMultiplier != 0 {
			list.Add(Mod{Name: "Damage", Type: "MORE", Value: lv.DamageMultiplier, Flags: mfAttack, Source: "Skill:" + a.id})
		}
		if lv.AttackTime != 0 {
			s.data.set("attackTime", lv.AttackTime)
		}
		if lv.AttackSpeedMultiplier != 0 {
			s.data.set("attackSpeedMultiplier", lv.AttackSpeedMultiplier)
		}
		levelStoredUses(s, lv) // CalcActiveSkill.lua:615-620
		if lv.SoulPreventionDuration != 0 {
			s.data.set("soulPreventionDuration", lv.SoulPreventionDuration)
		}
	}
	for _, e := range list.List(s.cfg, "ExtraSkillMod") {
		v, _ := e.Value.(map[string]any)
		if m, isMod := v["mod"].(map[string]any); isMod {
			src, _ := m["source"].(string)
			if src == "" {
				src = e.Mod.Source
			}
			em := modFromMap(m, src)
			list.Add(em)
			s.extraSkillMods = append(s.extraSkillMods, em)
		}
	}
	if lv := ge.Levels[a.level]; lv != nil && lv.Cooldown != 0 {
		s.data.cooldown = optF{lv.Cooldown, true}
	}
	// Skill stages (Multiplier:<Name>MaxStages): the gem's stage count or
	// the maximum.
	stageName := strings.Join(strings.Fields(ge.Name), "")
	if limit := list.SumCfg(s.cfg, "BASE", "Multiplier:"+stageName+"MaxStages"); limit > 0 {
		flags["multiStage"] = true
		s.data.set("stagesMax", limit)
		count := limit
		if s.gem != nil && s.gem.SkillStageCount > 0 {
			count = float64(s.gem.SkillStageCount)
		}
		count = math.Max(count, 1+list.SumCfg(s.cfg, "BASE", "Multiplier:"+stageName+"MinimumStage"))
		s.activeStageCount, s.hasStageCount = count, true
		if count > 0 {
			list.NewMod("Multiplier:"+stageName+"Stage", "BASE", math.Min(limit, count), "Base")
			list.NewMod("Multiplier:"+stageName+"StageAfterFirst", "BASE", math.Min(limit-1, count-1), "Base")
			s.activeStageCount = count - 1
		}
	}
	// env.modDB: the player's SkillData, also for a minion's skills.
	player := db
	if db.owner != nil {
		player = db.owner
	}
	// Active mine multiplier (activeMineCount, the gem's skillMineCount).
	if flags["mine"] && s.gem != nil && s.gem.SkillMineCount > 0 {
		n := float64(s.gem.SkillMineCount)
		s.mineCount = n
		list.NewMod("Multiplier:ActiveMineCount", "BASE", n, "Base")
		if e := player.enemy; e != nil {
			e.multipliers["ActiveMineCount"] = math.Max(n, e.multipliers["ActiveMineCount"])
		}
	}
	for _, e := range player.List(s.cfg, "SkillData") {
		applySkillData(s.data, e.Value)
	}
	for _, e := range list.List(s.cfg, "SkillData") {
		applySkillData(s.data, e.Value)
	}
	c.separateBuffs(s)
	if len(s.buffs) > 0 && flags["attack"] {
		// Weapon flags (getWeaponFlags) are not ported: attack skills
		// match only mods without weapon flags.
		res.Incomplete = append(res.Incomplete, "attack skill weapon flags: "+ge.Name)
	}
}

// applySkillData applies a SkillData mod value {key, value} to d.
func applySkillData(d *skillData, v any) {
	m, _ := v.(map[string]any)
	if k, ok := m["key"].(string); ok {
		d.set(k, m["value"])
	}
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// levelStoredUses is the storedUses / vaalStoredUses part of a level
// (CalcActiveSkill.lua:560-565). "storedUses or 0 + vaalStoredUses" parses
// as storedUses or (0 + vaalStoredUses): an existing count is kept.
func levelStoredUses(s *activeSkill, lv *gamedata.SkillLevel) {
	if lv.StoredUses != 0 {
		s.data.storedUses = optF{lv.StoredUses, true}
	}
	if lv.VaalStoredUses != 0 && !s.data.storedUses.set {
		s.data.storedUses = optF{lv.VaalStoredUses, true}
	}
}
