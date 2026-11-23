package engine

import "testing"

// A synthetic table: 2 nodes, the seed range of Glorious Vanity, node 0
// with a 3-byte record at seed 101 and node 1 with a 6-byte one at 100.
func TestGloriousVanityRead(t *testing.T) {
	seedLen := gvSeedMax - gvSeedMin + 1
	sizes := make([]byte, 2*seedLen+1)
	sizes[0*seedLen+1] = 3 // node 0, seed 101
	sizes[1*seedLen+0] = 6 // node 1, seed 100
	data := append([]byte(nil), sizes...)
	data = data[:2*seedLen]                // the first block starts at byte 2*seedLen
	data = append(data, 5, 9, 1)           // node 0, seed 101
	data = append(data, 2, 30, 2, 4, 5, 6) // node 1, seed 100
	g := &gvLUT{sizes: sizes, starts: []int{2 * seedLen, 2*seedLen + 3}, data: data, seedLen: seedLen}
	if got := g.read(0, 101, map[int]int{5: 400}); len(got) != 3 || got[0] != 400 || got[1] != 9 {
		t.Errorf("node 0: %v", got)
	}
	if got := g.read(1, 100, map[int]int{2: 7}); len(got) != 6 || got[0] != 7 || got[2] != 7 || got[1] != 30 || got[3] != 4 {
		t.Errorf("node 1: %v", got)
	}
}
