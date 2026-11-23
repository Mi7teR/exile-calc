package engine

import "math"

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
	cached := c.offence(db, cloneSkill(s), w1, w2)
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
