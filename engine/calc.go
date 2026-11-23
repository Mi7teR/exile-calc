package engine

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/modparser"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// Calculator holds the game data; it is safe for concurrent use.
type Calculator struct {
	data   *gamedata.Data
	parser *modparser.Parser
	items  *itemtext.Parser
	luts   *timelessLUTs
	config configApplies
	gemIdx *gemIndex

	keystoneOnce sync.Once
	keystones    map[string]*gamedata.Node // PassiveTree.lua keystoneMap
}

func New() (*Calculator, error) {
	d, err := gamedata.PoE1()
	if err != nil {
		return nil, err
	}
	p, err := modparser.PoE1()
	if err != nil {
		return nil, err
	}
	return &Calculator{data: d, parser: p, items: itemtext.New(d), luts: &timelessLUTs{dir: defaultTimelessDir()}, gemIdx: newGemIndex(d)}, nil
}

// Result is the computed output. Unparsed lists mod lines the parser did
// not understand; UnsupportedTags counts mods left out because a tag type
// is not ported. Both make the result incomplete, never silently wrong.
type Result struct {
	Stats           map[string]float64
	DB              *ModDB
	Unparsed        []string
	Incomplete      []string // parts of the build not computed (timeless data missing, ...)
	UnsupportedTags map[string]int
	Skills          []SkillInfo   // the player's active skills, in PoB's order
	Minion          *MinionResult // the main skill's minion (env.minion)
}

// SkillInfo describes one active skill: its effective gem level and
// quality, its buffs ("Aura:Purity of Elements") and the aura effect its
// buffs get on the player (sums over AuraEffect, BuffEffect and the other
// names of CalcPerform.lua's Aura branch).
type SkillInfo struct {
	Name           string
	Level, Quality int
	Disabled       bool
	Buffs          []string
	AuraEffectInc  float64
	AuraEffectMore float64
	// Group and GroupSkill locate the skill: its socket group and its
	// index among the group's active skills (both 1-based).
	Group, GroupSkill int
	HasMinion         bool
}

// Data/Misc.lua characterConstants and Modules/Data.lua data.misc.
const (
	lifePerLevel        = 12
	manaPerLevel        = 6
	baseMaxResist       = 75
	baseEvasion         = 15
	maxResistCap        = 90
	resistFloor         = -200
	defaultAffixQuality = 0.5 // Main.lua defaultItemAffixQuality
)

var resistTypes = []string{"Fire", "Cold", "Lightning", "Chaos"}

func (c *Calculator) Calc(b *pobcode.Build) (*Result, error) {
	spec := b.ActiveSpec()
	if spec == nil {
		return nil, fmt.Errorf("engine: build has no passive tree")
	}
	tree := c.data.Tree
	if spec.ClassID < 0 || spec.ClassID >= len(tree.Classes) {
		return nil, fmt.Errorf("engine: unknown class id %d", spec.ClassID)
	}
	class := tree.Classes[spec.ClassID]
	db := NewModDB()
	db.tags = itemTags{special: c.parser.Table("itemTagSpecial"), exclusion: c.parser.Table("itemTagSpecialExclusionPattern")}
	db.build = b
	db.enemy = NewModDB()
	db.enemy.enemy = db
	db.gameIDs = c.gemIdx.gameID
	res := &Result{Stats: db.output, DB: db}
	config := b.ActiveConfig()

	// CalcSetup.lua: base values.
	db.NewMod("Str", "BASE", float64(class.BaseStr), "Base")
	db.NewMod("Dex", "BASE", float64(class.BaseDex), "Base")
	db.NewMod("Int", "BASE", float64(class.BaseInt), "Base")
	db.multipliers["Level"] = math.Max(1, math.Min(100, float64(b.Level)))
	for _, r := range resistTypes {
		db.NewMod(r+"ResistMax", "BASE", baseMaxResist, "Base")
	}
	chargeBaseMods(db)
	db.NewMod("ManaRegen", "BASE", manaRegenBase, "Base", map[string]any{"type": "PerStat", "stat": "Mana", "div": 1.0})
	db.NewMod("BlockChanceMax", "BASE", 75, "Base")      // characterConstants maximum_block_%
	db.NewMod("SpellBlockChanceMax", "BASE", 75, "Base") // base_maximum_spell_block_%
	// inherent_block_while_dual_wielding_% = 20
	db.NewMod("BlockChance", "BASE", 20, "Base", map[string]any{"type": "Condition", "var": "DualWielding"},
		map[string]any{"type": "Condition", "var": "NoInherentBlock", "neg": true},
		map[string]any{"type": "Condition", "var": "DoubledInherentDualWieldingBlock", "neg": true})
	db.NewMod("BlockChance", "BASE", 40, "Base", map[string]any{"type": "Condition", "var": "DualWielding"},
		map[string]any{"type": "Condition", "var": "NoInherentBlock", "neg": true},
		map[string]any{"type": "Condition", "var": "DoubledInherentDualWieldingBlock"})
	db.NewMod("Life", "BASE", lifePerLevel, "Base", map[string]any{"type": "Multiplier", "var": "Level", "base": 38.0})
	db.NewMod("Mana", "BASE", manaPerLevel, "Base", map[string]any{"type": "Multiplier", "var": "Level", "base": 34.0})
	db.NewMod("Evasion", "BASE", baseEvasion, "Base")
	initConditionMods(db)
	playerBaseMods(db)
	initConditionMods(db.enemy)
	penalty := -60.0
	if in, ok := configInput(config, "resistancePenalty"); ok && in.Kind == pobcode.KindNumber {
		penalty = in.Number
	}
	for _, r := range resistTypes {
		db.NewMod(r+"Resist", "BASE", penalty, "Base")
	}
	// CalcSetup.lua: bandit mods.
	switch bandit := configString(config, "bandit", b.Bandit); bandit {
	case "Alira":
		db.NewMod("ElementalResist", "BASE", 15, "Bandit")
	case "Kraityn":
		db.NewMod("MovementSpeed", "INC", 8, "Bandit")
	case "Oak":
		db.NewMod("Life", "BASE", 40, "Bandit")
	}
	// CalcSetup.lua: pantheon mods (PantheonTools.lua applySoulMod): every
	// soul of the chosen gods, named after the god's own soul.
	for _, x := range [][2]string{{"pantheonMajorGod", b.PantheonMajorGod}, {"pantheonMinorGod", b.PantheonMinorGod}} {
		god := c.data.Pantheons[configString(config, x[0], x[1])]
		if god == nil || len(god.Souls) == 0 {
			continue
		}
		for _, soul := range god.Souls {
			for _, m := range soul.Mods {
				r := c.parser.Parse(m.Line)
				if !r.Parsed() {
					continue
				}
				for _, pm := range r.Mods {
					db.Add(fromParsed(pm, "Pantheon:"+god.Souls[0].Name))
				}
			}
		}
	}

	// Passive tree (CalcSetup.lua buildModListForNodeList): node stats, mastery effects.
	allocated := map[int]bool{}
	for _, id := range spec.Nodes {
		allocated[id] = true
	}
	mastery := map[int]int{}
	for _, m := range spec.MasteryEffects {
		mastery[m.Node] = m.Effect
	}
	cluster := c.clusterGraph(spec, b, res)
	treeCounts(db, tree, spec.Nodes, cluster)
	conquered := c.conquests(spec, b, allocated, res)
	radiusFuncs := c.radiusFuncs(spec, b, allocated, res)
	// addTreeNode is buildModListForNode; with add false it only runs the
	// radius functions (PoB's extraRadiusNodeList, whose lists are dropped).
	addTreeNode := func(id int, add bool) {
		if g, ok := cluster[id]; ok {
			if !add {
				return
			}
			c.addNode(db, res, g.stats, g.incEffect, fmt.Sprintf("Tree:%d", id), nil, nil)
			return
		}
		node := tree.Nodes[id]
		if node == nil {
			return
		}
		stats := node.Stats
		source := fmt.Sprintf("Tree:%d", id)
		if node.IsMastery {
			stats = nil
			for _, e := range node.MasteryEffects {
				if e.Effect == mastery[id] {
					stats = e.Stats
				}
			}
		}
		cq, isConquered := conquered[id]
		if isConquered && !node.IsMastery {
			stats, source, _ = c.conqueredStats(node, id, cq, res)
		}
		funcs, second := radiusFuncs.byNode[id], radiusFuncs.second[id]
		mods := finishNode(c.nodeMods(res, stats, 0, source, node, funcs), add, node, second)
		if !add {
			return
		}
		for _, m := range mods {
			db.Add(m)
		}
	}
	for _, id := range spec.Nodes {
		addTreeNode(id, true)
	}

	// Items in the active set; jewels in allocated tree sockets.
	var flasks []flaskSlot
	var gear []equipped
	type reflect struct {
		slot string
		item *pobcode.Item
		it   *itemtext.Item
	}
	var kalandra []reflect
	gearMods := map[string][]modparser.Mod{} // all parsed mods of each gear slot
	db.gearMods = gearMods
	var reflected []*itemtext.Item // the other ring of each Kalandra's Touch
	useSwap := false
	if set := b.ActiveItems(); set != nil {
		useSwap = set.UseSecondWeaponSet
		type abyss struct {
			slot, parent string
			num          int
			item         *pobcode.Item
		}
		var abyssal []abyss
		ringItems := map[string]*pobcode.Item{}
		parents := map[string]*itemtext.Item{}
		jewelEffect := map[string]float64{}
		for _, slot := range set.Slots {
			if slot.ItemID == 0 {
				continue
			}
			if strings.HasPrefix(slot.Name, "Flask") {
				n, _ := strconv.Atoi(strings.TrimPrefix(slot.Name, "Flask "))
				flasks = append(flasks, flaskSlot{item: b.Item(slot.ItemID), active: slot.Active, num: n})
				continue
			}
			if skipSlot(slot.Name, set.UseSecondWeaponSet) {
				continue
			}
			if i := strings.Index(slot.Name, " Abyssal Socket "); i >= 0 {
				n, _ := strconv.Atoi(slot.Name[i+len(" Abyssal Socket "):])
				abyssal = append(abyssal, abyss{slot.Name, slot.Name[:i], n, b.Item(slot.ItemID)})
				continue
			}
			if strings.HasPrefix(slot.Name, "Ring") {
				if pit := c.peekItem(b.Item(slot.ItemID)); pit != nil && strings.Contains(pit.Name, "Kalandra's Touch") {
					// Kalandra's Touch: its own mods do not apply, except ExtraSkill.
					it, mods := c.itemMods(b.Item(slot.ItemID), res)
					for _, m := range mods {
						if m.Name == "ExtraSkill" {
							if sm, ok := slotTags(fromParsed(m, itemSource(b.Item(slot.ItemID), it)), slot.Name, slotNumber(it, slot.Name)); ok {
								db.Add(sm)
							}
						}
					}
					gear = append(gear, equipped{slot.Name, it})
					db.items[slot.Name] = it
					parents[slot.Name] = it
					kalandra = append(kalandra, reflect{slot.Name, b.Item(slot.ItemID), it})
					continue
				}
				ringItems[slot.Name] = b.Item(slot.ItemID)
			}
			it, mods := c.addItem(db, res, b.Item(slot.ItemID), 1, slot.Name)
			if it != nil {
				gearMods[slot.Name] = mods
				gear = append(gear, equipped{slot.Name, it})
				db.items[slot.Name] = it
				parents[slot.Name] = it
				for _, m := range mods {
					if m.Name == "SocketedJewelEffect" && m.Type == "INC" {
						if f, ok := num(m.Value); ok {
							jewelEffect[slot.Name] += f
						}
					}
				}
			}
		}
		// CalcSetup.lua: an abyss jewel counts only if its parent has that
		// many abyssal sockets, scaled by the parent's socketed jewel effect.
		// CalcSetup.lua: Kalandra's Touch takes the other ring's mods, except
		// SocketedIn ones, with its own source.
		for _, k := range kalandra {
			other := "Ring 1"
			if k.slot == "Ring 1" {
				other = "Ring 2"
			}
			oi := ringItems[other]
			if oi == nil {
				continue
			}
			oit, omods := c.itemMods(oi, &Result{})
			if oit == nil {
				continue
			}
			source := itemSource(k.item, k.it)
		mods:
			for _, m := range omods {
				for _, t := range m.Tags {
					if tagStr(t, "type") == "SocketedIn" {
						continue mods
					}
				}
				if sm, ok := slotTags(fromParsed(m, source), k.slot, slotNumber(k.it, k.slot)); ok {
					db.Add(sm)
				}
			}
			reflected = append(reflected, oit)
		}
		for _, a := range abyssal {
			parent := parents[a.parent]
			if parent == nil || abyssalSockets(parent) < a.num {
				continue
			}
			if it, _ := c.addItem(db, res, a.item, 1+jewelEffect[a.parent]/100, ""); it != nil {
				db.items[a.slot] = it
				abyssCounts(db, it, a.parent)
			}
		}
	}
	corruptedEffect := c.adornedEffect(spec, b, allocated, res)
	if v, ok := corruptedEffect[itemtext.Magic]; ok {
		db.multipliers["CorruptedMagicJewelEffect"] = v
	}
	if v, ok := corruptedEffect[itemtext.Rare]; ok {
		db.multipliers["CorruptedRareJewelEffect"] = v
	}
	for _, s := range spec.Sockets {
		if s.ItemID != 0 && allocated[s.NodeID] {
			scale := 1.0
			if it := c.peekItem(b.Item(s.ItemID)); it != nil && it.Corrupted && it.Base != nil &&
				it.Base.Type == "Jewel" && it.Base.SubType != "Charm" {
				scale += corruptedEffect[it.Rarity]
			}
			if it, _ := c.addItem(db, res, b.Item(s.ItemID), scale, ""); it != nil {
				db.items["Jewel "+strconv.Itoa(s.NodeID)] = it
				abyssCounts(db, it, "")
			}
		}
	}

	granted := grantedNodes(db, tree, class.Name, allocated)
	for _, id := range granted {
		addTreeNode(id, true)
	}
	// buildModListForNodeList, finishJewels: unallocated nodes in a radius,
	// then the radius jewels' finalisers.
	isGranted := map[int]bool{}
	for _, id := range granted {
		isGranted[id] = true
	}
	extra := make([]int, 0, len(radiusFuncs.inRadius))
	for id := range radiusFuncs.inRadius {
		if !allocated[id] && !isGranted[id] {
			extra = append(extra, id)
		}
	}
	sort.Ints(extra)
	for _, id := range extra {
		addTreeNode(id, false)
	}
	for _, final := range radiusFuncs.finals {
		for _, m := range final() {
			db.Add(m)
		}
	}
	itemConditions(db, gear)
	c.socketCounts(db, b, gear, config)
	// Kalandra's Touch: influence and corruption multipliers of the other ring.
	for _, o := range reflected {
		for mult, infl := range influenceMultipliers {
			if hasInfluence(o, infl) {
				db.multipliers[mult]++
				db.multipliers["Non"+mult]--
			}
		}
		if o.Corrupted {
			db.multipliers["CorruptedItem"]++
			db.multipliers["NonCorruptedItem"]--
		}
		if hasInfluence(o, "shaper") || hasInfluence(o, "elder") {
			db.multipliers["ShaperOrElderItem"]++
		}
		db.multipliers[strings.ReplaceAll(o.BaseName, " ", "")+"Equipped"]++
	}
	buffMode(db)
	buffMode(db.enemy)
	c.applyConfig(db, res, config)
	db.enemyEnv = newEnemyEnv(config, c.data.Config, b.Level, c.data.Monster, res)
	db.enemy.NewMod("Accuracy", "BASE", enemyAccuracy(c.data.Monster, db.enemyEnv.level), "Base")
	c.customMods(db, res, config)
	shrineBaseMods(db)
	skills := c.setupSkills(db, b, useSwap, res)
	for _, s := range skills {
		c.buildSkillMods(db, s, res)
	}
	if debugSkills != nil {
		debugSkills(skills)
	}
	keystones := map[string]bool{}
	flaskSt := c.applyFlasks(db, res, flasks)
	c.mergeKeystones(db, keystones, res)
	banners(db)
	warcryPower(db)
	playerEnemyMods := enemyModifiers{}
	playerEnemyMods.apply(db)
	// CalcPerform.lua: "gain maximum life instead of maximum energy shield
	// from equipped armour items".
	if db.Flag("ConvertArmourESToLife") {
		for _, slot := range esToLifeSlots {
			if it := db.items[slot]; it != nil {
				if es := itemArmour(it, gearMods[slot]).EnergyShield; es > 0 {
					db.NewMod("Life", "BASE", es, slot+" ES to Life Conversion")
				}
			}
		}
	}
	damageToAuraEffect(db)
	combatConditions(db, mainSkill(skills))
	c.attributes(db)
	shrineBuffs(db)
	c.lifeMana(db)
	reservations(db, skills, res)
	totemCounts(db, skills)
	c.minionCounts(db, skills)
	heraldAuraCounts(db, skills)
	chilling := chillSkills(db, skills)
	charges(db) // early, for stats that depend on charges
	curses, buffs := c.applyBuffs(db, skills, res)
	curses = c.extraCurses(db, curses, res)
	c.applyCurses(db, skills, curses, res)
	extraAuras(db)
	guaranteedChills(db, chilling)
	impaleStacks(db)
	c.mergeKeystones(db, keystones, res)
	defenceForConditionals(db, gearMods)
	charges(db)
	flaskRecovery(db, flaskSt)
	actorMisc(db, mainSkill(skills), flaskSt.silver, flaskSt.silverInc)
	rage(db, mainCfg(skills))
	playerEnemyMods.apply(db)
	enemyAilments(db, skills)
	actorMisc(db.enemy, nil, false, -100)
	exposures(db)
	res.Skills = skillInfo(skills, b.ActiveSkills())
	c.allyLifeMods(db, skills, buffs, b.Level, res)
	c.resistances(db)
	c.defence(db, gearMods, keystones)
	avoidanceAndRecovery(db)
	recoup(db)
	buildDefenceEstimations(db)
	res.Minion = c.mainMinion(db, skills, buffs, b.Level, res)
	for k, v := range db.UnsupportedTags {
		if res.UnsupportedTags == nil {
			res.UnsupportedTags = map[string]int{}
		}
		res.UnsupportedTags[k] = v
	}
	return res, nil
}

// skipSlot leaves out swap weapons (unless the swap set is active) and
// flasks, whose effects depend on being active.
func skipSlot(name string, useSwap bool) bool {
	isSwap := strings.Contains(name, "Swap")
	switch {
	case strings.HasPrefix(name, "Flask"):
		return true
	case useSwap:
		return strings.HasPrefix(name, "Weapon") && !isSwap
	default:
		return isSwap
	}
}

func configInput(c *pobcode.ConfigSet, name string) (pobcode.Input, bool) {
	if c == nil {
		return pobcode.Input{}, false
	}
	return c.Input(name)
}

func configString(c *pobcode.ConfigSet, name, def string) string {
	if in, ok := configInput(c, name); ok && in.Kind == pobcode.KindString {
		return in.String
	}
	return def
}

// treeCounts ports the allocated passive counts of CalcSetup.lua
// (Multiplier:AllocatedNotable, Keystone, Mastery, MasteryType, LifeMastery).
func treeCounts(db *ModDB, tree *gamedata.Tree, nodes []int, cluster map[int]clusterNode) {
	var notables, keystones, masteries float64
	types := map[string]float64{}
	for _, id := range nodes {
		if g, ok := cluster[id]; ok {
			if g.notable {
				notables++
			} else if g.keystone {
				keystones++
			}
			continue
		}
		n := tree.Nodes[id]
		if n == nil {
			continue
		}
		switch nodeType(n) {
		case "Mastery":
			masteries++
			types[n.Name]++
		case "Notable":
			notables++
		case "Keystone":
			keystones++
		}
	}
	add := func(name string, v float64) {
		if v > 0 {
			db.NewMod("Multiplier:"+name, "BASE", v, "")
		}
	}
	add("AllocatedNotable", notables)
	add("AllocatedKeystone", keystones)
	add("AllocatedMastery", masteries)
	add("AllocatedMasteryType", float64(len(types)))
	add("AllocatedLifeMastery", types["Life Mastery"])
}

// nodeLines ports the line handling of PassiveTreeClass:ProcessStats: stat
// strings are split on newlines, and a line that does not parse alone is
// parsed together with the lines after it.
func (c *Calculator) nodeLines(stats []string, res *Result) []modparser.Mod {
	var lines []string
	for _, s := range stats {
		for _, l := range strings.Split(s, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				lines = append(lines, l)
			}
		}
	}
	var mods []modparser.Mod
	for i := 0; i < len(lines); i++ {
		r := c.parser.Parse(lines[i])
		if !r.Parsed() {
			comb := lines[i]
			done := false
			for j := i + 1; j < len(lines); j++ {
				comb += " " + lines[j]
				if rc := c.parser.Parse(comb); rc.Parsed() {
					mods = append(mods, rc.Mods...)
					i, done = j, true
					break
				}
			}
			if !done {
				res.Unparsed = append(res.Unparsed, lines[i])
			}
			continue
		}
		mods = append(mods, r.Mods...)
	}
	return mods
}

// addNode adds one passive's mods, scaled by its PassiveSkillEffect
// (CalcSetup.lua buildModListForNode: calcLib.mod(modList, nil, "PassiveSkillEffect")).
// incEffect is the cluster jewel's "increased Effect" of small passives.
func (c *Calculator) addNode(db *ModDB, res *Result, stats []string, incEffect float64, source string, tn *gamedata.Node, funcs []radiusFunc) {
	for _, m := range finishNode(c.nodeMods(res, stats, incEffect, source, tn, funcs), true, tn, nil) {
		db.Add(m)
	}
}

// finishNode is the rest of buildModListForNode: a node that "grants
// nothing" loses its mods, the rest is scaled by passive skill effect, then
// the second-pass radius functions see the result (Self on allocated
// nodes, SelfUnalloc on unallocated ones).
func finishNode(mods []Mod, allocated bool, tn *gamedata.Node, second []secondPassFunc) []Mod {
	node := NewModDB()
	for _, m := range mods {
		node.Add(m)
	}
	if node.Flag("PassiveSkillHasNoEffect") || (allocated && node.Flag("AllocatedPassiveSkillHasNoEffect")) {
		mods = nil
	}
	scale := (1 + node.Sum("INC", "PassiveSkillEffect")/100) * node.More("PassiveSkillEffect")
	out := make([]Mod, 0, len(mods))
	for _, m := range mods {
		out = append(out, scaleMod(m, scale))
	}
	if tn != nil && nodeType(tn) != "Mastery" {
		for _, f := range second {
			if f.unalloc != allocated {
				f.f(tn, out)
			}
		}
	}
	return out
}

// nodeMods is a node's own mods plus what the first-pass radius functions add.
func (c *Calculator) nodeMods(res *Result, stats []string, incEffect float64, source string, tn *gamedata.Node, funcs []radiusFunc) []Mod {
	var mods []Mod
	for _, m := range c.nodeLines(stats, res) {
		mods = append(mods, fromParsed(m, source))
	}
	if incEffect != 0 {
		mods = append(mods, Mod{Name: "PassiveSkillEffect", Type: "INC", Value: incEffect, Source: source})
	}
	// First pass radius jewels ("Other" functions): conversions read the
	// node's own mods, collectors the list built so far.
	// Mastery nodes are skipped (radNode.type ~= "Mastery").
	if tn != nil && nodeType(tn) != "Mastery" {
		own := append([]Mod(nil), mods...)
		for _, f := range funcs {
			mods = append(mods, f(tn, own, mods)...)
		}
	}
	return mods
}

// localArmour names the mods ItemClass:BuildModListForSlotNum takes out of
// an armour item's list with calcLocal: they make the item's own defences.
var localArmour = map[string]string{
	"Armour": "BASE INC", "Evasion": "BASE INC", "EnergyShield": "BASE INC", "Ward": "BASE INC",
	"ArmourAndEvasion": "BASE INC", "ArmourAndEnergyShield": "BASE INC", "EvasionAndEnergyShield": "BASE INC",
	"Defences": "INC", "AlternateQualityArmour": "BASE",
}

// isLocalArmourMod is calcLocal's selection: no flags or keyword flags and
// no tag but a leading InSlot. Shields also take their block chance.
func isLocalArmourMod(m modparser.Mod, shield bool) bool {
	types, ok := localArmour[m.Name]
	if m.Name == "BlockChance" && shield {
		types, ok = "BASE INC", true
	}
	if !ok || !strings.Contains(types, m.Type) || m.Flags != 0 || m.KeywordFlags != 0 {
		return false
	}
	return len(m.Tags) == 0 || tagStr(m.Tags[0], "type") == "InSlot"
}

// localWeapon are the mods the weapon branch of BuildModListForSlotNum
// consumes with calcLocal (name -> "TYPE:flags"), so they leave the
// item's mod list.
var localWeapon = func() map[string]string {
	m := map[string]string{
		"Speed": "INC:1", "AlternateQualityLocalAttackSpeedPer8Quality": "INC:0",
		"WeaponRange": "BASE:0", "WeaponRangeMetre": "BASE:0", "AlternateQualityLocalWeaponRangePer10Quality": "BASE:0",
		"LocalElementalDamage": "INC:0", "PhysicalDamage": "INC:0", "AlternateQualityWeapon": "BASE:0",
		"AlternateQualityLocalCritChancePer4Quality": "INC:0",
	}
	for _, t := range []string{"Physical", "Lightning", "Cold", "Fire", "Chaos"} {
		m[t+"Min"], m[t+"Max"] = "BASE:0", "BASE:0"
	}
	for _, t := range []string{"Lightning", "Cold", "Fire"} {
		m["Local"+t+"Damage"] = "INC:0"
	}
	return m
}()

// isLocalWeaponMod reports whether calcLocal consumes m on a weapon.
func isLocalWeaponMod(m modparser.Mod) bool {
	if m.KeywordFlags != 0 || (len(m.Tags) > 0 && tagStr(m.Tags[0], "type") != "InSlot") {
		return false
	}
	if m.Name == "CritChance" && (m.Type == "BASE" || m.Type == "INC") && m.Flags == 0 {
		return true
	}
	return localWeapon[m.Name] == fmt.Sprintf("%s:%d", m.Type, m.Flags)
}

// Item.lua catalystList and catalystTags: a catalyst scales the lines that
// carry one of its tags by (100 + quality) / 100.
var catalystTags = map[string][]string{
	"Abrasive": {"attack"}, "Accelerating": {"speed"}, "Dextral": {"suffix"},
	"Fertile": {"life", "mana", "resource"}, "Imbued": {"caster"},
	"Intrinsic": {"jewellery_attribute", "attribute"}, "Noxious": {"physical_damage", "chaos_damage"},
	"Prismatic": {"jewellery_resistance", "resistance"}, "Sinistral": {"prefix"},
	"Tempering": {"jewellery_defense", "defences", "armour", "evasion", "energyshield"},
	"Turbulent": {"jewellery_elemental", "elemental_damage"}, "Unstable": {"critical"},
}

// catalystScalar ports getCatalystScalar (Classes/Item.lua).
func catalystScalar(it *itemtext.Item, l itemtext.ModLine) float64 {
	tags := catalystTags[it.Catalyst]
	if tags == nil || l.Unscalable || len(l.Tags) == 0 {
		return 1
	}
	q := it.CatalystQuality
	if q == 0 {
		q = 20
	}
	for _, want := range tags {
		for _, have := range l.Tags {
			if have == want {
				return float64(100+q) / 100
			}
		}
	}
	return 1
}

// itemLines returns an item's active mod lines with rolls and catalyst
// scaling applied. ModRange ids count lines in ItemsTab.lua's save order.
func (c *Calculator) itemLines(item *pobcode.Item, res *Result) (*itemtext.Item, []string) {
	it, err := c.items.Parse(item.Raw)
	if err != nil {
		res.Unparsed = append(res.Unparsed, "item: "+err.Error())
		return nil, nil
	}
	if item.Variant > 0 {
		it.Variant = item.Variant
	}
	for i, v := range item.VariantAlt { // ItemsTab: variantAlt.. attributes
		if v > 0 {
			it.VariantAlt[i] = v
		}
	}
	ranges := map[int]float64{}
	for _, mr := range item.ModRanges {
		ranges[mr.ID] = mr.Range
	}
	type entry struct {
		kind   string // enchant, scourge, implicit, explicit, crucible
		line   itemtext.ModLine
		r      float64
		scalar float64
	}
	var entries []entry
	id := 0
	for gi, group := range [][]itemtext.ModLine{it.Enchants, it.Scourge, it.Implicits, it.Explicits, it.Crucible} {
		kind := []string{"enchant", "scourge", "implicit", "explicit", "crucible"}[gi]
		for _, l := range group {
			id++
			if !it.LineActive(l) {
				continue
			}
			r := defaultAffixQuality
			if l.HasRange {
				r = l.Range
			}
			if v, ok := ranges[id]; ok {
				r = v
			}
			entries = append(entries, entry{kind, l, r, catalystScalar(it, l)})
		}
	}
	// Item.lua modMagnitudeMods: "N% increased <tags> modifier magnitudes"
	// lines scale the matching lines of crafted or advanced-copy items.
	if it.CraftedItem || it.AdvancedCopy {
		ml := make([]magnitudeLine, len(entries))
		for i, e := range entries {
			ml[i] = magnitudeLine{applyRange(e.line.Text, e.r, e.scalar), e.line.Disabled}
		}
		for _, mm := range magnitudeMods(ml) {
			for i := range entries {
				e := &entries[i]
				if mm.modType != "" && mm.modType != e.kind || mm.modType == "" && e.kind != "implicit" && e.kind != "explicit" && e.kind != "enchant" {
					continue
				}
				if e.line.Unscalable || c.grantsSkill(e.line.Text) || !magnitudeMatch(mm, e.line) {
					continue
				}
				if mm.multiplier != 0 {
					e.scalar *= mm.multiplier
				} else {
					e.scalar += mm.quality / 100
				}
			}
		}
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = applyRange(e.line.Text, e.r, e.scalar)
	}
	return it, lines
}

type magnitudeLine struct {
	text     string
	disabled bool
}

type magnitudeMod struct {
	tags, anyTags []string
	quality       float64
	multiplier    float64
	modType       string
}

var (
	magnitudeRes = []*regexp.Regexp{
		regexp.MustCompile(`(\d+)% ([ir][ne][cd][ru][ec][ae][sd]e?d?) ?([a-z\s]*) modifier magnitudes`),
		regexp.MustCompile(`(\d+)% ([ir][ne][cd][ru][ec][ae][sd]e?d?) effect of ([sp][ur][fe]fix)es`),
		regexp.MustCompile(`([a-z\s]*) modifier magnitudes are doubled`),
	}
	magnitudeTagMap = map[string]string{"defence": "defences", "enchantment": "enchant"}
)

// magnitudeMods reads the modifier magnitude lines of an item.
func magnitudeMods(lines []magnitudeLine) []magnitudeMod {
	var out []magnitudeMod
	for _, l := range lines {
		if l.disabled {
			continue
		}
		low := strings.ToLower(l.text)
		for pi, re := range magnitudeRes {
			m := re.FindStringSubmatch(low)
			if m == nil {
				continue
			}
			amount, dir, tagsText, mult := m[1], "", "", 0.0
			if pi == 2 {
				tagsText, amount, dir, mult = m[1], "100", "increased", 2
			} else {
				dir, tagsText = m[2], m[3]
			}
			if dir != "increased" && dir != "reduced" {
				continue
			}
			q, _ := strconv.ParseFloat(amount, 64)
			if dir == "reduced" {
				q = -q
			}
			if tagsText == "explicit physical and chaos damage" {
				out = append(out, magnitudeMod{tags: []string{"damage"}, anyTags: []string{"physical", "chaos"}, quality: q, modType: "explicit"})
				break
			}
			mm := magnitudeMod{quality: q, multiplier: mult}
			for _, w := range strings.Fields(tagsText) {
				if t, ok := magnitudeTagMap[w]; ok {
					w = t
				}
				switch w {
				case "implicit", "explicit", "enchant":
					mm.modType = w
				default:
					mm.tags = append(mm.tags, w)
				}
			}
			out = append(out, mm)
			break
		}
	}
	return out
}

func magnitudeMatch(mm magnitudeMod, l itemtext.ModLine) bool {
	has := map[string]bool{"prefix": l.Prefix, "suffix": l.Suffix, "unveiled": l.Unveiled}
	for _, t := range l.Tags {
		has[t] = true
	}
	for _, t := range mm.tags {
		if !has[t] {
			return false
		}
	}
	return mm.anyTags == nil || has[mm.anyTags[0]] || has[mm.anyTags[1]]
}

// grantsSkill: modifiers that grant skills are not affected by magnitudes.
func (c *Calculator) grantsSkill(text string) bool {
	r := c.parser.Parse(text)
	for _, m := range r.Mods {
		if m.Name == "ExtraSkill" {
			return true
		}
	}
	if r.Extra != "" {
		l := strings.ToLower(text)
		return grantsLevelRe.MatchString(l) || grantsNameRe.MatchString(l)
	}
	return false
}

var (
	grantsLevelRe = regexp.MustCompile(`^grants level \d+ `)
	grantsNameRe  = regexp.MustCompile(`^grants \D+$`)
)

// itemMods parses an item's active mod lines.
func (c *Calculator) itemMods(item *pobcode.Item, res *Result) (*itemtext.Item, []modparser.Mod) {
	it, lines := c.itemLines(item, res)
	if it == nil {
		return nil, nil
	}
	var mods []modparser.Mod
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		parsed := c.parser.Parse(line)
		// ItemClass:ParseRaw: a line that does not parse is tried together
		// with the next one (two-line mods such as cluster jewel enchants
		// "Added Small Passive Skills grant: ...").
		if !parsed.Parsed() && i+1 < len(lines) {
			if comb := c.parser.Parse(line + " " + lines[i+1]); comb.Parsed() {
				parsed = comb
				i++
			} else if !isRadiusFuncLine(line) && isRadiusFuncLine(line+" "+lines[i+1]) {
				i++ // a two-line radius jewel function (radius.go)
				continue
			}
		}
		if !parsed.Parsed() {
			if !isRadiusFuncLine(line) {
				res.Unparsed = append(res.Unparsed, line)
			}
			continue
		}
		mods = append(mods, parsed.Mods...)
	}
	return it, mods
}

// itemSource is PoB's modSource: "Item:<id>:<name>", where the name of a
// rare or unique is "<title>, <base>" and a normal or magic name includes
// the base already.
func itemSource(item *pobcode.Item, it *itemtext.Item) string {
	if it.Rarity == itemtext.Normal || it.Rarity == itemtext.Magic {
		return fmt.Sprintf("Item:%d:%s", item.ID, it.Name)
	}
	return fmt.Sprintf("Item:%d:%s, %s", item.ID, it.Name, it.BaseName)
}

// addItem adds an item's global mods, scaled (abyss jewels), and returns
// the parsed item and all its mods.
func (c *Calculator) addItem(db *ModDB, res *Result, item *pobcode.Item, scale float64, slot string) (*itemtext.Item, []modparser.Mod) {
	if item == nil {
		return nil, nil
	}
	it, mods := c.itemMods(item, res)
	if it == nil {
		return nil, nil
	}
	source := itemSource(item, it)
	// CalcSetup.lua: set conditions on restricted items.
	if cr := it.ClassRestriction(); cr != "" {
		db.classConds[strings.ReplaceAll(it.Name, " ", "")] = cr
	}
	isArmour := it.Base != nil && it.Base.Armour != nil
	isWeapon := it.Base != nil && it.Base.Weapon != nil
	for _, m := range mods {
		if isArmour && isLocalArmourMod(m, it.Base.Armour.BlockChance != 0) {
			continue // local to the item: part of its own defence values
		}
		if isWeapon && isLocalWeaponMod(m) {
			continue // local weapon stats: part of the weapon's own data
		}
		if sm, ok := slotTags(fromParsed(m, source), slot, slotNumber(it, slot)); ok {
			db.Add(scaleMod(sm, scale))
		}
	}
	// Item.lua: each Grand Spectrum counts for itself and for minions.
	if it.Base != nil && it.Base.Type == "Jewel" && strings.Contains(it.Name, "Grand Spectrum") {
		spectrum := Mod{Name: "Multiplier:GrandSpectrum", Type: "BASE", Value: 1.0, Source: it.Name}
		db.Add(scaleMod(spectrum, scale))
		db.Add(Mod{Name: "MinionModifier", Type: "LIST", Value: map[string]any{"mod": modToMap(spectrum)}, Source: it.Name})
	}
	if isArmour && it.Base.Armour.MovementPenalty != 0 {
		db.Add(Mod{Name: "MovementSpeed", Type: "INC", Value: -it.Base.Armour.MovementPenalty, Source: source,
			Tags: []map[string]any{{"type": "Condition", "var": "IgnoreMovementPenalties", "neg": true}}})
	}
	if slot != "" {
		name := strings.Replace(slot, " Swap", "", 1)
		socketMultipliers(db, it, name)
		db.NewMod("Multiplier:QualityOn"+name, "BASE", float64(it.Quality), "Quality")
	}
	return it, mods
}

// mergeKeystones is modLib.mergeKeystones: a Keystone mod (an item line
// such as "Blood Magic") adds that keystone's node mods once, with the
// source of the line unless it comes from the tree. PoB runs it at the
// start of calcs.perform, after flasks and after buffs.
func (c *Calculator) mergeKeystones(db *ModDB, added map[string]bool, res *Result) {
	c.keystoneOnce.Do(func() {
		c.keystones = map[string]*gamedata.Node{}
		for _, n := range c.data.Tree.Nodes {
			if n.IsKeystone && n.Group != 0 {
				c.keystones[n.Name] = n
				c.keystones[strings.ToLower(n.Name)] = n
			}
		}
	})
	for _, e := range db.List(nil, "Keystone") {
		name, _ := e.Value.(string)
		node := c.keystones[name]
		if added[name] || node == nil {
			continue
		}
		added[name] = true
		source := fmt.Sprintf("Tree:%d", node.ID)
		if e.Mod.Source != "" && !strings.Contains(strings.ToLower(e.Mod.Source), "tree") {
			source = e.Mod.Source
		}
		for _, m := range c.nodeLines(node.Stats, res) {
			db.Add(fromParsed(m, source))
		}
	}
}

// socketMultipliers ports the socket part of ItemClass:BuildModListForSlotNum:
// a multiplier per socket colour and the number of unlinked sockets.
func socketMultipliers(db *ModDB, it *itemtext.Item, slot string) {
	if len(it.Sockets) == 0 {
		return
	}
	names := map[byte]string{'R': "Red", 'G': "Green", 'B': "Blue", 'W': "White"}
	groups := map[int]int{}
	for _, s := range it.Sockets {
		groups[s.Group]++
		if n, ok := names[s.Color]; ok {
			db.NewMod("Multiplier:"+n+"SocketIn"+slot, "BASE", 1, "Item Sockets")
		}
	}
	unlinked := 0
	for _, n := range groups {
		if n == 1 {
			unlinked++
		}
	}
	db.NewMod("Multiplier:UnlinkedSocketIn"+slot, "BASE", float64(unlinked), "Unlinked Item Sockets")
}

// slotTags is the tag substitution of ItemClass:BuildModListForSlotNum:
// {SlotName}, {Hand} and {OtherSlotNum} for the slot the item is in.
// slotNumber is the slotNum ItemClass:BuildModList builds a list for:
// weapons and rings have one per slot, other items none (0).
func slotNumber(it *itemtext.Item, slot string) int {
	if it == nil || it.Base == nil || (it.Base.Weapon == nil && it.Base.Type != "Ring") {
		return 0
	}
	slot = strings.Replace(slot, " Swap", "", 1)
	if i := strings.LastIndex(slot, " "); i >= 0 {
		n, _ := strconv.Atoi(slot[i+1:])
		return n
	}
	return 0
}

// slotTags is the tag handling of ItemClass:BuildModListForSlotNum: mods
// for another slot number (SlotNumber, InSlot) are dropped, and
// {SlotName}, {Hand} and {OtherSlotNum} are filled in.
func slotTags(m Mod, slot string, slotNum int) (Mod, bool) {
	if slot == "" {
		return m, true
	}
	for _, t := range m.Tags {
		if typ := tagStr(t, "type"); typ == "SlotNumber" || typ == "InSlot" {
			if n, _ := tagNum(t, "num"); int(n) != slotNum {
				return m, false
			}
		}
	}
	slot = strings.Replace(slot, " Swap", "", 1)
	hand, other := "OffHand", "1"
	if slotNum == 1 {
		hand, other = "MainHand", "2"
	}
	r := strings.NewReplacer("{SlotName}", slot, "{Hand}", hand, "{OtherSlotNum}", other)
	var tags []map[string]any
	for i, t := range m.Tags {
		var nt map[string]any
		for k, v := range t {
			if sv, ok := v.(string); ok && strings.Contains(sv, "{") {
				if nt == nil {
					nt = copyMap(t)
				}
				nt[k] = r.Replace(sv)
			}
		}
		if nt != nil {
			if tags == nil {
				tags = append([]map[string]any(nil), m.Tags...)
			}
			tags[i] = nt
		}
	}
	if tags != nil {
		m.Tags = tags
	}
	return m, true
}

func abyssalSockets(it *itemtext.Item) int {
	n := 0
	for _, s := range it.Sockets {
		if s.Color == 'A' {
			n++
		}
	}
	return n
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// val is calcLib.val (Modules/CalcTools.lua).
func val(db *ModDB, name string) float64 {
	base := db.Sum("BASE", name)
	if base == 0 {
		return 0
	}
	return base * (1 + db.Sum("INC", name)/100) * db.More(name)
}

// attributes ports calculateAttributes and the attribute bonuses of
// doActorAttribsConditions (Modules/CalcPerform.lua).
func (c *Calculator) attributes(db *ModDB) {
	out := db.output
	for pass := 0; pass < 2; pass++ { // twice: conditions depend on the attributes
		for _, s := range []string{"Str", "Dex", "Int"} {
			out[s] = math.Max(round(val(db, s), 0), 0)
		}
		str, dex, int_ := out["Str"], out["Dex"], out["Int"]
		cond := db.conditions
		cond["StrHigherThanInt"] = str > int_
		cond["StrHigherThanDex"] = str > dex
		cond["IntHigherThanStr"] = int_ > str
		cond["DexHigherThanStr"] = dex > str
		cond["IntHigherThanDex"] = int_ > dex
		cond["DexHigherThanInt"] = dex > int_
		cond["StrHighestAttribute"] = str >= dex && str >= int_
		cond["IntHighestAttribute"] = int_ >= str && int_ >= dex
		cond["DexHighestAttribute"] = dex >= str && dex >= int_
	}
	out["TotalAttr"] = out["Str"] + out["Dex"] + out["Int"]
	out["Devotion"] = db.Sum("BASE", "Devotion") // CalcPerform.lua: special case for Devotion
	if db.Flag("NoAttributeBonuses") {
		return
	}
	if !db.Flag("NoStrengthAttributeBonuses") {
		if !db.Flag("NoStrBonusToLife") {
			db.NewMod("Life", "BASE", math.Floor(out["Str"]/2), "Strength")
		}
		bonus := out["Str"] + db.Sum("BASE", "DexIntToMeleeBonus")
		if r := db.Sum("BASE", "StrDmgBonusRatioOverride"); r > 0 {
			db.strDmgBonus = math.Floor(bonus * r)
		} else {
			db.strDmgBonus = math.Floor(bonus / 5)
		}
		db.Add(Mod{Name: "PhysicalDamage", Type: "INC", Value: db.strDmgBonus, Flags: mfMelee, Source: "Strength"})
	}
	if !db.Flag("NoDexterityAttributeBonuses") {
		perDex, ok := db.Override("DexAccBonusOverride")
		if !ok {
			perDex = 2 // data.misc.AccuracyPerDexBase
		}
		db.NewMod("Accuracy", "BASE", out["Dex"]*perDex, "Dexterity")
		if !db.Flag("NoDexBonusToEvasion") {
			db.NewMod("Evasion", "INC", math.Floor(out["Dex"]/5), "Dexterity")
		}
	}
	if !db.Flag("NoIntelligenceAttributeBonuses") {
		if !db.Flag("NoIntBonusToMana") {
			db.NewMod("Mana", "BASE", math.Floor(out["Int"]/2), "Intelligence")
		}
		if !db.Flag("NoIntBonusToES") {
			db.NewMod("EnergyShield", "INC", math.Floor(out["Int"]/10), "Intelligence")
		}
	}
}

// lifeMana ports doActorLifeMana (Modules/CalcPerform.lua).
func (c *Calculator) lifeMana(db *ModDB) {
	out := db.output
	out["LowLifePercentage"] = 100 * lowPoolThreshold
	if v := db.Sum("BASE", "LowLifePercentage"); v > 0 {
		out["LowLifePercentage"] = 100 * v
	}
	out["FullLifePercentage"] = 100
	if v := db.Sum("BASE", "FullLifePercentage"); v > 0 {
		out["FullLifePercentage"] = 100 * v
	}
	if db.Flag("ChaosInoculation") {
		out["Life"] = 1
		db.conditions["FullLife"] = true
	} else if o, ok := db.Override("Life"); ok {
		out["Life"] = o
	} else {
		base := db.Sum("BASE", "Life")
		inc := db.Sum("INC", "Life")
		more := db.More("Life")
		conv := db.Sum("BASE", "LifeConvertToEnergyShield")
		out["Life"] = math.Max(round(base*(1+inc/100)*more*(1-conv/100), 0), 1)
	}
	manaConv := db.Sum("BASE", "ManaConvertToArmour")
	out["Mana"] = round(val(db, "Mana")*(1-manaConv/100), 0)
}

// resistances ports calcs.resistances (Modules/CalcDefence.lua) without
// resistance conversion and Melding of the Flesh.
func (c *Calculator) resistances(db *ModDB) {
	out := db.output
	resistMax := func(elem string) float64 {
		maxNames := []string{elem + "ResistMax"}
		if elem != "Chaos" {
			maxNames = append(maxNames, "ElementalResistMax")
		}
		if max, ok := db.Override(elem + "ResistMax"); ok {
			return max
		}
		return math.Min(maxResistCap, db.Sum("BASE", maxNames...))
	}
	// Resistance conversion: non-base max resistances, then resistances.
	sumNonBase := func(name, typ string) float64 {
		total := 0.0
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if m.Source == "Base" || !eligible(m, typ, nil) {
				continue
			}
			if v, ok := db.eval(m, nil); ok {
				if f, isNum := num(v); isNum {
					total += f
				}
			}
		}
		return total
	}
	for _, from := range resistTypes {
		for _, to := range resistTypes {
			if rate := db.Sum("BASE", from+"MaxResConvertTo"+to) / 100; rate != 0 {
				if max := sumNonBase(from+"ResistMax", "BASE"); max != 0 {
					db.NewMod(to+"ResistMax", "BASE", max*rate, from+" To "+to+" Max Resistance Conversion")
				}
			}
		}
	}
	for _, from := range resistTypes {
		for _, to := range resistTypes {
			rate := db.Sum("BASE", from+"ResConvertTo"+to) / 100
			if rate == 0 {
				continue
			}
			if res := sumNonBase(from+"Resist", "BASE"); res != 0 {
				db.NewMod(to+"Resist", "BASE", res*rate, from+" To "+to+" Resistance Conversion")
			}
			for _, typ := range []string{"INC", "MORE"} {
				for _, r := range db.tabulateType(from+"Resist", typ) {
					if f, ok := num(r.Value); ok {
						db.NewMod(to+"Resist", typ, f*rate, r.Source)
					}
				}
			}
		}
	}
	// Melding of the Flesh: every elemental maximum becomes the highest one.
	if db.Flag("ElementalResistMaxIsHighestResistMax") {
		highest, highestType := 0.0, ""
		for _, elem := range resistTypes[:3] {
			if m := resistMax(elem); m > highest {
				highest, highestType = m, elem
			}
		}
		for _, elem := range resistTypes[:3] {
			db.NewMod(elem+"ResistMax", "OVERRIDE", highest, highestType+" Melding of the Flesh")
		}
	}
	for _, elem := range resistTypes {
		names := []string{elem + "Resist"}
		if elem != "Chaos" {
			names = append(names, "ElementalResist")
		}
		max := resistMax(elem)
		total, ok := db.Override(elem + "Resist")
		dotTotal := total
		if !ok {
			base := db.Sum("BASE", names...)
			inc := math.Max((1+db.Sum("INC", names...)/100)*db.More(names...), 0)
			total = base * inc
			dotTotal = db.SumCfg(&Cfg{Flags: mfDot}, "BASE", names...) * inc
		}
		total, max, dotTotal = modfInt(total), modfInt(max), modfInt(dotTotal)
		final := math.Max(math.Min(total, max), resistFloor)
		out[elem+"ResistOverTime"] = math.Max(math.Min(dotTotal, max), resistFloor)
		out[elem+"Resist"] = final
		out[elem+"ResistTotal"] = total
		out[elem+"ResistOverCap"] = math.Max(0, total-max)
	}
}

// customMods ports the Custom Modifiers of ConfigTabClass:BuildModList:
// each line of each enabled block is parsed; fully parsed lines apply with
// the source "Custom:<block title>".
func (c *Calculator) customMods(db *ModDB, res *Result, config *pobcode.ConfigSet) {
	if config == nil {
		return
	}
	for _, block := range config.CustomMods {
		if !block.Enabled {
			continue
		}
		title := block.Title
		if title == "" {
			title = "Default"
		}
		for _, line := range strings.Split(block.Text, "\n") {
			if line = strings.TrimSpace(line); line == "" {
				continue
			}
			r := c.parser.Parse(line)
			if !r.Parsed() {
				res.Unparsed = append(res.Unparsed, line)
				continue
			}
			for _, m := range r.Mods {
				db.Add(fromParsed(m, "Custom:"+title))
			}
		}
	}
}

// adornedEffect ports CalcSetup.lua's The Adorned handling: an allocated
// The Adorned sets Multiplier:Corrupted<Rarity>JewelEffect, and corrupted
// jewels of that rarity in tree sockets get it added to their scale.
// (PoB also checks node.containJewelSocket, which nothing sets.)
func (c *Calculator) adornedEffect(spec *pobcode.Spec, b *pobcode.Build, allocated map[int]bool, res *Result) map[itemtext.Rarity]float64 {
	out := map[itemtext.Rarity]float64{}
	for _, s := range spec.Sockets {
		if s.ItemID == 0 || !allocated[s.NodeID] {
			continue
		}
		if it := c.peekItem(b.Item(s.ItemID)); it == nil || it.Name != "The Adorned" || it.BaseName != "Crimson Jewel" {
			continue
		}
		_, mods := c.itemMods(b.Item(s.ItemID), res)
		for _, m := range mods {
			v, _ := m.Value.(map[string]any)
			if m.Name != "JewelData" || v == nil {
				continue
			}
			f, ok := num(v["value"])
			switch v["key"] {
			case "corruptedMagicJewelIncEffect":
				if ok {
					out[itemtext.Magic] = f / 100
				}
			case "corruptedRareJewelIncEffect":
				if ok {
					out[itemtext.Rare] = f / 100
				}
			}
		}
	}
	return out
}

// peekItem parses an item's text without recording parse problems.
func (c *Calculator) peekItem(item *pobcode.Item) *itemtext.Item {
	if item == nil {
		return nil
	}
	it, err := c.items.Parse(item.Raw)
	if err != nil {
		return nil
	}
	return it
}
