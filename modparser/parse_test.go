package modparser

import (
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

// TestParseCachedResultIsIndependent: a caller may modify what Parse
// returned without affecting later calls for the same line.
func TestParseCachedResultIsIndependent(t *testing.T) {
	p, err := PoE1()
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range testLines()[:7] {
		want := p.Parse(line)
		first := p.Parse(line)
		if len(first.Mods) == 0 {
			t.Fatalf("%q: no mods", line)
		}
		m := &first.Mods[0]
		if tbl, ok := m.Value.(map[string]any); ok {
			tbl["mutated"] = true
		}
		m.Value = "mutated"
		if len(m.Tags) > 0 {
			m.Tags[0]["mutated"] = true
			m.Tags[0] = map[string]any{"type": "mutated"}
		}
		m.Tags = append(m.Tags, map[string]any{"type": "appended"})
		first.Mods = append(first.Mods, Mod{Name: "appended"})
		for i := range 2 {
			if d := cmp.Diff(want, p.Parse(line)); d != "" {
				t.Errorf("%q call %d after mutation (-want +got):\n%s", line, i+2, d)
			}
		}
	}
}

// TestParseConcurrent: concurrent Parse calls agree with sequential ones.
func TestParseConcurrent(t *testing.T) {
	p, err := PoE1()
	if err != nil {
		t.Fatal(err)
	}
	cacheLines := testLines()
	want := make([]Result, len(cacheLines))
	for i, l := range cacheLines {
		want[i] = p.Parse(l)
	}
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for g := range 8 {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := range 1000 {
				k := (g + i) % len(cacheLines)
				got := p.Parse(cacheLines[k])
				if len(got.Mods) > 0 {
					got.Mods[0].Value = i // callers own their copy
				}
				got = p.Parse(cacheLines[k])
				if d := cmp.Diff(want[k], got); d != "" {
					errs <- cacheLines[k] + ":\n" + d
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
}
