package itemtext

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

func parser(t *testing.T) *Parser {
	t.Helper()
	d, err := gamedata.PoE1()
	if err != nil {
		t.Fatal(err)
	}
	return New(d)
}

func texts(lines []ModLine) []string {
	var out []string
	for _, l := range lines {
		out = append(out, l.Text)
	}
	return out
}

func TestPoBFormatRare(t *testing.T) {
	// From pobcode/testdata/builds/OccVortex.xml, item 1.
	it, err := parser(t).Parse(`Rarity: RARE
Loath Sanctuary
Vaal Spirit Shield
Unique ID: fc24691eb70b5a54708e71b8142ebbbc86143ec938445222927b525295777c27
Item Level: 83
Quality: 0
Sockets: B-B-B
LevelReq: 70
Implicits: 1
9% increased Spell Damage
+54 to Intelligence
30% increased Energy Shield
+87 to maximum Life
Corrupted`)
	if err != nil {
		t.Fatal(err)
	}
	if it.Rarity != Rare || it.Name != "Loath Sanctuary" || it.BaseName != "Vaal Spirit Shield" || it.Base == nil {
		t.Fatalf("header: %+v", it)
	}
	if it.ItemLevel != 83 || it.LevelReq != 70 || !it.Corrupted || it.UniqueID == "" {
		t.Errorf("specs: %+v", it)
	}
	if want := []Socket{{'B', 0}, {'B', 0}, {'B', 0}}; !reflect.DeepEqual(it.Sockets, want) {
		t.Errorf("sockets: %v", it.Sockets)
	}
	if !reflect.DeepEqual(texts(it.Implicits), []string{"9% increased Spell Damage"}) {
		t.Errorf("implicits: %v", texts(it.Implicits))
	}
	if len(it.Explicits) != 3 || it.Explicits[2].Text != "+87 to maximum Life" {
		t.Errorf("explicits: %v", texts(it.Explicits))
	}
}

func TestPoBFormatCraftedImplicitIsEnchant(t *testing.T) {
	// Item.lua: a crafted line inside the "Implicits: N" count goes to enchants (anoints).
	it, err := parser(t).Parse(`Rarity: RARE
Apocalypse Noose
Citrine Amulet
Implicits: 2
{crafted}Allocates Prodigal Perfection
+16 to Strength and Dexterity
+43 to Strength
{crafted}+46 to maximum Life`)
	if err != nil {
		t.Fatal(err)
	}
	if len(it.Enchants) != 1 || it.Enchants[0].Text != "Allocates Prodigal Perfection" || !it.Enchants[0].Crafted {
		t.Errorf("enchants: %+v", it.Enchants)
	}
	if !reflect.DeepEqual(texts(it.Implicits), []string{"+16 to Strength and Dexterity"}) {
		t.Errorf("implicits: %v", texts(it.Implicits))
	}
	if len(it.Explicits) != 2 || !it.Explicits[1].Crafted || it.Explicits[0].Crafted {
		t.Errorf("explicits: %+v", it.Explicits)
	}
}

func TestPoBFormatUniqueVariantsAndTags(t *testing.T) {
	it, err := parser(t).Parse(`Headhunter
Leather Belt
League: Nemesis
Variant: Pre 3.11.0
Variant: Current
Requires Level 40
Implicits: 1
{tags:resource}+(25-40) to maximum Life
{variant:1}+(40-55) to Strength
{variant:2}{tags:attribute}+(50-60) to Strength`)
	if err != nil {
		t.Fatal(err)
	}
	if it.Rarity != Unique || it.Name != "Headhunter" || it.BaseName != "Leather Belt" || it.LevelReq != 40 || it.League != "Nemesis" {
		t.Fatalf("header: %+v", it)
	}
	if !reflect.DeepEqual(it.Variants, []string{"Pre 3.11.0", "Current"}) || it.Variant != 2 {
		t.Errorf("variants: %v, selected %d", it.Variants, it.Variant)
	}
	if it.Implicits[0].Text != "+(25-40) to maximum Life" || !reflect.DeepEqual(it.Implicits[0].Tags, []string{"resource"}) {
		t.Errorf("implicit: %+v", it.Implicits[0])
	}
	if len(it.Explicits) != 2 || !reflect.DeepEqual(it.Explicits[0].Variants, []int{1}) {
		t.Errorf("explicits: %+v", it.Explicits)
	}
	active := it.ActiveExplicits()
	if len(active) != 1 || active[0].Text != "+(50-60) to Strength" {
		t.Errorf("active explicits for variant 2: %v", texts(active))
	}
}

func TestGameTextRare(t *testing.T) {
	it, err := parser(t).Parse(`Item Class: Rings
Rarity: Rare
Gale Coil
Coral Ring
--------
Requirements:
Level: 45
--------
Item Level: 81
--------
+25 to maximum Life (implicit)
--------
+38 to maximum Life
+40% to Fire Resistance
+12% to Cold Resistance (crafted)
--------
Corrupted`)
	if err != nil {
		t.Fatal(err)
	}
	if it.ItemClass != "Rings" || it.Rarity != Rare || it.Name != "Gale Coil" || it.BaseName != "Coral Ring" || it.LevelReq != 45 {
		t.Fatalf("header: %+v", it)
	}
	if !reflect.DeepEqual(texts(it.Implicits), []string{"+25 to maximum Life"}) || !it.Implicits[0].Implicit {
		t.Errorf("implicits: %+v", it.Implicits)
	}
	if !reflect.DeepEqual(texts(it.Explicits), []string{"+38 to maximum Life", "+40% to Fire Resistance", "+12% to Cold Resistance"}) || !it.Explicits[2].Crafted {
		t.Errorf("explicits: %+v", it.Explicits)
	}
	if !it.Corrupted {
		t.Error("corrupted")
	}
}

func TestGameTextUniqueDropsFlavourText(t *testing.T) {
	it, err := parser(t).Parse(`Item Class: Amulets
Rarity: Unique
Astramentis
Onyx Amulet
--------
Requirements:
Level: 20
--------
Item Level: 84
--------
+16 to all Attributes (implicit)
--------
+100 to all Attributes
-4 Physical Damage taken from Attack Hits
--------
Mere mortals simply strive
to become immortal.
--------
Corrupted`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(texts(it.Explicits), []string{"+100 to all Attributes", "-4 Physical Damage taken from Attack Hits"}) {
		t.Errorf("explicits: %v", texts(it.Explicits))
	}
	if !it.Corrupted || len(it.Implicits) != 1 {
		t.Errorf("item: %+v", it)
	}
}

func TestGameTextMagicFindsBaseInName(t *testing.T) {
	it, err := parser(t).Parse(`Item Class: Belts
Rarity: Magic
Sturdy Leather Belt of the Whelpling
--------
Item Level: 50
--------
+25 to maximum Life (implicit)
--------
+22 to maximum Life
+5% to Fire Resistance`)
	if err != nil {
		t.Fatal(err)
	}
	if it.BaseName != "Leather Belt" || it.Name != "Sturdy Leather Belt of the Whelpling" {
		t.Errorf("magic base: %q / %q", it.BaseName, it.Name)
	}
	if len(it.Explicits) != 2 || len(it.Implicits) != 1 {
		t.Errorf("mods: %v / %v", texts(it.Implicits), texts(it.Explicits))
	}
}

func TestGameTextWeaponInfluenceAndSockets(t *testing.T) {
	it, err := parser(t).Parse(`Item Class: Two Hand Axes
Rarity: Rare
Beast Edge
Vaal Axe
--------
Two Handed Axe
Physical Damage: 150-250 (augmented)
Critical Strike Chance: 5.00%
Attacks per Second: 1.30
Weapon Range: 1.3 metres
--------
Requirements:
Level: 64
Str: 158
Dex: 76
--------
Sockets: R-R-R G-G B
--------
Item Level: 84
--------
25% chance to Maim on Hit (implicit)
--------
150% increased Physical Damage
--------
Shaper Item`)
	if err != nil {
		t.Fatal(err)
	}
	want := []Socket{{'R', 0}, {'R', 0}, {'R', 0}, {'G', 1}, {'G', 1}, {'B', 2}}
	if !reflect.DeepEqual(it.Sockets, want) {
		t.Errorf("sockets: %v", it.Sockets)
	}
	if !reflect.DeepEqual(it.Influences, []string{"shaper"}) {
		t.Errorf("influences: %v", it.Influences)
	}
	if len(it.Implicits) != 1 || !reflect.DeepEqual(texts(it.Explicits), []string{"150% increased Physical Damage"}) {
		t.Errorf("mods: %v / %v", texts(it.Implicits), texts(it.Explicits))
	}
	if it.Req.Str != 158 || it.Req.Dex != 76 {
		t.Errorf("req: %+v", it.Req)
	}
}

func TestGameTextAdvancedCopy(t *testing.T) {
	it, err := parser(t).Parse(`Item Class: Rings
Rarity: Rare
Gale Coil
Coral Ring
--------
Item Level: 81
--------
{ Implicit Modifier — Life }
+25(20-30) to maximum Life (implicit)
--------
{ Prefix Modifier "Virile" (Tier: 4) — Life }
+48(40-49) to maximum Life
{ Master Crafted Suffix Modifier "of Craft" — Elemental, Cold, Resistance }
+12(10-15)% to Cold Resistance (crafted)`)
	if err != nil {
		t.Fatal(err)
	}
	imp := it.Implicits[0]
	if imp.Text != "+25 to maximum Life" || imp.Range != 0.5 || !imp.HasRange {
		t.Errorf("implicit: %+v", imp)
	}
	if len(it.Explicits) != 2 {
		t.Fatalf("explicits: %+v", it.Explicits)
	}
	life, cold := it.Explicits[0], it.Explicits[1]
	if life.Text != "+48 to maximum Life" || !reflect.DeepEqual(life.Tags, []string{"life"}) || life.Crafted {
		t.Errorf("life: %+v", life)
	}
	if cold.Text != "+12% to Cold Resistance" || !cold.Crafted || !reflect.DeepEqual(cold.Tags, []string{"elemental", "cold", "resistance"}) || cold.Range != 0.4 {
		t.Errorf("cold: %+v", cold)
	}
}

func TestParseErrors(t *testing.T) {
	p := parser(t)
	if _, err := p.Parse("   \n "); err == nil {
		t.Error("empty text must fail")
	}
	if _, err := p.Parse("Rarity: RARE\nSome Name\nNot A Real Base"); err == nil {
		t.Error("unknown base must fail")
	}
}

// Every item of the pobcode corpus and every exported unique must resolve
// to a known base. This is the stage 0 criterion for itemtext.
func TestCorpusAndUniques(t *testing.T) {
	p := parser(t)
	codes, _ := filepath.Glob("../pobcode/testdata/builds/*.code")
	items := 0
	for _, c := range codes {
		raw, err := os.ReadFile(c)
		if err != nil {
			t.Fatal(err)
		}
		b, err := pobcode.FromCode(string(raw))
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range b.Items {
			items++
			if _, err := p.Parse(item.Raw); err != nil {
				t.Errorf("%s item %d: %v", filepath.Base(c), item.ID, err)
			}
		}
	}
	d, _ := gamedata.PoE1()
	failed := 0
	for _, u := range d.Uniques {
		it, err := p.Parse(u.Raw)
		if err != nil {
			failed++
			if failed <= 10 {
				t.Errorf("unique %s: %v", u.Name, err)
			}
			continue
		}
		if it.Rarity != Unique || it.Name != sanitise(u.Name) { // PoB normalises "Mjölner" to "Mjolner"
			t.Errorf("unique %s parsed as %s %q", u.Name, it.Rarity, it.Name)
		}
	}
	t.Logf("corpus items: %d, uniques: %d (failed %d)", items, len(d.Uniques), failed)
	if items == 0 {
		t.Error("no corpus items")
	}
}

func TestSanitiseAndMarkup(t *testing.T) {
	// Common.lua: sanitiseText and escapeGGGString.
	it, err := parser(t).Parse("Duskdawn\nMaelström Staff\nImplicits: 0\n+(30-40)% to [Resistances|Elemental Resistances]\n<enchanted>{Adds 1 to 2} Fire Damage")
	if err != nil {
		t.Fatal(err)
	}
	if it.BaseName != "Maelstrom Staff" {
		t.Errorf("base: %q", it.BaseName)
	}
	if !reflect.DeepEqual(texts(it.Explicits), []string{"+(30-40)% to Elemental Resistances", "Adds 1 to 2 Fire Damage"}) {
		t.Errorf("explicits: %v", texts(it.Explicits))
	}
}

func TestClassRestriction(t *testing.T) {
	p := parser(t)
	it, err := p.Parse("Rarity: UNIQUE\nForbidden Flesh\nCobalt Jewel\nRequires Class Scion\nImplicits: 0\nAllocates Pathfinder if you have the matching modifier on Forbidden Flame\nCorrupted")
	if err != nil {
		t.Fatal(err)
	}
	if got := it.ClassRestriction(); got != "Scion" {
		t.Errorf("spec line: got %q", got)
	}
	it, err = p.Parse("Rarity: UNIQUE\nForbidden Flame\nCrimson Jewel\nVariant: (Witch) A\nVariant: (Shadow) B\nSelected Variant: 2\nImplicits: 0\n{variant:1}Requires Class Witch\n{variant:2}Requires Class Shadow\n{variant:1}Allocates A if you have the matching modifier on Forbidden Flesh\n{variant:2}Allocates B if you have the matching modifier on Forbidden Flesh")
	if err != nil {
		t.Fatal(err)
	}
	if got := it.ClassRestriction(); got != "Shadow" {
		t.Errorf("variant line: got %q", got)
	}
	if len(it.ActiveExplicits()) != 1 {
		t.Errorf("Requires Class lines must not be mods: %+v", it.Explicits)
	}
}

func TestVariantGroups(t *testing.T) {
	text := `Rarity: UNIQUE
Zana's Ingenuity
Prismatic Ring
Variant: Life
Variant: Energy Shield
Variant: Mana
Variant: Reflect Immune
Selected Variant Group: 1=2
Selected Variant Group: 2=2
Implicits: 0
{variant:2}{group:1}20% increased maximum Energy Shield
{variant:1}{group:1}10% increased maximum Life
{variant:4}{group:2}Damage cannot be Reflected
+10% to all Elemental Resistances`
	it, err := parser(t).Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range it.ActiveExplicits() {
		got = append(got, l.Text)
	}
	// Group 2 cannot take variant 2 (used by group 1): its first free
	// eligible variant is 4.
	want := []string{"20% increased maximum Energy Shield", "Damage cannot be Reflected", "+10% to all Elemental Resistances"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAltVariant(t *testing.T) {
	text := `Rarity: UNIQUE
Watcher's Eye
Prismatic Jewel
Variant: A
Variant: B
Variant: C
Selected Variant: 1
Has Alt Variant: true
Selected Alt Variant: 3
Implicits: 0
{variant:1}line a
{variant:2}line b
{variant:3}line c`
	it, err := parser(t).Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, l := range it.ActiveExplicits() {
		got = append(got, l.Text)
	}
	if want := []string{"line a", "line c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
