package engine

import (
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
)

func TestPobConqueredBy(t *testing.T) {
	start := 0
	radius := conquest{jewelType: 3}
	abyss := conquest{jewelType: 7}
	for _, c := range []struct {
		name string
		node *gamedata.Node
		cq   conquest
		want bool
	}{
		{"normal", &gamedata.Node{}, radius, true},
		{"notable", &gamedata.Node{IsNotable: true}, radius, true},
		{"socket", &gamedata.Node{IsJewelSocket: true}, radius, false},
		{"class start", &gamedata.Node{ClassStartIndex: &start}, radius, false},
		{"ascendancy", &gamedata.Node{AscendancyName: "Guardian"}, radius, false},
		{"abyss socket", &gamedata.Node{IsJewelSocket: true}, abyss, true},
	} {
		if got := pobConqueredBy(c.node, c.cq); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestRadiusJewelSkipsConqueredNodes: jewelOtherFuncs leave nodes a
// timeless jewel conquered alone (ModParser.lua:6683-6697). In
// JtugGaLoWqgT, Unnatural Instinct nulls the allocated small node
// Tree:18033 but not Tree:11688, which Brutal Restraint conquered: that
// keeps its +4 Dexterity.
func TestRadiusJewelSkipsConqueredNodes(t *testing.T) {
	if defaultTimelessDir() == "" {
		t.Skip("timeless jewel tables not found")
	}
	res, err := newCalc(t).Calc(corpusBuild(t, "JtugGaLoWqgT"))
	if err != nil {
		t.Fatal(err)
	}
	sum := func(stat, source string) float64 {
		total := 0.0
		for _, r := range res.DB.Tabulate(stat) {
			if v, ok := r.Value.(float64); ok && r.Source == source {
				total += v
			}
		}
		return total
	}
	if got := sum("Dex", "Tree:11688"); got != 4 {
		t.Errorf("Dex from conquered Tree:11688 = %v, want 4", got)
	}
	if got := sum("Int", "Tree:18033"); got != 0 {
		t.Errorf("Int from nulled Tree:18033 = %v, want 0", got)
	}
}
