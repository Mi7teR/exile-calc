package engine

import "testing"

// Build.lua copies the Build element's bandit into configTab.input, but
// ConfigTab:Load replaces it with the config set's input: with a Config
// section the attribute alone has no effect, without one it counts.
func TestBanditAttributeNeedsNoConfig(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "3ryNFCmR6SLS")
	base, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	b.Bandit = "Oak"
	attr, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if attr.Stats["Life"] != base.Stats["Life"] {
		t.Errorf("Oak attribute with a config set: Life %v, want %v", attr.Stats["Life"], base.Stats["Life"])
	}
	b.ConfigSets = nil
	b.ActiveConfigSetID = 0
	legacy, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := legacy.DB.Sum("BASE", "Life"); got <= base.DB.Sum("BASE", "Life") {
		t.Errorf("Oak attribute without config: base Life %v, want more than %v", got, base.DB.Sum("BASE", "Life"))
	}
}
