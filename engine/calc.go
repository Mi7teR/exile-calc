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
	keystones    map[string]keystone // PassiveTree.lua keystoneMap
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
	// main is env.player.mainSkill, nil when there is none.
	main *activeSkill
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

// equipmentBonusScale returns the scale of an equipped item's mods
// (CalcSetup.lua): quivers with Widowhail or EffectOfBonusesFromQuiver on
// passives, gloves and boots with EffectOfBonusesFromGloves/Boots (passives
// and custom mods); 1 otherwise. PoB merges same mods before scaling; for
// integer values the rounding is the same.
func (c *Calculator) equipmentBonusScale(db *ModDB, b *pobcode.Build, set *pobcode.ItemSet, res *Result) func(*pobcode.Item) float64 {
	gloves := (1 + db.Sum("INC", "EffectOfBonusesFromGloves")/100) * db.More("EffectOfBonusesFromGloves")
	boots := (1 + db.Sum("INC", "EffectOfBonusesFromBoots")/100) * db.More("EffectOfBonusesFromBoots")
	nodeQuiver := db.Sum("INC", "EffectOfBonusesFromQuiver")
	quiver := 1.0
	weapon1 := "Weapon 1"
	if set.UseSecondWeaponSet {
		weapon1 = "Weapon 1 Swap"
	}
	for _, slot := range set.Slots {
		if slot.Name != weapon1 || slot.ItemID == 0 {
			continue
		}
		if it, mods := c.itemMods(b.Item(slot.ItemID), res); it != nil {
			sum := nodeQuiver
			for _, m := range mods {
				if f, ok := num(m.Value); ok && m.Name == "EffectOfBonusesFromQuiver" && m.Type == "INC" {
					sum += f
				}
			}
			if strings.Contains(it.Name, "Widowhail") || nodeQuiver > 0 {
				quiver = 1 + sum/100
			}
		}
	}
	if quiver != 1 {
		db.NewMod("WidowHailMultiplier", "BASE", quiver, "Widowhail")
	}
	return func(item *pobcode.Item) float64 {
		it := c.peekItem(item)
		if it == nil || it.Base == nil {
			return 1
		}
		switch it.Base.Type {
		case "Quiver":
			return quiver
		case "Gloves":
			return gloves
		case "Boots":
			return boots
		}
		return 1
	}
}

// classStartJewelEffect is the jewelIncEffectFromClassStart part of the
// CalcSetup.lua item loop (Split Personality): the socket's effect grows by
// the jewel's percentage per allocated passive between it and the class
// start (PassiveSpec:SetNodeDistanceToClassStart).
func (c *Calculator) classStartJewelEffect(item *pobcode.Item, node int, alloc map[int]bool, classID int, links map[int][]int, res *Result) float64 {
	_, mods := c.itemMods(item, res)
	inc := 0.0
	for _, m := range mods {
		if v, ok := m.Value.(map[string]any); ok && m.Name == "JewelData" && v["key"] == "jewelIncEffectFromClassStart" {
			inc, _ = num(v["value"])
		}
	}
	if inc == 0 {
		return 0
	}
	// pathToClassStart is the socket, the class start, then the nodes back
	// to the socket.
	path := pathToClassStart(c.data.Tree, alloc, node, classID, links)
	if len(path) < 3 {
		return 0
	}
	return float64(len(path)-3) * inc / 100
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
	// ItemsTab:Load ignores the ModRange ids when there are more of them than
	// saved mod lines (lines that stopped being mod lines).
	ranges := map[int]float64{}
	if len(item.ModRanges) <= len(it.Enchants)+len(it.Scourge)+len(it.Implicits)+len(it.Explicits)+len(it.Crucible) {
		for _, mr := range item.ModRanges {
			ranges[mr.ID] = mr.Range
		}
	}
	type entry struct {
		kind   string // enchant, scourge, implicit, explicit, crucible
		line   itemtext.ModLine
		r      float64
		scalar float64
	}
	var entries []entry
	// The ids count the base flask buff lines (buffModLines) first.
	id := 0
	if it.Base != nil && it.Base.Flask != nil {
		id = len(it.Base.Flask.Buff)
	}
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
			if isWeapon {
				sm = weaponHandLocal(sm, m, slotNumber(it, slot))
			}
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

// weaponHandLocal is the end of Item.lua's weapon branch: accuracy, impale
// chance, life/mana on hit and physical leech on a weapon only apply to
// attacks with that hand; poison and bleed chance get the hand condition too.
func weaponHandLocal(sm Mod, m modparser.Mod, slotNum int) Mod {
	hand := map[string]any{"type": "Condition", "var": "OffHandAttack"}
	if slotNum == 1 {
		hand["var"] = "MainHandAttack"
	}
	kwOK := m.KeywordFlags == 0 || m.KeywordFlags == kfAttack
	switch {
	case ((m.Name == "Accuracy" && m.Flags == 0) || (m.Name == "ImpaleChance" && m.Flags != mfSpell) ||
		((m.Name == "LifeOnHit" || m.Name == "ManaOnHit") && m.Flags == mfAttack) ||
		((m.Name == "PhysicalDamageLifeLeech" || m.Name == "PhysicalDamageManaLeech") && m.Flags == mfAttack)) &&
		kwOK && len(m.Tags) == 0:
		sm.Tags = append([]map[string]any{hand}, sm.Tags...)
	case (m.Name == "PoisonChance" || m.Name == "BleedChance") && m.Flags != mfSpell &&
		(len(m.Tags) == 0 || (m.Tags[0]["type"] == "Condition" && m.Tags[0]["var"] == "CriticalStrike" && len(m.Tags) == 1)):
		sm.Tags = append(append([]map[string]any(nil), sm.Tags...), hand)
	}
	return sm
}

// mergeKeystones is modLib.mergeKeystones: a Keystone mod (an item line
// such as "Blood Magic") adds that keystone's node mods once, with the
// source of the line unless it comes from the tree. PoB runs it at the
// start of calcs.perform, after flasks and after buffs.
// keystone is an entry of the keystone map: its mod source and stat lines.
type keystone struct {
	source string
	stats  []string
}

func (c *Calculator) mergeKeystones(db *ModDB, added map[string]bool, res *Result) {
	c.keystoneOnce.Do(func() {
		c.keystones = map[string]keystone{}
		for _, n := range c.data.Tree.Nodes {
			if n.IsKeystone && n.Group != 0 {
				k := keystone{fmt.Sprintf("Tree:%d", n.ID), n.Stats}
				c.keystones[n.Name] = k
				c.keystones[strings.ToLower(n.Name)] = k
			}
		}
		// Legacy keystones of the legion data (Corrupted Soul...), unless
		// the tree has one of that name.
		if t := c.data.Timeless; t != nil {
			for _, n := range t.Nodes {
				if _, ok := c.keystones[n.Name]; n.Keystone && !ok {
					c.keystones[n.Name] = keystone{"Tree:" + n.ID, n.Stats}
				}
			}
		}
	})
	for _, e := range db.List(nil, "Keystone") {
		name, _ := e.Value.(string)
		node, ok := c.keystones[name]
		if added[name] || !ok {
			continue
		}
		added[name] = true
		source := node.source
		if e.Mod.Source != "" && !strings.Contains(strings.ToLower(e.Mod.Source), "tree") {
			source = e.Mod.Source
		}
		for _, m := range c.nodeLines(node.stats, res) {
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
		stats := []float64{str, dex, int_}
		sort.Float64s(stats)
		out["LowestAttribute"] = stats[0]
		cond["TwoHighestAttributesEqual"] = stats[1] == stats[2]
		cond["IntSingleHighestAttribute"] = int_ > str && int_ > dex
		cond["DexSingleHighestAttribute"] = dex > str && dex > int_
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
		out[elem+"ResistOver75"] = math.Max(0, final-75)
		out["Missing"+elem+"Resist"] = math.Max(0, max-final)
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
