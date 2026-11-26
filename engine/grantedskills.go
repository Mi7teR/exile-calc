package engine

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// extraSkill is the ExtraSkill mod name.
const extraSkill = "ExtraSkill"

// grantedSkill is one entry of env.grantedSkills (CalcSetup.lua:1478-1505):
// a skill granted by an allocated node, an equipped item or a Config tab
// custom modifier.
type grantedSkill struct {
	source   string // "Tree:<id>", the item's modSource, or "Config"
	slot     string // grantedSkill.slotName: the item's slot, "" otherwise
	id       string
	nameSpec string
	level    int
	// reqMult and reqCount are itemRequirement: the grant holds only with
	// at least reqCount of the reqMult item multiplier (reqMult "" = none).
	reqMult  string
	reqCount float64
}

// withGrantedSkills ports CalcSetup.lua's "Process extra skills granted by
// items or tree nodes" (1506-1620, MAIN mode only) on the active skill set
// of st.b (applyGrantedSkills): every granted skill gets a socket group,
// so does a non-empty explodeSources (the "Explode" group), and saved
// groups with a source that nothing took are removed. It also returns
// each resulting group's index in st.b (callerGroups), nil when b is
// unchanged.
func (c *Calculator) withGrantedSkills(st *calcState, explode []string) (*pobcode.Build, []int) {
	return applyGrantedSkills(st.b, c.grantedSkills(st), explode, c.data.Skills.Effects)
}

// grantedSkills is env.grantedSkills: the nodes' grants, then the items',
// then the Config tab's (CalcSetup.lua:1478-1505), without the grants
// whose itemRequirement the equipped items do not meet.
func (c *Calculator) grantedSkills(st *calcState) []grantedSkill {
	db := st.db
	grants := treeGrantedSkills(db)
	grants = append(grants, c.itemGrantedSkills(st)...)
	// CalcSetup.lua:1491-1499: influence-gated curses entered in Custom
	// Modifiers (configTab.modList ExtraSkill with itemRequirement).
	for _, e := range db.List(nil, extraSkill) {
		if !strings.HasPrefix(e.Mod.Source, "Custom:") {
			continue
		}
		v, _ := e.Value.(map[string]any)
		g := extraSkillGrant(v, "Config", "")
		ge := c.data.Skills.Effects[g.id]
		if g.reqMult == "" || ge == nil {
			continue
		}
		g.nameSpec = ge.Name
		grants = append(grants, g)
	}
	// CalcSetup.lua:1500-1506: only while enough items of the required
	// influence are equipped (env.itemModDB:GetMultiplier).
	return slices.DeleteFunc(grants, func(g grantedSkill) bool {
		return g.reqMult != "" && db.multipliers[g.reqMult] < g.reqCount
	})
}

// treeGrantedSkills is env.grantedSkillsNodes (CalcSetup.lua:1478-1487):
// the ExtraSkill mods of the allocated nodes (node.grantedSkills,
// CalcSetup.lua:251-263, source "Tree:<id>"). env.allocNodes is walked
// with pairs (no order); node id order keeps the result deterministic.
func treeGrantedSkills(db *ModDB) []grantedSkill {
	type nodeGrant struct {
		node int
		g    grantedSkill
	}
	var list []nodeGrant
	for _, e := range db.List(nil, extraSkill) {
		idStr, ok := strings.CutPrefix(e.Mod.Source, "Tree:")
		if !ok {
			continue
		}
		node, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		v, _ := e.Value.(map[string]any)
		g := extraSkillGrant(v, e.Mod.Source, "")
		// A node's grantedSkills entry copies only skillId, level and
		// source (CalcSetup.lua:255-259).
		g.reqMult, g.reqCount = "", 0
		if g.id == "" {
			continue
		}
		list = append(list, nodeGrant{node, g})
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].node < list[j].node })
	out := make([]grantedSkill, len(list))
	for i, n := range list {
		out[i] = n.g
	}
	return out
}

// extraSkillGrant reads an ExtraSkill value.
func extraSkillGrant(v map[string]any, source, slot string) grantedSkill {
	g := grantedSkill{source: source, slot: slot}
	g.id, _ = v["skillId"].(string)
	lvl, _ := num(v["level"])
	g.level = int(lvl)
	if r, ok := v["itemRequirement"].(map[string]any); ok {
		g.reqMult, _ = r["multiplier"].(string)
		g.reqCount, _ = num(r["count"])
	}
	return g
}

// grantSlotOrder is ItemsTab.lua's orderedSlots before the tree jewel
// sockets: baseSlots (37), each weapon slot followed by its swap slot and
// the swap slot's abyssal sockets (161-176), the abyssal sockets of the
// slots that have them (186-199), then the Wickerman slots (203-212).
//
//nolint:goconst // the slot names as ItemsTab.lua lists them
func grantSlotOrder() []string {
	var out []string
	for _, s := range []string{
		"Weapon 1", "Weapon 2", "Helmet", "Body Armour", "Gloves", "Boots", "Amulet", "Ring 1", "Ring 2", "Ring 3",
		"Belt", "Graft 1", "Graft 2", "Flask 1", "Flask 2", "Flask 3", "Flask 4", "Flask 5",
	} {
		out = append(out, s)
		if strings.HasPrefix(s, "Weapon") {
			out = append(out, s+" Swap")
			for i := 1; i <= abyssalSlots; i++ {
				out = append(out, fmt.Sprintf("%s Swap Abyssal Socket %d", s, i))
			}
		}
		switch s {
		case "Weapon 1", "Weapon 2", "Helmet", "Gloves", "Body Armour", "Boots", "Belt":
			for i := 1; i <= abyssalSlots; i++ {
				out = append(out, fmt.Sprintf("%s Abyssal Socket %d", s, i))
			}
		}
	}
	return append(out, "Head", "Body", "Arm", "Leg")
}

// slotItem is an item in a slot of ItemsTab's orderedSlots.
type slotItem struct {
	slot string
	item *pobcode.Item
}

// orderedSlotItems is the items of b's active item set in grantSlotOrder,
// then the tree's jewel sockets by node id (ItemsTab.lua:229-246), which
// hold the active spec's jewels.
func orderedSlotItems(b *pobcode.Build) []slotItem {
	var out []slotItem
	if set := b.ActiveItems(); set != nil {
		for _, name := range grantSlotOrder() {
			if id := set.SlotItem(name); id != 0 {
				out = append(out, slotItem{name, b.Item(id)})
			}
		}
	}
	if spec := b.ActiveSpec(); spec != nil {
		sockets := slices.Clone(spec.Sockets)
		sort.SliceStable(sockets, func(i, j int) bool { return sockets[i].NodeID < sockets[j].NodeID })
		for _, s := range sockets {
			if s.ItemID != 0 {
				out = append(out, slotItem{"Jewel " + strconv.Itoa(s.NodeID), b.Item(s.ItemID)})
			}
		}
	}
	return out
}

// itemSlotsUsed is ItemsTab's orderedSlots as the item loop of
// CalcSetup.lua:811-849 reads them for env.grantedSkillsItems and
// env.explodeSources: before the weapon set and jewel socket checks, so the
// inactive weapon set's items and the jewels of unallocated sockets count
// too. Graft slots count only on a 3.27 tree, slots named "Wickerman ..."
// only on a 3.30 tree (816-823), and Ring 3 only with AdditionalRingSlot
// from the allocated nodes (825).
func itemSlotsUsed(st *calcState) []slotItem {
	tree := st.spec.TreeVersion
	ringSlot := additionalRingSlot(st.db)
	var out []slotItem
	for _, si := range orderedSlotItems(st.b) {
		name := si.slot
		// The "^Wickerman " test is dead in PoB too: the Wickerman slots
		// are named "Head", "Body", "Arm" and "Leg" (ItemsTab.lua:208).
		if (strings.HasPrefix(name, "Graft ") && !strings.Contains(tree, "3_27")) ||
			(strings.HasPrefix(name, "Wickerman ") && !strings.Contains(tree, "3_30")) ||
			(name == "Ring 3" && !ringSlot) || si.item == nil {
			continue
		}
		out = append(out, si)
	}
	return out
}

// itemGrantedSkills is env.grantedSkillsItems (CalcSetup.lua:841-848): the
// grantedSkills of the items of itemSlotsUsed, with their slot names.
func (c *Calculator) itemGrantedSkills(st *calcState) []grantedSkill {
	var out []grantedSkill
	for _, si := range itemSlotsUsed(st) {
		for _, g := range c.itemInfo(si.item, st.compared).grants {
			g.slot = si.slot
			out = append(out, g)
		}
	}
	return out
}

// explodeSources is env.explodeSources as mod sources (the key the
// EnemyExplode gems match ExplodeMod by, Data/Skills/other.lua
// explodeSource.modSource or "Tree:"..id): the allocated nodes for which
// buildModListForNode returned the node as its explode result
// (st.explodeNodes, nodeCanExplode; buildModListForNodeList,
// CalcSetup.lua:288-290, merged at 1462-1464), then the items of
// itemSlotsUsed whose baseModList has CanExplode (851-853; tableConcat puts
// the nodes first). A node counts by its own final mod list, so a radius
// jewel's mods count for the node in radius, not for the socket, and the
// unallocated nodes of extraRadiusNodeList (296-303) never count.
// env.allocNodes is walked with pairs, which has no order; node id order
// here keeps the result deterministic.
func (c *Calculator) explodeSources(st *calcState) []string {
	nodes := slices.Clone(st.explodeNodes)
	slices.Sort(nodes)
	nodes = slices.Compact(nodes)
	var out []string
	for _, id := range nodes {
		out = append(out, "Tree:"+strconv.Itoa(id))
	}
	for _, si := range itemSlotsUsed(st) {
		if info := c.itemInfo(si.item, st.compared); info.canExplode {
			out = append(out, info.source)
		}
	}
	return out
}

// itemGrantInfo is what the granted skill block reads of an item.
type itemGrantInfo struct {
	source     string         // item.modSource
	grants     []grantedSkill // item.grantedSkills, without slot names
	canExplode bool           // item.baseModList:Flag(nil, "CanExplode")
}

// itemInfo is itemGrantInfo for item, from the cache when it is there.
// The result is shared: callers must not modify it. The Comparer's
// compared item (id compared, 0 = none) is parsed and never cached: each
// comparison is a one-off key that would fill the cache.
func (c *Calculator) itemInfo(item *pobcode.Item, compared int) itemGrantInfo {
	if compared != 0 && item.ID == compared {
		return c.parseItemInfo(item)
	}
	key := grantKey(item)
	if info, ok := c.grants.load(key); ok {
		return info
	}
	info := c.parseItemInfo(item)
	c.grants.store(key, info)
	return info
}

// parseItemInfo parses item for itemGrantInfo. item.grantedSkills is
// Item.lua:2666-2679: baseList:List(nil, "ExtraSkill") (no item line PoB
// parses gives ExtraSkill tags, so every one counts), with nameSpec the
// skill's name (CalcSetup.lua:844).
func (c *Calculator) parseItemInfo(item *pobcode.Item) itemGrantInfo {
	it, mods := c.itemMods(item, &Result{})
	if it == nil {
		return itemGrantInfo{}
	}
	info := itemGrantInfo{source: itemSource(item, it)}
	for _, m := range mods {
		switch m.Name {
		case "CanExplode":
			info.canExplode = info.canExplode || (m.Value == true && len(m.Tags) == 0)
		case extraSkill:
			v, _ := m.Value.(map[string]any)
			g := extraSkillGrant(v, info.source, "")
			if g.id == "" {
				continue
			}
			if ge := c.data.Skills.Effects[g.id]; ge != nil {
				g.nameSpec = ge.Name
			}
			info.grants = append(info.grants, g)
		}
	}
	return info
}

// grantKey is what parseItemInfo reads of an item: its id (in the source),
// variants, mod ranges and text.
func grantKey(item *pobcode.Item) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d %d %v %v\n", item.ID, item.Variant, item.VariantAlt, item.ModRanges)
	sb.WriteString(item.Raw)
	return sb.String()
}

// grantCache maps grantKey to parseItemInfo's result. Every Calc reads
// all the build's items, and FullDPS, CalcMinions and the Comparer
// calculate the same items many times.
type grantCache struct {
	m      sync.Map // string -> itemGrantInfo
	cached atomic.Int64
}

// grantCacheCap bounds the cache, as modparser's parseCacheCap does, for
// a long-running consumer that sees arbitrary items.
const grantCacheCap = 10_000

func (gc *grantCache) load(key string) (itemGrantInfo, bool) {
	v, ok := gc.m.Load(key)
	if !ok {
		return itemGrantInfo{}, false
	}
	info, _ := v.(itemGrantInfo)
	return info, true
}

// store caches info unless the cache is full; a slot is reserved before
// the insert, so concurrent misses never take the count past the cap.
func (gc *grantCache) store(key string, info itemGrantInfo) {
	if gc.cached.Load() >= grantCacheCap {
		return
	}
	if gc.cached.Add(1) > grantCacheCap {
		gc.cached.Add(-1)
		return
	}
	if _, loaded := gc.m.LoadOrStore(key, info); loaded {
		gc.cached.Add(-1)
	}
}

// additionalRingSlot is env.initialNodeModDB:Flag(nil, "AdditionalRingSlot")
// (CalcSetup.lua:825): the flag from an allocated node.
func additionalRingSlot(db *ModDB) bool {
	for _, m := range db.mods["AdditionalRingSlot"] {
		if strings.HasPrefix(m.Source, "Tree:") && m.Value == true && len(m.Tags) == 0 {
			return true
		}
	}
	return false
}

// explodeGroupLabel is the label of the group CalcSetup.lua:1576 creates.
const explodeGroupLabel = "On Kill Monster Explosion"

// applyGrantedSkills is CalcSetup.lua:1523-1619 on the active skill set of
// b:
//   - each grant takes the first group with its source, slot and skill at
//     its level (raw or validated, getNormalizedSkillLevel), or a new group
//     appended to the list (t_insert); that group's gem list becomes its
//     first gem (or a new one) at the granted level (1523-1562);
//   - with explode sources, the first "Explode" group, or a new one, holds
//     one EnemyExplode gem per source (1564-1605);
//   - every group with a source that neither took is removed (1607-1619).
//
// It returns b unchanged when nothing changes; otherwise a copy with its
// own skill set and group slices that shares the rest.
//
// The second result maps each resulting group to its index in b (both
// 1-based, callerGroups[i-1] for group i): a kept group has its own index,
// the k-th appended group len(b's groups)+k. It is nil when b is returned
// unchanged (the identity).
//
// Quirk (follows the game, not PoB): b.MainSocketGroup is read in b's own
// list, a kept group's index or len+k for the k-th appended group, and
// moved to where that group ends up, so the main skill stays the one the
// player chose when groups before it are removed. PoB does not adjust it
// (CalcSetup.lua:1607-1619 only clears skillsTab.displayGroup, and
// 1652-1653 clamp build.mainSocketGroup to the new length), so its main
// skill moves to a later group. When the main group itself is removed, or
// the index names no group, MainSocketGroup is kept and clamped as PoB
// does.
func applyGrantedSkills(
	b *pobcode.Build,
	grants []grantedSkill,
	explode []string,
	skills map[string]*gamedata.Skill,
) (*pobcode.Build, []int) {
	set := b.ActiveSkills()
	if set == nil {
		return b, nil
	}
	n := len(set.Groups)
	l := &groupList{groups: slices.Clone(set.Groups), marked: slices.Repeat([]bool{false}, n)}
	for i := range n {
		l.caller = append(l.caller, i+1)
	}
	for _, gr := range grants {
		l.grant(gr, skills)
	}
	if len(explode) > 0 {
		l.explode(len(explode))
	}
	// "Remove any socket groups that no longer have a matching item".
	newMain := b.MainSocketGroup
	kept := make([]pobcode.SocketGroup, 0, len(l.groups))
	caller := make([]int, 0, len(l.groups))
	for i, g := range l.groups {
		if g.Source != "" && !l.marked[i] {
			continue
		}
		kept = append(kept, g)
		caller = append(caller, l.caller[i])
		if l.caller[i] == b.MainSocketGroup {
			newMain = len(kept) // the quirk above
		}
	}
	if !l.changed && len(kept) == n {
		return b, nil
	}
	cp := *b
	cp.SkillSets = slices.Clone(b.SkillSets)
	cp.ActiveSkills().Groups = kept
	cp.MainSocketGroup = newMain
	return &cp, caller
}

// groupList is build.skillsTab.socketGroupList while applyGrantedSkills
// works on it: marked is markList, changed whether a group changed.
type groupList struct {
	groups  []pobcode.SocketGroup
	marked  []bool
	caller  []int // each group's index in the input build
	changed bool
}

// add appends a new group (t_insert) and marks it.
func (l *groupList) add(g pobcode.SocketGroup) int {
	l.groups = append(l.groups, g)
	l.marked = append(l.marked, true)
	l.caller = append(l.caller, len(l.caller)+1)
	return len(l.groups) - 1
}

// grant is one grantedSkill of CalcSetup.lua:1523-1562.
func (l *groupList) grant(gr grantedSkill, skills map[string]*gamedata.Skill) {
	idx := slices.IndexFunc(l.groups, func(g pobcode.SocketGroup) bool {
		return grantMatches(&g, gr, skills)
	})
	if idx < 0 {
		// group = { label = "", enabled = true, gemList = { }, source = ..., slot = ... }
		idx = l.add(pobcode.SocketGroup{Enabled: true, Source: gr.source, Slot: gr.slot})
	}
	l.marked[idx] = true
	g := &l.groups[idx]
	// activeGemInstance = group.gemList[1] or { skillId, nameSpec,
	// quality = 0, enabled = true }; gemId = nil; level =
	// grantedSkill.level; enableGlobal1 = true; the gem list is wiped and
	// holds only this gem (1546-1561).
	gem := pobcode.Gem{SkillID: gr.id, NameSpec: gr.nameSpec, Enabled: true}
	if len(g.Gems) > 0 {
		gem = g.Gems[0]
	}
	gem.GemID = ""
	gem.Level = gr.level
	// build.skillsTab:ProcessSocketGroup(group) (1562) validates the level
	// (SkillsTab.lua:1235).
	if ge := skills[gem.SkillID]; ge != nil {
		gem.Level = validateLevel(gem.Level, ge, nil)
	}
	gem.EnableGlobal1 = true
	if len(g.Gems) != 1 || g.Gems[0] != gem {
		g.Gems = []pobcode.Gem{gem}
		l.changed = true
	}
}

// explode is CalcSetup.lua:1564-1605 for n explode sources: the first
// "Explode" group, or a new one, holds one EnemyExplode gem per source.
func (l *groupList) explode(n int) {
	idx := slices.IndexFunc(l.groups, func(g pobcode.SocketGroup) bool { return g.Source == "Explode" })
	if idx < 0 {
		// { label = "On Kill Monster Explosion", enabled = true, gemList = { }, source = "Explode", noSupports = true }
		idx = l.add(pobcode.SocketGroup{Label: explodeGroupLabel, Enabled: true, Source: "Explode"})
	}
	l.marked[idx] = true
	// gemsBySource reuses gems by their explodeSource, which a saved build
	// does not carry (SkillsTab.lua saves no explodeSource), so every gem
	// is a new { skillId = "EnemyExplode", quality = 0, enabled = true,
	// level = 1, triggered = true } (1586-1600).
	gems := slices.Repeat([]pobcode.Gem{{SkillID: "EnemyExplode", Level: 1, Enabled: true}}, n)
	if !slices.Equal(l.groups[idx].Gems, gems) {
		l.groups[idx].Gems = gems
		l.changed = true
	}
}

// grantMatches is CalcSetup.lua:1527-1528: the group has the grant's source
// and slot and the granted skill first, at the granted level or at that
// level validated (getNormalizedSkillLevel, 1509-1518).
func grantMatches(g *pobcode.SocketGroup, gr grantedSkill, skills map[string]*gamedata.Skill) bool {
	if g.Source != gr.source || g.Slot != gr.slot || len(g.Gems) == 0 || gr.id == "" || g.Gems[0].SkillID != gr.id {
		return false
	}
	lvl := g.Gems[0].Level
	return lvl == gr.level || (skills[gr.id] != nil && lvl == validateLevel(gr.level, skills[gr.id], nil))
}

// carryGrantedGroups gives the Comparer's new item (next, in slot) the
// granted skill groups of the item it replaces (old): each group of old's
// source in slot whose skill next grants too takes next's source and
// granted level, so the granted skill block matches it and the group
// keeps its settings (enabled, label, Full DPS, count, main skill and the
// main socket group's index). The game ties a skill to the item that
// grants it, and the same item gives the same skill; without this the
// compared item, which has a new id and so a new source, would get a new
// group and the old one would be removed. b is returned unchanged when no
// group carries over; otherwise a copy with its own skill set and group
// slices.
func (c *Calculator) carryGrantedGroups(b *pobcode.Build, slot string, old, next *pobcode.Item) *pobcode.Build {
	set := b.ActiveSkills()
	if set == nil || old == nil || next == nil {
		return b
	}
	from := c.itemInfo(old, 0)
	to := c.parseItemInfo(next) // a compared item: not cached
	if from.source == "" || to.source == "" || len(to.grants) == 0 {
		return b
	}
	var groups []pobcode.SocketGroup
	for i, g := range set.Groups {
		if g.Source != from.source || g.Slot != slot || len(g.Gems) == 0 {
			continue
		}
		k := slices.IndexFunc(to.grants, func(gr grantedSkill) bool { return gr.id == g.Gems[0].SkillID })
		if k < 0 {
			continue
		}
		if groups == nil {
			groups = slices.Clone(set.Groups)
		}
		groups[i].Source = to.source
		groups[i].Gems = slices.Clone(g.Gems)
		groups[i].Gems[0].Level = to.grants[k].level
	}
	if groups == nil {
		return b
	}
	cp := *b
	cp.SkillSets = slices.Clone(b.SkillSets)
	cp.ActiveSkills().Groups = groups
	return &cp
}
