package engine

import (
	"os"
	"strings"
	"testing"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// A Calcs tab main group that is a stale granted group: the granted skill
// block removes it and appends the group that grants it again, so the
// main skill's group index is past the input build's groups. The export
// keeps the *Calcs selections as they are instead of indexing out of
// range, and a repeated call gives the same result.
func TestPartyExportStaleGrantedMain(t *testing.T) {
	c := newCalc(t)
	ents, err := os.ReadDir("../testdata/golden/builds")
	if err != nil {
		t.Fatal(err)
	}
	tried := 0
	for _, e := range ents {
		id := strings.TrimSuffix(e.Name(), ".code")
		b := corpusBuild(t, id)
		set := b.ActiveSkills()
		if set == nil {
			continue
		}
		gi := -1
		for i, g := range set.Groups {
			if strings.HasPrefix(g.Source, "Item:") && len(g.Gems) > 0 {
				gi = i
			}
		}
		if gi < 0 {
			continue
		}
		g := set.Groups[gi]
		g.Source = "Item:99999:Nothing, Weapon 1"
		set.Groups = append(append(set.Groups[:gi:gi], set.Groups[gi+1:]...), g)
		b.CalcsInputs = append(b.CalcsInputs,
			pobcode.Input{Name: "skill_number", Kind: pobcode.KindNumber, Number: float64(len(set.Groups))})
		if b.Party == nil {
			b.Party = &pobcode.Party{}
		}
		b.Party.ExportBuffs = true
		res, cerr := c.Calc(b)
		if cerr != nil {
			continue
		}
		tried++
		first, perr := res.PartyExportErr("Aura")
		if perr != nil {
			t.Errorf("%s: %v", id, perr)
		}
		if again := res.PartyExport("Aura"); again != first {
			t.Errorf("%s: second call %q, first %q", id, again, first)
		}
		if tried >= 6 {
			break
		}
	}
	if tried == 0 {
		t.Fatal("no build with a granted group")
	}
}

// A panic in the export pass is an error on every call, not an empty
// cache that looks like a build with nothing to export.
func TestPartyExportPanicIsError(t *testing.T) {
	p := &partyExportCache{calc: nil, build: &pobcode.Build{}}
	r := &Result{party: p}
	for range 2 {
		if s, err := r.PartyExportErr("Aura"); err == nil || s != "" {
			t.Fatalf("export %q err %v", s, err)
		}
	}
}
