package modparser

import (
	"math"
	"regexp"
	"slices"
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
	// ["(%d+)%% chance for hits to deal (%d+)%% of physical damage as extra damage of a random element"] = function(num, _, physPercent) return {
	//   mod("PhysicalDamageGainAsRandom", "BASE", (num*physPercent/100) ) } end,
	"specialModList[^(%d+)%% chance for hits to deal (%d+)%% of physical damage as extra damage of a random element$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		pct, _ := toNumber(argS(a, 2)).(float64)
		return []any{newMod("PhysicalDamageGainAsRandom", "BASE", n*pct/100, 0, 0)}
	},
	// ["(%d+)%% increased chaos damage per (%d+) maximum mana, up to a maximum of (%d+)%%"] = function(num, _, div, limit) return {
	//   mod("ChaosDamage", "INC", num, { type = "PerStat", stat = "Mana", div = tonumber(div), globalLimit = tonumber(limit), globalLimitKey = "DarkIdeation" }) } end,
	"specialModList[^(%d+)%% increased chaos damage per (%d+) maximum mana, up to a maximum of (%d+)%%$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		div, _ := toNumber(argS(a, 2)).(float64)
		limit, _ := toNumber(argS(a, 3)).(float64)
		return []any{withTags(newMod("ChaosDamage", "INC", n, 0, 0),
			map[string]any{"type": "PerStat", "stat": "Mana", "div": div, "globalLimit": limit, "globalLimitKey": "DarkIdeation"})}
	},
	// ["nearby enemies convert (%d+)%% of their (%a+) damage to (%a+)"] = function(num, _, damageFrom, damageTo) return {
	//   mod("EnemyModifier", "LIST", { mod = mod(<From>.."DamageConvertTo"..<To>, "BASE", num) }) } end,
	"specialModList[^nearby enemies convert (%d+)%% of their (%a+) damage to (%a+)$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		name := firstToUpper(argS(a, 2)) + "DamageConvertTo" + firstToUpper(argS(a, 3))
		return []any{newMod("EnemyModifier", "LIST", map[string]any{"mod": newMod(name, "BASE", n, 0, 0)}, 0, 0)}
	},
	// ["(%d+)%% increased area of effect per power charge, up to a maximum of (%d+)%%"] = function(num, _, limit) return {
	//   mod("AreaOfEffect", "INC", num, { type = "Multiplier", var = "PowerCharge", globalLimit = tonumber(limit), globalLimitKey = "VastPower" }) } end,
	"specialModList[^(%d+)%% increased area of effect per power charge, up to a maximum of (%d+)%%$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		limit, _ := toNumber(argS(a, 2)).(float64)
		return []any{withTags(newMod("AreaOfEffect", "INC", n, 0, 0),
			map[string]any{"type": "Multiplier", "var": "PowerCharge", "globalLimit": limit, "globalLimitKey": "VastPower"})}
	},
	// ["(%d+)%% increased attack and cast speed for each corpse consumed recently, up to a maximum of (%d+)%%"] = function(num, _, limit) return {
	//   mod("Speed", "INC", num, { type = "Multiplier", var = "CorpseConsumedRecently", limit = tonumber(limit / num) }) } end,
	"specialModList[^(%d+)%% increased attack and cast speed for each corpse consumed recently, up to a maximum of (%d+)%%$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		limit, _ := toNumber(argS(a, 2)).(float64)
		return []any{withTags(newMod("Speed", "INC", n, 0, 0), map[string]any{"type": "Multiplier", "var": "CorpseConsumedRecently", "limit": limit / n})}
	},
	// ["each mine applies (%d+)%% increased damage taken to enemies near it, up to (%d+)%%"] = function(num, _, limit) return {
	//   mod("EnemyModifier", "LIST", { mod = mod("DamageTaken", "INC", num, { type = "Multiplier", var = "ActiveMineCount", limit = limit / num }) }) } end,
	"specialModList[^each mine applies (%d+)%% increased damage taken to enemies near it, up to (%d+)%%$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		limit, _ := toNumber(argS(a, 2)).(float64)
		return []any{newMod("EnemyModifier", "LIST", map[string]any{"mod": withTags(newMod("DamageTaken", "INC", n, 0, 0),
			map[string]any{"type": "Multiplier", "var": "ActiveMineCount", "limit": limit / n})}, 0, 0)}
	},
	// ["enemies permanently take (%d+)%% increased damage for each second they've ever been frozen by you, up to a maximum of (%d+)%%"] = function(num, _, limit) return {
	//   mod("EnemyModifier", "LIST", { mod = mod("DamageTaken", "INC", num, { type = "Condition", var = "FrozenByYou" }, { type = "Multiplier", var = "FrozenByYouSeconds", limit = limit / num }) }) } end,
	"specialModList[^enemies permanently take (%d+)%% increased damage for each second they've ever been frozen by you, up to a maximum of (%d+)%%$]": func(_ *Parser, a []any) any {
		return everBeenAilmentedTaken(a, "FrozenByYou")
	},
	// ["enemies permanently take (%d+)%% increased damage for each second they've ever been chilled by you, up to a maximum of (%d+)%%"] = function(num, _, limit) return {
	//   mod("EnemyModifier", "LIST", { mod = mod("DamageTaken", "INC", num, { type = "Condition", var = "ChilledByYou" }, { type = "Multiplier", var = "ChilledByYouSeconds", limit = limit / num }) }) } end,
	"specialModList[^enemies permanently take (%d+)%% increased damage for each second they've ever been chilled by you, up to a maximum of (%d+)%%$]": func(_ *Parser, a []any) any {
		return everBeenAilmentedTaken(a, "ChilledByYou")
	},
	// ["each mine applies (%d+)%% reduced damage dealt to enemies near it, up to (%d+)%%"] = function(num, _, limit) return {
	//   mod("EnemyModifier", "LIST", { mod = mod("Damage", "INC", -num, { type = "Multiplier", var = "ActiveMineCount", limit = limit / num }) }) } end,
	"specialModList[^each mine applies (%d+)%% reduced damage dealt to enemies near it, up to (%d+)%%$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		limit, _ := toNumber(argS(a, 2)).(float64)
		return []any{newMod("EnemyModifier", "LIST", map[string]any{"mod": withTags(newMod("Damage", "INC", -n, 0, 0),
			map[string]any{"type": "Multiplier", "var": "ActiveMineCount", "limit": limit / n})}, 0, 0)}
	},
	// ["during f?l?a?s?k? ?effect, (%d+)%% reduced damage taken of each element for which your uncapped elemental resistance is lowest"] = function(num) return {
	//   mod("<E>DamageTaken", "INC", -num, { type = "StatThreshold", stat = "<E>ResistTotal", thresholdStat = "<O>ResistTotal", upper = true }, ...) } end,
	"specialModList[^during f?l?a?s?k? ?effect, (%d+)%% reduced damage taken of each element for which your uncapped elemental resistance is lowest$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return wiseOakMods(func(e string) map[string]any { return newMod(e+"DamageTaken", "INC", -n, 0, 0) }, true)
	},
	// ["during f?l?a?s?k? ?effect, damage penetrates (%d+)%% o?f? ?resistance of each element for which your uncapped elemental resistance is highest"] = function(num) return {
	//   mod("<E>Penetration", "BASE", num, { type = "StatThreshold", stat = "<E>ResistTotal", thresholdStat = "<O>ResistTotal" }, ...) } end,
	"specialModList[^during f?l?a?s?k? ?effect, damage penetrates (%d+)%% o?f? ?resistance of each element for which your uncapped elemental resistance is highest$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return wiseOakMods(func(e string) map[string]any { return newMod(e+"Penetration", "BASE", n, 0, 0) }, false)
	},
	// ["minions convert (%d+)%% of (.+) damage to (.+) damage"] = function(num, _, source, target) return {
	//   mod("MinionModifier", "LIST", { mod = mod(Source.."DamageConvertTo"..Target, "BASE", num) }) } end,
	"specialModList[^minions convert (%d+)%% of (.+) damage to (.+) damage$]": func(p *Parser, a []any) any {
		return []any{minionConvert(a, nil)}
	},
	// ["minions convert (%d+)%% of (.+) damage to (.+) damage per (.+) socket"]: { type = "Multiplier", var = Colour.."SocketIn{SlotName}" }
	"specialModList[^minions convert (%d+)%% of (.+) damage to (.+) damage per (.+) socket$]": func(p *Parser, a []any) any {
		return []any{minionConvert(a, map[string]any{"type": "Multiplier", "var": firstToUpper(argS(a, 4)) + "SocketIn{SlotName}"})}
	},
	// ["minions convert (%d+)%% of (.+) damage to (.+) damage per socketed (%a+) gem"]: red, green or blue only,
	//   { type = "Multiplier", var = "Socketed"..Colour.."GemsIn{SlotName}" }
	"specialModList[^minions convert (%d+)%% of (.+) damage to (.+) damage per socketed (%a+) gem$]": func(p *Parser, a []any) any {
		colour := argS(a, 4)
		if colour != "red" && colour != "green" && colour != "blue" {
			return nil
		}
		return []any{minionConvert(a, map[string]any{"type": "Multiplier", "var": "Socketed" + firstToUpper(colour) + "GemsIn{SlotName}"})}
	},
	// ["strength's damage bonus instead grants (%d+)%% increased melee physical damage per (%d+) strength"] = function(num, _, perStr) return {
	//   mod("StrDmgBonusRatioOverride", "BASE", num / tonumber(perStr)) } end,
	"specialModList[^strength's damage bonus instead grants (%d+)%% increased melee physical damage per (%d+) strength$]": func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		per, _ := toNumber(argS(a, 2)).(float64)
		return []any{newMod("StrDmgBonusRatioOverride", "BASE", n/per, 0, 0)}
	},
	// ["adds (%d+) to (%d+) fire damage to attacks for every (%d+)%% your light radius is above base value"] = function(_, min, max, lightRadius) return {
	//   mod("FireMin", "BASE", min, nil, ModFlag.Attack, { type = "PerStat", stat = "LightRadiusInc", div = lightRadius }), ...FireMax } end,
	"specialModList[^adds (%d+) to (%d+) fire damage to attacks for every (%d+)%% your light radius is above base value$]": func(p *Parser, a []any) any {
		mn, _ := toNumber(argS(a, 1)).(float64)
		mx, _ := toNumber(argS(a, 2)).(float64)
		div, _ := toNumber(argS(a, 3)).(float64)
		tag := map[string]any{"type": "PerStat", "stat": "LightRadiusInc", "div": div}
		return []any{withTags(newMod("FireMin", "BASE", mn, 1, 0), tag), withTags(newMod("FireMax", "BASE", mx, 1, 0), tag)}
	},
	// ["every (%d+) seconds, gain (%d+)%% of physical damage as extra fire damage for (%d+) seconds"] = function(frequency, _, num, duration) return {
	//   mod("PhysicalDamageGainAsFire", "BASE", tonumber(num), { type = "Condition", var = "NgamahuFlamesAdvance" }) } end,
	"specialModList[^every (%d+) seconds, gain (%d+)%% of physical damage as extra fire damage for (%d+) seconds$]": func(p *Parser, a []any) any {
		n, _ := toNumber(argS(a, 2)).(float64)
		return []any{withTags(newMod("PhysicalDamageGainAsFire", "BASE", n, 0, 0), map[string]any{"type": "Condition", "var": "NgamahuFlamesAdvance"})}
	},
	// ["^enemies (%a+) by you take "] = function(cond)
	//   return { tag = { type = "Condition", var = cond:gsub("^%a", string.upper) }, applyToEnemy = true, modSuffix = "Taken" } end,
	"preFlagList[^enemies (%a+) by you take ]": enemiesByYouHook("Taken"),
	// ["^enemies (%a+) by "] and ["^enemies (%a+) by you have "] = function(cond)
	//   return { tag = { type = "Condition", var = cond:gsub("^%a", string.upper) }, applyToEnemy = true } end,
	"preFlagList[^enemies (%a+) by ]":          enemiesByYouHook(""),
	"preFlagList[^enemies (%a+) by you have ]": enemiesByYouHook(""),
	// ["you and allies within ([%d%.]+) metres deal (%d+) to (%d+) additional cold damage"] = function(_, _, min, max) return {
	//   mod("ExtraAura", "LIST", { mod = mod("ColdMin", "BASE", tonumber(min)) }),
	//   mod("ExtraAura", "LIST", { mod = mod("ColdMax", "BASE", tonumber(max)) }) } end,
	"specialModList[^you and allies within ([%d%.]+) metres deal (%d+) to (%d+) additional cold damage$]": func(p *Parser, a []any) any {
		mn, _ := toNumber(argS(a, 2)).(float64)
		mx, _ := toNumber(argS(a, 3)).(float64)
		return []any{
			newMod("ExtraAura", "LIST", map[string]any{"mod": newMod("ColdMin", "BASE", mn, 0, 0)}, 0, 0),
			newMod("ExtraAura", "LIST", map[string]any{"mod": newMod("ColdMax", "BASE", mx, 0, 0)}, 0, 0),
		}
	},
	// ["deal no non%-(%a+) damage"] = function(_, dmgType) return dealNoNonDamageType(dmgType) end,
	// ["cannot deal non%-(%a+) damage"] = function(_, dmgType) return dealNoNonDamageType(dmgType) end,
	"specialModList[^deal no non%-(%a+) damage$]":     dealNoNonDamageType,
	"specialModList[^cannot deal non%-(%a+) damage$]": dealNoNonDamageType,
	// ["hits have (%d+)%% chance to deal (%d+)%% more area damage"] = function (num, _, more) return {
	//   mod("Damage", "MORE", (num*more/100), nil, bor(ModFlag.Area, ModFlag.Hit)) } end,
	"specialModList[^hits have (%d+)%% chance to deal (%d+)%% more area damage$]": func(p *Parser, a []any) any {
		const mfHit, mfArea = 0x4, 0x200
		n, _ := toNumber(arg(a, 0)).(float64)
		more, _ := toNumber(argS(a, 2)).(float64)
		return []any{newMod("Damage", "MORE", n*more/100, mfArea|mfHit, 0)}
	},
	// ["poisons you inflict during any flask effect have (%d+)%% chance to deal (%d+)%% more damage"] = function(num, _, more) return {
	//   mod("Damage", "MORE", tonumber(more) * num / 100, nil, 0, KeywordFlag.Poison, { type = "Condition", var = "UsingFlask" }) } end,
	"specialModList[^poisons you inflict during any flask effect have (%d+)%% chance to deal (%d+)%% more damage$]": func(p *Parser, a []any) any {
		const kfPoison = 0x200000
		n, _ := toNumber(arg(a, 0)).(float64)
		more, _ := toNumber(argS(a, 2)).(float64)
		return []any{withTags(newMod("Damage", "MORE", more*n/100, 0, kfPoison), map[string]any{"type": "Condition", "var": "UsingFlask"})}
	},
	// ["(%d+)%% increased bonuses gained from equipped (.+)"] = function(_, num, slot) return {
	//   mod("EffectOfBonusesFrom"..firstToUpper(argS(a, 2)), "INC", tonumber(num))} end,
	"specialModList[^(%d+)%% increased bonuses gained from equipped (.+)$]": func(p *Parser, a []any) any {
		return equippedBonus(a, 1)
	},
	"specialModList[^(%d+)%% reduced bonuses gained from equipped (.+)$]": func(p *Parser, a []any) any {
		return equippedBonus(a, -1)
	},
	// ["adds (%d+) to (%d+) attack physical damage to melee skills per (%d+) dexterity while you are unencumbered"] = function(_, min, max, dex) return {
	//   mod("PhysicalMin", "BASE", tonumber(min), nil, ModFlag.Melee, KeywordFlag.Attack, { type = "PerStat", stat = "Dex", div = tonumber(dex) }, { type = "Condition", var = "Unencumbered" }),
	//   mod("PhysicalMax", ...same with max) } end,
	"specialModList[^adds (%d+) to (%d+) attack physical damage to melee skills per (%d+) dexterity while you are unencumbered$]": func(p *Parser, a []any) any {
		const mfMelee, kfAttack = 0x100, 0x10000
		dex, _ := toNumber(argS(a, 3)).(float64)
		var out []any
		for i, name := range []string{"PhysicalMin", "PhysicalMax"} {
			v, _ := toNumber(argS(a, i+1)).(float64)
			out = append(out, withTags(newMod(name, "BASE", v, mfMelee, kfAttack),
				map[string]any{"type": "PerStat", "stat": "Dex", "div": dex},
				map[string]any{"type": "Condition", "var": "Unencumbered"}))
		}
		return out
	},
	// ["your hits treat cold resistance as (%d+)%% higher than actual value"] = function(num) return {
	//   mod("ColdPenetration", "BASE", -num, nil, 0, KeywordFlag.Hit) } end,
	"specialModList[^your hits treat cold resistance as (%d+)%% higher than actual value$]": func(p *Parser, a []any) any {
		const kfHit = 0x40000
		n, _ := toNumber(arg(a, 0)).(float64)
		return []any{newMod("ColdPenetration", "BASE", -n, 0, kfHit)}
	},
	// ["hits have (%d+)%% chance to treat enemy monster elemental resistance values as inverted"] = function(num) return {
	//   mod("HitsInvertEleResChance", "CHANCE", num / 100, nil) } end
	// and the cold, fire and lightning variants (HitsInvert<Type>ResChance).
	"specialModList[^hits have (%d+)%% chance to treat enemy monster elemental resistance values as inverted$]": invertResHook("HitsInvertEleResChance"),
	"specialModList[^hits have (%d+)%% chance to treat enemy monster cold resistance values as inverted$]":      invertResHook("HitsInvertColdResChance"),
	"specialModList[^hits have (%d+)%% chance to treat enemy monster fire resistance values as inverted$]":      invertResHook("HitsInvertFireResChance"),
	"specialModList[^hits have (%d+)%% chance to treat enemy monster lightning resistance values as inverted$]": invertResHook("HitsInvertLightningResChance"),
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
		return []any{withTags(newMod("Condition:CanWither", "FLAG", true, 0, 0), selfCastTag(argS(a, 2)))}
	},
	// The Balance of Terror: "... if you've cast <curse> in the past N seconds", each with
	// { type = "Condition", var = "SelfCast"..<Curse Name without spaces> }.
	"specialModList[^gain (%d+)%% of physical damage as a random element if you've cast (.-) in the past (%d+) seconds$]": func(p *Parser, a []any) any {
		return []any{withTags(newMod("PhysicalDamageGainAsRandom", "BASE", toNumber(arg(a, 0)), 0, 0), selfCastTag(argS(a, 2)))}
	},
	"specialModList[^(%a+) exposure on hit if you've cast (.-) in the past (%d+) seconds$]": func(p *Parser, a []any) any {
		return []any{castExposure(argS(a, 1), argS(a, 2))}
	},
	"specialModList[^inflict (%a+) exposure on hit if you've cast (.-) in the past (%d+) seconds$]": func(p *Parser, a []any) any {
		return []any{castExposure(argS(a, 1), argS(a, 2))}
	},
	"specialModList[^you are unaffected by (.+) if you've cast (.-) in the past (%d+) seconds$]": func(p *Parser, a []any) any {
		return []any{withTags(newMod("Self"+firstToUpper(argS(a, 1))+"Effect", "MORE", -100.0, 0, 0), selfCastTag(argS(a, 2)),
			map[string]any{"type": "GlobalEffect", "effectType": "Global", "unscalable": true})}
	},
	"specialModList[^immun[ei]t?y? to (%a-)s? if you've cast (.-) in the past (%d+) seconds$]": func(p *Parser, a []any) any {
		return []any{withTags(newMod(firstToUpper(argS(a, 1))+"Immune", "FLAG", true, 0, 0), selfCastTag(argS(a, 2)))}
	},
	"specialModList[^action speed cannot be slowed below base value if you've cast (.-) in the past (%d+) seconds$]": func(p *Parser, a []any) any {
		return []any{withTags(newMod("MinimumActionSpeed", "MAX", 100.0, 0, 0), selfCastTag(argS(a, 1)))}
	},
	"specialModList[^action speed cannot be modified to below base value if you've cast (.-) in the past (%d+) seconds$]": func(p *Parser, a []any) any {
		return []any{withTags(newMod("MinimumActionSpeed", "MAX", 100.0, 0, 0), selfCastTag(argS(a, 1)))}
	},
	"specialModList[^intimidate enemies on hit if you've cast (.-) in the past (%d+) seconds$]": func(p *Parser, a []any) any {
		return []any{withTags(newMod("EnemyModifier", "LIST", map[string]any{"mod": newMod("Condition:Intimidated", "FLAG", true, 0, 0)}, 4, 0), // ModFlag.Hit
			selfCastTag(argS(a, 1)))}
	},
	"specialModList[^take no extra damage from critical strikes if you've cast (.-) in the past (%d+) seconds$]": func(p *Parser, a []any) any {
		return []any{withTags(newMod("ReduceCritExtraDamage", "BASE", 100.0, 0, 0),
			map[string]any{"type": "GlobalEffect", "effectType": "Global", "unscalable": true}, selfCastTag(argS(a, 1)))}
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
	// ["while a unique enemy is in your presence, inflict (%w+) exposure on hit, applying %-(%d+)%% to (%w+) resistance"] = function(_, element1,  num, element2) return {
	//   mod( firstToUpper(element1).."ExposureChance", "BASE", 100, { type = "ActorCondition", actor = "enemy", var = "RareOrUnique" }, { type = "Condition", var = "Effective" }),
	//   mod("EnemyModifier", "LIST", { mod = mod(firstToUpper(element2).."Exposure", "BASE", -num, { type = "Condition", var = "RareOrUnique" }) }, { type = "Condition", var = "Effective" }),
	// } end
	"specialModList[^while a unique enemy is in your presence, inflict (%w+) exposure on hit, applying %-(%d+)%% to (%w+) resistance$]": func(_ *Parser, a []any) any {
		return presenceExposure(a, "RareOrUnique")
	},
	// ["while a pinnacle atlas boss is in your presence, inflict (%w+) exposure on hit, applying %-(%d+)%% to (%w+) resistance"]: the same with PinnacleBoss.
	"specialModList[^while a pinnacle atlas boss is in your presence, inflict (%w+) exposure on hit, applying %-(%d+)%% to (%w+) resistance$]": func(_ *Parser, a []any) any {
		return presenceExposure(a, "PinnacleBoss")
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
	// ["skills from equipped (.+) are supported by level (%d+) (.+)"] =
	//   function(_, slot, level, support) return extraSupport(support, level, slot) end
	"specialModList[^skills from equipped (.+) are supported by level (%d+) (.+)$]": func(p *Parser, a []any) any {
		slot := argS(a, 1)
		return p.extraSupport(argS(a, 3), arg(a, 2), &slot, nil)
	},
	// ["socketed [%a+]* ?spells a?r?e? ?supported by level (%d+) (.+)"] =
	//   function(num, _, support) return extraSupport(support, num, nil) end
	"specialModList[^socketed [%a+]* ?spells a?r?e? ?supported by level (%d+) (.+)$]": func(p *Parser, a []any) any {
		return p.extraSupport(argS(a, 2), arg(a, 0), nil, nil)
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
	// ["([%+%-]%d+) to level of socketed active skill gems per (%d+) player levels"] and
	// ["([%+%-]%d+) to level of socketed skill gems per (%d+) player levels"] = function(num, _, div) return {
	//   mod("GemProperty", "LIST", { keyword = "grants_active_skill", key = "level", value = num }, { type = "SocketedIn", slotName = "{SlotName}" }, { type = "Multiplier", var = "Level", div = tonumber(div) }) } end,
	"specialModList[^([%+%-]%d+) to level of socketed active skill gems per (%d+) player levels$]": socketedLevelPerPlayerLevels,
	"specialModList[^([%+%-]%d+) to level of socketed skill gems per (%d+) player levels$]":        socketedLevelPerPlayerLevels,
	// ["(%d+)%% chance for poisons inflicted with this weapon to deal (%d+)%% more damage"] = function(num, _, more) return {
	//   mod("Damage", "MORE", tonumber(more) * num / 100, nil, 0, KeywordFlag.Poison, { type = "Condition", var = "{Hand}Attack" }, { type = "SkillType", skillType = SkillType.Attack }) } end,
	"specialModList[^(%d+)%% chance for poisons inflicted with this weapon to deal (%d+)%% more damage$]": func(_ *Parser, a []any) any {
		return poisonChanceMore(a,
			map[string]any{"type": "Condition", "var": "{Hand}Attack"}, //nolint:goconst // Lua tag value
			map[string]any{"type": "SkillType", "skillType": 1.0})
	},
	// ["(%d+)%% chance for poisons inflicted with blast rain or artillery balls?i?s?ta to deal (%d+)%% more damage"]: the same for Blast Rain and Artillery Ballista.
	"specialModList[^(%d+)%% chance for poisons inflicted with blast rain or artillery balls?i?s?ta to deal (%d+)%% more damage$]": func(_ *Parser, a []any) any {
		return poisonChanceMore(a, map[string]any{
			"type": "SkillName", "skillNameList": []any{"Blast Rain", "Artillery Ballista"}, //nolint:goconst // Lua key
		})
	},
	// ["(%d+)%% chance for poisons inflicted with freezing pulse and eye of winter to deal (%d+)%% more damage"]: the same, transfigured gems included.
	"specialModList[^(%d+)%% chance for poisons inflicted with freezing pulse and eye of winter to deal (%d+)%% more damage$]": func(_ *Parser, a []any) any {
		return poisonChanceMore(a, map[string]any{
			"type": "SkillName", "skillNameList": []any{"Freezing Pulse", "Eye of Winter"}, "includeTransfigured": true,
		})
	},
	// ["([%a%s]+) reserves no mana"], ["([%a%s]+) has no reservation"] and
	// ["([%a%s]+) has no reservation if cast as an aura"] = function(_, name) return {
	//   mod("SkillData", "LIST", { key = "manaReservationFlat", value = 0 }, { type = "SkillId", skillId = gemIdLookup[name] },
	//     [{ type = "SkillType", skillType = SkillType.Aura },] { type = "SkillType", skillType = SkillType.Blessing, neg = true }),
	//   ... the same for lifeReservationFlat, manaReservationPercent, lifeReservationPercent } end,
	"specialModList[^([%a%s]+) reserves no mana$]": func(p *Parser, a []any) any {
		return noReservation(p, argS(a, 1), false)
	},
	"specialModList[^([%a%s]+) has no reservation$]": func(p *Parser, a []any) any {
		return noReservation(p, argS(a, 1), false)
	},
	"specialModList[^([%a%s]+) has no reservation if cast as an aura$]": func(p *Parser, a []any) any {
		return noReservation(p, argS(a, 1), true)
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
	// ["your (%w+) damage can (%w+)"] = function(_, element, ailment) return {
	//   flag(firstToUpper(element).."Can"..firstToUpper(ailment)) } end
	"specialModList[^your (%w+) damage can (%w+)$]": func(_ *Parser, a []any) any {
		return []any{newMod(firstToUpper(argS(a, cap1))+"Can"+firstToUpper(argS(a, cap2)), "FLAG", true, 0, 0)}
	},
	// ["your (%w+) damage cannot (%w+)"] = function(_, element, ailment) return {
	//   flag(firstToUpper(element).."Cannot"..firstToUpper(ailment)) } end
	"specialModList[^your (%w+) damage cannot (%w+)$]": func(_ *Parser, a []any) any {
		return []any{newMod(firstToUpper(argS(a, cap1))+"Cannot"+firstToUpper(argS(a, cap2)), "FLAG", true, 0, 0)}
	},
	// ["you and your minions take (%d+)%% reduced reflected (%w+) damage"] = function(num, _, type) return {
	//   mod(Type.."ReflectedDamageTaken", "INC", -tonumber(num), { type = "GlobalEffect", effectType = "Global", unscalable = true }),
	//   mod("MinionModifier", "LIST", { mod = mod(Type.."ReflectedDamageTaken", "INC", -tonumber(num), <same tag>) }) } end
	"specialModList[^you and your minions take (%d+)%% reduced reflected (%w+) damage$]": func(_ *Parser, a []any) any {
		return reflectedTaken(firstToUpper(argS(a, cap2))+"ReflectedDamageTaken", a, true)
	},
	// ["you and your minions take (%d+)%% reduced reflected damage"] = function(num) return {
	//   mod("ReflectedDamageTaken", "INC", -tonumber(num), <GlobalEffect tag>), mod("MinionModifier", "LIST", { mod = <same> }) } end
	"specialModList[^you and your minions take (%d+)%% reduced reflected damage$]": func(_ *Parser, a []any) any {
		return reflectedTaken("ReflectedDamageTaken", a, true)
	},
	// ["(%d+)%% reduced reflected damage taken during effect"] = function(num) return {
	//   mod("ReflectedDamageTaken", "INC", -tonumber(num), { type = "GlobalEffect", effectType = "Global", unscalable = true }) } end
	"specialModList[^(%d+)%% reduced reflected damage taken during effect$]": func(_ *Parser, a []any) any {
		return reflectedTaken("ReflectedDamageTaken", a, false)
	},
	// ["cannot take reflected (%a+) damage if (%d+) (%a+) items are equipped"] = function(_, element, thresh, influence) return {
	//   mod(firstToUpper(element).."ReflectedDamageTaken", "MORE", -100,
	//   { type = "MultiplierThreshold", var = firstToUpper(influence).."Item", threshold = tonumber(thresh) }) } end
	"specialModList[^cannot take reflected (%a+) damage if (%d+) (%a+) items are equipped$]": func(_ *Parser, a []any) any {
		tag := map[string]any{
			"type":      "MultiplierThreshold",
			"var":       firstToUpper(argS(a, cap3)) + "Item",
			"threshold": toNumber(argS(a, cap2)),
		}
		return []any{withTags(newMod(firstToUpper(argS(a, cap1))+"ReflectedDamageTaken", "MORE", -100.0, 0, 0), tag)}
	},
	// ["([%a%s]+) can affect hexproof enemies"] = function(_, name) return {
	//   mod("SkillData", "LIST", { key = "ignoreHexproof", value = true }, { type = "SkillId", skillId = gemIdLookup[name] }) } end
	"specialModList[^([%a%s]+) can affect hexproof enemies$]": func(p *Parser, a []any) any {
		tag := map[string]any{"type": "SkillId"}
		if id, ok := p.t.exact["gemIdLookup"][argS(a, cap1)].(string); ok {
			tag[keySkillID] = id
		}
		v := map[string]any{"key": "ignoreHexproof", "value": true}
		return []any{withTags(newMod("SkillData", "LIST", v, 0, 0), tag)}
	},
	// ["items and gems have (%d+)%% reduced attribute requirements"] = function(num) return {
	//   mod("GlobalAttributeRequirements", "INC", -num) } end
	"specialModList[^items and gems have (%d+)%% reduced attribute requirements$]": func(_ *Parser, a []any) any {
		n, _ := toNumber(arg(a, argNum)).(float64)
		return []any{newMod("GlobalAttributeRequirements", "INC", -n, 0, 0)}
	},
	// ["every (%d+) seconds, regenerate (%d+)%% of life over one second"] = function (num, _, percent) return {
	//   mod("LifeRegenPercent", "BASE", tonumber(percent), { type = "Condition", var = "LifeRegenBurstFull" }),
	//   mod("LifeRegenPercent", "BASE", tonumber(percent) / num, { type = "Condition", var = "LifeRegenBurstAvg" }) } end
	"specialModList[^every (%d+) seconds, regenerate (%d+)%% of life over one second$]": func(_ *Parser, a []any) any {
		n, _ := toNumber(arg(a, argNum)).(float64)
		pct, _ := toNumber(argS(a, cap2)).(float64)
		return []any{
			withTags(newMod("LifeRegenPercent", "BASE", pct, 0, 0), condTag("LifeRegenBurstFull")),
			withTags(newMod("LifeRegenPercent", "BASE", pct/n, 0, 0), condTag("LifeRegenBurstAvg")),
		}
	},
	// ["socketed skill gems get a (%d+)%% cost & reservation multiplier"] = function(num) return {
	//   mod("ExtraSkillMod", "LIST", { mod = mod("SupportManaMultiplier", "MORE", num - 100) }, { type = "SocketedIn", slotName = "{SlotName}" }) } end
	"specialModList[^socketed skill gems get a (%d+)%% cost & reservation multiplier$]": func(_ *Parser, a []any) any {
		n, _ := toNumber(arg(a, argNum)).(float64)
		mult := newMod("SupportManaMultiplier", "MORE", n-100, 0, 0) //nolint:mnd // PoB literal
		return []any{withTags(newMod("ExtraSkillMod", "LIST", map[string]any{"mod": mult}, 0, 0),
			map[string]any{"type": "SocketedIn", "slotName": "{SlotName}"})}
	},
	// ["summoned arbalists have (%d+)%% chance to inflict (.+) exposure on hit"] = function(num, _, type) return {
	//   mod(Type.."ExposureChance", "BASE", num, { type = "SkillName", skillName = "Summon Arbalists" }) } end
	"specialModList[^summoned arbalists have (%d+)%% chance to inflict (.+) exposure on hit$]": func(_ *Parser, a []any) any {
		return []any{withTags(newMod(firstToUpper(argS(a, cap2))+"ExposureChance", "BASE", arg(a, argNum), 0, 0),
			map[string]any{"type": "SkillName", "skillName": "Summon Arbalists"})}
	},
	// ["(%d+)%% chance to [c?t?][a?r?][s?i?][t?g?]g?e?r? socketed spells when you spend at least (%d+) mana on an upfront cost
	//   to use or trigger a skill, with a ([%d%.]+) second cooldown"] = function(num, _, amount, _) return {
	//   mod("KitavaTriggerChance", "BASE", num, "Kitava's Thirst"),
	//   mod("KitavaRequiredManaCost", "BASE", tonumber(amount), "Kitava's Thirst"),
	//   mod("ExtraSupport", "LIST", { skillId = "SupportCastOnManaSpent", level = 1 }, { type = "SocketedIn", slotName = "{SlotName}" }) } end
	"specialModList[^(%d+)%% chance to [c?t?][a?r?][s?i?][t?g?]g?e?r? socketed spells when you spend at least (%d+) mana on an upfront cost to use or trigger a skill, with a ([%d%.]+) second cooldown$]": func(_ *Parser, a []any) any {
		chance := newMod("KitavaTriggerChance", "BASE", arg(a, argNum), 0, 0)
		chance["source"] = "Kitava's Thirst"
		cost := newMod("KitavaRequiredManaCost", "BASE", toNumber(argS(a, cap2)), 0, 0)
		cost["source"] = "Kitava's Thirst"
		sup := map[string]any{keySkillID: "SupportCastOnManaSpent", "level": 1.0}
		support := newMod("ExtraSupport", "LIST", sup, 0, 0)
		return []any{chance, cost, withTags(support, map[string]any{"type": "SocketedIn", "slotName": "{SlotName}"})}
	},
	// ["(%d+)%% chance to gain (%d+)%% of non%-chaos damage with hits as extra chaos damage"] = function(num, _, perc) return {
	//   mod("NonChaosDamageGainAsChaos", "BASE", num / 100 * tonumber(perc)) } end
	"specialModList[^(%d+)%% chance to gain (%d+)%% of non%-chaos damage with hits as extra chaos damage$]": func(_ *Parser, a []any) any {
		n, _ := toNumber(arg(a, argNum)).(float64)
		perc, _ := toNumber(argS(a, cap2)).(float64)
		return []any{newMod("NonChaosDamageGainAsChaos", "BASE", n/100*perc, 0, 0)}
	},
	// ["([%+%-]%d+) to level of all (%a+) skill gems if (%d+) (%a+) items are equipped"] = function(num, _, element, thresh, influence) return {
	//   mod("GemProperty", "LIST", { keyword = element, key = "level", value = num, keyOfScaledMod = "value" },
	//   { type = "MultiplierThreshold", var = firstToUpper(influence) .. "Item", threshold = tonumber(thresh) }) } end
	"specialModList[^([%+%-]%d+) to level of all (%a+) skill gems if (%d+) (%a+) items are equipped$]": func(_ *Parser, a []any) any {
		prop := map[string]any{
			"keyword":        argS(a, cap2),
			"key":            "level",
			"value":          arg(a, argNum),
			"keyOfScaledMod": "value",
		}
		tag := map[string]any{
			"type":      "MultiplierThreshold",
			"var":       firstToUpper(argS(a, cap4)) + "Item",
			"threshold": toNumber(argS(a, cap3)),
		}
		return []any{withTags(newMod("GemProperty", "LIST", prop, 0, 0), tag)}
	},
	// ["nearby corpses explode when you warcry, dealing (%d+)%% of their life as (.+) damage"] = function(amount, _, type)
	//   return explodeFunc(100, amount, type) end
	"specialModList[^nearby corpses explode when you warcry, dealing (%d+)%% of their life as (.+) damage$]": func(_ *Parser, a []any) any {
		return explodeFunc(100, argS(a, cap1), argS(a, cap2)) //nolint:mnd // PoB literal
	},
	// The "Extra skill/support" block of ModParser.lua specialModList: one-line
	// functions that return triggerExtraSkill, as named by the helper used.
	"specialModList[^[ct][ar][si][tg]g?e?r?s? level (%d+) (.+) when equipped$]":                                                           numSkill,
	"specialModList[^[ct][ar][si][tg]g?e?r?s? level (%d+) (.+) on %a+$]":                                                                  numSkill,
	"specialModList[^use level (%d+) (.+) on %a+$]":                                                                                       numSkill,
	"specialModList[^[ct][ar][si][tg]g?e?r?s? level (%d+) (.+) when you attack$]":                                                         numSkill,
	"specialModList[^[ct][ar][si][tg]g?e?r?s? level (%d+) (.+) when you deal a critical strike$]":                                         numSkill,
	"specialModList[^[ct][ar][si][tg]g?e?r?s? level (%d+) (.+) when hit$]":                                                                numSkill,
	"specialModList[^[ct][ar][si][tg]g?e?r?s? level (%d+) (.+) when you kill an enemy$]":                                                  numSkill,
	"specialModList[^[ct][ar][si][tg]g?e?r?s? level (%d+) (.+) when you use a skill$]":                                                    numSkill,
	"specialModList[^trigger level (%d+) (.+) when you use a skill while you have a spirit charge$]":                                      numSkill,
	"specialModList[^trigger level (%d+) (.+) when you hit an enemy while cursed$]":                                                       numSkill,
	"specialModList[^trigger level (%d+) (.+) when you hit a bleeding enemy$]":                                                            numSkill,
	"specialModList[^trigger level (%d+) (.+) when you attack with this weapon$]":                                                         numSkill,
	"specialModList[^trigger level (%d+) (.+) when you hit a rare or unique enemy$]":                                                      numSkill,
	"specialModList[^trigger level (%d+) (.+) when you hit a rare or unique enemy and have no mark$]":                                     numSkill,
	"specialModList[^trigger level (%d+) (.+) when you hit a frozen enemy$]":                                                              numSkill,
	"specialModList[^trigger level (%d+) (.+) when you kill a frozen enemy$]":                                                             numSkill,
	"specialModList[^trigger level (%d+) (.+) when you attack with a bow$]":                                                               numSkill,
	"specialModList[^trigger level (%d+) (.+) when you block$]":                                                                           numSkill,
	"specialModList[^trigger level (%d+) (.+) when animated guardian kills an enemy$]":                                                    numSkill,
	"specialModList[^trigger level (%d+) (.+) when you lose cat's stealth$]":                                                              numSkill,
	"specialModList[^trigger level (%d+) (.+) when your trap is triggered$]":                                                              numSkill,
	"specialModList[^trigger level (%d+) (.+) on hit with this weapon$]":                                                                  numSkill,
	"specialModList[^trigger level (%d+) (.+) on melee hit$]":                                                                             numSkill,
	"specialModList[^trigger level (%d+) (.+) on melee hit while cursed$]":                                                                numSkill,
	"specialModList[^trigger level (%d+) (.+) on melee hit with this weapon$]":                                                            numSkill,
	"specialModList[^trigger level (%d+) (.+) every [%d%.]+ seconds while phasing$]":                                                      numSkill,
	"specialModList[^trigger level (%d+) (.+) when you gain avian's might or avian's flight$]":                                            numSkill,
	"specialModList[^trigger level (%d+) (.+) on melee hit if you have at least (%d+) strength$]":                                         numSkill,
	"specialModList[^trigger level (%d+) (.+) on critical strike with cleave or reave$]":                                                  numSkillCrit,
	"specialModList[^trigger level (%d+) (.+) on melee critical strike$]":                                                                 numSkillCrit,
	"specialModList[^trigger level (%d+) (.+) on critical strike against marked unique enemy$]":                                           numSkillCrit,
	"specialModList[^trigger level (%d+) (.+) on critical strike$]":                                                                       numSkillCrit,
	"specialModList[^trigger level (%d+) (.+) when you take a critical strike from a unique enemy$]":                                      numSkill,
	"specialModList[^trigger level (%d+) (.+) when you suppress spell damage from a unique enemy$]":                                       numSkill,
	"specialModList[^trigger level (%d+) (.+) when you block damage from a unique enemy$]":                                                numSkill,
	"specialModList[^trigger level (%d+) (.+) on taking a savage hit from a unique enemy$]":                                               numSkill,
	"specialModList[^trigger level (%d+) (.+) when a totem dies while a unique enemy is in your presence$]":                               numSkill,
	"specialModList[^trigger level (%d+) (.+) when you use a travel skill while a unique enemy is in your presence$]":                     numSkill,
	"specialModList[^trigger level (%d+) (.+) when you reach maximum rage while a unique enemy is in your presence$]":                     numSkill,
	"specialModList[^trigger level (%d+) (.+) when you reach low life while a unique enemy is in your presence$]":                         numSkill,
	"specialModList[^trigger level (%d+) (.+) when energy shield recharge starts while a unique enemy is in your presence$]":              numSkill,
	"specialModList[^trigger level (%d+) (.+) when your ward breaks$]":                                                                    numSkill,
	"specialModList[^trigger level (%d+) (.+) once every second$]":                                                                        numSkill,
	"specialModList[^triggers level (%d+) (.+)$]":                                                                                         numSkill,
	"specialModList[^trigger level (%d+) (.+) on attack critical strike against a rare or unique enemy and y?o?u? ?have no mark$]":        numSkillCrit,
	"specialModList[^triggers level (%d+) (.+) when equipped$]":                                                                           numSkill,
	"specialModList[^triggers level (%d+) (.+) when allocated$]":                                                                          numSkill,
	"specialModList[^attack with level (%d+) (.+) when you kill a bleeding enemy$]":                                                       numSkill,
	"specialModList[^triggers? level (%d+) (.+) when you kill a bleeding enemy$]":                                                         numSkill,
	"specialModList[^trigger level (%d+) (.+) when you attack with a non%-vaal slam or strike skill near an enemy$]":                      numSkill,
	"specialModList[^trigger level (%d+) (.+) every (%d+) seconds$]":                                                                      numSkill,
	"specialModList[^trigger level (%d+) (.+) after spending a total of (%d+) mana$]":                                                     numSkill,
	"specialModList[^trigger level (%d+) (.+) after spending (%d+)%% of your maximum life on spells$]":                                    numSkill,
	"specialModList[^consumes a void charge to trigger level (%d+) (.+) when you fire arrows$]":                                           numSkill,
	"specialModList[^consumes a void charge to trigger level (%d+) (.+) when you fire arrows with a non%-triggered skill$]":               numSkill,
	"specialModList[^curse enemies with level (%d+) (.+) on %a+$]":                                                                        numSkillCurse,
	"specialModList[^curse enemies with level (%d+) (%D+) on %a+, which can apply to hexproof enemies$]":                                  numSkillHexproof,
	"specialModList[^(%d+)%% chance to attack with level (%d+) (.+) on melee hit$]":                                                       chanceLevelSkill,
	"specialModList[^(%d+)%% chance to trigger level (%d+) (.+) when animated weapon kills an enemy$]":                                    chanceLevelSkill,
	"specialModList[^(%d+)%% chance to trigger level (%d+) (.+) on melee hit$]":                                                           chanceLevelSkill,
	"specialModList[^(%d+)%% chance to trigger level (%d+) (.+) [ow][nh]e?n? ?y?o?u? kill ?a?n? ?e?n?e?m?y?$]":                            chanceLevelSkill,
	"specialModList[^(%d+)%% chance to trigger level (%d+) (.+) when you use a socketed skill$]":                                          chanceLevelSkill,
	"specialModList[^(%d+)%% chance to trigger level (%d+) (.+) when you gain avian's might or avian's flight$]":                          chanceLevelSkill,
	"specialModList[^(%d+)%% chance to trigger level (%d+) (.+) on critical strike with this weapon$]":                                    chanceLevelSkillCrit,
	"specialModList[^(%d+)%% chance to trigger level (%d+) (.+) when you or a nearby ally kill an enemy, or hit a rare or unique enemy$]": chanceLevelSkill,
	"specialModList[^(%d+)%% chance to [ct][ar][si][tg]g?e?r? level (%d+) (.+) on %a+$]":                                                  chanceLevelSkill,
	"specialModList[^(%d+)%% chance to trigger (.+) when you kill an enemy$]":                                                             chanceSkill,
	"specialModList[^(.+) can trigger level (%d+) (.+)$]":                                                                                 sourceSkill,
	"specialModList[^curse enemies with (%D+) on %a+$]":                                                                                   skillOnly,
	"specialModList[^[ct][ar][si][tg]g?e?r?s? (.+) on %a+$]":                                                                              skillOnly,
	"specialModList[^[at][tr][ti][ag][cg][ke]r? (.+) on %a+$]":                                                                            skillOnly,
	"specialModList[^[at][tr][ti][ag][cg][ke]r? with (.+) on %a+$]":                                                                       skillOnly,
	"specialModList[^[ct][ar][si][tg]g?e?r?s? (.+) when hit$]":                                                                            skillOnly,
	"specialModList[^[at][tr][ti][ag][cg][ke]r? (.+) when hit$]":                                                                          skillOnly,
	"specialModList[^[at][tr][ti][ag][cg][ke]r? with (.+) when hit$]":                                                                     skillOnly,
	"specialModList[^[ct][ar][si][tg]g?e?r?s? (.+) when your skills or minions kill$]":                                                    skillOnly,
	"specialModList[^[at][tr][ti][ag][cg][ke]r? (.+) when you take a critical strike$]":                                                   skillOnly,
	"specialModList[^[at][tr][ti][ag][cg][ke]r? with (.+) when you take a critical strike$]":                                              skillOnly,
	"specialModList[^trigger (.+) on critical strike$]":                                                                                   skillOnlyCrit,
	"specialModList[^triggers? (.+) when you take a critical strike$]":                                                                    skillOnly,
	// ["grants (%D+)"] = function(_, skill) return grantedExtraSkill(skill, 1) end
	"specialModList[^grants (%D+)$]": func(p *Parser, a []any) any {
		return p.grantedExtraSkill(argS(a, cap1), 1.0, nil)
	},
	// ["grants level (%d+) (.+), which will be used by shaper memory"] = function(num, _, skill) return grantedExtraSkill(skill, num) end
	"specialModList[^grants level (%d+) (.+), which will be used by shaper memory$]": func(p *Parser, a []any) any {
		return p.grantedExtraSkill(argS(a, cap2), arg(a, argNum), nil)
	},
	// ["trigger level (%d+) (.+) when you consume a corpse"] = function(num, _, skill) return skill == "summon phantasm skill"
	//   and triggerExtraSkill("triggered summon phantasm skill", num) or triggerExtraSkill(skill, num) end
	// (triggerExtraSkill returns a table, always truthy, so the "and" branch wins.)
	"specialModList[^trigger level (%d+) (.+) when you consume a corpse$]": func(p *Parser, a []any) any {
		skill := argS(a, cap2)
		if skill == "summon phantasm skill" {
			skill = "triggered summon phantasm skill"
		}
		return p.triggerExtraSkill(skill, arg(a, argNum), triggerOpts{})
	},
	// ["trigger level (%d+) (.+), (.+) or (.+) every (%d+) seconds"] = function(num, _, skill1, skill2, skill3) return {
	//   mod("ExtraSkill", "LIST", { skillId = gemIdLookup[skill1], level = num, triggered = true }), ...skill2, skill3 } end
	"specialModList[^trigger level (%d+) (.+), (.+) or (.+) every (%d+) seconds$]": func(p *Parser, a []any) any {
		var mods []any
		for _, i := range []int{cap2, cap3, cap4} {
			v := map[string]any{"level": arg(a, argNum), keyTriggered: true}
			if id, ok := p.t.exact["gemIdLookup"][argS(a, i)].(string); ok {
				v[keySkillID] = id
			}
			mods = append(mods, newMod("ExtraSkill", "LIST", v, 0, 0))
		}
		return mods
	},
	// ["curse enemies with (.-) on hit if (%d+) (%a+) items are equipped"] = function(_, curse, count, influence)
	//   local mods = triggerExtraSkill(curse, 1, { noSupports = true })
	//   if mods[1] then mods[1].value.itemRequirement = { multiplier = firstToUpper(influence).."Item", count = tonumber(count) } end
	//   return mods end
	"specialModList[^curse enemies with (.-) on hit if (%d+) (%a+) items are equipped$]": func(p *Parser, a []any) any {
		mods := p.triggerExtraSkill(argS(a, cap1), 1.0, triggerOpts{noSupports: true})
		if len(mods) > 0 {
			if m, isMap := mods[0].(map[string]any); isMap {
				if v, ok := m["value"].(map[string]any); ok {
					v["itemRequirement"] = map[string]any{
						"multiplier": firstToUpper(argS(a, cap3)) + "Item", "count": toNumber(argS(a, cap2)),
					}
				}
			}
		}
		return mods
	},
	"specialModList[^curse enemies with (%D+) on %a+, with (%d+)%% increased effect$]":                                        curseIncreasedEffect,
	"specialModList[^curse enemies with (%D+) on %a+, with (%d+)%% reduced effect$]":                                          curseReducedEffect,
	"specialModList[^%d+%% chance to curse n?o?n?%-?c?u?r?s?e?d? ?enemies with (%D+) on %a+, with (%d+)%% increased effect$]": curseIncreasedEffect,
	// ["%d+%% chance to curse n?o?n?%-?c?u?r?s?e?d? ?enemies with (%D+) on %a+"] = function(_, skill) return {
	//   mod("ExtraSkill", "LIST", { skillId = gemIdLookup[skill], level = 1, noSupports = true, triggered = true }) } end
	"specialModList[^%d+%% chance to curse n?o?n?%-?c?u?r?s?e?d? ?enemies with (%D+) on %a+$]": func(p *Parser, a []any) any {
		return []any{p.curseSkill(argS(a, cap1))}
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

// triggerOpts are the options table of ModParser.lua triggerExtraSkill.
type triggerOpts struct {
	noSupports, onCrit, ignoreHexproof bool
	triggerChance, sourceSkill         any
}

// triggerExtraSkill ports ModParser.lua triggerExtraSkill(name, level, options):
// a triggered ExtraSkill for a known skill, plus the ignoreHexproof and
// onCrit SkillData mods when asked. An unknown skill gives an empty list.
func (p *Parser) triggerExtraSkill(name string, level any, o triggerOpts) []any {
	name = strings.ReplaceAll(name, " skill", "")
	id, known := p.t.exact["gemIdLookup"][name].(string)
	idTag := func() map[string]any {
		t := map[string]any{"type": "SkillId"}
		if known {
			t[keySkillID] = id
		}
		return t
	}
	mods := []any{}
	if known {
		v := map[string]any{keySkillID: id, keyTriggered: true}
		if l := toNumber(level); l != nil {
			v["level"] = l
		}
		if o.noSupports {
			v["noSupports"] = true
		}
		if s, ok := o.sourceSkill.(string); ok {
			v["source"] = strings.ReplaceAll(s, " skill", "")
		}
		if c := toNumber(o.triggerChance); c != nil {
			v["triggerChance"] = c
		}
		mods = append(mods, newMod("ExtraSkill", "LIST", v, 0, 0))
	}
	if o.ignoreHexproof {
		v := map[string]any{"key": "ignoreHexproof", "value": true}
		mods = append(mods, withTags(newMod("SkillData", "LIST", v, 0, 0), idTag()))
	}
	if o.onCrit {
		onCrit := newMod("SkillData", "LIST", map[string]any{"key": "triggerOnCrit", "value": true}, 0, 0)
		mods = append(mods, withTags(newMod("ExtraSkillMod", "LIST", map[string]any{"mod": onCrit}, 0, 0), idTag()))
	}
	return mods
}

// curseSkill is the ExtraSkill of the "curse enemies with <curse> on <event>,
// with N% effect" lines, which look the curse up without triggerExtraSkill.
func (p *Parser) curseSkill(name string) map[string]any {
	v := map[string]any{"level": 1.0, "noSupports": true, keyTriggered: true}
	if id, ok := p.t.exact["gemIdLookup"][name].(string); ok {
		v[keySkillID] = id
	}
	return newMod("ExtraSkill", "LIST", v, 0, 0)
}

// curseEffect is mod("CurseEffect", "INC", n, { type = "SkillName",
// skillName = string.gsub(" "..skill, "%W%l", string.upper):sub(2) }).
func curseEffect(skill string, n float64) map[string]any {
	name := slotWordRe.ReplaceAllStringFunc(" "+skill, strings.ToUpper)[1:]
	return withTags(newMod("CurseEffect", "INC", n, 0, 0), map[string]any{"type": "SkillName", "skillName": name})
}

// Capture positions in a hook's argument list: num (tonumber of the first
// capture) comes first, then the captures.
const (
	argNum = 0
	cap1   = 1
	cap2   = 2
	cap3   = 3
	cap4   = 4
)

// Value keys of ExtraSkill and SkillId tables.
const (
	keyTriggered = "triggered"
	keySkillID   = "skillId"
)

// The one-line triggerExtraSkill functions of ModParser.lua specialModList,
// by argument shape and options.

// numSkill is function(num, _, skill) return triggerExtraSkill(skill, num) end.
func numSkill(p *Parser, a []any) any {
	return p.triggerExtraSkill(argS(a, cap2), arg(a, argNum), triggerOpts{})
}

// numSkillCrit is function(num, _, skill) return triggerExtraSkill(skill, num, {onCrit = true}) end.
func numSkillCrit(p *Parser, a []any) any {
	return p.triggerExtraSkill(argS(a, cap2), arg(a, argNum), triggerOpts{onCrit: true})
}

// numSkillCurse is function(num, _, skill) return triggerExtraSkill(skill, num, {noSupports = true}) end.
func numSkillCurse(p *Parser, a []any) any {
	return p.triggerExtraSkill(argS(a, cap2), arg(a, argNum), triggerOpts{noSupports: true})
}

// numSkillHexproof is function(num, _, skill)
// return triggerExtraSkill(skill, num, {noSupports = true, ignoreHexproof = true}) end.
func numSkillHexproof(p *Parser, a []any) any {
	return p.triggerExtraSkill(argS(a, cap2), arg(a, argNum), triggerOpts{noSupports: true, ignoreHexproof: true})
}

// skillOnly is function(_, skill) return triggerExtraSkill(skill, 1, {noSupports = true}) end.
func skillOnly(p *Parser, a []any) any {
	return p.triggerExtraSkill(argS(a, cap1), 1.0, triggerOpts{noSupports: true})
}

// skillOnlyCrit is function(_, skill) return triggerExtraSkill(skill, 1, {noSupports = true, onCrit = true}) end.
func skillOnlyCrit(p *Parser, a []any) any {
	return p.triggerExtraSkill(argS(a, cap1), 1.0, triggerOpts{noSupports: true, onCrit: true})
}

// chanceLevelSkill is function(chance, _, level, skill)
// return triggerExtraSkill(skill, level, {triggerChance = chance}) end.
func chanceLevelSkill(p *Parser, a []any) any {
	return p.triggerExtraSkill(argS(a, cap3), arg(a, cap2), triggerOpts{triggerChance: arg(a, argNum)})
}

// chanceLevelSkillCrit is chanceLevelSkill with onCrit = true.
func chanceLevelSkillCrit(p *Parser, a []any) any {
	return p.triggerExtraSkill(argS(a, cap3), arg(a, cap2), triggerOpts{triggerChance: arg(a, argNum), onCrit: true})
}

// chanceSkill is function(chance, _, skill) return triggerExtraSkill(skill, 1, {triggerChance = chance}) end.
func chanceSkill(p *Parser, a []any) any {
	return p.triggerExtraSkill(argS(a, cap2), 1.0, triggerOpts{triggerChance: arg(a, argNum)})
}

// sourceSkill is function(_, sourceSkill, num, skill)
// return triggerExtraSkill(skill, tonumber(num), {sourceSkill = sourceSkill}) end.
func sourceSkill(p *Parser, a []any) any {
	return p.triggerExtraSkill(argS(a, cap3), arg(a, cap2), triggerOpts{sourceSkill: arg(a, cap1)})
}

// curseIncreasedEffect is ["curse enemies with (%D+) on %a+, with (%d+)%% increased effect"] = function(_, skill, num) return {
//
//	mod("ExtraSkill", "LIST", { skillId = gemIdLookup[skill], level = 1, noSupports = true, triggered = true }),
//	mod("CurseEffect", "INC", tonumber(num), { type = "SkillName", skillName = <Skill Name> }) } end
//
// and the "reduced" form (-tonumber(num)) and the "%d+%% chance to curse
// n?o?n?%-?c?u?r?s?e?d? ?enemies with ..." form, which drops the chance.
func curseIncreasedEffect(p *Parser, a []any) any { return curseWithEffect(p, a, 1) }

// curseReducedEffect is the "reduced" form of curseIncreasedEffect.
func curseReducedEffect(p *Parser, a []any) any { return curseWithEffect(p, a, -1) }

func curseWithEffect(p *Parser, a []any, sign float64) []any {
	n, _ := toNumber(argS(a, cap2)).(float64)
	return []any{p.curseSkill(argS(a, cap1)), curseEffect(argS(a, cap1), sign*n)}
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

// invertResHook returns the "chance to treat enemy monster <type> resistance
// values as inverted" specialModList functions: a CHANCE mod of num / 100.
func invertResHook(name string) Hook {
	return func(p *Parser, a []any) any {
		n, _ := toNumber(arg(a, 0)).(float64)
		return []any{newMod(name, "CHANCE", n/100, 0, 0)}
	}
}

// dealNoNonDamageType is ModParser.lua's dealNoNonDamageType (player only):
// a DealNo<Type> flag for every other damage type, nil for unknown types.
func dealNoNonDamageType(p *Parser, a []any) any {
	t := argS(a, 1)
	if t == "" {
		return nil
	}
	t = firstToUpper(t)
	types := []string{"Physical", "Lightning", "Cold", "Fire", "Chaos"}
	if !slices.Contains(types, t) {
		return nil
	}
	var mods []any
	for _, d := range types {
		if d != t {
			mods = append(mods, newMod("DealNo"+d, "FLAG", true, 0, 0))
		}
	}
	return mods
}

// enemiesByYouHook returns the "enemies <cond> by you ..." preFlagList
// functions: a Condition tag on the enemy, optionally with a mod suffix.
func enemiesByYouHook(suffix string) Hook {
	return func(p *Parser, a []any) any {
		out := map[string]any{
			"tag":          map[string]any{"type": "Condition", "var": firstToUpper(argS(a, 0))},
			"applyToEnemy": true,
		}
		if suffix != "" {
			out["modSuffix"] = suffix
		}
		return out
	}
}

// explodeFunc is ModParser.lua's explodeFunc: an ExplodeMod LIST entry and the
// CanExplode flag; nil when the amount is not a number, "tenth" or "quarter".
func explodeFunc(chance float64, amount, typ string, tags ...any) any {
	n, ok := toNumber(amount).(float64)
	if !ok {
		switch amount {
		case "tenth":
			n = 10
		case "quarter":
			n = 25
		default:
			return nil
		}
	}
	if typ != "" {
		typ = strings.ToUpper(typ[:1]) + typ[1:]
	}
	return []any{
		withTags(newMod("ExplodeMod", "LIST", map[string]any{"type": typ, "chance": chance / 100, "amount": n, "keyOfScaledMod": "chance"}, 0, 0), tags...),
		newMod("CanExplode", "FLAG", true, 0, 0),
	}
}

func init() {
	cond := func(v string) map[string]any { return map[string]any{"type": "Condition", "var": v} }
	enemy := func(v string) map[string]any {
		return map[string]any{"type": "ActorCondition", "actor": "enemy", "var": v}
	}
	// function(chance, _, amount, type) return explodeFunc(chance, amount, type, tags...) end
	chanceAmountType := func(tags ...any) Hook {
		return func(p *Parser, a []any) any {
			c, _ := toNumber(arg(a, 0)).(float64)
			return explodeFunc(c, argS(a, 2), argS(a, 3), tags...)
		}
	}
	// function(amount, _, type) return explodeFunc(100, amount, type, tags...) end
	amountType := func(tags ...any) Hook {
		return func(p *Parser, a []any) any {
			return explodeFunc(100, argS(a, 1), argS(a, 2), tags...)
		}
	}
	for k, h := range map[string]Hook{
		"enemies you kill have a (%d+)%% chance to explode, dealing a (.+) of their maximum life as (.+) damage":                            chanceAmountType(),
		"enemies you kill have ?a? ?(%d+)%% chance to explode, dealing (%d+)%% of their maximum life as (.+) damage":                        chanceAmountType(),
		"enemies you or your totems kill have (%d+)%% chance to explode, dealing (%d+)%% of their maximum life as (.+) damage":              chanceAmountType(),
		"enemies you kill while using pride have (%d+)%% chance to explode, dealing a (.+) of their maximum life as (.+) damage":            chanceAmountType(cond("AffectedByPride")),
		"enemies you kill while affected by glorious madness have a (%d+)%% chance to explode, dealing a (.+) of their life as (.+) damage": chanceAmountType(cond("AffectedByGloriousMadness")),
		"enemies killed with attack hits have a (%d+)%% chance to explode, dealing a (.+) of their life as (.+) damage":                     chanceAmountType(),
		"enemies killed with wand hits have a (%d+)%% chance to explode, dealing a (.+) of their life as (.+) damage":                       chanceAmountType(cond("UsingWand")),
		"cursed enemies you or your minions kill have a (%d+)%% chance to explode, dealing a (.+) of their maximum life as (.+) damage":     chanceAmountType(enemy("Cursed")),
		"enemies you kill explode, dealing (%d+)%% of their life as (.+) damage":                                                            amountType(),
		"enemies killed explode dealing (%d+)%% of their life as (.+) damage":                                                               amountType(),
		"enemies on fungal ground you kill explode, dealing (%d+)%% of their life as (.+) damage":                                           amountType(enemy("OnFungalGround")),
		"enemies killed with attack or spell hits explode, dealing (%d+)%% of their life as (.+) damage":                                    amountType(),
		"shocked enemies you kill explode, dealing (%d+)%% of their life as (.+) damage which cannot shock":                                 amountType(enemy("Shocked")),
		"ignited enemies you kill explode, dealing (%d+)%% of their life as (.+) damage which cannot ignite":                                amountType(enemy("Ignited")),
		"bleeding enemies you kill explode, dealing (%d+)%% of their maximum life as (.+) damage":                                           amountType(enemy("Bleeding")),
		// Haemophilia's Lua passes (chance, _, amount) as (amount, _, type).
		"burning enemies you kill have a (%d+)%% chance to explode, dealing a (.+) of their maximum life as (.+) damage": amountType(enemy("Burning")),
		"enemies killed near corpses affected by your curses explode, dealing (%d+)%% of their life as (.+) damage": amountType(
			map[string]any{"type": "MultiplierThreshold", "var": "NearbyCorpse", "threshold": 1.0}, enemy("Cursed")),
	} {
		hooks["specialModList[^"+k+"$]"] = h
	}
	// Oriath's End: function(chance, _, amount) return explodeFunc(chance, amount, "randomElement", ...) end
	hooks["specialModList[^enemies you kill during effect have a (%d+)%% chance to explode, dealing a (.+) of their maximum life as damage of a random element$]"] = func(p *Parser, a []any) any {
		c, _ := toNumber(arg(a, 0)).(float64)
		return explodeFunc(c, argS(a, 2), "randomElement", cond("UsingFlask"))
	}
}

// equippedBonus is EffectOfBonusesFrom<Slot> for "bonuses gained from
// equipped <slot>" lines.
func equippedBonus(a []any, sign float64) any {
	n, _ := toNumber(argS(a, 1)).(float64)
	return []any{newMod("EffectOfBonusesFrom"+firstToUpper(argS(a, 2)), "INC", sign*n, 0, 0)}
}

// noReservation is the SkillData list of the "<skill> has no reservation"
// lines: zero flat and percent reservation of both pools for that skill,
// except blessings (and only as an aura when aura is set).
func noReservation(p *Parser, name string, aura bool) any {
	const stAura, stBlessing = 39, 108
	skill := map[string]any{"type": "SkillId"}
	if id, ok := p.t.exact["gemIdLookup"][name]; ok {
		skill["skillId"] = id
	}
	tags := []any{skill}
	if aura {
		tags = append(tags, map[string]any{"type": "SkillType", "skillType": float64(stAura)})
	}
	tags = append(tags, map[string]any{"type": "SkillType", "skillType": float64(stBlessing), "neg": true})
	var out []any
	for _, key := range []string{"manaReservationFlat", "lifeReservationFlat", "manaReservationPercent", "lifeReservationPercent"} {
		out = append(out, withTags(newMod("SkillData", "LIST", map[string]any{"key": key, "value": 0.0}, 0, 0), tags...))
	}
	return out
}

// wiseOakMods are The Wise Oak's per-element mods, in ModParser's order
// (Lightning, Cold, Fire): each holds while that element's uncapped
// resistance is the lowest (upper) or highest of the three.
func wiseOakMods(mk func(elem string) map[string]any, upper bool) []any {
	others := map[string][2]string{"Lightning": {"Cold", "Fire"}, "Cold": {"Lightning", "Fire"}, "Fire": {"Lightning", "Cold"}}
	var out []any
	for _, e := range []string{"Lightning", "Cold", "Fire"} {
		var tags []any
		for _, o := range others[e] {
			t := map[string]any{"type": "StatThreshold", "stat": e + "ResistTotal", "thresholdStat": o + "ResistTotal"}
			if upper {
				t["upper"] = true
			}
			tags = append(tags, t)
		}
		out = append(out, withTags(mk(e), tags...))
	}
	return out
}

// minionConvert is the MinionModifier conversion of the "minions convert
// N% of X damage to Y damage" lines, with an optional outer tag.
func minionConvert(a []any, tag map[string]any) map[string]any {
	n, _ := toNumber(arg(a, 0)).(float64)
	// Lua arguments: num, cap1, source, target[, colour] (a[0] is num).
	name := firstToUpper(argS(a, 2)) + "DamageConvertTo" + firstToUpper(argS(a, 3))
	m := newMod("MinionModifier", "LIST", map[string]any{"mod": newMod(name, "BASE", n, 0, 0)}, 0, 0)
	if tag != nil {
		m = withTags(m, tag)
	}
	return m
}

// selfCastTag is { type = "Condition", var = "SelfCast"..<curse> } with each
// word capitalised and the spaces removed.
func selfCastTag(curse string) map[string]any {
	words := strings.Fields(curse)
	for i, w := range words {
		words[i] = firstToUpper(w)
	}
	return map[string]any{"type": "Condition", "var": "SelfCast" + strings.Join(words, "")}
}

// castExposure is the "(inflict) X exposure on hit if you've cast <curse>"
// mod: -10 X exposure on the enemy, while Effective.
func castExposure(element, curse string) map[string]any {
	return withTags(newMod("EnemyModifier", "LIST", map[string]any{"mod": newMod(firstToUpper(element)+"Exposure", "BASE", -10.0, 0, 0)}, 0, 0),
		selfCastTag(curse), map[string]any{"type": "Condition", "var": "Effective"})
}

// reflectedTaken is the "N% reduced reflected damage" mods: name INC -num
// with the unscalable Global effect tag, and the same for minions.
func reflectedTaken(name string, a []any, minions bool) []any {
	n, _ := toNumber(arg(a, 0)).(float64)
	global := func() map[string]any {
		tag := map[string]any{"type": "GlobalEffect", "effectType": "Global", "unscalable": true}
		return withTags(newMod(name, "INC", -n, 0, 0), tag)
	}
	out := []any{global()}
	if minions {
		out = append(out, newMod("MinionModifier", "LIST", map[string]any{"mod": global()}, 0, 0))
	}
	return out
}

// condTag is { type = "Condition", var = v }.
func condTag(v string) map[string]any { return map[string]any{"type": "Condition", "var": v} }

// everBeenAilmentedTaken is the Lua body of the "enemies permanently take
// (%d+)%% increased damage for each second they've ever been frozen/chilled
// by you" lines (ModParser.lua specialModList): cond is FrozenByYou or
// ChilledByYou, the multiplier is cond.."Seconds", limited to limit / num.
func everBeenAilmentedTaken(a []any, cond string) any {
	n, _ := toNumber(arg(a, 0)).(float64)
	limit, _ := toNumber(argS(a, 2)).(float64) //nolint:mnd // Lua capture index
	return []any{newMod("EnemyModifier", "LIST", map[string]any{"mod": withTags(newMod("DamageTaken", "INC", n, 0, 0),
		map[string]any{"type": "Condition", "var": cond},
		map[string]any{"type": "Multiplier", "var": cond + "Seconds", "limit": limit / n})}, 0, 0)}
}

// presenceExposure is the Lua body of the "while a unique enemy / pinnacle
// atlas boss is in your presence, inflict X exposure on hit" lines
// (ModParser.lua specialModList): the chance needs the enemy condition, the
// exposure the same condition on the enemy.
func presenceExposure(a []any, cond string) any {
	n, _ := toNumber(argS(a, 2)).(float64) //nolint:mnd // Lua capture index
	effective := map[string]any{"type": "Condition", "var": "Effective"}
	element := firstToUpper(argS(a, 3)) //nolint:mnd // Lua capture index
	exposure := withTags(newMod(element+"Exposure", "BASE", -n, 0, 0), map[string]any{"type": "Condition", "var": cond})
	return []any{
		withTags(newMod(firstToUpper(argS(a, 1))+"ExposureChance", "BASE", 100.0, 0, 0), //nolint:mnd // 100% chance
			map[string]any{"type": "ActorCondition", "actor": "enemy", "var": cond}, effective),
		withTags(newMod("EnemyModifier", "LIST", map[string]any{"mod": exposure}, 0, 0), effective),
	}
}

// socketedLevelPerPlayerLevels is the Lua body of the "to level of socketed
// (active) skill gems per N player levels" lines (The Poet's Pen).
func socketedLevelPerPlayerLevels(_ *Parser, a []any) any {
	div, _ := toNumber(argS(a, 2)).(float64) //nolint:mnd // Lua capture index
	prop := map[string]any{"keyword": "grants_active_skill", "key": "level", "value": arg(a, 0)}
	return []any{withTags(newMod("GemProperty", "LIST", prop, 0, 0),
		map[string]any{"type": "SocketedIn", "slotName": "{SlotName}"},
		map[string]any{"type": "Multiplier", "var": "Level", "div": div})}
}

// poisonChanceMore is the Lua body of the "N% chance for poisons inflicted
// with ... to deal M% more damage" lines: the averaged M * N / 100 more
// poison damage (KeywordFlag.Poison).
func poisonChanceMore(a []any, tags ...any) any {
	n, _ := toNumber(arg(a, 0)).(float64)
	more, _ := toNumber(argS(a, 2)).(float64)                                          //nolint:mnd // Lua capture index
	const kwPoison = 0x200000                                                          // KeywordFlag.Poison
	return []any{withTags(newMod("Damage", "MORE", more*n/100, 0, kwPoison), tags...)} //nolint:mnd // percent
}
