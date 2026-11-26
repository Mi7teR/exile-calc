package modparser

import "strings"

// Flask and tincture lines of ModParser.lua specialModList: charge
// generation, flask effect on you, mana burn and flask-effect conditions.

// chargesEvery is function(num, _, div) return { mod(name, "BASE", num / div, tags...) } end.
func chargesEvery(name string, tags ...any) Hook {
	return func(_ *Parser, a []any) any {
		return []any{withTags(newMod(name, "BASE", argF(a, argNum)/argF(a, cap2), 0, 0), tags...)}
	}
}

// negNum is function(num) return { mod(name, typ, -num, tags...) } end.
func negNum(name, typ string, tags ...any) Hook {
	return func(_ *Parser, a []any) any {
		return []any{withTags(newMod(name, typ, -argF(a, argNum), 0, 0), tags...)}
	}
}

func init() {
	registerSpecial(map[string]Hook{
		// ModParser.lua:5280 ["flasks gain (%d+) charges? every (%d+) seconds"] = function(num, _, div) return {
		//   mod("FlaskChargesGenerated", "BASE", num / div) } end
		"flasks gain (%d+) charges? every (%d+) seconds": chargesEvery("FlaskChargesGenerated"),
		// ModParser.lua:5283 ["flasks gain a charge every (%d+) seconds"] = function(_, div) return {
		//   mod("FlaskChargesGenerated", "BASE", 1 / div) } end
		"flasks gain a charge every (%d+) seconds": func(_ *Parser, a []any) any {
			return []any{newMod("FlaskChargesGenerated", "BASE", 1/argF(a, cap1), 0, 0)}
		},
		// ModParser.lua:5292 ["utility flasks gain (%d+) charges? every (%d+) seconds"]: UtilityFlaskChargesGenerated, num / div
		"utility flasks gain (%d+) charges? every (%d+) seconds": chargesEvery("UtilityFlaskChargesGenerated"),
		// ModParser.lua:5298 ["life flasks gain (%d+) charges? every (%d+) seconds"]: LifeFlaskChargesGenerated, num / div
		"life flasks gain (%d+) charges? every (%d+) seconds": chargesEvery("LifeFlaskChargesGenerated"),
		// ModParser.lua:5301 ["while on low life, life flasks gain (%d+) charges? every (%d+) seconds"] = function(num, _, div) return {
		//   mod("LifeFlaskChargesGenerated", "BASE", num / div, { type = "Condition", var = "LowLife" }) } end
		"while on low life, life flasks gain (%d+) charges? every (%d+) seconds": chargesEvery(
			"LifeFlaskChargesGenerated",
			condTag("LowLife"),
		),
		// ModParser.lua:5897 ["life flasks gain (%d+) charges? every (%d+) seconds if you haven't used a life flask recently"] =
		//   function(num, _, div) return { mod("LifeFlaskChargesGenerated", "BASE", num / div,
		//   { type = "Condition", var = "UsingLifeFlask", neg = true }) } end
		"life flasks gain (%d+) charges? every (%d+) seconds if you haven't used a life flask recently": chargesEvery(
			"LifeFlaskChargesGenerated",
			map[string]any{"type": "Condition", "var": "UsingLifeFlask", "neg": true},
		),
		// ModParser.lua:5304 ["mana flasks gain (%d+) charges? every (%d+) seconds"]: ManaFlaskChargesGenerated, num / div
		"mana flasks gain (%d+) charges? every (%d+) seconds": chargesEvery("ManaFlaskChargesGenerated"),
		// ModParser.lua:5307 ["flasks gain (%d+) charges? per empty flask slot every (%d+) seconds"] = function(num, _, div) return {
		//   mod("FlaskChargesGeneratedPerEmptyFlask", "BASE", num / div) } end
		"flasks gain (%d+) charges? per empty flask slot every (%d+) seconds": chargesEvery(
			"FlaskChargesGeneratedPerEmptyFlask",
		),
		// ModParser.lua:5277 ["(%d+)%% less flask charges gained from kills"] = function(num) return {
		//   mod("FlaskChargesGained", "MORE", -num, "from Kills") } end
		"(%d+)%% less flask charges gained from kills": func(_ *Parser, a []any) any {
			return []any{sourced(newMod("FlaskChargesGained", "MORE", -argF(a, argNum), 0, 0), "from Kills")}
		},
		// ModParser.lua:5355 ["flasks applied to you have (%d+)%% reduced effect"] = function(num) return {
		//   mod("FlaskEffect", "INC", -num, { type = "ActorCondition", actor = "player"}) } end
		"flasks applied to you have (%d+)%% reduced effect": negNum("FlaskEffect", "INC", playerTag()),
		// ModParser.lua:5359 ["tinctures have (%d+)%% increased effect while at or above (%d+) stacks of mana burn"] = function(num, _, threshold) return {
		//   mod("TinctureEffect", "INC", num, { type = "MultiplierThreshold", varList = {"ManaBurnStacks", "WeepingWoundsStacks"}, threshold = tonumber(threshold) }) } end
		"tinctures have (%d+)%% increased effect while at or above (%d+) stacks of mana burn": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("TinctureEffect", "INC", argF(a, argNum), 0, 0), map[string]any{
				"type":      "MultiplierThreshold",
				"varList":   []any{"ManaBurnStacks", "WeepingWoundsStacks"},
				"threshold": toNumber(argS(a, cap2)),
			})}
		},
		// ModParser.lua:5360 ["tinctures applied to you have (%d+)%% reduced mana burn rate"] = function(num) return {
		//   mod("TinctureManaBurnRate", "INC", -num, { type = "ActorCondition", actor = "player"}) } end
		"tinctures applied to you have (%d+)%% reduced mana burn rate": negNum(
			"TinctureManaBurnRate",
			"INC",
			playerTag(),
		),
		// ModParser.lua:5361 ["tinctures applied to you have (%d+)%% less mana burn rate"] = function(num) return {
		//   mod("TinctureManaBurnRate", "MORE", -num, { type = "ActorCondition", actor = "player"}) } end
		"tinctures applied to you have (%d+)%% less mana burn rate": negNum(
			"TinctureManaBurnRate",
			"MORE",
			playerTag(),
		),
		// ModParser.lua:5362 ["the first (%d+) mana burn applied to you have no effect"] = function(num) return {
		//   mod("EffectiveManaBurnStacks", "BASE", -num, { type = "ActorCondition", actor = "player"}) } end
		"the first (%d+) mana burn applied to you have no effect": negNum(
			"EffectiveManaBurnStacks",
			"BASE",
			playerTag(),
		),
		// ModParser.lua:3656 ["on non%-channelling attack, set a life flask with greater than %d+%% of maximum charges remaining to (%d+)%%
		//   for each charge removed this way, that attack gains %+(%d+)%% to damage over time multiplier"] = function(chargesConsumedPercent, _, damage) return {
		//   mod("DotMultiplier", "BASE", tonumber(damage), { type = "PercentStat", stat = "LifeFlaskCharges", percent = 100 - chargesConsumedPercent, floor = true },
		//     { type = "SkillType", skillType = SkillType.Channel, neg = true }, { type = "SkillType", skillType = SkillType.Attack }) } end
		"on non%-channelling attack, set a life flask with greater than %d+%% of maximum charges remaining to (%d+)%% for each charge removed this way, that attack gains %+(%d+)%% to damage over time multiplier": func(_ *Parser, a []any) any {
			channel := skillTypeTag(skillTypeChannel)
			channel["neg"] = true
			return []any{
				withTags(
					newMod("DotMultiplier", "BASE", toNumber(argS(a, cap2)), 0, 0),
					map[string]any{
						"type":    "PercentStat",
						"stat":    "LifeFlaskCharges",
						"percent": 100 - argF(a, argNum),
						"floor":   true,
					},
					channel,
					skillTypeTag(skillTypeAttack),
				),
			}
		},
		// ModParser.lua:2326 ["vaal skills used during effect have (%d+)%% reduced soul gain prevention duration"] = function(num) return {
		//   mod("SoulGainPreventionDuration", "INC", -num, { type = "Condition", var = "UsingFlask" }, { type = "SkillType", skillType = SkillType.Vaal }) } end
		"vaal skills used during effect have (%d+)%% reduced soul gain prevention duration": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("SoulGainPreventionDuration", "INC", -argF(a, argNum), 0, 0),
					condTag("UsingFlask"),
					skillTypeTag(skillTypeVaal),
				),
			}
		},
		// ModParser.lua:5226 ["grants level (%d+) (.+) curse aura during f?l?a?s?k? ?effect"] = function(num, _, skill) return {
		//   mod("ExtraCurse", "LIST", { skillId = gemIdLookup[skill:gsub(" skill","")] or "Unknown", level = num }, { type = "Condition", var = "UsingFlask" }) } end
		"grants level (%d+) (.+) curse aura during f?l?a?s?k? ?effect": func(p *Parser, a []any) any {
			id, ok := p.t.exact["gemIdLookup"][strings.ReplaceAll(argS(a, cap2), " skill", "")].(string)
			if !ok {
				id = "Unknown"
			}
			return []any{
				withTags(
					newMod("ExtraCurse", "LIST", map[string]any{keySkillID: id, "level": arg(a, argNum)}, 0, 0),
					condTag("UsingFlask"),
				),
			}
		},
	})
}

// flaskChargeInPresence is function(_, div) return { mod("FlaskChargesGenerated", "BASE", 1 / div,
// { type = "ActorCondition", actor = "enemy", var = v }) } end (ModParser.lua:5286, 5289).
func flaskChargeInPresence(v string) Hook {
	return func(_ *Parser, a []any) any {
		return []any{withTags(newMod("FlaskChargesGenerated", "BASE", 1/argF(a, cap1), 0, 0),
			map[string]any{"type": "ActorCondition", "actor": "enemy", "var": v})}
	}
}

func init() {
	registerSpecial(map[string]Hook{
		// ModParser.lua:5286 ["while a unique enemy is in your presence, flasks gain a charge every (%d+) seconds"]: var = "RareOrUnique"
		"while a unique enemy is in your presence, flasks gain a charge every (%d+) seconds": flaskChargeInPresence(
			"RareOrUnique",
		),
		// ModParser.lua:5289 ["while a pinnacle atlas boss is in your presence, flasks gain a charge every (%d+) seconds"]: var = "PinnacleBoss"
		"while a pinnacle atlas boss is in your presence, flasks gain a charge every (%d+) seconds": flaskChargeInPresence(
			"PinnacleBoss",
		),
	})
}
