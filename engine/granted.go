package engine

import (
	"sort"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
)

// notableMaps builds PassiveTree.lua's notableMap and ascendancyMap: notable
// nodes by lower-cased name. A duplicate name is replaced only by a node
// that is in a group (on the tree).
func notableMaps(tree *gamedata.Tree) (notable, ascendancy map[string]*gamedata.Node) {
	notable, ascendancy = map[string]*gamedata.Node{}, map[string]*gamedata.Node{}
	ids := make([]int, 0, len(tree.Nodes))
	for id := range tree.Nodes {
		ids = append(ids, id)
	}
	sort.Ints(ids) // PoB walks a hash table; any fixed order will do
	for _, id := range ids {
		n := tree.Nodes[id]
		if !n.IsNotable || n.IsKeystone || n.IsMastery {
			continue
		}
		name := strings.ToLower(n.Name)
		if n.AscendancyName != "" {
			ascendancy[name] = n
		} else if _, dup := notable[name]; !dup || n.Group != 0 {
			notable[name] = n
		}
	}
	return notable, ascendancy
}

// grantedNodes ports CalcSetup.lua's "Add granted passives" (anoints and
// other "Allocates X" lines) and "Add granted ascendancy node" (Forbidden
// Flame and Flesh naming the same node, both restricted to the character's
// class). It returns the granted nodes that are not allocated already.
func grantedNodes(db *ModDB, tree *gamedata.Tree, className string, allocated map[int]bool) []int {
	notable, ascendancy := notableMaps(tree)
	seen := map[int]bool{}
	var out []int
	grant := func(n *gamedata.Node) {
		if n == nil || allocated[n.ID] || seen[n.ID] {
			return
		}
		seen[n.ID] = true
		out = append(out, n.ID)
	}
	for _, m := range db.mods["GrantedPassive"] {
		if v, ok := db.eval(&m); ok {
			if name, ok := v.(string); ok {
				if n := notable[name]; n != nil {
					grant(n)
				} else {
					grant(ascendancy[name])
				}
			}
		}
	}
	type side struct {
		side    string
		matched bool
	}
	matched := map[string]*side{}
	for _, m := range db.mods["GrantedAscendancyNode"] {
		v, ok := db.eval(&m)
		t, _ := v.(map[string]any)
		if !ok || t == nil {
			continue
		}
		name, _ := t["name"].(string)
		sd, _ := t["side"].(string)
		if prev := matched[name]; prev != nil && prev.side != sd && !prev.matched {
			prev.matched = true
			if db.classConds["ForbiddenFlesh"] == className && db.classConds["ForbiddenFlame"] == className {
				grant(ascendancy[name])
			}
		} else {
			matched[name] = &side{side: sd}
		}
	}
	return out
}
