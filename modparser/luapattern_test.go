package modparser

import (
	"reflect"
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
