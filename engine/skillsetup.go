package engine

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// resolveGem ports SkillsTabClass:LoadSkill and ProcessSocketGroup: the
// gem data (or the granted effect alone, for skills from items).
func (c *Calculator) resolveGem(g *pobcode.Gem) (*gamedata.Gem, string, bool) {
	idx, skills := c.gemIdx, c.data.Skills.Effects
	var gem *gamedata.Gem
	switch {
	case g.GemID != "":
		variants := idx.byGameID[g.GemID]
		if g.VariantID != "" {
			gem = variants[g.VariantID]
		} else if g.SkillID != "" {
			ids := make([]string, 0, len(variants))
			for v := range variants {
				ids = append(ids, v)
			}
			sort.Strings(ids)
			for _, v := range ids {
				if variants[v].GrantedEffectID == g.SkillID {
					gem = variants[v]
					break
				}
			}
		}
		if gem == nil {
			return nil, "", false
		}
	case g.SkillID != "":
		// Data.lua keys data.gemForSkill by the granted effect table, while
		// ProcessSocketGroup looks it up by skill id: a gem given only by
		// skill id (skills granted by items) never gets gem data.
		if skills[g.SkillID] != nil {
			return nil, g.SkillID, true
		}
		return nil, "", false
	default:
		return nil, "", false
	}
	if ge := skills[gem.GrantedEffectID]; ge == nil || ge.Unsupported {
		return nil, "", false
	}
	return gem, gem.GrantedEffectID, true
}

// validateLevel is calcLib.validateGemLevel.
func validateLevel(level int, ge *gamedata.Skill, gem *gamedata.Gem) int {
	if ge.Levels[level] != nil {
		return level
	}
	level = max(1, level)
	if n := len(ge.Levels); n > 0 {
		level = min(n, level)
	}
	if ge.Levels[level] == nil && gem != nil && gem.NaturalMaxLevel != 0 {
		level = gem.NaturalMaxLevel
	}
	if ge.Levels[level] == nil {
		keys := make([]int, 0, len(ge.Levels))
		for k := range ge.Levels {
			keys = append(keys, k)
		}
		sort.Ints(keys)
		if len(keys) > 0 {
			level = keys[0] // next(levels): an arbitrary level in PoB
		}
	}
	return level
}

// applyGemMods is CalcSetup.lua applyGemMods.
func (c *Calculator) applyGemMods(e *skillEffect, props []ListEntry) {
	for _, p := range props {
		v, _ := p.Value.(map[string]any)
		if v == nil || e.gem == nil {
			continue
		}
		match := true
		if kws, ok := v["keywordList"].([]any); ok {
			for _, k := range kws {
				ks, _ := k.(string)
				if !gemIsType(e.gem, ks, c.gemIdx) {
					match = false
					break
				}
			}
		} else {
			kw, _ := v["keyword"].(string)
			match = gemIsType(e.gem, kw, c.gemIdx)
		}
		if !match {
			continue
		}
		src := "Global"
		for _, t := range p.Mod.Tags {
			if tagStr(t, "type") == "SocketedIn" {
				src = "Item"
				break
			}
		}
		key, _ := v["key"].(string)
		val, _ := num(v["value"])
		switch key {
		case "level":
			e.level += int(val)
		case "quality":
			e.quality += int(val)
		}
		e.props = append(e.props, gemProperty{key: key, value: val, sourceType: src, mod: p.Mod})
	}
}

// socketQuality is CalcSetup.lua getMatchingSocketQuality.
func socketQuality(item *itemtext.Item, ge *gamedata.Skill, socketIndex int) int {
	if item == nil || len(item.Sockets) == 0 {
		return 0
	}
	colours := map[int]byte{1: 'R', 2: 'G', 3: 'B'}
	colour, ok := colours[ge.Color]
	if !ok || socketIndex < 1 || socketIndex > len(item.Sockets) {
		return 0
	}
	if item.Sockets[socketIndex-1].Color == colour {
		return matchingSocketQualityBonus
	}
	return 0
}

// addBestSupport is CalcSetup.lua addBestSupport.
func addBestSupport(e *skillEffect, list []*skillEffect) []*skillEffect {
	for i, o := range list {
		switch {
		case e.id == o.id:
			if e.level > o.level || (e.level == o.level && e.quality > o.quality) {
				list[i] = e
			}
			return list
		case e.ge.PlusVersionOf == o.id:
			list[i] = e
			return list
		case o.ge.PlusVersionOf == e.id:
			return list
		}
	}
	return append(list, e)
}

type groupCfg struct {
	cfg   *Cfg
	props []ListEntry
}

// setupSkills ports the skill part of calcs.initEnv: it returns the
// player's active skills with their supports.
func (c *Calculator) setupSkills(db *ModDB, b *pobcode.Build, useSwap bool, res *Result) []*activeSkill {
	set := b.ActiveSkills()
	if set == nil || len(set.Groups) == 0 {
		return nil
	}
	skills := c.data.Skills.Effects
	mainGroup := min(max(len(set.Groups), 1), max(b.MainSocketGroup, 1))
	// CalcSetup.lua crossLinkedSupportGroups: the slot of the item with
	// LinkedSupport (The Squire) -> the slots its socketed supports support.
	crossLinked := map[string][]string{}
	if items := b.ActiveItems(); items != nil {
		for _, e := range db.List(nil, "LinkedSupport") {
			v, _ := e.Value.(map[string]any)
			target, _ := v["targetSlotName"].(string)
			for _, sl := range items.Slots {
				if sl.ItemID != 0 && strings.HasPrefix(e.Mod.Source, fmt.Sprintf("Item:%d:", sl.ItemID)) {
					src := sl.Name
					if items.UseSecondWeaponSet {
						src = strings.Replace(src, " Swap", "", 1)
					}
					crossLinked[src] = append(crossLinked[src], target)
					break
				}
			}
		}
	}

	var weapons [2]string
	for i, slot := range []string{"Weapon 1", "Weapon 2"} {
		if it := db.items[slot]; it != nil && it.Base != nil && it.Base.Weapon != nil {
			weapons[i] = it.Base.Type
		}
	}

	slotOf := func(g *pobcode.SocketGroup) string {
		if g.Slot != "" {
			return strings.Replace(g.Slot, " Swap", "", 1)
		}
		if g.Source != "" && strings.Contains(strings.ToLower(g.Source), "tree") {
			return "Passive Tree"
		}
		return ""
	}
	enabled := make([]bool, len(set.Groups))
	cfgs := make([]*groupCfg, len(set.Groups))
	for i := range set.Groups {
		g := &set.Groups[i]
		// group.slotEnabled: weapon swap slots count only with the second weapon set.
		slotEnabled := true
		if strings.HasPrefix(g.Slot, "Weapon") {
			slotEnabled = strings.HasSuffix(g.Slot, " Swap") == useSwap
		}
		enabled[i] = i+1 == mainGroup || (g.Enabled && slotEnabled)
		// CalcSetup.lua rebuilds the groups of tree-granted skills from the
		// allocated nodes' ExtraSkill mods and removes the rest ("Remove any
		// socket groups that no longer have a matching item"): a saved
		// Tree:<id> group whose node no longer grants the skill (a build
		// moved to a newer tree) is gone.
		if strings.HasPrefix(g.Source, "Tree:") && !treeGrants(db, g) {
			enabled[i] = false
		}
		cfg := &Cfg{SlotName: slotOf(g), GemCounts: map[string]int{}}
		cfgs[i] = &groupCfg{cfg: cfg, props: db.List(&Cfg{SlotName: cfg.SlotName}, "GemProperty")}
	}

	// gemCfg copies the group cfg with the gem's socket.
	gemCfg := func(gc *groupCfg, gemIndex int) (*Cfg, bool) {
		cfg := *gc.cfg
		// copyTable(groupCfg, true): the gem counts as they are now.
		cfg.GemCounts = make(map[string]int, len(gc.cfg.GemCounts))
		for k, v := range gc.cfg.GemCounts {
			cfg.GemCounts[k] = v
		}
		cfg.SocketNum = gemIndex
		item := db.items[gc.cfg.SlotName]
		if item != nil && gemIndex <= len(item.Sockets) {
			cfg.SocketColor = string(item.Sockets[gemIndex-1].Color)
			return &cfg, true
		}
		return &cfg, false
	}
	// socketIndexByGem: gems of all groups in a slot fill its sockets in order.
	socketIndex := map[*pobcode.Gem]int{}
	slotCounts := map[string]int{}
	for i := range set.Groups {
		g := &set.Groups[i]
		if g.Slot == "" {
			continue
		}
		for gi := range g.Gems {
			socketIndex[&g.Gems[gi]] = slotCounts[g.Slot] + gi + 1
		}
		slotCounts[g.Slot] += len(g.Gems)
	}
	counted := map[*pobcode.Gem]bool{}
	countGem := func(gc *groupCfg, g *pobcode.Gem, gem *gamedata.Gem) {
		if counted[g] {
			return
		}
		counted[g] = true
		for _, a := range []string{"intelligence", "dexterity", "strength"} {
			if hasTag(gem, a) {
				gc.cfg.GemCounts[a+"Gems"]++
			}
		}
	}

	// newEffect builds an activeEffect or supportEffect for a gem: its
	// matching socket quality, gemCfg and GemProperty levels and quality.
	newEffect := func(gc *groupCfg, gi int, gm *pobcode.Gem, gem *gamedata.Gem, eid string, ge *gamedata.Skill) *skillEffect {
		sq := socketQuality(db.items[gc.cfg.SlotName], ge, socketIndex[gm])
		eff := &skillEffect{id: eid, ge: ge, gem: gem, level: gm.Level, quality: gm.Quality + sq,
			srcLevel: gm.Level, srcQuality: gm.Quality, srcPart: gm.SkillPart, hasSrc: true}
		if gem != nil {
			cfg, socketed := gemCfg(gc, gi+1)
			eff.cfg = cfg
			props := gc.props
			if socketed {
				props = db.List(cfg, "GemProperty")
			}
			c.applyGemMods(eff, props)
			countGem(gc, gm, gem)
		}
		return eff
	}

	// Support gems, per group.
	supports := make([][]*skillEffect, len(set.Groups))
	for i := range set.Groups {
		g := &set.Groups[i]
		if !enabled[i] {
			continue
		}
		gc := cfgs[i]
		for _, e := range db.List(gc.cfg, "ExtraSupport") {
			v, _ := e.Value.(map[string]any)
			applies, _ := v["appliesToGrantedSkills"].(bool)
			if g.Source != "" && !applies {
				continue
			}
			id, _ := v["skillId"].(string)
			ge := skills[id]
			if ge != nil && !ge.Support {
				id = "Support" + id
				ge = skills[id]
			}
			if ge == nil {
				continue
			}
			lvl, _ := num(v["level"])
			gemID := c.gemIdx.forBaseName[strings.ToLower(ge.Name)]
			if gemID == "" {
				gemID = c.gemIdx.forBaseName[strings.ToLower(ge.Name+" Support")]
			}
			eff := &skillEffect{id: id, ge: ge, gem: c.gemIdx.gems[gemID], level: int(lvl), fromItem: true, appliesToGranted: applies}
			supports[i] = addBestSupport(eff, supports[i])
		}
		disabled := db.FlagCfg(gc.cfg, "SocketedSupportGemsHaveNoEffect")
		for gi := range g.Gems {
			gm := &g.Gems[gi]
			if !gm.Enabled {
				continue
			}
			gem, id, ok := c.resolveGem(gm)
			if !ok {
				continue
			}
			ids := []string{id}
			if gem != nil && gem.SecondaryGrantedEffectID != "" {
				ids = append(ids, gem.SecondaryGrantedEffectID)
			}
			for _, eid := range ids {
				ge := skills[eid]
				if ge == nil || !ge.Support {
					continue
				}
				eff := newEffect(gc, gi, gm, gem, eid, ge)
				eff.level = validateLevel(eff.level, ge, gem)
				if !disabled {
					supports[i] = addBestSupport(eff, supports[i])
				}
			}
		}
	}

	// Active skills with their supports.
	var out []*activeSkill
	for i := range set.Groups {
		g := &set.Groups[i]
		if !enabled[i] {
			continue
		}
		gc := cfgs[i]
		for gi := range g.Gems {
			gm := &g.Gems[gi]
			if !gm.Enabled {
				continue
			}
			gem, id, ok := c.resolveGem(gm)
			if !ok {
				if gm.NameSpec != "" || gm.SkillID != "" || gm.GemID != "" {
					res.Incomplete = append(res.Incomplete, fmt.Sprintf("skill gem %q not found", firstNonEmpty(gm.NameSpec, gm.SkillID, gm.GemID)))
				}
				continue
			}
			ids := []string{id}
			if gem != nil && gem.SecondaryGrantedEffectID != "" {
				ids = append(ids, gem.SecondaryGrantedEffectID)
			}
			for n, eid := range ids {
				ge := skills[eid]
				if ge == nil || ge.Support || ge.Unsupported {
					continue
				}
				if ge.HasGlobalEffect && !(n == 0 && gm.EnableGlobal1 || n == 1 && gm.EnableGlobal2) {
					continue
				}
				eff := newEffect(gc, gi, gm, gem, eid, ge)
				applied := append([]*skillEffect(nil), supports[i]...)
				if g.Source != "" && gc.cfg.SlotName != "" {
					// Skills granted by an item take the supports of every group in its slot.
					for j := range set.Groups {
						if j != i && enabled[j] && cfgs[j].cfg.SlotName == gc.cfg.SlotName {
							for _, s := range supports[j] {
								applied = addBestSupport(s, applied)
							}
						}
					}
				}
				// Then the supports of cross-linked slots.
				for _, src := range slices.Sorted(maps.Keys(crossLinked)) {
					if !contains(crossLinked[src], gc.cfg.SlotName) {
						continue
					}
					for j := range set.Groups {
						if enabled[j] && cfgs[j].cfg.SlotName == src {
							for _, s := range supports[j] {
								applied = addBestSupport(s, applied)
							}
						}
					}
				}
				s := c.createActiveSkill(eff, applied, g, weapons, nil)
				s.cfg = &Cfg{SlotName: gc.cfg.SlotName}
				s.gem = gm
				out = append(out, s)
			}
		}
	}
	// The main skill: the group's mainActiveSkill among its active skills.
	var inMain []*activeSkill
	for _, s := range out {
		if s.group == &set.Groups[mainGroup-1] {
			inMain = append(inMain, s)
		}
	}
	if len(inMain) > 0 {
		inMain[min(len(inMain), max(set.Groups[mainGroup-1].MainActiveSkill, 1))-1].main = true
	} else if ge := skills["Melee"]; ge != nil {
		// Add a default main skill if none are specified (CalcSetup.lua):
		// level 1 Default Attack with no supports.
		s := c.createActiveSkill(&skillEffect{id: "Melee", ge: ge, level: 1}, nil, nil, weapons, nil)
		s.cfg = &Cfg{}
		s.main = true
		out = append(out, s)
	}
	return out
}

func typeSet(l []int) map[int]bool {
	m := make(map[int]bool, len(l))
	for _, t := range l {
		m[t] = true
	}
	return m
}

// typeExpressionMatch is calcLib.doesTypeExpressionMatch.
func typeExpressionMatch(check []int, types, minionTypes map[int]bool) bool {
	var stack []bool
	for _, t := range check {
		switch t {
		case stOR, stAND:
			if len(stack) < 2 {
				return false
			}
			other := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if t == stOR {
				stack[len(stack)-1] = stack[len(stack)-1] || other
			} else {
				stack[len(stack)-1] = stack[len(stack)-1] && other
			}
		case stNOT:
			if len(stack) == 0 {
				return false
			}
			stack[len(stack)-1] = !stack[len(stack)-1]
		default:
			stack = append(stack, types[t] || minionTypes[t])
		}
	}
	for _, v := range stack {
		if v {
			return true
		}
	}
	return false
}

// canSupport is calcLib.canGrantedEffectSupportActiveSkill without the
// weapon type and trigger checks (reported by the caller).
func canSupport(sup *skillEffect, s *activeSkill) bool {
	ge := sup.ge
	if ge.Unsupported || s.active.ge.CannotBeSupported {
		return false
	}
	if ge.SupportGemsOnly && s.active.gem == nil {
		return false
	}
	// srcInstance.fromItem: gems of groups CalcSetup creates for skills granted by items.
	srcFromItem := s.group != nil && strings.HasPrefix(s.group.Source, "Item")
	if !sup.appliesToGranted && sup.fromItem && ge.Support && (s.active.ge.FromItem || srcFromItem) {
		return false
	}
	// A minion's skill is supported by what supports its summoning skill.
	types, minionTypes := s.types, map[int]bool(nil)
	if s.summon != nil {
		types = s.summon.types
	}
	if !ge.IgnoreMinionTypes {
		minionTypes = s.minionTypes
		if s.summon != nil {
			minionTypes = s.summon.minionTypes
		}
	}
	if len(ge.ExcludeSkillTypes) > 0 && typeExpressionMatch(ge.ExcludeSkillTypes, types, nil) {
		return false
	}
	if ge.IsTrigger && s.summon != nil {
		return false // triggers support only the player's skills
	}
	if len(ge.WeaponTypes) > 0 {
		// Varunastra's countsAsAll1H is not ported.
		have := map[string]bool{}
		if len(s.active.ge.WeaponTypes) > 0 {
			for _, t := range s.active.ge.WeaponTypes {
				have[t] = true
			}
		} else if types[stSkillGrantedBySupport] {
			for _, t := range s.weapons {
				if t != "" {
					have[t] = true
				}
			}
		}
		match := false
		for _, t := range ge.WeaponTypes {
			match = match || have[t]
		}
		if !match {
			return false
		}
	}
	return len(ge.RequireSkillTypes) == 0 || typeExpressionMatch(ge.RequireSkillTypes, types, minionTypes)
}

// createActiveSkill is calcs.createActiveSkill.
func (c *Calculator) createActiveSkill(active *skillEffect, supports []*skillEffect, g *pobcode.SocketGroup, weapons [2]string, summon *activeSkill) *activeSkill {
	s := &activeSkill{active: active, group: g, data: map[string]any{}, weapons: weapons, supportList: supports, summon: summon,
		types: typeSet(active.ge.SkillTypes), minionTypes: typeSet(active.ge.MinionSkillTypes), flags: map[string]bool{}}
	for _, f := range active.ge.BaseFlags {
		s.flags[f] = true
	}
	if s.types[stAttack] || s.types[stDamage] || s.types[stProjectile] {
		s.flags["hit"] = true
	}
	s.effects = []*skillEffect{active}
	// Pass 1 adds the types of supports that fit in group order. PoB's
	// retry loop over "rejected" supports only collects non-support
	// effects, so it never adds types; it is not ported.
	for _, sup := range supports {
		if sup.ge.Support && canSupport(sup, s) {
			for _, t := range sup.ge.AddSkillTypes {
				s.types[t] = true
			}
		}
	}
	for _, sup := range supports {
		if sup.ge.Support && canSupport(sup, s) {
			s.effects = append(s.effects, sup)
			if summon != nil {
				continue
			}
			for _, f := range sup.ge.AddFlags {
				s.flags[f] = true
			}
		}
	}
	return s
}

// treeGrants reports whether a tree-sourced group's skill is granted by an
// ExtraSkill mod of that node (source "Tree:<id>").
func treeGrants(db *ModDB, g *pobcode.SocketGroup) bool {
	if len(g.Gems) == 0 {
		return false
	}
	for _, e := range db.List(nil, "ExtraSkill") {
		if e.Mod.Source != g.Source {
			continue
		}
		v, _ := e.Value.(map[string]any)
		if id, _ := v["skillId"].(string); id == g.Gems[0].SkillID {
			return true
		}
	}
	return false
}
