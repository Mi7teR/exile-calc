package luadata

import (
	"reflect"
	"strings"
	"testing"
)

type weapon struct {
	PhysicalMin float64 `lua:"PhysicalMin"`
	Range       int     `lua:"Range"`
}

type base struct {
	Type    string            `lua:"type"`
	Tags    []string          `lua:"tags,keys"` // { default = true, ring = true } -> keys set to true
	Infl    map[string]string `lua:"influenceTags"`
	Weapon  *weapon           `lua:"weapon"`
	Missing *weapon           `lua:"armour"`
	Hidden  bool              `lua:"hidden"`
	In      []int             `lua:"in"` // tree: { "35362" }
	Nested  [][]string        `lua:"implicitModTypes"`
}

type mod struct {
	Affix  string              `lua:"affix"`
	Lines  []string            `lua:",list"`
	Hashes map[uint32][]string `lua:"tradeHashes"`
}

func TestDecodeStruct(t *testing.T) {
	v := eval(t, `return {
		type = "Ring", tags = { default = true, ring = true, off = false },
		influenceTags = { shaper = "ring_shaper" },
		weapon = { PhysicalMin = 5.5, Range = 11, Unknown = 1 },
		hidden = true, ["in"] = { "35362", 7 },
		implicitModTypes = { { "physical_damage", "damage" }, { } },
		ignored = { 1, 2 },
	}`)
	var got base
	if err := Decode(v, &got); err != nil {
		t.Fatal(err)
	}
	want := base{
		Type:   "Ring",
		Tags:   []string{"default", "ring"},
		Infl:   map[string]string{"shaper": "ring_shaper"},
		Weapon: &weapon{PhysicalMin: 5.5, Range: 11},
		Hidden: true,
		In:     []int{35362, 7},
		Nested: [][]string{{"physical_damage", "damage"}, {}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestDecodeListAndNumberKeys(t *testing.T) {
	v := eval(t, `return { affix = "of the Brute", "+(8-12) to Strength", "second line",
		tradeHashes = { [4080418644] = { "+(8-12) to Strength" } } }`)
	var got mod
	if err := Decode(v, &got); err != nil {
		t.Fatal(err)
	}
	if got.Affix != "of the Brute" || !reflect.DeepEqual(got.Lines, []string{"+(8-12) to Strength", "second line"}) {
		t.Errorf("got %+v", got)
	}
	if !reflect.DeepEqual(got.Hashes[4080418644], []string{"+(8-12) to Strength"}) {
		t.Errorf("hashes: %+v", got.Hashes)
	}
}

func TestDecodeMapOfStructs(t *testing.T) {
	v := eval(t, `return { ["Iron Ring"] = { type = "Ring" }, ["Coral Ring"] = { type = "Ring" } }`)
	var got map[string]*base
	if err := Decode(v, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["Coral Ring"].Type != "Ring" {
		t.Errorf("got %+v", got)
	}
}

func TestDecodeErrorsNamePath(t *testing.T) {
	v := eval(t, `return { weapon = { Range = "far" } }`)
	var got base
	err := Decode(v, &got)
	if err == nil || !strings.Contains(err.Error(), "weapon.Range") {
		t.Fatalf("want error naming weapon.Range, got %v", err)
	}
	v = eval(t, `return { ["in"] = { 1.5 } }`)
	if err := Decode(v, &got); err == nil {
		t.Error("fractional number into int must fail")
	}
}
