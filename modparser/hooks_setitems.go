package modparser

// Lines of ModParser.lua specialModList that hold while enough items of an
// influence (or ring base) are equipped: the set bonuses of the influenced
// and Foulborn unique sets.

// equippedFlag is function(_, thresh, influence) return { flag(name,
// { type = "MultiplierThreshold", var = firstToUpper(influence) .. "Item", threshold = tonumber(thresh) }) } end.
func equippedFlag(name string) Hook {
	return func(_ *Parser, a []any) any {
		return []any{flagMod(name, itemsEquipped(argS(a, cap1), argS(a, cap2)))}
	}
}

// equippedBase is equippedFlag with mod(name, "BASE", value, tag).
func equippedBase(name string, value float64) Hook {
	return func(_ *Parser, a []any) any {
		return []any{withTags(newMod(name, "BASE", value, 0, 0), itemsEquipped(argS(a, cap1), argS(a, cap2)))}
	}
}

func init() {
	registerSpecial(map[string]Hook{
		// ModParser.lua:3281 ["([%+%-]%d+) to level of all (%a+) skill gems if a?t? ?l?e?a?s?t? ?(%d+) (%a+) (%a+) items are equipped"] =
		//   function(num, _, element, thresh, influence, influence2) return { mod("GemProperty", "LIST",
		//   { keywordList = { element, "grants_active_skill" }, key = "level", value = num, keyOfScaledMod = "value" },
		//   { type = "MultiplierThreshold", var = firstToUpper(influence) .. firstToUpper(influence2) .. "Item", threshold = tonumber(thresh) }) } end
		"([%+%-]%d+) to level of all (%a+) skill gems if a?t? ?l?e?a?s?t? ?(%d+) (%a+) (%a+) items are equipped": func(_ *Parser, a []any) any {
			prop := map[string]any{
				"keywordList":    []any{argS(a, cap2), "grants_active_skill"},
				"key":            "level",
				"value":          arg(a, argNum),
				"keyOfScaledMod": "value",
			}
			return []any{withTags(newMod("GemProperty", "LIST", prop, 0, 0),
				itemsEquipped(argS(a, cap3), argS(a, cap4)+firstToUpper(argS(a, cap5))))}
		},
		// ModParser.lua:3282 ["([%+%-]%d+) to level of all ?([%a%- ]*) support gems if (%d+) (%a+) items are equipped"] =
		//   function(num, _, element, thresh, influence) return { mod("GemProperty", "LIST",
		//   { keyword = element, key = "level", value = num, keyOfScaledMod = "value" },
		//   { type = "MultiplierThreshold", var = firstToUpper(influence) .. "Item", threshold = tonumber(thresh) }) } end
		"([%+%-]%d+) to level of all ?([%a%- ]*) support gems if (%d+) (%a+) items are equipped": func(_ *Parser, a []any) any {
			prop := map[string]any{
				"keyword":        argS(a, cap2),
				"key":            "level",
				"value":          arg(a, argNum),
				"keyOfScaledMod": "value",
			}
			return []any{
				withTags(newMod("GemProperty", "LIST", prop, 0, 0), itemsEquipped(argS(a, cap3), argS(a, cap4))),
			}
		},
		// ModParser.lua:4892 ["([%d%.]+)%% of physical damage prevented recently is regenerated as energy shield per second if (%d+) (%a+) items are equipped"] =
		//   function(num, _, thresh, influence) return { mod("PhysicalDamageMitigatedEnergyShieldPseudoRecoup", "BASE", num * 4,
		//   { type = "MultiplierThreshold", var = firstToUpper(influence) .. "Item", threshold = tonumber(thresh) }) } end
		"([%d%.]+)%% of physical damage prevented recently is regenerated as energy shield per second if (%d+) (%a+) items are equipped": func(_ *Parser, a []any) any {
			return []any{
				withTags(newMod("PhysicalDamageMitigatedEnergyShieldPseudoRecoup", "BASE", argF(a, argNum)*4, 0, 0),
					itemsEquipped(argS(a, cap2), argS(a, cap3))),
			}
		},
		// ModParser.lua:4990 ["cannot be frozen if (%d+) (%a+) items are equipped"] = function(_, thresh, influence) return { flag("FreezeImmune", <items>) } end
		"cannot be frozen if (%d+) (%a+) items are equipped": equippedFlag("FreezeImmune"),
		// ModParser.lua:4983 ["cannot be stunned if (%d+) (%a+) items are equipped"] = function(_, thresh, influence) return { flag("StunImmune", <items>) } end
		"cannot be stunned if (%d+) (%a+) items are equipped": equippedFlag("StunImmune"),
		// ModParser.lua:5748 ["consecrated ground around you while stationary if (%d+) (%a+) items are equipped"] =
		//   function(_, thresh, influence) return { flag("Condition:OnConsecratedGround", <items>) } end
		"consecrated ground around you while stationary if (%d+) (%a+) items are equipped": equippedFlag(
			"Condition:OnConsecratedGround",
		),
		// ModParser.lua:4676 ["skills fire an additional projectile if (%d+) (%a+) items are equipped"] =
		//   function(_, thresh, influence) return { mod("ProjectileCount", "BASE", 1, <items>) } end
		"skills fire an additional projectile if (%d+) (%a+) items are equipped": equippedBase("ProjectileCount", 1),
		// ModParser.lua:4185 ["you can apply an additional curse if (%d+) (%a+) items are equipped"] =
		//   function(_, thresh, influence) return { mod("EnemyCurseLimit", "BASE", 1, <items>) } end
		"you can apply an additional curse if (%d+) (%a+) items are equipped": equippedBase("EnemyCurseLimit", 1),
		// ModParser.lua:4693 ["projectiles pierce (%d+) additional targets if (%d+) (%a+) items are equipped"] =
		//   function(_, num, thresh, influence) return { mod("PierceCount", "BASE", num, <items>) } end
		// num is the first capture as a string here, as in PoB.
		"projectiles pierce (%d+) additional targets if (%d+) (%a+) items are equipped": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("PierceCount", "BASE", argS(a, cap1), 0, 0),
					itemsEquipped(argS(a, cap2), argS(a, cap3)),
				),
			}
		},
		// ModParser.lua:5106 ["unaffected by (.+) if (%d+) (%a+) items are equipped"] = function (_, ailment, thresh, influence) return {
		//   mod("Self"..ailment:gsub("^%l", string.upper).."Effect", "MORE", -100, <items>) } end
		"unaffected by (.+) if (%d+) (%a+) items are equipped": func(_ *Parser, a []any) any {
			return []any{
				withTags(
					newMod("Self"+firstToUpper(argS(a, cap1))+"Effect", "MORE", -100.0, 0, 0),
					itemsEquipped(argS(a, cap2), argS(a, cap3)),
				),
			}
		},
		// ModParser.lua:3585 ["gain (%d+)%% of physical damage as extra damage of each element if (%d+) (%a+) items are equipped"] =
		//   function(num, _, thresh, influence) return { mod("PhysicalDamageGainAsLightning", "BASE", num, <items>), ...Cold, ...Fire } end
		"gain (%d+)%% of physical damage as extra damage of each element if (%d+) (%a+) items are equipped": func(_ *Parser, a []any) any {
			var out []any
			for _, e := range []string{"Lightning", "Cold", "Fire"} {
				out = append(
					out,
					withTags(
						newMod("PhysicalDamageGainAs"+e, "BASE", argF(a, argNum), 0, 0),
						itemsEquipped(argS(a, cap2), argS(a, cap3)),
					),
				)
			}
			return out
		},
		// ModParser.lua:6027 ["every (%d+) seconds, regenerate (%d+)%% of life over one second if (%d+) (%a+) items are equipped"] =
		//   function (num, _, percent, thresh, influence) return {
		//   mod("LifeRegenPercent", "BASE", tonumber(percent), { type = "Condition", var = "LifeRegenBurstFull" }, <items>),
		//   mod("LifeRegenPercent", "BASE", tonumber(percent) / num, { type = "Condition", var = "LifeRegenBurstAvg" }, <items>) } end
		"every (%d+) seconds, regenerate (%d+)%% of life over one second if (%d+) (%a+) items are equipped": func(_ *Parser, a []any) any {
			pct := argF(a, cap2)
			return []any{
				withTags(
					newMod("LifeRegenPercent", "BASE", pct, 0, 0),
					condTag("LifeRegenBurstFull"),
					itemsEquipped(argS(a, cap3), argS(a, cap4)),
				),
				withTags(
					newMod("LifeRegenPercent", "BASE", pct/argF(a, argNum), 0, 0),
					condTag("LifeRegenBurstAvg"),
					itemsEquipped(argS(a, cap3), argS(a, cap4)),
				),
			}
		},
		// ModParser.lua:5628 ["your hits ignore enemy monster (%a+) resistances? if all equipped rings are (%a+) rings"] =
		//   function(_, damageType, baseType) return { flag("Ignore"..firstToUpper(damageType).."Resistance",
		//   { type = "MultiplierThreshold", var = firstToUpper(baseType).."RingEquipped", threshold = 2 }) } end
		"your hits ignore enemy monster (%a+) resistances? if all equipped rings are (%a+) rings": func(_ *Parser, a []any) any {
			return []any{
				flagMod(
					"Ignore"+firstToUpper(argS(a, cap1))+"Resistance",
					thresholdTag(firstToUpper(argS(a, cap2))+"RingEquipped", 2.0),
				),
			}
		},
	})
}
