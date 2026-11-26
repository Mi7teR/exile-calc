package engine

import (
	"slices"
	"testing"
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
	for src := 0; src < 12; src++ {
		if back := translateClusterOrbitIndex(translateClusterOrbitIndex(src, 12, 16), 16, 12); back != src {
			t.Errorf("12->16->12: %d -> %d", src, back)
		}
	}
	if got := translateClusterOrbitIndex(5, 6, 16); got != 13 {
		t.Errorf("6->16 of 5 = %d, want 13", got)
	}
}
