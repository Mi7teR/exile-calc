package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

func TestZZMinionDebug(t *testing.T) {
	name := os.Getenv("ZZ_BUILD")
	if name == "" {
		t.Skip()
	}
	raw, _ := os.ReadFile("../testdata/golden/builds/" + name + ".code")
	b, err := pobcode.FromCode(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	calc, _ := New()
	res, err := calc.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.DB.List(nil, "ExtraAura") {
		t.Logf("player ExtraAura %v src=%s tags=%v", e.Value, e.Mod.Source, e.Mod.Tags)
	}
	for _, m := range res.DB.mods["MinionModifier"] {
		if strings.Contains(m.Source, os.Getenv("ZZ_SRC")) {
			t.Logf("raw %v src=%s tags=%v", m.Value, m.Source, m.Tags)
		}
	}
}
