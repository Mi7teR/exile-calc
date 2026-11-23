package main

import (
	"fmt"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/internal/luadata"
)

// convertConfigOptions reads Modules/ConfigOptions.lua's varList. Only the
// returned table is evaluated: the local helper functions before it are
// tooltips, which the engine does not need.
func convertConfigOptions(src string) ([]gamedata.ConfigOption, error) {
	i := strings.Index(src, "\nreturn {")
	if i < 0 {
		return nil, fmt.Errorf("ConfigOptions.lua: no top-level return")
	}
	v, err := luadata.Eval(src[i+1:], nil)
	if err != nil {
		return nil, fmt.Errorf("ConfigOptions.lua: %w", err)
	}
	list, ok := v.(*luadata.Table)
	if !ok {
		return nil, fmt.Errorf("ConfigOptions.lua: returns %T", v)
	}
	plain := func(v luadata.Value) any {
		switch x := v.(type) {
		case luadata.Bool:
			return bool(x)
		case luadata.Number:
			return float64(x)
		case luadata.String:
			return string(x)
		}
		return nil
	}
	var out []gamedata.ConfigOption
	for _, e := range list.List {
		t, ok := e.(*luadata.Table)
		if !ok {
			continue
		}
		name, _ := t.Get("var").(luadata.String)
		if name == "" {
			continue // section headers
		}
		typ, _ := t.Get("type").(luadata.String)
		o := gamedata.ConfigOption{
			Var: string(name), Type: string(typ),
			DefaultState:            plain(t.Get("defaultState")),
			DefaultPlaceholderState: plain(t.Get("defaultPlaceholderState")),
		}
		if f, ok := t.Get("apply").(*luadata.Func); ok {
			o.Apply = f.Source
		}
		if idx, ok := t.Get("defaultIndex").(luadata.Number); ok {
			if l, ok := t.Get("list").(*luadata.Table); ok && int(idx) >= 1 && int(idx) <= len(l.List) {
				if item, ok := l.List[int(idx)-1].(*luadata.Table); ok {
					o.DefaultValue = plain(item.Get("val"))
				}
			}
		}
		out = append(out, o)
	}
	return out, nil
}
