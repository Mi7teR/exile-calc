package modparser

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
)

// data/poe1.json.gz holds the pattern tables of PoB's Modules/ModParser.lua,
// exported by cmd/gamedatagen. Values follow the generator's encoding: a Lua
// table is an array, or an object with its positional entries under "@list";
// a Lua function is {"$hook": name}.
//
//go:embed data/poe1.json.gz
var poe1Data []byte

type entry struct {
	pattern string
	re      *regexp.Regexp // nil for plain tables
	value   any
}

type tables struct {
	pob string
	t   map[string][]entry
	// lookup tables keyed by exact text (resourceTypes, dmgTypes, ...)
	exact map[string]map[string]any
	// patterns that could not be translated (counted, not used)
	skipped []string
}

// Tables looked up by the exact lower-cased line, not scanned.
var exactTables = map[string]bool{"clusterJewelSkills": true, "unsupportedModList": true, "SkillType": true,
	"resourceTypes": true, "regenTypes": true, "degenTypes": true, "dmgTypes": true,
	"gemIdLookup": true, "supportGems": true, "conquerorList": true}

// Which ModParser.lua tables scan() reads with plain=true (substring search).
var plainTables = map[string]bool{
	"modNameList": true, "modFlagList": true, "penTypes": true, "baseCostTypes": true,
	"costTypes": true, "suffixTypes": true,
}

func loadTables(gz []byte) (*tables, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	var raw struct {
		PoB    string                    `json:"pob"`
		Tables map[string]map[string]any `json:"tables"`
	}
	if err := json.NewDecoder(zr).Decode(&raw); err != nil {
		return nil, fmt.Errorf("modparser: data: %w", err)
	}
	t := &tables{pob: raw.PoB, t: map[string][]entry{}, exact: raw.Tables}
	for name, tbl := range raw.Tables {
		if exactTables[name] {
			continue
		}
		keys := make([]string, 0, len(tbl))
		for k := range tbl {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic tie-break where Lua's pairs() is not
		for _, k := range keys {
			e := entry{pattern: k, value: tbl[k]}
			if !plainTables[name] {
				re, err := translateLuaPattern(k)
				if err != nil {
					t.skipped = append(t.skipped, name+": "+k)
					continue
				}
				e.re = re
			}
			t.t[name] = append(t.t[name], e)
		}
	}
	return t, nil
}
