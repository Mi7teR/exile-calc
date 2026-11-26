package engine

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Mi7teR/exile-calc/pobcode"
)

// radiusJewelBuild is a corpus build with one jewel in an allocated socket
// with an allocated notable in its Medium radius replaced by a Medium
// radius jewel with the given mod lines; it returns the build, the socket
// and the allocated nodes in its radius.
func radiusJewelBuild(t *testing.T, c *Calculator, id string, lines ...string) (*pobcode.Build, int, []int) {
	t.Helper()
	b := corpusBuild(t, id)
	spec := b.ActiveSpec()
	alloc := map[int]bool{}
	for _, n := range spec.Nodes {
		alloc[n] = true
	}
	r, _ := radiusByLabel("Medium")
	pos := c.nodePositions()
	for _, s := range spec.Sockets {
		it := b.Item(s.ItemID)
		if it == nil || !alloc[s.NodeID] || strings.Contains(it.Raw, "Cluster Jewel") {
			continue
		}
		var in []int
		notable := false
		for _, n := range nodesInRadius(c.data.Tree, pos, s.NodeID, r) {
			if alloc[n] {
				in = append(in, n)
				notable = notable || c.data.Tree.Nodes[n].IsNotable
			}
		}
		if !notable {
			continue
		}
		cp := *b
		cp.Items = slices.Clone(b.Items)
		for i := range cp.Items {
			if cp.Items[i].ID == s.ItemID {
				cp.Items[i].Raw = "Rarity: UNIQUE\nTest Jewel\nCobalt Jewel\nRadius: Medium\nImplicits: 0\n" + strings.Join(
					lines,
					"\n",
				)
				cp.Items[i].ModRanges = nil
			}
		}
		return &cp, s.NodeID, in
	}
	t.Fatalf("%s: no allocated jewel socket with an allocated notable in radius", id)
	return nil, 0, nil
}

// sumFrom sums the numeric values of stat's mods from source (any flags).
func sumFrom(db *ModDB, stat, source string, flags uint64) float64 {
	total := 0.0
	for _, m := range db.mods[stat] {
		if v, ok := m.Value.(float64); ok && m.Source == source && m.Flags == flags {
			total += v
		}
	}
	return total
}

// TestTransformedNotables: "Notable Passive Skills in Radius are
// Transformed to instead grant: ..." (ModParser.lua:6469-6493) gives each
// notable in radius PassiveSkillHasOtherEffect and NodeModifier mods, and
// buildModListForNode replaces the notable's mods with the NodeModifier
// mods (CalcSetup.lua:233-249), which keep the jewel's source.
func TestTransformedNotables(t *testing.T) {
	c := newCalc(t)
	b, socket, in := radiusJewelBuild(
		t,
		c,
		"VFJTgt8AL50o",
		"Notable Passive Skills in Radius are Transformed to instead grant: 10% increased Mana Cost of Skills and 20% increased Spell Damage",
	)
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	notables, small := 0, 0
	for _, id := range in {
		n := c.data.Tree.Nodes[id]
		if n == nil {
			continue
		}
		src := fmt.Sprintf("Tree:%d", id)
		has := false
		for _, l := range res.DB.mods {
			for _, m := range l {
				has = has || m.Source == src
			}
		}
		switch nodeType(n) {
		case "Notable":
			notables++
			if has {
				t.Errorf("notable %d (%s) keeps mods of its own", id, n.Name)
			}
		case "Normal":
			if len(n.Stats) > 0 {
				small++
				if !has {
					t.Errorf("small node %d (%s) lost its mods", id, n.Name)
				}
			}
		}
	}
	if notables == 0 {
		t.Fatal("no allocated notable in radius")
	}
	src := fmt.Sprintf("Tree:%d", socket)
	if got := sumFrom(res.DB, "ManaCost", src, 0); got != 10*float64(notables) {
		t.Errorf("ManaCost from %s = %v, want %v (%d notables)", src, got, 10*notables, notables)
	}
	if got := sumFrom(res.DB, "Damage", src, mfSpell); got != 20*float64(notables) {
		t.Errorf("spell Damage from %s = %v, want %v", src, got, 20*notables)
	}
	for _, name := range []string{"PassiveSkillHasOtherEffect", "NodeModifier"} {
		if len(res.DB.mods[name]) != 0 {
			t.Errorf("%s left in the mod list: %v", name, res.DB.mods[name])
		}
	}
	t.Logf("%d notables, %d small nodes in radius of %d", notables, small, socket)
}

// TestReplaceNodeMods is CalcSetup.lua:233-249 on synthetic lists.
func TestReplaceNodeMods(t *testing.T) {
	own := Mod{Name: "Life", Type: "INC", Value: 10.0, Source: "Tree:1"}
	flag := Mod{Name: "PassiveSkillHasOtherEffect", Type: "FLAG", Value: true, Source: "Tree:2"}
	node := func(m Mod) Mod {
		return Mod{Name: "NodeModifier", Type: "LIST", Value: modValue(m), Source: "Tree:2"}
	}
	mana := Mod{Name: "ManaCost", Type: "INC", Value: 5.0, Source: "Tree:2"}
	skill := Mod{
		Name:   extraSkill,
		Type:   "LIST",
		Value:  map[string]any{"skillId": "Foo", "level": 1.0},
		Source: "Tree:2",
	}
	minion := minionModifier(Mod{Name: "DamageTaken", Type: "INC", Value: 4.0, Source: "Tree:2"})
	for _, c := range []struct {
		name string
		in   []Mod
		want []Mod
	}{
		{"no flag", []Mod{own, node(mana)}, []Mod{own, node(mana)}},
		{"flag without NodeModifier", []Mod{own, flag}, []Mod{own, flag}},
		{"replaced in order", []Mod{own, flag, node(mana), node(skill)}, []Mod{mana, skill}},
		{"nested minion mod", []Mod{own, flag, node(minion)}, []Mod{minion}},
		{"flag false by its tag", []Mod{own, {Name: flag.Name, Type: "FLAG", Value: true, Source: "Tree:2",
			Tags: []map[string]any{{"type": "Condition", "var": "Never"}}}, node(mana)}, nil},
	} {
		got := replaceNodeMods(c.in)
		if c.want == nil {
			c.want = c.in
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%s:\n got %v\nwant %v", c.name, got, c.want)
		}
	}
	// The other two transforms only touch notables.
	for _, line := range []string{
		"notable passive skills in radius are transformed to instead grant: minions take 15% increased damage",
		"notable passive skills in radius are transformed to instead grant: minions have 10% reduced movement speed",
	} {
		if !isRadiusFuncLine(line) {
			t.Errorf("%q is not a radius function", line)
		}
	}
}

// TestRadiusJewelInClusterSocket: a radius jewel in a cluster jewel's
// socket affects no node. In a2nRZLW9ZVuu, Fortress Covenant sits in
// 44169, the Medium socket of a Large cluster jewel; the tree places the
// allocated notables Whirling Barrier (42917) and Counterweight (39761)
// within its Medium radius, but PoB's radius list for that socket is
// empty (the oracle shows env.radiusJewelList's entry with no nodes, as
// the spec's socket node is BuildSubgraph's and has no nodesInRadius), so
// both keep their own mods.
func TestRadiusJewelInClusterSocket(t *testing.T) {
	c := newCalc(t)
	r, _ := radiusByLabel("Medium")
	if got := nodesInRadius(c.data.Tree, c.nodePositions(), 44169, r); len(got) != 0 {
		t.Errorf("nodes in radius of cluster socket 44169: %v, want none", got)
	}
	res, err := c.Calc(corpusBuild(t, "a2nRZLW9ZVuu"))
	if err != nil {
		t.Fatal(err)
	}
	if got := sumFrom(res.DB, "CritChance", "Tree:39761", 0); got != 60 {
		t.Errorf("CritChance from Counterweight = %v, want 60", got)
	}
	if sumFromAnyFlags(res.DB, "BlockChance", "Tree:42917") != 8 {
		t.Errorf("BlockChance from Whirling Barrier = %v, want 8", sumFromAnyFlags(res.DB, "BlockChance", "Tree:42917"))
	}
	for _, l := range res.Unparsed {
		if strings.Contains(l, "Transformed to") {
			t.Errorf("unparsed radius line %q", l)
		}
	}
}

// sumFromAnyFlags sums stat's numeric mods from source whatever their flags.
func sumFromAnyFlags(db *ModDB, stat, source string) float64 {
	total := 0.0
	for _, m := range db.mods[stat] {
		if v, ok := m.Value.(float64); ok && m.Source == source {
			total += v
		}
	}
	return total
}

// TestExtraJewelFuncs: Ngamahu, Flame's Advance (50692) gives every
// non-unique jewel a Large radius that converts other damage type
// increases to fire and grants +4 Strength to small and notable passives
// (ModParser.lua:2658-2672); PoB builds the env a second time with them
// (CalcSetup.lua:1466-1475, 919-944). With the node allocated the oracle
// gives Str 570 for 3e_oEmQebpBA and 349 for 8zRIqztIGNif.
func TestExtraJewelFuncs(t *testing.T) {
	c := newCalc(t)
	for _, tc := range []struct {
		id  string
		str float64
	}{{"3e_oEmQebpBA", 570}, {"8zRIqztIGNif", 349}} {
		b := corpusBuild(t, tc.id)
		spec := b.ActiveSpec()
		for _, n := range []int{14996, 50692} {
			if !slices.Contains(spec.Nodes, n) {
				spec.Nodes = append(spec.Nodes, n)
			}
		}
		res, err := c.Calc(b)
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Stats["Str"]; got != tc.str {
			t.Errorf("%s: Str %v, want %v", tc.id, got, tc.str)
		}
		without, err := c.calc(b, calcOpts{extraJewelFuncs: []Mod{}})
		if err != nil {
			t.Fatal(err)
		}
		if without.Stats["Str"] >= tc.str {
			t.Errorf("%s: Str without the extra jewel functions %v, want less than %v",
				tc.id, without.Stats["Str"], tc.str)
		}
	}
}
