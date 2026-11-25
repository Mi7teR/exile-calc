package engine

import (
	"strings"

	"github.com/Mi7teR/poe-lib/itemtext"
)

// itemTags holds Data.lua's itemTagSpecial and itemTagSpecialExclusionPattern:
// substring -> slot -> patterns.
type itemTags struct {
	special, exclusion map[string]any
}

func (t itemTags) list(table map[string]any, substring, slot string) []string {
	bySlot, _ := table[substring].(map[string]any)
	l, _ := bySlot[slot].([]any)
	var out []string
	for _, v := range l {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// luaFind is s:find(p) for the patterns these tables use: plain text with
// optional ^ and $ anchors (checked by TestItemTagPatterns).
func luaFind(s, p string) bool {
	start := strings.HasPrefix(p, "^")
	end := strings.HasSuffix(p, "$") && !strings.HasSuffix(p, "%$")
	p = strings.TrimSuffix(strings.TrimPrefix(p, "^"), "$")
	switch {
	case start && end:
		return s == p
	case start:
		return strings.HasPrefix(s, p)
	case end:
		return strings.HasSuffix(s, p)
	}
	return strings.Contains(s, p)
}

func variantLines(it *itemtext.Item, lines []itemtext.ModLine) []itemtext.ModLine {
	var out []itemtext.ModLine
	for _, l := range lines {
		if l.Disabled {
			continue
		}
		if it.LineActive(l) {
			out = append(out, l)
		}
	}
	return out
}

// findModifierSubstring ports ItemClass:FindModifierSubstring (Item.lua).
func (t itemTags) findModifierSubstring(it *itemtext.Item, substring, slot string) bool {
	explicitOnly := strings.Contains(substring, "explicit ")
	substring = strings.ReplaceAll(substring, "explicit ", "")
	lines := variantLines(it, it.Explicits)
	if !explicitOnly {
		lines = append(lines, variantLines(it, it.Enchants)...)
		lines = append(lines, variantLines(it, it.Scourge)...)
		lines = append(lines, variantLines(it, it.Implicits)...)
		lines = append(lines, variantLines(it, it.Crucible)...)
	}
	for _, l := range lines {
		line := strings.ToLower(l.Text)
		if luaFind(line, substring) && !luaFind(line, substring+" modifier") {
			excluded := false
			for _, p := range t.list(t.exclusion, substring, slot) {
				if luaFind(line, strings.ToLower(p)) {
					excluded = true
					break
				}
			}
			if !excluded {
				return true
			}
		}
		for _, p := range t.list(t.special, substring, slot) {
			if luaFind(line, strings.ToLower(p)) {
				return true
			}
		}
	}
	return false
}

// titleSlot is itemSlot:lower():gsub("(%l)(%w*)", upper first letter), trimmed.
func titleSlot(s string) string {
	w := strings.Fields(strings.ToLower(s))
	for i, x := range w {
		if x[0] >= 'a' && x[0] <= 'z' {
			w[i] = string(x[0]-32) + x[1:]
		}
	}
	return strings.Join(w, " ")
}

func isKalandra(it *itemtext.Item) bool {
	return it != nil && strings.Contains(it.Name, "Kalandra's Touch")
}

// itemCondition ports the ItemCondition branch of ModStoreClass:EvalMod.
// db.items is the actor's itemList; cfg.item is not ported (no skill cfg).
func (db *ModDB) itemCondition(t map[string]any) bool {
	slot := titleSlot(tagStr(t, "itemSlot"))
	items := map[string]*itemtext.Item{}
	switch {
	case tagBool(t, "allSlots"):
		for k, v := range db.items {
			items[k] = v
		}
	case tagBool(t, "bothSlots"):
		i1, i2 := db.items[slot+" 1"], db.items[slot+" 2"]
		if isKalandra(i1) {
			i1 = i2
		}
		if isKalandra(i2) {
			i2 = i1
		}
		if i1 != nil && i2 != nil {
			items[slot+" 1"], items[slot+" 2"] = i1, i2
		}
	default:
		it := db.items[slot]
		if isKalandra(it) {
			other := slot
			if strings.HasSuffix(slot, "1") {
				other = slot[:len(slot)-1] + "2"
			} else if strings.HasSuffix(slot, "2") {
				other = slot[:len(slot)-1] + "1"
			}
			it = db.items[other]
		}
		if it != nil {
			items[slot] = it
		}
	}
	var matches []bool
	if sc := tagStr(t, "searchCond"); sc != "" {
		for s, it := range items {
			typ := ""
			if it.Base != nil {
				typ = it.Base.Type
			}
			if (!tagBool(t, "allSlots") || (typ != "Jewel" && typ != "Graft")) && s != slot || !tagBool(t, "excludeSelf") {
				matches = append(matches, db.tags.findModifierSubstring(it, strings.ToLower(sc), strings.ToLower(s)))
			}
		}
	}
	if rc := tagStr(t, "rarityCond"); rc != "" {
		for _, it := range items {
			matches = append(matches, string(it.Rarity) == rc)
		}
	}
	if _, ok := t["corruptedCond"]; ok {
		for _, it := range items {
			matches = append(matches, it.Corrupted == tagBool(t, "corruptedCond"))
		}
	}
	if _, ok := t["shaperCond"]; ok {
		for _, it := range items {
			matches = append(matches, hasInfluence(it, "shaper") == tagBool(t, "shaperCond"))
		}
	}
	if _, ok := t["elderCond"]; ok {
		for _, it := range items {
			matches = append(matches, hasInfluence(it, "elder") == tagBool(t, "elderCond"))
		}
	}
	if nc := tagStr(t, "nameCond"); nc != "" {
		for _, it := range items {
			// Item.lua: item.name is "<title>, <base>" for named items.
			name := it.Name
			if it.BaseName != "" && it.Name != "" && it.Name != it.BaseName {
				name = it.Name + ", " + it.BaseName
			}
			matches = append(matches, strings.EqualFold(name, nc))
		}
	}
	neg := tagBool(t, "neg")
	for _, b := range matches {
		if b == neg {
			return false
		}
	}
	return len(items) > 0 || neg
}
