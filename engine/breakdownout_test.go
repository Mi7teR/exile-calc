package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBreakdownOnlyOutputs: outputs PoB sets only inside "if breakdown"
// blocks, which MAIN mode always has: the flask breakdown
// (CalcPerform.lua:1617-1637) and the tincture breakdown (1876-1880).
// Every corpus build matches the oracle.
func TestBreakdownOnlyOutputs(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus")
	}
	c := newCalc(t)
	keys := []string{"FlaskEffect", "FlaskChargeGen", "LifeFlaskChargeGen", "ManaFlaskChargeGen",
		"UtilityFlaskChargeGen", "FlaskChargeOnCritChance", "TinctureEffect", "TinctureLimit"}
	files, err := filepath.Glob("../testdata/golden/expected/*.json")
	if err != nil || len(files) == 0 {
		t.Fatal("no expected results", err)
	}
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".json")
		raw, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		var exp map[string]any
		if jerr := json.Unmarshal(raw, &exp); jerr != nil {
			t.Fatal(jerr)
		}
		res, cerr := c.Calc(corpusBuild(t, id))
		if cerr != nil {
			t.Fatal(cerr)
		}
		for _, k := range keys {
			want, ok := exp[k].(float64)
			got, has := res.Stats[k]
			if !ok || !has || !near(got, want, 1e-9) {
				t.Errorf("%s %s = %v (set %v), oracle %v", id, k, got, has, exp[k])
			}
		}
	}
}
