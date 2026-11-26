package main

import (
	"fmt"
	"regexp"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/internal/luadata"
)

// convertMapMods reads Data/ModMap.lua's AffixData (data.mapMods,
// Modules/Data.lua:727): each map affix's type, values and apply source.
func convertMapMods(src string) (map[string]*gamedata.MapAffix, error) {
	v, err := luadata.Eval(src, nil)
	if err != nil {
		return nil, fmt.Errorf("ModMap.lua: %w", err)
	}
	root, ok := v.(*luadata.Table)
	if !ok {
		return nil, fmt.Errorf("ModMap.lua: returns %T", v)
	}
	affixes, ok := root.Get("AffixData").(*luadata.Table)
	if !ok {
		return nil, fmt.Errorf("ModMap.lua: no AffixData")
	}
	out := map[string]*gamedata.MapAffix{}
	for _, k := range affixes.Keys {
		name, isStr := k.(luadata.String)
		t, isTable := affixes.Get(k).(*luadata.Table)
		if !isStr || !isTable {
			continue
		}
		a := &gamedata.MapAffix{Values: plainValue(t.Get("values"))}
		if typ, isTyp := t.Get("type").(luadata.String); isTyp {
			a.Type = string(typ)
		}
		if f, isFunc := t.Get("apply").(*luadata.Func); isFunc {
			a.Apply = f.Source
		}
		out[string(name)] = a
	}
	return out, nil
}

// plainValue is a luadata number, string, bool or list (nested) as JSON
// data.
func plainValue(v luadata.Value) any {
	switch x := v.(type) {
	case luadata.Number:
		return float64(x)
	case luadata.String:
		return string(x)
	case luadata.Bool:
		return bool(x)
	case *luadata.Table:
		l := make([]any, len(x.List))
		for i, e := range x.List {
			l[i] = plainValue(e)
		}
		return l
	}
	return nil
}

// mapAffixOption matches the ConfigOptions.lua entries whose apply is
// mapAffixDropDownFunction (Modules/ConfigOptions.lua:116-131).
var mapAffixOption = regexp.MustCompile(`var = "(\w+)".*apply = mapAffixDropDownFunction`)
