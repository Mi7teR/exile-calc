package engine

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/Mi7teR/exile-calc/gamedata"
)

// The abyssal timeless jewels of 3.30 (jewel types 7-11) port
// Modules/DataAbyssJewelLookUpTableHelper.lua and the Abyss branch of
// PassiveSpecClass:BuildAllDependsAndPaths.

// abyssFiles names data.timelessJewelTypes 7-11 without spaces.
var abyssFiles = map[int]string{7: "AbyssTecrod", 8: "AbyssUlaman", 9: "AbyssKurgal", 10: "AbyssAmanamu", 11: "AbyssZorath"}

// abyssComponent is one part of a modification: type 1 replaces the node
// with a legion node, type 2 adds a legion addition; rolls are its stat values.
type abyssComponent struct {
	typ   int
	id    int
	rolls []int
}

type abyssLUT struct {
	data                      []byte
	format                    string // ABYS (by socket, types 7-10) or ABYN (by node, Zorath)
	seedMin, seedMax, seedInc int
	seedCount                 int
	blocks                    map[int]int    // socket or node id -> offset of its first record
	ascendancies              map[string]int // ABYN: ascendancy name -> offset
	// seedOffsets memoises recordOffsets per block; mu guards it, as
	// concurrent Calcs read one table.
	mu            sync.Mutex
	seedOffsets   map[string][]int
	localToGlobal map[int]int
}

func u16(b []byte, off int) int { return int(binary.LittleEndian.Uint16(b[off:])) }

// skipModification: a count of components, each with type, id, stat count
// and two bytes per stat.
func skipModification(b []byte, off int) int {
	n := int(b[off])
	off++
	for range n {
		off += 3 + int(b[off+2])*2
	}
	return off
}

func skipSocketRecord(b []byte, off int) int {
	n := int(b[off])
	off++
	for range n {
		off = skipModification(b, off+2)
	}
	return off
}

func skipAscendancyRecord(b []byte, off int) int { return off + 1 + int(b[off])*2 }

func (l *abyssLUT) readModification(off int) []abyssComponent {
	b := l.data
	n := int(b[off])
	off++
	out := make([]abyssComponent, 0, n)
	for range n {
		typ, local, stats := int(b[off]), int(b[off+1]), int(b[off+2])
		off += 3
		id := local
		if g, ok := l.localToGlobal[local]; ok {
			id = g
		}
		c := abyssComponent{typ: typ, id: id}
		for range stats {
			c.rolls = append(c.rolls, int(int16(binary.LittleEndian.Uint16(b[off:]))))
			off += 2
		}
		out = append(out, c)
	}
	return out
}

// parseAbyss is parseAbyssJewel.
func parseAbyss(jewelType int, b []byte, localToGlobal map[int]int) (*abyssLUT, error) {
	if len(b) < 12 {
		return nil, fmt.Errorf("abyss table too short")
	}
	l := &abyssLUT{data: b, format: string(b[0:4]), seedMin: u16(b, 6), seedMax: u16(b, 8), seedInc: u16(b, 10),
		blocks: map[int]int{}, seedOffsets: map[string][]int{}, localToGlobal: localToGlobal}
	if b[4] != 1 || int(b[5]) != jewelType || l.seedInc == 0 {
		return nil, fmt.Errorf("invalid abyss timeless jewel header for jewel type %d", jewelType)
	}
	l.seedCount = (l.seedMax-l.seedMin)/l.seedInc + 1
	off := 12
	switch l.format {
	case "ABYS":
		count := int(b[off])
		off += 2 // socket count, abyssSize
		ids := make([]int, count)
		for i := range ids {
			ids[i] = u16(b, off)
			off += 2
		}
		for _, id := range ids {
			l.blocks[id] = off
			for range l.seedCount {
				off = skipSocketRecord(b, off)
			}
		}
	case "ABYN":
		count := u16(b, off)
		off += 2
		ids := make([]int, count)
		for i := range ids {
			ids[i] = u16(b, off)
			off += 2
		}
		for _, id := range ids {
			l.blocks[id] = off
			for range l.seedCount {
				off = skipModification(b, off)
			}
		}
		if string(b[off:off+4]) != "ASCS" {
			return nil, fmt.Errorf("missing abyss ascendancy section for jewel type %d", jewelType)
		}
		off += 4
		n := u16(b, off)
		off += 2
		l.ascendancies = map[string]int{}
		for range n {
			nameLen := int(b[off])
			name := string(b[off+1 : off+1+nameLen])
			off += nameLen + 1
			l.ascendancies[name] = off
			for range l.seedCount {
				off = skipAscendancyRecord(b, off)
			}
		}
	default:
		return nil, fmt.Errorf("unknown abyss timeless jewel format %q", l.format)
	}
	return l, nil
}

func (l *abyssLUT) seedIndex(seed int) (int, bool) {
	o := seed - l.seedMin
	if o < 0 || seed > l.seedMax || o%l.seedInc != 0 {
		return 0, false
	}
	return o / l.seedInc, true
}

// recordOffsets is getRecordOffsets: where each seed's record of a block starts.
func (l *abyssLUT) recordOffsets(key string, start int, skip func([]byte, int) int) []int {
	l.mu.Lock()
	defer l.mu.Unlock()
	if offs, ok := l.seedOffsets[key]; ok {
		return offs
	}
	offs := make([]int, l.seedCount)
	off := start
	for i := range offs {
		offs[i] = off
		off = skip(l.data, off)
	}
	l.seedOffsets[key] = offs
	return offs
}

// abyss loads one abyssal jewel table (DataJewelFileLoader.lua: the
// concatenated .zip.partN files, zlib compressed).
func (l *timelessLUTs) abyss(jewelType int, localToGlobal map[int]int) (*abyssLUT, error) {
	v, err := l.load(jewelType, func() (any, error) {
		name, ok := abyssFiles[jewelType]
		if !ok || l.dir == "" {
			return nil, fmt.Errorf("abyss jewel type %d: no lookup table", jewelType)
		}
		b, err := l.inflate(name, l.partFiles(name))
		if err != nil {
			return nil, err
		}
		return parseAbyss(jewelType, b, localToGlobal)
	})
	if err != nil {
		return nil, err
	}
	t, _ := v.(*abyssLUT)
	return t, nil
}

// readAbyss is readAbyssJewelLUT: the nodes a jewel changes and how. Types
// 7-10 read the socket's record; Zorath (11) reads each node of the path to
// the class start and the node it selects in every ascendancy.
func (l *abyssLUT) read(seed, socket int, path []int) map[int][]abyssComponent {
	out := map[int][]abyssComponent{}
	si, ok := l.seedIndex(seed)
	if !ok {
		return out
	}
	readNode := func(id int) []abyssComponent {
		start, ok := l.blocks[id]
		if !ok || l.format != "ABYN" {
			return nil
		}
		return l.readModification(l.recordOffsets(strconv.Itoa(id), start, skipModification)[si])
	}
	if l.format == "ABYS" {
		start, ok := l.blocks[socket]
		if !ok {
			return out
		}
		off := l.recordOffsets(strconv.Itoa(socket), start, skipSocketRecord)[si]
		n := int(l.data[off])
		off++
		for range n {
			id := u16(l.data, off)
			mod := l.readModification(off + 2)
			out[id] = mod
			off = skipModification(l.data, off+2)
		}
		return out
	}
	for _, id := range path {
		if m := readNode(id); len(m) > 0 {
			out[id] = m
		}
	}
	for name, start := range l.ascendancies {
		off := l.recordOffsets("ascendancy:"+name, start, skipAscendancyRecord)[si]
		n := int(l.data[off])
		off++
		for range n {
			id := u16(l.data, off)
			off += 2
			out[id] = readNode(id)
		}
	}
	return out
}

// pathToClassStart is PassiveSpecClass:GetShortestPathToClassStart: a
// breadth-first search from the socket through allocated nodes. Links are
// a node's out then in ids, as PassiveTree.lua builds linkedId, then the
// cluster subgraph links (extra).
func pathToClassStart(tree *gamedata.Tree, alloc map[int]bool, root, classID int, extra map[int][]int) []int {
	if !alloc[root] {
		return nil
	}
	target := -1
	for id, n := range tree.Nodes {
		if n.ClassStartIndex != nil && *n.ClassStartIndex == classID {
			target = id
		}
	}
	nodeType := func(n *gamedata.Node) string {
		switch {
		case n == nil:
			return "" // a cluster subgraph node
		case n.ClassStartIndex != nil:
			return "ClassStart"
		case n.IsAscendancyStart:
			return "AscendClassStart"
		case n.IsMastery:
			return "Mastery"
		}
		return ""
	}
	parent := map[int]int{}
	queue := []int{root}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		node := tree.Nodes[id]
		var linked []int
		if node != nil {
			linked = append(append(linked, node.Out...), node.In...)
		}
		for _, other := range append(linked, extra[id]...) {
			if other == target {
				path := []int{root, other}
				for cur, ok := id, true; ok; cur, ok = parent[cur] {
					path = append(path, cur)
				}
				return path
			}
			on := tree.Nodes[other]
			_, seen := parent[other]
			if alloc[other] && nodeType(node) != "Mastery" && nodeType(on) != "ClassStart" &&
				nodeType(on) != "AscendClassStart" && !seen && other != root {
				parent[other] = id
				queue = append(queue, other)
			}
		}
	}
	return nil
}

// abyssStats applies a modification to a node's stats: a replacement
// takes the legion node's lines, an addition appends the addition's lines,
// each with its rolled values filled in.
func (c *Calculator) abyssStats(stats []string, mods []abyssComponent, res *Result) []string {
	t := c.data.Timeless
	out := append([]string(nil), stats...)
	for _, comp := range mods {
		var ln *gamedata.LegionNode
		replace := false
		switch comp.typ {
		case 1:
			if i := comp.id - timelessAdditions; i >= 0 && i < len(t.Nodes) {
				ln, replace = &t.Nodes[i], true
			}
		case 2:
			if comp.id >= 0 && comp.id < len(t.Additions) {
				ln = &t.Additions[comp.id]
			}
		}
		if ln == nil {
			res.Incomplete = append(res.Incomplete, fmt.Sprintf("abyss jewel component %d/%d", comp.typ, comp.id))
			continue
		}
		var lines []string
		for _, line := range ln.Stats {
			for key, sm := range ln.StatMods {
				if sm.Index-1 < len(comp.rolls) && sm.Index >= 1 {
					line = fixLegionStat(line, key, sm, float64(comp.rolls[sm.Index-1]))
				}
			}
			lines = append(lines, line)
		}
		if replace {
			out = lines
		} else {
			out = append(out, lines...)
		}
	}
	return out
}

// fixLegionStat is replaceHelperFunc: the rolled value replaces the
// "(min-max)" range, or the minimum of a fixed value.
func fixLegionStat(line, key string, sm gamedata.LegionStat, value float64) string {
	if sm.Fmt == "g" {
		switch {
		case strings.Contains(key, "per_minute"):
			value = round(value/60, 1)
		case strings.Contains(key, "permyriad"):
			value /= 100
		case strings.Contains(key, "_ms"):
			value /= 1000
		}
	}
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	if sm.Min != sm.Max {
		return strings.ReplaceAll(line, "("+f(sm.Min)+"-"+f(sm.Max)+")", f(value))
	}
	if sm.Min != value {
		return strings.ReplaceAll(line, f(sm.Min), f(value))
	}
	return line
}
