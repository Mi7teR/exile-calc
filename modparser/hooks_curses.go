package modparser

// Curse, hex and offering lines of ModParser.lua specialModList.

// cursedEnemyFlag is mod("EnemyModifier", "LIST", { mod = flag(name,
// { type = "MultiplierThreshold", var = "CurseExpired", threshold = tonumber(num) }, { type = "ActorCondition", var = "Cursed" }) }).
func cursedEnemyFlag(name string) Hook {
	return func(_ *Parser, a []any) any {
		return []any{modifierOf("EnemyModifier", flagMod(name, thresholdTag("CurseExpired", argF(a, argNum)),
			map[string]any{"type": "ActorCondition", "var": "Cursed"}))}
	}
}

func init() {
	registerSpecial(map[string]Hook{
		// ModParser.lua:4121 ["your curses have (%d+)%% increased effect if (%d+)%% of curse duration expired"] = function(num, _, limit) return {
		//   mod("CurseEffect", "INC", num, { type = "MultiplierThreshold", actor = "enemy", var = "CurseExpired", threshold = tonumber(limit) },
		//   { type = "SkillType", skillType = SkillType.Hex }) } end
		"your curses have (%d+)%% increased effect if (%d+)%% of curse duration expired": func(_ *Parser, a []any) any {
			expired := thresholdTag("CurseExpired", toNumber(argS(a, cap2)))
			expired["actor"] = "enemy"
			return []any{
				withTags(newMod("CurseEffect", "INC", argF(a, argNum), 0, 0), expired, skillTypeTag(skillTypeHex)),
			}
		},
		// ModParser.lua:4124 ["non%-aura hexes expire upon reaching (%d+)%% of base effect non%-aura hexes gain (%d+)%% increased effect per second"] =
		//   function(limit, _, num) return { mod("CurseEffect", "INC", tonumber(num),
		//   { type = "Multiplier", actor = "enemy", var = "CurseDurationExpired", limit = tonumber(limit), limitTotal = true },
		//   { type = "SkillType", skillType = SkillType.Aura, neg = true }, { type = "SkillType", skillType = SkillType.Hex }) } end
		"non%-aura hexes expire upon reaching (%d+)%% of base effect non%-aura hexes gain (%d+)%% increased effect per second": func(_ *Parser, a []any) any {
			aura := skillTypeTag(skillTypeAura)
			aura["neg"] = true
			return []any{
				withTags(
					newMod("CurseEffect", "INC", toNumber(argS(a, cap2)), 0, 0),
					map[string]any{
						"type":       "Multiplier",
						"actor":      "enemy",
						"var":        "CurseDurationExpired",
						"limit":      argF(a, argNum),
						"limitTotal": true,
					},
					aura,
					skillTypeTag(skillTypeHex),
				),
			}
		},
		// ModParser.lua:4127 ["enemies cursed by you have malediction if (%d+)%% of curse duration expired"] = function(num) return {
		//   mod("EnemyModifier", "LIST", { mod = flag("HasMalediction", { type = "MultiplierThreshold", var = "CurseExpired", threshold = tonumber(num) },
		//   { type = "ActorCondition", var = "Cursed" }) }) } end
		"enemies cursed by you have malediction if (%d+)%% of curse duration expired": cursedEnemyFlag(
			"HasMalediction",
		),
		// ModParser.lua:4130 ["enemies cursed by you are hindered if (%d+)%% of curse duration expired"]: flag("Condition:Hindered", <same tags>)
		"enemies cursed by you are hindered if (%d+)%% of curse duration expired": cursedEnemyFlag(
			"Condition:Hindered",
		),
		// ModParser.lua:4206 ["temporal chains has (%d+)%% reduced effect on you"] = function(num) return {
		//   mod("CurseEffectOnSelf", "INC", -num, { type = "SkillName", skillName = "Temporal Chains" }) } end
		"temporal chains has (%d+)%% reduced effect on you": negNum(
			"CurseEffectOnSelf",
			"INC",
			map[string]any{"type": "SkillName", "skillName": "Temporal Chains"},
		),
		// ModParser.lua:5680 ["offering skills have (%d+)%% reduced duration"] = function(num) return {
		//   mod("Duration", "INC", -num, { type = "SkillName", skillNameList = { "Bone Offering", "Flesh Offering", "Spirit Offering", "Blood Offering" } }) } end
		"offering skills have (%d+)%% reduced duration": negNum(
			"Duration",
			"INC",
			map[string]any{
				"type":          "SkillName",
				"skillNameList": []any{"Bone Offering", "Flesh Offering", "Spirit Offering", "Blood Offering"},
			},
		),
	})
}
