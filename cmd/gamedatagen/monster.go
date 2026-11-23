package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
)

// convertMonsterTables reads the data.monster<X>Table = { ... } lines of
// Data/Misc.lua.
func convertMonsterTables(src string) (*gamedata.MonsterTables, error) {
	out := &gamedata.MonsterTables{}
	for name, dst := range map[string]*[]float64{
		"monsterEvasionTable": &out.Evasion, "monsterAccuracyTable": &out.Accuracy, "monsterDamageTable": &out.Damage,
		"monsterArmourTable": &out.Armour, "monsterPhysConversionMultiTable": &out.PhysConversionMulti,
		"monsterAllyLifeTable": &out.AllyLife, "monsterLifeTable": &out.Life, "monsterLifeTable2": &out.Life2,
		"monsterLifeTable3": &out.Life3,
	} {
		m := regexp.MustCompile(`(?m)^data\.` + name + ` = \{([^}]*)\}`).FindStringSubmatch(src)
		if m == nil {
			return nil, fmt.Errorf("Misc.lua: no %s", name)
		}
		for _, f := range strings.Split(m[1], ",") {
			if f = strings.TrimSpace(f); f == "" {
				continue
			}
			v, err := strconv.ParseFloat(f, 64)
			if err != nil {
				return nil, fmt.Errorf("Misc.lua %s: %w", name, err)
			}
			*dst = append(*dst, v)
		}
	}
	m := regexp.MustCompile(`(?m)^data\.totemLifeMult = \{([^}]*)\}`).FindStringSubmatch(src)
	if m == nil {
		return nil, fmt.Errorf("Misc.lua: no totemLifeMult")
	}
	out.TotemLifeMult = map[int]float64{}
	for _, e := range regexp.MustCompile(`\[(\d+)\] = ([\d.]+)`).FindAllStringSubmatch(m[1], -1) {
		k, _ := strconv.Atoi(e[1])
		v, _ := strconv.ParseFloat(e[2], 64)
		out.TotemLifeMult[k] = v
	}
	return out, nil
}
