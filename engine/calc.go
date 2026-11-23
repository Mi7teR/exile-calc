package engine

import (
	"fmt"
	"math"
	"strings"

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
	return &Calculator{data: d, parser: p, items: itemtext.New(d), luts: &timelessLUTs{dir: defaultTimelessDir()}}, nil
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
	db.enemy = NewModDB()
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
	conquered := c.conquests(spec, b, allocated, res)
	radiusFuncs := c.radiusFuncs(spec, b, allocated, res)
	for _, id := range spec.Nodes {
		if g, ok := cluster[id]; ok {
			c.addNode(db, res, g.stats, g.incEffect, fmt.Sprintf("Tree:%d", id), nil, nil)
			continue
		}
		node := tree.Nodes[id]
		if node == nil {
			continue
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
			funcs = radiusFuncs[id]
		}
		c.addNode(db, res, stats, 0, source, node, funcs)
	}

	// Items in the active set; jewels in allocated tree sockets.
	var flasks []*pobcode.Item
	if set := b.ActiveItems(); set != nil {
		for _, slot := range set.Slots {
			if slot.ItemID == 0 {
				continue
			}
			if strings.HasPrefix(slot.Name, "Flask") {
				if slot.Active {
					flasks = append(flasks, b.Item(slot.ItemID))
				}
				continue
			}
			if skipSlot(slot.Name, set.UseSecondWeaponSet) {
				continue
			}
			c.addItem(db, res, b.Item(slot.ItemID), "")
		}
	}
	for _, s := range spec.Sockets {
		if s.ItemID != 0 && allocated[s.NodeID] {
			c.addItem(db, res, b.Item(s.ItemID), "")
		}
	}

	c.applyFlasks(db, res, flasks)
	c.attributes(db)
	c.lifeMana(db)
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

// addNode adds one passive's mods, scaled by its PassiveSkillEffect
// (CalcSetup.lua buildModListForNode: calcLib.mod(modList, nil, "PassiveSkillEffect")).
// incEffect is the cluster jewel's "increased Effect" of small passives.
func (c *Calculator) addNode(db *ModDB, res *Result, stats []string, incEffect float64, source string, tn *gamedata.Node, funcs []radiusFunc) {
	node := NewModDB()
	var mods []Mod
	for _, line := range stats {
		r := c.parser.Parse(line)
		if !r.Parsed() {
			res.Unparsed = append(res.Unparsed, line)
			continue
		}
		for _, m := range r.Mods {
			mods = append(mods, fromParsed(m, source))
		}
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
	for _, line := range lines {
		parsed := c.parser.Parse(line)
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

func (c *Calculator) addItem(db *ModDB, res *Result, item *pobcode.Item, _ string) {
	if item == nil {
		return
	}
	it, mods := c.itemMods(item, res)
	if it == nil {
		return
	}
	source := itemSource(item, it)
	isArmour := it.Base != nil && it.Base.Armour != nil
	isWeapon := it.Base != nil && it.Base.Weapon != nil
	for _, m := range mods {
		if isArmour && localDefence[m.Name] && !hasGlobalTag(m) {
			continue // local to the item: part of its own defence values
		}
		if isWeapon && m.Flags != 0 {
			continue // local weapon stats: part of the weapon's own data
		}
		db.Add(fromParsed(m, source))
	}
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
	for _, elem := range resistTypes {
		maxNames := []string{elem + "ResistMax"}
		names := []string{elem + "Resist"}
		if elem != "Chaos" {
			maxNames = append(maxNames, "ElementalResistMax")
			names = append(names, "ElementalResist")
		}
		max, ok := db.Override(elem + "ResistMax")
		if !ok {
			max = math.Min(maxResistCap, db.Sum("BASE", maxNames...))
		}
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
