package engine

import "slices"

// Per-skill hooks: PoB's skill functions (initialFunc, preSkillTypeFunc,
// preDamageFunc, postCritFunc, preDotFunc in Data/Skills/*.lua) plus the warcry share
// of calcs.perform, keyed by the active skill's gem name. Each skills_*.go
// file registers its group in init().

// skillHooks holds one skill's hooks; a nil field means the skill has none.
type skillHooks struct {
	initial      func(actor *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource)
	preSkillType func(actor *ModDB, s *activeSkill)
	preDamage    func(actor *ModDB, s *activeSkill, out map[string]float64)
	postCrit     func(s *activeSkill, out, global map[string]float64)
	// preDot is preDotFunc, run before the skill's DoT (CalcOffence.lua:
	// 5851). No skill of PoB v2.68.1 has one (TestLuaPortPreDotFuncs).
	preDot      func(actor *ModDB, s *activeSkill, out map[string]float64)
	warcryShare func(db *ModDB, s, main *activeSkill)
}

// skillHookRegistry maps a gem name to its hooks.
var skillHookRegistry = map[string]*skillHooks{}

// registerSkill adds h's hooks to every name. Hooks of different kinds for
// one name may come from separate calls; the same kind twice panics.
func registerSkill(names []string, h skillHooks) {
	for _, name := range names {
		e := skillHookRegistry[name]
		if e == nil {
			e = &skillHooks{}
			skillHookRegistry[name] = e
		}
		set := func(kind string, dup bool) {
			if dup {
				panic("engine: duplicate " + kind + " hook for skill " + name)
			}
		}
		if h.initial != nil {
			set("initial", e.initial != nil)
			e.initial = h.initial
		}
		if h.preSkillType != nil {
			set("preSkillType", e.preSkillType != nil)
			e.preSkillType = h.preSkillType
		}
		if h.preDamage != nil {
			set("preDamage", e.preDamage != nil)
			e.preDamage = h.preDamage
		}
		if h.postCrit != nil {
			set("postCrit", e.postCrit != nil)
			e.postCrit = h.postCrit
		}
		if h.preDot != nil {
			set("preDot", e.preDot != nil)
			e.preDot = h.preDot
		}
		if h.warcryShare != nil {
			set("warcryShare", e.warcryShare != nil)
			e.warcryShare = h.warcryShare
		}
	}
}

// hooksOf returns the hooks of s's gem, or nil if it has none.
func hooksOf(s *activeSkill) *skillHooks {
	return skillHookRegistry[s.active.ge.Name]
}

// mulDPS scales the skill's dpsMultiplier (default 1) by m.
func mulDPS(data *skillData, m float64) {
	data.dpsMultiplier = optF{data.dpsMultiplier.or(1) * m, true}
}

// mulSkillDPSOutput is output.SkillDPSMultiplier =
// (output.SkillDPSMultiplier or 1) * m.
func mulSkillDPSOutput(out map[string]float64, m float64) {
	if v, ok := out["SkillDPSMultiplier"]; ok {
		out["SkillDPSMultiplier"] = v * m
	} else {
		out["SkillDPSMultiplier"] = m
	}
}

// applySkillDPSMultiplier scales skillData.dpsMultiplier and
// output.SkillDPSMultiplier by m, as the skills' preDamageFuncs do (their
// "if dpsMultiplier ~= 1" check stays with the caller).
func applySkillDPSMultiplier(s *activeSkill, out map[string]float64, m float64) {
	mulDPS(s.data, m)
	mulSkillDPSOutput(out, m)
}

// enemyRadius is skillModList:Override(skillCfg, "EnemyRadius") or
// skillModList:Sum("BASE", skillCfg, "EnemyRadius").
func enemyRadius(s *activeSkill) float64 {
	if v, ok := s.mods.OverrideCfg(s.cfg, "EnemyRadius"); ok {
		return v
	}
	return s.mods.SumCfg(s.cfg, "BASE", "EnemyRadius")
}

// noteIncomplete records on the actor a part a hook could not compute;
// the player's notes go to Result.Incomplete. A nil actor (hook tests)
// drops the note.
func noteIncomplete(actor *ModDB, msg string) {
	if actor == nil {
		return
	}
	r := actor.root()
	if !slices.Contains(r.incomplete, msg) {
		r.incomplete = append(r.incomplete, msg)
	}
}
