package engine

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"github.com/Mi7teR/poe-lib/gamedata"
)

// gvLUT is the Glorious Vanity table (DataLegionLookUpTableHelper.lua):
// a byte per (node, seed) giving the length of that seed's record, then
// each node's records in node index order.
type gvLUT struct {
	sizes   []byte
	starts  []int // byte offset of each node index's block
	data    []byte
	seedLen int
}

const (
	gvSeedMin = 100
	gvSeedMax = 8000
)

func (l *timelessLUTs) gloriousVanity(t *gamedata.Timeless) (*gvLUT, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.gv != nil {
		return l.gv, nil
	}
	if l.dir == "" {
		return nil, fmt.Errorf("glorious vanity: no lookup table")
	}
	var compressed bytes.Buffer
	for part := 0; ; part++ {
		raw, err := os.ReadFile(filepath.Join(l.dir, "GloriousVanity.zip.part"+strconv.Itoa(part)))
		if err != nil {
			if part == 0 {
				return nil, err
			}
			break
		}
		compressed.Write(raw)
	}
	zr, err := zlib.NewReader(&compressed)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	seedLen := gvSeedMax - gvSeedMin + 1
	sizeOffset := t.Size * seedLen
	if sizeOffset+1 > len(b) {
		return nil, fmt.Errorf("glorious vanity table too short")
	}
	// Nodes in index order; their blocks follow the sizes, the first one
	// starting at byte sizeOffset (0-based), as in loadTimelessJewel.
	byIndex := make([]int, t.Size)
	for id, idx := range t.NodeIndex {
		if idx >= 0 && idx < t.Size {
			byIndex[idx] = id
		}
	}
	starts := make([]int, t.Size)
	off := sizeOffset
	for i := range byIndex {
		starts[i] = off
		off += t.NodeSize[byIndex[i]]
	}
	l.gv = &gvLUT{sizes: b[:sizeOffset+1], starts: starts, data: b, seedLen: seedLen}
	return l.gv, nil
}

// read is readLUT for jewel type 1: the record of one node and seed, with
// legion ids converted (the first byte for headers of 2 and 3, the first
// half for 6 and 8).
func (g *gvLUT) read(index, seed int, localToGlobal map[int]int) []int {
	seedOffset := seed - gvSeedMin
	if index < 0 || index >= len(g.starts) || seedOffset < 0 || seedOffset >= g.seedLen {
		return nil
	}
	base := index * g.seedLen
	off := g.starts[index]
	for k := 0; k < seedOffset; k++ {
		off += int(g.sizes[base+k])
	}
	n := int(g.sizes[base+seedOffset])
	if off+n > len(g.data) {
		return nil
	}
	out := make([]int, n)
	for i := range out {
		out[i] = int(g.data[off+i])
	}
	conv := func(v int) int {
		if gl, ok := localToGlobal[v]; ok {
			return gl
		}
		return v
	}
	switch n {
	case 2, 3:
		out[0] = conv(out[0])
	case 6, 8:
		for i := 0; i < n/2; i++ {
			out[i] = conv(out[i])
		}
	}
	return out
}

// legionLines fills a legion node's rolled values: its stat lines in
// order with the value for each sortedStats index.
func legionLines(ln *gamedata.LegionNode, value func(sm gamedata.LegionStat) float64) []string {
	var out []string
	for i, line := range ln.Stats {
		if i < len(ln.SortedStats) {
			if sm, ok := ln.StatMods[ln.SortedStats[i]]; ok {
				line = fixLegionStat(line, ln.SortedStats[i], sm, value(sm))
			}
		}
		out = append(out, line)
	}
	return out
}

// gvStats is the Glorious Vanity branch of BuildAllDependsAndPaths for
// notables and normal nodes.
func (c *Calculator) gvStats(node *gamedata.Node, id int, cq conquest, res *Result) ([]string, bool) {
	t := c.data.Timeless
	lut, err := c.luts.gloriousVanity(t)
	if err != nil {
		res.Incomplete = append(res.Incomplete, "Glorious Vanity: "+err.Error())
		return node.Stats, false
	}
	index, ok := t.NodeIndex[id]
	if !ok || cq.seed < gvSeedMin || cq.seed > gvSeedMax {
		return node.Stats, false
	}
	tbl := lut.read(index, cq.seed, t.LocalToGlobal[1])
	if len(tbl) == 0 {
		return node.Stats, false
	}
	legion := func(i int) *gamedata.LegionNode {
		if i >= 0 && i < len(t.Nodes) {
			return &t.Nodes[i]
		}
		return nil
	}
	at := func(i int) float64 {
		if i >= 0 && i < len(tbl) {
			return float64(tbl[i])
		}
		return 0
	}
	if !node.IsNotable {
		// Normal node: replaced by the legion node, its first line set to the
		// second byte (each line replaces the node's lines; ipairs stops after one).
		ln := legion(tbl[0] - timelessAdditions)
		if ln == nil || len(ln.Stats) == 0 {
			return node.Stats, false
		}
		lines := legionLines(ln, func(gamedata.LegionStat) float64 { return at(1) })
		return lines[:1], true
	}
	switch len(tbl) {
	case 2, 3:
		ln := legion(tbl[0] - timelessAdditions)
		if ln == nil {
			return node.Stats, false
		}
		return legionLines(ln, func(sm gamedata.LegionStat) float64 { return at(sm.Index) }), true
	case 6, 8:
		half := len(tbl) / 2
		bias := 0
		for _, v := range tbl[:half] {
			if v <= 21 {
				bias++
			} else {
				bias--
			}
		}
		base := legion(76) // legionNodes[77]: might of the vaal
		if bias < 0 {
			base = legion(77) // legionNodes[78]: legacy of the vaal
		}
		var lines []string
		if base != nil {
			lines = append(lines, base.Stats...)
		}
		additions := map[int]int{}
		var order []int
		for i := 0; i < half; i++ {
			if _, ok := additions[tbl[i]]; !ok {
				order = append(order, tbl[i])
			}
			additions[tbl[i]] += tbl[i+half]
		}
		sort.Ints(order) // PoB walks a hash table; the order does not change sums
		for _, a := range order {
			if a < 0 || a >= len(t.Additions) {
				continue
			}
			add := &t.Additions[a]
			for _, line := range add.Stats {
				for k, sm := range add.StatMods {
					line = fixLegionStat(line, k, sm, float64(additions[a]))
				}
				lines = append(lines, line)
			}
		}
		return lines, true
	}
	res.Incomplete = append(res.Incomplete, fmt.Sprintf("Glorious Vanity header size %d", len(tbl)))
	return node.Stats, false
}
