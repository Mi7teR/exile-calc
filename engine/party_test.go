package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// TestPartyExportFlagInert: the Import tab's "Export Support"
// (partyTabEnableExportBuffs, CalcPerform.lua:1314) only fills buffExports,
// so it never changes the build's own numbers, and a Party that only
// exports is the same as none. The party's imported buffs apply with the
// flag off, as in PoB (the oracle gives identical output for the party-*
// corpus builds with exportParty on and off).
func TestPartyExportFlagInert(t *testing.T) {
	c := newCalc(t)
	stats := func(b *pobcode.Build) map[string]float64 {
		t.Helper()
		res, err := c.Calc(b)
		if err != nil {
			t.Fatal(err)
		}
		return res.Stats
	}

	base := corpusBuild(t, "NCgd_ieVLUw7")
	exportOnly := corpusBuild(t, "NCgd_ieVLUw7")
	exportOnly.Party = &pobcode.Party{ExportBuffs: true}
	if diff := cmp.Diff(stats(base), stats(exportOnly)); diff != "" {
		t.Errorf("exporting changed the build (-none +export):\n%s", diff)
	}

	party := corpusBuild(t, "party-NCgd_ieVLUw7")
	if party.Party == nil || party.Party.ExportBuffs {
		t.Fatalf("party-NCgd_ieVLUw7 must import buffs without exporting: %+v", party.Party)
	}
	withParty := stats(party)
	if withParty["Armour"] == stats(base)["Armour"] {
		t.Error("imported party auras must apply without export")
	}
	party.Party.ExportBuffs = true
	if diff := cmp.Diff(withParty, stats(party)); diff != "" {
		t.Errorf("export flag changed a party build (-off +on):\n%s", diff)
	}
}

func TestPartyBuffsParse(t *testing.T) {
	a := newPartyActor(&pobcode.Party{Buffs: []pobcode.PartyBuffs{
		{Name: "Aura", Text: "Hatred\n200\n" +
			"30|Skill:Hatred|PhysicalDamageGainAsCold|BASE|-|-|type=GlobalEffect/effectType=Aura\n" +
			"2|Tree:10099|Speed|INC|Cast|-|-\n" +
			"---\nHatred\n100\n" + // weaker duplicate: skipped
			"99|Skill:Hatred|PhysicalDamageGainAsCold|BASE|-|-|type=GlobalEffect/effectType=Aura\n" +
			"---\nVaal Haste\n300\n" +
			"30|Skill:VaalHaste|Speed|INC|Attack|-|type=GlobalEffect/effectType=Aura\n" +
			"---\nextraAura\n" +
			"10|Item:7:Mask of the Tribunal, Magistrate Crown|SpellBlockChance|BASE|-|-|-\n" +
			"--- a comment\n---\n" +
			"Smite\n250\n" +
			"20|Skill:Smite|EnemyShockChance|BASE|-|-|type=GlobalEffect/effectType=Aura,type=Condition/neg=true/var=AffectedByVaalSmite\n---"},
	}})
	checkPartyAuras(t, a)

	a = newPartyActor(&pobcode.Party{Buffs: []pobcode.PartyBuffs{
		{Name: "Curse", Text: "2\nAssassin's Mark\n150\ntrue\n" +
			"5|Skill:AssassinsMark|SelfCritChance|BASE|-|-|type=GlobalEffect/effectType=Curse\n---"},
		{Name: "Link Skills", Text: "Soul Link\n120\n" +
			"1|Skill:SoulLink|ParentLifeRegen|BASE|-|-|type=GlobalEffect/effectType=Link,type=PerStat/actor=parent/stat=Life/div=100\n---"},
		{
			Name: "PartyMemberStats",
			Text: "MovementSpeedMod|percent|max=174\nLife=5000\nMainHand.CritChance=50\nPartyMemberFortifyEqualToYours",
		},
		{Name: "EnemyConditions", Text: "Condition:Ignited"},
		{Name: "EnemyMods", Text: "ActionSpeed\n-10|Chill|ActionSpeed|INC|-|-|-"},
	}})

	if a.curseLimit != 2 {
		t.Errorf("curse limit = %v", a.curseLimit)
	}
	if mark := a.curse["Curse"].get("Assassin's Mark"); mark == nil || !mark.isMark || mark.effect != 150 {
		t.Errorf("Assassin's Mark = %+v", mark)
	}
	link := a.link["Link"].get("Soul Link")
	if link == nil || link.mods[0].Name != "PartyMemberLifeRegen" || link.mods[0].Tags[1]["actor"] != "partyMembers" ||
		link.mods[0].Tags[1]["div"] != "100" {
		t.Errorf("Soul Link = %+v", link)
	}
	if a.output["MovementSpeedMod"] != 1.74 || a.output["Life"] != 5000 || a.output["MainHand.CritChance"] != 50 {
		t.Errorf("member stats = %v", a.output)
	}
	if len(a.flags) != 1 || a.flags[0].Name != "PartyMemberFortifyEqualToYours" {
		t.Errorf("member flags = %+v", a.flags)
	}
	want := []Mod{
		{Name: "Condition:Party:Ignited", Type: "FLAG", Value: true, Source: "Party"},
		{Name: "ActionSpeed", Type: "INC", Value: -10.0, Source: "PartyChill"},
	}
	if diff := cmp.Diff(want, a.enemyMods); diff != "" {
		t.Errorf("enemy mods (-want +got):\n%s", diff)
	}
	if newPartyActor(nil) != nil || newPartyActor(&pobcode.Party{ExportBuffs: true}) != nil {
		t.Error("no imported buffs must give no party actor")
	}
}

// TestPartyCurseWarcryLink checks the hand-written Curse, Warcry and Link
// blocks of party-NCgd_ieVLUw7 reach the enemy and the player.
func TestPartyCurseWarcryLink(t *testing.T) {
	res, err := newCalc(t).Calc(corpusBuild(t, "party-NCgd_ieVLUw7"))
	if err != nil {
		t.Fatal(err)
	}
	db := res.DB
	found := func(d *ModDB, name, source string) bool {
		for _, m := range d.mods[name] {
			if m.Source == source {
				return true
			}
		}
		return false
	}
	if !found(db.enemy, "FireResist", "Skill:Flammability") {
		t.Error("party Flammability not on the enemy")
	}
	if !found(db, "Armour", "Skill:AncestralCry") || !db.conditions["AffectedByAncestralCry"] {
		t.Error("party Ancestral Cry not on the player")
	}
	if !found(db, "PhysicalDamageGainAsFire", "Skill:FlameLink") || !db.conditions["AffectedByLink"] {
		t.Error("party Flame Link not on the player")
	}
	// The real Flame Link export: FireMin 3 per 1% of the linking party
	// member's Life (PercentStat actor=partyMembers, Life=6000 from
	// PartyMemberStats), at 200% effect: 3 * 6000 * 1/100 * 2 = 360, as PoB.
	for _, name := range []string{"FireMin", "FireMax"} {
		got := 0.0
		for _, r := range db.TabulateCfg(nil, "BASE", name) {
			if v, ok := num(r.Value); ok && r.Source == "Skill:FlameLink" {
				got += v
			}
		}
		if got != 360 {
			t.Errorf("Flame Link %s = %v, want 360", name, got)
		}
	}
}

// checkPartyAuras checks the Aura tab of TestPartyBuffsParse.
func checkPartyAuras(t *testing.T, a *partyActor) {
	t.Helper()
	hatred := a.aura["Aura"].get("Hatred")
	if hatred == nil || hatred.effect != 200 || len(hatred.mods) != 2 {
		t.Fatalf("Hatred = %+v", hatred)
	}
	if hatred.mods[1].Name != "Speed" || hatred.mods[1].Flags != 0x10 || hatred.mods[1].Tags != nil {
		t.Errorf("untagged line = %+v, want Speed INC with ModFlag.Cast in the aura", hatred.mods[1])
	}
	if a.aura["Aura"].get("Vaal Haste") != nil || len(a.aura["Aura"].vaal) != 1 {
		t.Errorf("Vaal auras are kept apart: %+v", a.aura["Aura"])
	}
	if l := a.aura["extraAura"]; l == nil || len(l.buffs) != 1 ||
		l.buffs[0].mods[0].Source != "Party - Mask of the Tribunal, Magistrate Crown" {
		t.Errorf("extraAura = %+v", l)
	}
	smite := a.aura["Aura"].get("Smite")
	if smite == nil || len(smite.mods) != 1 || len(smite.mods[0].Tags) != 2 ||
		smite.mods[0].Tags[1]["neg"] != true || smite.mods[0].Tags[1]["var"] != "AffectedByVaalSmite" {
		t.Errorf("Smite = %+v", smite)
	}
}

// TestLinkToMinion: with "Linked To Minion?" on, Flame Link reaches the
// Animated Guardian. The link also carries the player's non-unique flasks
// to it, which is not ported: the calculation reports it only when such a
// flask is active (none is in the saved build).
func TestLinkToMinion(t *testing.T) {
	c := newCalc(t)
	calc := func(b *pobcode.Build) *Result {
		t.Helper()
		res, err := c.Calc(withMainSkill(b, 2, 2))
		if err != nil {
			t.Fatal(err)
		}
		if res.Minion == nil || res.Minion.Minion != "Animated Guardian" {
			t.Fatalf("main minion = %+v", res.Minion)
		}
		return res
	}
	note := func(res *Result) bool {
		return slices.Contains(res.Incomplete, "flask effects on the minion are not applied")
	}
	plain, linked := calc(corpusBuild(t, "b7vXBYdtxO95")), calc(corpusBuild(t, "link-b7vXBYdtxO95"))
	if linked.Minion.Stats["TotalDPS"] <= plain.Minion.Stats["TotalDPS"] {
		t.Errorf("Flame Link must raise the guardian's DPS: %v -> %v",
			plain.Minion.Stats["TotalDPS"], linked.Minion.Stats["TotalDPS"])
	}
	if note(plain) || note(linked) {
		t.Errorf("flask note without an active flask: %v / %v", plain.Incomplete, linked.Incomplete)
	}

	b := corpusBuild(t, "link-b7vXBYdtxO95")
	set := b.ActiveItems()
	for i := range set.Slots {
		if strings.HasPrefix(set.Slots[i].Name, "Flask") {
			set.Slots[i].Active = true
		}
	}
	if res := calc(b); !note(res) {
		t.Errorf("active non-unique flasks on a link: no flask note (%v)", res.Incomplete)
	}
}

// TestPartyProtectiveLink: a party Protective Link sets block to the
// linking member's (CalcDefence.lua ...IsEqualToPartyMember), and a link
// whose effect line is not a number counts at 100% ((effectMult or 100)).
func TestPartyProtectiveLink(t *testing.T) {
	b := corpusBuild(t, "NCgd_ieVLUw7")
	b.Party = &pobcode.Party{Buffs: []pobcode.PartyBuffs{
		{Name: "PartyMemberStats", Text: "BlockChance=40\nBlockChanceMax=60"},
		{Name: "Link Skills", Text: "Protective Link\nnot a number\n" +
			"1|Skill:ProtectiveLink|MaximumBlockAttackChanceIsEqualToParent|FLAG|-|-|type=GlobalEffect/effectType=Link\n" +
			"1|Skill:ProtectiveLink|BlockAttackChanceIsEqualToParent|FLAG|-|-|type=GlobalEffect/effectType=Link\n" +
			"10|Skill:ProtectiveLink|Armour|INC|-|-|type=GlobalEffect/effectType=Link\n---"},
	}}
	res, err := newCalc(t).Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if res.Stats["BlockChanceMax"] != 60 || res.Stats["BlockChance"] != 40 {
		t.Errorf("block = %v/%v, want the party member's 40/60", res.Stats["BlockChance"], res.Stats["BlockChanceMax"])
	}
	got := 0.0
	for _, r := range res.DB.TabulateCfg(nil, "INC", "Armour") {
		if v, ok := num(r.Value); ok && r.Source == "Skill:ProtectiveLink" {
			got += v
		}
	}
	if got != 10 {
		t.Errorf("Protective Link Armour INC = %v, want 10 (100%% effect)", got)
	}
}
