package engine

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// TimelessDirEnv names the directory holding PoB's timeless jewel lookup
// tables (Data/TimelessJewelData/*.zip). cmd/gamedatagen downloads them
// into its cache and logs the path. Without them, timeless jewels are
// reported in Result.Incomplete instead of being applied.
const TimelessDirEnv = "POE_LIB_TIMELESS_DIR"

// TimelessCacheEnv overrides where decoded timeless tables are cached:
// "off" turns the cache off, any other value is the cache directory.
// Unset, the cache is <user cache dir>/poe-lib/timeless-decoded.
const TimelessCacheEnv = "POE_LIB_TIMELESS_CACHE"

// timelessCacheMaxBytes caps the decoded table cache; the oldest files go
// first. timelessTmpMaxAge is the age after which a temporary file is
// taken as left by a dead process.
const (
	timelessCacheMaxBytes = 512 << 20
	timelessTmpMaxAge     = time.Hour
)

// PassiveSpec.lua timelessJewelTypeByConqueror and Data.lua seed ranges.
var (
	timelessTypeByConqueror = map[string]int{
		"vaal": 1, "karui": 2, "maraketh": 3, "templar": 4, "eternal": 5, "kalguur": 6,
		"abyss_murderous": 7, "abyss_searching": 8, "abyss_hypnotic": 9, "abyss_ghastly": 10, "abyss_special": 11,
	}
	timelessFiles   = map[int]string{2: "LethalPride", 3: "BrutalRestraint", 4: "MilitantFaith", 5: "ElegantHubris", 6: "HeroicTragedy"}
	timelessSeedMin = map[int]int{2: 10000, 3: 500, 4: 2000, 5: 2000 / 20, 6: 100}
	timelessSeedMax = map[int]int{2: 18000, 3: 8000, 4: 10000, 5: 160000 / 20, 6: 8000}
)

const timelessAdditions = 337 // Data.lua timelessJewelAdditions = #legionAdditions

type conquest struct {
	seed      int
	jewelType int
	conqID    string // conqueror id: "1", "2_v2", ...
	conqType  string
	abyss     []abyssComponent // types 7-11: the modification of this node
}

// timelessLUTs holds the timeless jewel tables, each read on first use.
// Each table has its own entry, so the first use of one table does not
// wait for another one to load.
type timelessLUTs struct {
	dir string
	// cacheDir holds the decoded tables (timelessCacheFile); "" turns the
	// cache off.
	cacheDir string
	mu       sync.Mutex
	entries  map[int]*lutEntry // by jewel type
}

// lutEntry is one table: v is set once it has loaded. A load that fails
// leaves v nil, and the next use tries again.
type lutEntry struct {
	mu sync.Mutex
	v  any
}

func newTimelessLUTs() *timelessLUTs {
	l := &timelessLUTs{dir: defaultTimelessDir()}
	if l.dir != "" {
		l.cacheDir = timelessCacheDir()
	}
	return l
}

// load returns the table of jewelType, loading it with f on first use.
func (l *timelessLUTs) load(jewelType int, f func() (any, error)) (any, error) {
	l.mu.Lock()
	if l.entries == nil {
		l.entries = map[int]*lutEntry{}
	}
	e := l.entries[jewelType]
	if e == nil {
		e = &lutEntry{}
		l.entries[jewelType] = e
	}
	l.mu.Unlock()
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.v != nil {
		return e.v, nil
	}
	v, err := f()
	if err != nil {
		return nil, err
	}
	e.v = v
	return v, nil
}

func defaultTimelessDir() string {
	d := os.Getenv(TimelessDirEnv)
	if d == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return ""
		}
		d = filepath.Join(cache, "poe-lib", "pob-v2.68.1-7", "src", "Data", "TimelessJewelData")
	}
	if st, err := os.Stat(d); err != nil || !st.IsDir() {
		return ""
	}
	return d
}

// table loads one jewel type's table (DataJewelFileLoader.lua: zlib data).
func (l *timelessLUTs) table(jewelType int) ([]byte, error) {
	v, err := l.load(jewelType, func() (any, error) {
		name, ok := timelessFiles[jewelType]
		if !ok || l.dir == "" {
			return nil, fmt.Errorf("timeless jewel type %d: no lookup table", jewelType)
		}
		return l.inflate(name, []string{name + ".zip"})
	})
	if err != nil {
		return nil, err
	}
	b, _ := v.([]byte)
	return b, nil
}

// partFiles lists name.zip.part0, part1, ... as they exist in l.dir
// (DataJewelFileLoader.lua reads the parts in order until one is missing).
func (l *timelessLUTs) partFiles(name string) []string {
	var out []string
	for part := 0; ; part++ {
		f := name + ".zip.part" + strconv.Itoa(part)
		if _, err := os.Stat(filepath.Join(l.dir, f)); err != nil {
			return out
		}
		out = append(out, f)
	}
}

// timelessCacheFormat versions the layout of the decoded table files.
const timelessCacheFormat = 1

// timelessCacheMagic starts a decoded table file; a CRC-32C of the table
// and its length follow, then the table.
const timelessCacheMagic = "PLTD"

// inflate is files of l.dir, concatenated and zlib decompressed. The
// decoded table is kept in l.cacheDir, so later processes read it
// instead of decompressing again (a 100 MB table decompresses in about
// 0.3 s and reads in about 10 ms). A cache file is named after the
// table, timelessCacheFormat and each source file's name, size and
// modification time, so a changed source gets a new file; one that does
// not hold the table it claims (length, CRC-32C) is decoded again.
func (l *timelessLUTs) inflate(name string, files []string) ([]byte, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no lookup table", name)
	}
	cacheFile := ""
	if l.cacheDir != "" {
		cacheFile = timelessCacheFile(l.cacheDir, l.dir, name, files)
	}
	if cacheFile != "" {
		if b, ok := readTimelessCache(cacheFile); ok {
			return b, nil
		}
	}
	var compressed bytes.Buffer
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(l.dir, f))
		if err != nil {
			return nil, err
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
	if cacheFile != "" {
		writeTimelessCache(cacheFile, b)
	}
	return b, nil
}

// timelessCacheFile is the decoded table file of name from files; "" when
// a source file cannot be read.
func timelessCacheFile(cacheDir, dir, name string, files []string) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%d\x00%s\x00", timelessCacheFormat, name)
	for _, f := range files {
		st, err := os.Stat(filepath.Join(dir, f))
		if err != nil {
			return ""
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00", f, st.Size(), st.ModTime().UnixNano())
	}
	return filepath.Join(cacheDir, fmt.Sprintf("%s-%016x.bin", name, h.Sum64()))
}

// readTimelessCache reads a decoded table file written by
// writeTimelessCache; false when it is missing or does not check out.
func readTimelessCache(path string) ([]byte, bool) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) < 16 || string(raw[:4]) != timelessCacheMagic {
		return nil, false
	}
	sum := binary.LittleEndian.Uint32(raw[4:8])
	n := binary.LittleEndian.Uint64(raw[8:16])
	b := raw[16:]
	if uint64(len(b)) != n || crc32.Checksum(b, crc32.MakeTable(crc32.Castagnoli)) != sum {
		return nil, false
	}
	return b, true
}

// timelessCacheDir is the cache directory by TimelessCacheEnv; "" when off.
func timelessCacheDir() string {
	switch v := os.Getenv(TimelessCacheEnv); v {
	case "off":
		return ""
	case "":
		if cache, err := os.UserCacheDir(); err == nil {
			return filepath.Join(cache, "poe-lib", "timeless-decoded")
		}
		return ""
	default:
		return v
	}
}

// pruneTimelessCache runs after path was written: it removes the other
// versions of the same table (<name>-*.bin), temporary files older than
// timelessTmpMaxAge, and then the oldest tables until the directory fits
// timelessCacheMaxBytes. Errors are ignored; the cache is best effort.
func pruneTimelessCache(path string, now time.Time) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	name, _, _ := strings.Cut(base, "-")
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	var kept []file
	total := int64(0)
	for _, e := range ents {
		info, ierr := e.Info()
		if ierr != nil || e.IsDir() {
			continue
		}
		n, p := e.Name(), filepath.Join(dir, e.Name())
		switch {
		case strings.Contains(n, ".tmp"):
			if now.Sub(info.ModTime()) > timelessTmpMaxAge {
				_ = os.Remove(p)
			}
			continue
		case !strings.HasSuffix(n, ".bin"):
			continue
		case n != base && strings.HasPrefix(n, name+"-"):
			_ = os.Remove(p)
			continue
		}
		kept = append(kept, file{p, info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].mod.Before(kept[j].mod) })
	for _, f := range kept {
		if total <= timelessCacheMaxBytes {
			break
		}
		if f.path == path {
			continue
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}

// writeTimelessCache writes b to path through a temporary file and a
// rename, so a reader in another process sees either no file or the whole
// one. A failure leaves no cache; the table was decoded anyway.
func writeTimelessCache(path string, b []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return
	}
	var hdr [16]byte
	copy(hdr[:4], timelessCacheMagic)
	binary.LittleEndian.PutUint32(hdr[4:8], crc32.Checksum(b, crc32.MakeTable(crc32.Castagnoli)))
	binary.LittleEndian.PutUint64(hdr[8:16], uint64(len(b)))
	_, err = tmp.Write(hdr[:])
	if err == nil {
		_, err = tmp.Write(b)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return
	}
	pruneTimelessCache(path, time.Now())
}

// readLUT ports DataLegionLookUpTableHelper.lua readLUT for jewel types
// 2-6: one byte per (notable index, seed), mapped to a legion id.
func (c *Calculator) readLUT(cq conquest, nodeID int) ([]int, error) {
	t := c.data.Timeless
	lut, err := c.luts.table(cq.jewelType)
	if err != nil {
		return nil, err
	}
	index, ok := t.NodeIndex[nodeID]
	if !ok || index > t.SizeNotable {
		return nil, nil
	}
	seedSize := timelessSeedMax[cq.jewelType] - timelessSeedMin[cq.jewelType] + 1
	pos := index*seedSize + (cq.seed - timelessSeedMin[cq.jewelType])
	if pos < 0 || pos >= len(lut) {
		return nil, fmt.Errorf("timeless table out of range")
	}
	local := int(lut[pos])
	if g, ok := t.LocalToGlobal[cq.jewelType][local]; ok {
		local = g
	}
	return []int{local}, nil
}

// readConquest reads a timeless jewel's conqueredBy from its parsed mods.
func readConquest(v any) (conquest, bool) {
	m, _ := v.(map[string]any)
	if m["key"] != "conqueredBy" {
		return conquest{}, false
	}
	val, _ := m["value"].(map[string]any)
	who, _ := val["conqueror"].(map[string]any)
	seed, _ := val["id"].(float64)
	typ, _ := who["type"].(string)
	var id string
	switch x := who["id"].(type) {
	case float64:
		id = strconv.Itoa(int(x))
	case string:
		id = x
	}
	jt := timelessTypeByConqueror[typ]
	if jt == 0 {
		jt = 5
	}
	s := int(seed)
	if jt == 5 {
		s = int(seed) / 20 // Elegant Hubris seeds come in steps of 20
	}
	return conquest{seed: s, jewelType: jt, conqID: id, conqType: typ}, true
}

// conquests finds the nodes each allocated timeless jewel conquers
// (PassiveSpec.lua: node.conqueredBy for nodes in the jewel's radius).
func (c *Calculator) conquests(spec *pobcode.Spec, b *pobcode.Build, alloc map[int]bool, res *Result) map[int]conquest {
	out := map[int]conquest{}
	var pos map[int]point
	for _, s := range spec.Sockets {
		if s.ItemID == 0 || !alloc[s.NodeID] {
			continue
		}
		item := b.Item(s.ItemID)
		if item == nil {
			continue
		}
		it, mods := c.itemMods(item, res)
		if it == nil {
			continue
		}
		for _, m := range mods {
			if m.Name != "JewelData" || m.Type != "LIST" {
				continue
			}
			cq, ok := readConquest(m.Value)
			if !ok {
				continue
			}
			if cq.jewelType >= 7 {
				// BuildAllDependsAndPaths: abyss changes are read per socket
				// (Zorath: along the path to the class start), not by radius.
				if n := c.data.Tree.Nodes[s.NodeID]; n != nil && n.ExpansionJewel != nil && n.ExpansionJewel.Parent != 0 {
					continue
				}
				lut, err := c.luts.abyss(cq.jewelType, c.data.Timeless.LocalToGlobal[cq.jewelType])
				if err != nil {
					res.Incomplete = append(res.Incomplete, "abyssal timeless jewel: "+err.Error())
					continue
				}
				var path []int
				if cq.jewelType == 11 {
					path = pathToClassStart(c.data.Tree, alloc, s.NodeID, spec.ClassID, nil)
				}
				for id, mod := range lut.read(cq.seed, s.NodeID, path) {
					a := cq
					a.abyss = mod
					out[id] = a
				}
				continue
			}
			r, ok := radiusByLabel(it.Radius)
			if !ok {
				continue
			}
			if pos == nil {
				pos = c.nodePositions()
			}
			for _, id := range nodesInRadius(c.data.Tree, pos, s.NodeID, r) {
				out[id] = cq
			}
		}
	}
	return out
}

func radiusByLabel(label string) (radius, bool) {
	for _, r := range jewelRadii {
		if r.label == label {
			return r, true
		}
	}
	return radius{}, false
}

// pobConqueredBy reports whether PoB sets node.conqueredBy for a node in
// conquests' map (PassiveSpec.lua:1192 abyss conquests on any node;
// 1196-1213 radius conquests skip ClassStart, Socket and ascendancy nodes).
func pobConqueredBy(node *gamedata.Node, cq conquest) bool {
	const firstAbyssType = 7 // timelessTypeByConqueror: abyss_* are 7-11
	if cq.jewelType >= firstAbyssType {
		return true
	}
	return node.ClassStartIndex == nil && !node.IsJewelSocket && node.AscendancyName == ""
}

// otherFuncsFor is the node's first pass radius functions: none when a
// timeless jewel conquered it, as jewelOtherFuncs skip nodes with
// node.conqueredBy (ModParser.lua:6683-6697).
func otherFuncsFor(funcs []radiusFunc, node *gamedata.Node, cq conquest, conquered bool) []radiusFunc {
	if conquered && pobConqueredBy(node, cq) {
		return nil
	}
	return funcs
}

var attributeNodeNames = map[string]bool{"Dexterity": true, "Intelligence": true, "Strength": true}

// conqueredStats ports the conquest part of PassiveSpecClass:BuildAllDependsAndPaths
// for jewel types 1-6: it returns the node's stats after conquest and the
// source name PoB gives them.
func (c *Calculator) conqueredStats(node *gamedata.Node, id int, cq conquest, res *Result) ([]string, string, bool) {
	t := c.data.Timeless
	src := fmt.Sprintf("Tree:%d", id)
	if t == nil {
		return node.Stats, src, false
	}
	if cq.jewelType == 1 && !node.IsKeystone && !node.IsMastery && !node.IsJewelSocket && node.ClassStartIndex == nil && !node.IsAscendancyStart {
		stats, ok := c.gvStats(node, id, cq, res)
		return stats, src, ok
	}
	replace := func(index int) ([]string, string) {
		if index < 0 || index >= len(t.Nodes) {
			return node.Stats, src
		}
		return t.Nodes[index].Stats, "Tree:" + t.Nodes[index].ID
	}
	if cq.jewelType >= 7 {
		if node.IsJewelSocket {
			return node.Stats, src, false
		}
		return c.abyssStats(node.Stats, cq.abyss, res), src, true
	}
	switch {
	case node.IsKeystone:
		want := cq.conqType + "_keystone_" + cq.conqID
		for i, ln := range t.Nodes {
			if ln.ID == want {
				s, so := replace(i)
				return s, so, true
			}
		}
	case node.IsNotable:
		ids, err := c.readLUT(cq, id)
		if err != nil {
			res.Incomplete = append(res.Incomplete, "timeless jewel: "+err.Error())
			return node.Stats, src, false
		}
		stats := append([]string(nil), node.Stats...)
		so := src
		for _, v := range ids {
			if v >= timelessAdditions {
				stats, so = replace(v - timelessAdditions)
			} else if v < len(t.Additions) {
				stats = append(stats, t.Additions[v].Stats...)
			}
		}
		return stats, so, true
	case node.IsMastery || node.IsJewelSocket || node.ClassStartIndex != nil || node.IsAscendancyStart:
		return node.Stats, src, false
	default: // Normal
		small := attributeNodeNames[node.Name]
		add := func(line string) ([]string, string, bool) {
			return append(append([]string(nil), node.Stats...), line), src, true
		}
		switch cq.conqType {
		case "karui":
			return add(map[bool]string{true: "+2 to Strength", false: "+4 to Strength"}[small])
		case "maraketh":
			return add(map[bool]string{true: "+2 to Dexterity", false: "+4 to Dexterity"}[small])
		case "kalguur":
			return add(map[bool]string{true: "1% increased Ward", false: "2% increased Ward"}[small])
		case "templar":
			if small {
				s, so := replace(90) // legionNodes[91]: templar_devotion_node
				return s, so, true
			}
			return add("+5 to Devotion")
		case "eternal":
			s, so := replace(109) // legionNodes[110]: eternal_small_blank
			return s, so, true
		}
	}
	return node.Stats, src, false
}
