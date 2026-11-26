package engine

import (
	"fmt"
	"slices"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/modparser"
	"github.com/Mi7teR/poe-lib/pobcode"
)

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
// (CalcSetup.lua buildModListForNode: calcLib.mod(modList, nil, "PassiveSkillEffect")),
// and returns them: the node's final mod list.
// incEffect is the cluster jewel's "increased Effect" of small passives.
func (c *Calculator) addNode(db *ModDB, res *Result, stats []string, incEffect float64, source string, tn *gamedata.Node, funcs []radiusFunc) []Mod {
	mods := finishNode(c.nodeMods(res, stats, incEffect, source, tn, funcs), true, tn, nil)
	for _, m := range mods {
		db.Add(m)
	}
	return mods
}

// nodeCanExplode is buildModListForNode's second result (CalcSetup.lua:
// 266-267): a node is an explode source when its final mod list has a
// CanExplode mod and modList:Flag(nil, "CanExplode") holds. hasExplode
// follows that list: set by the prefilter (170-189), cleared by the "grant
// nothing" wipe (191-197) and rescanned after the second-pass radius
// jewels (207-232); the first-pass jewels (159-168) run before the
// prefilter. The Flag reads the node's own list, so tags are evaluated
// against that list alone.
func nodeCanExplode(mods []Mod) bool {
	// hasExplode (CalcSetup.lua:184-185, 227-228).
	if !slices.ContainsFunc(mods, func(m Mod) bool { return m.Name == "CanExplode" }) {
		return false
	}
	node := NewModDB()
	for _, m := range mods {
		node.Add(m)
	}
	return node.Flag("CanExplode")
}

// finishNode is the rest of buildModListForNode: a node that "grants
// nothing" loses its mods, the rest is scaled by passive skill effect, then
// the second-pass radius functions see the result (Self on allocated
// nodes, SelfUnalloc on unallocated ones), and a node with
// PassiveSkillHasOtherEffect grants its NodeModifier mods instead
// (replaceNodeMods).
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
	return replaceNodeMods(out)
}

// replaceNodeMods is CalcSetup.lua:233-249: when the node's list has
// PassiveSkillHasOtherEffect (modList:Flag(nil, ...), so tags are
// evaluated against that list), the mods of its NodeModifier entries, in
// order, replace the whole list. Without a NodeModifier the list is kept
// (the wipe runs on the first entry). hasOtherEffect follows the final
// list (prefilter 170-189, wipe 191-197, rescan 219-232), which is what
// the flag test sees here.
func replaceNodeMods(mods []Mod) []Mod {
	if !slices.ContainsFunc(mods, func(m Mod) bool { return m.Name == "PassiveSkillHasOtherEffect" }) {
		return mods
	}
	node := NewModDB()
	for _, m := range mods {
		node.Add(m)
	}
	if !node.Flag("PassiveSkillHasOtherEffect") {
		return mods
	}
	var out []Mod
	replaced := false
	for _, e := range node.List(nil, "NodeModifier") {
		v, _ := e.Value.(map[string]any)
		mm, ok := v[nestedModKey].(map[string]any)
		if !ok {
			continue
		}
		replaced = true
		source, _ := mm["source"].(string)
		out = append(out, modFromMap(mm, source))
	}
	if !replaced {
		return mods
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
