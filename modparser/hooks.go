package modparser

import (
	"regexp"
	"strings"
)

// Hook is a Go port of one Lua function from ModParser.lua's tables. It
// receives the arguments ModParser.lua passes (numbers as float64 or nil,
// captures as lower-case strings) and returns what the Lua function returns
// in the generic encoding: a mod list for specialModList, a tag table for
// modTagList. Returning nil means the Lua function returned nil.
type Hook func(p *Parser, args []any) any

// hooks maps the generator's hook names to their ports. The name is the
// table and the key in ModParser.lua; each port quotes its Lua source.
// Unported hooks make a line unsupported, never wrong.
var hooks = map[string]Hook{
	// ["socketed [%a+]* ?gems a?r?e? ?supported by level (%d+) (.+)"] =
	//   function(num, _, support) return extraSupport(support, num) end
	"specialModList[^socketed [%a+]* ?gems a?r?e? ?supported by level (%d+) (.+)$]": func(p *Parser, a []any) any {
		return p.extraSupport(argS(a, 2), arg(a, 0), nil, nil)
	},
	// ["skills socketed in your (.+) are supported by level (%d+) (.+)"] =
	//   function(_, slot, level, support) return extraSupport(support, level, slot, true) end
	"specialModList[^skills socketed in your (.+) are supported by level (%d+) (.+)$]": func(p *Parser, a []any) any {
		slot := argS(a, 1)
		return p.extraSupport(argS(a, 3), arg(a, 2), &slot, true)
	},
	// ["skills granted by your passive tree are supported by level (%d+) (.+)"] =
	//   function(_, level, support) return extraSupport(support, level, "passive tree", true) end
	"specialModList[^skills granted by your passive tree are supported by level (%d+) (.+)$]": func(p *Parser, a []any) any {
		slot := "passive tree"
		return p.extraSupport(argS(a, 2), arg(a, 1), &slot, true)
	},
	// ["([%+%-]%d+)%%? to (%a+) of all (.+) gems"] = function(num, _, property, skill)
	//   if gemIdLookup[skill] then return { mod("GemProperty", "LIST", {keyword = skill, key = "level", value = num, keyOfScaledMod = "value" }) } end
	//   local wordList = {} for tag in skill:gmatch("%S+") do table.insert(wordList, tag) end
	//   return { mod("GemProperty", "LIST", {keywordList = wordList, key = property, value = num, keyOfScaledMod = "value" }) }
	"specialModList[^([%+%-]%d+)%%? to (%a+) of all (.+) gems$]": func(p *Parser, a []any) any {
		skill := argS(a, 3)
		if _, ok := p.t.exact["gemIdLookup"][skill]; ok {
			return []any{newMod("GemProperty", "LIST", map[string]any{"keyword": skill, "key": "level", "value": arg(a, 0), "keyOfScaledMod": "value"}, 0, 0)}
		}
		var words []any
		for _, w := range strings.Fields(skill) {
			words = append(words, w)
		}
		return []any{newMod("GemProperty", "LIST", map[string]any{"keywordList": words, "key": argS(a, 2), "value": arg(a, 0), "keyOfScaledMod": "value"}, 0, 0)}
	},
	// ["([%+%-]%d+)%%? to (%a+) of socketed ?([%a%- ]*) gems"] = function(num, _, property, type)
	//   if type == "" then type = "all" end
	//   return { mod("GemProperty", "LIST", { keyword = type, key = property, value = num }, { type = "SocketedIn", slotName = "{SlotName}" }) }
	"specialModList[^([%+%-]%d+)%%? to (%a+) of socketed ?([%a%- ]*) gems$]": func(p *Parser, a []any) any {
		typ := argS(a, 3)
		if typ == "" {
			typ = "all"
		}
		return []any{withTags(newMod("GemProperty", "LIST", map[string]any{"keyword": typ, "key": argS(a, 2), "value": arg(a, 0)}, 0, 0),
			map[string]any{"type": "SocketedIn", "slotName": "{SlotName}"})}
	},
	// ["grants level (%d+) (.+)"] = function(num, _, skill) return grantedExtraSkill(skill, num) end
	"specialModList[^grants level (%d+) (.+)$]": func(p *Parser, a []any) any {
		return p.grantedExtraSkill(argS(a, 2), arg(a, 0), nil)
	},
	// ["while holding a (%w+)"] = function (_, gear) return {
	//   tag = { type = "Condition", varList = { "Using"..firstToUpper(gear) } } } end
	"modTagList[while holding a (%w+)]": func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "Condition", "varList": []any{"Using" + firstToUpper(argS(a, 1))}}}
	},
	// ["if (%d+) (%a+) items are equipped"] = function(num, _, effectName) return {
	//   tag = { type = "MultiplierThreshold", var = firstToUpper(effectName) .. "Item", threshold = num} } end
	"modTagList[if (%d+) (%a+) items are equipped]": func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "MultiplierThreshold", "var": firstToUpper(argS(a, 2)) + "Item", "threshold": arg(a, 0)}}
	},
}

func arg(a []any, i int) any {
	if i < len(a) {
		return a[i]
	}
	return nil
}

func argS(a []any, i int) string {
	s, _ := arg(a, i).(string)
	return s
}

// firstToUpper is ModParser.lua's str:gsub("^%l", string.upper).
func firstToUpper(s string) string {
	if s != "" && s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-32) + s[1:]
	}
	return s
}

// toNumber is tonumber(v): numbers pass through, numeric strings convert.
func toNumber(v any) any {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		if f, ok := luaNumber(x); ok {
			return f
		}
	}
	return nil
}

var slotWordRe = regexp.MustCompile(`[^0-9A-Za-z][a-z]`)

// extraSupport ports ModParser.lua extraSupport(name, level, slot, appliesToGrantedSkills).
func (p *Parser) extraSupport(name string, level any, slot *string, applies any) any {
	lookup := p.t.exact["gemIdLookup"]
	skillID, _ := lookup[name].(string)
	if skillID == "" {
		skillID, _ = lookup[strings.TrimPrefix(name, "increased ")].(string)
	}
	if skillID == "" {
		skillID, _ = lookup[strings.TrimSuffix(name, " support")].(string)
	}
	slotName := "{SlotName}"
	if slot != nil {
		switch *slot {
		case "main hand", "main hand weapon":
			slotName = "Weapon 1"
		case "off hand", "off hand weapon":
			slotName = "Weapon 2"
		default:
			// string.gsub(" "..slot, "%W%l", string.upper):sub(2)
			slotName = slotWordRe.ReplaceAllStringFunc(" "+*slot, strings.ToUpper)[1:]
		}
	}
	if skillID == "" {
		return nil
	}
	lvl := toNumber(level)
	support := func(id string) map[string]any {
		v := map[string]any{"skillId": id, "level": lvl}
		if applies != nil {
			v["appliesToGrantedSkills"] = applies
		}
		return withTags(newMod("ExtraSupport", "LIST", v, 0, 0), map[string]any{"type": "SocketedIn", "slotName": slotName})
	}
	gem, ok := p.t.exact["supportGems"][skillID].(map[string]any)
	if !ok {
		return []any{support(skillID)}
	}
	mods := []any{support(gem["grantedEffectId"].(string))}
	if sec, ok := gem["secondaryGrantedEffectId"].(string); ok {
		if isSupport, _ := gem["secondaryIsSupport"].(bool); isSupport {
			mods = append(mods, support(sec))
		} else {
			mods = append(mods, newMod("ExtraSkill", "LIST", map[string]any{"skillId": sec, "level": lvl}, 0, 0))
		}
	}
	return mods
}

// grantedExtraSkill ports ModParser.lua grantedExtraSkill(name, level, noSupports).
func (p *Parser) grantedExtraSkill(name string, level, noSupports any) any {
	name = strings.ReplaceAll(name, " skill", "")
	id, ok := p.t.exact["gemIdLookup"][name].(string)
	if !ok {
		return nil
	}
	v := map[string]any{"skillId": id, "level": toNumber(level)}
	if noSupports != nil {
		v["noSupports"] = noSupports
	}
	return []any{newMod("ExtraSkill", "LIST", v, 0, 0)}
}

// call evaluates a table value that came from a Lua function: a template
// gets its arguments substituted, a hook runs its Go port. Plain data is
// returned as is. ok is false for a hook without a port.
func (p *Parser) call(v any, args []any) (any, bool) {
	m, isMap := v.(map[string]any)
	if !isMap {
		return v, true
	}
	if t, ok := m["$template"]; ok {
		return substitute(t, args), true
	}
	if name, ok := m["$hook"].(string); ok {
		h, ok := hooks[name]
		if !ok {
			return nil, false
		}
		return h(p, args), true
	}
	return v, true
}

func isFunc(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	_, t := m["$template"]
	_, h := m["$hook"]
	return t || h
}

// substitute copies a template, replacing {"$arg": n} with args[n-1].
func substitute(v any, args []any) any {
	switch x := v.(type) {
	case map[string]any:
		if n, ok := x["$arg"].(float64); ok && len(x) == 1 {
			if i := int(n) - 1; i >= 0 && i < len(args) {
				return args[i]
			}
			return nil
		}
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = substitute(e, args)
		}
		return m
	case []any:
		l := make([]any, len(x))
		for i, e := range x {
			l[i] = substitute(e, args)
		}
		return l
	}
	return v
}

// unresolved reports values PoB fills in at run time from data we do not
// export ({"$ref": ...}, {"$call": ...}); such lines are unsupported.
func unresolved(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["$ref"]; ok {
			return true
		}
		if _, ok := x["$call"]; ok {
			return true
		}
		if _, ok := x["$hook"]; ok {
			return true
		}
		for _, e := range x {
			if unresolved(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if unresolved(e) {
				return true
			}
		}
	}
	return false
}
