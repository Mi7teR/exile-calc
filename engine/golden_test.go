package engine

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// The golden corpus (testdata/golden) holds real 3.30 builds and the stats
// headless PoB v2.68.1 computed for them. These are the stats the engine
// covers so far, with the tolerance from the spec: 0.5% for defence.
var goldenStats = []string{
	"Str", "Dex", "Int", "Life", "Mana",
	"FireResist", "ColdResist", "LightningResist", "ChaosResist",
}

const tolerance = 0.005

// Ratchet: builds where every stat above is within tolerance. Raise it as
// the engine improves; never lower it.
const minBuildsWithinTolerance = 8

// minBuildsWithoutTimeless applies when the timeless jewel tables are absent.
const minBuildsWithoutTimeless = 2

func within(got, want float64) bool {
	if want == 0 {
		return math.Abs(got) < 0.5
	}
	return math.Abs(got-want)/math.Abs(want) <= tolerance
}

func TestGoldenCorpus(t *testing.T) {
	codes, err := filepath.Glob("../testdata/golden/builds/*.code")
	if err != nil || len(codes) == 0 {
		t.Fatalf("no golden builds: %v", err)
	}
	sort.Strings(codes)
	calc, err := New()
	if err != nil {
		t.Fatal(err)
	}

	perStat := map[string]int{}
	allOK := 0
	unsupportedTags := map[string]int{}
	for _, c := range codes {
		name := strings.TrimSuffix(filepath.Base(c), ".code")
		raw, err := os.ReadFile(c)
		if err != nil {
			t.Fatal(err)
		}
		var want map[string]float64
		exp, err := os.ReadFile(filepath.Join("../testdata/golden/expected", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var anyWant map[string]any
		if err := json.Unmarshal(exp, &anyWant); err != nil {
			t.Fatal(err)
		}
		want = map[string]float64{}
		for k, v := range anyWant {
			if f, ok := v.(float64); ok {
				want[k] = f
			}
		}
		b, err := pobcode.FromCode(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		res, err := calc.Calc(b)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		for tag, n := range res.UnsupportedTags {
			unsupportedTags[tag] += n
		}
		ok := true
		var diffs []string
		for _, s := range goldenStats {
			got := res.Stats[s]
			if within(got, want[s]) {
				perStat[s]++
			} else {
				ok = false
				diffs = append(diffs, fmt.Sprintf("%s %g/%g", s, got, want[s]))
			}
		}
		if ok {
			allOK++
		}
		t.Logf("%-14s %-12s ok=%v %s", name, b.AscendClassName, ok, strings.Join(diffs, ", "))
	}
	for _, s := range goldenStats {
		t.Logf("%-16s within %.1f%%: %d/%d", s, tolerance*100, perStat[s], len(codes))
	}
	var tags []string
	for tag, n := range unsupportedTags {
		tags = append(tags, fmt.Sprintf("%s x%d", tag, n))
	}
	sort.Strings(tags)
	t.Logf("unsupported tag types: %s", strings.Join(tags, ", "))
	t.Logf("builds with all stats within tolerance: %d/%d", allOK, len(codes))
	ratchet := minBuildsWithinTolerance
	if defaultTimelessDir() == "" {
		// Timeless jewel tables are not in the repository (see TimelessDirEnv);
		// without them only builds with no timeless jewel can match.
		t.Logf("timeless jewel tables not found: run go run ./cmd/gamedatagen or set %s", TimelessDirEnv)
		ratchet = minBuildsWithoutTimeless
	}
	if allOK < ratchet {
		t.Errorf("builds within tolerance %d < ratchet %d", allOK, ratchet)
	}
}
