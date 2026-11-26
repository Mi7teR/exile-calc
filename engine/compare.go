package engine

import (
	"errors"
	"fmt"
	"maps"
	"math"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// Comparer compares items against one build, like the stat comparison of
// PoB's item tooltip (Classes/ItemsTab.lua AddItemTooltip with
// calcs.getMiscCalculator, Modules/Calcs.lua): the build is computed once,
// then each Item call computes it again with one slot replaced.
//
// A Comparer is safe for concurrent use. It keeps the build it was made
// from and reads it on every Item call, so that build must not be
// modified while the Comparer is in use.
type Comparer struct {
	c      *Calculator
	b      *pobcode.Build
	before map[string]float64
	// usedFullDPS is getMiscCalculator's usedFullDPS: the base Full DPS
	// has skills, so every comparison computes Full DPS too.
	usedFullDPS bool
}

// ItemComparison is the change from putting one item in one slot.
type ItemComparison struct {
	Before, After map[string]float64 // Result.Stats (+ "FullDPS", "FullDotDPS" when used)
	Delta         map[string]float64 // After - Before, only non-zero
	Replaced      []string           // slots the new item emptied or replaced ("Weapon 2" for a two-hander)
}

// NewComparer computes b once (Calc, and Full DPS when b has skills in
// it) as the base of later Item calls. b is not modified.
func (c *Calculator) NewComparer(b *pobcode.Build) (*Comparer, error) {
	res, err := c.Calc(b)
	if err != nil {
		return nil, err
	}
	full, err := c.fullDPSFrom(b, res)
	if err != nil {
		return nil, err
	}
	cp := &Comparer{c: c, b: b, usedFullDPS: len(full.Skills) > 0}
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
// before and after. itemText is an item as the game copies it (Ctrl+C,
// Ctrl+Alt+C) or as PoB exports it. slot is a PoB slot name: "Weapon 1",
// "Weapon 2", "Helmet", "Body Armour", "Gloves", "Boots", "Amulet",
// "Ring 1".."Ring 3", "Belt", "Flask 1".."Flask 5", "Graft 1", "Graft 2",
// the Wickerman slots ("Head", "Body", "Arm", "Leg"), "Weapon 1 Swap",
// "Weapon 2 Swap", abyssal sockets ("Helmet Abyssal Socket 1") and tree
// jewel sockets ("Jewel 26725"). A slot the build does not use (the
// inactive weapon set, an unallocated jewel socket) gives no change, as
// in PoB. The caller picks a slot that fits the item: the item type is
// not checked against the slot.
//
// As CalcSetup.lua's override.repSlotName/repItem (calcs.initEnv), a
// Staff or two-handed sword, axe or mace in a "Weapon 1" slot empties the
// set's "Weapon 2", and so does a bow unless "Weapon 2" holds a quiver.
func (cp *Comparer) Item(slot string, itemText string) (*ItemComparison, error) {
	if !validSlot(slot) {
		return nil, fmt.Errorf("engine: unknown item slot %q", slot)
	}
	text := strings.ReplaceAll(itemText, "\r", "")
	it, err := cp.c.items.Parse(text)
	if err != nil {
		return nil, fmt.Errorf("engine: item: %w", err)
	}
	if it.Base == nil {
		return nil, errors.New("engine: item: unknown item base")
	}
	b, replaced, err := cp.withItem(slot, normaliseItemText(text), it.Base.Type)
	if err != nil {
		return nil, err
	}
	res, err := cp.c.Calc(b)
	if err != nil {
		return nil, err
	}
	full := &FullDPSResult{}
	if cp.usedFullDPS {
		if full, err = cp.c.fullDPSFrom(b, res); err != nil {
			return nil, err
		}
	}
	after := cp.stats(res, full)
	delta := map[string]float64{}
	for k, v := range after {
		if w := cp.before[k]; v != w && !(math.IsNaN(v) && math.IsNaN(w)) {
			delta[k] = v - w
		}
	}
	for k, v := range cp.before {
		if _, ok := after[k]; !ok && v != 0 {
			delta[k] = -v
		}
	}
	// Before is a copy: callers may change their maps.
	return &ItemComparison{Before: maps.Clone(cp.before), After: after, Delta: delta, Replaced: replaced}, nil
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

// normaliseItemText is pobcode's: an item's Raw has no indentation and no
// blank lines.
func normaliseItemText(s string) string {
	var lines []string
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}
