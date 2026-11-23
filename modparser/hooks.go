package modparser

import (
	"math"
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
	// ["(%d+)%% chance to defend with (%d+)%% of armour"] = function(numChance, _, numArmourMultiplier) return {
	//   mod("ArmourDefense", "MAX", tonumber(numArmourMultiplier) - 100, "Armour Mastery: Max Calc", { type = "Condition", var = "ArmourMax" }),
	//   mod("ArmourDefense", "MAX", math.min(numChance / 100, 1.0) * (tonumber(numArmourMultiplier) - 100), "Armour Mastery: Average Calc", { type = "Condition", var = "ArmourAvg" }),
	//   mod("ArmourDefense", "MAX", math.min(math.floor(numChance / 100), 1.0) * (tonumber(numArmourMultiplier) - 100), "Armour Mastery: Min Calc",
	//     { type = "Condition", var = "ArmourMax", neg = true }, { type = "Condition", var = "ArmourAvg", neg = true }) } end
	"specialModList[^(%d+)%% chance to defend with (%d+)%% of armour$]": func(p *Parser, a []any) any {
		chance, _ := toNumber(arg(a, 0)).(float64)
		mult, _ := toNumber(argS(a, 2)).(float64)
		withSource := func(m map[string]any, src string) map[string]any { m["source"] = src; return m }
		cond := func(v string, neg bool) map[string]any {
			t := map[string]any{"type": "Condition", "var": v}
			if neg {
				t["neg"] = true
			}
			return t
		}
		return []any{
			withTags(withSource(newMod("ArmourDefense", "MAX", mult-100, 0, 0), "Armour Mastery: Max Calc"), cond("ArmourMax", false)),
			withTags(withSource(newMod("ArmourDefense", "MAX", math.Min(chance/100, 1)*(mult-100), 0, 0), "Armour Mastery: Average Calc"), cond("ArmourAvg", false)),
			withTags(withSource(newMod("ArmourDefense", "MAX", math.Min(math.Floor(chance/100), 1)*(mult-100), 0, 0), "Armour Mastery: Min Calc"),
				cond("ArmourMax", true), cond("ArmourAvg", true)),
		}
	},
	// ["deal (%d+)%% more damage with hits and ailments to rare and unique enemies for each second they've ever been in your presence, up to a maximum of (%d+)%%"] = function(num, _, limit) return {
	//   mod("Damage", "MORE", num, nil, 0, bor(KeywordFlag.Hit, KeywordFlag.Ailment), { type = "Multiplier", var = "EnemyPresenceSeconds", actor = "enemy", limit = tonumber(limit) }, { type = "ActorCondition", actor = "enemy", var = "RareOrUnique" }) } end
	"specialModList[^deal (%d+)%% more damage with hits and ailments to rare and unique enemies for each second they've ever been in your presence, up to a maximum of (%d+)%%$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		limit, _ := toNumber(argS(a, 2)).(float64)
		return []any{presenceDamage(n, limit, nil)}
	},
	// ["deal (%d+)%% more damage with hits and ailments to rare and unique enemies for every (%d+) seconds they've ever been in your presence, up to a maximum of (%d+)%%"] = function(num, _, div, limit) return {
	//   mod("Damage", "MORE", num, nil, 0, bor(KeywordFlag.Hit, KeywordFlag.Ailment), { type = "Multiplier", var = "EnemyPresenceSeconds", actor = "enemy", limit = tonumber(limit), div = div }, { type = "ActorCondition", actor = "enemy", var = "RareOrUnique" }) } end
	"specialModList[^deal (%d+)%% more damage with hits and ailments to rare and unique enemies for every (%d+) seconds they've ever been in your presence, up to a maximum of (%d+)%%$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		limit, _ := toNumber(argS(a, 3)).(float64)
		return []any{presenceDamage(n, limit, argS(a, 2))}
	},
	// ["socketed support gems can also support skills from y?o?u?r? ?e?q?u?i?p?p?e?d? ?([%a%s]+)"] = function (_, itemSlotName)
	//   local targetItemSlotName = "Body Armour"; if itemSlotName == "main hand" then targetItemSlotName = "Weapon 1" end
	//   return { mod("LinkedSupport", "LIST", { targetSlotName = targetItemSlotName }) } end
	"specialModList[^socketed support gems can also support skills from y?o?u?r? ?e?q?u?i?p?p?e?d? ?([%a%s]+)$]": func(p *Parser, a []any) any {
		target := "Body Armour"
		if argS(a, 1) == "main hand" {
			target = "Weapon 1"
		}
		return []any{newMod("LinkedSupport", "LIST", map[string]any{"targetSlotName": target}, 0, 0)}
	},
	// ["inflict withered for (%d+) seconds on hit if you've cast (.-) in the past (%d+) seconds"] = function (_, _, curse) return {
	//   flag("Condition:CanWither", { type = "Condition", var = "SelfCast"..curse:gsub("^%l", string.upper):gsub(" %l", string.upper):gsub(" ", "") }) } end
	"specialModList[^inflict withered for (%d+) seconds on hit if you've cast (.-) in the past (%d+) seconds$]": func(p *Parser, a []any) any {
		words := strings.Fields(argS(a, 2))
		for i, w := range words {
			words[i] = firstToUpper(w)
		}
		return []any{withTags(newMod("Condition:CanWither", "FLAG", true, 0, 0),
			map[string]any{"type": "Condition", "var": "SelfCast" + strings.Join(words, "")})}
	},
	// ["(%d+)%% of chaos damage t?a?k?e?n? ?does not bypass energy shield"] = function(num) return {
	//   mod("ChaosEnergyShieldBypass", "BASE", -num) } end
	"specialModList[^(%d+)%% of chaos damage t?a?k?e?n? ?does not bypass energy shield$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return []any{newMod("ChaosEnergyShieldBypass", "BASE", -n, 0, 0)}
	},
	// ["you count as on low (%a+) while at (%d+)%% of maximum (%a+) or below"] = function(_, resourceType, numStr) return {
	//   mod("Low"..resourceType:gsub("^%l", string.upper).."Percentage", "BASE", tonumber(numStr) / 100.0) } end
	"specialModList[^you count as on low (%a+) while at (%d+)%% of maximum (%a+) or below$]": poolPercentage("Low"),
	// ["you count as on full (%a+) while at (%d+)%% of maximum (%a+) or above"]: the same with "Full".
	"specialModList[^you count as on full (%a+) while at (%d+)%% of maximum (%a+) or above$]": poolPercentage("Full"),
	// ["you are cursed with level (%d+) (%D+)"] = function(num, _, name) return {
	//   mod("ExtraCurse", "LIST", { skillId = gemIdLookup[name], level = num, applyToPlayer = true }) } end
	"specialModList[^you are cursed with level (%d+) (%D+)$]": func(p *Parser, a []any) any {
		return []any{newMod("ExtraCurse", "LIST", p.curseOnSelf(argS(a, 2), arg(a, 0)), 0, 0)}
	},
	// ["you are cursed with (%D+)"] = function(_, skill) return {
	//   mod("ExtraCurse", "LIST", { skillId = gemIdLookup[skill], level = 1, applyToPlayer = true }) } end
	"specialModList[^you are cursed with (%D+)$]": func(p *Parser, a []any) any {
		return []any{newMod("ExtraCurse", "LIST", p.curseOnSelf(argS(a, 1), 1.0), 0, 0)}
	},
	// ["you are cursed with (%D+), with (%d+)%% increased effect"] = function(_, skill, num) return {
	//   mod("ExtraCurse", "LIST", { skillId = gemIdLookup[skill], level = 1, applyToPlayer = true }),
	//   mod("CurseEffectOnSelf", "INC", tonumber(num), { type = "SkillName", skillName = string.gsub(" "..skill, "%W%l", string.upper):sub(2) }) } end
	"specialModList[^you are cursed with (%D+), with (%d+)%% increased effect$]": func(p *Parser, a []any) any {
		skill := argS(a, 1)
		return []any{newMod("ExtraCurse", "LIST", p.curseOnSelf(skill, 1.0), 0, 0),
			withTags(newMod("CurseEffectOnSelf", "INC", toNumber(argS(a, 2)), 0, 0),
				map[string]any{"type": "SkillName", "skillName": upperAfterNonWord(" " + skill)[1:]})}
	},
	// ["(%d+)%% less damage taken per (%d+) rage, up to a maximum of (%d+)%%"] = function(num, _, div, limit) return {
	//   mod("DamageTaken", "MORE", -num, { type = "Multiplier", var = "Rage", div = div, limit = -tonumber(limit), limitNegTotal = true }) } end
	"specialModList[^(%d+)%% less damage taken per (%d+) rage, up to a maximum of (%d+)%%$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		limit, _ := toNumber(argS(a, 3)).(float64)
		return []any{withTags(newMod("DamageTaken", "MORE", -n, 0, 0),
			map[string]any{"type": "Multiplier", "var": "Rage", "div": argS(a, 2), "limit": -limit, "limitNegTotal": true})}
	},
	// ["take (%d+)%% less damage from hits for (%d+) seconds"] = function(num, _, duration) return {
	//   mod("DamageTakenWhenHit", "MORE", -num, { type = "Condition", var = "HeartstopperHIT" }),
	//   mod("DamageTakenWhenHit", "MORE", -num * tonumber(duration) / 10, { type = "Condition", var = "HeartstopperAVERAGE" }) } end
	"specialModList[^take (%d+)%% less damage from hits for (%d+) seconds$]": heartstopper("DamageTakenWhenHit", "HeartstopperHIT"),
	// ["take (%d+)%% less damage over time for (%d+) seconds"]: the same for DamageTakenOverTime and HeartstopperDOT.
	"specialModList[^take (%d+)%% less damage over time for (%d+) seconds$]": heartstopper("DamageTakenOverTime", "HeartstopperDOT"),
	// ["(%d+)%% of (%a+) and (%a+) damage from hits taken as (%a+) damage"] = function(num, _, type1, type2, type3)
	//   ... return fromType1 and fromType2 and toType and { mod(from1.."DamageFromHitsTakenAs"..to, "BASE", num), mod(from2..., "BASE", num) } end
	"specialModList[^(%d+)%% of (%a+) and (%a+) damage from hits taken as (%a+) damage$]": func(p *Parser, a []any) any {
		from1, from2, to := dmgTypeNames[argS(a, 2)], dmgTypeNames[argS(a, 3)], dmgTypeNames[argS(a, 4)]
		if from1 == "" || from2 == "" || to == "" {
			return nil
		}
		return []any{newMod(from1+"DamageFromHitsTakenAs"+to, "BASE", arg(a, 0), 0, 0), newMod(from2+"DamageFromHitsTakenAs"+to, "BASE", arg(a, 0), 0, 0)}
	},
	// ["increases and reductions to (%a+) damage also apply to effect of auras from (%a+) skills at (%d+)%% of their value, up to a maximum of (%d+)%%"] = function(_, element, skillElement, effect, limit) return {
	//   flag((element:gsub("^%l", string.upper)) .. "DamageAppliesTo" .. (skillElement:gsub("^%l", string.upper) .. "AuraEffect")),
	//   mod(("Improved" .. element:gsub("^%l", string.upper)) .. "DamageAppliesTo" .. (skillElement:gsub("^%l", string.upper) .. "AuraEffect"), "BASE", tonumber(effect)),
	//   mod((element:gsub("^%l", string.upper)) .. "DamageAppliesTo" .. (skillElement:gsub("^%l", string.upper) .. "AuraEffectLimit"), "MAX", tonumber(limit))
	// } end
	"specialModList[^increases and reductions to (%a+) damage also apply to effect of auras from (%a+) skills at (%d+)%% of their value, up to a maximum of (%d+)%%$]": func(p *Parser, a []any) any {
		name := firstToUpper(argS(a, 1)) + "DamageAppliesTo" + firstToUpper(argS(a, 2)) + "AuraEffect"
		effect, _ := toNumber(argS(a, 3)).(float64)
		limit, _ := toNumber(argS(a, 4)).(float64)
		return []any{
			newMod(name, "FLAG", true, 0, 0),
			newMod("Improved"+name, "BASE", effect, 0, 0),
			newMod(name+"Limit", "MAX", limit, 0, 0),
		}
	},
	// ["herald skills have (%d+)%% more buff effect for every (%d+)%% of maximum mana they reserve"] = function(_, num1, num2) return {
	//   mod("BuffEffect", "MORE", tonumber(num1), { type = "PerStat", stat = "ManaReservedPercent", div = tonumber(num2) }, { type = "SkillType", skillType = SkillType.Herald }) } end
	"specialModList[^herald skills have (%d+)%% more buff effect for every (%d+)%% of maximum mana they reserve$]": func(p *Parser, a []any) any {
		n1, _ := toNumber(argS(a, 1)).(float64)
		n2, _ := toNumber(argS(a, 2)).(float64)
		return []any{withTags(newMod("BuffEffect", "MORE", n1, 0, 0),
			map[string]any{"type": "PerStat", "stat": "ManaReservedPercent", "div": n2},
			map[string]any{"type": "SkillType", "skillType": 52.0})} // SkillType.Herald
	},
	// ["herald skills and minions from herald skills deal (%d+)%% more damage for every (%d+)%% of maximum life those skills reserve"] = function(_, num1, num2) return {
	//   mod("MinionModifier", "LIST", { mod = mod("Damage", "MORE", tonumber(num1), { type = "PerStat", stat = "LifeReservedPercent", div = tonumber(num2), actor = "parent" })}, { type = "SkillType", skillType = SkillType.Herald }),
	//   mod("Damage", "MORE", tonumber(num1), { type = "PerStat", stat = "LifeReservedPercent", div = tonumber(num2) }, { type = "SkillType", skillType = SkillType.Herald }),
	// } end
	"specialModList[^herald skills and minions from herald skills deal (%d+)%% more damage for every (%d+)%% of maximum life those skills reserve$]": func(p *Parser, a []any) any {
		n1, _ := toNumber(argS(a, 1)).(float64)
		n2, _ := toNumber(argS(a, 2)).(float64)
		herald := map[string]any{"type": "SkillType", "skillType": 52.0} // SkillType.Herald
		inner := withTags(newMod("Damage", "MORE", n1, 0, 0), map[string]any{"type": "PerStat", "stat": "LifeReservedPercent", "div": n2, "actor": "parent"})
		return []any{
			withTags(newMod("MinionModifier", "LIST", map[string]any{"mod": inner}, 0, 0), herald),
			withTags(newMod("Damage", "MORE", n1, 0, 0), map[string]any{"type": "PerStat", "stat": "LifeReservedPercent", "div": n2}, herald),
		}
	},
	// ["inflict (%w+) exposure on hit, applying %-(%d+)%% to (%w+) resistance"] = function(_, element1,  num, element2) return {
	//   mod( firstToUpper(element1).."ExposureChance", "BASE", 100, { type = "Condition", var = "Effective" }),
	//   mod("EnemyModifier", "LIST", { mod = mod(firstToUpper(element2).."Exposure", "BASE", -num) }, { type = "Condition", var = "Effective" }),
	// } end
	"specialModList[^inflict (%w+) exposure on hit, applying %-(%d+)%% to (%w+) resistance$]": func(p *Parser, a []any) any {
		n, _ := toNumber(argS(a, 2)).(float64)
		effective := map[string]any{"type": "Condition", "var": "Effective"}
		return []any{
			withTags(newMod(firstToUpper(argS(a, 1))+"ExposureChance", "BASE", 100.0, 0, 0), effective),
			withTags(newMod("EnemyModifier", "LIST", map[string]any{"mod": newMod(firstToUpper(argS(a, 3))+"Exposure", "BASE", -n, 0, 0)}, 0, 0), effective),
		}
	},
	// ["with at least one nearby corpse, nearby enemies deal (%d+)%% reduced damage"] = function(num) return {
	//   mod("EnemyModifier", "LIST", { mod = mod("Damage", "INC", -num) }, { type = "MultiplierThreshold", var = "NearbyCorpse", threshold = 1 }) } end
	"specialModList[^with at least one nearby corpse, nearby enemies deal (%d+)%% reduced damage$]": enemyModifier("Damage", "INC",
		map[string]any{"type": "MultiplierThreshold", "var": "NearbyCorpse", "threshold": 1.0}),
	// ["nearby hindered enemies deal (%d+)%% reduced damage over time"] = function(num) return {
	//   mod("EnemyModifier", "LIST", { mod = mod("DamageOverTime", "INC", -num) }, { type = "ActorCondition", actor = "enemy", var = "Hindered" }) } end
	"specialModList[^nearby hindered enemies deal (%d+)%% reduced damage over time$]": enemyModifier("DamageOverTime", "INC",
		map[string]any{"type": "ActorCondition", "actor": "enemy", "var": "Hindered"}),
	// ["nearby chilled enemies deal (%d+)%% reduced damage with hits"] = function(num) return {
	//   mod("EnemyModifier", "LIST", { mod = mod("Damage", "INC", -num) }, { type = "ActorCondition", actor = "enemy", var = "Chilled" }) } end
	"specialModList[^nearby chilled enemies deal (%d+)%% reduced damage with hits$]": enemyModifier("Damage", "INC",
		map[string]any{"type": "ActorCondition", "actor": "enemy", "var": "Chilled"}),
	// ["enemies ignited by you have (%d+)%% of (%a+) damage they deal converted to (%a+)"] = function(num, _, damageFrom, damageTo) return {
	//   mod("EnemyModifier", "LIST", { mod = mod(From.."DamageConvertTo"..To, "BASE", num, { type = "Condition", var = "Ignited" }) }) } end
	// (and the shocked and poisoned variants)
	"specialModList[^enemies ignited by you have (%d+)%% of (%a+) damage they deal converted to (%a+)$]":  enemyConversion("Ignited"),
	"specialModList[^enemies shocked by you have (%d+)%% of (%a+) damage they deal converted to (%a+)$]":  enemyConversion("Shocked"),
	"specialModList[^enemies poisoned by you have (%d+)%% of (%a+) damage they deal converted to (%a+)$]": enemyConversion("Poisoned"),
	// ["marked enemy has (%d+)%% reduced accuracy rating"] = function(num) return {
	//   mod("EnemyModifier", "LIST", { mod = mod("Accuracy", "INC", -num) }, {type = "ActorCondition", actor = "enemy", var = "Marked" }) } end
	"specialModList[^marked enemy has (%d+)%% reduced accuracy rating$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return []any{withTags(newMod("EnemyModifier", "LIST", map[string]any{"mod": newMod("Accuracy", "INC", -n, 0, 0)}, 0, 0),
			map[string]any{"type": "ActorCondition", "actor": "enemy", "var": "Marked"})}
	},
	// ["regenerate (%d+) life per second for each (%d+)%% uncapped fire resistance"] = function(num, _, percent)
	//   return { mod("LifeRegen", "BASE", num, { type = "PerStat", stat = "FireResistTotal", div = 1 / percent }) } end
	"specialModList[^regenerate (%d+) life per second for each (%d+)%% uncapped fire resistance$]": func(p *Parser, a []any) any {
		pct, _ := toNumber(argS(a, 2)).(float64)
		return []any{withTags(newMod("LifeRegen", "BASE", arg(a, 0), 0, 0),
			map[string]any{"type": "PerStat", "stat": "FireResistTotal", "div": 1 / pct})}
	},
	// ["your movement speed is (%d+)%% of its base value"] =
	//   function(num) return { mod("MovementSpeed", "OVERRIDE", num / 100) } end
	"specialModList[^your movement speed is (%d+)%% of its base value$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return []any{newMod("MovementSpeed", "OVERRIDE", n/100, 0, 0)}
	},
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
	// ["modifiers to maximum (%a+) resistance also apply to maximum (%a+) and (%a+) resistances"] =
	//   function(_, resFrom, resTo1, resTo2) return { mod(From.."MaxResConvertTo"..To1, "BASE", 100), (To2) } end
	"specialModList[^modifiers to maximum (%a+) resistance also apply to maximum (%a+) and (%a+) resistances$]": func(p *Parser, a []any) any {
		from := firstToUpper(argS(a, 1))
		return []any{newMod(from+"MaxResConvertTo"+firstToUpper(argS(a, 2)), "BASE", 100.0, 0, 0),
			newMod(from+"MaxResConvertTo"+firstToUpper(argS(a, 3)), "BASE", 100.0, 0, 0)}
	},
	// ["modifiers to (%a+) resistance also apply to (%a+) and (%a+) resistances at (%d+)%% of their value"] =
	//   function(_, resFrom, resTo1, resTo2, rate) return { mod(From.."ResConvertTo"..To1, "BASE", tonumber(rate)), (To2) } end
	"specialModList[^modifiers to (%a+) resistance also apply to (%a+) and (%a+) resistances at (%d+)%% of their value$]": func(p *Parser, a []any) any {
		from, rate := firstToUpper(argS(a, 1)), toNumber(argS(a, 4))
		return []any{newMod(from+"ResConvertTo"+firstToUpper(argS(a, 2)), "BASE", rate, 0, 0),
			newMod(from+"ResConvertTo"+firstToUpper(argS(a, 3)), "BASE", rate, 0, 0)}
	},
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
	// ["per (%d+) mana spent recently, up to (%d+)%%"] = function(num, _, limit) return {
	//   tag = { type = "Multiplier", var = "ManaSpentRecently", div = num, limit = tonumber(limit), limitTotal = true } } end
	"modTagList[per (%d+) mana spent recently, up to (%d+)%%]": func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "Multiplier", "var": "ManaSpentRecently", "div": arg(a, 0),
			"limit": toNumber(argS(a, 2)), "limitTotal": true}}
	},
	// ["per minion, up to ?a? ?m?a?x?i?m?u?m? ?o?f? (%d+)%%"] = function(num) return {
	//   tag = { type = "Multiplier", var = "SummonedMinion", limit = tonumber(num), limitTotal = true } } end
	"modTagList[per minion, up to ?a? ?m?a?x?i?m?u?m? ?o?f? (%d+)%%]": limitTotalTag("SummonedMinion"),
	// ["per second you've been stationary, up to a maximum of (%d+)%%"] = function(num) return {
	//   tag = { type = "Multiplier", var = "StationarySeconds", limit = tonumber(num), limitTotal = true } } end
	"modTagList[per second you've been stationary, up to a maximum of (%d+)%%]": limitTotalTag("StationarySeconds"),
	// Tags of ModParser.lua modTagList (each line quotes the Lua tag).
	// { type = "Multiplier", var = "PoisonAppliedRecently", globalLimit = tonumber(num), globalLimitKey = "DurationPerPoisonRecently" }
	"modTagList[for each poison you have inflicted recently, up to a maximum of (%d+)%%]": globalLimitTag("Multiplier", "var", "PoisonAppliedRecently", "DurationPerPoisonRecently", false),
	// { type = "Multiplier", var = "ManaSpentRecently", div = num, limit = tonumber(limit), limitTotal = true }
	"modTagList[for each (%d+) total mana you have spent recently, up to (%d+)%%]": divLimitTag("Multiplier", "var", "ManaSpentRecently"),
	// { type = "PerStat", stat = "Evasion", div = num, limit = tonumber(limit), limitTotal = true }
	"modTagList[per (%d+) evasion rating, up to (%d+)%%]": divLimitTag("PerStat", "stat", "Evasion"),
	// { type = "Multiplier", var = "ShockedNonShockedEnemyRecently", limit = tonumber(num), limitTotal = true }
	"modTagList[for each time you have shocked a non%-shocked enemy recently, up to a maximum of (%d+)%%]": limitTotalTag("ShockedNonShockedEnemyRecently"),
	// { type = "Multiplier", var = "EnemyKilledRecently", limit = tonumber(num), limitTotal = true }
	"modTagList[per enemy killed recently, up to (%d+)%%]": limitTotalTag("EnemyKilledRecently"),
	// { type = "Multiplier", var = "ManaBurnStacks", limit = tonumber(num), limitTotal = true }
	"modTagList[per mana burn, up to a maximum of (%d+)%%]": limitTotalTag("ManaBurnStacks"),
	// { type = "Multiplier", var = "PoisonStack", limit = tonumber(num), limitTotal = true }
	"modTagList[for each poison on you up to a maximum of (%d+)%%]": limitTotalTag("PoisonStack"),
	"modTagList[per poison on you, up to (%d+) per second]":         limitTotalTag("PoisonStack"),
	// { type = "Multiplier", var = "Rampage", div = num, limit = 1000 / num, limitTotal = true }
	"modTagList[per (%d+) rampage kills]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return map[string]any{"tag": map[string]any{"type": "Multiplier", "var": "Rampage", "div": arg(a, 0), "limit": 1000 / n, "limitTotal": true}}
	},
	// { type = "PerStat", stat = "MissingFireResist", div = num, globalLimit = tonumber(limit), globalLimitKey = "ReplicaNebulisFire" }
	"modTagList[per (%d+)%% missing fire resistance, up to a maximum of (%d+)%%]": globalLimitTag("PerStat", "stat", "MissingFireResist", "ReplicaNebulisFire", true),
	"modTagList[per (%d+)%% missing cold resistance, up to a maximum of (%d+)%%]": globalLimitTag("PerStat", "stat", "MissingColdResist", "ReplicaNebulisCold", true),
	// { type = "PerStat", statList = { "MissingFireResist", "MissingColdResist", "MissingLightningResist" }, div = num, globalLimit = tonumber(limit), globalLimitKey = "ReplicaNebulisCold" }
	"modTagList[per (%d+)%% missing fire, cold, or lightning resistance, up to a maximum of (%d+)%%]": func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "PerStat", "statList": []any{"MissingFireResist", "MissingColdResist", "MissingLightningResist"},
			"div": arg(a, 0), "globalLimit": toNumber(argS(a, 2)), "globalLimitKey": "ReplicaNebulisCold"}}
	},
	// { type = "Multiplier", var = firstToUpper(effectName) .. "Effect", div = num, actor = "enemy" }
	"modTagList[per (%d+)%% (%a+) effect on enemy]": func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "Multiplier", "var": firstToUpper(argS(a, 2)) + "Effect", "div": arg(a, 0), "actor": "enemy"}}
	},
	// { type = "DistanceRamp", ramp = { { num * 10, 1 }, { num * 10, 0 } } }
	"modTagList[against enemies within ([%d%.]+) metres?]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return map[string]any{"tag": map[string]any{"type": "DistanceRamp", "ramp": []any{[]any{n * 10, 1.0}, []any{n * 10, 0.0}}}}
	},
	// { type = "Condition", var = rarity:gsub("^%l", string.upper).."ItemIn"..item:gsub("^%l", string.upper).." "..(slot == "right" and 2 or slot == "left" and 1) }
	"modTagList[if you have a (%a+) (%a+) in (%a+) slot]": func(p *Parser, a []any) any {
		n := map[string]string{"right": "2", "left": "1"}[argS(a, 3)]
		if n == "" {
			return nil // Lua concatenates false: an error
		}
		return map[string]any{"tag": map[string]any{"type": "Condition", "var": firstToUpper(argS(a, 1)) + "ItemIn" + firstToUpper(argS(a, 2)) + " " + n}}
	},
	// { type = "Multiplier", var = "ActiveBrand", globalLimit = tonumber(num), globalLimitKey = "ChipAway" }
	"modTagList[per brand, up to a maximum of (%d+)%%]": globalLimitTag("Multiplier", "var", "ActiveBrand", "ChipAway", false),
	// { type = "PerStat", stat = "ManaUnreserved", div = num, limit = tonumber(limit), limitTotal = true }
	"modTagList[per (%d+) unreserved maximum mana, up to (%d+)%%]": divLimitTag("PerStat", "stat", "ManaUnreserved"),
	// { type = "PerStat", stat = "Mana", div = num, limit = tonumber(limit), limitTotal = true }
	"modTagList[per (%d+) maximum mana, up to (%d+)%%]": divLimitTag("PerStat", "stat", "Mana"),
	// { type = "ActorCondition", actor = "enemy", var = "Has"..(firstToUpper(element).."Exposure") }
	"modTagList[against enemies with (%w+) exposure]": func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "ActorCondition", "actor": "enemy", "var": "Has" + firstToUpper(argS(a, 1)) + "Exposure"}}
	},
	// { type = "ItemCondition", searchCond = conditionSubstring, itemSlot = itemSlotName:sub(1, #itemSlotName - 1), bothSlots = true }
	"modTagList[if both equipped left and right ([%a%s]+) have a?n? ?([%a%s]+) modifiers?]": func(p *Parser, a []any) any {
		slot := argS(a, 1)
		if slot != "" {
			slot = slot[:len(slot)-1]
		}
		return map[string]any{"tag": map[string]any{"type": "ItemCondition", "searchCond": argS(a, 2), "itemSlot": slot, "bothSlots": true}}
	},
	"modTagList[if (%d+) (%a+) items are equipped]": func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "MultiplierThreshold", "var": firstToUpper(argS(a, 2)) + "Item", "threshold": arg(a, 0)}}
	},
}

// enemyModifier builds the hooks that give the enemy "num% reduced <name>".
func enemyModifier(name, typ string, tag map[string]any) Hook {
	return func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return []any{withTags(newMod("EnemyModifier", "LIST", map[string]any{"mod": newMod(name, typ, -n, 0, 0)}, 0, 0), tag)}
	}
}

// enemyConversion builds the "enemies <cond> by you have N% of X damage
// they deal converted to Y" hooks.
func enemyConversion(cond string) Hook {
	return func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		inner := withTags(newMod(firstToUpper(argS(a, 2))+"DamageConvertTo"+firstToUpper(argS(a, 3)), "BASE", n, 0, 0),
			map[string]any{"type": "Condition", "var": cond})
		return []any{newMod("EnemyModifier", "LIST", map[string]any{"mod": inner}, 0, 0)}
	}
}

// curseOnSelf is { skillId = gemIdLookup[name], level = level, applyToPlayer = true }.
func (p *Parser) curseOnSelf(name string, level any) map[string]any {
	v := map[string]any{"level": level, "applyToPlayer": true}
	if id, ok := p.t.exact["gemIdLookup"][name]; ok {
		v["skillId"] = id
	}
	return v
}

// upperAfterNonWord is gsub(s, "%W%l", string.upper).
func upperAfterNonWord(s string) string {
	b := []byte(s)
	for i := 0; i+1 < len(b); i++ {
		c := b[i]
		isWord := c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !isWord && b[i+1] >= 'a' && b[i+1] <= 'z' {
			b[i+1] -= 'a' - 'A'
			i++
		}
	}
	return string(b)
}

// poolPercentage builds the Low/Full <Pool>Percentage hooks.
func poolPercentage(prefix string) Hook {
	return func(p *Parser, a []any) any {
		n, _ := toNumber(argS(a, 2)).(float64)
		return []any{newMod(prefix+firstToUpper(argS(a, 1))+"Percentage", "BASE", n/100, 0, 0)}
	}
}

// dmgTypeNames is ModParser.lua's local dmgTypes.
var dmgTypeNames = map[string]string{"physical": "Physical", "lightning": "Lightning", "cold": "Cold", "fire": "Fire", "chaos": "Chaos"}

// heartstopper builds the two Heartstopper hooks.
func heartstopper(name, cond string) Hook {
	return func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		d, _ := toNumber(argS(a, 2)).(float64)
		return []any{
			withTags(newMod(name, "MORE", -n, 0, 0), map[string]any{"type": "Condition", "var": cond}),
			withTags(newMod(name, "MORE", -n*d/10, 0, 0), map[string]any{"type": "Condition", "var": "HeartstopperAVERAGE"}),
		}
	}
}

// limitTotalTag builds the "per X, up to N%" tag hooks.
func limitTotalTag(v string) Hook {
	return func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": "Multiplier", "var": v, "limit": toNumber(arg(a, 0)), "limitTotal": true}}
	}
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

// presenceDamage is the War of Attrition mod: MORE damage with hits and
// ailments per second (or per div seconds) the enemy was in your presence.
func presenceDamage(n, limit float64, div any) map[string]any {
	const kfHit, kfAilment = 0x40000, 0x80000
	mult := map[string]any{"type": "Multiplier", "var": "EnemyPresenceSeconds", "actor": "enemy", "limit": limit}
	if div != nil {
		mult["div"] = div
	}
	return withTags(newMod("Damage", "MORE", n, 0, kfHit|kfAilment), mult,
		map[string]any{"type": "ActorCondition", "actor": "enemy", "var": "RareOrUnique"})
}

// divLimitTag is { type = typ, [key] = v, div = num, limit = tonumber(limit), limitTotal = true }.
func divLimitTag(typ, key, v string) Hook {
	return func(p *Parser, a []any) any {
		return map[string]any{"tag": map[string]any{"type": typ, key: v, "div": arg(a, 0), "limit": toNumber(argS(a, 2)), "limitTotal": true}}
	}
}

// globalLimitTag is { type = typ, [key] = v, (div = num,) globalLimit = tonumber(limit), globalLimitKey = gk }.
func globalLimitTag(typ, key, v, gk string, div bool) Hook {
	return func(p *Parser, a []any) any {
		t := map[string]any{"type": typ, key: v, "globalLimitKey": gk}
		if div {
			t["div"], t["globalLimit"] = arg(a, 0), toNumber(argS(a, 2))
		} else {
			t["globalLimit"] = toNumber(arg(a, 0))
		}
		return map[string]any{"tag": t}
	}
}
