package engine

import (
	"bytes"
	"compress/zlib"
	"os"
	"path/filepath"
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
