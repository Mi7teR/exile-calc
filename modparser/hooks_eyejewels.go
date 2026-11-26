package modparser

// Eye jewel lines of ModParser.lua specialModList (Tecrod's, Kurgal's and
// Amanamu's Gaze, and the "while you have at least N <kind> eye jewels
// socketed" bonuses).

// eyeJewels is { type = "MultiplierThreshold", var = <Kind>EyeJewel, threshold = jewels }.
func eyeJewels(kind string, a []any) map[string]any {
	return thresholdTag(kind+"EyeJewel", arg(a, argNum))
}

// perEyeJewel is { type = "Multiplier", var = kind, globalLimit = tonumber(limit), globalLimitKey = key }.
func perEyeJewel(kind, key string, a []any) map[string]any {
	return map[string]any{
		"type":           "Multiplier",
		"var":            kind,
		"globalLimit":    toNumber(argS(a, cap2)),
		"globalLimitKey": key,
	}
}

func init() {
	registerSpecial(map[string]Hook{
		// ModParser.lua:4155 ["(%d+)%% increased effect of arcane surge on you per hypnotic eye jewel affecting you, up to a maximum of (%d+)%%"] =
		//   function(num, _, limit) return { mod("ArcaneSurgeEffect", "INC", num,
		//   { type = "Multiplier", var = "HypnoticEyeJewel", globalLimit = tonumber(limit), globalLimitKey = "KurgalGaze" }) } end
		"(%d+)%% increased effect of arcane surge on you per hypnotic eye jewel affecting you, up to a maximum of (%d+)%%": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("ArcaneSurgeEffect", "INC", argF(a, argNum), 0, 0),
					perEyeJewel("HypnoticEyeJewel", "KurgalGaze", a),
				),
			}
		},
		// ModParser.lua:4152 ["minions have %+(%d+)%% to damage over time multiplier per ghastly eye jewel affecting you, up to a maximum of %+(%d+)%%"] =
		//   function(num, _, limit) return { mod("MinionModifier", "LIST", { mod = mod("DotMultiplier", "BASE", num,
		//   { type = "Multiplier", var = "GhastlyEyeJewel", actor = "parent", globalLimit = tonumber(limit), globalLimitKey = "AmanamuGaze" }) }) } end
		"minions have %+(%d+)%% to damage over time multiplier per ghastly eye jewel affecting you, up to a maximum of %+(%d+)%%": func(_ *Parser, a []any) any {
			tag := perEyeJewel("GhastlyEyeJewel", "AmanamuGaze", a)
			tag["actor"] = "parent"
			return []any{
				modifierOf("MinionModifier", withTags(newMod("DotMultiplier", "BASE", argF(a, argNum), 0, 0), tag)),
			}
		},
		// ModParser.lua:4158 ["(%d+)%% increased main hand critical strike chance per murderous eye jewel affecting you, up to a maximum of (%d+)%%"] =
		//   function(num, _, limit) return { mod("CritChance", "INC", num,
		//   { type = "Multiplier", var = "MurderousEyeJewel", globalLimit = tonumber(limit), globalLimitKey = "TecrodGazeMainHand" },
		//   { type = "Condition", var = "MainHandAttack" }) } end
		"(%d+)%% increased main hand critical strike chance per murderous eye jewel affecting you, up to a maximum of (%d+)%%": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("CritChance", "INC", argF(a, argNum), 0, 0),
				perEyeJewel("MurderousEyeJewel", "TecrodGazeMainHand", a), condTag("MainHandAttack"))}
		},
		// ModParser.lua:4161 ["%+(%d+)%% to off hand critical strike multiplier per murderous eye jewel affecting you, up to a maximum of %+(%d+)%%"] =
		//   function(num, _, limit) return { mod("CritMultiplier", "BASE", num,
		//   { type = "Multiplier", var = "MurderousEyeJewel", globalLimit = tonumber(limit), globalLimitKey = "TecrodGazeOffHand" },
		//   { type = "Condition", var = "OffHandAttack" }) } end
		"%+(%d+)%% to off hand critical strike multiplier per murderous eye jewel affecting you, up to a maximum of %+(%d+)%%": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("CritMultiplier", "BASE", argF(a, argNum), 0, 0),
				perEyeJewel("MurderousEyeJewel", "TecrodGazeOffHand", a), condTag("OffHandAttack"))}
		},
		// ModParser.lua:3120 ["while you have at least (%d+) hypnotic eye jewels socketed: arcane surge also grants (%d+)%% of damage taken recouped as mana
		//   to you arcane surge also grants (%d+)%% increased mana cost efficiency to you"] = function(jewels, _, recoup, costEfficiency) return {
		//   mod("ArcaneSurgeAlsoManaRecoup", "BASE", tonumber(recoup), { type = "MultiplierThreshold", var = "HypnoticEyeJewel", threshold = jewels }),
		//   mod("ArcaneSurgeAlsoManaCostEfficiency", "INC", tonumber(costEfficiency), <same>) } end
		"while you have at least (%d+) hypnotic eye jewels socketed: arcane surge also grants (%d+)%% of damage taken recouped as mana to you arcane surge also grants (%d+)%% increased mana cost efficiency to you": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("ArcaneSurgeAlsoManaRecoup", "BASE", toNumber(argS(a, cap2)), 0, 0),
					eyeJewels("Hypnotic", a),
				),
				withTags(
					newMod("ArcaneSurgeAlsoManaCostEfficiency", "INC", toNumber(argS(a, cap3)), 0, 0),
					eyeJewels("Hypnotic", a),
				),
			}
		},
		// ModParser.lua:3126 ["while you have at least (%d+) ghastly eye jewels socketed: unholy might you grant also causes target's damage to penetrate (%d+)%%
		//   chaos resistance unholy might you grant also applies (%d+)%% increased effect of withered"] = function(jewels, _, pen, wither) return {
		//   mod("UnholyMightAlsoChaosPenetration", "BASE", tonumber(pen), { type = "MultiplierThreshold", var = "GhastlyEyeJewel", threshold = jewels }),
		//   mod("UnholyMightAlsoWitherEffect", "INC", tonumber(wither), <same>) } end
		"while you have at least (%d+) ghastly eye jewels socketed: unholy might you grant also causes target's damage to penetrate (%d+)%% chaos resistance unholy might you grant also applies (%d+)%% increased effect of withered": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("UnholyMightAlsoChaosPenetration", "BASE", toNumber(argS(a, cap2)), 0, 0),
					eyeJewels("Ghastly", a),
				),
				withTags(
					newMod("UnholyMightAlsoWitherEffect", "INC", toNumber(argS(a, cap3)), 0, 0),
					eyeJewels("Ghastly", a),
				),
			}
		},
		// ModParser.lua:3132 ["while you have at least (%d+) searching eye jewels socketed: targets affected by maim you inflict cannot deal critical strikes
		//   maim you inflict causes hits against the target to have (%d+)%% more critical strike chance"] = function(jewels, _, crit) return {
		//   mod("EnemyModifier", "LIST", { mod = flag("NeverCrit", { type = "Condition", var = "Maimed" }) },
		//     { type = "MultiplierThreshold", var = "SearchingEyeJewel", threshold = jewels }),
		//   mod("EnemyModifier", "LIST", { mod = mod("SelfCritChance", "MORE", tonumber(crit), { type = "Condition", var = "Maimed" }) }, <same>) } end
		"while you have at least (%d+) searching eye jewels socketed: targets affected by maim you inflict cannot deal critical strikes maim you inflict causes hits against the target to have (%d+)%% more critical strike chance": func(_ *Parser, a []any) any {
			return []any{
				modifierOf("EnemyModifier", flagMod("NeverCrit", condTag("Maimed")), eyeJewels("Searching", a)),
				modifierOf(
					"EnemyModifier",
					withTags(newMod("SelfCritChance", "MORE", toNumber(argS(a, cap2)), 0, 0), condTag("Maimed")),
					eyeJewels("Searching", a),
				),
			}
		},
		// ModParser.lua:3139 ["while you have at least (%d+) murderous eye jewels socketed: intimidate you inflict applies an extra %+(%d+)%% increased attack damage
		//   taken intimidate you inflict causes targets to deal (%d+)%% less damage"] = function(jewels, _, attackDam, lessDam) return {
		//   mod("EnemyModifier", "LIST", { mod = mod("DamageTaken", "INC", tonumber(attackDam), 0, ModFlag.Attack, { type = "Condition", var = "Intimidated" }) },
		//     { type = "MultiplierThreshold", var = "MurderousEyeJewel", threshold = jewels }),
		//   mod("EnemyModifier", "LIST", { mod = mod("Damage", "MORE", -tonumber(lessDam), { type = "Condition", var = "Intimidated" }) }, <same>) } end
		// The 0 source of the first inner mod is not kept by ModCache.
		"while you have at least (%d+) murderous eye jewels socketed: intimidate you inflict applies an extra %+(%d+)%% increased attack damage taken intimidate you inflict causes targets to deal (%d+)%% less damage": func(_ *Parser, a []any) any {
			return []any{
				modifierOf(
					"EnemyModifier",
					withTags(
						newMod("DamageTaken", "INC", toNumber(argS(a, cap2)), modFlagAttack, 0),
						condTag("Intimidated"),
					),
					eyeJewels("Murderous", a),
				),
				modifierOf(
					"EnemyModifier",
					withTags(newMod("Damage", "MORE", -argF(a, cap3), 0, 0), condTag("Intimidated")),
					eyeJewels("Murderous", a),
				),
			}
		},
	})
}
