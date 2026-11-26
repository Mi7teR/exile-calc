package engine

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
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

// TestPartyExportLeechLink: a link's MaximumLifeLeechIsEqualToPartyMember
// exports the player's leech cap (user decision; PoB's "Parent" filter at
// CalcPerform.lua:3949 skips its branch, 3954-3955), and a "Parent" mod
// still exports as PoB does (3950-3951).
func TestPartyExportLeechLink(t *testing.T) {
	db := NewModDB()
	db.enemy = NewModDB()
	db.output["MaxLifeLeechRatePercent"], db.output["BlockChanceMax"] = 20, 75
	e := newPartyExports()
	e.link["Protective Link"] = &partyExport{effectMult: 1, mods: []Mod{
		{Name: "MaximumLifeLeechIsEqualToPartyMember", Type: "FLAG", Value: true},
		{Name: "MaximumBlockAttackChanceIsEqualToParent", Type: "FLAG", Value: true},
	}}
	e.finish(db, nil, hitSource{})
	for _, k := range []string{"MaxLifeLeechRatePercent=20", "BlockChanceMax=75"} {
		if !e.playerMods[k] {
			t.Errorf("PlayerMods %v without %s", e.playerMods, k)
		}
	}
}
