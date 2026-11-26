package engine

import (
	"math"
	"regexp"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
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
	// comparer is config.comparer (defaultComparer when nil): whether a
	// candidate's output o beats the source so far (hasSource) whose rate
	// is trigRate (CalcTriggers.lua:69-72).
	comparer func(o map[string]float64, hasSource bool, trigRate float64) bool
	// sourceWeapon is config.sourceWeapon: only attacks with the weapon
	// the trigger is on are sources, and a source attacking with both
	// weapons triggers at half its rate (CalcTriggers.lua:418-420, 425,
	// 462).
	sourceWeapon bool
	// minimumCost is config.minimumCostPerUse: a source must cost at least
	// minimum of stat per use (findTriggerSkill, CalcTriggers.lua:97).
	minimumCost *triggerCost
	// cached: the config's own source has a GlobalCache entry (it returns
	// its uuid, as "trigger craft" does), so the handler reads its output.
	cached bool
	// sourceCopy is the source as calculated for the cache entry
	// (GlobalCache.cachedData[uuid].ActiveSkill), when cached.
	sourceCopy *activeSkill
	// hasTrigRate: trigRate is set (Lua's trigRate ~= nil).
	hasTrigRate bool
	// byGE replaces actor.mainSkill.triggeredBy.grantedEffect, as the Call
	// to Arms, Automation and Autoexertion configs do for the cooldown of
	// the active part (CalcTriggers.lua:1235-1240).
	byGE *gamedata.Skill
	// brand is the Arcanist Brand activation set on
	// actor.mainSkill.triggeredBy (CalcTriggers.lua:1345-1357).
	brand *brandTrigger
	// durationCap is the Shattershard config's trigger rate cap override:
	// 1 / the skill's own duration (CalcTriggers.lua:1315-1330).
	durationCap bool
	// craft is the "trigger craft" config: its source search
	// (craftSource) runs before the handler.
	craft bool
	// keepTriggered is the Cast on Death config: no handler runs and the
	// skill stays triggered.
	keepTriggered bool
	// ignoreSourceRate: the source rate is the trigger rate cap.
	ignoreSourceRate bool
	// selfSource is a global trigger with config.source = mainSkill
	// (skillFlags.globalTrigger): no source search, and the source rate is
	// the trigger rate cap.
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

// triggerCost is config.minimumCostPerUse.
type triggerCost struct {
	stat    string
	minimum float64
}

// brandTrigger is the Arcanist Brand part of actor.mainSkill.triggeredBy:
// the brand's repeatFrequency, its activation frequency multipliers and
// its attached count (CalcTriggers.lua:1345-1356).
type brandTrigger struct {
	repeatFrequency, freqInc, freqMore, attached float64
}

// defaultComparer is CalcTriggers.lua:69-72.
func defaultComparer(o map[string]float64, hasSource bool, trigRate float64) bool {
	speed, ok := o["HitSpeed"]
	if !ok {
		speed, ok = o["Speed"]
	}
	return (!hasSource && ok) || (ok && speed > trigRate)
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
	// CalcTriggers.lua:1211-1220.
	"cast when stunned": func(_ *ModDB, main *activeSkill) *triggerConfig {
		main.flags["globalTrigger"] = true
		return (&triggerConfig{selfSource: true}).withChance(main.data.val("chanceToTriggerOnStun"))
	},
	"cast on ward break": func(_ *ModDB, main *activeSkill) *triggerConfig {
		main.flags["globalTrigger"] = true
		return (&triggerConfig{selfSource: true}).withChance(main.data.val("chanceToTriggerOnWardBreak"))
	},
	"avenging flame": func(*ModDB, *activeSkill) *triggerConfig {
		// CalcTriggers.lua:1490-1497, with one difference (user decision):
		// PoB's comparer gets the current source's rate as
		// currentTotemLife (findTriggerSkill passes triggerRate, 97), so a
		// totem wins when its life exceeds that rate. In the game each
		// totem triggers its own Avenging Flame, which deals damage from
		// that totem's life, and the trigger rate is the rate cap
		// (ignoreSourceRate), so here totem life is compared with the
		// current source's totem life: the source is the largest totem.
		cfg := &triggerConfig{ignoreSourceRate: true,
			triggerSkillCond: func(main, s *activeSkill) bool { return s.flags["totem"] && slotMatch(main, s) }}
		cfg.comparer = func(o map[string]float64, hasSource bool, _ float64) bool {
			life, ok := o["TotemLife"]
			return (!hasSource && ok) || (ok && life > cfg.sourceOut["TotemLife"])
		}
		return cfg
	},
	"spellslinger": func(_ *ModDB, main *activeSkill) *triggerConfig {
		// CalcTriggers.lua:1221-1224: the Spellslinger skill itself has no
		// effective rate; sourceRateIsFinal only changes the breakdown.
		if main.active.ge.Name == "Spellslinger" { //nolint:goconst // PoB skill name
			main.flags["skipEffectiveRate"] = true
		}
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
		main.flags["globalTrigger"] = true
		return &triggerConfig{selfSource: true}
	},
	// CalcTriggers.lua:1311-1314.
	"tempest shield": func(_ *ModDB, main *activeSkill) *triggerConfig {
		main.flags["globalTrigger"] = true
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
	// The rest of CalcTriggers.lua configTable (917-1599). Lua configs that
	// only set breakdown or display values (sourceRateIsFinal, useCastRate,
	// assumingEveryHitKills, sourceName, customTriggerName, infoMessage)
	// have nothing else to port for those fields.
	// CalcTriggers.lua:918-924.
	"law of the wilds": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerSkillCond: func(_, s *activeSkill) bool {
			return !s.types[stSummonsTotem] && meleeOrAttack(s) && s.cfg.Flags&modFlagByName["Claw"] > 0
		}}
	},
	// CalcTriggers.lua:925-934.
	"the rippling thoughts": func(_ *ModDB, main *activeSkill) *triggerConfig {
		if main.active.ge.Name != "Storm Cascade" {
			return nil
		}
		return &triggerConfig{triggerSkillCond: anyMeleeOrAttack}
	},
	// CalcTriggers.lua:935-945.
	"the surging thoughts": func(_ *ModDB, main *activeSkill) *triggerConfig {
		if main.active.ge.Name != "Storm Cascade" {
			return nil
		}
		return &triggerConfig{triggerOnUse: true, triggerSkillCond: anyMeleeOrAttack}
	},
	// CalcTriggers.lua:946-960: triggered at most twice a second while
	// phasing; otherwise the skill is disabled and the config returns
	// nothing.
	"the hidden blade": func(db *ModDB, main *activeSkill) *triggerConfig {
		main.flags["globalTrigger"] = true
		main.data.set("triggerRateCapOverride", 2.0) //nolint:mnd // PoB literal
		if db.Flag("Condition:Phasing") {
			return &triggerConfig{selfSource: true}
		}
		main.disabled = true
		return nil
	},
	// CalcTriggers.lua:961-968.
	"replica eternity shroud": globalSelfTrigger,
	"shroud of the lightless": globalSelfTrigger,
	// CalcTriggers.lua:969-977.
	"limbsplit":      namedTrigger("Gore Shockwave", anyMeleeOrAttack),
	"the cauteriser": namedTrigger("Gore Shockwave", anyMeleeOrAttack),
	"duskblight":     namedTrigger("Stalking Pustule", anyDamageOrAttack),
	// CalcTriggers.lua:990-994: as Replica Lioneye's Paws, the skill gets a
	// 1 second cooldown.
	"moonbender's wing": func(_ *ModDB, main *activeSkill) *triggerConfig {
		main.data.cooldown = optF{1, true}
		return &triggerConfig{triggerName: "Lightning Warp", triggerSkillCond: anyMeleeOrAttack}
	},
	// CalcTriggers.lua:995-1006.
	"ngamahu's flame":     namedTrigger("Molten Burst", anyMeleeOrAttack),
	"cameria's avarice":   namedTrigger("Icicle Burst", anyDamageOrAttack),
	"starcaller":          namedTrigger("Starfall", anyMeleeOrAttack),
	"uul-netol's embrace": namedTrigger("Bone Nova", anyDamageOrAttack),
	// CalcTriggers.lua:1007-1031.
	"rigwald's crest":       namedTrigger("", anyDamageOrAttack),
	"jorrhast's blacksteel": namedTrigger("", anyDamageOrAttack),
	"ashcaller":             namedTrigger("", anyDamageOrAttack),
	"arakaali's fang":       namedTrigger("", anyDamageOrAttack),
	"sporeguard":            namedTrigger("", anyDamageOrAttack),
	"mark of the elder":     namedTrigger("", anyDamageOrAttack),
	"mark of the shaper":    namedTrigger("", anyDamageOrAttack),
	// CalcTriggers.lua:1041-1053: the unique's name comes from the modSource
	// of the item in the skill's slot; the replica is triggered by spells,
	// the original by bow attacks.
	"maloney's mechanism": func(db *ModDB, main *activeSkill) *triggerConfig {
		it := db.items[main.cfg.SlotName]
		if it == nil {
			return nil
		}
		m := sourceItemRe.FindStringSubmatch("Item:0:" + it.Name + ", " + it.BaseName)
		if m == nil {
			return nil
		}
		name := m[1]
		replica := regexp.MustCompile(`Replica.`).MatchString(name)
		return &triggerConfig{triggerOnUse: true, triggerName: name,
			triggerSkillCond: func(_, s *activeSkill) bool {
				attack := s.types[stAttack] && s.cfg.Flags&modFlagByName["Bow"] > 0 && !replica
				spell := s.types[stSpell] && replica
				return attack || spell
			},
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByUnique")) && sameSlot(main, s) && s.types[stRangedAttack]
			}}
	},
	// CalcTriggers.lua:1063-1068.
	"vixen's entrapment": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stHex] }}
	},
	// CalcTriggers.lua:1069-1080.
	"flames of judgement": queensDemandTrigger,
	"storm of judgement":  queensDemandTrigger,
	// CalcTriggers.lua:1081-1101: the source search is the handler's
	// (craftSource).
	"trigger craft": func(_ *ModDB, main *activeSkill) *triggerConfig {
		if !truthy(main.data.val("triggeredByCraft")) {
			return nil
		}
		return &triggerConfig{craft: true}
	},
	// CalcTriggers.lua:1102-1121: any skill that costs enough mana (life)
	// per use.
	"kitava's thirst": func(db *ModDB, _ *activeSkill) *triggerConfig {
		//nolint:goconst // the ManaCost output key, as in buildoutput.go
		cfg := &triggerConfig{triggerName: "Kitava's Thirst",
			minimumCost:      &triggerCost{"ManaCost", db.Sum("BASE", "KitavaRequiredManaCost")},
			triggerSkillCond: func(_, _ *activeSkill) bool { return true }}
		return cfg.withChance(db.Sum("BASE", "KitavaTriggerChance"))
	},
	"foulborn kitava's thirst": func(db *ModDB, _ *activeSkill) *triggerConfig {
		cfg := &triggerConfig{triggerName: "Foulborn Kitava's Thirst",
			minimumCost:      &triggerCost{"LifeCost", db.Sum("BASE", "FoulbornKitavaRequiredLifeCost")},
			triggerSkillCond: func(_, _ *activeSkill) bool { return true }}
		return cfg.withChance(db.Sum("BASE", "FoulbornKitavaTriggerChance"))
	},
	// CalcTriggers.lua:1122-1129.
	"mjolner": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond: func(main, s *activeSkill) bool {
				return damageOrAttack(s) && s.cfg.Flags&(modFlagByName["Mace"]|mfWeapon1H) > 0 && !slotMatch(main, s)
			},
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByMjolner")) && slotMatch(main, s)
			}}
	},
	// CalcTriggers.lua:1130-1137.
	"wing of the wyvern": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond: func(main, s *activeSkill) bool {
				return damageOrAttack(s) && s.cfg.Flags&modFlagByName["Bow"] > 0 && !slotMatch(main, s)
			},
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByUnique")) && slotMatch(main, s)
			}}
	},
	// CalcTriggers.lua:1138-1143.
	"cospri's malice": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond: func(_, s *activeSkill) bool {
				return s.types[stMelee] && s.cfg.Flags&(modFlagByName["Sword"]|mfWeapon1H) > 0
			},
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByCospris")) && sameSlot(main, s)
			}}
	},
	// CalcTriggers.lua:1144-1148.
	"temporal mirage": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerOnUse: true,
			triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stMovement] && s.types[stAttack] },
			triggeredSkillCond: func(_, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByTemporalMirage"))
			}}
	},
	// CalcTriggers.lua:1153-1156.
	"seven teachings": func(*ModDB, *activeSkill) *triggerConfig {
		both := modFlagByName["Unarmed"] | mfMelee
		return &triggerConfig{
			triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stMelee] && s.cfg.Flags&both == both },
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredBySevenTeachings")) && slotMatch(main, s)
			}}
	},
	// CalcTriggers.lua:1157-1169: needs recent kills.
	"squirming terror": func(db *ModDB, _ *activeSkill) *triggerConfig {
		if !db.Flag("Condition:KilledRecently") {
			return nil
		}
		return &triggerConfig{
			triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stAttack] && s.types[stMelee] },
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredBySquirmingTerror")) && slotMatch(main, s)
			}}
	},
	// CalcTriggers.lua:1170-1173.
	"kinetic flux": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond: func(main, s *activeSkill) bool { return s.types[stAttack] && slotMatch(main, s) },
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByKineticFlux")) && slotMatch(main, s)
			}}
	},
	// CalcTriggers.lua:1178-1190: needs recent kills.
	"cast on melee kill": func(db *ModDB, _ *activeSkill) *triggerConfig {
		if !db.Flag("Condition:KilledRecently") {
			return nil
		}
		return &triggerConfig{
			triggerSkillCond: func(main, s *activeSkill) bool {
				return s.types[stAttack] && s.types[stMelee] && slotMatch(main, s)
			},
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByMeleeKill")) && slotMatch(main, s)
			}}
	},
	// CalcTriggers.lua:1229-1292.
	"call to arms": activePartTrigger("Call to Arms", true),
	"automation":   activePartTrigger("Automation", false),
	"autoexertion": activePartTrigger("Autoexertion", true),
	// CalcTriggers.lua:1293-1298.
	"seize the flesh": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerSkillCond: func(main, s *activeSkill) bool {
			return s.types[stWarcry] && slotMatch(main, s)
		}}
	},
	"fissure": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerOnUse: true, triggerSkillCond: func(main, s *activeSkill) bool {
			return s.types[stSlam] && slotMatch(main, s)
		}}
	},
	// CalcTriggers.lua:1299-1310.
	"mark on hit": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stAttack] }}
	},
	"hextouch": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerSkillCond: func(main, s *activeSkill) bool {
			return s.types[stAttack] && slotMatch(main, s)
		}}
	},
	"oskarm": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stAttack] }}
	},
	// CalcTriggers.lua:1315-1330.
	"shattershard": func(_ *ModDB, main *activeSkill) *triggerConfig {
		main.flags["globalTrigger"] = true
		return &triggerConfig{selfSource: true, durationCap: true}
	},
	// CalcTriggers.lua:1331-1342: a melee source counts only when the cry
	// exerts it (it has an uptime ratio).
	"battlemage's cry": func(_ *ModDB, main *activeSkill) *triggerConfig {
		if main.active.ge.Name == "Battlemage's Cry" { //nolint:goconst // PoB skill name
			return nil
		}
		return &triggerConfig{
			triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stMelee] },
			comparer:         uptimeComparer("BattlemageUpTimeRatio"),
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByBattleMageCry")) && slotMatch(main, s)
			}}
	},
	// CalcTriggers.lua:1343-1361: the source is the Arcanist Brand, at its
	// activation rate.
	"arcanist brand": func(db *ModDB, main *activeSkill) *triggerConfig {
		if main.active.ge.Name == "Arcanist Brand" {
			return nil
		}
		b := skillNamed(db, "Arcanist Brand")
		if b == nil {
			// triggeredBy.mainSkill is nil: PoB errors here.
			return nil
		}
		bt := &brandTrigger{
			repeatFrequency: numOr(b.data.val("repeatFrequency"), 0),
			freqInc:         (100 + b.mods.SumCfg(b.cfg, "INC", "Speed", "BrandActivationFrequency")) / 100, //nolint:mnd // percent
			freqMore:        b.mods.MoreCfg(b.cfg, "BrandActivationFrequency"),
			attached:        numOr(b.data.val("attachedBrandCount"), 0),
		}
		return &triggerConfig{brand: bt, source: b, hasTrigRate: true,
			trigRate: bt.repeatFrequency * bt.freqInc * bt.freqMore * bt.attached,
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByBrand")) && slotMatch(main, s)
			}}
	},
	// CalcTriggers.lua:1362-1366, with one difference (user decision): the
	// Lua config returns nothing, so calcs.triggers tries the next name and,
	// with no other config, clears skillData.triggered again (1634). In the
	// game the skill is triggered (on death), so here the config stops the
	// lookup and keeps it triggered, without a trigger rate.
	"cast on death": func(_ *ModDB, main *activeSkill) *triggerConfig {
		main.flags["globalTrigger"] = true
		main.data.set("triggered", true)
		return &triggerConfig{keepTriggered: true}
	},
	// CalcTriggers.lua:1367-1374.
	"combust": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stMelee] },
			comparer:         uptimeComparer("InfernalUpTimeRatio"),
		}
	},
	// CalcTriggers.lua:1375-1380.
	"prismatic burst": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerSkillCond: func(main, s *activeSkill) bool {
			return s.types[stAttack] && slotMatch(main, s)
		}}
	},
	"voidstorm": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerOnUse: true, triggerSkillCond: func(main, s *activeSkill) bool {
			return s.types[stAttack] && s.types[stRain] && slotMatch(main, s)
		}}
	},
	// CalcTriggers.lua:1388-1390.
	"falling crystal": meleeSlotTrigger,
	// CalcTriggers.lua:1391-1398: needs an ignited enemy.
	"call the pyre": func(db *ModDB, _ *activeSkill) *triggerConfig {
		if db.enemy == nil || !db.enemy.Flag("Condition:Ignited") {
			return nil
		}
		return (&triggerConfig{triggerSkillCond: func(main, s *activeSkill) bool {
			return s.types[stMelee] && slotMatch(main, s)
		}}).withChance(50.0) //nolint:mnd // PoB literal
	},
	// CalcTriggers.lua:1501-1516: the source is the Intuitive Link skill at
	// the configured rate.
	"intuitive link": func(db *ModDB, main *activeSkill) *triggerConfig {
		if main.active.ge.Name == "Intuitive Link" {
			return nil
		}
		il := skillNamed(db, "Intuitive Link")
		cfg := &triggerConfig{trigRate: db.Sum("BASE", "IntuitiveLinkSourceRate"), hasTrigRate: true,
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return s.types[stSpell] && slotMatch(main, s) && s != il
			}}
		if il != nil {
			cfg.source = il
		}
		return cfg
	},
	// CalcTriggers.lua:1517-1532.
	"svalinn cast on block":              castOnBlockTrigger,
	"festering resentment cast on block": castOnBlockTrigger,
	// CalcTriggers.lua:1533-1542.
	"hex on trap": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stTrapped] },
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredByTrapTrigger")) && slotMatch(main, s)
			}}
	},
	// CalcTriggers.lua:1543-1552.
	"supporttriggerfirespellonhit": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{
			triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stMelee] },
			triggeredSkillCond: func(main, s *activeSkill) bool {
				return truthy(s.data.val("triggeredBySettlersEnchantTrigger")) && slotMatch(main, s)
			}}
	},
	// CalcTriggers.lua:1553-1561.
	"ghostly artillery": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{sourceWeapon: true, triggerOnUse: true,
			triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stAttack] }}
	},
	// CalcTriggers.lua:1562-1568.
	"replica gifts from above": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerSkillCond: func(_, s *activeSkill) bool {
			return !s.types[stSummonsTotem] && s.types[stAttack]
		}}
	},
	// CalcTriggers.lua:1569-1573 (a skill id key).
	"BloodShrineUniqueTriggeredExplodingToad": func(db *ModDB, main *activeSkill) *triggerConfig {
		// srcInstance.triggerChance + ...: PoB errors when the gem has none.
		chance := main.active.srcTriggerChance + db.Sum("BASE", "BloodShrineExplodingToadTriggerChance")
		cfg := &triggerConfig{triggerSkillCond: anyDamageOrAttack}
		return cfg.withChance(math.Min(chance, 100)) //nolint:mnd // percent
	},
	// CalcTriggers.lua:1574-1593: the shortest Hextoad interval of any skill
	// caps the trigger rate.
	"bursting toad": func(db *ModDB, main *activeSkill) *triggerConfig {
		interval := math.Inf(1)
		for _, s := range db.activeSkills {
			if v, ok := num(s.data.val("hextoadTriggerInterval")); ok {
				interval = math.Min(interval, v)
			}
		}
		if math.IsInf(interval, 1) {
			return nil
		}
		main.flags["globalTrigger"] = true
		main.data.set("triggerRateCapOverride", 1/interval)
		return &triggerConfig{selfSource: true}
	},
	// CalcTriggers.lua:1594-1599 (skill id keys).
	"TriggeredMoltenStrike": namedTrigger("", anyMeleeOrAttack),
	"FieryImpactHeistMaceImplicit": func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{sourceWeapon: true, triggerSkillCond: anyMeleeOrAttack}
	},
}

func meleeOrAttack(s *activeSkill) bool  { return s.types[stMelee] || s.types[stAttack] }
func damageOrAttack(s *activeSkill) bool { return s.types[stDamage] || s.types[stAttack] }

func anyMeleeOrAttack(_, s *activeSkill) bool  { return meleeOrAttack(s) }
func anyDamageOrAttack(_, s *activeSkill) bool { return damageOrAttack(s) }

// sameSlot is env.player.mainSkill.socketGroup.slot == skill.socketGroup.slot.
func sameSlot(main, s *activeSkill) bool {
	return main.group != nil && s.group != nil && main.group.Slot == s.group.Slot
}

// namedTrigger is a config with only a triggerName (when not "") and a
// triggerSkillCond.
func namedTrigger(name string, cond func(main, s *activeSkill) bool) func(*ModDB, *activeSkill) *triggerConfig {
	return func(*ModDB, *activeSkill) *triggerConfig {
		return &triggerConfig{triggerName: name, triggerSkillCond: cond}
	}
}

// globalSelfTrigger is a global trigger with the skill as its own source.
func globalSelfTrigger(_ *ModDB, main *activeSkill) *triggerConfig {
	main.flags["globalTrigger"] = true
	return &triggerConfig{selfSource: true}
}

// skillNamed is the first active skill whose granted effect has name.
func skillNamed(db *ModDB, name string) *activeSkill {
	for _, s := range db.activeSkills {
		if s.active.ge.Name == name {
			return s
		}
	}
	return nil
}

// queensDemandTrigger is CalcTriggers.lua:1069-1080.
func queensDemandTrigger(_ *ModDB, main *activeSkill) *triggerConfig {
	return &triggerConfig{triggerName: main.active.ge.Name,
		triggerSkillCond: func(_, s *activeSkill) bool { return s.active.ge.Name == "Queen's Demand" },
		triggeredSkillCond: func(main, s *activeSkill) bool {
			return truthy(s.data.val("triggeredByUnique")) && sameSlot(main, s)
		}}
}

// uptimeComparer is the Battlemage's Cry and Combust comparer: the default
// one, for a source with the warcry's uptime ratio.
func uptimeComparer(key string) func(map[string]float64, bool, float64) bool {
	return func(o map[string]float64, hasSource bool, trigRate float64) bool {
		_, ok := o[key]
		return defaultComparer(o, hasSource, trigRate) && ok
	}
}

// activePartTrigger is the Call to Arms, Automation and Autoexertion
// configs (CalcTriggers.lua:1229-1292): the skill itself is a global
// trigger without an effective rate; a supported skill takes its trigger
// cooldown from the active part's granted effect. ignoresTick: Call to
// Arms and Autoexertion set skillData.ignoresTickRate.
func activePartTrigger(name string, ignoresTick bool) func(*ModDB, *activeSkill) *triggerConfig {
	return func(db *ModDB, main *activeSkill) *triggerConfig {
		cfg := &triggerConfig{triggerOnUse: true, selfSource: true}
		if main.active.ge.Name == name {
			main.flags["globalTrigger"] = true
			main.flags["skipEffectiveRate"] = true
		} else if s := skillNamed(db, name); s != nil {
			cfg.byGE = s.active.ge
		}
		if ignoresTick {
			main.data.set("ignoresTickRate", true)
		}
		return cfg
	}
}

// castOnBlockTrigger is CalcTriggers.lua:1517-1532: a global trigger for
// the skills its trigger support can support.
func castOnBlockTrigger(_ *ModDB, main *activeSkill) *triggerConfig {
	main.flags["globalTrigger"] = true
	return &triggerConfig{selfSource: true,
		triggeredSkillCond: func(main, s *activeSkill) bool {
			by := triggeredBy(s)
			return slotMatch(main, s) && by != nil && canSupport(by, s)
		}}
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
	adds, _ := processAddedCastTime(s)
	return &simSkill{skill: s, cd: s.data.cooldown.or(0), cdOverride: o, hasOverride: has,
		icdr:          calcMod(s.mods, s.cfg, "CooldownRecovery"),
		addedCooldown: s.mods.SumCfg(s.cfg, "BASE", "CooldownRecovery"), addsCast: adds}
}

// processAddedCastTime is CalcTriggers.lua:45-62: with
// SpellCastTimeAddedToCooldownIfTriggered the skill's cast time, at its
// cast speed rounded to two places, adds to its trigger cooldown; ok is
// false (Lua's nil) without it.
func processAddedCastTime(s *activeSkill) (float64, bool) {
	if s == nil || s.mods == nil || !s.mods.FlagCfg(s.cfg, "SpellCastTimeAddedToCooldownIfTriggered") {
		return 0, false
	}
	base := 1.0
	if v, ok := num(s.data.val("castTimeOverride")); ok {
		base = v
	} else if !s.active.ge.NoCastTime {
		base = s.active.ge.CastTime
	}
	speed := (1 + s.mods.SumCfg(s.cfg, "INC", "Speed")/100) * s.mods.MoreCfg(s.cfg, "Speed")
	csi := round(speed, 2) //nolint:mnd // two places
	s.flags["addsCastTime"] = true
	return base / csi, true
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
		switch n {
		case "cast while channelling":
			c.cwcTrigger(db, main)
			return
		case "snipe": //nolint:goconst // configTable key
			c.snipeTrigger(db, main, by, w1, w2)
			return
		case "focus":
			// CalcTriggers.lua:1417-1419: the custom handler.
			c.focusTrigger(db, main, by)
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
	if cfg.keepTriggered {
		return
	}
	cfg.preTriggered, cfg.hasPreTriggered = preTriggered, hasPreTriggered
	// config.triggerChance = config.triggerChance or srcInstance.triggerChance.
	if !cfg.hasTriggerChance && main.active.srcHasTriggerChance {
		cfg.triggerChance, cfg.hasTriggerChance = main.active.srcTriggerChance, true
	}
	if cfg.craft {
		c.craftSource(db, main, cfg, w1, w2)
	}
	c.defaultTrigger(db, main, by, cfg, w1, w2)
}

// findTriggerSkill is CalcTriggers.lua:76-101: s replaces the config's
// source when the comparer prefers its output, it pays the minimum cost
// per use and it is not used by a mirage or another thing.
func (c *Calculator) findTriggerSkill(db *ModDB, s *activeSkill, cfg *triggerConfig, w1 hitSource, w2 *hitSource) {
	o, cp := c.offenceOnClone(db, s, w1, w2)
	cmp := cfg.comparer
	if cmp == nil {
		cmp = defaultComparer
	}
	if !cmp(o, cfg.source != nil, cfg.trigRate) {
		return
	}
	if m := cfg.minimumCost; m != nil && o[m.stat] < m.minimum {
		return
	}
	if s.disabled || s.cfg.SkillCond["usedByMirage"] || s.types[stOtherThingUsesSkill] {
		return
	}
	rate, ok := o["HitSpeed"]
	if !ok {
		rate = o["Speed"]
	}
	cfg.source, cfg.sourceOut, cfg.sourceCopy, cfg.trigRate = s, o, cp, rate
	cfg.hasTrigRate, cfg.cached = true, true
}

// craftSource is the source search of configTable["trigger craft"]
// (CalcTriggers.lua:1079-1099): the fastest damaging skill not from an
// item and not triggered itself. A totem, golem, banner or ballista
// source triggers at its cast rate (with its cooldown).
//
// One difference (user decision): in PoB (1086-1093) any such skill met
// in the search sets the rate, whether or not it became the source, so a
// later non-summon source keeps it or is compared against it. Here only
// the skill that is the source sets it: the game triggers at the real
// source's rate.
func (c *Calculator) craftSource(db *ModDB, main *activeSkill, cfg *triggerConfig, w1 hitSource, w2 *hitSource) {
	for _, s := range db.activeSkills {
		if !s.types[stDamage] && !s.types[stAttack] && !s.types[stSpell] || s.flags["aura"] || s == main ||
			truthy(s.data.val("triggeredByCraft")) || s.active.ge.FromItem || isTriggered(s) {
			continue
		}
		c.findTriggerSkill(db, s, cfg, w1, w2)
		summons := s.flags["totem"] || s.flags["golem"] || s.flags["banner"] || s.flags["ballista"]
		if summons && !s.active.ge.NoCastTime && cfg.source == s {
			cd := 0.0
			if lv := s.active.ge.Levels[s.active.level]; lv != nil {
				cd = lv.Cooldown
			}
			cfg.trigRate, cfg.hasTrigRate = 1/(s.active.ge.CastTime+cd), true
		}
	}
}

// focusTrigger is helmetFocusHandler (CalcTriggers.lua:150-234): a spell
// triggered when you Focus, assuming Focus is used again as soon as its
// cooldown ends.
func (c *Calculator) focusTrigger(db *ModDB, main *activeSkill, by *skillEffect) {
	if main.flags["minion"] || main.disabled || by == nil {
		return
	}
	main.data.set("triggered", true)
	out := db.output
	triggerCD := 0.0
	if lv := by.ge.Levels[by.level]; lv != nil {
		triggerCD = lv.Cooldown
	}
	triggeredCD := main.data.cooldown.or(0)
	icdrFocus := calcMod(main.mods, main.cfg, "FocusCooldownRecovery")
	icdrSkill := calcMod(main.mods, main.cfg, "CooldownRecovery")
	focusDuration, focusCD := 0.0, 0.0
	if f := c.data.Skills.Effects["Focus"]; f != nil {
		if len(f.ConstantStats) > 0 {
			focusDuration = f.ConstantStats[0].Value / 1000 //nolint:mnd // milliseconds
		}
		if lv := f.Levels[1]; lv != nil {
			focusCD = lv.Cooldown / icdrFocus
		}
	}
	// One difference (user decision): PoB (CalcTriggers.lua:172) does not
	// divide the triggered skill's own cooldown by its cooldown recovery,
	// only the trigger's; in the game cooldown recovery applies to both.
	action := math.Max(triggeredCD/icdrSkill, triggerCD/icdrSkill)
	rounded := math.Ceil(action*serverTickRate) / serverTickRate
	out["TriggerRateCap"] = math.Inf(1)
	if rounded != 0 {
		out["TriggerRateCap"] = 1 / rounded
	}
	out["SkillTriggerRate"] = 1 / (focusDuration + focusCD)
	addTriggerIncMoreMods(main, main)
	main.infoTrigger = "Focus"
	main.data.set("triggerRate", out["SkillTriggerRate"])
	main.flags["globalTrigger"] = true
}

// triggeredOnDeath is the Cast on Death part of calcs.perform's skill loop
// (CalcPerform.lua:1475-1483): a player skill triggered on death is
// triggered, its TriggeredDamage, with env.player.mainSkill's cfg,
// applies as Damage, and its trigger time is a minute (1484, in ms as PoB
// writes it: "any large value would do").
func triggeredOnDeath(skills []*activeSkill) {
	pm := mainSkill(skills)
	if pm == nil {
		return
	}
	for _, s := range skills {
		if !truthy(s.data.val("triggeredOnDeath")) || s.flags["minion"] {
			continue
		}
		s.data.set("triggered", true)
		addTriggerIncMoreMods(s, pm)
		s.data.set("triggerTime", 60.0*1000) //nolint:mnd // CalcPerform.lua:1484
	}
}

// addTriggerIncMoreMods is CalcTriggers.lua:24-31: TriggeredDamage, with
// the source's cfg, applies to the triggered skill as Damage.
//
//nolint:goconst // PoB mod names and types
func addTriggerIncMoreMods(main, source *activeSkill) {
	for _, typ := range []string{"INC", "MORE"} {
		for _, m := range appliedMods(main.mods, source.cfg, typ, "TriggeredDamage") {
			main.mods.Add(Mod{Name: "Damage", Type: typ, Value: m.Value, Flags: m.Flags,
				KeywordFlags: m.KeywordFlags, Source: m.Source, Tags: m.Tags})
		}
	}
}

// allProjectilesRe is CalcTriggers.lua:488's "(.*)All(.*)Projectiles(.*)".
var allProjectilesRe = regexp.MustCompile(`All.*Projectiles`)

// exertedRate is the Battlemage's Cry and Infernal Cry uptime part of
// defaultTriggerHandler (CalcTriggers.lua:503-540): the source's hits
// during the cry's duration, capped by the exert count, every cooldown
// plus cast time.
func exertedRate(o map[string]float64, rate float64, prefix, cry string) float64 {
	dur := ceilB(o[cry+"Duration"], serverTickTime)
	cd := ceilB(o[cry+"Cooldown"], serverTickTime)
	hits := math.Max(math.Min(rate*dur, o[prefix+"ExertsCount"]), 0)
	return hits / (o[cry+"CastTime"] + cd)
}

// ceilB is ceil_b (Modules/Common.lua:1014).
func ceilB(x, base float64) float64 { return float64(base * math.Ceil(x/base)) }

// bothHandsChance is the chance that either hand's roll of stat
// succeeds, for a source that hits with both weapons
// (CalcTriggers.lua:758-764).
func bothHandsChance(cp *activeSkill, stat string) float64 {
	mh, oh := handStat(cp, 1, stat), handStat(cp, 2, stat) //nolint:mnd // off hand
	return mh*oh/100 + mh*(100-oh)/100 + (100-mh)*oh/100   //nolint:mnd // percent
}

// handStat is the copy's output.MainHand (hand 1) or OffHand stat.
func handStat(cp *activeSkill, hand int, stat string) float64 {
	if cp == nil {
		return 0
	}
	cfg := cp.weapon1Cfg
	if hand == 2 { //nolint:mnd // off hand
		cfg = cp.weapon2Cfg
	}
	if cfg == nil {
		return 0
	}
	return cfg.SkillStats[stat]
}

//nolint:gocognit,gocyclo,cyclop,funlen,maintidx // defaultTriggerHandler as one function
func (c *Calculator) defaultTrigger(
	db *ModDB,
	main *activeSkill,
	by *skillEffect,
	cfg *triggerConfig,
	w1 hitSource,
	w2 *hitSource,
) {
	actor := db
	if cfg.actor != nil {
		actor = cfg.actor
	}
	out := actor.output
	global := func() bool { return main.flags["globalTrigger"] }
	// infoTrigger = config.triggerName, which calcs.triggers defaults to
	// triggeredBy's name, else the skill's, before the config runs.
	label := main.active.ge.Name
	if by != nil {
		label = by.ge.Name
	}
	if cfg.triggerName != "" {
		label = cfg.triggerName
	}
	if cfg.byGE != nil && by != nil {
		nb := *by
		nb.ge = cfg.byGE
		by = &nb
	}
	if cfg.selfSource {
		cfg.source, cfg.sourceOut, cfg.cached = main, nil, false
	}
	// CalcTriggers.lua:418-420: a source-weapon trigger takes attacks with
	// the weapon in its slot only.
	weaponFlag := ""
	if cfg.sourceWeapon && main.group != nil && main.group.Slot != "" {
		weaponFlag = "weapon1Attack"
		if strings.HasPrefix(main.group.Slot, "Weapon 2") {
			weaponFlag = "weapon2Attack"
		}
	}
	var triggered []*simSkill
	if cfg.triggerSkillCond != nil || cfg.triggeredSkillCond != nil {
		for _, s := range db.activeSkills {
			// CalcTriggers.lua:425: triggered skills are sources of a global
			// trigger only (config.allowTriggered is set by no config).
			cand := cfg.triggerSkillCond != nil && cfg.triggerSkillCond(main, s)
			cand = cand && (weaponFlag == "" || s.flags[weaponFlag]) && (!isTriggered(s) || global())
			if cand && s != main && !s.disabled {
				c.findTriggerSkill(db, s, cfg, w1, w2)
			}
			if cfg.triggeredSkillCond != nil && cfg.triggeredSkillCond(main, s) {
				triggered = append(triggered, packageSim(s))
			}
		}
	}
	if len(triggered) == 0 && cfg.triggeredSkillCond != nil {
		return
	}
	source, sourceOut, cp := cfg.source, cfg.sourceOut, cfg.sourceCopy
	trigRate, hasRate, cached := cfg.trigRate, cfg.hasTrigRate, cfg.cached && cfg.sourceOut != nil
	// CalcTriggers.lua:433: a global trigger with triggered skills goes on
	// without a source; PoB then indexes the nil source, so it counts as
	// self-cast here.
	if source == nil {
		main.data.del("triggered")
		main.infoTrigger = ""
		return
	}
	main.data.set("triggered", true)
	main.infoTrigger = label
	dualWield := w2 != nil && main.weapons[0] != "" && main.weapons[1] != ""
	// CalcTriggers.lua:461-468: dual wield triggers.
	bothWeapons := source.flags["weapon1Attack"] && source.flags["weapon2Attack"]
	itemSupport := by != nil && by.ge.Support && by.ge.FromItem
	if hasRate && dualWield && !truthy(source.data.val("doubleHitsWhenDualWielding")) && meleeOrAttack(source) &&
		(cfg.sourceWeapon && bothWeapons || itemSupport) {
		trigRate /= 2
	}
	ignoresTick := truthy(main.data.val("ignoresTickRate")) || main.data.storedUses.or(0) > 1
	if cached { //nolint:nestif // CalcTriggers.lua:475-494
		// CalcTriggers.lua:475-485: Unleash.
		if source.mods.Flag("HasSeals") && source.types[stCanRapidFire] {
			if cp != nil {
				trigRate *= cp.data.dpsMultiplier.or(1)
			}
			main.flags["HasSeals"] = true
			ignoresTick = true
		}
		// CalcTriggers.lua:487-494: skills that hit with all projectiles.
		if source.flags["projectile"] && allProjectilesRe.MatchString(partName(source, nil)) {
			n, ok := sourceOut["ProjectileCount"]
			if !ok {
				n = 1
			}
			trigRate *= n
		}
	}
	if ignoresTick {
		main.data.set("ignoresTickRate", true)
	}
	// CalcTriggers.lua:496-503: Kitava's Thirst; repeats use no mana.
	if truthy(main.data.val("triggeredByManaSpent")) {
		trigRate /= 1 + source.mods.Sum("BASE", "RepeatCount")
	}
	if cached && source.types[stMelee] {
		if truthy(main.data.val("triggeredByBattleMageCry")) {
			trigRate = exertedRate(sourceOut, trigRate, "BattleCry", "BattleMageCry")
		}
		if main.active.ge.Name == "Combust" {
			trigRate = exertedRate(sourceOut, trigRate, "Infernal", "InfernalCry")
		}
	}
	trigRate = c.manaforgedRate(db, main, cfg, sourceOut, trigRate, w1, w2)
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
	// CalcTriggers.lua:619-622: a brand's activation cooldown, which
	// cooldown recovery does not change.
	if cfg.brand != nil && truthy(main.data.val("triggeredByBrand")) {
		triggerCD = cfg.brand.repeatFrequency / cfg.brand.freqMore / cfg.brand.freqInc * icdr
	}
	triggeredAdj := (triggeredCD + added) / icdr
	// CalcTriggers.lua:634: output.addsCastTime is env.player.mainSkill's.
	pm := mainSkill(db.activeSkills)
	if pm == nil {
		pm = main
	}
	addsCast, hasAdds := processAddedCastTime(pm)
	if hasAdds {
		out["addsCastTime"] = addsCast
	}
	triggerAdj := (triggerCD + addsCast) / icdr
	tick := func(v float64) float64 { return math.Ceil(v*serverTickRate) / serverTickRate }
	triggeredRounded := tick(triggeredAdj)
	if ignoresTick {
		triggeredRounded = triggeredAdj
	}
	// triggeredBy.ignoresTickRate: set by the Arcanist Brand config.
	triggerRounded := tick(triggerAdj)
	if cfg.brand != nil {
		triggerRounded = triggerAdj
	}
	actionRounded := math.Max(triggerRounded, triggeredRounded)
	if hasOverride {
		actionRounded = tick(override)
	}
	// CalcTriggers.lua:643-646.
	rateCap := math.Inf(1)
	if source == main {
		if cfg.durationCap {
			if d := c.ownDuration(db, main, cfg, w1, w2); d != 0 {
				main.data.set("triggerRateCapOverride", 1/d)
			}
		}
		if v, ok := num(main.data.val("triggerRateCapOverride")); ok {
			rateCap = v
		}
	}
	if actionRounded != 0 {
		rateCap = 1 / actionRounded
	}
	out["TriggerRateCap"] = rateCap
	// CalcTriggers.lua:729-743.
	if hasRate && !global() && !cfg.ignoreSourceRate {
		out["EffectiveSourceRate"] = trigRate
	} else {
		out["EffectiveSourceRate"] = rateCap
		main.flags["globalTrigger"] = true
	}
	main.data.set("triggerSourceTotemLife", sourceOut["TotemLife"])
	if cfg.doomBlast {
		c.doomBlastSourceRate(db, main, sourceOut, trigRate, icdr, w1, w2)
	}
	chance := 100.0
	//nolint:nestif // CalcTriggers.lua:752-801
	if cached && meleeOrAttack(source) && !cfg.triggerOnUse {
		both := dualWield && truthy(source.data.val("doubleHitsWhenDualWielding"))
		if hc := sourceOut["HitChance"]; hc != 100 {
			if both {
				chance *= bothHandsChance(cp, "HitChance") / 100 //nolint:mnd // percent
			} else {
				chance *= hc / 100 //nolint:mnd // percent
			}
		}
		if truthy(main.data.val("triggerOnCrit")) {
			if !cfg.hasTriggerChance {
				cfg.withChance(main.data.val("chanceToTriggerOnCrit"))
			}
			if !cfg.hasTriggerChance && cp != nil {
				cfg.withChance(cp.data.val("chanceToTriggerOnCrit"))
			}
			if cc := sourceOut["CritChance"]; cc != 100 {
				if both {
					chance *= bothHandsChance(cp, "CritChance") / 100 //nolint:mnd // percent
				} else {
					chance *= cc / 100 //nolint:mnd // percent
				}
			}
		}
	}
	if cfg.hasTriggerChance && cfg.triggerChance != 100 {
		chance *= cfg.triggerChance / 100
	}
	var skillRate float64
	switch {
	// CalcTriggers.lua:747: also when env.player.mainSkill skips the
	// effective rate.
	case out["EffectiveSourceRate"] == 0 || pm.flags["skipEffectiveRate"]:
		skillRate = 0
	// CalcTriggers.lua:816, with one difference (user decision): PoB's
	// check that the triggered skills are the skill alone compares
	// triggeredSkills[1] with a newly built table, so it is never true and
	// a trigger with a triggeredSkillCond never uses the charge-based rate.
	// Here the check works: the only triggered skill is the skill itself.
	case ignoresTick && (cfg.triggeredSkillCond == nil || len(triggered) == 1 && triggered[0].skill == main):
		overlaps := cfg.overlaps
		if overlaps == 0 {
			overlaps = 1
		}
		skillRate = math.Min(out["TriggerRateCap"], out["EffectiveSourceRate"]*overlaps)
	case global() && cfg.triggeredSkillCond == nil:
		// No source rate breakpoints.
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
		if truthy(main.data.val("triggeredByBrand")) {
			cd = 0
		}
		skillRate = rotationImpact(main, rot, out["EffectiveSourceRate"], cd, chance)
		if actor.Flag("HaveTriggerBots") && main.types[stSpell] {
			skillRate *= 2
		}
	}
	out["SkillTriggerRate"] = skillRate
	main.data.set("triggerRate", skillRate)
	addTriggerIncMoreMods(main, source)
}

// ownDuration is the Shattershard config's calcs.buildActiveSkill of the
// skill itself, with its trigger limited (CalcTriggers.lua:1317-1321):
// its output.Duration.
func (c *Calculator) ownDuration(
	db *ModDB,
	main *activeSkill,
	cfg *triggerConfig,
	w1 hitSource,
	w2 *hitSource,
) float64 {
	cp := cloneSkill(main)
	if cfg.hasPreTriggered {
		cp.data.set("triggered", cfg.preTriggered)
	} else {
		cp.data.del("triggered")
	}
	return c.offenceOnCopy(db, cp, w1, w2)["Duration"]
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
	// CalcTriggers.lua:261, 276: the added cast time counts against the
	// triggered skill's cooldown.
	addsCast, hasAdds := processAddedCastTime(main)
	if hasAdds {
		out["addsCastTime"] = addsCast
	}
	total := math.Max(main.data.cooldown.or(0), addsCast) / icdr
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

// snipeTrigger is calcs.triggers for a player's main skill that looks up
// the "snipe" config: the config's source, when it gives one, goes
// through the default handler.
func (c *Calculator) snipeTrigger(db *ModDB, main *activeSkill, by *skillEffect, w1 hitSource, w2 *hitSource) {
	if cfg := c.snipeConfig(db, main, w1, w2); cfg != nil {
		c.defaultTrigger(db, main, by, cfg, w1, w2)
	}
}

// snipeConfig is configTable["snipe"] (CalcTriggers.lua:529-604) with pm
// as env.player.mainSkill, which the config reads and changes whichever
// actor calcs.triggers runs for. Snipe channels stages (config
// Multiplier:SnipeStage, at most SnipeStagesMax) and on release triggers
// one supported bow skill per stage. As the main skill Snipe hits with its
// stage multipliers when it supports nothing, and deals no damage itself
// when it does. A supported skill gets the stage multipliers and Snipe's
// hit rate (hitTimeMultiplier stages - 0.5) as the trigger rate, or is
// self-cast when Snipe lacks the stages. The result is nil when the config
// returns nothing (Snipe itself, or too few stages: then pm is no longer
// triggered); otherwise its source may be nil, which the default handler
// treats as self-cast.
//
//nolint:gocognit // moved from snipeTrigger as it was
func (c *Calculator) snipeConfig(db *ModDB, pm *activeSkill, w1 hitSource, w2 *hitSource) *triggerConfig {
	// PoB compares socketGroup.slot of skills with a group; a main skill
	// without one (granted) has no Snipe to pair with.
	if pm.group == nil {
		pm.data.del("triggered")
		return nil
	}
	stages := math.Min(db.Sum("BASE", "Multiplier:SnipeStage"), db.Sum("BASE", "Multiplier:SnipeStagesMax"))
	hitMulti := pm.mods.SumCfg(pm.cfg, "BASE", "snipeHitMulti")
	ailmentMulti := pm.mods.SumCfg(pm.cfg, "BASE", "snipeAilmentMulti")
	slot := func(s *activeSkill) string {
		if s.group == nil {
			return ""
		}
		return s.group.Slot
	}
	var triggered []*activeSkill
	for _, s := range db.activeSkills {
		if truthy(s.data.val("triggeredBySnipe")) && s.group != nil && slot(s) == slot(pm) {
			triggered = append(triggered, s)
		}
	}
	stageMods := func() {
		tag := map[string]any{"type": "Multiplier", "var": "SnipeStages"}
		pm.mods.NewMod("Multiplier:SnipeStages", "BASE", stages, "Snipe")
		pm.mods.Add(
			Mod{
				Name:   "Damage",
				Type:   "MORE",
				Value:  hitMulti,
				Flags:  mfHit,
				Source: "Snipe", //nolint:goconst // PoB source
				Tags:   []map[string]any{tag},
			},
		)
		pm.mods.Add(
			Mod{
				Name:   "Damage",
				Type:   "MORE",
				Value:  ailmentMulti,
				Flags:  mfAilment,
				Source: "Snipe",
				Tags:   []map[string]any{tag},
			},
		)
	}
	if pm.active.ge.Name == "Snipe" {
		// max(1, stages) as other channelled ranged skills; the first stage
		// takes half the time.
		pm.data.set("hitTimeMultiplier", math.Max(1, stages)-0.5) //nolint:mnd // PoB literal
		if len(triggered) == 0 {
			stageMods()
			return nil
		}
		for _, t := range []string{"Lightning", "Cold", "Fire", "Chaos", "Physical"} {
			pm.mods.Add(Mod{Name: "DealNo" + t, Type: "FLAG", Value: true, //nolint:goconst // PoB mod type
				Tags: []map[string]any{{"type": "SkillName", "skillName": "Snipe", "includeTransfigured": true}}})
		}
		return nil
	}
	idx := 0
	for i, s := range triggered {
		if s == pm {
			idx = i + 1
			break
		}
	}
	if idx == 0 || float64(idx) > stages {
		pm.data.del("triggered")
		return nil
	}
	stageMods()
	cfg := &triggerConfig{}
	for _, s := range db.activeSkills {
		if s.active.ge.Name != "Snipe" || s.group == nil || slot(s) != slot(pm) {
			continue
		}
		s.data.set("hitTimeMultiplier", stages-0.5) //nolint:mnd // PoB literal
		o := c.offenceOnCopy(db, s, w1, w2)
		speed, ok := o["HitSpeed"]
		if !s.disabled && !s.cfg.SkillCond["usedByMirage"] && !s.types[stOtherThingUsesSkill] && ok &&
			(cfg.source == nil || speed > cfg.trigRate) {
			cfg.source, cfg.sourceOut, cfg.trigRate, cfg.hasTrigRate = s, o, speed, true
			db.output["ChannelTimeToTrigger"] = o["HitTime"]
		}
	}
	return cfg
}

// minionTriggerConfig is configTable[name](env) for calcs.triggers(env,
// env.minion), with pm as env.player.mainSkill (the summoning skill) and
// mdb as env.minion; nil when the entry returns nothing. The entries are
// those a minion's main skill can reach (see minionTriggers): "nova" and
// "snipe".
func (c *Calculator) minionTriggerConfig(
	name string,
	db, mdb *ModDB,
	pm *activeSkill,
	w1 hitSource,
	w2 *hitSource,
) *triggerConfig {
	var cfg *triggerConfig
	switch name {
	case "nova":
		// CalcTriggers.lua:1188-1195: the Holy Relic's Nova, triggered by
		// the player's attacks. Its triggeredSkills list is the minion's
		// main skill alone and it has no triggeredSkillCond.
		cfg = &triggerConfig{triggerName: "Summon Holy Relic",
			triggerSkillCond: func(_, s *activeSkill) bool { return s.types[stAttack] }}
	case "snipe":
		// CalcTriggers.lua:529-604 reads env.player.mainSkill only: the
		// summoning skill is not supported by Snipe, so it loses
		// skillData.triggered and the config returns nothing, unless it is
		// one of Snipe's triggered skills; then the player's Snipe is the
		// source of the minion's skill.
		cfg = c.snipeConfig(db, pm, w1, w2)
	}
	if cfg != nil {
		// config.actor = config.actor or actor (CalcTriggers.lua:743).
		cfg.actor = mdb
	}
	return cfg
}

// minionTriggers is calcs.triggers(env, env.minion) (CalcPerform.lua:3844,
// CalcTriggers.lua:727-752): the minion's main skill ms looks its config
// up by its id, its name, its trigger support's name and its unique
// item's name; the trigger sources are the player's skills
// (env.player.activeSkillList), run with the player's weapons, and pm is
// env.player.mainSkill, the summoning skill.
//
// Of the configTable entries only "nova" (RelicTriggeredNova) and "snipe"
// (the Snipe of AtlasEyrieArcherSnipe and LegionKaruiArcherSnipe) match a
// minion skill's id or name in Data/Minions.lua and Data/Spectres.lua. A
// minion skill has no trigger support: calcLib.canGrantedEffectSupport-
// ActiveSkill rejects isTrigger supports for an actor other than the
// player (CalcTools.lua:114). Its unique item name would come from
// skillData.triggerSource, which no minion skill has, or from a supportList
// entry that is not a support, which CalcSetup.lua never adds (1705-1709,
// 1752). Any other entry a name reaches is noted as not ported.
func (c *Calculator) minionTriggers(db, mdb *ModDB, pm, ms *activeSkill, res *Result) {
	if ms.disabled {
		return
	}
	classID := -1
	if b := db.theBuild(); b != nil && b.ActiveSpec() != nil {
		classID = b.ActiveSpec().ClassID
	}
	w1, w2 := playerWeapons(db, db.gearMods, classID)
	var cfg *triggerConfig
	for _, n := range append([]string{ms.active.id}, triggerNames(ms)...) {
		if n == "nova" || n == "snipe" {
			if pm == nil {
				continue
			}
			if cfg = c.minionTriggerConfig(n, db, mdb, pm, w1, w2); cfg != nil {
				break
			}
			continue
		}
		if triggerConfigs[n] != nil || n == "cast while channelling" {
			res.Incomplete = append(res.Incomplete, "minion skill trigger "+n+" is not ported")
			return
		}
	}
	if cfg == nil {
		ms.data.del("triggered")
		return
	}
	// config.triggerChance = config.triggerChance or srcInstance.triggerChance.
	if !cfg.hasTriggerChance && ms.active.srcHasTriggerChance {
		cfg.triggerChance, cfg.hasTriggerChance = ms.active.srcTriggerChance, true
	}
	c.defaultTrigger(db, ms, triggeredBy(ms), cfg, w1, w2)
}
