package modparser

import (
	"fmt"
	"reflect"
	"regexp/syntax"
	"sort"
	"sync"
	"testing"
)

func TestTranslateLuaPattern(t *testing.T) {
	cases := []struct {
		pat, in string
		want    []string // full match then captures; nil = no match
	}{
		{`^(%d+)%% increased`, "15% increased damage", []string{"15% increased", "15"}},
		{`^([%+%-][%d%.]+)%%? to`, "+1.5% to fire", []string{"+1.5% to", "+1.5"}},
		{`^([%+%-][%d%.]+)%%? to`, "12% to fire", nil},
		{`^axe attacks [hd][ae][va][el] `, "axe attacks deal ", []string{"axe attacks deal "}},
		{`^removes? ([%d%.]+) ?o?f? ?y?o?u?r?`, "remove 5 of your", []string{"remove 5 of your", "5"}},
		{`adds (%d+) to (%d+) (%a+) damage`, "adds 1 to 2 fire damage", []string{"adds 1 to 2 fire damage", "1", "2", "fire"}},
		{`(.-) and (.+)`, "a and b and c", []string{"a and b and c", "a", "b and c"}},
		{`^%-(%d+)`, "-5 to", []string{"-5", "5"}},
		{`a.b`, "axb", []string{"axb"}},
		{`x{y}|z`, "x{y}|z", []string{"x{y}|z"}},
		{`[^%d]+`, "ab1", []string{"ab"}},
		{`^%a+$`, "abc", []string{"abc"}},
		// A quantifier with nothing to quantify is a plain character.
		{`^+(%d+) to`, "+25 to", []string{"+25 to", "25"}},
		{`^+(%d+) to`, "25 to", nil},
		{`(a)+b`, "a+b", []string{"a+b", "a"}},
		// An escaped character is one item, even "(" or ")".
		{`^%(+x`, "((x", []string{"((x"}},
		{`^a%)*b`, "a))b", []string{"a))b"}},
		// A quantifier cannot be quantified: "%d++" is %d+ then a plain "+".
		{`^(%d++)`, "12+", []string{"12+", "12+"}},
		{`^(%d++)`, "12", nil},
	}
	for _, c := range cases {
		re, err := translateLuaPattern(c.pat)
		if err != nil {
			t.Errorf("%q: %v", c.pat, err)
			continue
		}
		got := re.FindStringSubmatch(c.in)
		if !reflect.DeepEqual(got, c.want) && !(got == nil && c.want == nil) {
			t.Errorf("%q on %q: got %q, want %q (regexp %s)", c.pat, c.in, got, c.want, re)
		}
	}
	if _, err := translateLuaPattern(`%b()`); err == nil {
		t.Error("percent-b must be rejected")
	}
}

func TestRequiredLiteral(t *testing.T) {
	cases := []struct{ pat, want string }{
		{`^(%d+)%% increased`, "% increased"},
		{`^([%+%-][%d%.]+)%%? to`, " to"},
		{`adds (%d+) to (%d+) (%a+) damage`, " damage"},
		{`(.-) and (.+)`, " and "},
		{`^removes? ([%d%.]+) ?o?f? ?y?o?u?r?`, "remove"},
		{`^axe attacks [hd][ae][va][el] `, "axe attacks "},
		{`ab+c`, "a"}, // "b+" is not one "b": no "abc" run
		{`(fire) damage`, " damage"},
		{`^%a+$`, ""},
		{`[^%d]+`, ""},
		{`a.b`, "a"},
	}
	for _, c := range cases {
		expr, err := luaPatternExpr(c.pat)
		if err != nil {
			t.Fatal(err)
		}
		re, err := syntax.Parse(expr, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		if got := requiredLiteral(re); got != c.want {
			t.Errorf("%q: got %q, want %q", c.pat, got, c.want)
		}
	}
}

// TestIndexedScanSame checks the scan shortcuts (gram index, required
// literal) against the full in-order scan of every entry, on every
// ModCache line (as given and with parseMod's trailing space): the same
// value, rest and captures for every table.
func TestIndexedScanSame(t *testing.T) {
	if testing.Short() {
		t.Skip("runs every pattern over the whole ModCache")
	}
	p := freshParser(t)
	cache := loadModCache(t)
	lines := make([]string, 0, 2*len(cache))
	for l := range cache {
		lines = append(lines, l, l+" ")
	}
	sort.Strings(lines)
	names := make([]string, 0, len(p.t.t))
	all := map[string][]int32{}
	for name, tbl := range p.t.t {
		names = append(names, name)
		for i := range tbl {
			all[name] = append(all[name], int32(i))
		}
	}
	sort.Strings(names)
	// The lines split over a few goroutines; scan is safe for concurrent use.
	const workers = 8
	errs := make([]string, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for li := w; li < len(lines) && errs[w] == ""; li += workers {
				line := lines[li]
				lower := asciiLower(line)
				for _, name := range names {
					v, rest, caps, ok := p.scan(line, name)
					wv, wrest, wcaps, wok := scanEntries(line, lower, p.t.t[name], all[name], false)
					if ok != wok || rest != wrest || !reflect.DeepEqual(caps, wcaps) || !reflect.DeepEqual(v, wv) {
						errs[w] = fmt.Sprintf("%s on %q: indexed (%v, %q, %q), full (%v, %q, %q)",
							name, line, ok, rest, caps, wok, wrest, wcaps)
						break
					}
				}
			}
		})
	}
	wg.Wait()
	for _, e := range errs {
		if e != "" {
			t.Error(e)
		}
	}
}
