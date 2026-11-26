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
// of st.b: every granted skill gets a socket group, and saved groups with a
// source that no grant matched are removed. b is returned unchanged when
// nothing changes; otherwise the copy has its own skill set and group
// slices and shares the rest.
//
// Removing a group shifts the groups after it, and PoB does not adjust
// build.mainSocketGroup for it: CalcSetup.lua:1607-1619 only clears
// skillsTab.displayGroup, and 1652-1653 clamps mainSocketGroup to the new
// list's length. The saved main socket group index is kept here as well,
// so it selects the group at that index in the returned build.
//
// The "Explode" group (1564-1605, the EnemyExplode gems of
// env.explodeSources) is not built: a saved Explode group is kept as it is,
// as when env.explodeSources is not empty.
func (c *Calculator) withGrantedSkills(st *calcState) *pobcode.Build {
	return applyGrantedSkills(st.b, c.grantedSkills(st), c.data.Skills.Effects)
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

// itemGrantedSkills is env.grantedSkillsItems (CalcSetup.lua:811-849): the
// grantedSkills of the item in every slot of ItemsTab's orderedSlots, read
// before the weapon set and jewel socket checks, so the inactive weapon
// set's items and the jewels of unallocated sockets grant theirs too. Graft
// slots count only on a 3.27 tree, slots named "Wickerman ..." only on a
// 3.30 tree (816-823), and Ring 3 only with AdditionalRingSlot from the
// allocated nodes (825).
func (c *Calculator) itemGrantedSkills(st *calcState) []grantedSkill {
	tree := st.spec.TreeVersion
	ringSlot := additionalRingSlot(st.db)
	var out []grantedSkill
	for _, si := range orderedSlotItems(st.b) {
		name := si.slot
		if (strings.HasPrefix(name, "Graft ") && !strings.Contains(tree, "3_27")) ||
			(strings.HasPrefix(name, "Wickerman ") && !strings.Contains(tree, "3_30")) ||
			(name == "Ring 3" && !ringSlot) || si.item == nil {
			continue
		}
		out = append(out, c.itemGrants(si)...)
	}
	return out
}

// itemGrants is item.grantedSkills (Item.lua:2666-2679) with the slot
// (CalcSetup.lua:841-848): the item's ExtraSkill mods, baseList:List(nil,
// "ExtraSkill"), where a mod with tags does not evaluate without a cfg.
func (c *Calculator) itemGrants(si slotItem) []grantedSkill {
	key := grantKey(si.item)
	list, ok := c.grants.load(key)
	if !ok {
		list = c.parseItemGrants(si.item)
		c.grants.store(key, list)
	}
	out := slices.Clone(list)
	for i := range out {
		out[i].slot = si.slot
	}
	return out
}

// parseItemGrants is itemGrants without the slot, from the parsed item.
func (c *Calculator) parseItemGrants(item *pobcode.Item) []grantedSkill {
	it, mods := c.itemMods(item, &Result{})
	if it == nil {
		return nil
	}
	var out []grantedSkill
	for _, m := range mods {
		if m.Name != extraSkill || len(m.Tags) > 0 {
			continue
		}
		v, _ := m.Value.(map[string]any)
		g := extraSkillGrant(v, itemSource(item, it), "")
		if g.id == "" {
			continue
		}
		// nameSpec = skillData and skillData.name or nil.
		if ge := c.data.Skills.Effects[g.id]; ge != nil {
			g.nameSpec = ge.Name
		}
		out = append(out, g)
	}
	return out
}

// grantKey is what parseItemGrants reads of an item: its id (in the
// source), variants, mod ranges and text.
func grantKey(item *pobcode.Item) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%d %d %v %v\n", item.ID, item.Variant, item.VariantAlt, item.ModRanges)
	sb.WriteString(item.Raw)
	return sb.String()
}

// grantCache maps grantKey to parseItemGrants' result. Every Calc reads
// the grants of all the build's items, and FullDPS, CalcMinions and the
// Comparer calculate the same items many times. Entries are never handed
// out: itemGrants returns a copy.
type grantCache struct {
	m      sync.Map // string -> []grantedSkill
	cached atomic.Int64
}

// grantCacheCap bounds the cache, as modparser's parseCacheCap does, for
// a long-running consumer that sees arbitrary items.
const grantCacheCap = 10_000

func (gc *grantCache) load(key string) ([]grantedSkill, bool) {
	v, ok := gc.m.Load(key)
	if !ok {
		return nil, false
	}
	list, _ := v.([]grantedSkill)
	return list, true
}

// store caches list unless the cache is full; a slot is reserved before
// the insert, so concurrent misses never take the count past the cap.
func (gc *grantCache) store(key string, list []grantedSkill) {
	if gc.cached.Load() >= grantCacheCap {
		return
	}
	if gc.cached.Add(1) > grantCacheCap {
		gc.cached.Add(-1)
		return
	}
	if _, loaded := gc.m.LoadOrStore(key, list); loaded {
		gc.cached.Add(-1)
	}
}

// additionalRingSlot is env.initialNodeModDB:Flag(nil, "AdditionalRingSlot")
// (CalcSetup.lua:825): the flag from an allocated node.
func additionalRingSlot(db *ModDB) bool {
	for _, m := range db.mods["AdditionalRingSlot"] {
		if strings.HasPrefix(m.Source, "Tree:") && m.Value == true {
			return true
		}
	}
	return false
}

// applyGrantedSkills is CalcSetup.lua:1523-1562 and 1607-1619 on the active
// skill set of b: each grant takes the first group with its source, slot
// and skill at its level (raw or validated, getNormalizedSkillLevel), or a
// new group appended to the list (t_insert); that group's gem list becomes
// its first gem (or a new one) at the granted level; then every group with
// a source that no grant took is removed. "Explode" groups are kept (see
// withGrantedSkills).
func applyGrantedSkills(b *pobcode.Build, grants []grantedSkill, skills map[string]*gamedata.Skill) *pobcode.Build {
	set := b.ActiveSkills()
	if set == nil {
		return b
	}
	groups := slices.Clone(set.Groups)
	marked := slices.Repeat([]bool{false}, len(groups))
	changed := false
	for _, gr := range grants {
		idx := slices.IndexFunc(groups, func(g pobcode.SocketGroup) bool {
			return grantMatches(&g, gr, skills)
		})
		if idx < 0 {
			// group = { label = "", enabled = true, gemList = { }, source = ..., slot = ... }
			groups = append(groups, pobcode.SocketGroup{Enabled: true, Source: gr.source, Slot: gr.slot})
			marked = append(marked, true)
			idx = len(groups) - 1
		}
		marked[idx] = true
		g := &groups[idx]
		// activeGemInstance = group.gemList[1] or { skillId, nameSpec,
		// quality = 0, enabled = true }; gemId = nil; level =
		// grantedSkill.level; enableGlobal1 = true; the gem list is wiped
		// and holds only this gem (1546-1561).
		gem := pobcode.Gem{SkillID: gr.id, NameSpec: gr.nameSpec, Enabled: true}
		if len(g.Gems) > 0 {
			gem = g.Gems[0]
		}
		gem.GemID = ""
		gem.Level = gr.level
		// build.skillsTab:ProcessSocketGroup(group) (1562) validates the
		// level (SkillsTab.lua:1235).
		if ge := skills[gem.SkillID]; ge != nil {
			gem.Level = validateLevel(gem.Level, ge, nil)
		}
		gem.EnableGlobal1 = true
		if len(g.Gems) != 1 || g.Gems[0] != gem {
			g.Gems = []pobcode.Gem{gem}
			changed = true
		}
	}
	// "Remove any socket groups that no longer have a matching item".
	kept := make([]pobcode.SocketGroup, 0, len(groups))
	for i, g := range groups {
		if g.Source != "" && g.Source != "Explode" && !marked[i] {
			continue
		}
		kept = append(kept, g)
	}
	if !changed && len(kept) == len(set.Groups) {
		return b
	}
	cp := *b
	cp.SkillSets = slices.Clone(b.SkillSets)
	cp.ActiveSkills().Groups = kept
	return &cp
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
