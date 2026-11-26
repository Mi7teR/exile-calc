package engine

import "math"

// warcryCastTime is calcWarcryCastTime (CalcOffence.lua).
func warcryCastTime(actor *ModDB, v *activeSkill) float64 {
	if v.mods.FlagCfg(v.cfg, "InstantWarcry") || truthy(v.data.val("triggeredByAutoexertion")) {
		return 0
	}
	base := v.mods.SumCfg(v.cfg, "BASE", "WarcryCastTime")
	if base == 0 {
		return 0
	}
	speed := 1 / base * calcMod(v.mods, v.cfg, "WarcrySpeed") * actionSpeedMod(actor)
	return 1 / math.Min(speed, serverTickRate)
}

// cryOutputKey names the cry outputs set whatever the skill
// (CalcOffence.lua:2682-2684, 2831-2833); the other cries set theirs only
// when they apply to the skill.
var cryOutputKey = map[string]string{"Infernal Cry": "InfernalCry", "Battlemage's Cry": "BattleMageCry"}

// setCryOutputs sets the cry's duration, cooldown and cast time outputs
// (CalcOffence.lua:2659-2661 and the other cries' branches).
func setCryOutputs(actor *ModDB, v *activeSkill, out map[string]float64, cry string) {
	cd, _ := skillCooldown(v.mods, v.cfg, v.data)
	out[cry+"Duration"] = skillDuration(v.mods, v.cfg, v.data, actor.enemy)
	out[cry+"Cooldown"] = cd
	out[cry+"CastTime"] = warcryCastTime(actor, v)
}

// warcries is the exerted attack part of calcs.offence's damage pass
// (mode_buffs): each warcry's exert uptime, the offensive warcry effect
// (Rallying Cry), Intimidating Cry's double damage and the exert damage
// mods scaled by uptime, and Seismic Cry's area. Ancestral Cry's extra
// strikes are not ported.
func warcries(actor *ModDB, s *activeSkill, cfg *Cfg, out map[string]float64) {
	out["OffensiveWarcryEffect"], out["MaxOffensiveWarcryEffect"], out["AilmentWarcryEffect"] = 1, 1, 1
	out["TheoreticalOffensiveWarcryEffect"], out["TheoreticalMaxOffensiveWarcryEffect"] = 1, 1
	out["RallyingHitEffect"] = 1
	out["GlobalWarcryUptimeRatio"] = 0
	if !modeBuffs(actor) { // CalcOffence.lua:2653
		return
	}
	for _, t := range []int{stNeverExertable, stTriggered, stChannel, stOtherThingUsesSkill, stRetaliation, stSummonsTotem} {
		if s.types[t] {
			return
		}
	}
	list := s.mods
	player := actor
	if actor.owner != nil {
		player = actor.owner
	}
	speed := out["Speed"]
	exertDD := player.SumCfg(cfg, "BASE", "ExertDoubleDamageChance")
	maxHit := list.Flag("Condition:WarcryMaxHit")
	ratios := map[string]float64{}
	uptime := func(v *activeSkill, count float64) float64 {
		cd, _ := skillCooldown(v.mods, v.cfg, v.data)
		r := 100.0
		if speed != 0 {
			r = math.Min((count/speed)/(cd+warcryCastTime(actor, v)), 1) * 100
		}
		stored := v.data.storedUses.or(0) + v.mods.SumCfg(v.cfg, "BASE", "AdditionalCooldownUses")
		return math.Min(100, r*stored)
	}
	done := map[string]bool{}
	for _, v := range actor.activeSkills {
		name := v.active.ge.Name
		if v.disabled || done[name] {
			continue
		}
		var key, exerts string
		// The Infernal and Battlemage's Cry outputs, which the Combust and
		// Battlemage's Cry trigger rates read (CalcTriggers.lua:503-540).
		if cry := cryOutputKey[name]; cry != "" {
			done[name] = true
			setCryOutputs(actor, v, out, cry)
			out[cry+"Calculated"] = 1
		}
		switch name {
		case "Ancestral Cry":
			if s.types[stMeleeSingleTarget] {
				key, exerts = "Ancestral", "NumAncestralExerts"
			}
		case "Infernal Cry":
			if s.types[stMelee] {
				key, exerts = "Infernal", "NumInfernalExerts"
			}
		case "Intimidating Cry":
			if s.types[stMelee] {
				key, exerts = "Intimidating", "NumIntimidatingExerts"
			}
		case "Rallying Cry":
			if s.types[stMelee] {
				key, exerts = "Rallying", "NumRallyingExerts"
			}
		case "Seismic Cry":
			if s.types[stSlam] {
				key, exerts = "Seismic", "NumSeismicExerts"
			}
		case "Battlemage's Cry":
			if s.types[stMelee] {
				key, exerts = "Battlemage", "NumBattlemageExerts"
			}
		}
		if key == "" {
			continue
		}
		done[name] = true
		if cryOutputKey[name] == "" {
			setCryOutputs(actor, v, out, key+"Cry")
			out[key+"CryCalculated"] = 1
		}
		if key == "Intimidating" || key == "Rallying" || key == "Seismic" {
			out["CreateWarcryOffensiveCalcSection"] = 1
		}
		count := player.Sum("BASE", exerts)
		// BattleCryExertsCount is Battlemage's Cry's (CalcOffence.lua:2835).
		if key == "Battlemage" {
			out["BattleCryExertsCount"] = count
		} else {
			out[key+"ExertsCount"] = count
		}
		r := uptime(v, count)
		ratios[key] = r
		out[key+"UpTimeRatio"] = r
		out["GlobalWarcryUptimeRatio"] += r
		switch key {
		case "Seismic":
			// Account for the exert's AoE increase, then recalculate the area.
			if maxHit {
				list.NewMod("AreaOfEffect", "MORE", player.Sum("BASE", "SeismicMoreAoE"), "Max Seismic Exert AoE")
			} else {
				list.NewMod("AreaOfEffect", "MORE", math.Floor(player.Sum("BASE", "SeismicMoreAoE")/100*r), "Avg Seismic Exert AoE")
			}
			areaOfEffect(s, out)
		case "Intimidating":
			selfDD := 0.0
			if modeEffective(actor) { // CalcOffence.lua:2729
				selfDD = actor.enemy.SumCfg(cfg, "BASE", "SelfDoubleDamageChance")
			}
			dd := math.Min(list.SumCfg(cfg, "BASE", "DoubleDamageChance")+selfDD+exertDD, 100)
			avg := 2 * (1 - dd/100)
			out["IntimidatingAvgDmg"] = avg
			// CalcOffence.lua:2737-2747.
			out["IntimidatingHitEffect"] = 1 + float64(avg*r)/100
			out["IntimidatingMaxHitEffect"] = 1 + avg
			out["TheoreticalOffensiveWarcryEffect"] *= out["IntimidatingHitEffect"]
			out["TheoreticalMaxOffensiveWarcryEffect"] *= out["IntimidatingMaxHitEffect"]
		case "Rallying":
			avg := float64(math.Min(player.SumCfg(cfg, "BASE", "Multiplier:NearbyAlly"), 5) * (player.Sum("BASE", "RallyingExertMoreDamagePerAlly") / 100))
			// CalcOffence.lua:2775-2794.
			out["RallyingAvgDmg"] = avg
			out["RallyingHitEffect"] = 1 + float64(avg*r)/100
			out["RallyingMaxHitEffect"] = 1 + avg
			out["OffensiveWarcryEffect"] *= out["RallyingHitEffect"]
			out["MaxOffensiveWarcryEffect"] *= out["RallyingMaxHitEffect"]
			out["TheoreticalOffensiveWarcryEffect"] *= out["RallyingHitEffect"]
			out["TheoreticalMaxOffensiveWarcryEffect"] *= out["RallyingMaxHitEffect"]
		}
	}
	exerted := 0.0
	for _, r := range ratios {
		exerted = math.Max(exerted, r)
	}
	out["ExertedAttackUptimeRatio"] = exerted
	out["AilmentWarcryEffect"] = out["OffensiveWarcryEffect"]
	if maxHit {
		// CalcOffence.lua:2858-2862: the max hit shows the average hit.
		out["AilmentWarcryEffect"] = out["MaxOffensiveWarcryEffect"]
		s.data.set("showAverage", true)
		s.flags["showAverage"] = true
	}
	if exerted <= 0 {
		return
	}
	inc := list.SumCfg(cfg, "INC", "ExertIncrease")
	more := list.SumCfg(cfg, "MORE", "ExertIncrease")
	moreAttack := list.SumCfg(cfg, "MORE", "ExertAttackIncrease")
	overexertion := list.SumCfg(cfg, "MORE", "OverexertionExertAverageIncrease")
	echoes := list.SumCfg(cfg, "MORE", "EchoesExertAverageIncrease")
	if maxHit {
		count := player.Sum("BASE", "Multiplier:ExertingWarcryCount")
		list.NewMod("Damage", "INC", inc, "Exerted Attacks")
		list.NewMod("Damage", "MORE", more, "Exerted Attacks")
		list.Add(Mod{Name: "Damage", Type: "MORE", Value: moreAttack, Flags: mfAttack, Source: "Exerted Attack Damage"})
		list.NewMod("Damage", "MORE", overexertion*count, "Max Autoexertion Support")
		list.NewMod("Damage", "MORE", echoes*count, "Max Echoes of Creation")
	} else {
		global := out["GlobalWarcryUptimeRatio"]
		list.NewMod("Damage", "INC", inc*exerted/100, "Uptime Scaled Exerted Attacks")
		list.NewMod("Damage", "MORE", more*exerted/100, "Uptime Scaled Exerted Attacks")
		list.Add(Mod{Name: "Damage", Type: "MORE", Value: moreAttack * exerted / 100, Flags: mfAttack, Source: "Uptime Scaled Exerted Attack Damage"})
		list.NewMod("Damage", "MORE", overexertion*global/100, "Uptime Scaled Autoexertion Support")
		list.NewMod("Damage", "MORE", echoes*global/100, "Uptime Scaled Echoes of Creation")
	}
	// CalcOffence.lua:2903-2914.
	avg := calcMod(list, s.cfg, "ExertIncrease") * calcMod(list, s.cfg, "ExertAttackIncrease", "OverexertionExertAverageIncrease", "EchoesExertAverageIncrease")
	out["ExertedAttackAvgDmg"] = avg
	out["ExertedAttackHitEffect"] = float64(avg*exerted) / 100
	out["ExertedAttackMaxHitEffect"] = avg
	out["ExertedAttackUptimeRatioCalculated"] = 1
}

// ruthlessAndFistOfWar is the Ruthless and Fist of War part of the damage
// pass (mode_combat): the hit effects, with ruthlessSupportMode AVERAGE (a
// 1 in N chance) or MAX (every hit, at 1/N of the rate).
func ruthlessAndFistOfWar(s *activeSkill, p *hitPass, global map[string]float64) {
	list, cfg, out := s.mods, p.cfg, p.out
	out["RuthlessBlowHitEffect"], out["FistOfWarDamageEffect"] = 1, 1
	if !modeCombat(list.root()) { // CalcOffence.lua:3008
		return
	}
	mode := "AVERAGE"
	if b := list.theBuild(); b != nil {
		mode = configString(b.ActiveConfig(), "ruthlessSupportMode", "AVERAGE")
	}
	chance := 0.0
	n := list.SumCfg(cfg, "BASE", "RuthlessBlowMaxCount")
	out["RuthlessBlowMaxCount"] = n
	if n > 0 && (!cfg.SkillCond["usedByMirage"] || numOr(s.data.val("mirageUses"), 0) > n) {
		switch mode {
		case "AVERAGE":
			chance = round(100/n, 0)
		case "MAX":
			chance = 100
			s.data.dpsMultiplier = optF{s.data.dpsMultiplier.or(1) / n, true}
		}
	}
	out["RuthlessBlowChance"] = chance
	hitMult := 1 + list.SumCfg(cfg, "BASE", "RuthlessBlowHitMultiplier")/100
	out["RuthlessBlowHitEffect"] = 1 - chance/100 + float64(chance/100*hitMult)
	ailmentMult := 1 + list.SumCfg(cfg, "BASE", "RuthlessBlowAilmentMultiplier")/100
	out["RuthlessBlowAilmentEffect"] = 1 - chance/100 + float64(chance/100*ailmentMult)
	out["RuthlessBlowHitMultiplier"], out["RuthlessBlowAilmentMultiplier"] = hitMult, ailmentMult // CalcOffence.lua:3022-3023
	cd := list.SumCfg(cfg, "BASE", "FistOfWarCooldown")
	global["FistOfWarCooldown"] = cd // CalcOffence.lua:3027
	if cd != 0 && s.types[stSlam] && !s.types[stVaal] && !s.types[stOtherThingUsesSkill] && global["Speed"] != 0 {
		mult := list.Sum("BASE", "FistOfWarDamageMultiplier") / 100
		up := math.Min((1/global["Speed"])/cd, 1) * 100
		// CalcOffence.lua:3030-3048.
		global["FistOfWarDamageMultiplier"], global["FistOfWarUptimeRatio"] = mult, up
		global["AvgFistOfWarDamage"], global["AvgFistOfWarDamageEffect"] = mult, 1+float64(mult*(up/100))
		global["MaxFistOfWarDamageEffect"] = 1 + mult
		incAoE := list.Sum("BASE", "FistOfWarIncAoE")
		if list.Flag("Condition:WarcryMaxHit") {
			out["FistOfWarDamageEffect"] = global["MaxFistOfWarDamageEffect"]
			list.NewMod("AreaOfEffect", "INC", incAoE, "Max Fist of War Boosted AoE")
		} else {
			out["FistOfWarDamageEffect"] = global["AvgFistOfWarDamageEffect"]
			list.NewMod("AreaOfEffect", "INC", math.Floor(incAoE/100*up), "Avg Fist Of War Boosted AoE")
		}
		areaOfEffect(s, global)
		// CalcOffence.lua:3057-3058.
		global["TheoreticalOffensiveWarcryEffect"] *= global["AvgFistOfWarDamageEffect"]
		global["TheoreticalMaxOffensiveWarcryEffect"] *= global["MaxFistOfWarDamageEffect"]
	}
}
