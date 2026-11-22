package main

import (
	"reflect"
	"testing"
)

func TestConvertBases(t *testing.T) {
	src := map[string]string{"Data/Bases/ring.lua": `return function(itemBases)
itemBases["Coral Ring"] = {
	baseId = "Metadata/Items/Rings/Ring2",
	type = "Ring",
	tags = { default = true, ring = true, },
	implicit = "+(20-30) to maximum Life",
	implicitModTypes = { { "resource", "life" }, },
	req = { },
}
itemBases["Rusted Sword"] = {
	type = "One Handed Sword", socketLimit = 3,
	tags = { sword = true },
	weapon = { PhysicalMin = 4, PhysicalMax = 9, CritChanceBase = 5, AttackRateBase = 1.55, Range = 11, },
	req = { level = 1, str = 8, dex = 8, },
}
end`}
	bases, err := convertBases(src)
	if err != nil {
		t.Fatal(err)
	}
	ring := bases["Coral Ring"]
	if ring == nil || ring.Name != "Coral Ring" || ring.Implicit != "+(20-30) to maximum Life" || !reflect.DeepEqual(ring.Tags, []string{"default", "ring"}) {
		t.Fatalf("ring: %+v", ring)
	}
	sw := bases["Rusted Sword"]
	if sw.Weapon == nil || sw.Weapon.AttackRateBase != 1.55 || sw.Req.Str != 8 || sw.SocketLimit != 3 {
		t.Errorf("sword: %+v", sw)
	}
}

func TestConvertModsByDomain(t *testing.T) {
	src := map[string]string{
		"Data/ModExplicit.lua": `return {
	["Strength1"] = { type = "Suffix", affix = "of the Brute", "+(8-12) to Strength", statOrder = { 848 }, level = 1, group = "Strength", weightKey = { "ring", "default", }, weightVal = { 1000, 0 }, modTags = { "attribute" }, tradeHashes = { [4080418644] = { "+(8-12) to Strength" }, } },
}`,
		"Data/ModItemExclusive.lua": `return {
	["StrengthUniqueRing2"] = { affix = "", "+(10-20) to Strength", statOrder = { 848 }, level = 1, group = "Strength", weightKey = { }, weightVal = {  }, modTags = { "attribute" } },
}`,
	}
	mods, err := convertMods(src)
	if err != nil {
		t.Fatal(err)
	}
	s1 := mods["Explicit"]["Strength1"]
	if s1.Domain != "Explicit" || s1.Type != "Suffix" || !reflect.DeepEqual(s1.Lines, []string{"+(8-12) to Strength"}) || s1.WeightVal[0] != 1000 {
		t.Errorf("Strength1: %+v", s1)
	}
	if m := mods["ItemExclusive"]["StrengthUniqueRing2"]; m == nil || m.Domain != "ItemExclusive" {
		t.Errorf("domain: %+v", m)
	}

	// The same id in another file is another mod.
	src["Data/ModCorrupted.lua"] = `return { ["Strength1"] = { "x", statOrder = { 418.1 }, level = 1, group = "g" } }`
	mods, err = convertMods(src)
	if err != nil {
		t.Fatal(err)
	}
	if c := mods["Corrupted"]["Strength1"]; c.Lines[0] != "x" || c.StatOrder[0] != 418.1 || mods["Explicit"]["Strength1"].Lines[0] == "x" {
		t.Errorf("namespaces mixed: %+v", c)
	}
}

func TestConvertGemsAndUniques(t *testing.T) {
	gems, err := convertGems(`return {
	["Metadata/Items/Gems/SkillGemFireball"] = {
		name = "Fireball", gameId = "Metadata/Items/Gems/SkillGemFireball", variantId = "Fireball",
		grantedEffectId = "Fireball", tags = { spell = true, fire = true },
		tagString = "Spell, Fire", reqStr = 0, reqDex = 0, reqInt = 100, naturalMaxLevel = 20,
	},
}`)
	if err != nil {
		t.Fatal(err)
	}
	fb := gems["Metadata/Items/Gems/SkillGemFireball"]
	if fb.ID != "Metadata/Items/Gems/SkillGemFireball" || fb.ReqInt != 100 || !reflect.DeepEqual(fb.Tags, []string{"fire", "spell"}) {
		t.Errorf("fireball: %+v", fb)
	}

	uniques, err := convertUniques(map[string]string{"Data/Uniques/ring.lua": `return {
-- Ring
[[
Andvarius
Gold Ring
Implicits: 1
+(6-15)% to Item Rarity
]],[[
Item Class: Rings
Rarity: Unique
Astral Projector
Topaz Ring
]],
}`})
	if err != nil {
		t.Fatal(err)
	}
	if len(uniques) != 2 || uniques[0].Name != "Andvarius" || uniques[0].Base != "Gold Ring" || uniques[0].File != "ring" {
		t.Fatalf("uniques: %+v", uniques)
	}
	if uniques[1].Name != "Astral Projector" || uniques[1].Base != "Topaz Ring" {
		t.Errorf("header lines must be skipped: %+v", uniques[1])
	}
	if uniques[0].Raw != "Andvarius\nGold Ring\nImplicits: 1\n+(6-15)% to Item Rarity" {
		t.Errorf("raw: %q", uniques[0].Raw)
	}
}

func TestConvertTree(t *testing.T) {
	tree, err := convertTree(`return {
	["classes"] = { { ["name"] = "Scion", ["base_str"] = 20, ["base_dex"] = 20, ["base_int"] = 20,
		["ascendancies"] = { { ["id"] = "Ascendant", ["name"] = "Ascendant" } } } },
	["groups"] = { { ["nodes"] = { "10016" }, ["orbits"] = { 0, 3 }, ["x"] = -1096.9, ["y"] = 13399.92 } },
	["jewelSlots"] = { 26725 },
	["nodes"] = {
		[10016] = { ["group"] = 1, ["in"] = { "35362" }, ["out"] = { "35118" }, ["isNotable"] = true,
			["name"] = "Executioner", ["orbit"] = 3, ["orbitIndex"] = 11, ["skill"] = 10016,
			["stats"] = { "60% increased Damage with Hits against Enemies that are on Low Life" } },
		["root"] = { ["out"] = { "10016" }, ["name"] = "Root", ["skill"] = 0 },
		[44683] = { ["name"] = "Seven Years Bad Luck", ["skill"] = 44683, ["classStartIndex"] = 0, ["stats"] = { } },
	},
	["points"] = { ["ascendancyPoints"] = 8, ["totalPoints"] = 123 },
}`)
	if err != nil {
		t.Fatal(err)
	}
	n := tree.Nodes[10016]
	if n == nil || n.ID != 10016 || !n.IsNotable || !reflect.DeepEqual(n.In, []int{35362}) || n.Group != 1 {
		t.Fatalf("node: %+v", n)
	}
	if tree.Groups[0].X != -1096.9 || tree.Classes[0].Ascendancies[0].ID != "Ascendant" || tree.Points.TotalPoints != 123 {
		t.Errorf("tree: %+v", tree)
	}
	if tree.Root == nil || tree.Root.Out[0] != 10016 || len(tree.Nodes) != 2 {
		t.Errorf("root node: %+v, nodes %d", tree.Root, len(tree.Nodes))
	}
	if s := tree.Nodes[44683].ClassStartIndex; s == nil || *s != 0 {
		t.Errorf("classStartIndex 0 must survive: %v", s)
	}
}
