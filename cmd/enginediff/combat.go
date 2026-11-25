package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Mi7teR/poe-lib/engine"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// runCombat prints the combat DPS model for the golden builds. It does not
// use the PoB oracle: the numbers are the engine's own model.
func runCombat(ids string) error {
	codes, err := filepath.Glob("testdata/golden/builds/*.code")
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, id := range strings.Split(ids, ",") {
		if id != "" {
			want[id] = true
		}
	}
	found := map[string]bool{}
	for _, c := range codes {
		found[strings.TrimSuffix(filepath.Base(c), ".code")] = true
	}
	var missing []string
	for id := range want {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("no golden build %s", strings.Join(missing, ", "))
	}
	calc, err := engine.New()
	if err != nil {
		return err
	}
	for _, c := range codes {
		name := strings.TrimSuffix(filepath.Base(c), ".code")
		if len(want) > 0 && !want[name] {
			continue
		}
		raw, err := os.ReadFile(c)
		if err != nil {
			return err
		}
		x, err := pobcode.Decode(string(raw))
		if err != nil {
			return err
		}
		b, err := pobcode.Parse(x)
		if err != nil {
			return err
		}
		r, err := calc.Combat(b, engine.CombatOptions{})
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		fmt.Printf("== %s: %s\n", name, r.Skill)
		printCombat("boss", r.Boss)
		printCombat("pack", r.Pack)
		fmt.Println("  assumptions:")
		for _, a := range r.Assumptions {
			fmt.Printf("    - %s\n", a)
		}
	}
	return nil
}

func printCombat(label string, d engine.CombatDPS) {
	fmt.Printf("  %s: PoB CombinedDPS %s  sustained %s  burst %s  hitsPerCast %.2f\n",
		label, human(d.PoB), human(d.Sustained), human(d.Burst), d.HitsPerCast)
	names := make([]string, 0, len(d.Components))
	for k := range d.Components {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Printf("      %-16s %s\n", k, human(d.Components[k]))
	}
}

// human formats a number as 37.3M, 12.4k or 850.
func human(v float64) string {
	switch a := v; {
	case a >= 1e9 || a <= -1e9:
		return fmt.Sprintf("%.1fG", v/1e9)
	case a >= 1e6 || a <= -1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case a >= 1e3 || a <= -1e3:
		return fmt.Sprintf("%.1fk", v/1e3)
	}
	return fmt.Sprintf("%.0f", v)
}
