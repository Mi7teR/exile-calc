package modparser

import (
	"sort"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// testLines mixes plain stats, tagged mods, table (LIST) values and a line
// that does not parse.
func testLines() []string {
	return []string{
		"+10 to maximum Life",
		"10% increased Attack Speed while Dual Wielding",
		"Adds 5 to 10 Fire Damage to Attacks",
		"20% increased Damage with Bleeding while Fortified",
		"Minions deal 15% increased Damage",
		"Auras from your Skills grant 2% increased Damage to you and Allies",
		"Nearby Enemies have -10% to Fire Resistance",
		"this line is not a mod at all",
	}
}

// freshParser returns a parser with an empty cache over the PoE1 tables.
func freshParser(t testing.TB) *Parser {
	t.Helper()
	p, err := PoE1()
	if err != nil {
		t.Fatal(err)
	}
	return newParser(p.t)
}

// mutate changes everything a caller could change in r.
func mutate(r *Result) {
	m := &r.Mods[0]
	if tbl, ok := m.Value.(map[string]any); ok {
		tbl["mutated"] = true
	}
	m.Value = "mutated"
	if len(m.Tags) > 0 {
		m.Tags[0]["mutated"] = true
		m.Tags[0] = map[string]any{"type": "mutated"}
	}
	m.Tags = append(m.Tags, map[string]any{"type": "appended"})
	r.Mods = append(r.Mods, Mod{Name: "appended"})
}

// TestParseCachedResultIsIndependent: a caller may modify what Parse
// returned, from a cold miss or from a cache hit, without affecting later
// calls for the same line.
func TestParseCachedResultIsIndependent(t *testing.T) {
	ref := freshParser(t)
	p := freshParser(t)
	for _, line := range testLines()[:7] {
		want := ref.Parse(line)
		if len(want.Mods) == 0 {
			t.Fatalf("%q: no mods", line)
		}
		cold := p.Parse(line) // miss: parsed and stored
		mutate(&cold)
		hit := p.Parse(line)
		if d := cmp.Diff(want, hit); d != "" {
			t.Errorf("%q after mutating the miss (-want +got):\n%s", line, d)
		}
		mutate(&hit)
		for i := range 2 {
			if d := cmp.Diff(want, p.Parse(line)); d != "" {
				t.Errorf("%q call %d after mutating a hit (-want +got):\n%s", line, i+3, d)
			}
		}
	}
}

// TestParseConcurrent: concurrent Parse calls on a cold cache, so the first
// calls race on misses, agree with a separate sequential parser.
func TestParseConcurrent(t *testing.T) {
	lines := testLines()
	ref := freshParser(t)
	want := make([]Result, len(lines))
	for i, l := range lines {
		want[i] = ref.Parse(l)
	}
	p := freshParser(t)
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for g := range 8 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range 100 {
				k := (g + i) % len(lines)
				got := p.Parse(lines[k])
				if len(got.Mods) > 0 {
					got.Mods[0].Value = i // callers own their copy
				}
				got = p.Parse(lines[k])
				if d := cmp.Diff(want[k], got); d != "" {
					errs <- lines[k] + ":\n" + d
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if n := p.cached.Load(); n != int64(len(lines)) {
		t.Errorf("cached %d entries, want %d", n, len(lines))
	}
}

// TestParseCacheCap: past the cap, lines are still parsed correctly but not
// stored, and the count never exceeds the cap, also under concurrency.
func TestParseCacheCap(t *testing.T) {
	lines := testLines()
	ref := freshParser(t)
	p := freshParser(t)
	p.cacheCap = 3
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range 3 * len(lines) {
				l := lines[(g+i)%len(lines)]
				if d := cmp.Diff(ref.Parse(l), p.Parse(l)); d != "" {
					t.Errorf("%q (-want +got):\n%s", l, d)
				}
			}
		}(g)
	}
	wg.Wait()
	stored := 0
	p.cache.Range(func(_, _ any) bool { stored++; return true })
	if n := p.cached.Load(); n != 3 || stored != 3 {
		t.Errorf("count %d, stored %d, want 3", n, stored)
	}
}

// BenchmarkParseCorpusCold parses a fixed set of PoB ModCache lines with an
// empty cache each iteration, so parse-speed regressions stay visible
// behind the cache that makes the engine benchmarks warm.
func BenchmarkParseCorpusCold(b *testing.B) {
	cache := loadModCache(b)
	lines := make([]string, 0, len(cache))
	for l := range cache {
		lines = append(lines, l)
	}
	sort.Strings(lines)
	lines = lines[:1000]
	base := freshParser(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		p := newParser(base.t)
		for _, l := range lines {
			p.Parse(l)
		}
	}
}
