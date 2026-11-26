package engine

import (
	"fmt"
	"maps"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/modparser"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// calcState holds what one Calc call shares between its steps: the locals
// of the old single-body Calc that outlive one step. It is created per
// call, so Calc stays safe for concurrent use.
type calcState struct {
	b      *pobcode.Build
	spec   *pobcode.Spec // the pruned active spec
	class  gamedata.Class
	config *pobcode.ConfigSet
	db     *ModDB
	res    *Result

	// calculator is env.mode ~= "MAIN" (getMiscCalculator and
	// calcFullDPS run in "CALCULATOR" mode): b's socket groups are used as
	// they are, without CalcSetup.lua's granted skill block (1506).
	calculator bool
	// skillPass is a MAIN-mode calcs.buildActiveSkill pass (the cost
	// warnings of buildOutput) on the calculated build: env.buildBreakdown
	// is false (Calcs.lua:372) and the granted skill block, which would
	// change nothing on that build, is skipped as in CALCULATOR mode.
	skillPass bool
	// compared is the id of the Comparer's compared item (0 = none),
	// whose grants are not cached (itemInfo).
	compared int
	// extraJewelFuncs is override.extraJewelFuncs (CalcSetup.lua:1466-1475):
	// the ExtraJewelFunc mods of the first pass, which the radius jewel
	// list of the second pass gives every jewel they apply to; nil in the
	// first pass.
	extraJewelFuncs []Mod
	// grants is what the granted skill block set on each socket group of
	// b (groupGrant): computed in MAIN mode, given by calcOpts in
	// CALCULATOR mode, whose groups the MAIN pass left as PoB keeps them.
	grants []groupGrant

	// Passive tree.
	allocated    map[int]bool
	clusterLinks map[int][]int
	radiusFuncs  radiusJewels
	// addTreeNode is buildModListForNode; with add false it only runs the
	// radius functions (PoB's extraRadiusNodeList, whose lists are dropped).
	addTreeNode func(id int, add bool)
	// treeSpan is where stepPassiveTree's mods sit in each name's list
	// ([start, end)), for stepMergeTree.
	treeSpan map[string][2]int
	// explodeNodes are the allocated nodes whose final mod list has
	// CanExplode (buildModListForNode's second result, CalcSetup.lua:265-266),
	// in the order addTreeNode added them.
	explodeNodes []int

	// Items.
	flasks    []flaskSlot
	gear      []equipped
	gearMods  map[string][]modparser.Mod // all parsed mods of each gear slot
	reflected []*itemtext.Item           // the other ring of each Kalandra's Touch
	useSwap   bool

	// Skills and calcs.perform.
	skills              []*activeSkill
	keystones           map[string]bool
	tinctures           tinctureEffect
	flaskSt             *flaskState
	playerEnemyMods     enemyModifiers
	chilling            []*activeSkill
	guaranteedBonechill bool
	curses              []*enemyCurse
	buffs               map[string][]Mod
}

// calcStep is one named stage of Calc.
type calcStep struct {
	name string
	run  func(c *Calculator, st *calcState) error
}

// calcSteps is Calc's pipeline. The order is PoB's CalcSetup.lua initEnv
// order followed by CalcPerform.lua calcs.perform order: bandit (663),
// pantheon (674), enemy Accuracy (689) and the config tab mods (692) come
// before the tree and the items. Tree mods are added before the items so
// the item step can read them, as PoB's item loop reads initialNodeModDB;
// mergeTree then moves them behind the item mods, matching PoB's final
// order (items 1426, tree 1463). MORE mods round per layer, so moving a
// step changes numbers. TestCalcStepOrder pins the names.
var calcSteps = []calcStep{
	{"initEnv", stepInitEnv},
	{"baseMods", stepBaseMods},
	{"bandit", stepBandit},
	{"pantheon", stepPantheon},
	{"config", stepConfig},
	{"customMods", stepCustomMods},
	{"passiveTree", stepPassiveTree},
	{"items", stepItems},
	{"jewelSockets", stepJewelSockets},
	{"mergeTree", stepMergeTree},
	{"grantedPassives", stepGrantedPassives},
	{"itemConditions", stepItemConditions},
	{"skills", stepSkills},
	{"flasks", stepFlasks},
	{"skillModLayer", stepSkillModLayer},
	{"earlyEnemyMods", stepEarlyEnemyMods},
	{"armourESToLife", stepArmourESToLife},
	{"attributesLifeMana", stepAttributesLifeMana},
	{"reservations", stepReservations},
	{"tinctures", stepTinctures},
	{"skillCounts", stepSkillCounts},
	{"chillSkills", stepChillSkills},
	{"earlyCharges", stepEarlyCharges},
	{"buffs", stepBuffs},
	{"curses", stepCurses},
	{"extraAuras", stepExtraAuras},
	{"disableWeapons", stepDisableWeapons},
	{"defenceForConditionals", stepDefenceForConditionals},
	{"charges", stepCharges},
	{"actorMisc", stepActorMisc},
	{"skillInfo", stepSkillInfo},
	{"allyLife", stepAllyLife},
	{"defence", stepDefence},
	{"minion", stepMinion},
	{"offence", stepOffence},
	{"unsupportedTags", stepUnsupportedTags},
}

// Calc computes a build (PoB's calcs.initEnv and calcs.perform).
func (c *Calculator) Calc(b *pobcode.Build) (*Result, error) {
	res, err := c.calc(b, calcOpts{})
	if err != nil {
		return nil, err
	}
	return res, c.buildOutput(res)
}

// calcOpts are the calcState settings Calc leaves at zero.
type calcOpts struct {
	calculator bool         // calcState.calculator: CALCULATOR mode
	skillPass  bool         // calcState.skillPass
	compared   int          // calcState.compared
	grants     []groupGrant // calcState.grants in CALCULATOR mode
	// extraJewelFuncs is calcState.extraJewelFuncs: non-nil (maybe empty)
	// once the first pass has collected them.
	extraJewelFuncs []Mod
}

// calc is Calc with opts: MAIN mode, or CALCULATOR mode with
// opts.calculator.
func (c *Calculator) calc(b *pobcode.Build, opts calcOpts) (*Result, error) {
	// The steps of calcSteps run in order over a fresh calcState.
	st := &calcState{b: b, calculator: opts.calculator, skillPass: opts.skillPass, compared: opts.compared,
		grants: opts.grants, extraJewelFuncs: opts.extraJewelFuncs}
	for _, step := range calcSteps {
		if err := step.run(c, st); err != nil {
			return nil, err
		}
		// CalcSetup.lua:1466-1475, after the items and granted passives
		// are merged: ExtraJewelFunc mods (Ngamahu, Flame's Advance) make
		// PoB build the env again with them in the radius jewel list.
		if step.name == "grantedPassives" && opts.extraJewelFuncs == nil {
			// env.modDB:Tabulate("LIST", nil, "ExtraJewelFunc"): LIST mods
			// whose tags hold, over the parent chain.
			extra := tabulateMods(st.db, "LIST", "ExtraJewelFunc")
			if len(extra) > 0 {
				opts.extraJewelFuncs = extra
				return c.calc(b, opts)
			}
		}
	}
	return st.res, nil
}

// stepInitEnv: CalcSetup.lua calcs.initEnv — the active spec, the class
// and a fresh player/enemy ModDB pair.
func stepInitEnv(c *Calculator, st *calcState) error {
	b := st.b
	spec := b.ActiveSpec()
	if spec == nil {
		return fmt.Errorf("engine: build has no passive tree")
	}
	tree := c.data.Tree
	spec = c.pruneSpec(c.legacyClusterSpec(spec, b), b)
	if spec.ClassID < 0 || spec.ClassID >= len(tree.Classes) {
		return fmt.Errorf("engine: unknown class id %d", spec.ClassID)
	}
	st.spec = spec
	st.class = tree.Classes[spec.ClassID]
	db := NewModDB()
	db.tags = itemTags{
		special:   c.parser.Table("itemTagSpecial"),
		exclusion: c.parser.Table("itemTagSpecialExclusionPattern"),
	}
	db.build = b
	db.enemy = NewModDB()
	db.enemy.enemy = db
	db.player, db.enemy.player = db, db
	db.gameIDs = c.gemIdx.gameID
	db.buildBreakdown = !st.calculator && !st.skillPass // CalcSetup.lua:512, Calcs.lua:372
	st.db = db
	st.res = &Result{Stats: db.output, DB: db}
	st.config = b.ActiveConfig()
	return nil
}

// stepBaseMods: CalcSetup.lua calcs.initModDB and initEnv's base values
// (attributes, resists, block, life/mana per level, resistance penalty).
func stepBaseMods(_ *Calculator, st *calcState) error {
	db, b, class, config := st.db, st.b, st.class, st.config
	db.NewMod("Str", "BASE", float64(class.BaseStr), "Base")
	db.NewMod("Dex", "BASE", float64(class.BaseDex), "Base")
	db.NewMod("Int", "BASE", float64(class.BaseInt), "Base")
	db.multipliers["Level"] = math.Max(1, math.Min(100, float64(b.Level)))
	for _, r := range resistTypes {
		db.NewMod(r+"ResistMax", "BASE", baseMaxResist, "Base")
	}
	for _, r := range resistTypes { // CalcSetup.lua:39-42
		db.NewMod("Totem"+r+"ResistMax", "BASE", baseMaxResist, "Base")
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
	// CalcSetup.lua:621-624.
	for _, r := range resistTypes[:3] {
		db.NewMod("Totem"+r+"Resist", "BASE", 40, "Base")
	}
	db.NewMod("TotemChaosResist", "BASE", 20, "Base")
	return nil
}

// legacyBuildChoice is the default of the bandit and pantheon config
// inputs. Build.lua copies the Build element's bandit/pantheon attributes
// into configTab.input ("Load legacy bandit and pantheon choices"), but
// ConfigTab:Load then replaces the input with the active config set's, so
// the attribute counts only for a build saved without a Config section;
// otherwise a missing input is "None".
func legacyBuildChoice(b *pobcode.Build, attr string) string {
	if len(b.ConfigSets) == 0 {
		return attr
	}
	return "None"
}

// stepBandit: CalcSetup.lua "Add bandit mods".
func stepBandit(_ *Calculator, st *calcState) error {
	db := st.db
	switch bandit := configString(st.config, "bandit", legacyBuildChoice(st.b, st.b.Bandit)); bandit {
	case "Alira":
		db.NewMod("ElementalResist", "BASE", 15, "Bandit")
	case "Kraityn":
		db.NewMod("MovementSpeed", "INC", 8, "Bandit")
	case "Oak":
		db.NewMod("Life", "BASE", 40, "Bandit")
	default:
		db.NewMod("ExtraPoints", "BASE", 1, "Bandit")
	}
	return nil
}

// stepPantheon: CalcSetup.lua "Add Pantheon mods" (PantheonTools.lua
// applySoulMod): every soul of the chosen gods, named after the god's own
// soul.
func stepPantheon(c *Calculator, st *calcState) error {
	db, b, config := st.db, st.b, st.config
	for _, x := range [][2]string{
		{"pantheonMajorGod", legacyBuildChoice(b, b.PantheonMajorGod)},
		{"pantheonMinorGod", legacyBuildChoice(b, b.PantheonMinorGod)},
	} {
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
	return nil
}

// stepPassiveTree: CalcSetup.lua buildModListForNodeList over the
// allocated nodes: node stats, mastery effects, cluster, conquered and
// radius-jewel nodes.
// CalcSetup.lua builds the node list at 786 (env.initialNodeModDB, which
// the item loop reads) but merges it into env.modDB at 1463, after the
// item mods (1426): the mods are added here, so the item step sees them,
// and stepMergeTree moves them behind the item mods.
//
//nolint:gocognit // moved verbatim from Calc (addTreeNode closure); splitting it is out of scope for a pure refactor
func stepPassiveTree(c *Calculator, st *calcState) error {
	db, b, spec, res := st.db, st.b, st.spec, st.res
	tree := c.data.Tree
	allocated := map[int]bool{}
	for _, id := range spec.Nodes {
		allocated[id] = true
	}
	mastery := map[int]int{}
	for _, m := range spec.MasteryEffects {
		mastery[m.Node] = m.Effect
	}
	cluster, clusterLinks, _ := c.clusterGraph(spec, b, res)
	treeCounts(db, tree, spec.Nodes, cluster)
	conquered := c.conquests(spec, b, allocated, res)
	radiusFuncs := c.radiusFuncs(spec, b, allocated, res, st.extraJewelFuncs)
	addTreeNode := func(id int, add bool) {
		if g, ok := cluster[id]; ok {
			if !add {
				return
			}
			st.noteExplodeNode(id, c.addNode(db, res, g.stats, g.incEffect, fmt.Sprintf("Tree:%d", id), nil, nil))
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
		funcs, second := otherFuncsFor(radiusFuncs.byNode[id], node, cq, isConquered), radiusFuncs.second[id]
		mods := finishNode(c.nodeMods(res, stats, 0, source, node, funcs), add, node, second)
		if !add {
			return
		}
		for _, m := range mods {
			db.Add(m)
		}
		st.noteExplodeNode(id, mods)
	}
	before := make(map[string]int, len(db.mods))
	for name, l := range db.mods {
		before[name] = len(l)
	}
	for _, id := range spec.Nodes {
		addTreeNode(id, true)
	}
	st.treeSpan = map[string][2]int{}
	for name, l := range db.mods {
		if len(l) > before[name] {
			st.treeSpan[name] = [2]int{before[name], len(l)}
		}
	}
	st.allocated, st.clusterLinks, st.radiusFuncs, st.addTreeNode = allocated, clusterLinks, radiusFuncs, addTreeNode
	return nil
}

// noteExplodeNode records allocated node id as an explode source when its
// final mod list can explode (buildModListForNodeList, CalcSetup.lua:
// 288-290: t_insert(explodeSources, explode)).
func (st *calcState) noteExplodeNode(id int, mods []Mod) {
	if nodeCanExplode(mods) {
		st.explodeNodes = append(st.explodeNodes, id)
	}
}

// stepItems: CalcSetup.lua "Build and merge item modifiers" for the active
// item set: flasks, gear, Kalandra's Touch and abyss jewels.
//
//nolint:gocognit,gocyclo,cyclop,nestif,funlen // moved verbatim from Calc; splitting it is out of scope for a pure refactor
func stepItems(c *Calculator, st *calcState) error {
	db, b, spec, res := st.db, st.b, st.spec, st.res
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
		bonusScale := c.equipmentBonusScale(db, b, set, res)
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
			// CalcSetup.lua: Graft slots exist on the 3.27 tree only, Wickerman
			// slots on the 3.30 tree only.
			if tree := spec.TreeVersion; (strings.HasPrefix(slot.Name, "Graft ") && !strings.Contains(tree, "3_27")) ||
				(strings.HasPrefix(slot.Name, "Wickerman ") && !strings.Contains(tree, "3_30")) {
				continue
			}
			// CalcSetup.lua:825: Ring 3 counts only with AdditionalRingSlot
			// from the allocated nodes (The Unseen Hand).
			if slot.Name == "Ring 3" && !additionalRingSlot(db) {
				continue
			}
			// CalcSetup.lua: the active swap set's slots lose " Swap".
			if set.UseSecondWeaponSet {
				slot.Name = strings.Replace(slot.Name, " Swap", "", 1)
			}
			if i := strings.Index(slot.Name, " Abyssal Socket "); i >= 0 {
				n, _ := strconv.Atoi(slot.Name[i+len(" Abyssal Socket "):])
				abyssal = append(abyssal, abyss{slot.Name, slot.Name[:i], n, b.Item(slot.ItemID)})
				continue
			}
			if strings.HasPrefix(slot.Name, "Ring") {
				if pit := c.peekItem(
					b.Item(slot.ItemID),
				); pit != nil &&
					strings.Contains(pit.Name, "Kalandra's Touch") {
					// Kalandra's Touch: its own mods do not apply, except ExtraSkill.
					it, mods := c.itemMods(b.Item(slot.ItemID), res)
					for _, m := range mods {
						if m.Name == "ExtraSkill" {
							if sm, ok := slotTags(
								fromParsed(m, itemSource(b.Item(slot.ItemID), it)),
								slot.Name,
								slotNumber(it, slot.Name),
							); ok {
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
			scale, mode := bonusScale(b.Item(slot.ItemID))
			it, mods := c.addItem(db, res, b.Item(slot.ItemID), scale, slot.Name, mode)
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
			if it, _ := c.addItem(db, res, a.item, 1+jewelEffect[a.parent]/100, "", scaleEach); it != nil {
				db.items[a.slot] = it
				abyssCounts(db, it, a.parent)
			}
		}
	}
	st.flasks, st.gear, st.gearMods, st.reflected, st.useSwap = flasks, gear, gearMods, reflected, useSwap
	return nil
}

// stepJewelSockets: CalcSetup.lua's jewel slots — jewels in allocated tree
// sockets, with corrupted (Adorned) and class-start jewel effect.
func stepJewelSockets(c *Calculator, st *calcState) error {
	db, b, spec, res, allocated := st.db, st.b, st.spec, st.res, st.allocated
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
			// CalcSetup.lua merges the item's mods before scaling them
			// only in the corrupted-jewel-effect branch.
			mode := scaleEach
			if it := c.peekItem(b.Item(s.ItemID)); it != nil && it.Corrupted && it.Base != nil &&
				it.Base.Type == "Jewel" && it.Base.SubType != "Charm" {
				if v, ok := corruptedEffect[it.Rarity]; ok {
					scale += v
					mode = scaleMerged
				}
			}
			scale += c.classStartJewelEffect(b.Item(s.ItemID), s.NodeID, allocated, spec.ClassID, st.clusterLinks, res)
			if it, _ := c.addItem(db, res, b.Item(s.ItemID), scale, "", mode); it != nil {
				db.items["Jewel "+strconv.Itoa(s.NodeID)] = it
				abyssCounts(db, it, "")
			}
		}
	}
	return nil
}

// stepCustomMods: CalcSetup.lua "Add mods from the config tab" (692), the
// custom modifiers, which ConfigTabClass:BuildModList appends to
// configTab.modList after the options' mods (stepConfig). They join
// env.modDB before the tree nodes and items, and so before the granted
// passives are added: "Allocates X" custom lines grant their node.
func stepCustomMods(c *Calculator, st *calcState) error {
	c.customMods(st.db, st.res, st.config)
	return nil
}

// stepGrantedPassives: CalcSetup.lua "Add granted passives", then
// buildModListForNodeList's finishJewels: unallocated nodes in a radius,
// then the radius jewels' finalisers.
func stepGrantedPassives(c *Calculator, st *calcState) error {
	db, allocated, radiusFuncs, addTreeNode := st.db, st.allocated, st.radiusFuncs, st.addTreeNode
	notable, ascendancy := c.notableMaps()
	granted := grantedNodes(db, notable, ascendancy, st.class.Name, allocated)
	for _, id := range granted {
		addTreeNode(id, true)
	}
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
	return nil
}

// stepMergeTree: CalcSetup.lua:1426 merges the item mods (gear, abyss and
// tree-socket jewels) into env.modDB before the allocated nodes' mods
// (1460-1464): each name's tree mods move behind the mods added since.
// Order matters where a list is read in order (List, Override, the first
// matching mod).
func stepMergeTree(_ *Calculator, st *calcState) error {
	db := st.db
	for name, span := range st.treeSpan {
		l := db.mods[name]
		if len(l) < span[1] || len(l) == span[1] {
			continue // nothing added after the tree mods (or the list changed)
		}
		moved := make([]Mod, 0, len(l))
		moved = append(moved, l[:span[0]]...)
		moved = append(moved, l[span[1]:]...)
		moved = append(moved, l[span[0]:span[1]]...)
		db.mods[name] = moved
	}
	return nil
}

// stepItemConditions: CalcSetup.lua's item conditions and "Calculate
// socket counts"; Kalandra's Touch adds the influence and corruption
// multipliers of the other ring.
func stepItemConditions(c *Calculator, st *calcState) error {
	db := st.db
	itemConditions(db, st.gear)
	c.socketCounts(db, st.b, st.gear, st.config)
	for _, o := range st.reflected {
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
	return nil
}

// stepConfig: buff mode, the enemy environment (CalcSetup.lua enemy level,
// enemy Accuracy at 689), the config tab's option mods (ConfigOptions.lua
// apply; CalcSetup.lua "Add mods from the config tab", 692) and the party
// tab's enemy mods (697), all before the tree nodes (786) and items (811).
func stepConfig(c *Calculator, st *calcState) error {
	db, b, res := st.db, st.b, st.res
	buffMode(db)
	buffMode(db.enemy)
	db.enemyEnv = newEnemyEnv(st.config, c.data.Config, b.Level, c.data.Monster, res)
	db.enemy.NewMod("Accuracy", "BASE", enemyAccuracy(c.data.Monster, db.enemyEnv.level), "Base")
	c.applyConfig(db, res, st.config)
	// CalcSetup.lua: env.enemyDB:AddList(build.partyTab.enemyModList).
	db.party = newPartyActor(b.Party)
	// env.partyMembers always exists in PoB: an empty actor without a Party tab.
	db.partyMembers = NewModDB()
	if db.party != nil {
		maps.Copy(db.partyMembers.output, db.party.output)
		for _, m := range db.party.flags {
			db.partyMembers.Add(m)
		}
		for _, m := range db.party.enemyMods {
			db.enemy.Add(m)
		}
		// The party member's ModDB flags (PartyMemberStats) are read by
		// mods such as "your nearby party members maximum endurance charges
		// is equal to yours"; none of those reads is ported.
		for _, m := range db.party.flags {
			res.Incomplete = append(res.Incomplete, "party member flag not applied: "+m.Name)
		}
		// Party link mods whose readers are not ported (CalcDefence.lua
		// leech cap, CalcOffence.lua Destructive Link crit).
		for _, name := range db.party.linkModNames() {
			if name == "MaximumLifeLeechIsEqualToPartyMember" || name == "MainHandCritIsEqualToPartyMember" {
				res.Incomplete = append(res.Incomplete, "party link mod not applied: "+name)
			}
		}
	}
	shrineBaseMods(db)
	return nil
}

// stepSkills: CalcSetup.lua calcs.createActiveSkill and
// calcs.buildActiveSkillModList for every active skill.
func stepSkills(c *Calculator, st *calcState) error {
	db, res := st.db, st.res
	// CalcSetup.lua:1506-1620 creates and removes the groups of granted
	// skills before the active skills are built (MAIN mode only); later
	// steps see the result too.
	// env.explodeSources is gathered in every mode; the EnemyExplode gems
	// read their source from it (enemyExplode).
	db.explodeSources = c.explodeSources(st)
	if !st.calculator && !st.skillPass {
		st.b, res.callerGroups, st.grants = c.withGrantedSkills(st, db.explodeSources)
	}
	db.build, res.build, res.compared, res.grants = st.b, st.b, st.compared, st.grants
	skills := c.setupSkills(db, st.b, st.useSwap, res, st.grants)
	db.activeSkills = skills
	for _, s := range skills {
		c.buildSkillMods(db, s, res)
	}
	if debugSkills != nil {
		debugSkills(skills)
	}
	st.skills = skills
	return nil
}

// stepFlasks: CalcPerform.lua the Alchemist's Genius buff (before flasks
// are merged), "Merge flask modifiers", then "Merge keystones again to
// catch any that were added by flasks".
func stepFlasks(c *Calculator, st *calcState) error {
	db, res := st.db, st.res
	st.keystones = map[string]bool{}
	// CalcSetup.lua:153-154: an allocated keystone node adds its Keystone
	// LIST mod, so mergeKeystones marks it in env.keystonesAdded; the
	// engine adds the node's stats directly and records the name here.
	for _, id := range st.spec.Nodes {
		if n := c.data.Tree.Nodes[id]; n != nil && n.IsKeystone {
			st.keystones[n.Name] = true
		}
	}
	if db.Flag("AlchemistsGenius") {
		effect := 1 + db.Sum("INC", "BuffEffectOnSelf")/100
		db.NewMod("FlaskEffect", "INC", math.Floor(10*effect), "Alchemist's Genius")
		db.NewMod("FlaskChargesGained", "INC", math.Floor(20*effect), "Alchemist's Genius")
	}
	st.tinctures = readTinctureEffect(db)
	// CalcPerform.lua:1876-1880 "tincture breakdown": outputs PoB sets only
	// with a breakdown (MAIN mode).
	if db.buildBreakdown {
		db.output["TinctureEffect"], db.output["TinctureLimit"] = st.tinctures.inc, st.tinctures.limit
	}
	st.flaskSt = c.applyFlasks(db, res, st.flasks)
	c.lifeFlaskMultipliers(db, st.flasks)
	c.mergeKeystones(db, st.keystones, res)
	return nil
}

// stepSkillModLayer: CalcPerform.lua skillModList = new("ModList",
// baseSkillModList), so mods added while calculating sit in their own
// layer (MORE rounds per layer).
func stepSkillModLayer(_ *Calculator, st *calcState) error {
	for _, s := range st.skills {
		if s.mods != nil {
			s.mods = s.mods.child()
		}
	}
	return nil
}

// stepEarlyEnemyMods: banners, warcry power and the first pass of
// CalcPerform.lua "Process enemy modifiers" (player and minion enemies).
func stepEarlyEnemyMods(c *Calculator, st *calcState) error {
	db := st.db
	banners(db)
	warcryPower(db)
	st.playerEnemyMods = enemyModifiers{}
	st.playerEnemyMods.apply(db)
	c.earlyMinionEnemyMods(db, st.skills, st.b.Level)
	return nil
}

// stepArmourESToLife: CalcPerform.lua ConvertArmourESToLife — "gain maximum
// life instead of maximum energy shield from equipped armour items".
func stepArmourESToLife(_ *Calculator, st *calcState) error {
	db := st.db
	if db.Flag("ConvertArmourESToLife") {
		for _, slot := range esToLifeSlots {
			if it := db.items[slot]; it != nil {
				if es := itemArmour(it, st.gearMods[slot]).EnergyShield; es > 0 {
					db.NewMod("Life", "BASE", es, slot+" ES to Life Conversion")
				}
			}
		}
	}
	return nil
}

// stepAttributesLifeMana: CalcPerform.lua "Calculate attributes and
// life/mana pools" (doActorAttribsConditions, doActorLifeMana).
func stepAttributesLifeMana(c *Calculator, st *calcState) error {
	db := st.db
	damageToAuraEffect(db)
	combatConditions(db, mainSkill(st.skills))
	c.attributes(db)
	shrineBuffs(db)
	c.lifeMana(db)
	// CalcPerform.lua:1991: env.minion gets the main skill's
	// MinionModifier mods here, after the player's attributes and before
	// the buffs.
	if s := mainSkill(st.skills); s != nil && s.mods != nil {
		s.minionModifiers = s.mods.List(s.cfg, "MinionModifier")
	}
	return nil
}

// stepReservations: CalcPerform.lua "Calculate skill life and mana
// reservations" (doActorLifeManaReservation).
func stepReservations(_ *Calculator, st *calcState) error {
	reservations(st.db, st.skills, st.res)
	return nil
}

// stepTinctures: CalcPerform.lua's tincture modifiers.
func stepTinctures(c *Calculator, st *calcState) error {
	c.applyTinctures(st.db, st.res, st.flasks, st.tinctures)
	// CalcPerform.lua:2131-2196, after the reservations and tinctures.
	c.attributeRequirements(st.db, st.b, st.compared)
	return nil
}

// stepSkillCounts: CalcPerform.lua's per-skill counts — brands, totems,
// minions, exerts, "Calculate number of active heralds and auras affecting
// self" — and Mana applies to Shock.
func stepSkillCounts(c *Calculator, st *calcState) error {
	db, skills := st.db, st.skills
	brandCounts(db, skills)
	totemCounts(db, skills)
	hexDoom(db, skills)
	c.minionCounts(db, skills)
	lootBonuses(db)
	warcryExertMods(db, skills)
	heraldAuraCounts(db, skills)
	manaAppliesToShock(db)
	return nil
}

// stepChillSkills: CalcPerform.lua's chilling-skill scan (HasBonechill,
// hasGuaranteedBonechill); the chills themselves come later.
func stepChillSkills(_ *Calculator, st *calcState) error {
	st.chilling, st.guaranteedBonechill = chillSkills(st.db, st.skills)
	return nil
}

// stepEarlyCharges: CalcPerform.lua "Calculate charges early to enable
// usage of stats that depend on charge count" (doActorCharges).
func stepEarlyCharges(_ *Calculator, st *calcState) error {
	charges(st.db)
	return nil
}

// stepBuffs: CalcPerform.lua "Combine buffs/debuffs" (Buff, Aura, ...).
func stepBuffs(c *Calculator, st *calcState) error {
	st.curses, st.buffs = c.applyBuffs(st.db, st.skills, st.res)
	return nil
}

// stepCurses: CalcPerform.lua "Check for extra curses", then the curse
// slots.
func stepCurses(c *Calculator, st *calcState) error {
	st.curses = c.extraCurses(st.db, st.curses, st.res)
	c.applyCurses(st.db, st.skills, st.curses, st.res)
	return nil
}

// stepExtraAuras: impale stacks (before ExtraAura, as in calcs.perform),
// CalcPerform.lua ExtraAura, and keystones merged again.
func stepExtraAuras(c *Calculator, st *calcState) error {
	db := st.db
	impaleStacks(db)
	hallowingFlame(db, mainSkill(st.skills))
	extraAuras(db, mainSkill(st.skills))
	db.party.partyExtraAuras(db)
	c.mergeKeystones(db, st.keystones, st.res)
	return nil
}

// hallowingFlame is CalcPerform.lua:3445: with Condition:CanInflictHallowingFlame
// (for the main skill), attack hits against an enemy with Hallowing Flame
// gain 25% (scaled by its magnitude) of physical damage as extra fire per
// stack, as an ExtraAura for the player and its allies.
func hallowingFlame(db *ModDB, main *activeSkill) {
	var cfg *Cfg
	if main != nil {
		cfg = main.cfg
	}
	if !db.FlagCfg(cfg, "Condition:CanInflictHallowingFlame") {
		return
	}
	magnitude, ok := db.Override("HallowingFlameMagnitude")
	if !ok {
		magnitude = db.Sum("INC", "HallowingFlameMagnitude")
	}
	val := math.Floor(25 * (1 + magnitude/100))
	mod := map[string]any{
		"name": "PhysicalDamageGainAsFire", "type": "BASE", "value": val, "flags": 0.0, "keywordFlags": 0.0,
		"@list": []any{
			map[string]any{"type": "GlobalEffect", "effectType": "Global", "unscalable": true},
			map[string]any{"type": "ActorCondition", "actor": "enemy", "var": "HallowingFlame"},
			map[string]any{"type": "Multiplier", "var": "HallowingFlame", "actor": "enemy"},
		}}
	db.Add(Mod{Name: "ExtraAura", Type: modTypeList, Value: map[string]any{"mod": mod}, Source: "Hallowing Flame"})
}

// stepDisableWeapons: CalcPerform.lua Dancing Dervish (DisableWeapons);
// playerWeapons makes the main hand unarmed, and the player is
// Unencumbered without gloves. PoB differs: it checks env.player.Gloves,
// which is never set, so it is always Unencumbered. The Dervish's own
// global mods (Rampage) still apply to the player, as in game.
func stepDisableWeapons(_ *Calculator, st *calcState) error {
	db := st.db
	if db.Flag("DisableWeapons") {
		db.conditions["Unarmed"] = true
		if db.items["Gloves"] == nil {
			db.conditions["Unencumbered"] = true
		}
	}
	return nil
}

// stepDefenceForConditionals: CalcPerform.lua calcs.defenceForConditionals.
func stepDefenceForConditionals(_ *Calculator, st *calcState) error {
	defenceForConditionals(st.db, st.gearMods)
	return nil
}

// stepCharges: doActorCharges, flask recovery and CalcPerform.lua "Process
// stats from alternate charges" (3158) before the guaranteed chills
// (3414): Affliction Charges' more chill effect scales them.
func stepCharges(_ *Calculator, st *calcState) error {
	db := st.db
	charges(db)
	flaskRecovery(db, st.flaskSt)
	alternateCharges(db)
	guaranteedChills(db, st.chilling)
	return nil
}

// stepActorMisc: CalcPerform.lua doActorMisc for the player and the enemy,
// rage, the second "Process enemy modifiers" pass, enemy ailments,
// exposures and consecrated ground.
func stepActorMisc(_ *Calculator, st *calcState) error {
	db, skills := st.db, st.skills
	actorMisc(db, mainSkill(skills), st.flaskSt.silver, st.flaskSt.silverInc)
	rage(db, mainCfg(skills))
	st.playerEnemyMods.apply(db)
	enemyAilments(db, skills, st.guaranteedBonechill)
	actorMisc(db.enemy, nil, false, -100)
	exposures(db)
	consecratedGround(db)
	return nil
}

// stepSkillInfo fills Result.Skills (no PoB counterpart; the skills tab).
func stepSkillInfo(_ *Calculator, st *calcState) error {
	st.res.Skills = skillInfo(st.skills, st.b.ActiveSkills())
	for i := range st.res.Skills {
		st.res.Skills[i].Group = st.res.callerGroup(st.res.Skills[i].Group)
	}
	return nil
}

// stepAllyLife: life-based mods from allies (CalcPerform.lua's minion and
// party life buffs).
func stepAllyLife(c *Calculator, st *calcState) error {
	c.allyLifeMods(st.db, st.skills, st.buffs, st.b.Level, st.res)
	return nil
}

// stepDefence: CalcPerform.lua calcs.defence and
// calcs.buildDefenceEstimations for the player.
func stepDefence(c *Calculator, st *calcState) error {
	db := st.db
	c.resistances(db)
	c.defence(db, st.gearMods, st.keystones)
	avoidanceAndRecovery(db)
	recoup(db)
	buildDefenceEstimations(db)
	return nil
}

// stepMinion: the main skill's minion (CalcPerform.lua env.minion).
func stepMinion(c *Calculator, st *calcState) error {
	st.res.Minion = c.mainMinion(st.db, st.skills, st.buffs, st.b.Level, st.res)
	return nil
}

// stepOffence: CalcPerform.lua calcs.triggers, calcs.mirages and
// calcs.offence for the player's main skill.
func stepOffence(c *Calculator, st *calcState) error {
	db, res := st.db, st.res
	res.main = mainSkill(st.skills)
	if s := res.main; s != nil {
		w1, w2 := playerWeapons(db, st.gearMods, st.spec.ClassID)
		c.triggers(db, s, w1, w2)
		if truthy(s.data.val("triggeredByGeneralsCry")) && !s.disabled {
			c.generalsCry(db, s, w1, w2)
		}
		c.mirages(db, s, w1, w2, res)
		for k, v := range c.offence(db, s, w1, w2) {
			db.output[k] = v //nolint:modernize // kept as moved from Calc
		}
		if debugPlayerSkill != nil {
			debugPlayerSkill(s)
		}
		gemLevelQuality(db, s)
		// CalcOffence.lua:2025: env.keystonesAdded, attacks only (nil when
		// the keystone was not added).
		if s.flags["attack"] && st.keystones["Precise Technique"] {
			db.output["PreciseTechnique"] = 1
		}
	}
	res.Incomplete = append(res.Incomplete, db.incomplete...)
	return nil
}

// stepUnsupportedTags copies the tag types left out to the Result.
func stepUnsupportedTags(_ *Calculator, st *calcState) error {
	res := st.res
	for k, v := range st.db.UnsupportedTags {
		if res.UnsupportedTags == nil {
			res.UnsupportedTags = map[string]int{}
		}
		res.UnsupportedTags[k] = v
	}
	return nil
}
