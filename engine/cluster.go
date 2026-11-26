package engine

import (
	"maps"
	"slices"
	"sort"

	"github.com/Mi7teR/exile-calc/gamedata"
	"github.com/Mi7teR/exile-calc/modparser"
	"github.com/Mi7teR/exile-calc/pobcode"
)

// clusterNode is one generated subgraph node: its stats and, for small
// passives, the extra PassiveSkillEffect of the jewel.
type clusterNode struct {
	stats     []string
	incEffect float64
	notable   bool
	keystone  bool
}

// jewelData is what Item.lua builds from a cluster jewel's mods.
type jewelData struct {
	skill, keystone                        string
	notables, addedMods                    []string
	nodeCount, socketCount, socketOverride int
	nothingness                            int
	incEffect                              float64
	hasNodeCount, hasSocketOverride        bool
	smallsNothing                          bool // clusterJewelSmallsAreNothingness (Megalomaniac)
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
			case "clusterJewelSmallsAreNothingness":
				d.smallsNothing = v["value"] == true
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
	valid := d.keystone != "" || ((d.skill != "" || d.smallsNothing) && d.hasNodeCount) || (d.hasSocketOverride && d.nothingness > 0)
	return d, valid
}

// clusterGraph ports PassiveSpecClass:BuildClusterJewelGraphs and
// BuildSubgraph (Classes/PassiveSpec.lua) as far as stats go: it returns
// the generated nodes by id and the subgraphs' links (both directions).
// Positions are not needed: the build lists the allocated ids itself.
func (c *Calculator) clusterGraph(spec *pobcode.Spec, b *pobcode.Build, res *Result) (map[int]clusterNode, map[int][]int, map[int]bool) {
	return c.clusterGraphLegacy(spec, b, res, nil)
}

// clusterNotablesByName returns PassiveTree.lua's clusterNodeMap:
// notables and keystones without a group, by name. It is built once per
// Calculator; callers must not modify it.
// PassiveTree.lua:546,645 fills it in pairs(self.nodes) order, last one
// wins, so PoB's pick between two nodes with one name is undefined; the
// lowest id wins here. The 3.30 tree has one such name, Evil Eye (42305
// and 57104), with the same stats.
func (c *Calculator) clusterNotablesByName() map[string]*gamedata.Node {
	c.clusterNotablesOnce.Do(func() {
		c.clusterNotables = map[string]*gamedata.Node{}
		for _, n := range c.data.Tree.Nodes {
			if n.Group != 0 || (!n.IsNotable && !n.IsKeystone) {
				continue
			}
			if prev := c.clusterNotables[n.Name]; prev == nil || n.ID < prev.ID {
				c.clusterNotables[n.Name] = n
			}
		}
	})
	return c.clusterNotables
}

// clusterGraphLegacy is clusterGraph that also fills legacy, when not nil,
// with PassiveSpec's legacyClusterNodeMap: the ids a format-1 spec saved
// for the subgraphs' sockets and nodes -> the current ids
// (BuildLegacyProxyGroup and BuildLegacyClusterOrbitMappings).
//
//nolint:gocognit,gocyclo,cyclop,funlen // the BuildSubgraph walk, ported as one function
func (c *Calculator) clusterGraphLegacy(
	spec *pobcode.Spec,
	b *pobcode.Build,
	res *Result,
	legacy map[int]int,
) (map[int]clusterNode, map[int][]int, map[int]bool) {
	// subSockets: the tree sockets the subgraphs create (PassiveSpec keeps
	// nested cluster sockets out of self.nodes until a subgraph adds them).
	subSockets := map[int]bool{}
	tree, data := c.data.Tree, c.data.Cluster
	out := map[int]clusterNode{}
	links := map[int][]int{}
	if data == nil {
		return out, links, subSockets
	}
	linkNodes := func(a, b int) {
		links[a] = append(links[a], b)
		links[b] = append(links[b], a)
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
	reverse := map[int]int{} // legacyClusterNodeMapReverse
	register := func(legacyID, currentID int) {
		if legacy != nil {
			legacy[legacyID] = currentID
			reverse[currentID] = legacyID
		}
	}
	// GetSocketedJewel: a socket with no jewel takes its legacy id's.
	jewelAt := func(id int) *pobcode.Item {
		if it := socketed[id]; it != nil {
			return it
		}
		if l, ok := reverse[id]; ok {
			return socketed[l]
		}
		return nil
	}
	notableByName := c.clusterNotablesByName()

	var build func(jewel *pobcode.Item, exp *gamedata.ExpansionJewel, id, parent int)
	build = func(jewel *pobcode.Item, exp *gamedata.ExpansionJewel, id, parent int) {
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
				out[nodeID] = clusterNode{stats: n.Stats, keystone: true}
				linkNodes(nodeID, parent)
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
		byIndex := map[int]int{} // BuildSubgraph's indicies: node index -> node id

		// Sockets: they are real tree nodes of the proxy group.
		proxy := tree.Nodes[exp.Proxy]
		var proxyGroup []int
		if proxy != nil && proxy.Group >= 1 && proxy.Group <= len(tree.Groups) {
			proxyGroup = tree.Groups[proxy.Group-1].Nodes
		}
		findSocket := func(group []int, index int) *gamedata.Node {
			return findClusterSocket(tree, group, index)
		}
		// BuildLegacyProxyGroup: the legacy parser downsized the proxy group
		// while descending into nested clusters; its sockets are the ids a
		// format-1 spec saved.
		var legacyGroup []int
		if legacy != nil {
			legacyGroup = legacyProxyGroup(tree, proxyGroup, exp.Size, cj.SizeIndex)
		}
		var sockets []*gamedata.Node
		makeJewel := func(nodeIndex, jewelIndex int) {
			if s := findSocket(proxyGroup, jewelIndex); s != nil {
				used[nodeIndex] = true
				byIndex[nodeIndex] = s.ID
				sockets = append(sockets, s)
				subSockets[s.ID] = true
				if legacyGroup != nil {
					if ls := findSocket(legacyGroup, jewelIndex); ls != nil {
						register(ls.ID, s.ID)
					}
				}
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
			out[nodeID+idx] = clusterNode{stats: notables[i].Stats, notable: true}
			byIndex[idx] = nodeID + idx
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
			var stats []string
			if d.skill != "" && !d.smallsNothing {
				stats = append(append(stats, skill.Stats...), d.addedMods...)
			}
			out[nodeID+idx] = clusterNode{stats: stats, incEffect: d.incEffect}
			byIndex[idx] = nodeID + idx
		}
		// Connectors: a ring in index order (closed unless Small), and the
		// entrance (index 0) to the parent socket.
		first, last := -1, -1
		for i := 0; i < cj.TotalIndicies; i++ {
			n, ok := byIndex[i]
			if !ok {
				continue
			}
			if first < 0 {
				first = n
			}
			if last >= 0 {
				linkNodes(n, last)
			}
			last = n
		}
		if first != last && cj.Size != "Small" {
			linkNodes(first, last)
		}
		if entrance, ok := byIndex[0]; ok {
			linkNodes(entrance, parent)
		}
		if legacy != nil && proxy != nil {
			for l, cur := range legacyOrbitMappings(tree, data, proxy, cj, byIndex) {
				register(l, cur)
			}
		}

		// Recurse into allocated sockets holding a valid cluster jewel.
		for _, s := range sockets {
			if sub := jewelAt(s.ID); allocated[s.ID] && sub != nil && s.ExpansionJewel != nil {
				build(sub, s.ExpansionJewel, id, s.ID)
			}
		}
	}

	// In id order: the jewels' parse notes (Result.Unparsed) come out in a
	// stable order.
	for _, id := range slices.Sorted(maps.Keys(allocated)) {
		n := tree.Nodes[id]
		if n == nil || !n.IsJewelSocket || n.ExpansionJewel == nil || n.ExpansionJewel.Size != 2 {
			continue
		}
		if jewel := socketed[id]; jewel != nil {
			build(jewel, n.ExpansionJewel, 0x10000, id)
		}
	}
	return out, links, subSockets
}

// findClusterSocket is PassiveSpecClass:FindClusterSocket: the socket of
// the group with that expansion jewel index.
func findClusterSocket(tree *gamedata.Tree, group []int, index int) *gamedata.Node {
	for _, nid := range group {
		if n := tree.Nodes[nid]; n != nil && n.ExpansionJewel != nil && n.ExpansionJewel.Index == index {
			return n
		}
	}
	return nil
}

// legacyProxyGroup is PassiveSpecClass:BuildLegacyProxyGroup.
func legacyProxyGroup(tree *gamedata.Tree, proxyGroup []int, expansionSize, sizeIndex int) []int {
	group, size := proxyGroup, expansionSize
	for guard := 0; sizeIndex < size && guard < 4; guard++ {
		socket := findClusterSocket(tree, group, 1)
		if socket == nil {
			socket = findClusterSocket(tree, group, 0)
		}
		if socket == nil {
			break
		}
		p := tree.Nodes[socket.ExpansionJewel.Proxy]
		if p == nil || p.Group < 1 || p.Group > len(tree.Groups) {
			break
		}
		group, size = tree.Groups[p.Group-1].Nodes, socket.ExpansionJewel.Size
	}
	return group
}

// translateClusterOrbitIndex is PassiveSpecClass:TranslateClusterOrbitIndex.
func translateClusterOrbitIndex(src, srcPerOrbit, destPerOrbit int) int {
	switch {
	case srcPerOrbit == destPerOrbit:
		return src
	case srcPerOrbit == 12 && destPerOrbit == 16:
		return []int{0, 1, 3, 4, 5, 7, 8, 9, 11, 12, 13, 15}[src]
	case srcPerOrbit == 16 && destPerOrbit == 12:
		return []int{0, 1, 1, 2, 3, 4, 4, 5, 6, 7, 7, 8, 9, 10, 10, 11}[src]
	case srcPerOrbit == 6 && destPerOrbit == 16:
		return []int{0, 3, 5, 8, 11, 13}[src]
	case srcPerOrbit == 16 && destPerOrbit == 6:
		return []int{0, 0, 0, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 5, 5, 5}[src]
	}
	return src * destPerOrbit / srcPerOrbit
}

// legacyOrbitMappings ports ApplyClusterOrbitIndexAdjustment and
// BuildLegacyClusterOrbitMappings (PassiveSpec.lua): it matches the
// subgraph's nodes (byIndex: cluster template index -> node id) in the
// legacy orbit space, where the proxy node's own orbit placed them, with
// the current one, offset by orbitOffsets, and returns legacy id -> current
// id for the nodes whose ids differ.
func legacyOrbitMappings(
	tree *gamedata.Tree,
	data *gamedata.ClusterJewels,
	proxy *gamedata.Node,
	cj *gamedata.ClusterJewel,
	byIndex map[int]int,
) map[int]int {
	spo := tree.Constants.SkillsPerOrbit
	total := cj.TotalIndicies
	if total == 0 || cj.SizeIndex+1 >= len(spo) || proxy.Orbit >= len(spo) {
		return nil
	}
	skillsPerOrbit, legacyPerOrbit := spo[cj.SizeIndex+1], spo[proxy.Orbit]
	startOidx := data.OrbitOffsets[proxy.ID][cj.SizeIndex]
	legacyProxyRel := translateClusterOrbitIndex(proxy.OrbitIndex, legacyPerOrbit, total)
	legacyByOidx, currentByOidx := map[int]int{}, map[int]int{}
	for _, idx := range slices.Sorted(maps.Keys(byIndex)) {
		id := byIndex[idx]
		legacyByOidx[translateClusterOrbitIndex((idx+legacyProxyRel)%total, total, legacyPerOrbit)] = id
		oidx := translateClusterOrbitIndex((idx+startOidx)%total, total, skillsPerOrbit)
		rel := translateClusterOrbitIndex(oidx, skillsPerOrbit, total)
		currentByOidx[translateClusterOrbitIndex(rel, total, legacyPerOrbit)] = id
	}
	out := map[int]int{}
	for oidx, l := range legacyByOidx {
		if cur, ok := currentByOidx[oidx]; ok && cur != l {
			out[l] = cur
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

// legacyClusterSpec ports the format-1 conversion of
// PassiveSpecClass:BuildClusterJewelGraphs (BeginLegacyClusterHashConversion
// and ApplyLegacyClusterNodeRemap): a spec saved without
// clusterHashFormatVersion lists its cluster sockets and subgraph nodes by
// the ids of the old parser, which PoB maps to the current ones before it
// allocates them. Jewels move with their sockets. Other specs are returned
// as they are.
func (c *Calculator) legacyClusterSpec(spec *pobcode.Spec, b *pobcode.Build) *pobcode.Spec {
	if spec.ClusterHashFormatVersion != 1 {
		return spec
	}
	legacy := map[int]int{}
	cluster, _, subSockets := c.clusterGraphLegacy(spec, b, &Result{}, legacy)
	// GetMappedClusterNodeId: only to a node the subgraphs created.
	mapped := func(id int) (int, bool) {
		cur, ok := legacy[id]
		if !ok || cur == id {
			return id, false
		}
		if _, gen := cluster[cur]; gen || subSockets[cur] {
			return cur, true
		}
		return id, false
	}
	changed := false
	seen := map[int]bool{}
	var nodes []int
	for _, id := range spec.Nodes {
		if cur, ok := mapped(id); ok {
			id, changed = cur, true
		}
		if !seen[id] {
			seen[id] = true
			nodes = append(nodes, id)
		}
	}
	sockets, moved := remapSockets(spec.Sockets, mapped)
	changed = changed || moved
	if !changed {
		return spec
	}
	out := *spec
	out.Nodes, out.Sockets = nodes, sockets
	return &out
}

// remapSockets moves the jewels of legacy sockets to their current ids.
// self.jewels is keyed by node: a jewel moved onto a socket that already
// holds one does not add a second entry. (PoB's pairs order decides
// between them, and the socket item slots have already written a 0 for
// every empty socket, so PoB can also lose the moved jewel; the jewel is
// kept here, see testdata/golden/quirks.json.) PassiveSpec.lua:130-132
// Load drops sockets saved with itemId 0, so only sockets holding a jewel
// take a place.
func remapSockets(in []pobcode.Socket, mapped func(int) (int, bool)) ([]pobcode.Socket, bool) {
	held := map[int]bool{}
	for _, s := range in {
		if s.ItemID != 0 {
			held[s.NodeID] = true
		}
	}
	changed := false
	var out []pobcode.Socket
	for _, s := range in {
		if cur, ok := mapped(s.NodeID); ok {
			changed = true
			if held[cur] {
				continue
			}
			s.NodeID, held[cur] = cur, true
		}
		out = append(out, s)
	}
	return out, changed
}
