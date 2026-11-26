package modparser

// Helpers shared by the themed hook files (hooks_*.go). Each of those files
// registers its specialModList ports from an init function, keyed by the
// Lua pattern as written in ModParser.lua.

// cap5 is the fifth capture's position in a hook's arguments (see cap1).
const cap5 = 5

// ModFlag, KeywordFlag and SkillType values (all in
// Data/Global.lua) used by the themed hooks.
const (
	modFlagAttack     = 0x1
	modFlagHit        = 0x4
	modFlagCast       = 0x10
	kwFlagPhysical    = 0x10
	kwFlagTrap        = 0x1000
	kwFlagMine        = 0x2000
	kwFlagHit         = 0x40000
	kwFlagAilment     = 0x80000
	kwFlagBleed       = 0x400000
	skillTypeAttack   = 1.0
	skillTypeProj     = 3.0
	skillTypeVaal     = 38.0
	skillTypeAura     = 39.0
	skillTypeChannel  = 48.0
	skillTypeTravel   = 79.0
	skillTypeHex      = 97.0
	skillTypeRandElem = 102.0
)

// registerSpecial adds ports of specialModList functions, keyed by their
// Lua pattern. A pattern ported twice is a programming error.
func registerSpecial(m map[string]Hook) {
	for pattern, h := range m {
		key := "specialModList[^" + pattern + "$]"
		if _, dup := hooks[key]; dup {
			panic("modparser: hook ported twice: " + key)
		}
		hooks[key] = h
	}
}

// argF is tonumber(a[i]) as a float64 (0 when it is not a number).
func argF(a []any, i int) float64 {
	f, _ := toNumber(arg(a, i)).(float64)
	return f
}

// flagMod is ModParser.lua's flag(name, ...): a FLAG mod with value true.
func flagMod(name string, tags ...any) map[string]any {
	return withTags(newMod(name, "FLAG", true, 0, 0), tags...)
}

// sourced sets the mod's source, mod(name, type, value, source, ...).
func sourced(m map[string]any, source string) map[string]any {
	m["source"] = source
	return m
}

// itemsEquipped is { type = "MultiplierThreshold", var = firstToUpper(influence) .. "Item",
// threshold = tonumber(thresh) }, the tag of the "if N <influence> items are
// equipped" lines.
func itemsEquipped(thresh, influence string) map[string]any {
	return map[string]any{
		"type":      "MultiplierThreshold",
		"var":       firstToUpper(influence) + "Item",
		"threshold": toNumber(thresh),
	}
}

// perStat is { type = "PerStat", stat = stat, div = div }.
func perStat(stat string, div any) map[string]any {
	return map[string]any{"type": "PerStat", "stat": stat, "div": div}
}

// skillTypeTag is { type = "SkillType", skillType = t }.
func skillTypeTag(t float64) map[string]any {
	return map[string]any{"type": "SkillType", "skillType": t}
}

// thresholdTag is { type = "MultiplierThreshold", var = v, threshold = n }.
func thresholdTag(v string, n any) map[string]any {
	return map[string]any{"type": "MultiplierThreshold", "var": v, "threshold": n}
}

// playerTag is { type = "ActorCondition", actor = "player" }.
func playerTag() map[string]any {
	return map[string]any{"type": "ActorCondition", "actor": "player"}
}

// modifierOf wraps a mod for another actor: mod(list, "LIST", { mod = m }, tags...).
func modifierOf(list string, m map[string]any, tags ...any) map[string]any {
	return withTags(newMod(list, "LIST", map[string]any{"mod": m}, 0, 0), tags...)
}

// nonUniqueJewelTags are the ItemCondition tags of the "Non-Unique Jewels
// cause ..." lines (ModParser.lua:2664, 2671): the jewel is neither unique
// nor relic.
func nonUniqueJewelTags() []any {
	return []any{
		map[string]any{"type": tagItemCondition, keyItemSlot: "{SlotName}", "rarityCond": "UNIQUE", "neg": true},
		map[string]any{"type": tagItemCondition, keyItemSlot: "{SlotName}", "rarityCond": "RELIC", "neg": true},
	}
}

// Tag type and keys of ModParser.lua's ItemCondition and SkillName tags.
const (
	tagItemCondition       = "ItemCondition"
	keyItemSlot            = "itemSlot"
	keyIncludeTransfigured = "includeTransfigured"
)

func init() {
	registerSpecial(map[string]Hook{
		// ModParser.lua:2658 ["non%-unique jewels cause increases and reductions to other damage types in a (%a+) radius to be transformed
		//   to apply to (%a+) damage"] = function(_, radius, dmgType) return { mod("ExtraJewelFunc", "LIST", {radius = firstToUpper(radius),
		//   type = "Other", func = function(node, out, data) if node and not node.conqueredBy then getSimpleConv({ "PhysicalDamage","FireDamage",
		//   "ColdDamage","LightningDamage","ChaosDamage","ElementalDamage" }, (dmgType:gsub("^%l", string.upper)).."Damage", "INC", true)(node, out, data)
		//   end end}, <non-unique jewel tags>) } end
		// The Lua function is data here: func names the conversion ("$jewelFunc").
		"non%-unique jewels cause increases and reductions to other damage types in a (%a+) radius to be transformed to apply to (%a+) damage": func(
			_ *Parser, a []any,
		) any {
			fn := map[string]any{"$jewelFunc": "convertDamageTypes", "dst": firstToUpper(argS(a, cap2)) + "Damage"}
			return []any{withTags(newMod(
				"ExtraJewelFunc",
				"LIST",
				map[string]any{
					"radius": firstToUpper(argS(a, cap1)),
					"type":   "Other",
					"func":   fn,
				},
				0,
				0,
			), nonUniqueJewelTags()...)}
		},
		// ModParser.lua:2666 ["non%-unique jewels cause small and notable passive skills in a (%a+) radius to also grant %+(%d+) to (%a+)"] =
		//   function(_, radius, val, attr) return { mod("ExtraJewelFunc", "LIST", {radius = (radius:gsub("^%l", string.upper)), type = "Other",
		//   func = function(node, out, data) if node and not node.conqueredBy and (node.type == "Notable" or node.type == "Normal") then
		//   out:NewMod(firstToUpper(attr):match("^%a%l%l"), "BASE", tonumber(val), data.modSource) end end}, <non-unique jewel tags>) } end
		"non%-unique jewels cause small and notable passive skills in a (%a+) radius to also grant %+(%d+) to (%a+)": func(_ *Parser, a []any) any {
			attr := firstToUpper(argS(a, cap3))
			if len(attr) >= 3 { // ("^%a%l%l"): the first three letters
				attr = attr[:3]
			}
			fn := map[string]any{"$jewelFunc": "grantAttribute", "stat": attr, "value": toNumber(argS(a, cap2))}
			return []any{withTags(newMod(
				"ExtraJewelFunc",
				"LIST",
				map[string]any{
					"radius": firstToUpper(argS(a, cap1)),
					"type":   "Other",
					"func":   fn,
				},
				0,
				0,
			), nonUniqueJewelTags()...)}
		},
	})
}
