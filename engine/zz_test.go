package engine

import (
	"os"
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

func TestZZ(t *testing.T) {
	c, _ := New()
	raw, _ := os.ReadFile("../testdata/golden/builds/vPuXJxFgodF9.code")
	b, _ := pobcode.FromCode(string(raw))
	res, _ := c.Calc(b)
	t.Log(res.Stats["Rage"], res.Stats["MaximumRage"], res.DB.Sum("BASE", "Multiplier:RageStack"), res.DB.Flag("Condition:CanGainRage"))
	for _, s := range res.Skills {
		_ = s
	}
	for _, r := range res.DB.Tabulate("MaximumRage") {
		t.Logf("%+v", r)
	}
}
