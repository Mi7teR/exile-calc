package engine

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// TestCostWarnings: the WarningList tables of Calcs.lua:418-454 on every
// corpus build equal PoB's. The fixture holds mainOutput's *WarningList
// tables printed by a debug script on the oracle (PoB v2.68.1); a build
// without lists has none.
func TestCostWarnings(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus")
	}
	raw, err := os.ReadFile("../testdata/golden/costwarnings.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]map[string][]string
	if err = json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	c := newCalc(t)
	for _, id := range corpusIDs(t) {
		got, cerr := c.CostWarnings(corpusBuild(t, id))
		if cerr != nil {
			t.Fatal(cerr)
		}
		for k, names := range want[id] {
			if !slices.Equal(got[k], names) {
				t.Errorf("%s %s = %q, oracle %q", id, k, got[k], names)
			}
		}
		for k, names := range got {
			if _, ok := want[id][k]; !ok {
				t.Errorf("%s %s = %q, oracle has none", id, k, names)
			}
		}
	}
}
