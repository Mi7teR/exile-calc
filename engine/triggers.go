package engine

import (
	"math"
	"regexp"
	"strings"
)

// The trigger rate part of the calculation: calcs.triggers
// (Modules/CalcTriggers.lua) with its default handler, for the trigger
// configs ported so far. The source skill's rates come from an offence
// run of a copy of it in the main skill's environment (PoB builds a new
// environment with the source as the main skill).

const serverTickTime = 0.033

const stInbuiltTrigger = 50 // SkillType.InbuiltTrigger

// triggerConfig is an entry of CalcTriggers.lua configTable.
type triggerConfig struct {
	triggerOnUse       bool
	triggerChance      float64 // 0: not set
	triggerSkillCond   func(main, s *activeSkill) bool
	triggeredSkillCond func(main, s *activeSkill) bool
	overlaps           float64 // 0: not set
	doomBlast          bool
	// sourceValue is the config's comparer: the source with the largest
	// value wins (default: the highest rate).
	sourceValue func(o map[string]float64) float64
	// ignoreSourceRate: the source rate is the trigger rate cap.
	ignoreSourceRate bool
}

func isTriggered(s *activeSkill) bool {
	return truthy(s.data["triggeredByUnique"]) || truthy(s.data["triggered"]) || s.types[stInbuiltTrigger] || s.types[stTriggered]
}

// triggeredBy is activeSkill.triggeredBy: the trigger support (isTrigger
// with added skill types) supporting the skill.
func triggeredBy(s *activeSkill) *skillEffect {
	for _, e := range s.effects[1:] {
		if e.ge.Support && e.ge.IsTrigger && len(e.ge.AddSkillTypes) > 0 {
			return e
		}
	}
	return nil
}

func slotMatch(main, s *activeSkill) bool {
	// srcInstance.fromItem: gems of groups granted by items.
	srcFromItem := func(x *activeSkill) bool { return x.group != nil && strings.HasPrefix(x.group.Source, "Item") }
	fromItem := main.active.ge.FromItem || s.active.ge.FromItem || srcFromItem(main) || srcFromItem(s)
	match1 := fromItem && s.group != nil && main.group != nil && s.group.Slot == main.group.Slot
	match2 := !main.active.ge.FromItem && s.group == main.group
	return match1 || match2
}

var triggerConfigs = map[string]func(db *ModDB, main *activeSkill) *triggerConfig{
	"doom blast": func(db *ModDB, main *activeSkill) *triggerConfig {
		const stHex = 97 // SkillType.Hex
		main.data["ignoresTickRate"] = true
		cfg := &triggerConfig{doomBlast: true,
			triggerSkillCond: func(main, s *activeSkill) bool { return s.types[stHex] && slotMatch(main, s) }}
		if len(db.TabulateCfg(nil, "BASE", "Multiplier:CurseOverlaps")) > 0 {
			cfg.overlaps = math.Max(db.Sum("BASE", "Multiplier:CurseOverlaps"), 1)
		}
		return cfg
	},
	"asenath's chant": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerOnUse: true,
			triggerSkillCond: func(main, s *activeSkill) bool {
				return (s.types[stDamage] || s.types[stAttack]) && s.cfg.Flags&modFlagByName["Bow"] > 0
			},
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data["triggeredByUnique"]) && main.group != nil && s.group != nil && main.group.Slot == s.group.Slot && s.types[stSpell]
			}}
	},
	"the covetous dark": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond:   func(main, s *activeSkill) bool { return s.types[20] }, // Melee
			triggeredSkillCond: func(main, s *activeSkill) bool { return truthy(s.data["triggeredByCovetousDark"]) }}
	},
	"avenging flame": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{ignoreSourceRate: true,
			triggerSkillCond: func(main, s *activeSkill) bool { return s.flags["totem"] && slotMatch(main, s) },
			sourceValue:      func(o map[string]float64) float64 { return o["TotemLife"] }}
	},
	"spellslinger": func(*ModDB, *activeSkill) *triggerConfig {
		// skipEffectiveRate / sourceRateIsFinal only change the breakdown.
		return &triggerConfig{triggerOnUse: true,
			triggerSkillCond: func(main, s *activeSkill) bool {
				return s.types[stAttack] && s.types[stProjectile] && s.cfg.Flags&modFlagByName["Wand"] > 0 && !truthy(s.data["triggeredBySpellSlinger"])
			}}
	},
	"cast on critical strike": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond:   func(main, s *activeSkill) bool { return s.types[stAttack] && slotMatch(main, s) },
			triggeredSkillCond: func(main, s *activeSkill) bool { return truthy(s.data["triggeredByCoc"]) && slotMatch(main, s) }}
	},
}

var sourceItemRe = regexp.MustCompile(`^.*:.*:(.*),.*$`)

// uniqueTriggerName is getUniqueItemTriggerName.
func uniqueTriggerName(s *activeSkill) string {
	if v, ok := s.data["triggerSource"].(string); ok {
		return v
	}
	for _, e := range s.supportList {
		if e.ge.FromItem && !e.ge.Support {
			return e.ge.Name
		}
	}
	if s.group != nil && s.group.Source != "" {
		if m := sourceItemRe.FindStringSubmatch(s.group.Source); m != nil {
			return m[1]
		}
	}
	return ""
}

// simSkill is packageSkillDataForSimulation.
type simSkill struct {
	skill                             *activeSkill
	cd, addedCooldown, icdr, addsCast float64
	cdOverride                        float64
	hasOverride                       bool
	nextTrig, count                   float64
}

func packageSim(s *activeSkill) *simSkill {
	o, has := s.mods.OverrideCfg(s.cfg, "CooldownRecovery")
	return &simSkill{skill: s, cd: numOr(s.data["cooldown"], 0), cdOverride: o, hasOverride: has,
		icdr: calcMod(s.mods, s.cfg, "CooldownRecovery"), addedCooldown: s.mods.SumCfg(s.cfg, "BASE", "CooldownRecovery")}
}

// rotationImpact is calcMultiSpellRotationImpact: the main skill's trigger
// rate in a rotation of triggered skills.
func rotationImpact(main *activeSkill, rotation []*simSkill, sourceRate, triggerCD, chance float64) float64 {
	if sourceRate <= 0 || len(rotation) == 0 {
		return 0
	}
	inc := 1 / sourceRate
	simTime := inc * 1000
	if chance == 0 {
		chance = 100
	}
	for _, s := range rotation {
		icdr := s.icdr
		if icdr == 0 {
			icdr = 1
		}
		cd := (s.cd + s.addedCooldown) / icdr
		if s.hasOverride {
			cd = s.cdOverride
		}
		s.cd = math.Max(cd, (triggerCD+s.addsCast)/icdr)
		s.nextTrig, s.count = 0, 0
	}
	idx := 0
	n := len(rotation)
	for next := 0.0; next < simTime; next += inc {
		cur := idx
		for {
			if rotation[cur].nextTrig <= next {
				rotation[cur].count++
				rotation[cur].nextTrig = serverTickTime * math.Ceil((serverTickTime*math.Floor(next/serverTickTime)+rotation[cur].cd)/serverTickTime)
				break
			}
			cur = (cur + 1) % n
			if cur == idx {
				break
			}
		}
		idx = (idx + 1) % n
	}
	for _, s := range rotation {
		if s.skill == main {
			if s.count == 0 {
				return 0
			}
			return 1 / (simTime/s.count + inc/chance*100 - inc)
		}
	}
	return 0
}

// triggers is calcs.triggers for the player's main skill: the trigger rate
// in skillData.triggerRate, or the skill counts as self-cast.
func (c *Calculator) triggers(db *ModDB, main *activeSkill, w1 hitSource, w2 *hitSource) {
	if main.disabled {
		return
	}
	var cfgFn func(*ModDB, *activeSkill) *triggerConfig
	by := triggeredBy(main)
	names := []string{strings.ToLower(main.active.ge.Name)}
	if by != nil {
		n := strings.ToLower(by.ge.Name)
		names = append(names, n, strings.TrimPrefix(n, "awakened "))
	}
	if isTriggered(main) {
		if u := uniqueTriggerName(main); u != "" {
			names = append(names, strings.ToLower(u))
		}
	}
	for _, n := range names {
		if f := triggerConfigs[n]; f != nil {
			cfgFn = f
			break
		}
	}
	for _, n := range names {
		if n == "cast while channelling" {
			c.cwcTrigger(db, main)
			return
		}
	}
	if cfgFn == nil {
		delete(main.data, "triggered")
		return
	}
	c.defaultTrigger(db, main, by, cfgFn(db, main), w1, w2)
}

func (c *Calculator) defaultTrigger(db *ModDB, main *activeSkill, by *skillEffect, cfg *triggerConfig, w1 hitSource, w2 *hitSource) {
	out := db.output
	var source *activeSkill
	var sourceOut map[string]float64
	trigRate := 0.0
	var triggered []*simSkill
	rate := func(o map[string]float64) float64 {
		if v, ok := o["HitSpeed"]; ok {
			return v
		}
		return o["Speed"]
	}
	for _, s := range db.activeSkills {
		if cfg.triggerSkillCond != nil && cfg.triggerSkillCond(main, s) && !isTriggered(s) && s != main && !s.disabled {
			o := c.offenceOnCopy(db, s, w1, w2)
			value := rate
			if cfg.sourceValue != nil {
				value = cfg.sourceValue
			}
			if source == nil || value(o) > value(sourceOut) {
				if s.cfg.SkillCond["usedByMirage"] || s.types[95] {
					continue
				}
				source, sourceOut, trigRate = s, o, rate(o)
			}
		}
		if cfg.triggeredSkillCond != nil && cfg.triggeredSkillCond(main, s) {
			triggered = append(triggered, packageSim(s))
		}
	}
	if len(triggered) == 0 && cfg.triggeredSkillCond != nil {
		return
	}
	if source == nil {
		delete(main.data, "triggered")
		return
	}
	main.data["triggered"] = true
	if w2 != nil && main.weapons[0] != "" && main.weapons[1] != "" && !truthy(source.data["doubleHitsWhenDualWielding"]) &&
		(source.types[20] || source.types[stAttack]) && by != nil && by.ge.FromItem {
		trigRate /= 2
	}
	ignoresTick := truthy(main.data["ignoresTickRate"]) || numOr(main.data["storedUses"], 0) > 1
	icdr := calcMod(main.mods, main.cfg, "CooldownRecovery")
	if icdr == 0 {
		icdr = 1
	}
	added := main.mods.SumCfg(main.cfg, "BASE", "CooldownRecovery")
	override, hasOverride := main.mods.OverrideCfg(main.cfg, "CooldownRecovery")
	triggerCD := 0.0
	if by != nil {
		if lv := by.ge.Levels[by.level]; lv != nil {
			triggerCD = lv.Cooldown
		}
	} else if sb := triggeredBy(source); sb != nil {
		if lv := sb.ge.Levels[sb.level]; lv != nil {
			triggerCD = lv.Cooldown
		}
	}
	triggeredCD := numOr(main.data["cooldown"], 0)
	triggeredAdj := (triggeredCD + added) / icdr
	triggerAdj := triggerCD / icdr
	tick := func(v float64) float64 { return math.Ceil(v*serverTickRate) / serverTickRate }
	triggeredRounded := tick(triggeredAdj)
	if ignoresTick {
		triggeredRounded = triggeredAdj
	}
	actionRounded := math.Max(tick(triggerAdj), triggeredRounded)
	if hasOverride {
		actionRounded = tick(override)
	}
	out["TriggerRateCap"] = math.Inf(1)
	if actionRounded != 0 {
		out["TriggerRateCap"] = 1 / actionRounded
	}
	out["EffectiveSourceRate"] = trigRate
	if cfg.ignoreSourceRate {
		out["EffectiveSourceRate"] = out["TriggerRateCap"]
	}
	main.data["triggerSourceTotemLife"] = sourceOut["TotemLife"]
	if cfg.doomBlast {
		c.doomBlastSourceRate(db, main, sourceOut, trigRate, icdr, w1, w2)
	}
	chance := 100.0
	if (source.types[20] || source.types[stAttack]) && !cfg.triggerOnUse {
		if hc := sourceOut["HitChance"]; hc != 100 {
			chance *= hc / 100
		}
		if truthy(main.data["triggerOnCrit"]) {
			if cfg.triggerChance == 0 {
				cfg.triggerChance = numOr(main.data["chanceToTriggerOnCrit"], 0)
			}
			if cc := sourceOut["CritChance"]; cc != 100 {
				chance *= cc / 100
			}
		}
	}
	if cfg.triggerChance != 0 && cfg.triggerChance != 100 {
		chance *= cfg.triggerChance / 100
	}
	var skillRate float64
	switch {
	case out["EffectiveSourceRate"] == 0:
		skillRate = 0
	case ignoresTick && (cfg.triggeredSkillCond == nil || len(triggered) == 1 && triggered[0].skill == main):
		overlaps := cfg.overlaps
		if overlaps == 0 {
			overlaps = 1
		}
		skillRate = math.Min(out["TriggerRateCap"], out["EffectiveSourceRate"]*overlaps)
	default:
		rot := triggered
		if cfg.triggeredSkillCond == nil {
			rot = []*simSkill{packageSim(main)}
		}
		cd := triggerCD
		if cd == 0 {
			cd = triggeredCD
		}
		skillRate = rotationImpact(main, rot, out["EffectiveSourceRate"], cd, chance)
		if db.Flag("HaveTriggerBots") && main.types[stSpell] {
			skillRate *= 2
		}
	}
	out["SkillTriggerRate"] = skillRate
	main.data["triggerRate"] = skillRate
	// addTriggerIncMoreMods: TriggeredDamage applies to the triggered skill.
	for _, typ := range []string{"INC", "MORE"} {
		for _, m := range appliedMods(main.mods, source.cfg, typ, "TriggeredDamage") {
			main.mods.Add(Mod{Name: "Damage", Type: typ, Value: m.Value, Flags: m.Flags, KeywordFlags: m.KeywordFlags, Source: m.Source, Tags: m.Tags})
		}
	}
}

// doomBlastSourceRate is the Doom Blast part of the default trigger
// handler: the source rate by the doomBlastSource config (default "vixen").
func (c *Calculator) doomBlastSourceRate(db *ModDB, main *activeSkill, sourceOut map[string]float64, trigRate, icdr float64, w1 hitSource, w2 *hitSource) {
	out := db.output
	src := "vixen"
	if db.build != nil {
		src = configString(db.build.ActiveConfig(), "doomBlastSource", src)
	}
	switch src {
	case "expiration":
		if d := sourceOut["Duration"]; d > 0 && 1/d <= trigRate {
			out["EffectiveSourceRate"] = 1 / d
		}
	case "hexblast":
		var rate float64
		found := false
		for _, s := range db.activeSkills {
			if s.active.ge.Name == "Hexblast" && !isTriggered(s) && s != main && !s.disabled {
				o := c.offenceOnCopy(db, s, w1, w2)
				r, ok := o["HitSpeed"]
				if !ok {
					r = o["Speed"]
				}
				if !found || r > rate {
					rate, found = r, true
				}
			}
		}
		if found && rate > 0 {
			out["EffectiveSourceRate"] = 1 / (1/trigRate + 1/rate)
		}
	case "vixen":
		vixensCD := 0.0
		if v := c.data.Skills.Effects["SupportUniqueCastCurseOnCurse"]; v != nil {
			if lv := v.Levels[1]; lv != nil {
				vixensCD = lv.Cooldown / icdr
			}
		}
		out["EffectiveSourceRate"] = rotationImpact(main, []*simSkill{{skill: main, icdr: icdr}}, trigRate, vixensCD, 0)
	}
}

// cwcTrigger is CWCHandler: Cast while Channelling triggers at the
// channelled skill's trigger interval.
func (c *Calculator) cwcTrigger(db *ModDB, main *activeSkill) {
	out := db.output
	var source *activeSkill
	var triggered []*simSkill
	for _, s := range db.activeSkills {
		match := slotMatch(main, s)
		if source == nil && s.data["triggerTime"] != nil && s.types[stChannel] && s != main && match && !isTriggered(s) {
			source = s
		}
		if truthy(s.data["triggeredWhileChannelling"]) && match {
			triggered = append(triggered, packageSim(s))
		}
	}
	if source == nil || len(triggered) == 0 {
		delete(main.data, "triggered")
		return
	}
	icdr := calcMod(main.mods, main.cfg, "CooldownRecovery")
	if icdr == 0 {
		icdr = 1
	}
	interval := numOr(source.data["triggerTime"], 0)
	if interval <= 0 {
		return
	}
	rateOfTrigger := 1 / interval
	total := numOr(main.data["cooldown"], 0) / icdr
	if o, ok := main.mods.OverrideCfg(main.cfg, "CooldownRecovery"); ok {
		total = o
	}
	adj := math.Ceil(total*serverTickRate) / serverTickRate
	eff := math.Ceil(adj*rateOfTrigger) / rateOfTrigger
	if eff > 0 {
		out["TriggerRateCap"] = math.Min(1/eff, rateOfTrigger)
	} else {
		out["TriggerRateCap"] = rateOfTrigger
	}
	rate := rotationImpact(main, triggered, rateOfTrigger, 0, 100)
	if db.Flag("HaveTriggerBots") && main.types[stSpell] {
		rate *= 2
	}
	out["SkillTriggerRate"] = rate
	for _, typ := range []string{"INC", "MORE"} {
		for _, m := range appliedMods(main.mods, main.cfg, typ, "TriggeredDamage") {
			main.mods.Add(Mod{Name: "Damage", Type: typ, Value: m.Value, Flags: m.Flags, KeywordFlags: m.KeywordFlags, Source: m.Source, Tags: m.Tags})
		}
	}
	main.data["triggered"] = true
	main.data["triggerRate"] = rate
	main.flags["globalTrigger"] = true
}
