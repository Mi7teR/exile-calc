package modparser

// Helpers shared by the themed hook files (hooks_*.go). Each of those files
// registers its specialModList ports from an init function, keyed by the
// Lua pattern as written in ModParser.lua.

// cap5 is the fifth capture's position in a hook's arguments (see cap1).
const cap5 = 5

// ModFlag, KeywordFlag and SkillType values (Modules/Common.lua,
// Data/Global.lua) used by the themed hooks.
const (
	modFlagAttack     = 0x1
	modFlagHit        = 0x4
	modFlagCast       = 0x10
	kwFlagPhysical    = 0x10
	kwFlagTrap        = 0x1000
	kwFlagMine        = 0x2000
	kwFlagHit         = 0x40000
	kwFlagAilment     = 0x80000
	kwFlagBleed       = 0x400000
	skillTypeAttack   = 1.0
	skillTypeProj     = 3.0
	skillTypeVaal     = 38.0
	skillTypeAura     = 39.0
	skillTypeChannel  = 48.0
	skillTypeTravel   = 79.0
	skillTypeHex      = 97.0
	skillTypeRandElem = 102.0
)

// registerSpecial adds ports of specialModList functions, keyed by their
// Lua pattern. A pattern ported twice is a programming error.
func registerSpecial(m map[string]Hook) {
	for pattern, h := range m {
		key := "specialModList[^" + pattern + "$]"
		if _, dup := hooks[key]; dup {
			panic("modparser: hook ported twice: " + key)
		}
		hooks[key] = h
	}
}

// argF is tonumber(a[i]) as a float64 (0 when it is not a number).
func argF(a []any, i int) float64 {
	f, _ := toNumber(arg(a, i)).(float64)
	return f
}

// flagMod is ModParser.lua's flag(name, ...): a FLAG mod with value true.
func flagMod(name string, tags ...any) map[string]any {
	return withTags(newMod(name, "FLAG", true, 0, 0), tags...)
}

// sourced sets the mod's source, mod(name, type, value, source, ...).
func sourced(m map[string]any, source string) map[string]any {
	m["source"] = source
	return m
}

// itemsEquipped is { type = "MultiplierThreshold", var = firstToUpper(influence) .. "Item",
// threshold = tonumber(thresh) }, the tag of the "if N <influence> items are
// equipped" lines.
func itemsEquipped(thresh, influence string) map[string]any {
	return map[string]any{
		"type":      "MultiplierThreshold",
		"var":       firstToUpper(influence) + "Item",
		"threshold": toNumber(thresh),
	}
}

// perStat is { type = "PerStat", stat = stat, div = div }.
func perStat(stat string, div any) map[string]any {
	return map[string]any{"type": "PerStat", "stat": stat, "div": div}
}

// skillTypeTag is { type = "SkillType", skillType = t }.
func skillTypeTag(t float64) map[string]any {
	return map[string]any{"type": "SkillType", "skillType": t}
}

// thresholdTag is { type = "MultiplierThreshold", var = v, threshold = n }.
func thresholdTag(v string, n any) map[string]any {
	return map[string]any{"type": "MultiplierThreshold", "var": v, "threshold": n}
}

// playerTag is { type = "ActorCondition", actor = "player" }.
func playerTag() map[string]any {
	return map[string]any{"type": "ActorCondition", "actor": "player"}
}

// modifierOf wraps a mod for another actor: mod(list, "LIST", { mod = m }, tags...).
func modifierOf(list string, m map[string]any, tags ...any) map[string]any {
	return withTags(newMod(list, "LIST", map[string]any{"mod": m}, 0, 0), tags...)
}
