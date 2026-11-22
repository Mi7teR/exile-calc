package gamedata

import (
	"strings"
	"testing"
)

func TestPoE1Loads(t *testing.T) {
	d, err := PoE1()
	if err != nil {
		t.Fatal(err)
	}
	if d.Version.PoB == "" || d.Version.Tree == "" {
		t.Errorf("version: %+v", d.Version)
	}

	// Lower bounds catch a generator that silently drops data.
	counts := map[string][2]int{
		"bases":         {len(d.Bases), 1000},
		"gems":          {len(d.Gems), 700},
		"uniques":       {len(d.Uniques), 1000},
		"nodes":         {len(d.Tree.Nodes), 3000},
		"explicit mods": {len(d.Mods["Explicit"]), 2000},
	}
	for name, c := range counts {
		if c[0] < c[1] {
			t.Errorf("%s: %d, want at least %d", name, c[0], c[1])
		}
	}

	if b := d.Bases["Coral Ring"]; b == nil || b.Implicit != "+(20-30) to maximum Life" || b.Type != "Ring" {
		t.Errorf("Coral Ring: %+v", b)
	}
	if m := d.Mods["Explicit"]["Strength1"]; m == nil || m.Type != "Suffix" || len(m.Lines) != 1 || !strings.Contains(m.Lines[0], "to Strength") {
		t.Errorf("Strength1: %+v", m)
	}
	if g := d.Gems["Metadata/Items/Gems/SkillGemFireball"]; g == nil || g.Name != "Fireball" || g.ReqInt == 0 {
		t.Errorf("Fireball: %+v", g)
	}
	if u := d.Unique("Andvarius"); u == nil || u.Base != "Gold Ring" {
		t.Errorf("Andvarius: %+v", u)
	}
	if n := d.Tree.Nodes[10016]; n == nil || n.Name != "Executioner" || !n.IsNotable {
		t.Errorf("node 10016: %+v", n)
	}
	if d.Tree.Root == nil || len(d.Tree.Root.Out) == 0 {
		t.Error("tree root missing")
	}
	if len(d.Tree.Classes) < 7 || d.Tree.Classes[0].Name != "Scion" {
		t.Errorf("classes: %d", len(d.Tree.Classes))
	}
}

func TestPoE1IsCached(t *testing.T) {
	a, err := PoE1()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := PoE1()
	if a != b {
		t.Error("PoE1 must return the same loaded data")
	}
}
