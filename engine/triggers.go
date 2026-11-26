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

// defaultTriggerChance is calcMultiSpellRotationImpact's chance when the
// caller passes none (CalcTriggers.lua:110).
const defaultTriggerChance = 100

// triggerConfig is an entry of CalcTriggers.lua configTable.
type triggerConfig struct {
	triggerOnUse bool
	// triggerChance is config.triggerChance when hasTriggerChance (0 is a
	// set value: Lua's "or" keeps it).
	triggerChance      float64
	hasTriggerChance   bool
	triggerSkillCond   func(main, s *activeSkill) bool
	triggeredSkillCond func(main, s *activeSkill) bool
	overlaps           float64 // 0: not set
	doomBlast          bool
	// sourceValue is the config's comparer: the source with the largest
	// value wins (default: the highest rate).
	sourceValue func(o map[string]float64) float64
	// ignoreSourceRate: the source rate is the trigger rate cap.
	ignoreSourceRate bool
	// selfSource is config.source = mainSkill (a global trigger): no
	// source search, and the source rate is the trigger rate cap.
	selfSource bool
	// source, sourceOut, trigRate: a source the config already chose
	// (config.source / config.trigRate), so no source search.
	source    *activeSkill
	sourceOut map[string]float64
	trigRate  float64
	// triggerName is config.triggerName: the infoTrigger label when the
	// config sets its own.
	triggerName string
	// preTriggered is skillData.triggered as it was before calcs.triggers
	// ran (hasPreTriggered: present): the state of the env PoB builds for
	// the triggered skill with its trigger limited (manaforgedRate).
	preTriggered    any
	hasPreTriggered bool
	// actor is config.actor when it is env.minion (the "nova" config): its
	// output takes the rates and its ModDB the HaveTriggerBots flag; nil
	// is the player.
	actor *ModDB
}

// withChance is the config with config.triggerChance = v when v is set.
func (cfg *triggerConfig) withChance(v any) *triggerConfig {
	if f, ok := num(v); ok {
		cfg.triggerChance, cfg.hasTriggerChance = f, true
	}
	return cfg
}

// isTriggered is isTriggered (CalcTriggers.lua:41-43).
func isTriggered(s *activeSkill) bool {
	return truthy(s.data.val("triggeredByUnique")) || truthy(s.data.val("triggered")) ||
		s.types[stInbuiltTrigger] || s.types[stTriggered] || s.active.srcTriggered
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
		main.data.set("ignoresTickRate", true)
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
				return truthy(s.data.val("triggeredByUnique")) && main.group != nil && s.group != nil &&
					main.group.Slot == s.group.Slot && s.types[stSpell]
			}}
	},
	// CalcTriggers.lua configTable["poet's pen"]: a socketed spell triggered
	// by wand attacks.
	"poet's pen": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerOnUse: true,
			triggerSkillCond: func(_, s *activeSkill) bool {
				return (s.types[stDamage] || s.types[stAttack]) && s.cfg.Flags&modFlagByName["Wand"] > 0
			},
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByUnique")) && main.group != nil && s.group != nil &&
					main.group.Slot == s.group.Slot && s.types[stSpell]
			}}
	},
	"the covetous dark": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond:   func(_, s *activeSkill) bool { return s.types[stMelee] },
			triggeredSkillCond: func(_, s *activeSkill) bool { return truthy(s.data.val("triggeredByCovetousDark")) }}
	},
	"cast when stunned": func(_ *ModDB, main *activeSkill) *triggerConfig {
		return (&triggerConfig{selfSource: true}).withChance(main.data.val("chanceToTriggerOnStun"))
	},
	"cast on ward break": func(_ *ModDB, main *activeSkill) *triggerConfig {
		return (&triggerConfig{selfSource: true}).withChance(main.data.val("chanceToTriggerOnWardBreak"))
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
				return s.types[stAttack] && s.types[stProjectile] && s.cfg.Flags&modFlagByName["Wand"] > 0 &&
					!truthy(s.data.val("triggeredBySpellSlinger"))
			}}
	},
	// CalcTriggers.lua:976-987: Rain of Arrows triggered by (Replica)
	// Lioneye's Paws gets a 1 second cooldown and bow attacks as sources.
	"lioneye's paws":         lioneyesPaws,
	"replica lioneye's paws": lioneyesPaws,
	// CalcTriggers.lua:1196-1210: a global trigger without a source search;
	// nil (the next name is tried) when the skill has no damage threshold.
	"cast when damage taken": func(db *ModDB, main *activeSkill) *triggerConfig {
		// if skillData.triggeredByDamageTaken: any number, 0 included, is truthy.
		th, ok := num(main.data.val("triggeredByDamageTaken"))
		if !ok {
			return nil
		}
		db.output["CWDTThreshold"] = th * calcMod(main.mods, nil, "CWDTThreshold")
		return &triggerConfig{selfSource: true}
	},
	// CalcTriggers.lua:1311-1314.
	"tempest shield": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{selfSource: true}
	},
	// CalcTriggers.lua:1382-1387.
	"shockwave":      meleeSlotTrigger,
	"void shockwave": meleeSlotTrigger,
	// CalcTriggers.lua:1399-1403; the mana spent rate is in defaultTrigger.
	"manaforged arrows": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerOnUse: true, triggerName: "Manaforged Arrows",
			triggerSkillCond: func(_, s *activeSkill) bool {
				return s.types[stAttack] && s.cfg.Flags&modFlagByName["Bow"] > 0
			}}
	},
	"cast on critical strike": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond: func(main, s *activeSkill) bool { return s.types[stAttack] && slotMatch(main, s) },
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByCoc")) && slotMatch(main, s)
			}}
	},
}

func lioneyesPaws(_ *ModDB, main *activeSkill) *triggerConfig {
	main.data.cooldown = optF{1, true}
	return &triggerConfig{triggerOnUse: true,
		triggerSkillCond: func(_, s *activeSkill) bool {
			return s.types[stAttack] && s.cfg.Flags&modFlagByName["Bow"] > 0
		}}
}

func meleeSlotTrigger(*ModDB, *activeSkill) *triggerConfig {
	return &triggerConfig{
		triggerSkillCond: func(main, s *activeSkill) bool { return s.types[stMelee] && slotMatch(main, s) },
	}
}

var sourceItemRe = regexp.MustCompile(`^.*:.*:(.*),.*$`)

// uniqueTriggerName is getUniqueItemTriggerName.
func uniqueTriggerName(s *activeSkill) string {
	if v, ok := s.data.val("triggerSource").(string); ok {
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
	return &simSkill{skill: s, cd: s.data.cooldown.or(0), cdOverride: o, hasOverride: has,
		icdr:          calcMod(s.mods, s.cfg, "CooldownRecovery"),
		addedCooldown: s.mods.SumCfg(s.cfg, "BASE", "CooldownRecovery")}
}

// rotationImpact is calcMultiSpellRotationImpact: the main skill's trigger
// rate in a rotation of triggered skills.
func rotationImpact(main *activeSkill, rotation []*simSkill, sourceRate, triggerCD, chance float64) float64 {
	if sourceRate <= 0 || len(rotation) == 0 {
		return 0
	}
	inc := 1 / sourceRate
	simTime := inc * 1000
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
				rotation[cur].nextTrig = serverTickTime * math.Ceil(
					(float64(serverTickTime*math.Floor(next/serverTickTime))+rotation[cur].cd)/serverTickTime,
				)
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
			return 1 / (simTime/s.count + float64(inc/chance*100) - inc)
		}
	}
	return 0
}

// triggers is calcs.triggers for the player's main skill: the trigger rate
// in skillData.triggerRate, or the skill counts as self-cast.
// triggerNames are the configTable keys calcs.triggers tries after the
// skill id (CalcTriggers.lua:1620-1632): the skill's name, its trigger
// support's name with and without "awakened ", and the unique item's.
func triggerNames(main *activeSkill) []string {
	names := []string{strings.ToLower(main.active.ge.Name)}
	if by := triggeredBy(main); by != nil {
		n := strings.ToLower(by.ge.Name)
		names = append(names, n, strings.TrimPrefix(n, "awakened "))
	}
	if isTriggered(main) {
		if u := uniqueTriggerName(main); u != "" {
			names = append(names, strings.ToLower(u))
		}
	}
	return names
}

func (c *Calculator) triggers(db *ModDB, main *activeSkill, w1 hitSource, w2 *hitSource) {
	if main.disabled {
		return
	}
	by := triggeredBy(main)
	names := triggerNames(main)
	for _, n := range names {
		if n == "cast while channelling" {
			c.cwcTrigger(db, main)
			return
		}
		if n == "snipe" {
			c.snipeTrigger(db, main, by, w1, w2)
			return
		}
	}
	// config = config or configTable[name](env): a config function that
	// returns nil falls through to the next name (CalcTriggers.lua:1627-1631).
	// The first key is the granted effect id, as it is (configTable[skillId]).
	preTriggered, hasPreTriggered := main.data.get("triggered")
	var cfg *triggerConfig
	for _, n := range append([]string{main.active.id}, names...) {
		if f := triggerConfigs[n]; f != nil {
			if cfg = f(db, main); cfg != nil {
				break
			}
		}
	}
	if cfg == nil {
		main.data.del("triggered")
		return
	}
	cfg.preTriggered, cfg.hasPreTriggered = preTriggered, hasPreTriggered
	// config.triggerChance = config.triggerChance or srcInstance.triggerChance.
	if !cfg.hasTriggerChance && main.active.srcHasTriggerChance {
		cfg.triggerChance, cfg.hasTriggerChance = main.active.srcTriggerChance, true
	}
	c.defaultTrigger(db, main, by, cfg, w1, w2)
}

func (c *Calculator) defaultTrigger(db *ModDB, main *activeSkill, by *skillEffect, cfg *triggerConfig, w1 hitSource, w2 *hitSource) {
	actor := db
	if cfg.actor != nil {
		actor = cfg.actor
	}
	out := actor.output
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
				if s.cfg.SkillCond["usedByMirage"] || s.types[stOtherThingUsesSkill] {
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
	if cfg.selfSource {
		source, sourceOut = main, map[string]float64{}
		cfg.ignoreSourceRate = true
	}
	if cfg.source != nil {
		source, sourceOut, trigRate = cfg.source, cfg.sourceOut, cfg.trigRate
	}
	if source == nil {
		main.data.del("triggered")
		return
	}
	main.data.set("triggered", true)
	// defaultTriggerHandler: infoTrigger = config.triggerName, which
	// calcs.triggers defaults to triggeredBy's name, else the skill's.
	main.infoTrigger = main.active.ge.Name
	if by != nil {
		main.infoTrigger = by.ge.Name
	}
	if cfg.triggerName != "" {
		main.infoTrigger = cfg.triggerName
	}
	if w2 != nil && main.weapons[0] != "" && main.weapons[1] != "" &&
		!truthy(source.data.val("doubleHitsWhenDualWielding")) &&
		(source.types[stMelee] || source.types[stAttack]) && by != nil && by.ge.FromItem {
		trigRate /= 2
	}
	trigRate = c.manaforgedRate(db, main, cfg, sourceOut, trigRate, w1, w2)
	ignoresTick := truthy(main.data.val("ignoresTickRate")) || main.data.storedUses.or(0) > 1
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
	triggeredCD := main.data.cooldown.or(0)
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
	main.data.set("triggerSourceTotemLife", sourceOut["TotemLife"])
	if cfg.doomBlast {
		c.doomBlastSourceRate(db, main, sourceOut, trigRate, icdr, w1, w2)
	}
	chance := 100.0
	//nolint:nestif // pre-existing shape of the Lua port; only the SkillType constant was renamed
	if (source.types[stMelee] || source.types[stAttack]) && !cfg.triggerOnUse {
		if hc := sourceOut["HitChance"]; hc != 100 {
			chance *= hc / 100
		}
		if truthy(main.data.val("triggerOnCrit")) {
			if !cfg.hasTriggerChance {
				cfg.withChance(main.data.val("chanceToTriggerOnCrit"))
			}
			if cc := sourceOut["CritChance"]; cc != 100 {
				chance *= cc / 100
			}
		}
	}
	if cfg.hasTriggerChance && cfg.triggerChance != 100 {
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
	case cfg.ignoreSourceRate && cfg.triggeredSkillCond == nil:
		// skillFlags.globalTrigger: no source rate breakpoints.
		skillRate = out["EffectiveSourceRate"]
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
		if actor.Flag("HaveTriggerBots") && main.types[stSpell] {
			skillRate *= 2
		}
	}
	out["SkillTriggerRate"] = skillRate
	main.data.set("triggerRate", skillRate)
	// addTriggerIncMoreMods: TriggeredDamage applies to the triggered skill.
	for _, typ := range []string{"INC", "MORE"} {
		for _, m := range appliedMods(main.mods, source.cfg, typ, "TriggeredDamage") {
			main.mods.Add(Mod{Name: "Damage", Type: typ, Value: m.Value, Flags: m.Flags, KeywordFlags: m.KeywordFlags, Source: m.Source, Tags: m.Tags})
		}
	}
}

// manaforgedRate is the Manaforged Arrows part of defaultTriggerHandler
// (CalcTriggers.lua:540-575): the source rate divided by the source uses
// needed to spend the threshold, a multiple of the triggered skill's own
// mana cost; 0 when the source costs no mana.
func (c *Calculator) manaforgedRate(
	db *ModDB,
	main *activeSkill,
	cfg *triggerConfig,
	sourceOut map[string]float64,
	trigRate float64,
	w1 hitSource,
	w2 *hitSource,
) float64 {
	if !truthy(main.data.val("triggeredByManaforged")) || trigRate <= 0 {
		return trigRate
	}
	triggeredCost := c.triggeredManaCost(db, main, cfg, w1, w2)
	if triggeredCost <= 0 {
		return trigRate
	}
	threshold := triggeredCost * numOr(main.data.val("ManaForgedArrowsPercentThreshold"), 0)
	sourceCost := sourceOut["ManaCostRaw"]
	if sourceCost <= 0 {
		return 0
	}
	return trigRate / math.Ceil(threshold/sourceCost)
}

// triggeredManaCost is the triggered skill's ManaCostRaw as PoB caches it:
// calcs.buildActiveSkill(env, mode, mainSkill, uuid, {uuid}) (Calcs.lua:365-400)
// runs calcs.perform on a fresh env with the skill as the main skill and
// its uuid in limitedSkills, so calcs.triggers skips it (CalcTriggers.lua:1619):
// skillData.triggered is what the skill setup gave, no TriggeredDamage mods
// are added and the trigger config has not run. The copy here is the skill
// before this trigger pass: skillData.triggered restored to its value when
// calcs.triggers began (manaforgedRate runs before addTriggerIncMoreMods,
// and the Manaforged Arrows config changes nothing on the skill). The perform
// part (CalcPerform.lua:247's conditions) saw that same state in this Calc.
// The cost is computed once per Calc and skill (PoB caches it by uuid).
func (c *Calculator) triggeredManaCost(
	db *ModDB,
	main *activeSkill,
	cfg *triggerConfig,
	w1 hitSource,
	w2 *hitSource,
) float64 {
	if v, ok := db.triggeredCostCache[main]; ok {
		return v
	}
	cp := cloneSkill(main)
	if cfg.hasPreTriggered {
		cp.data.set("triggered", cfg.preTriggered)
	} else {
		cp.data.del("triggered")
	}
	v := c.offenceOnCopy(db, cp, w1, w2)["ManaCostRaw"]
	if db.triggeredCostCache == nil {
		db.triggeredCostCache = map[*activeSkill]float64{}
	}
	db.triggeredCostCache[main] = v
	return v
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
		// CalcTriggers.lua:720-723.
		if g := db.items["Gloves"]; g == nil || !strings.Contains(g.Name, "Vixen's Entrapment") {
			out["VixenModeNoVixenGlovesWarn"] = 1
		}
		vixensCD := 0.0
		if v := c.data.Skills.Effects["SupportUniqueCastCurseOnCurse"]; v != nil {
			if lv := v.Levels[1]; lv != nil {
				vixensCD = lv.Cooldown / icdr
			}
		}
		// chance nil: calcMultiSpellRotationImpact's default.
		rot := []*simSkill{{skill: main, icdr: icdr}}
		out["EffectiveSourceRate"] = rotationImpact(main, rot, trigRate, vixensCD, defaultTriggerChance)
		out["VixensTooMuchCastSpeedWarn"] = b2f(vixensCD > 1/trigRate) // CalcTriggers.lua:729
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
		if source == nil && s.data.val("triggerTime") != nil && s.types[stChannel] && s != main && match &&
			!isTriggered(s) {
			source = s
		}
		if truthy(s.data.val("triggeredWhileChannelling")) && match {
			triggered = append(triggered, packageSim(s))
		}
	}
	if source == nil || len(triggered) == 0 {
		main.data.del("triggered")
		return
	}
	icdr := calcMod(main.mods, main.cfg, "CooldownRecovery")
	if icdr == 0 {
		icdr = 1
	}
	interval := numOr(source.data.val("triggerTime"), 0)
	if interval <= 0 {
		return
	}
	rateOfTrigger := 1 / interval
	total := main.data.cooldown.or(0) / icdr
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
	db.output["ChannelTimeToTrigger"] = interval // CalcTriggers.lua:397
	main.data.set("triggered", true)
	main.data.set("triggerRate", rate)
	main.flags["globalTrigger"] = true
}

// snipeTrigger is the "snipe" trigger config (CalcTriggers.lua). Snipe
// channels stages (config Multiplier:SnipeStage, at most SnipeStagesMax)
// and on release triggers one supported bow skill per stage. As the main
// skill Snipe hits with its stage multipliers when it supports nothing,
// and deals no damage itself when it does. A supported skill gets the
// stage multipliers and Snipe's hit rate (hitTimeMultiplier stages - 0.5)
// as the trigger rate, or is self-cast when Snipe lacks the stages.
func (c *Calculator) snipeTrigger(db *ModDB, main *activeSkill, by *skillEffect, w1 hitSource, w2 *hitSource) {
	// PoB compares socketGroup.slot of skills with a group; a main skill
	// without one (granted) has no Snipe to pair with.
	if main.group == nil {
		main.data.del("triggered")
		return
	}
	stages := math.Min(db.Sum("BASE", "Multiplier:SnipeStage"), db.Sum("BASE", "Multiplier:SnipeStagesMax"))
	hitMulti := main.mods.SumCfg(main.cfg, "BASE", "snipeHitMulti")
	ailmentMulti := main.mods.SumCfg(main.cfg, "BASE", "snipeAilmentMulti")
	slot := func(s *activeSkill) string {
		if s.group == nil {
			return ""
		}
		return s.group.Slot
	}
	var triggered []*activeSkill
	for _, s := range db.activeSkills {
		if truthy(s.data.val("triggeredBySnipe")) && s.group != nil && slot(s) == slot(main) {
			triggered = append(triggered, s)
		}
	}
	stageMods := func() {
		tag := map[string]any{"type": "Multiplier", "var": "SnipeStages"}
		main.mods.NewMod("Multiplier:SnipeStages", "BASE", stages, "Snipe")
		main.mods.Add(Mod{Name: "Damage", Type: "MORE", Value: hitMulti, Flags: mfHit, Source: "Snipe", Tags: []map[string]any{tag}})
		main.mods.Add(Mod{Name: "Damage", Type: "MORE", Value: ailmentMulti, Flags: mfAilment, Source: "Snipe", Tags: []map[string]any{tag}})
	}
	if main.active.ge.Name == "Snipe" {
		// max(1, stages) as other channelled ranged skills; the first stage
		// takes half the time.
		main.data.set("hitTimeMultiplier", math.Max(1, stages)-0.5) //nolint:mnd // PoB literal
		if len(triggered) == 0 {
			stageMods()
			return
		}
		for _, t := range []string{"Lightning", "Cold", "Fire", "Chaos", "Physical"} {
			main.mods.Add(Mod{Name: "DealNo" + t, Type: "FLAG", Value: true,
				Tags: []map[string]any{{"type": "SkillName", "skillName": "Snipe", "includeTransfigured": true}}})
		}
		return
	}
	idx := 0
	for i, s := range triggered {
		if s == main {
			idx = i + 1
			break
		}
	}
	if idx == 0 || float64(idx) > stages {
		main.data.del("triggered")
		return
	}
	stageMods()
	var source *activeSkill
	var sourceOut map[string]float64
	trigRate := 0.0
	for _, s := range db.activeSkills {
		if s.active.ge.Name != "Snipe" || s.group == nil || slot(s) != slot(main) {
			continue
		}
		s.data.set("hitTimeMultiplier", stages-0.5) //nolint:mnd // PoB literal
		o := c.offenceOnCopy(db, s, w1, w2)
		speed, ok := o["HitSpeed"]
		if !s.disabled && !s.cfg.SkillCond["usedByMirage"] && !s.types[stOtherThingUsesSkill] && ok &&
			(source == nil || speed > trigRate) {
			source, sourceOut, trigRate = s, o, speed
			db.output["ChannelTimeToTrigger"] = o["HitTime"]
		}
	}
	if source == nil {
		main.data.del("triggered")
		return
	}
	c.defaultTrigger(db, main, by, &triggerConfig{source: source, sourceOut: sourceOut, trigRate: trigRate}, w1, w2)
}

// minionTriggerConfig is the configTable entry name for the minion mdb
// when the entry names env.minion as its actor, else nil.
func minionTriggerConfig(name string, mdb *ModDB) *triggerConfig {
	if name == "nova" {
		// CalcTriggers.lua:1188-1195: the Holy Relic's Nova, triggered by
		// the player's attacks. Its triggeredSkills list is the minion's
		// main skill alone and it has no triggeredSkillCond.
		return &triggerConfig{actor: mdb, triggerName: "Summon Holy Relic",
			triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stAttack] }}
	}
	return nil
}

// minionTriggers is calcs.triggers(env, env.minion) (CalcPerform.lua:3844):
// the minion's main skill ms looks its config up as the player's does; the
// trigger sources are the player's skills (env.player.activeSkillList),
// run with the player's weapons. Player configs a minion skill could
// match are not ported for minions and leave the skill as it is.
func (c *Calculator) minionTriggers(db, mdb *ModDB, ms *activeSkill, res *Result) {
	if ms.disabled {
		return
	}
	var cfg *triggerConfig
	for _, n := range append([]string{ms.active.id}, triggerNames(ms)...) {
		if cfg = minionTriggerConfig(n, mdb); cfg != nil {
			break
		}
		if triggerConfigs[n] != nil || n == "cast while channelling" || n == "snipe" {
			res.Incomplete = append(res.Incomplete, "minion skill trigger "+n+" is not ported")
			return
		}
	}
	if cfg == nil {
		ms.data.del("triggered")
		return
	}
	// config.triggerChance = config.triggerChance or srcInstance.triggerChance.
	if ms.active.srcHasTriggerChance {
		cfg.triggerChance, cfg.hasTriggerChance = ms.active.srcTriggerChance, true
	}
	classID := -1
	if b := db.theBuild(); b != nil && b.ActiveSpec() != nil {
		classID = b.ActiveSpec().ClassID
	}
	w1, w2 := playerWeapons(db, db.gearMods, classID)
	c.defaultTrigger(db, ms, triggeredBy(ms), cfg, w1, w2)
}
