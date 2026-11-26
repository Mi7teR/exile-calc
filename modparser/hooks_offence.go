package modparser

// Offence lines of ModParser.lua specialModList: damage, ailments, traps
// and mines, projectiles, warcries and skill-specific modifiers.

// trapThrowChance is the averaged TrapThrowCount (and MineThrowCount) of the
// "N% chance to throw up to M additional traps" lines: num * chance / 100.
func trapThrowChance(names ...string) Hook {
	return func(_ *Parser, a []any) any {
		var out []any
		for _, name := range names {
			out = append(out, newMod(name, "BASE", argF(a, cap2)*argF(a, argNum)/100, 0, 0))
		}
		return out
	}
}

func init() {
	registerSpecial(map[string]Hook{
		// ModParser.lua:3962 ["(%d+)%% chance for bleeding inflicted with this weapon to deal (%d+)%% more damage"] = function(num, _, more) return {
		//   mod("Damage", "MORE", tonumber(more) * num / 100, nil, 0, KeywordFlag.Bleed, { type = "Condition", var = "{Hand}Attack" },
		//   { type = "SkillType", skillType = SkillType.Attack }) } end
		"(%d+)%% chance for bleeding inflicted with this weapon to deal (%d+)%% more damage": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("Damage", "MORE", argF(a, cap2)*argF(a, argNum)/100, 0, kwFlagBleed),
				condTag("{Hand}Attack"), skillTypeTag(skillTypeAttack))}
		},
		// ModParser.lua:4491 ["(%d+)%% chance to throw up to (%d+) additional traps?"] = function(chance, _, num) return {
		//   mod("TrapThrowCount", "BASE", tonumber(num) * tonumber(chance) / 100.0) } end
		"(%d+)%% chance to throw up to (%d+) additional traps?": trapThrowChance("TrapThrowCount"),
		// ModParser.lua:4494 ["(%d+)%% chance to throw up to (%d+) additional traps? or mines?"] = function(chance, _, num) return {
		//   mod("TrapThrowCount", "BASE", tonumber(num) * tonumber(chance) / 100.0), mod("MineThrowCount", "BASE", <same>) } end
		"(%d+)%% chance to throw up to (%d+) additional traps? or mines?": trapThrowChance("TrapThrowCount", "MineThrowCount"),
		// ModParser.lua:4498 ["skills which throw traps throw up to (%d+) additional traps?"] = function(num) return {
		//   mod("TrapThrowCount", "BASE", tonumber(num)) } end
		"skills which throw traps throw up to (%d+) additional traps?": func(_ *Parser, a []any) any {
			return []any{newMod("TrapThrowCount", "BASE", argF(a, argNum), 0, 0)}
		},
		// ModParser.lua:4487 ["can have (%d+) fewer traps placed at a time"] = function(num) return { mod("ActiveTrapLimit", "BASE", -num) } end
		"can have (%d+) fewer traps placed at a time": negNum("ActiveTrapLimit", "BASE"),
		// ModParser.lua:4479 ["traps and mines deal (%d+) to (%d+) additional physical damage"] = function(_, min, max) return {
		//   mod("PhysicalMin", "BASE", tonumber(min), nil, 0, bor(KeywordFlag.Trap, KeywordFlag.Mine)),
		//   mod("PhysicalMax", "BASE", tonumber(max), nil, 0, bor(KeywordFlag.Trap, KeywordFlag.Mine)) } end
		"traps and mines deal (%d+) to (%d+) additional physical damage": func(_ *Parser, a []any) any {
			return []any{
				newMod("PhysicalMin", "BASE", argF(a, cap1), 0, kwFlagTrap|kwFlagMine),
				newMod("PhysicalMax", "BASE", argF(a, cap2), 0, kwFlagTrap|kwFlagMine),
			}
		},
		// ModParser.lua:2623 ["deal (%d+)%% less damage"] = function(num) return { mod("Damage", "MORE", -num) } end
		"deal (%d+)%% less damage": negNum("Damage", "MORE"),
		// ModParser.lua:3644 ["non%-critical strikes deal (%d+)%% less damage"] = function(num) return {
		//   mod("Damage", "MORE", -num, nil, ModFlag.Hit, { type = "Condition", var = "CriticalStrike", neg = true }) } end
		"non%-critical strikes deal (%d+)%% less damage": func(_ *Parser, a []any) any {
			crit := condTag("CriticalStrike")
			crit["neg"] = true
			return []any{withTags(newMod("Damage", "MORE", -argF(a, argNum), modFlagHit, 0), crit)}
		},
		// ModParser.lua:3900 ["hits with prismatic skills always ?i?n?f?l?i?c?t? (%w+)"] = function(_, type) return {
		//   mod("Enemy" .. firstToUpper(type) .. "Chance", "BASE", 100, nil, ModFlag.Hit, { type = "SkillType", skillType = SkillType.RandomElement }) } end
		"hits with prismatic skills always ?i?n?f?l?i?c?t? (%w+)": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("Enemy"+firstToUpper(argS(a, cap1))+"Chance", "BASE", 100.0, modFlagHit, 0), skillTypeTag(skillTypeRandElem))}
		},
		// ModParser.lua:5708 ["impale damage dealt to enemies impaled by you overwhelms (%d+)%% physical damage reduction"] = function(num) return {
		//   mod("EnemyImpalePhysicalDamageReduction", "BASE", -num) } end
		"impale damage dealt to enemies impaled by you overwhelms (%d+)%% physical damage reduction": negNum("EnemyImpalePhysicalDamageReduction", "BASE"),
		// ModParser.lua:4403 ["inflict fire exposure on hit against enemies with (%d+) cinderflame, applying %-(%d+)%% to (%w+) resistance"] =
		//   function(cinderflame, _, num, element) return {
		//   mod("FireExposureChance", "BASE", 100, { type = "Condition", var = "Effective" }, { type = "MultiplierThreshold", var = "CinderflameStacks", threshold = cinderflame }),
		//   mod("EnemyModifier", "LIST", { mod = mod(firstToUpper(element).."Exposure", "BASE", -num) }, <same two tags>) } end
		"inflict fire exposure on hit against enemies with (%d+) cinderflame, applying %-(%d+)%% to (%w+) resistance": func(_ *Parser, a []any) any {
			stacks := func() map[string]any { return thresholdTag("CinderflameStacks", arg(a, argNum)) }
			return []any{
				withTags(newMod("FireExposureChance", "BASE", 100.0, 0, 0), condTag("Effective"), stacks()),
				modifierOf("EnemyModifier", newMod(firstToUpper(argS(a, cap3))+"Exposure", "BASE", -argF(a, cap2), 0, 0), condTag("Effective"), stacks()),
			}
		},
		// ModParser.lua:2689 ["projectiles deal (%d+)%% increased damage with hits and ailments for each remaining chain, up to a maximum of (%d+)%%"] =
		//   function(num, _, limit) return { mod("Damage", "INC", num, nil, 0, bor(KeywordFlag.Hit, KeywordFlag.Ailment),
		//   { type = "PerStat", stat = "ChainRemaining", globalLimit = tonumber(limit), globalLimitKey = "FollowThrough" },
		//   { type = "SkillType", skillType = SkillType.Projectile }) } end
		"projectiles deal (%d+)%% increased damage with hits and ailments for each remaining chain, up to a maximum of (%d+)%%": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("Damage", "INC", argF(a, argNum), 0, kwFlagHit|kwFlagAilment),
				map[string]any{"type": "PerStat", "stat": "ChainRemaining", "globalLimit": toNumber(argS(a, cap2)), "globalLimitKey": "FollowThrough"},
				skillTypeTag(skillTypeProj))}
		},
		// ModParser.lua:2934 ["gain (%d+)%% of (%a+) damage as extra (%a+) damage for each of your hallowing flames that have been removed by an allied hit recently,
		//   up to (%d+)%%"] = function(num, _, fromType, destType, limit) return { mod(From .. "DamageGainAs" .. Dest, "BASE", num,
		//   { type = "ActorCondition", actor = "enemy", var = "HallowingFlame" },
		//   { type = "Multiplier", var = "HallowingFlameStacksRemovedByAlly", limit = tonumber(limit) / num }) } end
		"gain (%d+)%% of (%a+) damage as extra (%a+) damage for each of your hallowing flames that have been removed by an allied hit recently, up to (%d+)%%": func(_ *Parser, a []any) any {
			n := argF(a, argNum)
			return []any{withTags(newMod(firstToUpper(argS(a, cap2))+"DamageGainAs"+firstToUpper(argS(a, cap3)), "BASE", n, 0, 0),
				map[string]any{"type": "ActorCondition", "actor": "enemy", "var": "HallowingFlame"},
				map[string]any{"type": "Multiplier", "var": "HallowingFlameStacksRemovedByAlly", "limit": argF(a, cap4) / n})}
		},
		// ModParser.lua:4146 ["(%d+)%% increased area of effect per second you've been stationary, up to a maximum of (%d+)%%"] = function(num, _, limit) return {
		//   mod("AreaOfEffect", "INC", num, { type = "Multiplier", var = "StationarySeconds", globalLimit = tonumber(limit), globalLimitKey = "ExpansiveMight",
		//   limitTotal = true }) } end
		"(%d+)%% increased area of effect per second you've been stationary, up to a maximum of (%d+)%%": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("AreaOfEffect", "INC", argF(a, argNum), 0, 0), map[string]any{"type": "Multiplier", "var": "StationarySeconds",
				"globalLimit": toNumber(argS(a, cap2)), "globalLimitKey": "ExpansiveMight", "limitTotal": true})}
		},
		// ModParser.lua:5416 ["warcries have (%d+)%% chance to exert (%d+) additional attacks?"] = function(num, _, var) return {
		//   mod("ExtraExertedAttacks", "BASE", (num*var/100)) } end
		"warcries have (%d+)%% chance to exert (%d+) additional attacks?": func(_ *Parser, a []any) any {
			return []any{newMod("ExtraExertedAttacks", "BASE", argF(a, argNum)*argF(a, cap2)/100, 0, 0)}
		},
		// ModParser.lua:4056 ["poisons you inflict deal damage (%d+)%% slower"] = function(num) return { mod("PoisonFaster", "INC", -num) } end
		"poisons you inflict deal damage (%d+)%% slower": negNum("PoisonFaster", "INC"),
		// ModParser.lua:2217 ["enemies taunted by your warcries explode on death, dealing (%d+)%% of their maximum life as (.+) damage"] = function(amount, _, type)
		//   return explodeFunc(100, amount, type, { type = "ActorCondition", actor = "enemy", var = "Taunted" }, { type = "Condition", var = "UsedWarcryRecently" }) end
		"enemies taunted by your warcries explode on death, dealing (%d+)%% of their maximum life as (.+) damage": func(_ *Parser, a []any) any {
			return explodeFunc(100, argS(a, cap1), argS(a, cap2),
				map[string]any{"type": "ActorCondition", "actor": "enemy", "var": "Taunted"}, condTag("UsedWarcryRecently"))
		},
		// ModParser.lua:5959 ["%+([%d%.]+) metres? to weapon range per 10%% quality"] = function(num) return {
		//   mod("AlternateQualityLocalWeaponRangePer10Quality", "BASE", num * 10) } end
		"%+([%d%.]+) metres? to weapon range per 10%% quality": func(_ *Parser, a []any) any {
			return []any{newMod("AlternateQualityLocalWeaponRangePer10Quality", "BASE", argF(a, argNum)*10, 0, 0)}
		},
		// ModParser.lua:6050 ["physical skills have (%d+)%% increased duration per (%d+) intelligence"] = function(num1, _, num2)
		//   return { mod("Duration", "INC", num1, nil, nil, KeywordFlag.Physical, { type = "PerStat", stat = "Int", div = tonumber(num2) }) } end
		"physical skills have (%d+)%% increased duration per (%d+) intelligence": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("Duration", "INC", argF(a, argNum), 0, kwFlagPhysical), perStat("Int", toNumber(argS(a, cap2))))}
		},
		// ModParser.lua:5539 ["travel skills other than ([%a%s]+) are disabled"] = function(_, name) return {
		//   flag("DisableSkill", { type = "SkillType", skillType = SkillType.Travel }), flag("EnableSkill", { type = "SkillId", skillId = gemIdLookup[name] }) } end
		"travel skills other than ([%a%s]+) are disabled": func(p *Parser, a []any) any {
			skill := map[string]any{"type": "SkillId"}
			if id, ok := p.t.exact["gemIdLookup"][argS(a, cap1)]; ok {
				skill[keySkillID] = id
			}
			return []any{flagMod("DisableSkill", skillTypeTag(skillTypeTravel)), flagMod("EnableSkill", skill)}
		},
		// ModParser.lua:3313 ["socketed vaal skills require (%d+)%% less souls per use"] = function(num) return {
		//   mod("ExtraSkillMod", "LIST", { mod = mod("SoulCost", "MORE", -num) }, { type = "SocketedIn", slotName = "{SlotName}" },
		//   { type = "SkillType", skillType = SkillType.Vaal }) } end
		"socketed vaal skills require (%d+)%% less souls per use": func(_ *Parser, a []any) any {
			return []any{modifierOf("ExtraSkillMod", newMod("SoulCost", "MORE", -argF(a, argNum), 0, 0),
				map[string]any{"type": "SocketedIn", "slotName": "{SlotName}"}, skillTypeTag(skillTypeVaal))}
		},
		// ModParser.lua:3263 ["(%d+)%% chance to [c?t?][a?r?][s?i?][t?g?]g?e?r? socketed spells when you spend at least (%d+) life on an upfront cost
		//   to use or trigger a skill, with a ([%d%.]+) second cooldown"] = function(num, _, amount, _) return {
		//   mod("FoulbornKitavaTriggerChance", "BASE", num, "Kitava's Thirst"),
		//   mod("FoulbornKitavaRequiredLifeCost", "BASE", tonumber(amount), "Kitava's Thirst"),
		//   mod("ExtraSupport", "LIST", { skillId = "SupportCastOnLifeSpent", level = 1 }, { type = "SocketedIn", slotName = "{SlotName}" }) } end
		"(%d+)%% chance to [c?t?][a?r?][s?i?][t?g?]g?e?r? socketed spells when you spend at least (%d+) life on an upfront cost to use or trigger a skill, with a ([%d%.]+) second cooldown": func(_ *Parser, a []any) any {
			support := newMod("ExtraSupport", "LIST", map[string]any{keySkillID: "SupportCastOnLifeSpent", "level": 1.0}, 0, 0)
			return []any{
				sourced(newMod("FoulbornKitavaTriggerChance", "BASE", arg(a, argNum), 0, 0), "Kitava's Thirst"),
				sourced(newMod("FoulbornKitavaRequiredLifeCost", "BASE", toNumber(argS(a, cap2)), 0, 0), "Kitava's Thirst"),
				withTags(support, map[string]any{"type": "SocketedIn", "slotName": "{SlotName}"}),
			}
		},
		// ModParser.lua:3785 ["base (%a+) duration is ([%d%.]+) seconds?"] = function(_, ailment, num)
		//   local ailmentName = (ailment == "bleeding" or ailment == "bleed") and "Bleed" or (ailment == "ignite" or ailment == "poison") and firstToUpper(ailment)
		//   if ailmentName then return { mod(ailmentName.."DurationBase", "OVERRIDE", tonumber(num)) } end end
		"base (%a+) duration is ([%d%.]+) seconds?": func(_ *Parser, a []any) any {
			var name string
			switch ailment := argS(a, cap1); ailment {
			case "bleeding", "bleed":
				name = "Bleed"
			case "ignite", "poison":
				name = firstToUpper(ailment)
			default:
				return nil
			}
			return []any{newMod(name+"DurationBase", "OVERRIDE", toNumber(argS(a, cap2)), 0, 0)}
		},
	})
}
