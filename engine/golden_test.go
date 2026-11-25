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
	"Str", "Dex", "Int", "Life", "Mana", "FireResist", "ColdResist", "LightningResist", "ChaosResist", "Armour",
	"Evasion", "EnergyShield", "Ward", "BlockChance", "SpellBlockChance", "SpellSuppressionChance",
	"AttackDodgeChance", "SpellDodgeChance", "LifeRegenRecovery", "EnergyShieldRegenRecovery",
	"EnergyShieldRecharge", "ActionSpeedMod", "AvoidAllDamageFromHitsChance", "IgniteAvoidChance",
	"ChillAvoidChance", "FreezeAvoidChance", "ShockAvoidChance", "BleedAvoidChance", "PoisonAvoidChance",
	"CritExtraDamageReduction", "EffectiveBlockChance", "EffectiveSpellBlockChance", "FireTakenDotMult",
	"ColdTakenHit", "LifeHitPool", "PhysicalTotalPool", "ChaosTotalPool", "EvadeChance", "MeleeEvadeChance",
	"ProjectileEvadeChance", "AverageNotHitChance", "EnemyCritChance", "PhysicalEnemyDamage", "FireEnemyDamage",
	"ChaosEnemyDamage", "totalTakenDamage", "FireTakenHit", "ChaosTakenHit", "BasePhysicalDamageReduction",
	"BasePhysicalDamageReductionWhenHit", "PhysicalTakenDotMult", "PhysicalTakenHit", "totalTakenHit",
	"FireTotalHitPool", "PhysicalMaximumHitTaken", "FireMaximumHitTaken", "ColdMaximumHitTaken",
	"LightningMaximumHitTaken", "ChaosMaximumHitTaken", "NumberOfDamagingHits", "NumberOfMitigatedDamagingHits",
	"TotalEHP", "EHPSurvivalTime", "ManaRegenRecovery", "MovementSpeedMod", "CurseEffectOnSelf",
}

const tolerance = 0.005

// reportStats are compared and logged but not part of the ratchet yet.
var reportStats = []string{}

// Ratchet: builds where every stat above is within tolerance. Raise it as
// the engine improves; never lower it.
const minBuildsWithinTolerance = 89

// minBuildsWithoutTimeless applies when the timeless jewel tables are absent.
const minBuildsWithoutTimeless = 2

// Minion ratchet: oracle minions (each minion skill as the main skill,
// output.Minion) whose minionResultStats are all within tolerance.
const minMinionsWithinTolerance = 119

// Minion offence ratchet: oracle minions whose TotalDPS is within tolerance.
const minMinionDPSWithinTolerance = 119

// Player offence ratchet: builds whose main skill playerRatchetStats are
// each within tolerance.
const minPlayerDPSWithinTolerance = 89

// playerOffenceStats are the main skill's hit, ignite and skill DoT stats.
var playerOffenceStats = []string{"Speed", "HitChance", "CritChance", "CritMultiplier", "AverageDamage", "TotalDPS",
	"IgniteChance", "IgniteDPS", "TotalDot", "WithIgniteDPS", "TotalDotDPS", "CombinedDPS", "MirageDPS",
	"BleedChance", "BleedDPS", "WithBleedDPS", "PoisonChance", "PoisonDPS", "TotalPoisonDPS", "WithPoisonDPS",
	"ImpaleChance", "ImpaleModifier", "ImpaleDPS", "WithImpaleDPS", "ImpaleDuration", "WeaponRange", "StrikeTargets", "AreaOfEffectRadius", "AreaOfEffectMod", "PierceCount", "ForkCountMax"}

// playerRatchetStats are the playerOffenceStats each held at
// minPlayerDPSWithinTolerance.
var playerRatchetStats = []string{"TotalDPS", "IgniteChance", "IgniteDPS", "TotalDot", "WithIgniteDPS", "TotalDotDPS", "CombinedDPS", "MirageDPS",
	"BleedChance", "BleedDPS", "WithBleedDPS", "PoisonChance", "PoisonDPS", "TotalPoisonDPS", "WithPoisonDPS",
	"ImpaleChance", "ImpaleModifier", "ImpaleDPS", "WithImpaleDPS", "ImpaleDuration", "WeaponRange", "StrikeTargets", "AreaOfEffectRadius", "AreaOfEffectMod", "PierceCount", "ForkCountMax"}

// goldenMinion is one entry of the oracle's "Minions" list.
type goldenMinion struct {
	Name   string         `json:"name"`
	Minion string         `json:"minion"`
	Output map[string]any `json:"output"`
}

// compareMinions matches the oracle's minions to ours by skill and minion
// name and returns how many match on every stat, with the differences.
func compareMinions(t *testing.T, raw json.RawMessage, got []MinionResult, perStat map[string]int) (ok, total int, diffs []string) {
	var want []goldenMinion
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
	}
	used := map[int]bool{}
	for _, w := range want {
		total++
		idx := -1
		for i, g := range got {
			if !used[i] && g.Skill == w.Name && g.Minion == w.Minion {
				idx = i
				break
			}
		}
		if idx < 0 {
			diffs = append(diffs, fmt.Sprintf("%s/%s missing", w.Name, w.Minion))
			continue
		}
		used[idx] = true
		wantStats := map[string]float64{}
		for k, v := range w.Output {
			if f, ok := v.(float64); ok {
				wantStats[k] = f
			}
		}
		all := true
		for _, s := range minionResultStats {
			if matches(got[idx].Stats[s], wantStats, s) {
				perStat[s]++
			} else {
				all = false
				diffs = append(diffs, fmt.Sprintf("%s %s %g/%g", w.Minion, s, got[idx].Stats[s], wantStats[s]))
			}
		}
		if all {
			ok++
		}
		for _, s := range minionOffenceStats {
			if matches(got[idx].Stats[s], wantStats, s) {
				perStat["Offence:"+s]++
			} else {
				diffs = append(diffs, fmt.Sprintf("%s %s %.4g/%.4g", w.Minion, s, got[idx].Stats[s], wantStats[s]))
			}
		}
	}
	return ok, total, diffs
}

// matches is within, plus: the oracle drops infinite numbers (math.huge)
// from its JSON, so a missing stat matches our +Inf.
func matches(got float64, want map[string]float64, stat string) bool {
	if _, has := want[stat]; !has && math.IsInf(got, 1) {
		return true
	}
	return within(got, want[stat])
}

func within(got, want float64) bool {
	if want == 0 {
		return math.Abs(got) < 0.5
	}
	return math.Abs(got-want)/math.Abs(want) <= tolerance
}

func TestGoldenCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus-heavy; run without -short")
	}
	codes, err := filepath.Glob("../testdata/golden/builds/*.code")
	if err != nil || len(codes) == 0 {
		t.Fatalf("no golden builds: %v", err)
	}
	sort.Strings(codes)
	calc, err := New()
	if err != nil {
		t.Fatal(err)
	}

	quirks := loadQuirks(t)
	perStat := map[string]int{}
	allOK := 0
	minionPerStat := map[string]int{}
	minionsOK, minionsTotal := 0, 0
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
		var rawMinions struct {
			Minions json.RawMessage `json:"Minions"`
		}
		if err := json.Unmarshal(exp, &rawMinions); err != nil {
			t.Fatal(err)
		}
		want = map[string]float64{}
		for k, v := range anyWant {
			if f, ok := v.(float64); ok {
				want[k] = f
			}
		}
		for stat, q := range quirks[name] {
			want[stat] = q.Value
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
			if matches(got, want, s) {
				perStat[s]++
			} else {
				ok = false
				diffs = append(diffs, fmt.Sprintf("%s %g/%g", s, got, want[s]))
			}
		}
		if ok {
			allOK++
		}
		var extra []string
		for _, s := range reportStats {
			if matches(res.Stats[s], want, s) {
				perStat[s]++
			} else {
				extra = append(extra, fmt.Sprintf("%s %g/%g", s, res.Stats[s], want[s]))
			}
		}
		var offDiffs []string
		for _, s := range playerOffenceStats {
			if matches(res.Stats[s], want, s) {
				perStat["Offence:"+s]++
			} else {
				offDiffs = append(offDiffs, fmt.Sprintf("%s %g/%g", s, res.Stats[s], want[s]))
			}
		}
		if len(offDiffs) > 0 {
			t.Logf("%-14s offence: %s", name, strings.Join(offDiffs, ", "))
		}
		t.Logf("%-14s %-12s ok=%v %s | report: %s", name, b.AscendClassName, ok, strings.Join(diffs, ", "), strings.Join(extra, ", "))
		minions, err := calc.CalcMinions(b)
		if err != nil {
			t.Fatal(err)
		}
		mOK, mTotal, mDiffs := compareMinions(t, rawMinions.Minions, minions, minionPerStat)
		minionsOK += mOK
		minionsTotal += mTotal
		if len(mDiffs) > 0 {
			t.Logf("%-14s minions: %s", name, strings.Join(mDiffs, ", "))
		}
	}
	for _, s := range append(append([]string(nil), goldenStats...), reportStats...) {
		t.Logf("%-16s within %.1f%%: %d/%d", s, tolerance*100, perStat[s], len(codes))
	}
	for _, s := range playerOffenceStats {
		t.Logf("Offence %-16s within %.1f%%: %d/%d", s, tolerance*100, perStat["Offence:"+s], len(codes))
	}
	var tags []string
	for tag, n := range unsupportedTags {
		tags = append(tags, fmt.Sprintf("%s x%d", tag, n))
	}
	sort.Strings(tags)
	t.Logf("unsupported tag types: %s", strings.Join(tags, ", "))
	for _, s := range minionResultStats {
		t.Logf("Minion %-16s within %.1f%%: %d/%d", s, tolerance*100, minionPerStat[s], minionsTotal)
	}
	for _, s := range minionOffenceStats {
		t.Logf("Minion offence %-14s within %.1f%%: %d/%d", s, tolerance*100, minionPerStat["Offence:"+s], minionsTotal)
	}
	t.Logf("minions with all stats within tolerance: %d/%d", minionsOK, minionsTotal)
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
	if ratchet == minBuildsWithinTolerance && minionPerStat["Offence:TotalDPS"] < minMinionDPSWithinTolerance {
		t.Errorf("minion TotalDPS within tolerance %d < ratchet %d", minionPerStat["Offence:TotalDPS"], minMinionDPSWithinTolerance)
	}
	for _, st := range playerRatchetStats {
		if ratchet == minBuildsWithinTolerance && perStat["Offence:"+st] < minPlayerDPSWithinTolerance {
			t.Errorf("player %s within tolerance %d < ratchet %d", st, perStat["Offence:"+st], minPlayerDPSWithinTolerance)
		}
	}
	if ratchet == minBuildsWithinTolerance && minionsOK < minMinionsWithinTolerance {
		t.Errorf("minions within tolerance %d < ratchet %d", minionsOK, minMinionsWithinTolerance)
	}
}

// goldenQuirk is an entry of testdata/golden/quirks.json: an expected value
// that deliberately differs from the oracle because the engine fixes a PoB
// bug there. Regenerating the expected JSON from the oracle keeps these.
type goldenQuirk struct {
	Value  float64 `json:"value"`
	Reason string  `json:"reason"`
}

// loadQuirks reads quirks.json: build code -> stat -> expected value.
func loadQuirks(t *testing.T) map[string]map[string]goldenQuirk {
	raw, err := os.ReadFile("../testdata/golden/quirks.json")
	if err != nil {
		t.Fatal(err)
	}
	var q map[string]map[string]goldenQuirk
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatal(err)
	}
	return q
}
