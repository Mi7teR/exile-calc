package main

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/internal/luadata"
)

// files maps a path relative to PoB's src ("Data/Bases/ring.lua") to its source.

func sortedKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func evalInto(name, src string, out any) error {
	v, err := luadata.Eval(src, nil)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if err := luadata.Decode(v, out); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func convertBases(files map[string]string) (map[string]*gamedata.Base, error) {
	all := map[string]*gamedata.Base{}
	for _, name := range sortedKeys(files) {
		var bases map[string]*gamedata.Base
		if err := evalInto(name, files[name], &bases); err != nil {
			return nil, err
		}
		for n, b := range bases {
			if _, dup := all[n]; dup {
				return nil, fmt.Errorf("%s: base %q defined twice", name, n)
			}
			b.Name = n
			all[n] = b
		}
	}
	return all, nil
}

// modDomain turns "Data/ModItemExclusive.lua" into "ItemExclusive".
func modDomain(name string) string {
	return strings.TrimSuffix(strings.TrimPrefix(path.Base(name), "Mod"), ".lua")
}

// convertMods returns mods by domain, then id. Each PoB mod file is its own
// namespace (the same id appears in ModDelve and ModJewel, for example).
func convertMods(files map[string]string) (map[string]map[string]*gamedata.Mod, error) {
	all := map[string]map[string]*gamedata.Mod{}
	for _, name := range sortedKeys(files) {
		var mods map[string]*gamedata.Mod
		if err := evalInto(name, files[name], &mods); err != nil {
			return nil, err
		}
		domain := modDomain(name)
		for id, m := range mods {
			m.ID, m.Domain = id, domain
		}
		all[domain] = mods
	}
	return all, nil
}

func convertGems(src string) (map[string]*gamedata.Gem, error) {
	var gems map[string]*gamedata.Gem
	if err := evalInto("Data/Gems.lua", src, &gems); err != nil {
		return nil, err
	}
	for id, g := range gems {
		g.ID = id
	}
	return gems, nil
}

func convertUniques(files map[string]string) ([]*gamedata.Unique, error) {
	var out []*gamedata.Unique
	for _, name := range sortedKeys(files) {
		var texts []string
		if err := evalInto(name, files[name], &texts); err != nil {
			return nil, err
		}
		file := strings.TrimSuffix(path.Base(name), ".lua")
		for i, text := range texts {
			var lines []string
			for _, l := range strings.Split(text, "\n") {
				if l = strings.TrimSpace(l); l != "" {
					lines = append(lines, l)
				}
			}
			// A few entries are written as game text with these header lines.
			for len(lines) > 0 && (strings.HasPrefix(lines[0], "Item Class:") || strings.HasPrefix(lines[0], "Rarity:")) {
				lines = lines[1:]
			}
			if len(lines) < 2 {
				return nil, fmt.Errorf("%s: unique #%d has no name and base", name, i+1)
			}
			out = append(out, &gamedata.Unique{
				Name: lines[0], Base: lines[1], File: file, Raw: strings.Join(lines, "\n"),
			})
		}
	}
	return out, nil
}

func convertTree(src string) (*gamedata.Tree, error) {
	v, err := luadata.Eval(src, nil)
	if err != nil {
		return nil, fmt.Errorf("tree.lua: %w", err)
	}
	var tree gamedata.Tree
	var raw struct {
		Nodes map[string]*gamedata.Node `lua:"nodes"`
	}
	if err := luadata.Decode(v, &tree); err != nil {
		return nil, fmt.Errorf("tree.lua: %w", err)
	}
	if err := luadata.Decode(v, &raw); err != nil {
		return nil, fmt.Errorf("tree.lua: %w", err)
	}
	tree.Nodes = make(map[int]*gamedata.Node, len(raw.Nodes))
	for k, n := range raw.Nodes {
		if k == "root" {
			tree.Root = n
			continue
		}
		id, err := strconv.Atoi(k)
		if err != nil {
			return nil, fmt.Errorf("tree.lua: node key %q is not a number", k)
		}
		// PassiveTree.lua "hide legacy alternate ascendancies that are no
		// longer obtainable": PoB deletes the nodes of these bloodlines
		// from the tree, so a spec that lists them loses them.
		if legacyAlternateAscendancies[n.AscendancyName] {
			continue
		}
		tree.Nodes[id] = n
	}
	return &tree, nil
}

// legacyAlternateAscendancies is PassiveTree.lua's
// legacyAlternateAscendancyIds.
var legacyAlternateAscendancies = map[string]bool{"Warden": true, "Warlock": true, "Primalist": true}
