package engine

import (
	"fmt"
	"math"
	"strings"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// Base types with their own "increased effect" stat (CalcPerform.lua calcFlaskMods).
var flaskTypeEffect = map[string]string{
	"Iron Flask": "IronFlaskEffect", "Granite Flask": "GraniteFlaskEffect", "Ruby Flask": "RubyFlaskEffect",
	"Sapphire Flask": "SapphireFlaskEffect", "Topaz Flask": "TopazFlaskEffect",
}

// applyFlasks ports the player part of mergeFlasks (Modules/CalcPerform.lua):
// active flasks add their base buff and their own mods, scaled by flask
// effect; mods with the same parameters from flasks of one base (or the
// same unique / the same mod set) do not stack, the highest wins.
func (c *Calculator) applyFlasks(db *ModDB, res *Result, flasks []*pobcode.Item) {
	effectInc := db.Sum("INC", "FlaskEffect")
	effectIncMagic := db.Sum("INC", "MagicUtilityFlaskEffect")
	merged := map[string][]Mod{}
	var keys []string
	add := func(key string, m Mod) {
		if _, ok := merged[key]; !ok {
			keys = append(keys, key)
		}
		for i, d := range merged[key] {
			if m.Type != "LIST" && sameParams(m, d) {
				if a, ok := num(m.Value); ok {
					if b, ok := num(d.Value); ok && a > b {
						merged[key][i] = m
					}
				}
				return
			}
		}
		merged[key] = append(merged[key], m)
	}
	for _, item := range flasks {
		if item == nil {
			continue
		}
		it, mods := c.itemMods(item, res)
		if it == nil || it.Base == nil || it.Base.Flask == nil {
			continue
		}
		base := it.Base.Flask
		recovery := base.Life > 0 || base.Mana > 0
		db.conditions["UsingFlask"] = true
		db.conditions["Using"+strings.ReplaceAll(it.BaseName, " ", "")] = true
		if base.Life > 0 {
			db.conditions["UsingLifeFlask"] = true
		}
		if base.Mana > 0 {
			db.conditions["UsingManaFlask"] = true
		}

		// Item.lua: flaskData.effectInc from the flask's own mods.
		inc := effectInc
		for _, m := range mods {
			if (m.Name == "FlaskEffect" || m.Name == "LocalEffect") && m.Type == "INC" && len(m.Tags) == 0 {
				if f, ok := num(m.Value); ok {
					inc += f
				}
			}
		}
		if stat := flaskTypeEffect[it.BaseName]; stat != "" {
			inc += db.Sum("INC", stat)
		}
		if it.Rarity == "MAGIC" && !recovery {
			inc += effectIncMagic
		}
		scale := 1 + inc/100
		source := itemSource(item, it)

		for _, line := range base.Buff {
			r := c.parser.Parse(line)
			if !r.Parsed() {
				res.Unparsed = append(res.Unparsed, line)
				continue
			}
			for _, m := range r.Mods {
				add("base:"+it.BaseName, scaleMod(fromParsed(m, source), scale))
			}
		}
		key := it.Name
		if it.Rarity != "UNIQUE" && it.Rarity != "RELIC" {
			var b strings.Builder
			for _, m := range mods {
				fmt.Fprintf(&b, "%s|%s|%v&", m.Name, m.Type, m.Value)
			}
			key = b.String()
		}
		for _, m := range mods {
			add("mods:"+key, scaleMod(fromParsed(m, source), scale))
		}
	}
	for _, k := range keys {
		for _, m := range merged[k] {
			db.Add(m)
		}
	}
}

// scaleMod is ModStoreClass:ScaleAddMod for numeric values without
// high-precision data: modf(round(value * scale, 2)).
func scaleMod(m Mod, scale float64) Mod {
	if scale == 1 {
		return m
	}
	switch v := m.Value.(type) {
	case float64:
		if math.Floor(v) != v {
			m.Value = math.Floor(v*scale*10) / 10 // data.defaultHighPrecision = 1
		} else {
			m.Value = modfInt(round(v*scale, 2))
		}
	case map[string]any:
		if sub, ok := v["mod"].(map[string]any); ok {
			if f, ok := sub["value"].(float64); ok {
				c := copyMap(v)
				s := copyMap(sub)
				s["value"] = modfInt(round(f*scale, 2))
				c["mod"] = s
				m.Value = c
			}
		}
	}
	return m
}

func copyMap(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// sameParams is modLib.compareModParams: name, type, flags, keywordFlags
// and tags equal.
func sameParams(a, b Mod) bool {
	if a.Name != b.Name || a.Type != b.Type || a.Flags != b.Flags || a.KeywordFlags != b.KeywordFlags || len(a.Tags) != len(b.Tags) {
		return false
	}
	return fmt.Sprint(a.Tags) == fmt.Sprint(b.Tags)
}
