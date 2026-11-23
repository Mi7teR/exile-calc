package engine

import (
	"sort"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/modparser"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// clusterNode is one generated subgraph node: its stats and, for small
// passives, the extra PassiveSkillEffect of the jewel.
type clusterNode struct {
	stats     []string
	incEffect float64
}

// jewelData is what Item.lua builds from a cluster jewel's mods.
type jewelData struct {
	skill, keystone                        string
	notables, addedMods                    []string
	nodeCount, socketCount, socketOverride int
	nothingness                            int
	incEffect                              float64
	hasNodeCount, hasSocketOverride        bool
}

func readJewelData(mods []modparser.Mod, cj *gamedata.ClusterJewel) (jewelData, bool) {
	var d jewelData
	for _, m := range mods {
		if m.Type != "LIST" {
			continue
		}
		switch m.Name {
		case "ClusterJewelNotable":
			if s, ok := m.Value.(string); ok {
				d.notables = append(d.notables, s)
			}
		case "AddToClusterJewelNode":
			if s, ok := m.Value.(string); ok {
				d.addedMods = append(d.addedMods, s)
			}
		case "JewelData":
			v, _ := m.Value.(map[string]any)
			n, _ := v["value"].(float64)
			switch v["key"] {
			case "clusterJewelSkill":
				d.skill, _ = v["value"].(string)
			case "clusterJewelKeystone":
				d.keystone, _ = v["value"].(string)
			case "clusterJewelNodeCount":
				d.nodeCount, d.hasNodeCount = int(n), true
			case "clusterJewelSocketCount":
				d.socketCount = int(n)
			case "clusterJewelSocketCountOverride":
				d.socketOverride, d.hasSocketOverride = int(n), true
			case "clusterJewelNothingnessCount":
				d.nothingness = int(n)
			case "clusterJewelIncEffect":
				d.incEffect = n
			}
		}
	}
	// Item.lua: curse clusters share a parse; fix the skill per size.
	if cj.Size == "Small" && d.skill == "affliction_curse_effect" {
		d.skill = "affliction_curse_effect_small"
	} else if cj.Size == "Medium" && d.skill == "affliction_curse_effect_small" {
		d.skill = "affliction_curse_effect"
	}
	if d.hasNodeCount {
		d.nodeCount = max(min(d.nodeCount, cj.MaxNodes), cj.MinNodes)
	}
	if _, ok := cj.Skills[d.skill]; d.skill != "" && !ok {
		d.skill = ""
	}
	valid := d.keystone != "" || (d.skill != "" && d.hasNodeCount) || (d.hasSocketOverride && d.nothingness > 0)
	return d, valid
}

// clusterGraph ports PassiveSpecClass:BuildClusterJewelGraphs and
// BuildSubgraph (Classes/PassiveSpec.lua) as far as stats go: it returns
// the generated nodes by id. Positions and links are not needed: the
// build lists the allocated ids itself.
func (c *Calculator) clusterGraph(spec *pobcode.Spec, b *pobcode.Build, res *Result) map[int]clusterNode {
	tree, data := c.data.Tree, c.data.Cluster
	out := map[int]clusterNode{}
	if data == nil {
		return out
	}
	allocated := map[int]bool{}
	for _, id := range spec.Nodes {
		allocated[id] = true
	}
	socketed := map[int]*pobcode.Item{}
	for _, s := range spec.Sockets {
		if s.ItemID != 0 {
			socketed[s.NodeID] = b.Item(s.ItemID)
		}
	}
	// PassiveTree.lua: notables and keystones without a group are the
	// cluster notables, by name.
	notableByName := map[string]*gamedata.Node{}
	for _, n := range tree.Nodes {
		if n.Group == 0 && (n.IsNotable || n.IsKeystone) {
			notableByName[n.Name] = n
		}
	}

	var build func(jewel *pobcode.Item, exp *gamedata.ExpansionJewel, id int)
	build = func(jewel *pobcode.Item, exp *gamedata.ExpansionJewel, id int) {
		it, mods := c.itemMods(jewel, res)
		if it == nil {
			return
		}
		cj := data.Jewels[it.BaseName]
		if cj == nil {
			return
		}
		d, valid := readJewelData(mods, cj)
		if !valid {
			return
		}
		switch exp.Size {
		case 2:
			id += exp.Index << 6
		case 1:
			id += exp.Index << 9
		}
		nodeID := id + cj.SizeIndex<<4

		if d.keystone != "" {
			if n := notableByName[d.keystone]; n != nil {
				out[nodeID] = clusterNode{stats: n.Stats}
			}
			return
		}
		var notables []*gamedata.Node
		for _, name := range d.notables {
			n := notableByName[name]
			if n == nil {
				return // PoB drops the whole subgraph
			}
			notables = append(notables, n)
		}
		sort.SliceStable(notables, func(i, j int) bool {
			return data.NotableSortOrder[notables[i].Name] < data.NotableSortOrder[notables[j].Name]
		})
		skill := cj.Skills[d.skill]
		socketCount := d.socketCount
		if d.hasSocketOverride {
			socketCount = d.socketOverride
		}
		nodeCount := d.nodeCount
		if !d.hasNodeCount {
			nodeCount = socketCount + len(notables) + d.nothingness
		}
		smallCount := nodeCount - socketCount - len(notables)
		used := map[int]bool{}

		// Sockets: they are real tree nodes of the proxy group.
		proxy := tree.Nodes[exp.Proxy]
		var proxyGroup []int
		if proxy != nil && proxy.Group >= 1 && proxy.Group <= len(tree.Groups) {
			proxyGroup = tree.Groups[proxy.Group-1].Nodes
		}
		findSocket := func(index int) *gamedata.Node {
			for _, nid := range proxyGroup {
				if n := tree.Nodes[nid]; n != nil && n.ExpansionJewel != nil && n.ExpansionJewel.Index == index {
					return n
				}
			}
			return nil
		}
		var sockets []*gamedata.Node
		makeJewel := func(nodeIndex, jewelIndex int) {
			if s := findSocket(jewelIndex); s != nil {
				used[nodeIndex] = true
				sockets = append(sockets, s)
			}
		}
		if cj.Size == "Large" && socketCount == 1 {
			makeJewel(6, 1)
		} else {
			getJewels := []int{0, 2, 1}
			for i := 0; i < socketCount && i < len(cj.SocketIndicies) && i < len(getJewels); i++ {
				makeJewel(cj.SocketIndicies[i], getJewels[i])
			}
		}

		for i, idx := range notableIndices(cj, socketCount, nodeCount, len(notables)) {
			if i >= len(notables) {
				break
			}
			out[nodeID+idx] = clusterNode{stats: notables[i].Stats}
			used[idx] = true
		}

		var smallIdx []int
		for _, idx := range cj.SmallIndicies {
			if len(smallIdx) == smallCount {
				break
			}
			if cj.Size == "Medium" {
				if nodeCount == 5 && idx == 4 {
					idx = 3
				} else if nodeCount == 4 {
					if idx == 8 {
						idx = 9
					} else if idx == 4 {
						idx = 3
					}
				}
			}
			if !used[idx] {
				smallIdx = append(smallIdx, idx)
			}
		}
		for _, idx := range smallIdx {
			stats := append(append([]string(nil), skill.Stats...), d.addedMods...)
			out[nodeID+idx] = clusterNode{stats: stats, incEffect: d.incEffect}
		}

		// Recurse into allocated sockets holding a valid cluster jewel.
		for _, s := range sockets {
			if allocated[s.ID] && socketed[s.ID] != nil && s.ExpansionJewel != nil {
				build(socketed[s.ID], s.ExpansionJewel, id)
			}
		}
	}

	for id := range allocated {
		n := tree.Nodes[id]
		if n == nil || !n.IsJewelSocket || n.ExpansionJewel == nil || n.ExpansionJewel.Size != 2 {
			continue
		}
		if jewel := socketed[id]; jewel != nil {
			build(jewel, n.ExpansionJewel, 0x10000)
		}
	}
	return out
}

// notableIndices ports itemLib.getClusterJewelNotableIndices (ItemTools.lua).
func notableIndices(cj *gamedata.ClusterJewel, socketCount, nodeCount, notableCount int) []int {
	sockets := map[int]bool{}
	if cj.Size == "Large" && socketCount == 1 {
		sockets[6] = true
	} else {
		for i := 0; i < socketCount && i < len(cj.SocketIndicies); i++ {
			sockets[cj.SocketIndicies[i]] = true
		}
	}
	var out []int
	for _, idx := range cj.NotableIndicies {
		if len(out) == notableCount {
			break
		}
		if cj.Size == "Medium" {
			if socketCount == 0 && notableCount == 2 {
				if idx == 6 {
					idx = 4
				} else if idx == 10 {
					idx = 8
				}
			} else if nodeCount == 4 {
				if idx == 10 {
					idx = 9
				} else if idx == 2 {
					idx = 3
				}
			}
		}
		if !sockets[idx] {
			out = append(out, idx)
		}
	}
	sort.Ints(out)
	return out
}
