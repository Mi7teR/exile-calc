package engine

import (
	"fmt"
	"math"
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
	Skills          []SkillInfo // the player's active skills, in PoB's order
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
	db.enemy = NewModDB()
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
	db.NewMod("Life", "BASE", lifePerLevel, "Base", map[string]any{"type": "Multiplier", "var": "Level", "base": 38.0})
	db.NewMod("Mana", "BASE", manaPerLevel, "Base", map[string]any{"type": "Multiplier", "var": "Level", "base": 34.0})
	db.NewMod("Evasion", "BASE", baseEvasion, "Base")
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
	case "Oak":
		db.NewMod("Life", "BASE", 40, "Bandit")
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
		var funcs []radiusFunc
		if !isConquered { // jewelFuncList: nodes changed by timeless jewels are left alone
			funcs = radiusFuncs.byNode[id]
		}
		if !add {
			c.nodeMods(res, stats, 0, source, node, funcs)
			return
		}
		c.addNode(db, res, stats, 0, source, node, funcs)
	}
	for _, id := range spec.Nodes {
		addTreeNode(id, true)
	}

	// Items in the active set; jewels in allocated tree sockets.
	var flasks []flaskSlot
	var gear []equipped
	useSwap := false
	if set := b.ActiveItems(); set != nil {
		useSwap = set.UseSecondWeaponSet
		type abyss struct {
			slot, parent string
			num          int
			item         *pobcode.Item
		}
		var abyssal []abyss
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
			it, mods := c.addItem(db, res, b.Item(slot.ItemID), 1, slot.Name)
			if it != nil {
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
		for _, a := range abyssal {
			parent := parents[a.parent]
			if parent == nil || abyssalSockets(parent) < a.num {
				continue
			}
			if it, _ := c.addItem(db, res, a.item, 1+jewelEffect[a.parent]/100, ""); it != nil {
				db.items[a.slot] = it
			}
		}
	}
	corruptedEffect := c.adornedEffect(spec, b, allocated, res)
	for _, s := range spec.Sockets {
		if s.ItemID != 0 && allocated[s.NodeID] {
			scale := 1.0
			if it := c.peekItem(b.Item(s.ItemID)); it != nil && it.Corrupted && it.Base != nil &&
				it.Base.Type == "Jewel" && it.Base.SubType != "Charm" {
				scale += corruptedEffect[it.Rarity]
			}
			if it, _ := c.addItem(db, res, b.Item(s.ItemID), scale, ""); it != nil {
				db.items["Jewel "+strconv.Itoa(s.NodeID)] = it
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
	buffMode(db)
	c.applyConfig(db, res, config)
	c.customMods(db, res, config)
	shrineBaseMods(db)
	skills := c.setupSkills(db, b, useSwap, res)
	for _, s := range skills {
		c.buildSkillMods(db, s, res)
	}
	keystones := map[string]bool{}
	c.applyFlasks(db, res, flasks)
	c.mergeKeystones(db, keystones, res)
	c.attributes(db)
	shrineBuffs(db)
	c.lifeMana(db)
	c.applyBuffs(db, skills, res)
	c.mergeKeystones(db, keystones, res)
	res.Skills = skillInfo(skills)
	c.resistances(db)
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
	mods := c.nodeMods(res, stats, incEffect, source, tn, funcs)
	node := NewModDB()
	for _, m := range mods {
		node.Add(m)
	}
	// buildModListForNode: a node that "grants nothing" loses its mods.
	if node.Flag("PassiveSkillHasNoEffect") || node.Flag("AllocatedPassiveSkillHasNoEffect") {
		return
	}
	scale := (1 + node.Sum("INC", "PassiveSkillEffect")/100) * node.More("PassiveSkillEffect")
	for _, m := range mods {
		db.Add(scaleMod(m, scale))
	}
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
	// First pass radius jewels ("Other" functions) see the node's own mods.
	if tn != nil {
		own := append([]Mod(nil), mods...)
		for _, f := range funcs {
			mods = append(mods, f(tn, own)...)
		}
	}
	return mods
}

// localDefence names the mods an armour piece applies to itself
// (Item:BuildModListForSlotNum); they do not reach the character directly.
var localDefence = map[string]bool{
	"Armour": true, "Evasion": true, "EnergyShield": true, "Ward": true,
	"ArmourAndEvasion": true, "ArmourAndEnergyShield": true, "EvasionAndEnergyShield": true, "Defences": true,
}

func hasGlobalTag(m modparser.Mod) bool {
	for _, t := range m.Tags {
		if t["type"] == "Global" {
			return true
		}
	}
	return false
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
	ranges := map[int]float64{}
	for _, mr := range item.ModRanges {
		ranges[mr.ID] = mr.Range
	}
	var lines []string
	id := 0
	for _, group := range [][]itemtext.ModLine{it.Enchants, it.Scourge, it.Implicits, it.Explicits, it.Crucible} {
		for _, l := range group {
			id++
			if len(l.Variants) > 0 && !containsInt(l.Variants, it.Variant) {
				continue
			}
			r := defaultAffixQuality
			if l.HasRange {
				r = l.Range
			}
			if v, ok := ranges[id]; ok {
				r = v
			}
			lines = append(lines, applyRange(l.Text, r, catalystScalar(it, l)))
		}
	}
	return it, lines
}

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
		if isArmour && localDefence[m.Name] && !hasGlobalTag(m) {
			continue // local to the item: part of its own defence values
		}
		if isWeapon && m.Flags != 0 {
			continue // local weapon stats: part of the weapon's own data
		}
		db.Add(scaleMod(slotTags(fromParsed(m, source), slot), scale))
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
func slotTags(m Mod, slot string) Mod {
	if slot == "" {
		return m
	}
	slot = strings.Replace(slot, " Swap", "", 1)
	hand, other := "MainHand", "2"
	if strings.HasSuffix(slot, " 2") {
		hand, other = "OffHand", "1"
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
	return m
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
	if !db.Flag("NoStrengthAttributeBonuses") && !db.Flag("NoStrBonusToLife") {
		db.NewMod("Life", "BASE", math.Floor(out["Str"]/2), "Strength")
	}
	if !db.Flag("NoDexterityAttributeBonuses") && !db.Flag("NoDexBonusToEvasion") {
		db.NewMod("Evasion", "INC", math.Floor(out["Dex"]/5), "Dexterity")
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
		if !ok {
			base := db.Sum("BASE", names...)
			inc := math.Max((1+db.Sum("INC", names...)/100)*db.More(names...), 0)
			total = base * inc
		}
		total, max = modfInt(total), modfInt(max)
		final := math.Max(math.Min(total, max), resistFloor)
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
