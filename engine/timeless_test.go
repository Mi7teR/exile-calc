package engine

import (
	"bytes"
	"compress/zlib"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Mi7teR/poe-lib/gamedata"
)

func TestPobConqueredBy(t *testing.T) {
	start := 0
	radius := conquest{jewelType: 3}
	abyss := conquest{jewelType: 7}
	for _, c := range []struct {
		name string
		node *gamedata.Node
		cq   conquest
		want bool
	}{
		{"normal", &gamedata.Node{}, radius, true},
		{"notable", &gamedata.Node{IsNotable: true}, radius, true},
		{"socket", &gamedata.Node{IsJewelSocket: true}, radius, false},
		{"class start", &gamedata.Node{ClassStartIndex: &start}, radius, false},
		{"ascendancy", &gamedata.Node{AscendancyName: "Guardian"}, radius, false},
		{"abyss socket", &gamedata.Node{IsJewelSocket: true}, abyss, true},
	} {
		if got := pobConqueredBy(c.node, c.cq); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestRadiusJewelSkipsConqueredNodes: jewelOtherFuncs leave nodes a
// timeless jewel conquered alone (ModParser.lua:6683-6697). In
// JtugGaLoWqgT, Unnatural Instinct nulls the allocated small node
// Tree:18033 but not Tree:11688, which Brutal Restraint conquered: that
// keeps its +4 Dexterity.
func TestRadiusJewelSkipsConqueredNodes(t *testing.T) {
	if defaultTimelessDir() == "" {
		t.Skip("timeless jewel tables not found")
	}
	res, err := newCalc(t).Calc(corpusBuild(t, "JtugGaLoWqgT"))
	if err != nil {
		t.Fatal(err)
	}
	sum := func(stat, source string) float64 {
		total := 0.0
		for _, r := range res.DB.Tabulate(stat) {
			if v, ok := r.Value.(float64); ok && r.Source == source {
				total += v
			}
		}
		return total
	}
	if got := sum("Dex", "Tree:11688"); got != 4 {
		t.Errorf("Dex from conquered Tree:11688 = %v, want 4", got)
	}
	if got := sum("Int", "Tree:18033"); got != 0 {
		t.Errorf("Int from nulled Tree:18033 = %v, want 0", got)
	}
}

// writeZlib writes data zlib compressed to path.
func writeZlib(t *testing.T, path string, data []byte) {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTimelessTableCache: a table read through the decoded table cache is
// the table decompressed; the cache file is written on first use, read
// afterwards, ignored when it does not check out, and replaced by a new
// file when the source changes.
func TestTimelessTableCache(t *testing.T) {
	dir, cache := t.TempDir(), t.TempDir()
	want := bytes.Repeat([]byte("lethal pride "), 1000)
	src := filepath.Join(dir, "LethalPride.zip")
	writeZlib(t, src, want)
	load := func() []byte {
		t.Helper()
		l := &timelessLUTs{dir: dir, cacheDir: cache}
		b, err := l.table(2)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if got := load(); !bytes.Equal(got, want) {
		t.Fatalf("first load: %d bytes, want %d", len(got), len(want))
	}
	file := timelessCacheFile(cache, dir, "LethalPride", []string{"LethalPride.zip"})
	cached, ok := readTimelessCache(file)
	if !ok || !bytes.Equal(cached, want) {
		t.Fatalf("cache file %s: ok %v, %d bytes", file, ok, len(cached))
	}
	// A corrupt cache file is not used.
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if err = os.WriteFile(file, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok = readTimelessCache(file); ok {
		t.Fatal("corrupt cache file accepted")
	}
	if got := load(); !bytes.Equal(got, want) {
		t.Fatal("load with a corrupt cache file")
	}
	// A changed source is a new cache file.
	want2 := bytes.Repeat([]byte("changed "), 500)
	writeZlib(t, src, want2)
	future := time.Now().Add(time.Hour)
	if err = os.Chtimes(src, future, future); err != nil {
		t.Fatal(err)
	}
	if got := load(); !bytes.Equal(got, want2) {
		t.Fatal("load after the source changed")
	}
	if timelessCacheFile(cache, dir, "LethalPride", []string{"LethalPride.zip"}) == file {
		t.Fatal("cache file name unchanged after the source changed")
	}
}

// TestTimelessTablesConcurrent loads tables from many goroutines: each
// table loads once and every caller gets it (run with -race).
func TestTimelessTablesConcurrent(t *testing.T) {
	dir := t.TempDir()
	for typ, name := range timelessFiles {
		writeZlib(t, filepath.Join(dir, name+".zip"), bytes.Repeat([]byte{byte(typ)}, 100))
	}
	l := &timelessLUTs{dir: dir, cacheDir: t.TempDir()}
	var wg sync.WaitGroup
	for i := range 40 {
		typ := 2 + i%5
		wg.Go(func() {
			b, err := l.table(typ)
			if err != nil || len(b) != 100 || b[0] != byte(typ) {
				t.Errorf("type %d: %d bytes, err %v", typ, len(b), err)
			}
		})
	}
	wg.Wait()
	if _, err := l.table(9); err == nil {
		t.Error("type 9 has no table, want an error")
	}
}

// TestTimelessCachePrune: a write removes the table's other versions and
// stale temporary files, keeps other tables and fresh temporary files,
// and evicts the oldest tables past the size cap.
func TestTimelessCachePrune(t *testing.T) {
	dir := t.TempDir()
	touch := func(name string, size int, age time.Duration) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := time.Now().Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		return p
	}
	touch("LethalPride-0000000000000001.bin", 10, time.Hour)
	other := touch("BrutalRestraint-0000000000000002.bin", 10, time.Hour)
	stale := touch("LethalPride-0000000000000003.bin.tmp123", 10, 2*time.Hour)
	fresh := touch("ElegantHubris-0000000000000004.bin.tmp456", 10, time.Minute)
	cur := touch("LethalPride-0000000000000005.bin", 10, 0)
	pruneTimelessCache(cur, time.Now())
	for p, want := range map[string]bool{
		filepath.Join(dir, "LethalPride-0000000000000001.bin"): false, other: true, stale: false, fresh: true, cur: true,
	} {
		if _, err := os.Stat(p); (err == nil) != want {
			t.Errorf("%s exists %v, want %v", filepath.Base(p), err == nil, want)
		}
	}

	// Size cap: the oldest table goes, the one just written stays.
	big := timelessCacheMaxBytes/2 + 1
	old := touch("GloriousVanity-0000000000000006.bin", big, 3*time.Hour)
	cur2 := touch("MilitantFaith-0000000000000007.bin", big, 0)
	pruneTimelessCache(cur2, time.Now())
	if _, err := os.Stat(old); err == nil {
		t.Error("oldest table not evicted past the size cap")
	}
	if _, err := os.Stat(cur2); err != nil {
		t.Error("the table just written was evicted")
	}
}

// TestTimelessCachePruneForeignFiles: in a shared directory the pruning
// leaves alone every file it did not write, however old or large, even
// when it looks like a table or a temporary file.
func TestTimelessCachePruneForeignFiles(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-3 * time.Hour)
	write := func(name string, size int64) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Truncate(p, size); err != nil { // sparse where the file system allows
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		return p
	}
	foreign := []string{
		write("other.tmp", 10),
		write("download.bin.tmp", 10),
		write("LethalPride-0000000000000001.bin.tmpx", 10),
		write("data.bin", timelessCacheMaxBytes+1),
		write("LethalPride-backup.bin", 10),
		write("SomeTable-0000000000000001.bin", 10),
		write("LethalPride-00000000000000zz.bin", 10),
	}
	stale := write("LethalPride-0000000000000002.bin.tmp77", 10)
	cur := filepath.Join(dir, "LethalPride-0000000000000003.bin")
	if err := os.WriteFile(cur, make([]byte, 10), 0o600); err != nil {
		t.Fatal(err)
	}
	pruneTimelessCache(cur, time.Now())
	for _, p := range foreign {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("foreign %s removed", filepath.Base(p))
		}
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("our stale temporary file kept")
	}
}

// TestTimelessCacheEnv: "off" turns the cache off, a path gets the
// poe-lib/timeless-decoded subdirectory unless it ends in it, unset is
// the user cache directory.
func TestTimelessCacheEnv(t *testing.T) {
	t.Setenv(TimelessCacheEnv, "off")
	if d := timelessCacheDir(); d != "" {
		t.Errorf("off: cache dir %q", d)
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "poe-lib", "timeless-decoded")
	t.Setenv(TimelessCacheEnv, dir)
	if d := timelessCacheDir(); d != sub {
		t.Errorf("dir: cache dir %q, want %q", d, sub)
	}
	t.Setenv(TimelessCacheEnv, sub+string(filepath.Separator))
	if d := timelessCacheDir(); d != sub {
		t.Errorf("dir ending in the subdirectory: cache dir %q, want %q", d, sub)
	}
	t.Setenv(TimelessCacheEnv, "")
	if d := timelessCacheDir(); !strings.HasSuffix(d, filepath.Join("poe-lib", "timeless-decoded")) {
		t.Errorf("default: cache dir %q", d)
	}
}
