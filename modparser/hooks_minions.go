package modparser

// Minion, ally and totem lines of ModParser.lua specialModList.

// minionPerDex is function(num, _, div) return { mod("MinionModifier", "LIST",
// { mod = m }, { type = "PerStat", stat = "Dex", div = tonumber(div) }) } end.
func minionPerDex(name string, flags uint64) Hook {
	return func(_ *Parser, a []any) any {
		return []any{
			modifierOf(
				"MinionModifier",
				newMod(name, "INC", argF(a, argNum), flags, 0),
				perStat("Dex", toNumber(argS(a, cap2))),
			),
		}
	}
}

// allyAuraPerStat is function(num, _, div) return { mod("ExtraAura", "LIST",
// { onlyAllies = true, mod = mod(name, typ, num, nil, flags) }, { type = "PerStat", stat = stat, div = tonumber(div) }) } end.
func allyAuraPerStat(name, typ string, flags uint64, stat string) Hook {
	return func(_ *Parser, a []any) any {
		aura := map[string]any{"onlyAllies": true, "mod": newMod(name, typ, argF(a, argNum), flags, 0)}
		return []any{withTags(newMod("ExtraAura", "LIST", aura, 0, 0), perStat(stat, toNumber(argS(a, cap2))))}
	}
}

func init() {
	registerSpecial(map[string]Hook{
		// ModParser.lua:4536 ["(%d+)%% increased minion attack speed per (%d+) dexterity"] = function(num, _, div) return {
		//   mod("MinionModifier", "LIST", { mod = mod("Speed", "INC", num, nil, ModFlag.Attack) }, { type = "PerStat", stat = "Dex", div = tonumber(div) }) } end
		"(%d+)%% increased minion attack speed per (%d+) dexterity": minionPerDex("Speed", modFlagAttack),
		// ModParser.lua:4537 ["(%d+)%% increased minion movement speed per (%d+) dexterity"]: mod("MovementSpeed", "INC", num), same tag
		"(%d+)%% increased minion movement speed per (%d+) dexterity": minionPerDex("MovementSpeed", 0),
		// ModParser.lua:4538 ["minions deal (%d+)%% increased damage per (%d+) dexterity"]: mod("Damage", "INC", num), same tag
		"minions deal (%d+)%% increased damage per (%d+) dexterity": minionPerDex("Damage", 0),
		// ModParser.lua:4584 ["minions deal no non%-(%a+) damage"] = function(_, dmgType) return dealNoNonDamageType(dmgType, true) end
		// dealNoNonDamageType (ModParser.lua:2032) with forMinion wraps each flag in mod("MinionModifier", "LIST", { mod = dealNo }).
		"minions deal no non%-(%a+) damage": func(p *Parser, a []any) any {
			mods, _ := dealNoNonDamageType(p, a).([]any)
			if mods == nil {
				return nil
			}
			out := make([]any, len(mods))
			for i, m := range mods {
				mm, _ := m.(map[string]any)
				out[i] = modifierOf("MinionModifier", mm)
			}
			return out
		},
		// ModParser.lua:4569 ["raised spectres fire (%d+) additional projectiles"] = function(num) return {
		//   mod("MinionModifier", "LIST", { mod = mod("ProjectileCount", "BASE", tonumber(num) )},
		//   { type = "SkillName", skillName = "Raise Spectre", includeTransfigured = true })} end
		"raised spectres fire (%d+) additional projectiles": func(_ *Parser, a []any) any {
			return []any{modifierOf("MinionModifier", newMod("ProjectileCount", "BASE", argF(a, argNum), 0, 0),
				map[string]any{"type": "SkillName", "skillName": "Raise Spectre", "includeTransfigured": true})}
		},
		// ModParser.lua:4645 ["summoned arbalists convert (%d+)%% of (.+) damage to (.+) damage"] = function(num, _, source, target) return {
		//   mod("MinionModifier", "LIST", { mod = mod(Source .. "DamageConvertTo" .. Target, "BASE", num) },
		//   { type = "SkillName", skillName = "Summon Arbalists" }) } end
		"summoned arbalists convert (%d+)%% of (.+) damage to (.+) damage": func(_ *Parser, a []any) any {
			return []any{minionConvert(a, map[string]any{"type": "SkillName", "skillName": "Summon Arbalists"})}
		},
		// ModParser.lua:4466 ["your linked minions take (%d+)%% less damage"] = function(num) return {
		//   mod("MinionModifier", "LIST", { mod = mod("DamageTaken", "MORE", -num, { type = "Condition", var = "AffectedByLink"}) }) } end
		"your linked minions take (%d+)%% less damage": func(_ *Parser, a []any) any {
			return []any{
				modifierOf(
					"MinionModifier",
					withTags(newMod("DamageTaken", "MORE", -argF(a, argNum), 0, 0), condTag("AffectedByLink")),
				),
			}
		},
		// ModParser.lua:4249 ["nearby allies have (%d+)%% increased defences per (%d+) strength you have"] = function(num, _, div) return {
		//   mod("ExtraAura", "LIST", { onlyAllies = true, mod = mod("Defences", "INC", num) }, { type = "PerStat", stat = "Str", div = tonumber(div) }) } end
		"nearby allies have (%d+)%% increased defences per (%d+) strength you have": allyAuraPerStat(
			"Defences",
			"INC",
			0,
			"Str",
		),
		// ModParser.lua:4250 ["nearby allies have %+(%d+)%% to critical strike multiplier per (%d+) dexterity you have"]:
		//   mod("CritMultiplier", "BASE", num), PerStat Dex
		"nearby allies have %+(%d+)%% to critical strike multiplier per (%d+) dexterity you have": allyAuraPerStat(
			"CritMultiplier",
			"BASE",
			0,
			"Dex",
		),
		// ModParser.lua:4251 ["nearby allies have (%d+)%% increased cast speed per (%d+) intelligence you have"]:
		//   mod("Speed", "INC", num, nil, ModFlag.Cast), PerStat Int
		"nearby allies have (%d+)%% increased cast speed per (%d+) intelligence you have": allyAuraPerStat(
			"Speed",
			"INC",
			modFlagCast,
			"Int",
		),
		// ModParser.lua:6047 ["nearby allies have (%d+)%% chance to block attack damage per (%d+) strength you have"] = function(block, _, str) return {
		//   mod("ExtraAura", "LIST", { onlyAllies = true, mod = mod("BlockChance", "BASE", block) }, { type = "PerStat", stat = "Str", div = tonumber(str) }) } end
		"nearby allies have (%d+)%% chance to block attack damage per (%d+) strength you have": allyAuraPerStat(
			"BlockChance",
			"BASE",
			0,
			"Str",
		),
		// ModParser.lua:4510 ["totems gain %+(%d+)%% to (%w+) resistance"] = function(num, _, resistance) return {
		//   mod("Totem"..firstToUpper(resistance).."Resist", "BASE", num) } end
		"totems gain %+(%d+)%% to (%w+) resistance": func(_ *Parser, a []any) any {
			return []any{newMod("Totem"+firstToUpper(argS(a, cap2))+"Resist", "BASE", argF(a, argNum), 0, 0)}
		},
	})
}

func init() {
	registerSpecial(map[string]Hook{
		// ModParser.lua:5827 ["minions attacks overwhelm (%d+)%% physical damage reduction"] = function(num) return {
		//   mod("MinionModifier", "LIST", { mod = mod("EnemyPhysicalDamageReduction", "BASE", -num, { type = "SkillType", skillType = SkillType.Attack }) }) } end
		"minions attacks overwhelm (%d+)%% physical damage reduction": func(_ *Parser, a []any) any {
			return []any{
				modifierOf(
					"MinionModifier",
					withTags(newMod("EnemyPhysicalDamageReduction", "BASE", -argF(a, argNum), 0, 0),
						skillTypeTag(skillTypeAttack)),
				),
			}
		},
		// ModParser.lua:4838 ["minions take (%d+)%% reduced damage"] = function(num) return {
		//   mod("MinionModifier", "LIST", { mod = mod("DamageTaken", "INC", -num) }) } end
		"minions take (%d+)%% reduced damage": func(_ *Parser, a []any) any {
			return []any{modifierOf("MinionModifier", newMod("DamageTaken", "INC", -argF(a, argNum), 0, 0))}
		},
		// ModParser.lua:2996 ["corpses you spawn have (%d+)%% reduced maximum life"] = function(num) return { mod("CorpseLife", "INC", -num) } end
		"corpses you spawn have (%d+)%% reduced maximum life": negNum("CorpseLife", "INC"),
	})
}
