package engine

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// This file ports the player's skills as far as they give buffs:
// CalcSetup.lua (socket groups to active skills, gem levels and supports),
// CalcActiveSkill.lua (createActiveSkill, buildActiveSkillModList),
// CalcTools.lua (buildSkillInstanceStats and helpers) and the Buff and Aura
// branches of CalcPerform.lua. Skill parts, minions, triggers, weapon flags
// of attacks and party buffs are not ported.

// Data/Global.lua ModFlag and KeywordFlag bits used here.
const (
	mfAttack     = 0x1
	mfSpell      = 0x2
	mfHit        = 0x4
	mfDot        = 0x8
	mfCast       = 0x10
	mfMelee      = 0x100
	mfArea       = 0x200
	mfProjectile = 0x400

	kfAura      = 0x1
	kfCurse     = 0x2
	kfWarcry    = 0x4
	kfMovement  = 0x8
	kfPhysical  = 0x10
	kfFire      = 0x20
	kfCold      = 0x40
	kfLightning = 0x80
	kfChaos     = 0x100
	kfVaal      = 0x200
	kfBow       = 0x400
	kfArrow     = 0x800
	kfTrap      = 0x1000
	kfMine      = 0x2000
	kfTotem     = 0x4000
	kfAttack    = 0x10000
	kfSpell     = 0x20000
	kfHit       = 0x40000
	kfBrand     = 0x100000
)

const matchingSocketQualityBonus = 10 // Data.lua data.misc.MatchingSocketQualityBonus

// gemIndex holds Data.lua's gem lookup tables.
type gemIndex struct {
	gems        map[string]*gamedata.Gem
	byGameID    map[string]map[string]*gamedata.Gem // gemsByGameId[gameId][variantId]
	forBaseName map[string]string                   // gemForBaseName: lower-case name -> gem id
	vaalBase    map[string]string                   // gemVaalGemIdForBaseGemId
}

func newGemIndex(d *gamedata.Data) *gemIndex {
	x := &gemIndex{gems: d.Gems, byGameID: map[string]map[string]*gamedata.Gem{},
		forBaseName: map[string]string{}, vaalBase: map[string]string{}}
	ids := make([]string, 0, len(d.Gems))
	for id := range d.Gems {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		g := d.Gems[id]
		if x.byGameID[g.GameID] == nil {
			x.byGameID[g.GameID] = map[string]*gamedata.Gem{}
		}
		x.byGameID[g.GameID][g.VariantID] = g
		base := g.Name
		if ge := d.Skills.Effects[g.GrantedEffectID]; ge != nil && ge.Support && g.GrantedEffectID != "SupportBarrage" {
			base += " Support"
		}
		x.forBaseName[strings.ToLower(base)] = id
		if g.BaseTypeName != "" && g.BaseTypeName != base {
			x.forBaseName[strings.ToLower(g.BaseTypeName)] = id
		}
	}
	for _, id := range ids {
		g := d.Gems[id]
		if !g.VaalGem {
			continue
		}
		for _, oid := range ids {
			if d.Gems[oid].GrantedEffectID == g.SecondaryGrantedEffectID {
				x.vaalBase[id] = oid
				break
			}
		}
	}
	// Data.lua: a Vaal gem whose base skill has transfigured variants
	// (AltX, AltY) gets a gem per variant, "Vaal <variant name>". The
	// skill's baseTypeName is its name for these.
	x.gems = make(map[string]*gamedata.Gem, len(d.Gems))
	for id, g := range d.Gems {
		x.gems[id] = g
	}
	for _, id := range ids {
		g := d.Gems[id]
		if !g.VaalGem {
			continue
		}
		for _, alt := range []string{"AltX", "AltY"} {
			sec := d.Skills.Effects[g.SecondaryGrantedEffectID+alt]
			if sec == nil {
				continue
			}
			n := *g
			n.ID, n.Name = id+alt, "Vaal "+sec.Name
			n.BaseTypeName = ""
			n.VariantID = g.VariantID + alt
			n.SecondaryGrantedEffectID = g.SecondaryGrantedEffectID + alt
			x.gems[id+alt] = &n
			x.byGameID[n.GameID][n.VariantID] = &n
			x.forBaseName[strings.ToLower(n.Name)] = id + alt
			if b, ok := x.vaalBase[id]; ok {
				x.vaalBase[id+alt] = b + alt
			}
		}
	}
	return x
}

// gameID is calcLib.getGameIdFromGemName(name, true).
func (x *gemIndex) gameID(name string) string {
	id, ok := x.forBaseName[strings.ToLower(name)]
	if !ok {
		return ""
	}
	g := x.gems[id]
	if g.VaalGem {
		if b := x.gems[x.vaalBase[id]]; b != nil {
			return b.GameID
		}
		return ""
	}
	return g.GameID
}

func hasTag(g *gamedata.Gem, tag string) bool {
	for _, t := range g.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// gemIsType is calcLib.gemIsType; idx is set for includeTransfigured.
func gemIsType(g *gamedata.Gem, typ string, idx *gemIndex) bool {
	name := strings.ToLower(g.Name)
	active := typ == "active skill" || typ == "grants_active_skill" || typ == "skill"
	switch {
	case typ == "all":
		return true
	case typ == "elemental" && (hasTag(g, "fire") || hasTag(g, "cold") || hasTag(g, "lightning")):
		return true
	case typ == "aoe" && hasTag(g, "area"):
		return true
	case typ == "trap or mine" && (hasTag(g, "trap") || hasTag(g, "mine")):
		return true
	case active && hasTag(g, "grants_active_skill") && !hasTag(g, "support"):
		return true
	case typ == "non-vaal" && !hasTag(g, "vaal"):
		return true
	case typ == "non-exceptional" && !hasTag(g, "exceptional"):
		return true
	case typ == name || typ == strings.TrimPrefix(name, "vaal "):
		return true
	case idx != nil && sameGameID(idx, g.Name, typ):
		return true
	}
	return !active && hasTag(g, typ)
}

// sameGameID is calcLib.isGemIdSame(a, b, true).
func sameGameID(idx *gemIndex, a, b string) bool {
	x, y := idx.gameID(a), idx.gameID(b)
	return x != "" && y != "" && x == y
}

// gemProperty is one entry of effect.gemPropertyInfo.
type gemProperty struct {
	key        string
	value      float64
	sourceType string // "Global" or "Item"
	mod        *Mod
}

// skillEffect is an activeEffect or supportEffect of CalcSetup.lua.
type skillEffect struct {
	id               string
	ge               *gamedata.Skill
	gem              *gamedata.Gem // gemData; nil for skills granted by items
	level, quality   int
	srcLevel         int // srcInstance.level and quality: the gem's own values
	srcQuality       int
	srcPart          int // srcInstance.skillPart (0: not set)
	hasSrc           bool
	cfg              *Cfg // gemCfg
	props            []gemProperty
	fromItem         bool // set on supports granted by items (ExtraSupport)
	appliesToGranted bool
	actorLevel       float64 // activeEffect.actorLevel: the minion's level for minion skills
}

// skillBuff is an entry of activeSkill.buffList.
type skillBuff struct {
	typ, name       string
	cond, enemyCond string
	activeSkillBuff bool
	applyNotPlayer  bool
	applyMinions    bool
	applyAllies     bool
	stackVar        string // effectStackVar and its limit (Debuff stacks)
	stackLimitVar   string
	stackLimit      float64
	hasStackLimit   bool
	allowTotemBuff  bool
	mods            []Mod
}

type activeSkill struct {
	// extraSkillMods is activeSkill.extraSkillModList: the ExtraSkillMod mods
	// added to the skill, also given to its minions.
	extraSkillMods []Mod
	active         *skillEffect
	effects        []*skillEffect // the active effect, then compatible supports
	types          map[int]bool
	minionTypes    map[int]bool
	flags          map[string]bool
	group          *pobcode.SocketGroup
	gem            *pobcode.Gem // srcInstance: the gem of the active effect
	cfg            *Cfg
	mods           *ModDB // skillModList
	data           map[string]any
	buffs          []*skillBuff
	disabled       bool
	main           bool // env.player.mainSkill
	// weapons are the types of the equipped weapons (weaponData1/2.type),
	// which supports with weaponTypes check for skills granted by supports.
	weapons [2]string
	// supportList is the support list the skill was created with; summon
	// is the summoning skill of a minion's skill (activeSkill.summonSkill).
	supportList []*skillEffect
	summon      *activeSkill
	// weapon1Cfg and weapon2Cfg are the skill cfgs of main and off hand
	// attacks (weapon flags).
	weapon1Cfg, weapon2Cfg *Cfg
	// weaponRange1/2 are actor.weaponRange1/2 of calcs.offence (melee).
	weaponRange1, weaponRange2 float64
	// part is activeSkill.skillPart (1-based, 0 for single part skills).
	part int
	// mirage is activeSkill.mirage: the Mirage Archer copy's output.
	mirage *mirageResult
	// mineCount is activeSkill.activeMineCount (0 when unset).
	mineCount float64
	// activeStageCount is activeSkill.activeStageCount of a multi-stage
	// skill (the stages after the first), set when hasStageCount.
	activeStageCount float64
	hasStageCount    bool
}

// modFlagByName is the weapon part of Data/Global.lua ModFlag.
var modFlagByName = map[string]uint64{
	"Axe": 0x10000, "Bow": 0x20000, "Claw": 0x40000, "Dagger": 0x80000, "Mace": 0x100000,
	"Staff": 0x200000, "Sword": 0x400000, "Wand": 0x800000, "Unarmed": 0x1000000, "Fishing": 0x2000000,
}

const (
	mfWeapon       = 0x2000
	mfWeaponMelee  = 0x4000000
	mfWeaponRanged = 0x8000000
	mfWeapon1H     = 0x10000000
	mfWeapon2H     = 0x20000000
)

// weaponFlags is CalcActiveSkill.lua getWeaponFlags without weaponTypes
// checks (minion skills ignore them): the ModFlags of a weapon type.
func weaponFlags(typ string) (uint64, bool) {
	info, ok := weaponTypeInfo[typ]
	if !ok {
		return 0, false
	}
	f := modFlagByName[info.flag]
	if typ != "None" {
		f |= mfWeapon
		if info.oneHand {
			f |= mfWeapon1H
		} else {
			f |= mfWeapon2H
		}
		if info.melee {
			f |= mfWeaponMelee
		} else {
			f |= mfWeaponRanged
		}
	}
	return f, true
}

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

// mainSkill is env.player.mainSkill, nil when there is none.
func mainSkill(skills []*activeSkill) *activeSkill {
	for _, s := range skills {
		if s.main {
			return s
		}
	}
	return nil
}

// combatConditions ports the mode_combat part of doActorAttribsConditions:
// conditions that follow from using the main skill.
func combatConditions(db *ModDB, s *activeSkill) {
	// mode_effective: exposure the player can apply.
	for _, el := range []string{"Fire", "Cold", "Lightning"} {
		if (s != nil && s.mods != nil && s.mods.SumCfg(s.cfg, "BASE", el+"ExposureChance") > 0) || db.Sum("BASE", el+"ExposureChance") > 0 {
			db.conditions["CanApply"+el+"Exposure"] = true
		}
	}
	if s == nil {
		return
	}
	cond, f := db.conditions, s.flags
	if !truthy(s.data["triggered"]) && !f["trap"] && !f["mine"] && !f["totem"] {
		if f["attack"] {
			cond["AttackedRecently"] = true
		} else if f["spell"] {
			cond["CastSpellRecently"] = true
		}
		if s.types[stMovement] {
			cond["UsedMovementSkillRecently"] = true
		}
		if f["minion"] && !f["permanentMinion"] {
			cond["UsedMinionSkillRecently"] = true
		}
		if s.types[stVaal] {
			cond["UsedVaalSkillRecently"] = true
		}
		if s.types[stChannel] {
			cond["Channelling"] = true
		}
	}
	if f["hit"] && !f["trap"] && !f["mine"] && !f["totem"] {
		cond["HitRecently"] = true
		if f["spell"] {
			cond["HitSpellRecently"] = true
		}
	}
	if f["totem"] {
		cond["HaveTotem"], cond["SummonedTotemRecently"] = true, true
		if f["hit"] {
			cond["TotemsHitRecently"] = true
			if f["spell"] {
				cond["TotemsSpellHitRecently"] = true
			}
		}
	}
	if f["mine"] {
		cond["DetonatedMinesRecently"] = true
	}
	if f["trap"] {
		cond["TriggeredTrapsRecently"] = true
	}
}

// mainCfg is env.player.mainSkill.skillCfg, nil when there is none (PoB
// then uses a default Melee skill, which is not built).
func mainCfg(skills []*activeSkill) *Cfg {
	for _, s := range skills {
		if s.main {
			return s.cfg
		}
	}
	return nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
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
				eff := (3.885209 + 0.360246*(actorLevel-1)) * be * math.Pow(1+ge.IncrementalEffectiveness, actorLevel-1)
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
			if tm, ok := t.(map[string]any); ok {
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
		if sub, ok := v["mod"].(map[string]any); ok {
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
	flags["buffs"], flags["combat"], flags["effective"] = true, true, true
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
	//nolint:goconst // PoB base item type name
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
	// ignore weapon types. Shield attacks, forced source weapons,
	// countsAsAll1H and "as though" weapon types are not ported.
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
	handFlags := func(typ string) (uint64, bool, bool) {
		if _, known := weaponTypeInfo[typ]; !known {
			return 0, false, false
		}
		for _, l := range weaponTypes {
			if !contains(l, typ) {
				return 0, false, true
			}
		}
		f, _ := weaponFlags(typ)
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
	f, ok, known := handFlags(main)
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
	} else if f, ok, known := handFlags(s.weapons[1]); ok && s.weapons[1] != "" {
		if types[stDualWieldRequiresDifferentTypes] && s.weapons[0] == s.weapons[1] {
			s.disabled = true
		} else if attackLike {
			w2Flags, w2Attack = f, true
			flags["weapon2Attack"] = true
		}
	} else if (types[stDualWieldOnly] || known && s.weapons[1] != "") && s.summon == nil {
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
		if in, ok := configInput(b.ActiveConfig(), distVar); ok && in.Kind == pobcode.KindNumber {
			dist = in.Number
		}
	}
	s.cfg = &Cfg{
		SkillDist: dist, HasSkillDist: true,
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
	if list.FlagCfg(s.cfg, "DisableSkill") && !list.FlagCfg(s.cfg, "EnableSkill") {
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
				s.data["manaReservationPercent"] = lv.ManaReservationPercent
			}
		}
	}
	a := s.active
	lvl, q := a.level, a.quality
	if a.hasSrc {
		lvl, q = a.srcLevel, a.srcQuality
	}
	list.NewMod("GemLevel", "BASE", float64(lvl), "Max Level")
	list.NewMod("GemQuality", "BASE", float64(q), "Max Quality")
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
			s.data["CritChance"] = lv.CritChance
		}
		if lv.DamageMultiplier != 0 {
			list.Add(Mod{Name: "Damage", Type: "MORE", Value: lv.DamageMultiplier, Flags: mfAttack, Source: "Skill:" + a.id})
		}
		if lv.AttackTime != 0 {
			s.data["attackTime"] = lv.AttackTime
		}
		if lv.AttackSpeedMultiplier != 0 {
			s.data["attackSpeedMultiplier"] = lv.AttackSpeedMultiplier
		}
		if lv.StoredUses != 0 {
			s.data["storedUses"] = lv.StoredUses
		}
	}
	for _, e := range list.List(s.cfg, "ExtraSkillMod") {
		v, _ := e.Value.(map[string]any)
		if m, ok := v["mod"].(map[string]any); ok {
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
		s.data["cooldown"] = lv.Cooldown
	}
	// Skill stages (Multiplier:<Name>MaxStages): the gem's stage count or
	// the maximum.
	stageName := strings.Join(strings.Fields(ge.Name), "")
	if limit := list.SumCfg(s.cfg, "BASE", "Multiplier:"+stageName+"MaxStages"); limit > 0 {
		flags["multiStage"] = true
		s.data["stagesMax"] = limit
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
		skillData(s.data, e.Value)
	}
	for _, e := range list.List(s.cfg, "SkillData") {
		skillData(s.data, e.Value)
	}
	c.separateBuffs(s)
	if len(s.buffs) > 0 && flags["attack"] {
		// Weapon flags (getWeaponFlags) are not ported: attack skills
		// match only mods without weapon flags.
		res.Incomplete = append(res.Incomplete, "attack skill weapon flags: "+ge.Name)
	}
}

func skillData(d map[string]any, v any) {
	m, _ := v.(map[string]any)
	if k, ok := m["key"].(string); ok {
		d[k] = m["value"]
	}
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

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
					buff.applyNotPlayer = buff.applyNotPlayer || truthy(s.data["buffNotPlayer"])
					buff.applyMinions = buff.applyMinions || truthy(s.data["buffMinions"])
					buff.applyAllies = truthy(s.data["buffAllies"])
					buff.allowTotemBuff = truthy(s.data["allowTotemBuff"])
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
				if truthy(s.data["thisIsNotABuff"]) {
					notBuff[b.name] = true
				}
			case "Aura":
				extra := extraAuraMods(db, cfg)
				if !truthy(s.data["auraCannotAffectSelf"]) {
					mult := (1 + list.SumCfg(cfg, "INC", auraEffectNames...)/100) * list.MoreCfg(cfg, auraEffectNames...)
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
				} else if v, ok := num(s.data["stackCount"]); ok {
					stack = v
				}
				if stack <= 0 {
					continue
				}
				name := strings.ReplaceAll(b.name, " ", "")
				db.enemy.conditions["AffectedBy"+name] = true
				db.conditions["AffectedBy"+name] = true
				mult := 1.0
				var extra []Mod
				if b.typ == "AuraDebuff" {
					extra = extraAuraDebuffMods(db, cfg)
					mult = 0
					if !db.Flag("SelfAurasOnlyAffectYou") {
						mult = (1 + list.SumCfg(cfg, "INC", "AuraEffect", "BuffEffect", "DebuffEffect")/100) * list.MoreCfg(cfg, "AuraEffect", "BuffEffect", "DebuffEffect")
					}
				} else {
					mult = (1 + list.SumCfg(cfg, "INC", "DebuffEffect")/100) * list.MoreCfg(cfg, "DebuffEffect")
				}
				src := append(scaleMods(b.mods, mult*stack), scaleMods(extra, mult*stack)...)
				if s.data["stackCount"] != nil || b.stackVar != "" {
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
				res.Incomplete = append(res.Incomplete, fmt.Sprintf("%s buff not applied: %s", strings.ToLower(b.typ), b.name))
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
	// Guard buffs: a Vaal Molten Shell alone, else every Vaal guard and the
	// first other guard (in PoB's pairs order; here the order of skills).
	var slots []string
	nonVaal := false
	for _, name := range guardOrder {
		if name == "Vaal Molten Shell" {
			slots, nonVaal = []string{name}, false
			break
		} else if strings.HasPrefix(name, "Vaal") {
			slots = append(slots, name)
		} else if !nonVaal {
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
	var extra []Mod
	for _, e := range db.List(cfg, "ExtraAuraEffect") {
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

func skillInfo(skills []*activeSkill, set *pobcode.SkillSet) []SkillInfo {
	out := make([]SkillInfo, 0, len(skills))
	perGroup := map[*pobcode.SocketGroup]int{}
	for _, s := range skills {
		info := SkillInfo{Name: s.active.ge.Name, Level: s.active.level, Quality: s.active.quality, Disabled: s.disabled}
		if set != nil {
			for i := range set.Groups {
				if s.group == &set.Groups[i] {
					perGroup[s.group]++
					info.Group, info.GroupSkill = i+1, perGroup[s.group]
				}
			}
		}
		list, _ := minionList(s)
		info.HasMinion = len(list) > 0
		for _, b := range s.buffs {
			info.Buffs = append(info.Buffs, b.typ+":"+b.name)
		}
		if s.mods != nil {
			info.AuraEffectInc = s.mods.SumCfg(s.cfg, "INC", auraEffectNames...)
			info.AuraEffectMore = s.mods.MoreCfg(s.cfg, auraEffectNames...)
		}
		out = append(out, info)
	}
	return out
}

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
		if div, ok := tagNum(m.Tags[0], "div"); ok && div != 0 {
			p := power
			if limit, ok := tagNum(m.Tags[0], "limit"); ok {
				p = math.Min(p, limit)
			}
			bonus[i] = math.Floor(p / div)
		}
	}
	duration := skillDuration(list, cfg, s.data, db.enemy)
	cooldown, ok := list.OverrideCfg(cfg, "CooldownRecovery")
	if !ok {
		cd, _ := num(s.data["cooldown"])
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
func skillDuration(list *ModDB, cfg *Cfg, data map[string]any, enemy *ModDB) float64 {
	names := []string{"Duration", "PrimaryDuration"}
	if truthy(data["mineDurationAppliesToSkill"]) {
		names = append(names, "MineDuration")
	}
	mod := math.Max(calcMod(list, cfg, names...), 0)
	base, _ := num(data["duration"])
	d := (base + list.SumCfg(cfg, "BASE", "Duration", "PrimaryDuration")) * mod
	if truthy(data["debuff"]) && enemy != nil {
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
		if ms.disabled || !truthy(ms.data["enable"]) {
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
