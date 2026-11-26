package engine

import (
	"slices"
	"testing"

	"github.com/Mi7teR/exile-calc/pobcode"
)

// PassiveSpec.lua ApplyLegacyClusterNodeRemap: fmxcrOoQ46rS is a format-1
// spec whose Watcher's Eye sits in 16218, the old id of the medium socket
// its large cluster in 7960 creates (29712 now). The socket allocation and
// the jewel move to 29712; a format-2 copy is left as it is.
func TestLegacyClusterSpecMovesSocket(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "fmxcrOoQ46rS")
	spec := b.ActiveSpec()
	if spec.ClusterHashFormatVersion != 1 {
		t.Fatalf("version %d, want 1 (no attribute)", spec.ClusterHashFormatVersion)
	}
	got := c.legacyClusterSpec(spec, b)
	if slices.Contains(got.Nodes, 16218) || !slices.Contains(got.Nodes, 29712) {
		t.Errorf("allocation not moved from 16218 to 29712")
	}
	item := map[int]int{}
	for _, s := range got.Sockets {
		item[s.NodeID] = s.ItemID
	}
	if item[16218] != 0 || item[29712] == 0 {
		t.Errorf("jewel not moved to 29712: %v", got.Sockets)
	}
	v2 := *spec
	v2.ClusterHashFormatVersion = 2
	if c.legacyClusterSpec(&v2, b) != &v2 {
		t.Error("a format-2 spec must not be remapped")
	}
}

func TestTranslateClusterOrbitIndex(t *testing.T) {
	for src := range 12 {
		if back := translateClusterOrbitIndex(translateClusterOrbitIndex(src, 12, 16), 16, 12); back != src {
			t.Errorf("12->16->12: %d -> %d", src, back)
		}
	}
	if got := translateClusterOrbitIndex(5, 6, 16); got != 13 {
		t.Errorf("6->16 of 5 = %d, want 13", got)
	}
}

// PassiveSpec.lua BuildLegacyProxyGroup: a medium cluster (sizeIndex 1) in
// the large socket 55190 creates its socket 49080 from the proxy group,
// while the old parser downsized the group and placed it at 36931
// (1U1EWET8OeON's Witchbane).
func TestLegacyProxyGroupSocket(t *testing.T) {
	c := newCalc(t)
	tree := c.data.Tree
	large := tree.Nodes[55190]
	proxy := tree.Nodes[large.ExpansionJewel.Proxy]
	group := tree.Groups[proxy.Group-1].Nodes
	legacy := legacyProxyGroup(tree, group, large.ExpansionJewel.Size, 1)
	if s := findClusterSocket(tree, group, 0); s == nil || s.ID != 49080 {
		t.Fatalf("current socket %v, want 49080", s)
	}
	if s := findClusterSocket(tree, legacy, 0); s == nil || s.ID != 36931 {
		t.Fatalf("legacy socket %v, want 36931", s)
	}
	b := corpusBuild(t, "1U1EWET8OeON")
	m := map[int]int{}
	c.clusterGraphLegacy(b.ActiveSpec(), b, &Result{}, m)
	if m[36931] != 49080 {
		t.Errorf("legacy map 36931 -> %d, want 49080", m[36931])
	}
}

// PassiveSpec.lua:130-132: a socket saved with itemId 0 is not loaded, so
// it does not block the jewel moved onto it.
func TestLegacyClusterSpecEmptySocket(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "fmxcrOoQ46rS")
	spec := *b.ActiveSpec()
	spec.Sockets = append([]pobcode.Socket{{NodeID: 29712, ItemID: 0}}, spec.Sockets...)
	got := c.legacyClusterSpec(&spec, b)
	found := false
	for _, s := range got.Sockets {
		if s.NodeID == 29712 && s.ItemID != 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("jewel from 16218 lost: %v", got.Sockets)
	}
}

// TestClusterNotablesLowestID: two cluster notables share the name Evil
// Eye; the map keeps the lowest id on every Calculator (PoB's pairs order
// is undefined).
func TestClusterNotablesLowestID(t *testing.T) {
	var ids []int
	c := newCalc(t)
	for _, n := range c.data.Tree.Nodes {
		if n.Group == 0 && (n.IsNotable || n.IsKeystone) && n.Name == "Evil Eye" {
			ids = append(ids, n.ID)
		}
	}
	if len(ids) < 2 {
		t.Skipf("Evil Eye is no longer a duplicate cluster notable (%v)", ids)
	}
	want := slices.Min(ids)
	for range 5 {
		if got := newCalc(t).clusterNotablesByName()["Evil Eye"].ID; got != want {
			t.Fatalf("Evil Eye: id %d, want %d of %v", got, want, ids)
		}
	}
}
