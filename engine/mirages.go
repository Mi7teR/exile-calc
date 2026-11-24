package engine

import (
	"maps"
	"math"
)

// cloneSkill copies a skill for a separate calculation (calcs.copyActiveSkill
// for one offence run): its own skill mod list, data, flags, types and cfgs.
func cloneSkill(s *activeSkill) *activeSkill {
	c := *s
	c.data = make(map[string]any, len(s.data))
	for k, v := range s.data {
		c.data[k] = v
	}
	c.flags = make(map[string]bool, len(s.flags))
	for k, v := range s.flags {
		c.flags[k] = v
	}
	c.types = make(map[int]bool, len(s.types))
	for k, v := range s.types {
		c.types[k] = v
	}
	m := *s.mods
	m.mods = make(map[string][]Mod, len(s.mods.mods))
	for k, l := range s.mods.mods {
		m.mods[k] = append([]Mod(nil), l...)
	}
	m.conditions = make(map[string]bool, len(s.mods.conditions))
	for k, v := range s.mods.conditions {
		m.conditions[k] = v
	}
	c.mods = &m
	cloneCfg := func(cfg *Cfg) *Cfg {
		if cfg == nil {
			return nil
		}
		n := *cfg
		n.SkillTypes = c.types
		n.SkillCond = make(map[string]bool, len(cfg.SkillCond))
		for k, v := range cfg.SkillCond {
			n.SkillCond[k] = v
		}
		return &n
	}
	c.cfg, c.weapon1Cfg, c.weapon2Cfg = cloneCfg(s.cfg), cloneCfg(s.weapon1Cfg), cloneCfg(s.weapon2Cfg)
	return &c
}

// offenceOnCopy runs calcs.offence for a copy of a skill without leaving
// traces in the actor (PoB builds a separate environment for it): the
// actor's output, mods, conditions, multipliers and unsupported tag counts
// are restored afterwards.
func (c *Calculator) offenceOnCopy(db *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource) map[string]float64 {
	output := maps.Clone(db.output)
	mods := make(map[string][]Mod, len(db.mods))
	for k, l := range db.mods {
		mods[k] = append([]Mod(nil), l...)
	}
	conds, mults, tags := maps.Clone(db.conditions), maps.Clone(db.multipliers), maps.Clone(db.UnsupportedTags)
	out := c.offence(db, cloneSkill(s), w1, w2)
	restore := func(dst, src map[string]float64) {
		clear(dst)
		maps.Copy(dst, src)
	}
	restore(db.output, output)
	restore(db.multipliers, mults)
	clear(db.mods)
	maps.Copy(db.mods, mods)
	clear(db.conditions)
	maps.Copy(db.conditions, conds)
	clear(db.UnsupportedTags)
	maps.Copy(db.UnsupportedTags, tags)
	return out
}

// setSkillCond sets a skill condition on all of a skill's cfgs.
func setSkillCond(s *activeSkill, name string) {
	for _, cfg := range []*Cfg{s.cfg, s.weapon1Cfg, s.weapon2Cfg} {
		if cfg == nil {
			continue
		}
		if cfg.SkillCond == nil {
			cfg.SkillCond = map[string]bool{}
		}
		cfg.SkillCond[name] = true
	}
}

// generalsCry is the General's Cry branch of calcs.mirages
// (CalcMirages.lua): the main skill used by the cry's mirage warriors.
func (c *Calculator) generalsCry(db *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource) {
	const stOtherThingUsesSkill = 95
	s.types[stTriggered] = true
	s.types[stOtherThingUsesSkill] = true
	setSkillCond(s, "usedByMirage")
	cached := c.offenceOnCopy(db, s, w1, w2)
	cooldown := 1.0
	var gc *activeSkill
	for _, v := range db.activeSkills {
		if v.active.ge.Name == "General's Cry" && v.group != nil && s.group != nil && v.group.Slot == s.group.Slot {
			cooldown, _ = skillCooldown(v.mods, v.cfg, v.data)
			gc = v
			break
		}
	}
	if gc == nil {
		return
	}
	list, cfg := s.mods, s.cfg
	for _, m := range appliedMods(gc.mods, gc.cfg, "BASE", "GeneralsCryDoubleMaxCount") {
		list.Add(Mod{Name: "QuantityMultiplier", Type: m.Type, Value: m.Value, Flags: m.Flags, KeywordFlags: m.KeywordFlags, Source: m.Source})
	}
	if !s.types[stChannel] {
		s.data["hitTimeOverride"] = 1.0
	}
	t, ok := cached["HitTime"]
	if !ok {
		t = cached["Time"]
	}
	cooldown = math.Max(cooldown, t)
	list.NewMod("DPS", "MORE", (1/cooldown-1)*100, "General's Cry Cooldown")
	list.Add(Mod{Name: "HasNoCost", Type: "FLAG", Value: true, Source: "Used by mirage"})
	// Supported attacks count as exerted.
	for _, x := range []struct{ typ, from, to string }{
		{"INC", "ExertIncrease", "Damage"}, {"MORE", "ExertIncrease", "Damage"}, {"MORE", "ExertAttackIncrease", "Damage"},
		{"MORE", "OverexertionExertAverageIncrease", "Damage"}, {"BASE", "ExertDoubleDamageChance", "DoubleDamageChance"},
	} {
		for _, m := range appliedMods(list, cfg, x.typ, x.from) {
			list.Add(Mod{Name: x.to, Type: m.Type, Value: m.Value, Flags: m.Flags, KeywordFlags: m.KeywordFlags, Source: m.Source})
		}
	}
}

// mirageResult is activeSkill.mirage: how many mirages use the skill and
// the output of the skill used by one of them.
type mirageResult struct {
	count float64
	out   map[string]float64
}

// mirages is the Mirage Archer and Sacred Wisps branches of calcs.mirages
// (CalcMirages.lua): the main skill recalculated as used by its mirages.
// PoB runs calcs.perform in a new environment; the mirage skill is the same
// skill, so the offence of a copy is used.
func (c *Calculator) mirages(db *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource) {
	if s.disabled || s.cfg == nil || s.cfg.SkillCond["usedByMirage"] {
		return
	}
	const stOtherThingUsesSkill = 95
	m := cloneSkill(s)
	setSkillCond(m, "usedByMirage")
	delete(m.flags, "multiPart")
	m.data["mirageUses"] = s.data["storedUses"]
	m.types[stOtherThingUsesSkill] = true
	// PoB quirk: mainSkill.ModFlags/KeywordFlags do not exist, so the mods have no flags.
	var count float64
	switch {
	case truthy(s.data["triggeredByMirageArcher"]):
		count = s.mods.SumCfg(s.cfg, "BASE", "MirageArcherMaxCount")
		m.mods.NewMod("Damage", "MORE", m.mods.SumCfg(m.cfg, "BASE", "MirageArcherLessDamage"), "Mirage Archer")
		m.mods.NewMod("Speed", "MORE", m.mods.SumCfg(m.cfg, "BASE", "MirageArcherLessAttackSpeed"), "Mirage Archer")
	case truthy(s.data["triggeredBySacredWisps"]):
		var wisps *activeSkill
		for _, v := range db.activeSkills {
			if v.active.ge.Name == "Summon Sacred Wisps" {
				wisps = v
				break
			}
		}
		if wisps == nil {
			return
		}
		count = wisps.mods.SumCfg(s.cfg, "BASE", "SacredWispsMaxCount")
		m.mods.NewMod("Damage", "MORE", m.mods.SumCfg(s.cfg, "BASE", "SacredWispsLessDamage"), "Used by Sacred Wisps")
		m.mods.NewMod("Speed", "MORE", wisps.mods.SumCfg(s.cfg, "BASE", "SacredWispsChance")-100, "Sacred Wisps cast chance")
	default:
		return
	}
	m.mods.Add(Mod{Name: "HasNoCost", Type: "FLAG", Value: true, Source: "Used by mirage"})
	s.mirage = &mirageResult{count: count, out: c.offenceOnCopy(db, m, w1, w2)}
}

// addMirageDPS is the mirage part of the combined DPS in calcs.offence; it
// returns the mirage's cull multiplier (1 when it does not cull).
func addMirageDPS(s *activeSkill, out map[string]float64) float64 {
	if s.mirage == nil {
		return 1
	}
	mo := s.mirage.out
	total, ok := mo["TotalDPS"]
	if !ok {
		return 1
	}
	n := s.mirage.count
	out["MirageDPS"] = total * n
	out["CombinedDPS"] += total * n
	if v, ok := mo["BurningGroundDPS"]; ok {
		out["MirageBurningGroundDPS"] = v
	}
	if v, ok := mo["CausticGroundDPS"]; ok {
		out["MirageCausticGroundDPS"] = v
	}
	// PoB quirk: a stronger mirage ignite/bleed zeroes the player's but is
	// added to MirageDPS only, so CombinedDPS loses it.
	for _, k := range []string{"IgniteDPS", "BleedDPS"} {
		if v, ok := mo[k]; ok && v > out[k] {
			out["MirageDPS"] += v
			out[k] = 0
		}
	}
	for _, k := range []string{"PoisonDPS", "ImpaleDPS"} {
		if v, ok := mo[k]; ok {
			out["MirageDPS"] += v * n
			out["CombinedDPS"] += v * n
		}
	}
	if v, ok := mo["DecayDPS"]; ok {
		out["MirageDPS"] += v
		out["CombinedDPS"] += v
	}
	if v, ok := mo["TotalDot"]; ok && (s.flags["DotCanStack"] || out["TotalDot"] == 0) {
		if s.flags["DotCanStack"] {
			v *= n
		}
		out["MirageDPS"] += v
		out["CombinedDPS"] += v
	}
	return max(1, mo["CullMultiplier"])
}
