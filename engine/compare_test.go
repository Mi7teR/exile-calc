package engine

import (
	"encoding/xml"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// gameRing is a rare ring as the game copies it (Ctrl+C).
const gameRing = "Item Class: Rings\r\nRarity: Rare\r\nGale Coil\r\nCoral Ring\r\n--------\r\nRequirements:\r\nLevel: 45\r\n--------\r\n" +
	"Item Level: 81\r\n--------\r\n+25 to maximum Life (implicit)\r\n--------\r\n+38 to maximum Life\r\n+40% to Fire Resistance\r\n" +
	"+12% to Cold Resistance (crafted)\r\n--------\r\nCorrupted\r\n"

// slotRaw is the raw text of the item in a slot of b's active item set.
func slotRaw(t *testing.T, b *pobcode.Build, slot string) string {
	t.Helper()
	it := b.Item(b.ActiveItems().SlotItem(slot))
	if it == nil {
		t.Fatalf("no item in %s", slot)
	}
	return it.Raw
}

// editSlots returns a copy of b with the active set's slots set to new
// items of the given texts ("" empties the slot), done by hand.
func editSlots(b *pobcode.Build, texts map[string]string) *pobcode.Build {
	cp := *b
	cp.Items = append([]pobcode.Item(nil), b.Items...)
	cp.ItemSets = append([]pobcode.ItemSet(nil), b.ItemSets...)
	set := cp.ActiveItems()
	set.Slots = append([]pobcode.Slot(nil), set.Slots...)
	for i := range set.Slots {
		text, ok := texts[set.Slots[i].Name]
		if !ok {
			continue
		}
		set.Slots[i].ItemID = 0
		if text != "" {
			id := 1000 + len(cp.Items)
			cp.Items = append(cp.Items, pobcode.Item{ID: id, Raw: normaliseItemText(text)})
			set.Slots[i].ItemID = id
		}
	}
	return &cp
}

func newComparer(t *testing.T, c *Calculator, b *pobcode.Build) *Comparer {
	t.Helper()
	cp, err := c.NewComparer(b)
	if err != nil {
		t.Fatal(err)
	}
	return cp
}

// The same item text in its own slot changes nothing; Before is Calc's.
func TestCompareSameItemZeroDelta(t *testing.T) {
	c := newCalc(t)
	for _, id := range []string{"vPuXJxFgodF9", "3uy_tEm_vqpB"} {
		b := corpusBuild(t, id)
		cp := newComparer(t, c, b)
		for _, slot := range []string{"Ring 1", "Weapon 1", "Body Armour"} {
			r, err := cp.Item(slot, slotRaw(t, b, slot))
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Delta) != 0 {
				t.Errorf("%s %s: delta %v, want none", id, slot, r.Delta)
			}
			if !slices.Contains(r.Replaced, slot) {
				t.Errorf("%s %s: Replaced %v", id, slot, r.Replaced)
			}
		}
		res, err := c.Calc(b)
		if err != nil {
			t.Fatal(err)
		}
		cmpRes, _ := cp.Item("Ring 1", slotRaw(t, b, "Ring 1"))
		for k, v := range res.Stats {
			if cmpRes.Before[k] != v {
				t.Errorf("%s Before[%s] %v, Calc %v", id, k, cmpRes.Before[k], v)
			}
		}
	}
}

// After is Calc (and Full DPS) of the build with the item put in by hand.
func TestCompareMatchesEditedBuild(t *testing.T) {
	c := newCalc(t)
	for _, id := range []string{"vPuXJxFgodF9", "3uy_tEm_vqpB", "-fxcKCnOf7D2"} {
		b := corpusBuild(t, id)
		cp := newComparer(t, c, b)
		r, err := cp.Item("Ring 1", gameRing)
		if err != nil {
			t.Fatal(err)
		}
		edited := editSlots(b, map[string]string{"Ring 1": gameRing})
		res, err := c.Calc(edited)
		if err != nil {
			t.Fatal(err)
		}
		want := maps.Clone(res.Stats)
		if cp.usedFullDPS {
			full, ferr := c.FullDPS(edited)
			if ferr != nil {
				t.Fatal(ferr)
			}
			want["FullDPS"], want["FullDotDPS"] = full.FullDPS, full.FullDotDPS
		}
		if d := cmp.Diff(want, r.After); d != "" {
			t.Errorf("%s: After differs from the edited build's Calc (-want +got):\n%s", id, d)
		}
		if r.Delta["FireResistTotal"] == 0 { // the ring has +40% fire resistance
			t.Errorf("%s: delta %v, want a FireResistTotal change", id, r.Delta)
		}
		for k, v := range r.Delta {
			if v != r.After[k]-r.Before[k] {
				t.Errorf("%s: Delta[%s] %v != After-Before %v", id, k, v, r.After[k]-r.Before[k])
			}
		}
	}
	// Full DPS is in the output only when the build uses it.
	b := corpusBuild(t, "3uy_tEm_vqpB")
	r, err := newComparer(t, c, b).Item("Ring 1", gameRing)
	if err != nil {
		t.Fatal(err)
	}
	if r.After["FullDPS"] == 0 {
		t.Errorf("3uy_tEm_vqpB: no FullDPS in %v", r.After["FullDPS"])
	}
	if r, err = newComparer(t, c, withoutFullDPS(b)).Item("Ring 1", gameRing); err != nil {
		t.Fatal(err)
	}
	if hasKey(r.After, "FullDPS") {
		t.Errorf("no Full DPS skills: After has FullDPS %v", r.After["FullDPS"])
	}
}

func hasKey(m map[string]float64, k string) bool {
	_, ok := m[k]
	return ok
}

// CalcSetup.lua override.repSlotName: a two-hander in Weapon 1 empties
// Weapon 2; a bow keeps a quiver and empties anything else.
func TestCompareTwoHanderClearsOffhand(t *testing.T) {
	c := newCalc(t)
	axe := slotRaw(t, corpusBuild(t, "vPuXJxFgodF9"), "Weapon 1") // Corpse Sever, Two Handed Axe
	bow := slotRaw(t, corpusBuild(t, "VFJTgt8AL50o"), "Weapon 1") // Hypnotic Twine, Bow

	b := corpusBuild(t, "-fxcKCnOf7D2") // wand and shield
	cp := newComparer(t, c, b)
	for _, weapon := range []string{axe, bow} {
		r, err := cp.Item("Weapon 1", weapon)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(r.Replaced, "Weapon 2") || !slices.Contains(r.Replaced, "Weapon 1") {
			t.Errorf("Replaced %v, want Weapon 1 and Weapon 2", r.Replaced)
		}
		cleared, err := c.Calc(editSlots(b, map[string]string{"Weapon 1": weapon, "Weapon 2": ""}))
		if err != nil {
			t.Fatal(err)
		}
		if d := cmp.Diff(cleared.Stats, r.After); d != "" {
			t.Errorf("After differs from the build without Weapon 2 (-want +got):\n%s", d)
		}
		kept, err := c.Calc(editSlots(b, map[string]string{"Weapon 1": weapon}))
		if err != nil {
			t.Fatal(err)
		}
		if kept.Stats["BlockChance"] == r.After["BlockChance"] || r.Before["BlockChance"] == r.After["BlockChance"] {
			t.Errorf("BlockChance before %v, with shield %v, after %v: the shield still counts",
				r.Before["BlockChance"], kept.Stats["BlockChance"], r.After["BlockChance"])
		}
	}

	// A bow over a bow keeps the quiver.
	b = corpusBuild(t, "2HB8-0xwYEA6") // bow and quiver
	r, err := newComparer(t, c, b).Item("Weapon 1", bow)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Replaced, []string{"Weapon 1"}) {
		t.Errorf("bow over bow: Replaced %v, want [Weapon 1]", r.Replaced)
	}
	// A one-hander does not touch the offhand.
	b = corpusBuild(t, "-fxcKCnOf7D2")
	if r, err = newComparer(t, c, b).Item("Weapon 1", slotRaw(t, b, "Weapon 1")); err != nil || len(r.Delta) != 0 {
		t.Errorf("one-hander: err %v, delta %v", err, r.Delta)
	}
}

// Comparisons leave the build as it was: a fresh decode is equal to it and
// its Calc is unchanged.
func TestComparerDoesNotMutateBuild(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "3uy_tEm_vqpB")
	before, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	cp := newComparer(t, c, b)
	axe := slotRaw(t, corpusBuild(t, "vPuXJxFgodF9"), "Weapon 1")
	for _, q := range []struct{ slot, text string }{
		{"Ring 1", gameRing}, {"Ring 2", gameRing}, {"Weapon 1", axe}, {"Flask 1", slotRaw(t, b, "Flask 1")},
		{"Helmet Abyssal Socket 1", gameRing}, {"Jewel 26725", gameRing}, {"Ring 1", gameRing},
	} {
		if _, ierr := cp.Item(q.slot, q.text); ierr != nil {
			t.Fatalf("%s: %v", q.slot, ierr)
		}
	}
	if !reflect.DeepEqual(b, corpusBuild(t, "3uy_tEm_vqpB")) {
		t.Error("build changed")
	}
	after, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(before.Stats, after.Stats); d != "" {
		t.Errorf("Calc changed (-before +after):\n%s", d)
	}
}

func TestCompareItemErrors(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "vPuXJxFgodF9")
	cp := newComparer(t, c, b)
	ring := slotRaw(t, b, "Ring 1")
	for _, q := range []struct{ name, slot, text string }{
		{"unknown slot", "Ring 4", ring},
		{"empty slot name", "", ring},
		{"lower-case slot", "ring 1", ring},
		{"jewel not a socket", "Jewel 1", ring},
		{"jewel not a number", "Jewel x", ring},
		{"empty text", "Ring 1", ""},
		{"blank text", "Ring 1", " \r\n\n "},
		{"garbage", "Ring 1", "hello\nworld"},
		{"unknown base", "Ring 1", "Rarity: Rare\nFoo Bar\nNot A Base"},
	} {
		if r, err := cp.Item(q.slot, q.text); err == nil {
			t.Errorf("%s: no error, got %+v", q.name, r.Delta)
		}
	}
}

// firstJewel is the raw text of the first jewel in b's tree sockets.
func firstJewel(t *testing.T, b *pobcode.Build) string {
	t.Helper()
	for _, s := range b.ActiveSpec().Sockets {
		if it := b.Item(s.ItemID); it != nil {
			return it.Raw
		}
	}
	t.Fatal("no socketed jewel")
	return ""
}

// Item calls from several goroutines, on different slots (a ring, a tree
// jewel socket, a two-hander emptying the offhand), give the same results
// as one call each.
func TestCompareConcurrent(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "-fxcKCnOf7D2") // wand and shield
	cp := newComparer(t, c, b)
	queries := []struct{ slot, text string }{
		{"Ring 1", gameRing},
		{"Jewel 26725", firstJewel(t, corpusBuild(t, "3uy_tEm_vqpB"))},
		{"Weapon 1", slotRaw(t, corpusBuild(t, "vPuXJxFgodF9"), "Weapon 1")}, // two-handed axe
	}
	want := make([]*ItemComparison, len(queries))
	for i, q := range queries {
		r, err := cp.Item(q.slot, q.text)
		if err != nil {
			t.Fatal(err)
		}
		want[i] = r
	}
	var wg sync.WaitGroup
	for n := range 3 * len(queries) {
		wg.Go(func() {
			i := n % len(queries)
			got, gerr := cp.Item(queries[i].slot, queries[i].text)
			if gerr != nil {
				t.Error(gerr)
				return
			}
			if d := cmp.Diff(want[i], got); d != "" {
				t.Errorf("concurrent Item %s differs:\n%s", queries[i].slot, d)
			}
		})
	}
	wg.Wait()
}

// A mod line the parser does not know is reported, not dropped.
func TestCompareReportsUnparsed(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "vPuXJxFgodF9")
	cp := newComparer(t, c, b)
	r, err := cp.Item(
		"Ring 1",
		strings.Replace(gameRing, "+38 to maximum Life", "+38 to maximum Life\n+7 to Wibbling of Nonsense", 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(r.Unparsed, func(l string) bool { return strings.Contains(l, "Wibbling of Nonsense") }) {
		t.Errorf("Unparsed %q, want the nonsense line", r.Unparsed)
	}
	// The base build's own unparsed lines are not repeated.
	if r, err = cp.Item("Ring 1", gameRing); err != nil {
		t.Fatal(err)
	}
	if len(r.Unparsed) != 0 || len(r.Incomplete) != 0 {
		t.Errorf("Unparsed %q Incomplete %q, want none", r.Unparsed, r.Incomplete)
	}
}

// One helmet as the game copies it and as PoB writes it: same After.
func TestCompareGameTextMatchesPoBText(t *testing.T) {
	const game = `Item Class: Helmets
Rarity: Rare
Doom Crown
Hubris Circlet
--------
Quality: +20% (augmented)
Energy Shield: 300 (augmented)
--------
Requirements:
Level: 69
Int: 154
--------
Sockets: B-B-R G
--------
Item Level: 84
--------
+25 to maximum Life (implicit)
--------
+80 to maximum Energy Shield
+40% to Fire Resistance
+12% to Cold Resistance (crafted)
`
	const pob = `Rarity: RARE
Doom Crown
Hubris Circlet
Quality: 20
Sockets: B-B-R G
Item Level: 84
LevelReq: 69
Implicits: 1
+25 to maximum Life
+80 to maximum Energy Shield
+40% to Fire Resistance
{crafted}+12% to Cold Resistance`
	c := newCalc(t)
	cp := newComparer(t, c, corpusBuild(t, "vPuXJxFgodF9"))
	g, err := cp.Item("Helmet", game)
	if err != nil {
		t.Fatal(err)
	}
	p, err := cp.Item("Helmet", pob)
	if err != nil {
		t.Fatal(err)
	}
	if d := cmp.Diff(p.After, g.After); d != "" {
		t.Errorf("game text After differs from PoB text (-pob +game):\n%s", d)
	}
	if g.Delta["FireResistTotal"] == 0 || len(g.Unparsed) != 0 || len(p.Unparsed) != 0 {
		t.Errorf(
			"FireResistTotal delta %v, Unparsed game %q pob %q",
			g.Delta["FireResistTotal"],
			g.Unparsed,
			p.Unparsed,
		)
	}
}

// normaliseItemText gives the Raw pobcode makes from the same XML text.
func TestNormaliseItemTextMatchesPobcode(t *testing.T) {
	text := "\n\t\tRarity: RARE\n  Doom Crown  \n\n\tHubris Circlet\n \nImplicits: 0\n\t+80 to maximum Energy Shield\n\t"
	var esc strings.Builder
	if err := xml.EscapeText(&esc, []byte(text)); err != nil {
		t.Fatal(err)
	}
	doc := `<PathOfBuilding><Build level="1"/><Tree activeSpec="1"><Spec/></Tree><Items activeItemSet="1">` +
		`<Item id="1">` + esc.String() + `</Item></Items></PathOfBuilding>`
	b, err := pobcode.Parse([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Items) != 1 || b.Items[0].Raw != normaliseItemText(text) {
		t.Errorf("pobcode items %+v, engine Raw %q", b.Items, normaliseItemText(text))
	}
}
