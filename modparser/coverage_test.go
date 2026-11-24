package modparser

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"testing"
)

// testdata/modcache.json.gz is PoB's Data/ModCache.lua (line -> [mods,
// extra]), exported by cmd/gamedatagen. It is the reference: for every line
// PoB parsed completely, we must produce the same mods or report the line
// as unsupported. A different result is a bug.
func loadModCache(t *testing.T) map[string][]any {
	t.Helper()
	f, err := os.Open("testdata/modcache.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	var cache map[string][]any
	if err := json.NewDecoder(zr).Decode(&cache); err != nil {
		t.Fatal(err)
	}
	return cache
}

var numbers = regexp.MustCompile(`[+-]?\d+(\.\d+)?`)

// knownDifferences are lines where PoB's own result is an artefact we do
// not reproduce on purpose.
var knownDifferences = map[string]string{
	// PoB puts the tag at index 2 with nil at 1, so ipairs never sees it and
	// the mod applies unconditionally; we keep the condition.
	"Hits have 15% chance to ignore Enemy Physical Damage Reduction while you have Sacrificial Zeal": "tag at index 2",
	"Hits have 43% chance to ignore Enemy Physical Damage Reduction while you have Sacrificial Zeal": "tag at index 2",
	"Hits have 50% chance to ignore Enemy Physical Damage Reduction while you have Sacrificial Zeal": "tag at index 2",
}

// Ratchet: raise these as hooks and fixes land; never lower them.
const (
	minMatched = 18720 // of 19265 lines PoB v2.68.1 parses fully (97.2%)
	maxWrong   = 0
)

func TestModCacheCoverage(t *testing.T) {
	p, err := PoE1()
	if err != nil {
		t.Fatal(err)
	}
	cache := loadModCache(t)
	var lines []string
	for line, v := range cache {
		if _, known := knownDifferences[line]; known {
			continue
		}
		if len(v) >= 1 && v[0] != nil && (len(v) < 2 || v[1] == nil) {
			lines = append(lines, line)
		}
	}
	sort.Strings(lines)

	matched, unsupported := 0, 0
	var wrong []string
	unsupportedForms := map[string]int{}
	missingHooks := map[string]int{}
	for _, line := range lines {
		want := cache[line][0]
		got, extra, hook := p.parseTracked(line)
		switch {
		case got == nil || extra != "":
			unsupported++
			if hook != "" {
				missingHooks[hook]++
			}
			unsupportedForms[numbers.ReplaceAllString(line, "#")]++
		case equalMods(got, want):
			matched++
		default:
			wrong = append(wrong, line)
		}
	}
	total := len(lines)
	t.Logf("ModCache lines PoB parses fully: %d", total)
	t.Logf("matched %d (%.1f%%), unsupported %d (%.1f%%), wrong %d (%.1f%%); untranslated patterns %d",
		matched, pct(matched, total), unsupported, pct(unsupported, total), len(wrong), pct(len(wrong), total), len(p.t.skipped))
	for i, line := range wrong {
		if i == 15 {
			break
		}
		got, _ := p.parseRaw(line)
		gj, _ := json.Marshal(got)
		wj, _ := json.Marshal(cache[line][0])
		t.Logf("WRONG %q\n  got  %s\n  want %s", line, gj, wj)
	}
	type form struct {
		text string
		n    int
	}
	var forms []form
	for f, n := range unsupportedForms {
		forms = append(forms, form{f, n})
	}
	sort.Slice(forms, func(i, j int) bool {
		return forms[i].n > forms[j].n || (forms[i].n == forms[j].n && forms[i].text < forms[j].text)
	})
	for i, f := range forms {
		if i == 15 {
			break
		}
		t.Logf("unsupported x%d: %s", f.n, f.text)
	}
	var hooksByCount []form
	for h, n := range missingHooks {
		hooksByCount = append(hooksByCount, form{h, n})
	}
	sort.Slice(hooksByCount, func(i, j int) bool {
		return hooksByCount[i].n > hooksByCount[j].n || (hooksByCount[i].n == hooksByCount[j].n && hooksByCount[i].text < hooksByCount[j].text)
	})
	blocked := 0
	for _, h := range hooksByCount {
		blocked += h.n
	}
	t.Logf("lines blocked by %d unported hooks: %d", len(hooksByCount), blocked)
	for i, h := range hooksByCount {
		if i == 25 {
			break
		}
		t.Logf("hook x%d: %s", h.n, h.text)
	}
	if matched < minMatched {
		t.Errorf("matched %d < ratchet %d", matched, minMatched)
	}
	if len(wrong) > maxWrong {
		t.Errorf("wrong %d > ratchet %d", len(wrong), maxWrong)
	}
}

func pct(a, b int) float64 { return 100 * float64(a) / float64(b) }

func equalMods(got []any, want any) bool {
	// Round-trip through JSON so both sides use the same Go types.
	g, _ := json.Marshal(got)
	var gv any
	_ = json.Unmarshal(g, &gv)
	// ModCache.lua prints numbers with 14 significant digits.
	return reflect.DeepEqual(sig14(gv), sig14(want)) || (len(got) == 0 && fmt.Sprint(want) == "[]")
}

func sig14(v any) any {
	switch x := v.(type) {
	case float64:
		f, _ := strconv.ParseFloat(strconv.FormatFloat(x, 'g', 14, 64), 64)
		return f
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = sig14(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = sig14(e)
		}
		return out
	}
	return v
}

func TestParseBasics(t *testing.T) {
	p, err := PoE1()
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]Mod{
		"+10 to maximum Life":             {Name: "Life", Type: "BASE", Value: 10.0},
		"10% increased maximum Life":      {Name: "Life", Type: "INC", Value: 10.0},
		"12% reduced Mana Cost of Skills": {},
		"+40% to Fire Resistance":         {Name: "FireResist", Type: "BASE", Value: 40.0},
	}
	for line, want := range cases {
		r := p.Parse(line)
		if want.Name == "" {
			continue // only checks that parsing does not panic
		}
		if !r.Parsed() || len(r.Mods) != 1 || r.Mods[0].Name != want.Name || r.Mods[0].Type != want.Type || r.Mods[0].Value != want.Value {
			t.Errorf("%q: %+v", line, r)
		}
	}
	r := p.Parse("Adds 1 to 2 Physical Damage")
	if !r.Parsed() || len(r.Mods) != 2 || r.Mods[0].Name != "PhysicalMin" || r.Mods[1].Value != 2.0 {
		t.Errorf("DMG form: %+v", r)
	}
	if r := p.Parse("this is not a mod"); r.Parsed() {
		t.Errorf("garbage parsed: %+v", r)
	}
}
