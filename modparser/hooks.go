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
	// ["([%a%s]+) has (%d+)%% increased effect"] = function(_, skill, num)
	//   return { mod("BuffEffect", "INC", num, { type = "SkillId", skillId = gemIdLookup[skill]}) } end
	"specialModList[^([%a%s]+) has (%d+)%% increased effect$]": func(p *Parser, a []any) any {
		tag := map[string]any{"type": "SkillId"}
		if id, ok := p.t.exact["gemIdLookup"][argS(a, 1)]; ok {
			tag["skillId"] = id
		}
		return []any{withTags(newMod("BuffEffect", "INC", arg(a, 2), 0, 0), tag)}
	},
	// ["grants %+(%d+) to maximum life per (%d+)%% quality"] = function(num, _, div) return {
	//   mod("Life", "BASE", num, { type = "Multiplier", var = "QualityOn{SlotName}", div = tonumber(div) }) } end
	// and the other "grants ... per N% quality" lines of ModParser.lua.
	"specialModList[^grants %+(%d+) to maximum life per (%d+)%% quality$]":            perQuality("Life", "BASE"),
	"specialModList[^grants %+(%d+) to maximum mana per (%d+)%% quality$]":            perQuality("Mana", "BASE"),
	"specialModList[^grants %+(%d+) to strength per (%d+)%% quality$]":                perQuality("Str", "BASE"),
	"specialModList[^grants %+(%d+) to dexterity per (%d+)%% quality$]":               perQuality("Dex", "BASE"),
	"specialModList[^grants %+(%d+) to intelligence per (%d+)%% quality$]":            perQuality("Int", "BASE"),
	"specialModList[^grants %+(%d+)%% to fire resistance per (%d+)%% quality$]":       perQuality("FireResist", "BASE"),
	"specialModList[^grants %+(%d+)%% to cold resistance per (%d+)%% quality$]":       perQuality("ColdResist", "BASE"),
	"specialModList[^grants %+(%d+)%% to lightning resistance per (%d+)%% quality$]":  perQuality("LightningResist", "BASE"),
	"specialModList[^grants (%d+)%% increased accuracy per (%d+)%% quality$]":         perQuality("Accuracy", "INC"),
	"specialModList[^grants (%d+)%% increased area of effect per (%d+)%% quality$]":   perQuality("AreaOfEffect", "INC"),
	"specialModList[^grants (%d+)%% increased elemental damage per (%d+)%% quality$]": perQuality("ElementalDamage", "INC"),
	// ["your offerings have (%d+)%% reduced effect on you"] = function(num) return {
	//   mod("ExtraSkillMod", "LIST", { mod = mod("BuffEffectOnPlayer", "INC", -num) },
	//   { type = "SkillName", skillNameList = { "Bone Offering", "Flesh Offering", "Spirit Offering", "Blood Offering" } }) } end
	// and the "increased" form with num.
	"specialModList[^your offerings have (%d+)%% reduced effect on you$]":   offeringEffectOnYou(-1),
	"specialModList[^your offerings have (%d+)%% increased effect on you$]": offeringEffectOnYou(1),
	// ["%+(%d+)%% chance to block spell damage for each (%d+)%% overcapped chance to block attack damage"] = function(num, _, div) return {
	//   mod("SpellBlockChance", "BASE", num, { type = "PerStat", stat = "BlockChanceOverCap", div = tonumber(div) }) } end
	"specialModList[^%+(%d+)%% chance to block spell damage for each (%d+)%% overcapped chance to block attack damage$]": func(p *Parser, a []any) any {
		return []any{withTags(newMod("SpellBlockChance", "BASE", arg(a, 0), 0, 0),
			map[string]any{"type": "PerStat", "stat": "BlockChanceOverCap", "div": toNumber(argS(a, 2))})}
	},
	// ["grants armour equal to (%d+)%% of your reserved mana to you and nearby allies"] = function(num) return {
	//   mod("GrantReservedManaAsAura", "LIST", { mod = mod("Armour", "BASE", num / 100) }) } end
	// and the same for maximum energy shield.
	"specialModList[^grants armour equal to (%d+)%% of your reserved mana to you and nearby allies$]":                reservedManaAura("Armour"),
	"specialModList[^grants maximum energy shield equal to (%d+)%% of your reserved mana to you and nearby allies$]": reservedManaAura("EnergyShield"),
	// ["grants level (%d+) (.+)"] = function(num, _, skill) return grantedExtraSkill(skill, num) end
	"specialModList[^grants level (%d+) (.+)$]": func(p *Parser, a []any) any {
		return p.grantedExtraSkill(argS(a, 2), arg(a, 0), nil)
	},
	// ["(%d+)%% more maximum life if you have at least (%d+) life masteries allocated"] = function(num, _, thresh) return {
	//   mod("Life", "MORE", num, { type = "MultiplierThreshold", var = "AllocatedLifeMastery", threshold = tonumber(thresh) }) } end
	"specialModList[^(%d+)%% more maximum life if you have at least (%d+) life masteries allocated$]": func(p *Parser, a []any) any {
		return []any{withTags(newMod("Life", "MORE", arg(a, 0), 0, 0),
			map[string]any{"type": "MultiplierThreshold", "var": "AllocatedLifeMastery", "threshold": toNumber(argS(a, 2))})}
	},
	// ["(%d+)%% increased effect of jewel socket passive skills containing corrupted (m?r?ag?r?i?e?c?) jewels, if not from cluster jewels"] =
	//   function(num, _, rarity) return { mod("JewelData", "LIST", { key = "corrupted" .. firstToUpper(rarity) .. "JewelIncEffect", value = num }) } end
	"specialModList[^(%d+)%% increased effect of jewel socket passive skills containing corrupted (m?r?ag?r?i?e?c?) jewels, if not from cluster jewels$]": corruptedJewelEffect,
	// ["(%d+)%% increased effect of jewel socket passive skills containing corrupted (m?r?ag?r?i?e?c?) jewels"] = (same function)
	"specialModList[^(%d+)%% increased effect of jewel socket passive skills containing corrupted (m?r?ag?r?i?e?c?) jewels$]": corruptedJewelEffect,
	// ["while holding a (%w+)"] = function (_, gear) return {
	//   tag = { type = "Condition", varList = { "Using"..firstToUpper(gear) } } } end
	"modTagList[while holding a (%w+)]": func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "Condition", "varList": []any{"Using" + firstToUpper(argS(a, 1))}}}
	},
	// ["while holding a (%w+) or (%w+)"] = function (_, g1, g2) return {
	//   tag = { type = "Condition", varList = { "Using"..firstToUpper(g1), "Using"..firstToUpper(g2) } } } end
	"modTagList[while holding a (%w+) or (%w+)]": func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "Condition", "varList": []any{"Using" + firstToUpper(argS(a, 1)), "Using" + firstToUpper(argS(a, 2))}}}
	},
	// ["if (%d+) (%a+) items are equipped"] = function(num, _, effectName) return {
	//   tag = { type = "MultiplierThreshold", var = firstToUpper(effectName) .. "Item", threshold = num} } end
	"modTagList[if (%d+) (%a+) items are equipped]": func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "MultiplierThreshold", "var": firstToUpper(argS(a, 2)) + "Item", "threshold": arg(a, 0)}}
	},
}

func corruptedJewelEffect(p *Parser, a []any) any {
	return []any{newMod("JewelData", "LIST", map[string]any{"key": "corrupted" + firstToUpper(argS(a, 2)) + "JewelIncEffect", "value": arg(a, 0)}, 0, 0)}
}

func reservedManaAura(name string) Hook {
	return func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return []any{newMod("GrantReservedManaAsAura", "LIST", map[string]any{"mod": newMod(name, "BASE", n/100, 0, 0)}, 0, 0)}
	}
}

func offeringEffectOnYou(sign float64) Hook {
	return func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return []any{withTags(newMod("ExtraSkillMod", "LIST", map[string]any{"mod": newMod("BuffEffectOnPlayer", "INC", sign*n, 0, 0)}, 0, 0),
			map[string]any{"type": "SkillName", "skillNameList": []any{"Bone Offering", "Flesh Offering", "Spirit Offering", "Blood Offering"}})}
	}
}

// perQuality builds the hooks of the "grants ... per N% quality" lines.
func perQuality(name, typ string) Hook {
	return func(p *Parser, a []any) any {
		return []any{withTags(newMod(name, typ, arg(a, 0), 0, 0),
			map[string]any{"type": "Multiplier", "var": "QualityOn{SlotName}", "div": toNumber(argS(a, 2))})}
	}
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
	// A table value that names a local helper function of ModParser.lua
	// (["bathed in the blood of ..."] = timelessJewel) is called like a hook.
	if name, ok := m["$ref"].(string); ok {
		if h, ok := helpers[name]; ok {
			return h(p, args), true
		}
	}
	return v, true
}

// helpers are Go ports of ModParser.lua's local functions that tables
// reference directly.
var helpers = map[string]Hook{
	// local function timelessJewel(num, _, name)
	//   local conqueror = conquerorList[name:lower()]
	//   if not conqueror then return nil end
	//   return { mod("JewelData", "LIST", { key = "conqueredBy", value = { id = num, conqueror = conqueror } }) }
	"timelessJewel": func(p *Parser, a []any) any {
		conqueror, ok := p.t.exact["conquerorList"][strings.ToLower(argS(a, 2))]
		if !ok {
			return nil
		}
		return []any{newMod("JewelData", "LIST", map[string]any{
			"key": "conqueredBy", "value": map[string]any{"id": arg(a, 0), "conqueror": deepCopy(conqueror)},
		}, 0, 0)}
	},
}

func isHelperRef(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	name, _ := m["$ref"].(string)
	_, known := helpers[name]
	return known
}

func isFunc(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	_, t := m["$template"]
	_, h := m["$hook"]
	return t || h || isHelperRef(v)
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
