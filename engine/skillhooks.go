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

// minionSkillHookRegistry maps a minion skill's granted effect id to its
// hooks. PoB keeps a skill's functions on its granted effect, and the
// minion skills of Data/Skills/minion.lua and spectre.lua have their own
// effects even where they share a player skill's name (a spectre's
// "Barrage", "Cyclone" or "Spark"), so they take none of the player
// hooks above. These are every function those two files define: three
// preDamageFuncs. The only other skills in a minion's skill list are
// Melee, Anger, Hatred and Wrath (Data/Minions.lua, Data/Spectres.lua),
// which have no hooks.
var minionSkillHookRegistry = map[string]*skillHooks{
	"GAZombieCorpseGroundImpact": {preDamage: summonSpeedHitTimePreDamage},
	"MinionInstability":          {preDamage: summonSpeedHitTimePreDamage},
	"AzmeriHydraBarrage":         {preDamage: azmeriHydraBarragePreDamage},
}

// hooksOf returns the hooks of s's gem, or nil if it has none; a minion's
// skill (summonSkill set) has only the hooks of its own granted effect.
func hooksOf(s *activeSkill) *skillHooks {
	if s.summon != nil {
		return minionSkillHookRegistry[s.active.id]
	}
	return skillHookRegistry[s.active.ge.Name]
}

// summonSpeedHitTimePreDamage is the preDamageFunc of Falling Slam
// (Data/Skills/minion.lua:756-758) and Minion Instability
// (minion.lua:1756-1758): the skill hits once per minion the summoning
// skill raises, skillData.summonSpeed (CalcOffence.lua:4141, or 1).
func summonSpeedHitTimePreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	speed := 1.0
	if s.summon != nil {
		if v, ok := num(s.summon.data.val("summonSpeed")); ok {
			speed = v
		}
	}
	s.data.hitTimeOverride = optF{1 / speed, true}
}

// azmeriHydraBarragePreDamage is the Azmeri Hydra's Barrage preDamageFunc
// (Data/Skills/spectre.lua:7277-7279): one hit per projectile, with no
// skill part check.
func azmeriHydraBarragePreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	s.data.dpsMultiplier = optF{out["ProjectileCount"], true}
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
