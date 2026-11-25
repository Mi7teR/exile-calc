package engine

// Per-skill hooks: PoB's skill functions (initialFunc, preSkillTypeFunc,
// preDamageFunc, postCritFunc in Data/Skills/*.lua) plus the warcry share
// of calcs.perform, keyed by the active skill's gem name. Each skills_*.go
// file registers its group in init().

// skillHooks holds one skill's hooks; a nil field means the skill has none.
type skillHooks struct {
	initial      func(actor *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource)
	preSkillType func(s *activeSkill)
	preDamage    func(actor *ModDB, s *activeSkill, out map[string]float64)
	postCrit     func(s *activeSkill, out, global map[string]float64)
	warcryShare  func(db *ModDB, s, main *activeSkill)
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
