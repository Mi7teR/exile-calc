package engine

import (
	"strings"
	"testing"

	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/modparser"
)

// luaFind handles only plain text with ^/$ anchors; fail if Data.lua
// starts using other pattern characters.
func TestItemTagPatterns(t *testing.T) {
	p, err := modparser.PoE1()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"itemTagSpecial", "itemTagSpecialExclusionPattern"} {
		tbl := p.Table(name)
		if len(tbl) == 0 {
			t.Fatalf("%s missing", name)
		}
		for sub, bySlot := range tbl {
			m, _ := bySlot.(map[string]any)
			for slot, l := range m {
				for _, v := range l.([]any) {
					s := strings.TrimSuffix(strings.TrimPrefix(v.(string), "^"), "$")
					if strings.ContainsAny(s, "%^$().[]*+-?") {
						t.Errorf("%s[%s][%s]: pattern %q needs a Lua pattern matcher", name, sub, slot, v)
					}
				}
			}
		}
	}
}

func TestItemConditionLifeOnBodyArmour(t *testing.T) {
	p, err := modparser.PoE1()
	if err != nil {
		t.Fatal(err)
	}
	db := NewModDB()
	db.tags = itemTags{special: p.Table("itemTagSpecial"), exclusion: p.Table("itemTagSpecialExclusionPattern")}
	tag := map[string]any{"type": "ItemCondition", "itemSlot": "body armour", "searchCond": "life", "neg": true}
	body := func(lines ...string) *itemtext.Item {
		it := &itemtext.Item{Rarity: itemtext.Rare}
		for _, l := range lines {
			it.Explicits = append(it.Explicits, itemtext.ModLine{Text: l})
		}
		return it
	}
	cases := []struct {
		item *itemtext.Item
		want bool
	}{
		{nil, true}, // no body armour: neg matches
		{body("+40 to Strength"), true},
		{body("+90 to maximum Life"), false},
		{body("Socketed Gems are Supported by Level 20 Life Leech"), true}, // excluded
		{body("Cannot Leech"), false}, // itemTagSpecial
	}
	for _, c := range cases {
		delete(db.items, "Body Armour")
		if c.item != nil {
			db.items["Body Armour"] = c.item
		}
		if got := db.itemCondition(tag); got != c.want {
			t.Errorf("%v: got %v want %v", c.item, got, c.want)
		}
	}
}
