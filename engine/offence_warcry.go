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

// warcries is the exerted attack part of calcs.offence's damage pass
// (mode_buffs): each warcry's exert uptime, the offensive warcry effect
// (Rallying Cry), Intimidating Cry's double damage and the exert damage
// mods scaled by uptime, and Seismic Cry's area. Ancestral Cry's extra
// strikes are not ported.
func warcries(actor *ModDB, s *activeSkill, cfg *Cfg, out map[string]float64) {
	out["OffensiveWarcryEffect"], out["MaxOffensiveWarcryEffect"], out["AilmentWarcryEffect"] = 1, 1, 1
	out["GlobalWarcryUptimeRatio"] = 0
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
		cd, _ := skillCooldown(v.mods, v.cfg, &v.data)
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
		r := uptime(v, player.Sum("BASE", exerts))
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
			dd := math.Min(list.SumCfg(cfg, "BASE", "DoubleDamageChance")+actor.enemy.SumCfg(cfg, "BASE", "SelfDoubleDamageChance")+exertDD, 100)
			out["IntimidatingAvgDmg"] = 2 * (1 - dd/100)
		case "Rallying":
			avg := math.Min(player.SumCfg(cfg, "BASE", "Multiplier:NearbyAlly"), 5) * player.Sum("BASE", "RallyingExertMoreDamagePerAlly") / 100
			out["OffensiveWarcryEffect"] *= 1 + avg*r/100
			out["MaxOffensiveWarcryEffect"] *= 1 + avg
		}
	}
	exerted := 0.0
	for _, r := range ratios {
		exerted = math.Max(exerted, r)
	}
	out["ExertedAttackUptimeRatio"] = exerted
	out["AilmentWarcryEffect"] = out["OffensiveWarcryEffect"]
	if maxHit {
		out["AilmentWarcryEffect"] = out["MaxOffensiveWarcryEffect"]
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
}

// ruthlessAndFistOfWar is the Ruthless and Fist of War part of the damage
// pass (mode_combat): the hit effects, with ruthlessSupportMode AVERAGE (a
// 1 in N chance) or MAX (every hit, at 1/N of the rate).
func ruthlessAndFistOfWar(s *activeSkill, p *hitPass, global map[string]float64) {
	list, cfg, out := s.mods, p.cfg, p.out
	out["RuthlessBlowHitEffect"], out["FistOfWarDamageEffect"] = 1, 1
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
	out["RuthlessBlowHitEffect"] = 1 - chance/100 + chance/100*hitMult
	ailmentMult := 1 + list.SumCfg(cfg, "BASE", "RuthlessBlowAilmentMultiplier")/100
	out["RuthlessBlowAilmentEffect"] = 1 - chance/100 + chance/100*ailmentMult
	cd := list.SumCfg(cfg, "BASE", "FistOfWarCooldown")
	if cd != 0 && s.types[stSlam] && !s.types[stVaal] && !s.types[stOtherThingUsesSkill] && global["Speed"] != 0 {
		mult := list.Sum("BASE", "FistOfWarDamageMultiplier") / 100
		up := math.Min((1/global["Speed"])/cd, 1) * 100
		incAoE := list.Sum("BASE", "FistOfWarIncAoE")
		if list.Flag("Condition:WarcryMaxHit") {
			out["FistOfWarDamageEffect"] = 1 + mult
			list.NewMod("AreaOfEffect", "INC", incAoE, "Max Fist of War Boosted AoE")
		} else {
			out["FistOfWarDamageEffect"] = 1 + mult*up/100
			list.NewMod("AreaOfEffect", "INC", math.Floor(incAoE/100*up), "Avg Fist Of War Boosted AoE")
		}
		areaOfEffect(s, global)
	}
}
