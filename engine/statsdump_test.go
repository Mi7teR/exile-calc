package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// The stats dump guards refactoring: every output of Calc, CalcMinions and
// Combat on the golden corpus, with floats formatted exactly. Take a base
// before a change and compare after it; any difference is a bug.
//
//	STATSDUMP_OUT=/tmp/base go test ./engine -run TestStatsDump
//	STATSDUMP_BASE=/tmp/base go test ./engine -run TestStatsDump

type statsDump struct {
	Stats           map[string]string
	Unparsed        []string
	Incomplete      []string
	UnsupportedTags map[string]int
	Skills          []SkillInfo
	Minion          map[string]string
	Minions         []minionDump
	Combat          *combatDump
}

type minionDump struct {
	Skill, Minion string
	Stats         map[string]string
}

type combatDump struct {
	Boss, Pack, PackPerMob combatDPSDump
	Skill                  string
	Assumptions            []string
}

type combatDPSDump struct {
	PoB, Sustained, Burst, HitsPerCast string
	Components                         map[string]string
}

func fmtFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func fmtStats(m map[string]float64) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmtFloat(v)
	}
	return out
}

func dumpCombatDPS(d CombatDPS) combatDPSDump {
	return combatDPSDump{
		fmtFloat(d.PoB), fmtFloat(d.Sustained), fmtFloat(d.Burst), fmtFloat(d.HitsPerCast), fmtStats(d.Components),
	}
}

func dumpBuild(t *testing.T, c *Calculator, id string) statsDump {
	t.Helper()
	b := corpusBuild(t, id)
	res, err := c.Calc(b)
	if err != nil {
		t.Fatalf("%s: Calc: %v", id, err)
	}
	d := statsDump{
		Stats: fmtStats(res.Stats), Unparsed: res.Unparsed, Incomplete: res.Incomplete,
		UnsupportedTags: res.UnsupportedTags, Skills: res.Skills,
	}
	if res.Minion != nil {
		d.Minion = fmtStats(res.Minion.Stats)
	}
	ms, err := c.CalcMinions(corpusBuild(t, id))
	if err != nil {
		t.Fatalf("%s: CalcMinions: %v", id, err)
	}
	for _, m := range ms {
		d.Minions = append(d.Minions, minionDump{m.Skill, m.Minion, fmtStats(m.Stats)})
	}
	if res.main != nil {
		cr, cerr := c.Combat(corpusBuild(t, id), CombatOptions{})
		if cerr != nil {
			t.Fatalf("%s: Combat: %v", id, cerr)
		}
		d.Combat = &combatDump{
			dumpCombatDPS(cr.Boss), dumpCombatDPS(cr.Pack), dumpCombatDPS(cr.PackPerMob), cr.Skill, cr.Assumptions,
		}
	}
	return d
}

// corpusIDs lists the golden corpus build ids in a stable order.
func corpusIDs(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob("../testdata/golden/builds/*.code")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no golden builds found: %v", err)
	}
	sort.Strings(paths)
	ids := make([]string, len(paths))
	for i, p := range paths {
		ids[i] = strings.TrimSuffix(filepath.Base(p), ".code")
	}
	return ids
}

// TestStatsDumpDeterministic dumps every build twice in one process; any
// difference means an output depends on map iteration order or similar.
func TestStatsDumpDeterministic(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus-heavy; run without -short")
	}
	c := newCalc(t)
	for _, id := range corpusIDs(t) {
		a, b := dumpBuild(t, c, id), dumpBuild(t, c, id)
		if diff := cmp.Diff(a, b); diff != "" {
			t.Errorf("%s: two runs differ (-first +second):\n%s", id, diff)
		}
	}
}

func TestStatsDump(t *testing.T) {
	out, base := os.Getenv("STATSDUMP_OUT"), os.Getenv("STATSDUMP_BASE")
	if out == "" && base == "" {
		t.Skip("set STATSDUMP_OUT or STATSDUMP_BASE")
	}
	ids := corpusIDs(t)
	c := newCalc(t)
	if out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	diffs := 0
	for _, id := range ids {
		d := dumpBuild(t, c, id)
		raw, err := json.MarshalIndent(d, "", " ")
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if out != "" {
			if werr := os.WriteFile(filepath.Join(out, id+".json"), raw, 0o644); werr != nil {
				t.Fatal(werr)
			}
		}
		if base != "" {
			wantRaw, rerr := os.ReadFile(filepath.Join(base, id+".json"))
			if rerr != nil {
				t.Fatal(rerr)
			}
			var want, got statsDump
			if uerr := json.Unmarshal(wantRaw, &want); uerr != nil {
				t.Fatal(uerr)
			}
			if uerr := json.Unmarshal(raw, &got); uerr != nil {
				t.Fatal(uerr)
			}
			if diff := cmp.Diff(want, got); diff != "" {
				diffs++
				t.Errorf("%s (-base +now):\n%s", id, diff)
			}
		}
	}
	t.Logf("%d builds, %d differ", len(ids), diffs)
}
