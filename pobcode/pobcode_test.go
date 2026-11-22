package pobcode

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Fixtures in testdata/builds come from PathOfBuilding spec/TestBuilds/3.13 (v2.68.1).
// Each .code was produced from its .xml independently of this package
// (python zlib + base64 with "+/" -> "-_"), the same way ImportTab.lua does it.
func corpus(t *testing.T) []string {
	t.Helper()
	codes, err := filepath.Glob("testdata/builds/*.code")
	if err != nil || len(codes) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	return codes
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeMatchesFixtureXML(t *testing.T) {
	for _, codePath := range corpus(t) {
		t.Run(filepath.Base(codePath), func(t *testing.T) {
			got, err := Decode(string(readFile(t, codePath)))
			if err != nil {
				t.Fatal(err)
			}
			want := readFile(t, strings.TrimSuffix(codePath, ".code")+".xml")
			if !bytes.Equal(got, want) {
				t.Fatalf("decoded XML differs from fixture (%d vs %d bytes)", len(got), len(want))
			}
		})
	}
}

func TestDecodeIsLenient(t *testing.T) {
	code := string(readFile(t, "testdata/builds/OccVortex.code"))
	want, err := Decode(code)
	if err != nil {
		t.Fatal(err)
	}
	std := strings.NewReplacer("-", "+", "_", "/").Replace(code)
	variants := map[string]string{
		"standard alphabet": std,
		"no padding":        strings.TrimRight(code, "="),
		"surrounding space": "  \n" + code + "\r\n",
		"line breaks":       code[:100] + "\n" + code[100:],
	}
	for name, v := range variants {
		got, err := Decode(v)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: decoded XML differs", name)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	notZlib := base64.URLEncoding.EncodeToString([]byte("plain text, not zlib"))
	for name, code := range map[string]string{
		"empty":      "",
		"not base64": "!!!@@@",
		"not zlib":   notZlib,
	} {
		if _, err := Decode(code); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseRejectsNonPoBXML(t *testing.T) {
	for name, x := range map[string]string{
		"garbage":    "not xml at all",
		"wrong root": `<?xml version="1.0"?><Other/>`,
	} {
		if _, err := Parse([]byte(x)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseLegacyBuild(t *testing.T) {
	b, err := FromCode(string(readFile(t, "testdata/builds/OccVortex.code")))
	if err != nil {
		t.Fatal(err)
	}
	if b.Level != 99 || b.ClassName != "Witch" || b.AscendClassName != "Occultist" || b.MainSocketGroup != 5 {
		t.Errorf("build header: %+v", b)
	}
	if v, ok := b.Stat("AverageHit"); !ok || v != 73773.92 {
		t.Errorf("AverageHit = %v, %v", v, ok)
	}

	it := b.Item(1)
	if it == nil || !strings.HasPrefix(it.Raw, "Rarity: RARE\nLoath Sanctuary\nVaal Spirit Shield\n") {
		t.Fatalf("item 1: %+v", it)
	}
	if strings.Contains(it.Raw, "\t") || !strings.HasSuffix(it.Raw, "Corrupted") {
		t.Errorf("item 1 raw not normalised: %q", it.Raw)
	}
	set := b.ActiveItems()
	if set == nil || set.SlotItem("Weapon 1") != 3 {
		t.Errorf("active item set: Weapon 1 -> %v", set)
	}

	spec := b.ActiveSpec()
	if spec == nil || spec.TreeVersion != "3_13" || spec.ClassID != 3 || spec.AscendClassID != 1 {
		t.Fatalf("spec: %+v", spec)
	}
	if len(spec.Nodes) == 0 || spec.Nodes[0] != 7388 {
		t.Errorf("nodes: %v", spec.Nodes[:min(3, len(spec.Nodes))])
	}
	if len(spec.Sockets) == 0 || spec.Sockets[0] != (Socket{NodeID: 16218, ItemID: 0}) {
		t.Errorf("sockets: %v", spec.Sockets)
	}

	skills := b.ActiveSkills()
	if skills == nil || len(skills.Groups) != 6 {
		t.Fatalf("legacy skills must become one skill set with 6 groups: %+v", skills)
	}
	g := skills.Groups[0]
	if g.Slot != "Weapon 2" || !g.Enabled || len(g.Gems) == 0 {
		t.Fatalf("group 0: %+v", g)
	}
	gem := g.Gems[0]
	if gem.NameSpec != "Flame Dash" || gem.SkillID != "FlameDash" || gem.Level != 20 || gem.Quality != 20 || !gem.Enabled || gem.Count != 1 {
		t.Errorf("gem 0: %+v", gem)
	}

	cfg := b.ActiveConfig()
	if cfg == nil {
		t.Fatal("legacy config must become one config set")
	}
	if in, ok := cfg.Input("enemyIsBoss"); !ok || in.Kind != KindString || in.String != "Shaper" {
		t.Errorf("enemyIsBoss: %+v", in)
	}
	if in, ok := cfg.Input("conditionCritRecently"); !ok || in.Kind != KindBool || !in.Bool {
		t.Errorf("conditionCritRecently: %+v", in)
	}
}

func TestParseModernBuild(t *testing.T) {
	b, err := Parse(readFile(t, "testdata/modern.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if b.PantheonMajorGod != "Lunaris" || b.Level != 94 {
		t.Errorf("header: %+v", b)
	}

	spec := b.ActiveSpec()
	if spec == nil || spec.Title != "Endgame" || spec.SecondaryAscendClassID != 3 {
		t.Fatalf("active spec: %+v", spec)
	}
	wantMastery := []MasteryEffect{{Node: 53188, Effect: 64875}, {Node: 6770, Effect: 38129}}
	if !reflect.DeepEqual(spec.MasteryEffects, wantMastery) {
		t.Errorf("mastery: %v", spec.MasteryEffects)
	}
	if !reflect.DeepEqual(spec.Nodes, []int{100, 200, 300}) {
		t.Errorf("nodes: %v", spec.Nodes)
	}

	hh := b.Item(1)
	if hh == nil || hh.Variant != 2 || len(hh.ModRanges) != 1 || hh.ModRanges[0] != (ModRange{ID: 2, Range: 0.5}) {
		t.Fatalf("item 1: %+v", hh)
	}
	if strings.Contains(hh.Raw, "ModRange") || !strings.HasSuffix(hh.Raw, "+(40-55) to Strength") {
		t.Errorf("item 1 raw: %q", hh.Raw)
	}
	set := b.ActiveItems()
	if set.Title != "Main" || set.SlotItem("Belt") != 1 || !set.Slots[1].Active {
		t.Errorf("item set: %+v", set)
	}

	skills := b.ActiveSkills()
	if skills.Title != "Bow" || len(skills.Groups) != 1 {
		t.Fatalf("skills: %+v", skills)
	}
	g := skills.Groups[0]
	if g.Label != "Main" || !g.IncludeInFullDPS {
		t.Errorf("group: %+v", g)
	}
	ts, ma := g.Gems[0], g.Gems[1]
	if ts.Level != 21 || ts.SkillPart != 2 || ts.VariantID != "TornadoShot" || ts.EnableGlobal2 {
		t.Errorf("tornado shot: %+v", ts)
	}
	// PoB defaults: enableGlobal1 absent -> true, enabled="false" respected.
	if ma.Enabled || !ma.EnableGlobal1 {
		t.Errorf("mirage archer: %+v", ma)
	}

	cfg := b.ActiveConfig()
	if cfg.Title != "Mapping" {
		t.Errorf("config title: %q", cfg.Title)
	}
	if in, _ := cfg.Input("multiplierPowerCharges"); in.Kind != KindNumber || in.Number != 3.5 {
		t.Errorf("number input: %+v", in)
	}
	if len(cfg.Placeholders) != 1 || cfg.Placeholders[0].Number != 50000 {
		t.Errorf("placeholders: %+v", cfg.Placeholders)
	}
	if len(cfg.CustomMods) != 1 || cfg.CustomMods[0].Text != "+10% to all Elemental Resistances" || !cfg.CustomMods[0].Enabled {
		t.Errorf("custom mods: %+v", cfg.CustomMods)
	}
}

func TestParseSkipsNonNumericStats(t *testing.T) {
	// Seen in Generals_Perforate_Zerker.xml: PoB saved a Lua table reference.
	b, err := Parse([]byte(`<PathOfBuilding><Build level="90">` +
		`<PlayerStat stat="SkillDPS" value="table: 0x155a5ac0"/><PlayerStat stat="Life" value="5000"/>` +
		`</Build></PathOfBuilding>`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := b.Stat("SkillDPS"); ok {
		t.Error("non-numeric stat must be skipped")
	}
	if v, _ := b.Stat("Life"); v != 5000 {
		t.Errorf("Life = %v", v)
	}
}

func TestRoundTrip(t *testing.T) {
	inputs := map[string][]byte{"modern.xml": readFile(t, "testdata/modern.xml")}
	for _, codePath := range corpus(t) {
		x, err := Decode(string(readFile(t, codePath)))
		if err != nil {
			t.Fatal(err)
		}
		inputs[filepath.Base(codePath)] = x
	}
	for name, x := range inputs {
		t.Run(name, func(t *testing.T) {
			b1, err := Parse(x)
			if err != nil {
				t.Fatal(err)
			}
			code, err := b1.Code()
			if err != nil {
				t.Fatal(err)
			}
			if strings.ContainsAny(code, "+/") {
				t.Errorf("code must use the URL-safe alphabet")
			}
			b2, err := FromCode(code)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(b1, b2) {
				t.Fatal("build changed after encode/decode")
			}
		})
	}
}
