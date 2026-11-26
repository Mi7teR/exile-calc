package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The number of distinct expected minion output keys the engine produces
// and compares in TestGoldenMinionOutputs; raise them as more ports land,
// never lower.
const (
	goldenMinionDefenceKeys = 529
	goldenMinionOffenceKeys = 240
)

// TestGoldenMinionOutputs compares every numeric output PoB has for a
// corpus minion with the engine's, whenever the engine produces that key:
// env.minion.output after its defence (calcs.defence and
// buildDefenceEstimations) and the minion's offence output. Minions are
// paired with the oracle's as compareMinions does: by summoning skill and
// minion name, in order.
func TestGoldenMinionOutputs(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus-heavy; run without -short")
	}
	c := newCalc(t)
	files, err := filepath.Glob("../testdata/golden/builds/*.code")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden builds: %v", err)
	}
	sort.Strings(files)
	type got struct {
		mr       MinionResult
		def, off map[string]float64
	}
	keys := map[string]map[string]bool{"defence": {}, "offence": {}}
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".code")
		raw, rerr := os.ReadFile("../testdata/golden/expected/" + id + ".json")
		if rerr != nil {
			t.Fatal(rerr)
		}
		var exp struct{ Minions []goldenMinion }
		if jerr := json.Unmarshal(raw, &exp); jerr != nil {
			t.Fatal(jerr)
		}
		if len(exp.Minions) == 0 {
			continue
		}
		res, cerr := c.Calc(corpusBuild(t, id))
		if cerr != nil {
			t.Fatal(cerr)
		}
		var gots []got
		for _, s := range res.Skills {
			if !s.HasMinion || s.Disabled {
				continue
			}
			var def map[string]float64
			// The first call is the main calculation; buildOutput's cost
			// warning passes (other skills of the same name) come after.
			debugMinionSkill = func(ms *activeSkill) {
				if def == nil {
					def = ms.mods.root().output
				}
			}
			r, merr := c.Calc(withMainSkill(res.build, res.buildGroup(s.Group), s.GroupSkill))
			debugMinionSkill = nil
			if merr != nil {
				t.Fatal(merr)
			}
			if r.Minion != nil {
				gots = append(gots, got{*r.Minion, def, r.Minion.out})
			}
		}
		used := map[int]bool{}
		for _, w := range exp.Minions {
			idx := -1
			for i, g := range gots {
				if !used[i] && g.mr.Skill == w.Name && g.mr.Minion == w.Minion {
					idx = i
					break
				}
			}
			if idx < 0 {
				t.Errorf("%s: %s/%s missing", id, w.Name, w.Minion)
				continue
			}
			used[idx] = true
			for kind, out := range map[string]map[string]float64{"defence": gots[idx].def, "offence": gots[idx].off} {
				for k, v := range w.Output {
					want, isNum := v.(float64)
					gv, has := out[k]
					if !isNum || !has {
						continue
					}
					keys[kind][k] = true
					if !within(gv, want) {
						t.Errorf("%s %s/%s %s %s: %v, PoB %v", id, w.Name, w.Minion, kind, k, gv, want)
					}
				}
			}
		}
	}
	t.Logf("compared keys: defence %d, offence %d", len(keys["defence"]), len(keys["offence"]))
	if len(keys["defence"]) < goldenMinionDefenceKeys || len(keys["offence"]) < goldenMinionOffenceKeys {
		t.Errorf("compared keys fell: defence %d (ratchet %d), offence %d (ratchet %d)",
			len(keys["defence"]), goldenMinionDefenceKeys, len(keys["offence"]), goldenMinionOffenceKeys)
	}
}
