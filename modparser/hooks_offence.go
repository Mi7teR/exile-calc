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

func init() { //nolint:funlen // one registration table of ported Lua functions
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
		"(%d+)%% chance to throw up to (%d+) additional traps? or mines?": trapThrowChance(
			"TrapThrowCount",
			"MineThrowCount",
		),
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
			return []any{
				withTags(
					newMod("Enemy"+firstToUpper(argS(a, cap1))+"Chance", "BASE", 100.0, modFlagHit, 0),
					skillTypeTag(skillTypeRandElem),
				),
			}
		},
		// ModParser.lua:5708 ["impale damage dealt to enemies impaled by you overwhelms (%d+)%% physical damage reduction"] = function(num) return {
		//   mod("EnemyImpalePhysicalDamageReduction", "BASE", -num) } end
		"impale damage dealt to enemies impaled by you overwhelms (%d+)%% physical damage reduction": negNum(
			"EnemyImpalePhysicalDamageReduction",
			"BASE",
		),
		// ModParser.lua:4403 ["inflict fire exposure on hit against enemies with (%d+) cinderflame, applying %-(%d+)%% to (%w+) resistance"] =
		//   function(cinderflame, _, num, element) return {
		//   mod("FireExposureChance", "BASE", 100, { type = "Condition", var = "Effective" }, { type = "MultiplierThreshold", var = "CinderflameStacks", threshold = cinderflame }),
		//   mod("EnemyModifier", "LIST", { mod = mod(firstToUpper(element).."Exposure", "BASE", -num) }, <same two tags>) } end
		"inflict fire exposure on hit against enemies with (%d+) cinderflame, applying %-(%d+)%% to (%w+) resistance": func(_ *Parser, a []any) any {
			stacks := func() map[string]any { return thresholdTag("CinderflameStacks", arg(a, argNum)) }
			return []any{
				withTags(newMod("FireExposureChance", "BASE", 100.0, 0, 0), condTag("Effective"), stacks()),
				modifierOf(
					"EnemyModifier",
					newMod(firstToUpper(argS(a, cap3))+"Exposure", "BASE", -argF(a, cap2), 0, 0),
					condTag("Effective"),
					stacks(),
				),
			}
		},
		// ModParser.lua:2689 ["projectiles deal (%d+)%% increased damage with hits and ailments for each remaining chain, up to a maximum of (%d+)%%"] =
		//   function(num, _, limit) return { mod("Damage", "INC", num, nil, 0, bor(KeywordFlag.Hit, KeywordFlag.Ailment),
		//   { type = "PerStat", stat = "ChainRemaining", globalLimit = tonumber(limit), globalLimitKey = "FollowThrough" },
		//   { type = "SkillType", skillType = SkillType.Projectile }) } end
		"projectiles deal (%d+)%% increased damage with hits and ailments for each remaining chain, up to a maximum of (%d+)%%": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("Damage", "INC", argF(a, argNum), 0, kwFlagHit|kwFlagAilment),
					map[string]any{
						"type":           "PerStat",
						"stat":           "ChainRemaining",
						"globalLimit":    toNumber(argS(a, cap2)),
						"globalLimitKey": "FollowThrough",
					},
					skillTypeTag(skillTypeProj),
				),
			}
		},
		// ModParser.lua:2934 ["gain (%d+)%% of (%a+) damage as extra (%a+) damage for each of your hallowing flames that have been removed by an allied hit recently,
		//   up to (%d+)%%"] = function(num, _, fromType, destType, limit) return { mod(From .. "DamageGainAs" .. Dest, "BASE", num,
		//   { type = "ActorCondition", actor = "enemy", var = "HallowingFlame" },
		//   { type = "Multiplier", var = "HallowingFlameStacksRemovedByAlly", limit = tonumber(limit) / num }) } end
		"gain (%d+)%% of (%a+) damage as extra (%a+) damage for each of your hallowing flames that have been removed by an allied hit recently, up to (%d+)%%": func(_ *Parser, a []any) any {
			n := argF(a, argNum)
			return []any{
				withTags(
					newMod(firstToUpper(argS(a, cap2))+"DamageGainAs"+firstToUpper(argS(a, cap3)), "BASE", n, 0, 0),
					map[string]any{"type": "ActorCondition", "actor": "enemy", "var": "HallowingFlame"},
					map[string]any{
						"type":  "Multiplier",
						"var":   "HallowingFlameStacksRemovedByAlly",
						"limit": argF(a, cap4) / n,
					},
				),
			}
		},
		// ModParser.lua:4146 ["(%d+)%% increased area of effect per second you've been stationary, up to a maximum of (%d+)%%"] = function(num, _, limit) return {
		//   mod("AreaOfEffect", "INC", num, { type = "Multiplier", var = "StationarySeconds", globalLimit = tonumber(limit), globalLimitKey = "ExpansiveMight",
		//   limitTotal = true }) } end
		"(%d+)%% increased area of effect per second you've been stationary, up to a maximum of (%d+)%%": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("AreaOfEffect", "INC", argF(a, argNum), 0, 0),
					map[string]any{"type": "Multiplier", "var": "StationarySeconds",
						"globalLimit": toNumber(argS(a, cap2)), "globalLimitKey": "ExpansiveMight", "limitTotal": true},
				),
			}
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
			return explodeFunc(
				100,
				argS(a, cap1),
				argS(a, cap2),
				map[string]any{
					"type":  "ActorCondition",
					"actor": "enemy",
					"var":   "Taunted",
				},
				condTag("UsedWarcryRecently"),
			)
		},
		// ModParser.lua:5959 ["%+([%d%.]+) metres? to weapon range per 10%% quality"] = function(num) return {
		//   mod("AlternateQualityLocalWeaponRangePer10Quality", "BASE", num * 10) } end
		"%+([%d%.]+) metres? to weapon range per 10%% quality": func(_ *Parser, a []any) any {
			return []any{newMod("AlternateQualityLocalWeaponRangePer10Quality", "BASE", argF(a, argNum)*10, 0, 0)}
		},
		// ModParser.lua:6050 ["physical skills have (%d+)%% increased duration per (%d+) intelligence"] = function(num1, _, num2)
		//   return { mod("Duration", "INC", num1, nil, nil, KeywordFlag.Physical, { type = "PerStat", stat = "Int", div = tonumber(num2) }) } end
		"physical skills have (%d+)%% increased duration per (%d+) intelligence": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("Duration", "INC", argF(a, argNum), 0, kwFlagPhysical),
					perStat("Int", toNumber(argS(a, cap2))),
				),
			}
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
			support := newMod(
				"ExtraSupport",
				"LIST",
				map[string]any{keySkillID: "SupportCastOnLifeSpent", "level": 1.0},
				0,
				0,
			)
			return []any{
				sourced(newMod("FoulbornKitavaTriggerChance", "BASE", arg(a, argNum), 0, 0), "Kitava's Thirst"),
				sourced(
					newMod("FoulbornKitavaRequiredLifeCost", "BASE", toNumber(argS(a, cap2)), 0, 0),
					"Kitava's Thirst",
				),
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

// skillNames is { type = "SkillName", skillNameList = { names... } } (with
// includeTransfigured = true when transfigured is set).
func skillNames(transfigured bool, names ...string) map[string]any {
	l := make([]any, len(names))
	for i, n := range names {
		l[i] = n
	}
	t := map[string]any{"type": "SkillName", "skillNameList": l}
	if transfigured {
		t[keyIncludeTransfigured] = true
	}
	return t
}

// skillName is { type = "SkillName", skillName = name, includeTransfigured = true }.
func skillName(name string) map[string]any {
	return map[string]any{"type": "SkillName", "skillName": name, keyIncludeTransfigured: true}
}

// chanceMore is function(num, _, more) return { mod("Damage", "MORE",
// tonumber(more) * num / 100, nil, 0, kw, skillNames) } end.
func chanceMore(kw uint64, names map[string]any) Hook {
	return func(_ *Parser, a []any) any {
		return []any{withTags(newMod("Damage", "MORE", argF(a, cap2)*argF(a, argNum)/100, 0, kw), names)}
	}
}

func init() { //nolint:funlen // one registration table of ported Lua functions
	const kwIgnite = 0x800000
	const kwWarcry = 0x4
	const skillTypeTriggerable = 32.0
	const skillTypeTriggered = 37.0
	const skillTypeInstant = 64.0
	const metresToUnits = 10
	registerSpecial(map[string]Hook{
		// ModParser.lua:5952 ["%+([%d%.]+) metres? to discharge radius"] = function(num) return {
		//   mod("AreaOfEffect", "BASE", num * 10, { type = "SkillName", skillName = "Discharge", includeTransfigured = true }) } end
		"%+([%d%.]+) metres? to discharge radius": func(_ *Parser, a []any) any {
			return []any{
				withTags(newMod("AreaOfEffect", "BASE", argF(a, argNum)*metresToUnits, 0, 0), skillName("Discharge")),
			}
		},
		// ModParser.lua:5951 ["chain hook has %+([%d%.]+) metres? to radius per (%d+) rage"] = function(num, _, rage) return {
		//   mod("AreaOfEffect", "BASE", num * 10, { type = "PerStat", stat = "Rage", div = tonumber(rage) },
		//   { type = "SkillName", skillName = "Chain Hook", includeTransfigured = true }) } end
		"chain hook has %+([%d%.]+) metres? to radius per (%d+) rage": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("AreaOfEffect", "BASE", argF(a, argNum)*metresToUnits, 0, 0),
				perStat("Rage", toNumber(argS(a, cap2))), skillName("Chain Hook"))}
		},
		// ModParser.lua:3612 ["%-(%d+)%% of toxic rain physical damage converted to chaos damage"] = function(num) return {
		//   mod("SkillPhysicalDamageConvertToChaos", "BASE", -num, 0, 0, { type = "SkillName", skillName = "Toxic Rain", includeTransfigured = true }) } end
		"%-(%d+)%% of toxic rain physical damage converted to chaos damage": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("SkillPhysicalDamageConvertToChaos", "BASE", -argF(a, argNum), 0, 0),
					skillName("Toxic Rain"),
				),
			}
		},
		// ModParser.lua:3615 ["cobra lash and venom gyre have %-(%d+)%% of physical damage converted to chaos damage"] = function(num) return {
		//   mod("SkillPhysicalDamageConvertToChaos", "BASE", -num, 0, 0, { type = "SkillName", skillNameList = { "Cobra Lash", "Venom Gyre" } }) } end
		"cobra lash and venom gyre have %-(%d+)%% of physical damage converted to chaos damage": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("SkillPhysicalDamageConvertToChaos", "BASE", -argF(a, argNum), 0, 0),
				skillNames(false, "Cobra Lash", "Venom Gyre"))}
		},
		// ModParser.lua:5159 ["(%a+) resistance cannot be penetrated"] = function(_, res) return {
		//   flag("EnemyCannotPen"..(res:gsub("^%l", string.upper)).."Resistance") } end
		"(%a+) resistance cannot be penetrated": func(_ *Parser, a []any) any {
			return []any{flagMod("EnemyCannotPen" + firstToUpper(argS(a, cap1)) + "Resistance")}
		},
		// ModParser.lua:3965 bleeding with cobra lash or venom gyre: KeywordFlag.Bleed, { skillNameList = { "Cobra Lash", "Venom Gyre" } }
		"(%d+)%% chance for bleeding inflicted with cobra lash or venom gyre to deal (%d+)%% more damage": chanceMore(
			kwFlagBleed, skillNames(false, "Cobra Lash", "Venom Gyre")),
		// ModParser.lua:3796 ignites with lightning strike or frost blades: KeywordFlag.Ignite, includeTransfigured
		"(%d+)%% chance for ignites inflicted with lightning strike or frost blades to deal (%d+)%% more damage": chanceMore(
			kwIgnite,
			skillNames(true, "Lightning Strike", "Frost Blades"),
		),
		// ModParser.lua:3799 ignites with lightning arrow or ice shot: KeywordFlag.Ignite, includeTransfigured
		"(%d+)%% chance for ignites inflicted with lightning arrow or ice shot to deal (%d+)%% more damage": chanceMore(
			kwIgnite, skillNames(true, "Lightning Arrow", "Ice Shot")),
		// ModParser.lua:3802 ignites with shock nova or storm call: KeywordFlag.Ignite, includeTransfigured
		"(%d+)%% chance for ignites inflicted with shock nova or storm call to deal (%d+)%% more damage": chanceMore(
			kwIgnite, skillNames(true, "Shock Nova", "Storm Call")),
		// ModParser.lua:3253 ["(%d+)%% chance to [c?t?][a?r?][s?i?][t?g?]g?e?r? socketed spells when you spend at least (%d+) mana to use a skill"] =
		//   function(num, _, amount) return { mod("KitavaTriggerChance", "BASE", num, "Kitava's Thirst"),
		//   mod("KitavaRequiredManaCost", "BASE", tonumber(amount), "Kitava's Thirst"),
		//   mod("ExtraSupport", "LIST", { skillId = "SupportCastOnManaSpent", level = 1 }, { type = "SocketedIn", slotName = "{SlotName}" }) } end
		"(%d+)%% chance to [c?t?][a?r?][s?i?][t?g?]g?e?r? socketed spells when you spend at least (%d+) mana to use a skill": func(_ *Parser, a []any) any {
			return []any{
				sourced(newMod("KitavaTriggerChance", "BASE", arg(a, argNum), 0, 0), "Kitava's Thirst"),
				sourced(newMod("KitavaRequiredManaCost", "BASE", toNumber(argS(a, cap2)), 0, 0), "Kitava's Thirst"),
				withTags(
					newMod(
						"ExtraSupport",
						"LIST",
						map[string]any{keySkillID: "SupportCastOnManaSpent", "level": 1.0},
						0,
						0,
					),
					map[string]any{"type": "SocketedIn", "slotName": "{SlotName}"},
				),
			}
		},
		// ModParser.lua:4493 ["(%d+)%% chance to throw up to (%d+) additional mines?"] = function(chance, _, num) return {
		//   mod("MineThrowCount", "BASE", tonumber(num) * tonumber(chance) / 100.0) } end
		"(%d+)%% chance to throw up to (%d+) additional mines?": trapThrowChance("MineThrowCount"),
		// ModParser.lua:2650 ["(%d+)%% more damage for each endurance charge lost recently, up to (%d+)%%"] = function(num, _, limit) return {
		//   mod("Damage", "MORE", num, { type = "Multiplier", var = "EnduranceChargesLostRecently", limit = tonumber(limit), limitTotal = true }) } end
		"(%d+)%% more damage for each endurance charge lost recently, up to (%d+)%%": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("Damage", "MORE", arg(a, argNum), 0, 0), map[string]any{"type": "Multiplier",
				"var": "EnduranceChargesLostRecently", "limit": toNumber(argS(a, cap2)), "limitTotal": true})}
		},
		// ModParser.lua:4686 ["(%d+)%% reduced essence drain and soulrend projectile speed"] = function(num) return {
		//   mod("ProjectileSpeed", "INC", -num, nil, { type = "SkillName", skillNameList = { "Essence Drain", "Soulrend" }, includeTransfigured = true }) } end
		// The tag sits in the flags position after the nil source: createMod
		// leaves mod[1] nil and the tag at mod[2], which EvalMod's ipairs
		// never reaches (as for Sacrificial Zeal's overwhelm).
		"(%d+)%% reduced essence drain and soulrend projectile speed": func(_ *Parser, a []any) any {
			m := newMod("ProjectileSpeed", "INC", -argF(a, argNum), 0, 0)
			m["@list"] = []any{nil, skillNames(true, "Essence Drain", "Soulrend")}
			return []any{m}
		},
		// ModParser.lua:5920 ["arcane cloak grants life regeneration equal to (%d+)%% of mana spent per second"] = function(num) return {
		//   mod("ExtraSkillMod", "LIST", { mod = mod("LifeRegen", "BASE", num / 100, 0, 0, { type = "Multiplier", var = "ArcaneCloakConsumedMana" },
		//   { type = "GlobalEffect", effectType = "Buff" }) }, { type = "SkillName", skillName = "Arcane Cloak" }) } end
		"arcane cloak grants life regeneration equal to (%d+)%% of mana spent per second": func(_ *Parser, a []any) any {
			regen := withTags(
				newMod("LifeRegen", "BASE", argF(a, argNum)/100, 0, 0),
				map[string]any{
					"type": "Multiplier",
					"var":  "ArcaneCloakConsumedMana",
				},
				map[string]any{"type": "GlobalEffect", "effectType": "Buff"},
			)
			return []any{
				modifierOf("ExtraSkillMod", regen, map[string]any{"type": "SkillName", "skillName": "Arcane Cloak"}),
			}
		},
		// ModParser.lua:4137 ["aura buffs from skills have (%d+)%% increased effect on you for each herald affecting you, up to (%d+)%%"] and
		// 4140 ["auras from your skills have ..."] = function(num, _, limit) return { mod("SkillAuraEffectOnSelf", "INC", num,
		//   { type = "Multiplier", var = "Herald", globalLimit = tonumber(limit), globalLimitKey = "PurposefulHarbinger" }) } end
		"aura buffs from skills have (%d+)%% increased effect on you for each herald affecting you, up to (%d+)%%": heraldAuraEffect,
		"auras from your skills have (%d+)%% increased effect on you for each herald affecting you, up to (%d+)%%": heraldAuraEffect,
		// ModParser.lua:5683 ["enemies have %-(%d+)%% to total physical damage reduction against your hits"] = function(num) return {
		//   mod("EnemyPhysicalDamageReduction", "BASE", -num) } end
		"enemies have %-(%d+)%% to total physical damage reduction against your hits": negNum(
			"EnemyPhysicalDamageReduction",
			"BASE",
		),
		// ModParser.lua:5686 ["enemies you impale have %-(%d+)%% to total physical damage reduction against impale hits"] = function(num) return {
		//   mod("EnemyImpalePhysicalDamageReduction", "BASE", -num) } end
		"enemies you impale have %-(%d+)%% to total physical damage reduction against impale hits": negNum(
			"EnemyImpalePhysicalDamageReduction", "BASE"),
		// ModParser.lua:5692 ["overwhelm (%d+)%% physical damage reduction"] = function(num) return { mod("EnemyPhysicalDamageReduction", "BASE", -num) } end
		"overwhelm (%d+)%% physical damage reduction": negNum("EnemyPhysicalDamageReduction", "BASE"),
		// ModParser.lua:5689 ["hits with this weapon overwhelm (%d+)%% physical damage reduction"] = function(num) return {
		//   mod("EnemyPhysicalDamageReduction", "BASE", -num, nil, ModFlag.Hit, { type = "Condition", var = "{Hand}Attack" },
		//   { type = "SkillType", skillType = SkillType.Attack }) } end
		"hits with this weapon overwhelm (%d+)%% physical damage reduction": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("EnemyPhysicalDamageReduction", "BASE", -argF(a, argNum), modFlagHit, 0),
				condTag("{Hand}Attack"), skillTypeTag(skillTypeAttack))}
		},
		// ModParser.lua:5818 ["hits overwhelm (%d+)%% of physical damage reduction while you have sacrificial zeal"] = function(num) return {
		//   mod("EnemyPhysicalDamageReduction", "BASE", -num, nil, { type = "Condition", var = "SacrificialZeal" }) } end
		// The tag table sits in the flags position after the nil source, so
		// createMod leaves mod[1] nil and the tag at mod[2]: EvalMod's ipairs
		// stops at the hole and never reads the condition.
		"hits overwhelm (%d+)%% of physical damage reduction while you have sacrificial zeal": func(_ *Parser, a []any) any {
			m := newMod("EnemyPhysicalDamageReduction", "BASE", -argF(a, argNum), 0, 0)
			m["@list"] = []any{nil, condTag("SacrificialZeal")}
			return []any{m}
		},
		// ModParser.lua:5879 ["frozen sweep deals (%d+)%% less damage"] = function(num) return {
		//   mod("Damage", "MORE", -num, { type = "SkillName", skillName = "Frozen Sweep", includeTransfigured = true }) } end
		"frozen sweep deals (%d+)%% less damage": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("Damage", "MORE", -argF(a, argNum), 0, 0), skillName("Frozen Sweep"))}
		},
		// ModParser.lua:5837 ["flamethrower, seismic and lightning spire trap have %-(%d+) cooldown uses?"] = function(num) return {
		//   mod("AdditionalCooldownUses", "BASE", -num, { type = "SkillName", skillNameList = { "Flamethrower Trap", "Seismic Trap",
		//   "Lightning Spire Trap" }, includeTransfigured = true }) } end
		"flamethrower, seismic and lightning spire trap have %-(%d+) cooldown uses?": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("AdditionalCooldownUses", "BASE", -argF(a, argNum), 0, 0),
				skillNames(true, "Flamethrower Trap", "Seismic Trap", "Lightning Spire Trap"))}
		},
		// ModParser.lua:5834 ["focus has (%d+)%% reduced cooldown recovery rate"] = function(num) return {
		//   mod("FocusCooldownRecovery", "INC", -num, { type = "Condition", var = "Focused" }) } end
		"focus has (%d+)%% reduced cooldown recovery rate": negNum("FocusCooldownRecovery", "INC", condTag("Focused")),
		// ModParser.lua:3642 ["non%-critical strikes deal (%d+)%% damage"] = function(num) return {
		//   mod("Damage", "MORE", -100 + num, nil, ModFlag.Hit, { type = "Condition", var = "CriticalStrike", neg = true }) } end
		"non%-critical strikes deal (%d+)%% damage": func(_ *Parser, a []any) any {
			return []any{
				withTags(newMod("Damage", "MORE", -100+argF(a, argNum), modFlagHit, 0),
					map[string]any{"type": "Condition", "var": "CriticalStrike", "neg": true}),
			}
		},
		// ModParser.lua:5640 ["non%-instant warcries you use yourself have no cooldown"] = function(num) return {
		//   mod("CooldownRecovery", "OVERRIDE", 0, nil, 0, KeywordFlag.Warcry, { type = "SkillType", skillTypeList =
		//   { SkillType.Instant, SkillType.Totem, SkillType.Triggered }, neg = true }) } end
		// SkillType.Totem does not exist, so the list is { 64, nil, 37 }: a
		// hole at 2, which EvalMod's pairs walks past (both 64 and 37 count).
		"non%-instant warcries you use yourself have no cooldown": func(_ *Parser, _ []any) any {
			list := map[string]any{"@list": []any{skillTypeInstant}, "3": skillTypeTriggered}
			return []any{withTags(newMod("CooldownRecovery", "OVERRIDE", 0.0, 0, kwWarcry),
				map[string]any{"type": "SkillType", "skillTypeList": list, "neg": true})}
		},
		// ModParser.lua:2692 ["projectiles deal (%d+)%% increased damage with hits and ailments for each remaining chain, up to (%d+)%%"] =
		//   function(num, _, limit) return { mod("Damage", "INC", num, nil, 0, bor(KeywordFlag.Hit, KeywordFlag.Ailment),
		//   { type = "PerStat", stat = "ChainRemaining", globalLimit = tonumber(limit), globalLimitKey = "FollowThrough" },
		//   { type = "SkillType", skillType = SkillType.Projectile }) } end
		"projectiles deal (%d+)%% increased damage with hits and ailments for each remaining chain, up to (%d+)%%": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("Damage", "INC", arg(a, argNum), 0, kwFlagHit|kwFlagAilment),
					map[string]any{
						"type":           "PerStat",
						"stat":           "ChainRemaining",
						"globalLimit":    toNumber(argS(a, cap2)),
						"globalLimitKey": "FollowThrough",
					},
					skillTypeTag(skillTypeProj),
				),
			}
		},
		// ModParser.lua:5877 ["shield charge and chain hook have (%d+)%% increased attack speed per (%d+) rampage kills"] = function(inc, _, num) return {
		//   mod("Speed", "INC", inc, nil, ModFlag.Attack, { type = "Multiplier", var = "Rampage", div = num, limit = 1000 / num, limitTotal = true },
		//   { type = "SkillName", skillNameList = { "Shield Charge", "Chain Hook" }, includeTransfigured = true }) } end
		// num is the capture: the string, which Lua coerces in 1000 / num.
		"shield charge and chain hook have (%d+)%% increased attack speed per (%d+) rampage kills": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("Speed", "INC", arg(a, argNum), modFlagAttack, 0),
					map[string]any{
						"type":       "Multiplier",
						"var":        "Rampage",
						"div":        argS(a, cap2),
						"limit":      1000 / argF(a, cap2),
						"limitTotal": true,
					},
					skillNames(true, "Shield Charge", "Chain Hook"),
				),
			}
		},
		// ModParser.lua:3291 ["socketed skill gems get a (%d+)%% mana multiplier"] = function(num) return {
		//   mod("ExtraSkillMod", "LIST", { mod = mod("SupportManaMultiplier", "MORE", num - 100) }, { type = "SocketedIn", slotName = "{SlotName}" }) } end
		"socketed skill gems get a (%d+)%% mana multiplier": func(_ *Parser, a []any) any {
			return []any{
				modifierOf(
					"ExtraSkillMod",
					newMod("SupportManaMultiplier", "MORE", argF(a, argNum)-100, 0, 0),
					map[string]any{"type": "SocketedIn", "slotName": "{SlotName}"},
				),
			}
		},
		// ModParser.lua:3312 ["socketed triggered bow skills deal (%d+)%% less damage"] = function(num) return {
		//   mod("ExtraSkillMod", "LIST", { mod = mod("Damage", "MORE", -num) }, { type = "SocketedIn", slotName = "{SlotName}", keyword = "bow" },
		//   { type = "SkillType", skillType = SkillType.Triggerable }) } end
		"socketed triggered bow skills deal (%d+)%% less damage": func(_ *Parser, a []any) any {
			return []any{
				modifierOf(
					"ExtraSkillMod",
					newMod("Damage", "MORE", -argF(a, argNum), 0, 0),
					map[string]any{
						"type":     "SocketedIn",
						"slotName": "{SlotName}",
						"keyword":  "bow",
					},
					skillTypeTag(skillTypeTriggerable),
				),
			}
		},
		// ModParser.lua:2220 ["totems explode on death, dealing (%d+)%% of their life as (.+) damage"] = function(amount, _, type)
		//   return explodeFunc(100, amount, type) end
		"totems explode on death, dealing (%d+)%% of their life as (.+) damage": func(_ *Parser, a []any) any {
			return explodeFunc(100, argS(a, cap1), argS(a, cap2))
		},
		// ModParser.lua:4478 ["traps and mines deal (%d+)%-(%d+) additional physical damage"] = function(_, min, max) return {
		//   mod("PhysicalMin", "BASE", tonumber(min), nil, 0, bor(KeywordFlag.Trap, KeywordFlag.Mine)),
		//   mod("PhysicalMax", "BASE", tonumber(max), nil, 0, bor(KeywordFlag.Trap, KeywordFlag.Mine)) } end
		"traps and mines deal (%d+)%-(%d+) additional physical damage": func(_ *Parser, a []any) any {
			return []any{newMod("PhysicalMin", "BASE", toNumber(argS(a, cap1)), 0, kwFlagTrap|kwFlagMine),
				newMod("PhysicalMax", "BASE", toNumber(argS(a, cap2)), 0, kwFlagTrap|kwFlagMine)}
		},
		// ModParser.lua:2327 ["vaal volcanic fissure and vaal molten strike have (%d+)%% reduced soul gain prevention duration"] = function(num) return {
		//   mod("SoulGainPreventionDuration", "INC", -num, { type = "SkillName", skillNameList = { "Volcanic Fissure", "Molten Strike" },
		//   includeTransfigured = true }, { type = "SkillType", skillType = SkillType.Vaal }) } end
		"vaal volcanic fissure and vaal molten strike have (%d+)%% reduced soul gain prevention duration": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("SoulGainPreventionDuration", "INC", -argF(a, argNum), 0, 0),
				skillNames(true, "Volcanic Fissure", "Molten Strike"), skillTypeTag(skillTypeVaal))}
		},
		// ModParser.lua:5764 ["while stationary, gain (%d+)%% increased area of effect every second, up to a maximum of (%d+)%%"] =
		//   function(num, _, limit) return { mod("AreaOfEffect", "INC", num, { type = "Multiplier", var = "StationarySeconds",
		//   globalLimit = tonumber(limit), globalLimitKey = "ExpansiveMight" }, { type = "Condition", var = "Stationary" }) } end
		"while stationary, gain (%d+)%% increased area of effect every second, up to a maximum of (%d+)%%": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("AreaOfEffect", "INC", arg(a, argNum), 0, 0),
					map[string]any{
						"type":           "Multiplier",
						"var":            "StationarySeconds",
						"globalLimit":    toNumber(argS(a, cap2)),
						"globalLimitKey": "ExpansiveMight",
					},
					condTag("Stationary"),
				),
			}
		},
		// ModParser.lua:2338 Elemental Equilibrium (legacy wording): function(plus, _, minus) minus = tonumber(minus) return {
		//   flag("ElementalEquilibrium"), flag("ElementalEquilibriumLegacy"), and per element E with others O1, O2:
		//   mod("EnemyModifier", "LIST", { mod = mod(E.."Resist", "BASE", plus, { type = "Condition", var = "HitBy"..E.."Damage" }) }),
		//   mod("EnemyModifier", "LIST", { mod = mod(E.."Resist", "BASE", minus, { type = "Condition", var = "HitBy"..E.."Damage", neg = true },
		//   { type = "Condition", varList = { "HitBy"..O1.."Damage", "HitBy"..O2.."Damage" } }) }) } end
		"enemies you hit with elemental damage temporarily get (%+%d+)%% resistance to those elements and (%-%d+)%% resistance to other elements": func(
			_ *Parser, a []any,
		) any {
			plus, minus := arg(a, argNum), toNumber(argS(a, cap2))
			out := []any{flagMod("ElementalEquilibrium"), flagMod("ElementalEquilibriumLegacy")}
			for _, e := range [][3]string{{"Fire", "Cold", "Lightning"}, {"Cold", "Fire", "Lightning"}, {"Lightning", "Fire", "Cold"}} {
				hit := "HitBy" + e[0] + "Damage"
				out = append(out,
					modifierOf("EnemyModifier", withTags(newMod(e[0]+"Resist", "BASE", plus, 0, 0), condTag(hit))),
					modifierOf("EnemyModifier", withTags(
						newMod(e[0]+"Resist", "BASE", minus, 0, 0),
						map[string]any{"type": "Condition", "var": hit, "neg": true},
						map[string]any{
							"type":    "Condition",
							"varList": []any{"HitBy" + e[1] + "Damage", "HitBy" + e[2] + "Damage"},
						},
					)))
			}
			return out
		},
	})
}

// heraldAuraEffect is Purposeful Harbinger's SkillAuraEffectOnSelf per herald (ModParser.lua:4137, 4140).
func heraldAuraEffect(_ *Parser, a []any) any {
	return []any{
		withTags(
			newMod("SkillAuraEffectOnSelf", "INC", arg(a, argNum), 0, 0),
			map[string]any{
				"type":           "Multiplier",
				"var":            "Herald",
				"globalLimit":    toNumber(argS(a, cap2)),
				"globalLimitKey": "PurposefulHarbinger",
			},
		),
	}
}
