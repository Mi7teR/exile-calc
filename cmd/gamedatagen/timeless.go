package main

import (
	"fmt"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/internal/luadata"
)

// convertTimeless exports LegionPassives.lua and NodeIndexMapping.lua.
func convertTimeless(read func(string) (string, error)) (*gamedata.Timeless, error) {
	src, err := read("Data/TimelessJewelData/LegionPassives.lua")
	if err != nil {
		return nil, err
	}
	var legion struct {
		Nodes     []gamedata.LegionNode `lua:"nodes"`
		Additions []gamedata.LegionNode `lua:"additions"`
	}
	if err := evalInto("LegionPassives.lua", src, &legion); err != nil {
		return nil, err
	}
	mapSrc, err := read("Data/TimelessJewelData/NodeIndexMapping.lua")
	if err != nil {
		return nil, err
	}
	g, err := luadata.EvalGlobals(mapSrc, nil)
	if err != nil {
		return nil, fmt.Errorf("NodeIndexMapping.lua: %w", err)
	}
	list, ok := g["nodeIDList"].(*luadata.Table)
	if !ok {
		return nil, fmt.Errorf("NodeIndexMapping.lua: no nodeIDList")
	}
	t := &gamedata.Timeless{
		Nodes: legion.Nodes, Additions: legion.Additions,
		NodeIndex: map[int]int{}, NodeSize: map[int]int{}, LocalToGlobal: map[int]map[int]int{},
	}
	for _, k := range list.Keys {
		switch key := k.(type) {
		case luadata.Number:
			var entry struct {
				Index int `lua:"index"`
				Size  int `lua:"size"`
			}
			if err := luadata.Decode(list.Get(k), &entry); err != nil {
				return nil, fmt.Errorf("NodeIndexMapping.lua [%v]: %w", key, err)
			}
			t.NodeIndex[int(key)] = entry.Index
			t.NodeSize[int(key)] = entry.Size
		case luadata.String:
			switch key {
			case "sizeNotable":
				n, _ := list.Get(k).(luadata.Number)
				t.SizeNotable = int(n)
			case "size":
				n, _ := list.Get(k).(luadata.Number)
				t.Size = int(n)
			case "localIdToGlobalId":
				byType, _ := list.Get(k).(*luadata.Table)
				for _, typ := range byType.Keys {
					m := map[int]int{}
					tbl, _ := byType.Get(typ).(*luadata.Table)
					for _, local := range tbl.Keys {
						if ln, ok := local.(luadata.Number); ok {
							if gn, ok := tbl.Get(local).(luadata.Number); ok {
								m[int(ln)] = int(gn)
							}
						}
					}
					if tn, ok := typ.(luadata.Number); ok {
						t.LocalToGlobal[int(tn)] = m
					}
				}
			}
		}
	}
	return t, nil
}
