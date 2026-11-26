package engine

import (
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// pruneSpec returns spec with the node list PoB keeps after loading it
// (PassiveSpec.lua):
//   - proxy nodes and nested cluster sockets are not in self.nodes unless a
//     cluster subgraph adds the socket, so their ids are dropped;
//   - BuildAllDependsAndPaths deallocates nodes with no allocated path to a
//     class or ascendancy start, except those in the radius of an allocated
//     Intuitive Leap-like jewel or of an Impossible Escape keystone.
//
// Builds moved to a newer tree (and hand-edited ones) carry such nodes.
func (c *Calculator) pruneSpec(spec *pobcode.Spec, b *pobcode.Build) *pobcode.Spec {
	tree := c.data.Tree
	cluster, links, subSockets := c.clusterGraph(spec, b, &Result{})
	alloc := map[int]bool{}
	for _, id := range spec.Nodes {
		n := tree.Nodes[id]
		if n == nil {
			if _, ok := cluster[id]; ok {
				alloc[id] = true
			}
			continue
		}
		if !subSockets[id] && (n.IsProxy || n.Group < 1 || n.Group > len(tree.Groups) || tree.Groups[n.Group-1].IsProxy ||
			(n.ExpansionJewel != nil && n.ExpansionJewel.Parent != 0)) {
			continue
		}
		alloc[id] = true
	}

	// Connected: reachable from an allocated start through allocated nodes
	// (FindStartFromNode does not path through masteries).
	keep := map[int]bool{}
	var queue []int
	for id := range alloc {
		if n := tree.Nodes[id]; n != nil && (n.ClassStartIndex != nil || n.IsAscendancyStart) {
			keep[id] = true
			queue = append(queue, id)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		n := tree.Nodes[id]
		if n != nil && n.IsMastery {
			continue
		}
		var linked []int
		if n != nil {
			linked = append(append(linked, n.Out...), n.In...)
		}
		for _, o := range append(linked, links[id]...) {
			if alloc[o] && !keep[o] {
				keep[o] = true
				queue = append(queue, o)
			}
		}
	}

	for id := range c.leapLikeNodes(spec, b, alloc) {
		if alloc[id] {
			keep[id] = true
		}
	}

	if len(keep) == len(spec.Nodes) {
		return spec
	}
	out := *spec
	out.Nodes = nil
	for _, id := range spec.Nodes {
		if keep[id] {
			out.Nodes = append(out.Nodes, id)
		}
	}
	return &out
}

// leapLikeNodes is NodesInIntuitiveLeapLikeRadius over the allocated
// sockets: nodes in the radius of an intuitiveLeapLike jewel, and in the
// radius of each Impossible Escape keystone.
func (c *Calculator) leapLikeNodes(spec *pobcode.Spec, b *pobcode.Build, alloc map[int]bool) map[int]bool {
	tree := c.data.Tree
	out := map[int]bool{}
	var pos map[int]point
	for _, s := range spec.Sockets {
		if s.ItemID == 0 || !alloc[s.NodeID] {
			continue
		}
		it, mods := c.itemMods(b.Item(s.ItemID), &Result{})
		if it == nil {
			continue
		}
		leap, radiusIndex := false, 0
		var keystones []string
		for _, m := range mods {
			v, _ := m.Value.(map[string]any)
			switch m.Name {
			case "JewelData":
				switch v["key"] {
				case "intuitiveLeapLike":
					leap = truthy(v["value"])
				case "radiusIndex":
					if f, ok := num(v["value"]); ok {
						radiusIndex = int(f)
					}
				}
			case "ImpossibleEscapeKeystones":
				if k, ok := v["key"].(string); ok {
					keystones = append(keystones, k)
				}
			}
		}
		if !leap && len(keystones) == 0 {
			continue
		}
		r, ok := radiusByLabel(it.Radius)
		if radiusIndex >= 1 && radiusIndex <= len(jewelRadii) {
			r, ok = jewelRadii[radiusIndex-1], true
		}
		if !ok {
			continue
		}
		if pos == nil {
			pos = c.nodePositions()
		}
		if leap {
			for _, id := range nodesInRadius(tree, pos, s.NodeID, r) {
				out[id] = true
			}
		}
		for _, k := range keystones {
			if kn := keystoneByName(tree, k); kn != nil {
				for _, id := range nodesInRadius(tree, pos, kn.ID, r) {
					out[id] = true
				}
			}
		}
	}
	return out
}

// keystoneByName is tree.keystoneMap: Impossible Escape names the
// keystone in lower case.
func keystoneByName(tree *gamedata.Tree, name string) *gamedata.Node {
	for _, n := range tree.Nodes {
		if n.IsKeystone && strings.EqualFold(n.Name, name) && n.Group != 0 {
			return n
		}
	}
	return nil
}
