package engine

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Mi7teR/exile-calc/pobcode"
)

// TestPartyExport: Result.PartyExport against PoB's
// partyTab:exportBuffs for every buff type (testdata/golden/
// party-exports.json, from the oracle with "Export Support" on; PoB keeps
// the CALCS pass's export, Classes/CalcsTab.lua:459-461). The builds cover
// warcries (-s4JtJhUcgeE), links (b7vXBYdtxO95), ally buffs as
// otherEffects (LQlWfZuc4PvZ), minion auras with spectre sources
// (a2nRZLW9ZVuu) and shock and scorch stacks (KR_vSVUJvzBw). Blocks come
// in PoB's order; inside a block the engine orders mods by name and PoB
// by stat processing, so a block's lines compare as a set.
func TestPartyExport(t *testing.T) {
	raw, err := os.ReadFile("../testdata/golden/party-exports.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]map[string]string
	if jerr := json.Unmarshal(raw, &want); jerr != nil {
		t.Fatal(jerr)
	}
	c := newCalc(t)
	for id, exports := range want {
		b := corpusBuild(t, id)
		if b.Party == nil {
			b.Party = &pobcode.Party{}
		}
		b.Party.ExportBuffs = true
		res, cerr := c.Calc(b)
		if cerr != nil {
			t.Fatal(cerr)
		}
		for typ, w := range exports {
			got := res.PartyExport(typ)
			gb, wb := exportBlocks(typ, got), exportBlocks(typ, w)
			if !slices.EqualFunc(gb, wb, slices.Equal[[]string]) {
				t.Errorf("%s %s:\n got %q\nwant %q", id, typ, got, w)
			}
		}
	}
}

// exportBlocks splits an export into its blocks (a name line, then its
// lines up to "---"), each block's lines after the name sorted.
func exportBlocks(typ, text string) [][]string {
	var out [][]string
	var cur []string
	flush := func() {
		if cur != nil {
			slices.Sort(cur[1:])
			out = append(out, cur)
			cur = nil
		}
	}
	for l := range strings.SplitSeq(text, "\n") {
		switch {
		case l == "---":
			flush()
		case typ == "PlayerMods" || typ == "EnemyConditions":
			out = append(out, []string{l})
		case typ == "EnemyMods" && !strings.Contains(l, "|"):
			flush()
			cur = []string{l}
		case cur == nil:
			cur = []string{l}
		default:
			cur = append(cur, l)
		}
	}
	flush()
	return out
}

// TestPartyExportLeechLink: Vampiric Link's real mod,
// MaximumLifeLeechIsEqualToParent (Data/Skills/act_dex.lua:18382), exports
// the player's MaxLifeLeechRatePercent output (user decision; PoB tests for
// the imported name MaximumLifeLeechIsEqualToPartyMember at
// CalcPerform.lua:3954, so it never exports it).
func TestPartyExportLeechLink(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "HBcW_8Hvft-z")
	var gem pobcode.Gem
	for id, g := range c.data.Gems {
		if g.Name == "Vampiric Link" {
			gem = pobcode.Gem{NameSpec: g.Name, GemID: id, SkillID: g.GrantedEffectID, Level: 20, Enabled: true,
				EnableGlobal1: true, EnableGlobal2: true, Count: 1}
		}
	}
	if gem.SkillID == "" {
		t.Fatal("no Vampiric Link gem")
	}
	set := b.ActiveSkills()
	set.Groups = append(set.Groups, pobcode.SocketGroup{Enabled: true, Gems: []pobcode.Gem{gem}})
	if b.Party == nil {
		b.Party = &pobcode.Party{}
	}
	b.Party.ExportBuffs = true
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	link, err := res.PartyExportErr("Link")
	if err != nil || !strings.Contains(link, "Vampiric Link") ||
		!strings.Contains(link, "MaximumLifeLeechIsEqualToParent") {
		t.Fatalf("Link export %q (%v)", link, err)
	}
	// The player's cap as the oracle has it on this build with the link
	// added (PoB v2.68.1: 21.16).
	const oracle = 21.16
	pct := res.DB.output["MaxLifeLeechRatePercent"]
	if pct != oracle {
		t.Fatalf("MaxLifeLeechRatePercent %v, oracle %v", pct, oracle)
	}
	want := "MaxLifeLeechRatePercent=" + luaToString(pct)
	if mods := res.PartyExport("PlayerMods"); !strings.Contains(mods, want) {
		t.Errorf("PlayerMods %q without %q", mods, want)
	}
}
