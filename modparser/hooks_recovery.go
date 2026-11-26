package modparser

// Recovery lines of ModParser.lua specialModList: rage, regeneration, recoup,
// leech and life gained on hit.

// burstRegen is the pair of mods of the "every N seconds, regenerate <pool>
// equal to P% of <stat> over D second" lines: the full burst and its average.
func burstRegen(name, stat string, a []any) []any {
	percent := argS(a, cap2)
	pct := map[string]any{"type": "PercentStat", "stat": stat, "percent": percent}
	pct2 := map[string]any{"type": "PercentStat", "stat": stat, "percent": percent}
	return []any{
		withTags(newMod(name, "BASE", 1.0, 0, 0), condTag("LifeRegenBurstFull"), pct),
		withTags(newMod(name, "BASE", 1/argF(a, argNum)*argF(a, cap3), 0, 0), condTag("LifeRegenBurstAvg"), pct2),
	}
}

// nearbyCorpses is { type = "Multiplier", var = "NearbyCorpse", limit = tonumber(limit), limitTotal = true }.
func nearbyCorpses(a []any) map[string]any {
	return map[string]any{
		"type":       "Multiplier",
		"var":        "NearbyCorpse",
		"limit":      toNumber(argS(a, cap2)),
		"limitTotal": true,
	}
}

func init() { //nolint:funlen // one registration table of ported Lua functions
	registerSpecial(map[string]Hook{
		// ModParser.lua:2227 ["(%d+) rage regenerated for every (%d+) mana regeneration per second"] = function(num, _, div) return {
		//   mod("RageRegen", "BASE", num, {type = "PerStat", stat = "ManaRegen", div = tonumber(div) }), flag("Condition:CanGainRage") } end
		"(%d+) rage regenerated for every (%d+) mana regeneration per second": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("RageRegen", "BASE", argF(a, argNum), 0, 0),
					perStat("ManaRegen", toNumber(argS(a, cap2))),
				),
				flagMod("Condition:CanGainRage"),
			}
		},
		// ModParser.lua:5650 ["regenerate (%d+) rage per second for every (%d+) life recovery per second from regeneration"] = function(num, _, div) return {
		//   mod("RageRegen", "BASE", num, {type = "PercentStat", stat = "LifeRegen", percent = tonumber(num/div*100) }), flag("Condition:CanGainRage") } end
		"regenerate (%d+) rage per second for every (%d+) life recovery per second from regeneration": func(_ *Parser, a []any) any {
			n := argF(a, argNum)
			return []any{
				withTags(
					newMod("RageRegen", "BASE", n, 0, 0),
					map[string]any{"type": "PercentStat", "stat": "LifeRegen", "percent": n / argF(a, cap2) * 100},
				),
				flagMod("Condition:CanGainRage"),
			}
		},
		// ModParser.lua:5659 ["inherent loss of rage is (%d+)%% slower"] = function(num) return { mod("InherentRageLoss", "INC", -num) } end
		"inherent loss of rage is (%d+)%% slower": negNum("InherentRageLoss", "INC"),
		// ModParser.lua:2236 ["(%d+)%% additional physical damage reduction for every (%d+)%% life recovery per second from leech"] = function(num, _, div)
		//   return { mod("PhysicalDamageReduction", "BASE", num, { type = "PerStat", stat = "MaxLifeLeechRatePercent", div = tonumber(div) },
		//   { type = "Condition", var = "Leeching"}) } end
		"(%d+)%% additional physical damage reduction for every (%d+)%% life recovery per second from leech": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("PhysicalDamageReduction", "BASE", argF(a, argNum), 0, 0),
				perStat("MaxLifeLeechRatePercent", toNumber(argS(a, cap2))), condTag("Leeching"))}
		},
		// ModParser.lua:2157 ["(%d+)%% increased life recovery rate per (%d+)%% missing unreserved life"] = function(num, _, div) return {
		//   mod("LifeRecoveryRate", "INC", num, { type = "PerStat", stat = "MissingUnreservedLifePercent", div = tonumber(div) }) } end
		"(%d+)%% increased life recovery rate per (%d+)%% missing unreserved life": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("LifeRecoveryRate", "INC", argF(a, argNum), 0, 0),
					perStat("MissingUnreservedLifePercent", toNumber(argS(a, cap2))),
				),
			}
		},
		// ModParser.lua:4887 ["([%d%.]+)%% of physical damage prevented from hits in the past (%d+) seconds is regenerated as life per second"] =
		//   function(num, _, duration) return { mod("PhysicalDamageMitigatedLifePseudoRecoup", "BASE", num * duration),
		//   mod("PhysicalDamageMitigatedLifePseudoRecoupDuration", "BASE", duration) } end
		// duration is the capture, a string, as in PoB.
		"([%d%.]+)%% of physical damage prevented from hits in the past (%d+) seconds is regenerated as life per second": func(_ *Parser, a []any) any {
			return []any{
				newMod("PhysicalDamageMitigatedLifePseudoRecoup", "BASE", argF(a, argNum)*argF(a, cap2), 0, 0),
				newMod("PhysicalDamageMitigatedLifePseudoRecoupDuration", "BASE", argS(a, cap2), 0, 0),
			}
		},
		// ModParser.lua:4891 ["([%d%.]+)%% of physical damage prevented from hits recently is regenerated as energy shield per second"] = function(num) return {
		//   mod("PhysicalDamageMitigatedEnergyShieldPseudoRecoup", "BASE", num * 4) } end
		"([%d%.]+)%% of physical damage prevented from hits recently is regenerated as energy shield per second": func(_ *Parser, a []any) any {
			return []any{newMod("PhysicalDamageMitigatedEnergyShieldPseudoRecoup", "BASE", argF(a, argNum)*4, 0, 0)}
		},
		// ModParser.lua:2231 ["(%w+) recovery from regeneration is not applied"] = function(_, type) return {
		//   flag("UnaffectedBy" .. firstToUpper(type) .. "Regen") } end
		"(%w+) recovery from regeneration is not applied": func(_ *Parser, a []any) any {
			return []any{flagMod("UnaffectedBy" + firstToUpper(argS(a, cap1)) + "Regen")}
		},
		// ModParser.lua:2235 ["(%w+) recovery from non%-instant leech is not applied"] = function(_, type) return {
		//   flag("UnaffectedByNonInstant" .. firstToUpper(type) .. "Leech") } end
		"(%w+) recovery from non%-instant leech is not applied": func(_ *Parser, a []any) any {
			return []any{flagMod("UnaffectedByNonInstant" + firstToUpper(argS(a, cap1)) + "Leech")}
		},
		// ModParser.lua:2993 ["regenerate (%d+)%% of energy shield over 2 seconds when you consume a corpse"] = function(num) return {
		//   mod("EnergyShieldRegenPercent", "BASE", num / 2, { type = "Condition", var = "ConsumedCorpseInPast2Sec" }) } end
		"regenerate (%d+)%% of energy shield over 2 seconds when you consume a corpse": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("EnergyShieldRegenPercent", "BASE", argF(a, argNum)/2, 0, 0),
					condTag("ConsumedCorpseInPast2Sec"),
				),
			}
		},
		// ModParser.lua:2994 ["regenerate (%d+)%% of mana over 2 seconds when you consume a corpse"] = function(num) return {
		//   mod("ManaRegen", "BASE", 1, { type = "PercentStat", stat = "Mana", percent = num / 2 }, { type = "Condition", var = "ConsumedCorpseInPast2Sec" }) } end
		"regenerate (%d+)%% of mana over 2 seconds when you consume a corpse": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("ManaRegen", "BASE", 1.0, 0, 0),
					map[string]any{
						"type":    "PercentStat",
						"stat":    "Mana",
						"percent": argF(a, argNum) / 2,
					},
					condTag("ConsumedCorpseInPast2Sec"),
				),
			}
		},
		// ModParser.lua:5802 ["for each nearby corpse, regenerate ([%d%.]+)%% life per second, up to ([%d%.]+)%%"] = function(num, _, limit) return {
		//   mod("LifeRegenPercent", "BASE", num, { type = "Multiplier", var = "NearbyCorpse", limit = tonumber(limit), limitTotal = true }) } end
		"for each nearby corpse, regenerate ([%d%.]+)%% life per second, up to ([%d%.]+)%%": func(_ *Parser, a []any) any {
			return []any{withTags(newMod("LifeRegenPercent", "BASE", argF(a, argNum), 0, 0), nearbyCorpses(a))}
		},
		// ModParser.lua:2984 ["for each nearby corpse, you and nearby allies regenerate ([%d%.]+)%% of energy shield per second, up to ([%d%.]+)%% per second"] =
		//   function(num, _, limit) return { mod("ExtraAura", "LIST", { mod = mod("EnergyShieldRegenPercent", "BASE", num) },
		//   { type = "Multiplier", var = "NearbyCorpse", limit = tonumber(limit), limitTotal = true }) } end
		"for each nearby corpse, you and nearby allies regenerate ([%d%.]+)%% of energy shield per second, up to ([%d%.]+)%% per second": func(_ *Parser, a []any) any {
			return []any{
				modifierOf(
					"ExtraAura",
					newMod("EnergyShieldRegenPercent", "BASE", argF(a, argNum), 0, 0),
					nearbyCorpses(a),
				),
			}
		},
		// ModParser.lua:2985 ["for each nearby corpse, you and nearby allies regenerate (%d+) mana per second, up to (%d+) per second"] =
		//   function(num, _, limit) return { mod("ExtraAura", "LIST", { mod = mod("ManaRegen", "BASE", num) }, <same multiplier>) } end
		"for each nearby corpse, you and nearby allies regenerate (%d+) mana per second, up to (%d+) per second": func(_ *Parser, a []any) any {
			return []any{modifierOf("ExtraAura", newMod("ManaRegen", "BASE", argF(a, argNum), 0, 0), nearbyCorpses(a))}
		},
		// ModParser.lua:4310 ["every (%d+) seconds, regenerate life equal to (%d+)%% of armour and evasion rating over (%d+) second"] =
		//   function (interval, _, percent, duration) return {
		//   mod("LifeRegen", "BASE", 1, { type = "Condition", var = "LifeRegenBurstFull" }, { type = "PercentStat", stat = "Armour", percent = percent }),
		//   mod("LifeRegen", "BASE", 1 / interval * duration, { type = "Condition", var = "LifeRegenBurstAvg" }, <Armour>),
		//   ...the same two for Evasion } end
		"every (%d+) seconds, regenerate life equal to (%d+)%% of armour and evasion rating over (%d+) second": func(_ *Parser, a []any) any {
			return append(burstRegen("LifeRegen", "Armour", a), burstRegen("LifeRegen", "Evasion", a)...)
		},
		// ModParser.lua:4316 ["every (%d+) seconds, regenerate energy shield equal to (%d+)%% of evasion rating over (%d+) second"] =
		//   function (interval, _, percent, duration) return { the two EnergyShieldRegen mods of 4310 for Evasion } end
		"every (%d+) seconds, regenerate energy shield equal to (%d+)%% of evasion rating over (%d+) second": func(_ *Parser, a []any) any {
			return burstRegen("EnergyShieldRegen", "Evasion", a)
		},
		// ModParser.lua:4791 ["(%d+)%% chance to gain (%d+) life on hit with attacks"] = function (chance, _, amount) return {
		//   mod("LifeOnHit", "BASE", amount * chance / 100, nil, bor(ModFlag.Attack, ModFlag.Hit), { type = "Condition", var = "AverageResourceGain" }),
		//   mod("LifeOnHit", "BASE", tonumber(amount), nil, bor(ModFlag.Attack, ModFlag.Hit), { type = "Condition", var = "MaxResourceGain" }) } end
		"(%d+)%% chance to gain (%d+) life on hit with attacks": func(_ *Parser, a []any) any {
			amount := argF(a, cap2)
			return []any{
				withTags(
					newMod("LifeOnHit", "BASE", amount*argF(a, argNum)/100, modFlagAttack|modFlagHit, 0),
					condTag("AverageResourceGain"),
				),
				withTags(newMod("LifeOnHit", "BASE", amount, modFlagAttack|modFlagHit, 0), condTag("MaxResourceGain")),
			}
		},
	})
}
