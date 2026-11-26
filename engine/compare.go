package engine

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// Errors Comparer.Item wraps when the item cannot be compared in the slot.
var (
	// ErrItemNotValidForSlot: the item cannot go in the slot
	// (ItemsTabClass:IsItemValidForSlot, Classes/ItemsTab.lua:2639-2692),
	// such as a ring in "Helmet", a two-hander in "Weapon 2" or a cluster
	// jewel bigger than its socket.
	ErrItemNotValidForSlot = errors.New("engine: item does not fit the slot")
	// ErrSlotNotAvailable: the build has no such slot, as PoB hides it
	// (Classes/ItemsTab.lua:178-185, 203-212): "Ring 3" without a "+1 Ring
	// Slot" passive, "Graft 1"/"Graft 2" on a tree other than 3.27, the
	// Wickerman slots on a tree other than 3.30.
	ErrSlotNotAvailable = errors.New("engine: the build has no such slot")
)

// Comparer compares items against one build, like the stat comparison of
// PoB's item tooltip (Classes/ItemsTab.lua AddItemTooltip with
// calcs.getMiscCalculator, Modules/Calcs.lua): the build is computed once,
// then each Item call computes it again with one slot replaced.
//
// A Comparer is safe for concurrent use. It keeps the calculated build
// (Result.Build), which shares its parts with the build it was made from,
// and reads it on every Item call, so that build must not be modified
// while the Comparer is in use.
type Comparer struct {
	c      *Calculator
	b      *pobcode.Build
	before map[string]float64
	// unparsed and incomplete are the base Result's.
	unparsed, incomplete []string
	// usedFullDPS is getMiscCalculator's usedFullDPS: the base Full DPS
	// has skills, so every comparison computes Full DPS too.
	usedFullDPS bool
	// ringSlot is the base build's AdditionalRingSlot flag from the
	// allocated nodes: whether "Ring 3" exists.
	ringSlot bool
}

// ItemComparison is the change from putting one item in one slot.
//
// Delta is exact (no rounding); round it for display. A stat equal on
// both sides (the same infinity included) has no entry, and neither has
// one whose difference is NaN (NaN on either side).
type ItemComparison struct {
	Before, After map[string]float64 // Result.Stats (+ "FullDPS", "FullDotDPS" when used)
	Delta         map[string]float64 // After - Before, only non-zero
	Replaced      []string           // slots the new item emptied or replaced ("Weapon 2" for a two-hander)
	// Unparsed and Incomplete are the entries of the after build's
	// Result.Unparsed and Result.Incomplete that the base build's Result
	// does not have, in order: mod lines of the new item the parser did
	// not understand, parts it made the engine leave out. Non-empty means
	// After (and so Delta) may be incomplete.
	Unparsed, Incomplete []string
}

// NewComparer computes b once (Calc, and Full DPS when b has skills in
// it) as the base of later Item calls. b is not modified.
func (c *Calculator) NewComparer(b *pobcode.Build) (*Comparer, error) {
	res, err := c.Calc(b)
	if err != nil {
		return nil, err
	}
	full, err := c.fullDPSFrom(res)
	if err != nil {
		return nil, err
	}
	cp := &Comparer{
		c: c, b: res.build, usedFullDPS: len(full.Skills) > 0, ringSlot: additionalRingSlot(res.DB),
		unparsed: slices.Clone(res.Unparsed), incomplete: slices.Clone(res.Incomplete),
	}
	cp.before = cp.stats(res, full)
	return cp, nil
}

// stats is the output a comparison reads: Result.Stats plus, when the
// build uses Full DPS, getMiscCalculator's output.FullDPS and FullDotDPS.
func (cp *Comparer) stats(res *Result, full *FullDPSResult) map[string]float64 {
	out := maps.Clone(res.Stats)
	if out == nil {
		out = map[string]float64{}
	}
	if cp.usedFullDPS {
		out["FullDPS"], out["FullDotDPS"] = full.FullDPS, full.FullDotDPS
	}
	return out
}

// Item computes the build with itemText in slot and returns the stats
// before and after. When the build uses Full DPS, it runs one Calc per
// Full DPS skill. itemText is an item as the game copies it (Ctrl+C,
// Ctrl+Alt+C) or as PoB exports it. slot is a PoB slot name: "Weapon 1",
// "Weapon 2", "Helmet", "Body Armour", "Gloves", "Boots", "Amulet",
// "Ring 1".."Ring 3", "Belt", "Flask 1".."Flask 5", "Graft 1", "Graft 2",
// the Wickerman slots ("Head", "Body", "Arm", "Leg"), "Weapon 1 Swap",
// "Weapon 2 Swap", abyssal sockets ("Helmet Abyssal Socket 1") and tree
// jewel sockets ("Jewel 26725"), the jewel sockets a cluster jewel opens
// included: those are tree nodes too, named by their node id as in PoB
// (ItemsTab.lua:233-246). A slot the build does not use (the inactive
// weapon set, an unallocated jewel socket, an abyssal socket the item in
// its parent slot does not have) gives no change, as in PoB.
//
// The item must fit the slot as in PoB's slot lists
// (ItemsTab.lua IsItemValidForSlot), or Item returns an error wrapping
// ErrItemNotValidForSlot: the item type names the slot ("Ring" for
// "Ring 1".."Ring 3"), a Tincture goes in a Flask slot, an abyss jewel in
// an abyssal socket, any weapon in "Weapon 1"; "Weapon 2" takes what
// the set's "Weapon 1" allows (a shield or one-hander with a one-hander or
// nothing, wands only with a wand, a quiver only with a bow, nothing with
// another two-hander); a jewel socket takes jewels, a charm socket only
// charms, a cluster jewel only a cluster socket of its size or larger. A
// slot PoB hides for the build ("Ring 3" without a "+1 Ring Slot"
// passive, the Graft slots off a 3.27 tree, the Wickerman slots off a
// 3.30 tree) gives an error wrapping ErrSlotNotAvailable.
//
// Gems socketed in the slot are kept, as in PoB, where socket groups are
// tied to slot names, not items (CalcSetup.lua's socket group loop
// before "Process support gems"); that includes the gems of an offhand
// a two-hander empties. The item already in the slot, given again, gives
// an empty Delta (PoB's tooltip shows "Removing this item" for it
// instead, ItemsTab.lua AddItemTooltip).
//
// Skills granted by items count as in the game, not as in PoB's tooltip:
// the item's granted skills get socket groups and those of the item it
// replaces lose theirs (CalcSetup.lua's granted skill block runs for the
// compared build). PoB computes the tooltip with getMiscCalculator in
// CALCULATOR mode, which skips that block (CalcSetup.lua:1506), so there a
// new "Curse Enemies with ... on Hit" ring adds no curse and a replaced
// one keeps its curse.
//
// As CalcSetup.lua's override.repSlotName/repItem (calcs.initEnv), a
// Staff or two-handed sword, axe or mace in a "Weapon 1" slot empties the
// set's "Weapon 2", and so does a bow unless "Weapon 2" holds a quiver.
func (cp *Comparer) Item(slot string, itemText string) (*ItemComparison, error) {
	if err := cp.checkSlot(slot); err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(itemText, "\r", "")
	it, err := cp.c.items.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("engine: item: %w", err)
	}
	if it.Base == nil {
		return nil, errors.New("engine: item: unknown item base")
	}
	if !cp.itemValidForSlot(it, slot) {
		return nil, fmt.Errorf("%w: %s in %q", ErrItemNotValidForSlot, it.BaseName, slot)
	}
	b, replaced, err := cp.compareBuild(slot, normaliseItemText(text), it.Base.Type)
	if err != nil {
		return nil, err
	}
	// A full (MAIN) Calc, not getMiscCalculator's CALCULATOR pass: the
	// granted skill block runs, so the item's granted skills count (see
	// the Item doc).
	res, err := cp.c.calc(b, calcOpts{compared: b.Items[len(b.Items)-1].ID})
	if err != nil {
		return nil, err
	}
	// As Calc, for the same stats as before.
	if err = cp.c.buildOutput(res); err != nil {
		return nil, err
	}
	full := &FullDPSResult{}
	if cp.usedFullDPS {
		if full, err = cp.c.fullDPSFrom(res); err != nil {
			return nil, err
		}
	}
	after := cp.stats(res, full)
	delta := map[string]float64{}
	for k, v := range after {
		if w := cp.before[k]; v != w {
			if d := v - w; !math.IsNaN(d) {
				delta[k] = d
			}
		}
	}
	for k, w := range cp.before {
		if _, ok := after[k]; !ok && w != 0 && !math.IsNaN(w) {
			delta[k] = -w
		}
	}
	// Before is a copy: callers may change their maps.
	return &ItemComparison{
		Before: maps.Clone(cp.before), After: after, Delta: delta, Replaced: replaced,
		Unparsed: newEntries(res.Unparsed, cp.unparsed), Incomplete: newEntries(res.Incomplete, cp.incomplete),
	}, nil
}

// newEntries returns the entries of after that base does not have, in
// order, counting repeats: a line base has once and after twice is new
// once.
func newEntries(after, base []string) []string {
	seen := make(map[string]int, len(base))
	for _, e := range base {
		seen[e]++
	}
	var out []string
	for _, e := range after {
		if seen[e] > 0 {
			seen[e]--
			continue
		}
		out = append(out, e)
	}
	return out
}

// compareBuild is withItem with the replaced item's granted skill groups
// carried to the new item (carryGrantedGroups): a group of a skill the new
// item grants too keeps its settings, as the same item in the game gives
// the same skill.
func (cp *Comparer) compareBuild(slot, raw, baseType string) (*pobcode.Build, []string, error) {
	var old *pobcode.Item
	if node, ok := strings.CutPrefix(slot, "Jewel "); ok {
		if spec := cp.b.ActiveSpec(); spec != nil {
			for _, s := range spec.Sockets {
				if strconv.Itoa(s.NodeID) == node {
					old = cp.b.Item(s.ItemID)
				}
			}
		}
	} else if set := cp.b.ActiveItems(); set != nil {
		old = cp.b.Item(set.SlotItem(slot))
	}
	b, replaced, err := cp.withItem(slot, raw, baseType)
	if err != nil {
		return nil, nil, err
	}
	return cp.c.carryGrantedGroups(b, slot, old, &b.Items[len(b.Items)-1]), replaced, nil
}

// withItem returns a copy of the build with a new item of that text in
// slot, and the slots whose item it replaced or emptied. The copy shares
// everything but the item list, the item sets and, for a jewel socket,
// the passive specs with the base build; the shared parts are read only.
func (cp *Comparer) withItem(slot, raw, baseType string) (*pobcode.Build, []string, error) {
	b := cp.b
	nb := *b
	id := 1
	for _, item := range b.Items {
		id = max(id, item.ID+1)
	}
	nb.Items = make([]pobcode.Item, len(b.Items), len(b.Items)+1)
	copy(nb.Items, b.Items)
	nb.Items = append(nb.Items, pobcode.Item{ID: id, Raw: raw})
	if node, ok := strings.CutPrefix(slot, "Jewel "); ok {
		replaced, err := cp.putJewel(&nb, slot, node, id)
		return &nb, replaced, err
	}
	replaced, err := cp.putGear(&nb, slot, id, baseType)
	return &nb, replaced, err
}

// putJewel puts item id in the tree jewel socket node of nb's active
// spec, a copy of the base build's.
func (cp *Comparer) putJewel(nb *pobcode.Build, slot, node string, id int) ([]string, error) {
	n, err := strconv.Atoi(node)
	if tn := cp.c.data.Tree.Nodes[n]; err != nil || tn == nil || !tn.IsJewelSocket {
		return nil, fmt.Errorf("engine: unknown item slot %q", slot)
	}
	if nb.ActiveSpec() == nil {
		return nil, errors.New("engine: build has no passive tree")
	}
	nb.Specs = append([]pobcode.Spec(nil), nb.Specs...)
	spec := nb.ActiveSpec()
	spec.Sockets = append([]pobcode.Socket(nil), spec.Sockets...)
	for i := range spec.Sockets {
		if spec.Sockets[i].NodeID == n {
			var replaced []string
			if spec.Sockets[i].ItemID != 0 {
				replaced = []string{slot}
			}
			spec.Sockets[i].ItemID = id
			return replaced, nil
		}
	}
	spec.Sockets = append(spec.Sockets, pobcode.Socket{NodeID: n, ItemID: id})
	return nil, nil
}

// putGear puts item id in slot of nb's active item set, a copy of the
// base build's.
func (cp *Comparer) putGear(nb *pobcode.Build, slot string, id int, baseType string) ([]string, error) {
	if nb.ActiveItems() == nil {
		return nil, errors.New("engine: build has no item set")
	}
	nb.ItemSets = append([]pobcode.ItemSet(nil), nb.ItemSets...)
	set := nb.ActiveItems()
	set.Slots = append([]pobcode.Slot(nil), set.Slots...)
	var replaced []string
	put := func(name string, itemID int) {
		for i := range set.Slots {
			if set.Slots[i].Name == name {
				if set.Slots[i].ItemID != 0 {
					replaced = append(replaced, name)
				}
				set.Slots[i].ItemID = itemID
				return
			}
		}
		if itemID != 0 {
			set.Slots = append(set.Slots, pobcode.Slot{Name: name, ItemID: itemID})
		}
	}
	put(slot, id)
	if offhand, ok := offhandSlot(slot); ok && cp.emptiesOffhand(nb, set.SlotItem(offhand), baseType) {
		put(offhand, 0)
	}
	return replaced, nil
}

// offhandSlot is the "Weapon 2" slot of a "Weapon 1" slot's weapon set.
// CalcSetup.lua skips every "^Weapon 2" slot for a two-handed repItem in
// a "^Weapon 1" repSlotName; clearing the offhand of the slot's own set
// gives the same numbers, as the other set's slots are not used, and the
// offhand's abyss jewels lose their parent and drop out in stepItems.
func offhandSlot(slot string) (string, bool) {
	switch slot {
	case slotWeapon1:
		return slotWeapon2, true
	case slotWeapon1Swap:
		return slotWeapon2Swap, true
	}
	return "", false
}

// emptiesOffhand is CalcSetup.lua's two-handed test (calcs.initEnv,
// override.repItem): a Staff or two-handed sword, axe or mace, or a bow
// with something other than a quiver in the offhand. PoB tests the
// quiver on its local item, which is still nil there, so in PoB a bow
// never empties Weapon 2; here it does, as in the game, where a bow
// leaves only a quiver equipped.
func (cp *Comparer) emptiesOffhand(nb *pobcode.Build, offhandID int, baseType string) bool {
	switch baseType {
	case baseStaff, "Two Handed Sword", "Two Handed Axe", "Two Handed Mace":
		return true
	case baseBow:
		off := cp.c.peekItem(nb.Item(offhandID))
		return off == nil || off.Base == nil || off.Base.Type != baseQuiver
	}
	return false
}

const (
	baseBow         = "Bow"
	baseQuiver      = "Quiver"
	baseStaff       = "Staff"
	slotWeapon1     = "Weapon 1"
	slotWeapon1Swap = "Weapon 1 Swap"
	slotWeapon2     = "Weapon 2"
	slotWeapon2Swap = "Weapon 2 Swap"
	slotBodyArmour  = "Body Armour"
	// abyssalSlots is ItemsTab.lua's abyssal socket slots per item (for i = 1, 6).
	abyssalSlots = 6
)

// validSlot reports whether slot is one of ItemsTab.lua's slots:
// baseSlots, the weapon swap slots, abyssal sockets, the Wickerman slots
// and tree jewel sockets ("Jewel <node id>", checked against the tree
// later).
//
//nolint:goconst // the slot names as ItemsTab.lua lists them
func validSlot(slot string) bool {
	if parent, n, ok := strings.Cut(slot, " Abyssal Socket "); ok {
		i, err := strconv.Atoi(n)
		if err != nil || strconv.Itoa(i) != n || i < 1 || i > abyssalSlots {
			return false
		}
		switch parent {
		case slotWeapon1,
			slotWeapon2,
			slotWeapon1Swap,
			slotWeapon2Swap,
			"Helmet",
			"Gloves",
			slotBodyArmour,
			"Boots",
			"Belt":
			return true
		}
		return false
	}
	if strings.HasPrefix(slot, "Jewel ") {
		return true
	}
	switch slot {
	case slotWeapon1, slotWeapon2, "Helmet", slotBodyArmour, "Gloves", "Boots", "Amulet", "Ring 1", "Ring 2", "Ring 3",
		"Belt", "Graft 1", "Graft 2", "Flask 1", "Flask 2", "Flask 3", "Flask 4", "Flask 5",
		slotWeapon1Swap, slotWeapon2Swap, "Head", "Body", "Arm", "Leg":
		return true
	}
	return false
}

// checkSlot returns an error for a slot that is not one of PoB's slot
// names (validSlot, socketNode) or that the build does not have
// (slotShown).
func (cp *Comparer) checkSlot(slot string) error {
	if !validSlot(slot) || (strings.HasPrefix(slot, "Jewel ") && cp.socketNode(slot) == nil) {
		return fmt.Errorf("engine: unknown item slot %q", slot)
	}
	if !cp.slotShown(slot) {
		return fmt.Errorf("%w: %q", ErrSlotNotAvailable, slot)
	}
	return nil
}

// socketNode is the tree jewel socket of a "Jewel <node id>" slot, nil
// when the slot names no jewel socket. ItemsTab.lua:233-246 makes a slot
// for every "Socket" node of the tree, the sockets of the cluster jewel
// subgraphs included (they are tree nodes of the proxy groups,
// PassiveSpec.lua FindClusterSocket).
func (cp *Comparer) socketNode(slot string) *gamedata.Node {
	node, ok := strings.CutPrefix(slot, "Jewel ")
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(node)
	if tn := cp.c.data.Tree.Nodes[n]; err == nil && tn != nil && tn.IsJewelSocket {
		return tn
	}
	return nil
}

// slotShown is the shown function of the slots ItemsTab.lua hides for a
// build, which therefore cannot hold an item: "Ring 3" without
// AdditionalRingSlot (182-185; CalcSetup.lua:824-827 ignores its item,
// testing the flag on the allocated nodes, as ringSlot does), the Graft
// slots off a 3.27 tree (178-181) and the Wickerman slots off a 3.30 tree
// (203-212).
func (cp *Comparer) slotShown(slot string) bool {
	tree := ""
	if spec := cp.b.ActiveSpec(); spec != nil {
		tree = spec.TreeVersion
	}
	switch slot {
	case "Ring 3":
		return cp.ringSlot
	case "Graft 1", "Graft 2":
		return strings.Contains(tree, "3_27")
	case "Head", "Body", "Arm", "Leg":
		return strings.Contains(tree, "3_30")
	}
	return true
}

// itemValidForSlot is ItemsTabClass:IsItemValidForSlot
// (Classes/ItemsTab.lua:2639-2692) for the base build's active item set.
// item.type there is the base type (Item.lua).
//
//nolint:goconst // the type names as IsItemValidForSlot spells them
func (cp *Comparer) itemValidForSlot(it *itemtext.Item, slot string) bool {
	base := it.Base
	slotType := slotTypeOf(slot)
	switch {
	case slotType == "Jewel":
		return cp.jewelValidForSocket(it, cp.socketNode(slot))
	case base.Type == slotType:
		return true
	case base.Type == "Viscera":
		// item.base.tags["viscera_" .. (slotName:gsub("^%u", string.lower))]
		return slices.Contains(base.Tags, "viscera_"+strings.ToLower(slot[:1])+slot[1:])
	case base.Type == "Tincture" && slotType == "Flask":
		return true
	case base.Type == "Jewel" && base.SubType == "Abyss" && strings.Contains(slot, "Abyssal Socket"):
		return true
	case slot == slotWeapon1 || slot == slotWeapon1Swap:
		return base.Weapon != nil
	case slot == slotWeapon2 || slot == slotWeapon2Swap:
		return cp.offhandValid(base.Type, slot)
	}
	return false
}

// slotTypeOf is IsItemValidForSlot's slotType: the name before a trailing
// number (slotName:match("^([%a ]+) (%d+)$")), else the whole name. So
// "Ring 3" is "Ring", "Weapon 2" is "Weapon" and "Jewel 26725" is
// "Jewel", but "Weapon 1 Swap" and the abyssal sockets keep their names.
func slotTypeOf(slot string) string {
	i := strings.LastIndexByte(slot, ' ')
	if i <= 0 || i == len(slot)-1 {
		return slot
	}
	head, num := slot[:i], slot[i+1:]
	for _, r := range num {
		if r < '0' || r > '9' {
			return slot
		}
	}
	for _, r := range head {
		if r != ' ' && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return slot
		}
	}
	return head
}

// jewelValidForSocket is IsItemValidForSlot's jewel socket branch
// (ItemsTab.lua:2645-2668). A charm socket is a socket named "Charm
// Socket" (PassiveTree.lua:704-705); a cluster jewel is an item whose
// base is in data.clusterJewels (Item.lua:1063); expansion jewel size 0
// is small, 1 medium, 2 large.
//
//nolint:goconst // the type names as IsItemValidForSlot spells them
func (cp *Comparer) jewelValidForSocket(it *itemtext.Item, node *gamedata.Node) bool {
	if node == nil || it.Base.Type != "Jewel" {
		return false
	}
	sub, exp := it.Base.SubType, node.ExpansionJewel
	var cluster *gamedata.ClusterJewel
	if cp.c.data.Cluster != nil {
		cluster = cp.c.data.Cluster.Jewels[it.BaseName]
	}
	switch charm := node.Name == "Charm Socket"; {
	case charm || sub == "Charm":
		// Charm sockets hold only charms, and charms go only there.
		return charm && sub == "Charm"
	case sub == "Blightcore":
		// Putrid Seed grows passives only from large jewel sockets.
		return exp != nil && exp.Size == 2
	case cluster != nil && exp == nil:
		// No cluster jewels in inner sockets.
		return false
	case exp == nil || exp.Size == 2:
		// Outer sockets fit anything.
		return true
	}
	return cluster == nil || cluster.SizeIndex <= exp.Size
}

// offhandValid is IsItemValidForSlot's "Weapon 2" branch
// (ItemsTab.lua:2681-2690): what the set's "Weapon 1" (of the same
// weapon set) allows in the offhand. With no weapon, a shield or a
// one-handed weapon; with a bow, a quiver; with a one-handed weapon, a
// shield or a one-handed weapon, a wand only beside a wand; with anything
// else (a two-hander), nothing. PoB errors on a "Weapon 1" item that is
// not a weapon (weaponTypeInfo[weapon1Type] is nil); here it allows
// nothing.
//
//nolint:goconst // the type names as IsItemValidForSlot spells them
func (cp *Comparer) offhandValid(typ, slot string) bool {
	main := slotWeapon1
	if slot == slotWeapon2Swap {
		main = slotWeapon1Swap
	}
	mainType := "None"
	if set := cp.b.ActiveItems(); set != nil {
		if w := cp.c.peekItem(cp.b.Item(set.SlotItem(main))); w != nil && w.Base != nil {
			mainType = w.Base.Type
		}
	}
	oneHand := func(t string) bool {
		info, ok := weaponTypeInfo[t]
		return ok && t != "None" && info.oneHand
	}
	switch {
	case mainType == "None":
		return typ == "Shield" || oneHand(typ)
	case mainType == baseBow:
		return typ == baseQuiver
	case oneHand(mainType):
		return typ == "Shield" || (oneHand(typ) && (mainType == "Wand") == (typ == "Wand"))
	}
	return false
}

// normaliseItemText is a copy of pobcode's normaliseItemText
// (pobcode/xml.go), which builds Item.Raw from the XML text: no
// indentation and no blank lines. TestNormaliseItemTextMatchesPobcode
// keeps the two the same.
func normaliseItemText(s string) string {
	var lines []string
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}
