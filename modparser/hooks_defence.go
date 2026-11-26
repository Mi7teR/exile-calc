package modparser

import (
	"math"
	"strings"
)

// Defence lines of ModParser.lua specialModList: armour, damage taken,
// resistances, debuffs and ailments on you, and enemy debuffs.

// incAndReducedRes is function(num) return { mod(stat, "INC", num, tags...), mod(res, "INC", -num) } end.
func incAndReducedRes(stat, res string, tags ...any) Hook {
	return func(_ *Parser, a []any) any {
		n := argF(a, argNum)
		return []any{withTags(newMod(stat, "INC", n, 0, 0), tags...), newMod(res, "INC", -n, 0, 0)}
	}
}

func init() { //nolint:funlen // one registration table of ported Lua functions
	registerSpecial(map[string]Hook{
		// ModParser.lua:2496 ["(%d+)%% chance to defend with (%d+)%% of armour for each time you've been hit by an enemy recently, up to (%d+)%%"] =
		//   function(numChance, _, numArmourMultiplier, cap) return {
		//   mod("ArmourDefense", "MAX", tonumber(numArmourMultiplier) - 100, "Armour Mastery: Max Calc", { type = "Condition", var = "ArmourMax" }),
		//   mod("ArmourDefense", "MAX", math.min(numChance / 100, 1.0) * (tonumber(numArmourMultiplier) - 100), "Armour Mastery: Average Calc",
		//     { type = "Condition", var = "ArmourAvg" }, { type = "Multiplier", var = "BeenHitRecently", limit = cap / numChance }),
		//   mod("ArmourDefense", "MAX", math.min(math.floor(numChance / 100), 1.0) * (tonumber(numArmourMultiplier) - 100), "Armour Mastery: Min Calc",
		//     { type = "Condition", var = "ArmourMax", neg = true }, { type = "Condition", var = "ArmourAvg", neg = true },
		//     { type = "Multiplier", var = "BeenHitRecently", limit = cap / numChance }) } end
		"(%d+)%% chance to defend with (%d+)%% of armour for each time you've been hit by an enemy recently, up to (%d+)%%": func(_ *Parser, a []any) any {
			chance, more := argF(a, argNum), argF(a, cap2)-100
			hits := func() map[string]any {
				return map[string]any{"type": "Multiplier", "var": "BeenHitRecently", "limit": argF(a, cap3) / chance}
			}
			neg := func(v string) map[string]any { t := condTag(v); t["neg"] = true; return t }
			return []any{
				withTags(
					sourced(newMod("ArmourDefense", "MAX", more, 0, 0), "Armour Mastery: Max Calc"),
					condTag("ArmourMax"),
				),
				withTags(
					sourced(
						newMod("ArmourDefense", "MAX", math.Min(chance/100, 1)*more, 0, 0),
						"Armour Mastery: Average Calc",
					),
					condTag("ArmourAvg"),
					hits(),
				),
				withTags(
					sourced(
						newMod("ArmourDefense", "MAX", math.Min(math.Floor(chance/100), 1)*more, 0, 0),
						"Armour Mastery: Min Calc",
					),
					neg("ArmourMax"),
					neg("ArmourAvg"),
					hits(),
				),
			}
		},
		// ModParser.lua:3935 ["modifiers to chance to suppress spell damage also apply to chance to defend with (%d+)%% of armour at (%d+)%% of their value"] =
		//   function (armourPercentage, _, effectiveness) return {
		//   mod("SpellSuppressionAppliesToChanceToDefendWithArmourPercentArmour", "MAX", tonumber(armourPercentage)),
		//   mod("SpellSuppressionAppliesToChanceToDefendWithArmourPercent", "MAX", tonumber(effectiveness)),
		//   flag("SpellSuppressionAppliesToChanceToDefendWithArmour") } end
		"modifiers to chance to suppress spell damage also apply to chance to defend with (%d+)%% of armour at (%d+)%% of their value": func(_ *Parser, a []any) any {
			return []any{
				newMod("SpellSuppressionAppliesToChanceToDefendWithArmourPercentArmour", "MAX", argF(a, argNum), 0, 0),
				newMod(
					"SpellSuppressionAppliesToChanceToDefendWithArmourPercent",
					"MAX",
					toNumber(argS(a, cap2)),
					0,
					0,
				),
				flagMod("SpellSuppressionAppliesToChanceToDefendWithArmour"),
			}
		},
		// ModParser.lua:4977 ["(%d+)%% increased armour per (%d+) reserved mana"] = function(num, _, mana) return {
		//   mod("Armour", "INC", num, { type = "PerStat", stat = "ManaReserved", div = tonumber(mana) }) } end
		"(%d+)%% increased armour per (%d+) reserved mana": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("Armour", "INC", argF(a, argNum), 0, 0),
					perStat("ManaReserved", toNumber(argS(a, cap2))),
				),
			}
		},
		// ModParser.lua:5153 ["(%d+)%% increased armour per second you've been stationary, up to a maximum of (%d+)%%"] = function(num, _, limit) return {
		//   mod("Armour", "INC", num, { type = "Multiplier", var = "StationarySeconds", limit = tonumber(limit / num) }, { type = "Condition", var = "Stationary" }) } end
		"(%d+)%% increased armour per second you've been stationary, up to a maximum of (%d+)%%": func(_ *Parser, a []any) any {
			n := argF(a, argNum)
			return []any{
				withTags(
					newMod("Armour", "INC", n, 0, 0),
					map[string]any{
						"type":  "Multiplier",
						"var":   "StationarySeconds",
						"limit": argF(a, cap2) / n,
					},
					condTag("Stationary"),
				),
			}
		},
		// ModParser.lua:2613 ["gain (%d+)%% increased armour per (%d+) power for 8 seconds when you warcry, up to a maximum of (%d+)%%"] =
		//   function(num, _, div, limit) return { mod("Armour", "INC", num,
		//   { type = "Multiplier", var = "WarcryPower", div = tonumber(div), globalLimit = tonumber(limit), globalLimitKey = "WarningCall" },
		//   { type = "Condition", var = "UsedWarcryInPast8Seconds" }) } end
		"gain (%d+)%% increased armour per (%d+) power for 8 seconds when you warcry, up to a maximum of (%d+)%%": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("Armour", "INC", argF(a, argNum), 0, 0),
					map[string]any{
						"type":           "Multiplier",
						"var":            "WarcryPower",
						"div":            toNumber(argS(a, cap2)),
						"globalLimit":    toNumber(argS(a, cap3)),
						"globalLimitKey": "WarningCall",
					},
					condTag("UsedWarcryInPast8Seconds"),
				),
			}
		},
		// ModParser.lua:4270 ["warcries grant arcane surge to you and allies, with (%d+)%% increased effect per (%d+) power, up to (%d+)%%"] =
		//   function(num, _, div, limit) return {
		//   mod("ExtraAura", "LIST", { mod = flag("Condition:ArcaneSurge")}, { type = "Condition", var = "UsedWarcryRecently" }),
		//   mod("ArcaneSurgeEffect", "INC", num, { type = "PerStat", stat = "WarcryPower", div = tonumber(div), globalLimit = tonumber(limit),
		//     globalLimitKey = "Brinerot Flag"}, { type = "Condition", var = "UsedWarcryRecently" }) } end
		"warcries grant arcane surge to you and allies, with (%d+)%% increased effect per (%d+) power, up to (%d+)%%": func(_ *Parser, a []any) any {
			power := perStat("WarcryPower", toNumber(argS(a, cap2)))
			power["globalLimit"], power["globalLimitKey"] = toNumber(argS(a, cap3)), "Brinerot Flag"
			return []any{
				modifierOf("ExtraAura", flagMod("Condition:ArcaneSurge"), condTag("UsedWarcryRecently")),
				withTags(
					newMod("ArcaneSurgeEffect", "INC", argF(a, argNum), 0, 0),
					power,
					condTag("UsedWarcryRecently"),
				),
			}
		},
		// ModParser.lua:2558 ["(%d+)%% less damage taken while there are at least two rare or unique enemies nearby"] = function(num) return {
		//   mod("DamageTaken", "MORE", -num, nil, 0, { type = "MultiplierThreshold", var = "NearbyRareOrUniqueEnemies", threshold = 2 }) } end
		"(%d+)%% less damage taken while there are at least two rare or unique enemies nearby": negNum(
			"DamageTaken",
			"MORE",
			thresholdTag("NearbyRareOrUniqueEnemies", 2.0),
		),
		// ModParser.lua:5122 ["(%d+)%% increased maximum life and reduced fire resistance"] = function(num) return {
		//   mod("Life", "INC", num), mod("FireResist", "INC", -num) } end
		"(%d+)%% increased maximum life and reduced fire resistance": incAndReducedRes("Life", "FireResist"),
		// ModParser.lua:5126 ["(%d+)%% increased maximum mana and reduced cold resistance"]: mod("Mana", "INC", num), mod("ColdResist", "INC", -num)
		"(%d+)%% increased maximum mana and reduced cold resistance": incAndReducedRes("Mana", "ColdResist"),
		// ModParser.lua:5130 ["(%d+)%% increased global maximum energy shield and reduced lightning resistance"]:
		//   mod("EnergyShield", "INC", num, { type = "Global" }), mod("LightningResist", "INC", -num)
		"(%d+)%% increased global maximum energy shield and reduced lightning resistance": incAndReducedRes(
			"EnergyShield",
			"LightningResist",
			map[string]any{"type": "Global"},
		),
		// ModParser.lua:4439 ["debuffs on you expire (%d+)%% slower"] = function(num) return { mod("SelfDebuffExpirationRate", "BASE", -num) } end
		"debuffs on you expire (%d+)%% slower": negNum("SelfDebuffExpirationRate", "BASE"),
		// ModParser.lua:4073 ["poisons on you expire (%d+)%% slower"] = function(num) return { mod("SelfPoisonDebuffExpirationRate", "BASE", -num) } end
		"poisons on you expire (%d+)%% slower": negNum("SelfPoisonDebuffExpirationRate", "BASE"),
		// ModParser.lua:6043 ["you take (%d+)%% increased extra damage from critical strikes"] = function(num) return { mod("ReduceCritExtraDamage", "BASE", -num) } end
		"you take (%d+)%% increased extra damage from critical strikes": negNum("ReduceCritExtraDamage", "BASE"),
		// ModParser.lua:2942 ["non%-damaging ailments have (%d+)%% reduced effect on you while you have arcane surge"] = function(num)
		//   for i, ailment in ipairs(data.nonDamagingAilmentTypeList) do
		//     mods[i] = mod("Self"..ailment.."Effect", "INC", -num, { type = "Condition", var = "AffectedByArcaneSurge" }) end
		// data.nonDamagingAilmentTypeList is Modules/Data.lua:421.
		"non%-damaging ailments have (%d+)%% reduced effect on you while you have arcane surge": func(_ *Parser, a []any) any {
			var out []any
			for _, ailment := range []string{"Chill", "Freeze", "Shock", "Scorch", "Brittle", "Sap"} {
				out = append(
					out,
					withTags(
						newMod("Self"+ailment+"Effect", "INC", -argF(a, argNum), 0, 0),
						condTag("AffectedByArcaneSurge"),
					),
				)
			}
			return out
		},
		// ModParser.lua:2416 ["gain no (.+) from equipped (.+)"] = function(_, stat, slot)
		//   if slot == "shield" then slot = "Weapon 2" end
		//   return { flag("GainNo" .. string.gsub(" "..stat, "%W%l", string.upper):sub(2):gsub(" ", "") .. "From" .. string.gsub(" "..slot, "%W%l", string.upper):sub(2)) } end
		"gain no (.+) from equipped (.+)": func(_ *Parser, a []any) any {
			stat, slot := argS(a, cap1), argS(a, cap2)
			if slot == "shield" {
				slot = "Weapon 2"
			}
			name := strings.ReplaceAll(upperAfterNonWord(" " + stat)[1:], " ", "")
			return []any{flagMod("GainNo" + name + "From" + upperAfterNonWord(" " + slot)[1:])}
		},
		// ModParser.lua:5801 ["nearby enemies have (%a+) resistance equal to yours"] = function(_, res) return {
		//   flag("Enemy"..(res:gsub("^%l", string.upper)).."ResistEqualToYours") } end
		"nearby enemies have (%a+) resistance equal to yours": func(_ *Parser, a []any) any {
			return []any{flagMod("Enemy" + firstToUpper(argS(a, cap1)) + "ResistEqualToYours")}
		},
		// ModParser.lua:3069 ["nearby enemy monsters' action speed is at most (%d+)%% of base value"] = function(num) return {
		//   mod("EnemyModifier", "LIST", { mod = mod("MaximumActionSpeedReduction", "MAX", 100 - num) }) } end
		"nearby enemy monsters' action speed is at most (%d+)%% of base value": func(_ *Parser, a []any) any {
			return []any{
				modifierOf("EnemyModifier", newMod("MaximumActionSpeedReduction", "MAX", 100-argF(a, argNum), 0, 0)),
			}
		},
	})
}
