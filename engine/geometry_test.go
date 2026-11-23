package engine

import (
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
)

// Sizes PoB v2.68.1 reports for socket 26725 (socket.nodesInRadius); the
// full sets matched PoB for 8 sockets x 5 radii when this was written.
func TestNodesInRadiusMatchesPoB(t *testing.T) {
	d, err := gamedata.PoE1()
	if err != nil {
		t.Fatal(err)
	}
	pos := nodePositions(d.Tree)
	for idx, want := range map[int]int{0: 27, 2: 88, 4: 194, 6: 29, 8: 44} {
		if got := len(nodesInRadius(d.Tree, pos, 26725, jewelRadii[idx])); got != want {
			t.Errorf("radius %s (%d): %d nodes, PoB %d", jewelRadii[idx].label, idx, got, want)
		}
	}
}
