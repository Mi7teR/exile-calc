package engine

import (
	"math"
)

// projectileStats is the projectile part of calcs.offence's skill type
// stats: Point Blank, Far Shot and ProjectileCount.
func projectileStats(s *activeSkill, out map[string]float64) {
	if !s.flags["projectile"] {
		return
	}
	list, cfg := s.mods, s.cfg
	if list.Flag("PointBlank") {
		list.Add(Mod{Name: "Damage", Type: "MORE", Value: 30.0, Flags: mfAttack | mfProjectile, Source: "Point Blank",
			Tags: []map[string]any{{"type": "DistanceRamp", "ramp": []any{[]any{10.0, 1.0}, []any{35.0, 0.0}, []any{120.0, -1.0}}}}})
	}
	if list.Flag("FarShot") {
		list.Add(Mod{Name: "Damage", Type: "MORE", Value: 100.0, Flags: mfAttack | mfProjectile, Source: "Far Shot",
			Tags: []map[string]any{{"type": "DistanceRamp", "ramp": []any{[]any{10.0, -0.2}, []any{25.0, 0.0}, []any{70.0, 0.6}}}}})
	}
	out["ProjectileSpeedMod"] = calcMod(list, cfg, "ProjectileSpeed")
	defer splitStats(s, out)
	if list.FlagCfg(cfg, "NoAdditionalProjectiles") || list.FlagCfg(cfg, "SingleProjectile") {
		out["ProjectileCount"] = 1
		return
	}
	projMin := list.SumCfg(cfg, "BASE", "ProjectileCountMinimum")
	projMax, ok := list.OverrideCfg(cfg, "ProjectileCountMaximum")
	if !ok {
		projMax = math.Inf(1)
	}
	proj := math.Floor(list.SumCfg(cfg, "BASE", "ProjectileCount") * list.MoreCfg(cfg, "ProjectileCount"))
	out["ProjectileCount"] = math.Max(math.Min(proj, projMax), projMin)
}

// pierceForkStats is the fork and pierce part of calcs.offence's projectile
// skill type stats: ForkCountMax, PierceCount (100 means all targets) and
// PiercedCount.
// Skills that cannot fork or pierce leave the output at 0.
func pierceForkStats(actor *ModDB, s *activeSkill, out map[string]float64) {
	if !s.flags["projectile"] {
		return
	}
	list, cfg := s.mods, s.cfg
	if !list.FlagCfg(cfg, "CannotFork") && list.FlagCfg(cfg, "ForkOnce") {
		limit := 1.0
		if list.FlagCfg(cfg, "ForkTwice") {
			limit = 2
		}
		out["ForkCountMax"] = math.Min(list.SumCfg(cfg, "BASE", "ForkCountMax"), limit)
		// CalcOffence.lua:1130-1132 (ForkCountString is the same number).
		out["ForkedCount"] = math.Min(out["ForkCountMax"], list.SumCfg(cfg, "BASE", "ForkedCount"))
		out["ForkCountString"] = out["ForkCountMax"]
		out["ForkRemaining"] = math.Max(0, out["ForkCountMax"]-out["ForkedCount"])
	}
	if list.FlagCfg(cfg, "CannotPierce") {
		out["PierceCount"] = 0
		return
	}
	pierce := list.SumCfg(cfg, "BASE", "PierceCount")
	allTargets := list.FlagCfg(cfg, "PierceAllTargets") || actor.enemy.Flag("AlwaysPierceSelf")
	if allTargets {
		pierce = 100
	} else {
		out["PierceCountString"] = pierce // "All targets" otherwise
	}
	out["PierceCount"] = pierce
	if pierce > 0 {
		s.flags["piercing"] = true
	}
	// CalcOffence.lua:1150: the times the skill has pierced (config
	// skillPierceCount), for StatThreshold tags on PiercedCount.
	pierced := math.Min(pierce, list.SumCfg(cfg, "BASE", "PiercedCount"))
	out["PiercedCount"], actor.output["PiercedCount"] = pierced, pierced
}

// chainStats is the chaining part of calcs.offence's skill type stats:
// ChainMax and Chain (actor output too, for StatThreshold tags).
func chainStats(actor *ModDB, s *activeSkill, out map[string]float64) {
	if !s.flags["chaining"] {
		return
	}
	list, cfg := s.mods, s.cfg
	if list.FlagCfg(cfg, "CannotChain") || list.FlagCfg(cfg, "NoAdditionalChains") {
		return
	}
	names := []string{"ChainCountMax"}
	if !s.flags["projectile"] {
		names = append(names, "BeamChainCountMax")
	}
	chainMax := list.SumCfg(cfg, "BASE", names...)
	if list.FlagCfg(cfg, "AdditionalProjectilesAddChainsInstead") && !list.FlagCfg(cfg, "SingleProjectile") {
		chainMax += math.Floor((list.SumCfg(cfg, "BASE", "ProjectileCount") - 1) * list.MoreCfg(cfg, "ProjectileCount"))
	}
	chainMax *= list.MoreCfg(cfg, "ChainCountMax")
	chain := math.Min(chainMax, list.SumCfg(cfg, "BASE", "ChainCount"))
	for _, o := range []map[string]float64{out, actor.output} {
		o["ChainMax"], o["Chain"], o["ChainRemaining"] = chainMax, chain, math.Max(0, chainMax-chain)
	}
	out["ChainMaxString"] = chainMax // CalcOffence.lua:1076
}

// splitStats is CalcOffence.lua:1103-1119: SplitCount and, when it is a
// number, SplitCountString ("Cannot split" is a string PoB shows).
func splitStats(s *activeSkill, out map[string]float64) {
	list, cfg, enemy := s.mods, s.cfg, s.mods.enemy
	if list.FlagCfg(cfg, "CannotSplit") || s.types[stProjectileNumber] {
		return
	}
	split := list.SumCfg(cfg, "BASE", "SplitCount") + enemy.SumCfg(cfg, "BASE", "SelfSplitCount")
	if list.FlagCfg(cfg, "AdditionalProjectilesAddSplitsInstead") && !list.FlagCfg(cfg, "SingleProjectile") {
		split += math.Floor((list.SumCfg(cfg, "BASE", "ProjectileCount") - 1) * list.MoreCfg(cfg, "ProjectileCount"))
	}
	if list.FlagCfg(cfg, "AdditionalChainsAddSplitsInstead") {
		split += list.SumCfg(cfg, "BASE", "ChainCountMax")
	}
	out["SplitCount"], out["SplitCountString"] = split, split
}

// valCfg is calcLib.val with a cfg: BASE * (1 + INC) * MORE.
func valCfg(db *ModDB, cfg *Cfg, name string) float64 {
	base := db.SumCfg(cfg, "BASE", name)
	if base == 0 {
		return 0
	}
	return base * calcMod(db, cfg, name)
}

// trapMineStats is the trap and mine part of calcs.offence's skill type
// stats: throwing and laying rates become skillData.timeOverride.
func trapMineStats(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, cfg, data := s.mods, s.cfg, s.data
	_, hasCD := data.get("cooldown")
	_, hasTrapCD := data.get("trapCooldown")
	if (s.flags["trap"] || s.flags["mine"]) && !hasCD && !hasTrapCD {
		data.set("showAverage", false)
	}
	action := actor.output["ActionSpeedMod"]
	if s.flags["trap"] {
		base := 1 / list.SumCfg(cfg, "BASE", "TrapThrowingTime")
		if m := calcMod(list, cfg, "SkillTrapThrowingTime"); m > 0 {
			base /= m
		}
		speed := base * calcMod(list, cfg, "TrapThrowingSpeed") * action
		count := valCfg(list, cfg, "TrapThrowCount")
		if hasCD || hasTrapCD {
			count = 1
		}
		if o, ok := actor.Override("TrapThrowCount"); ok {
			count = o
		}
		out["TrapThrowCount"] = count
		out["TrapThrowingSpeed"] = math.Min(speed, serverTickRate)
		out["TrapThrowingTime"] = 1 / out["TrapThrowingSpeed"]
		data.timeOverride = optF{out["TrapThrowingTime"] / count, true}
		baseCD, ok := num(data.val("trapCooldown"))
		if !ok {
			baseCD, ok = data.cooldown.get()
		}
		if ok || list.SumCfg(cfg, "BASE", "CooldownRecovery") != 0 {
			if ok {
				cd, has := list.OverrideCfg(cfg, "CooldownRecovery")
				if !has {
					cd = baseCD / calcMod(list, cfg, "CooldownRecovery")
				}
				out["TrapCooldown"] = math.Ceil(cd*serverTickRate) / serverTickRate
			} else {
				out["TrapCooldown"], _ = skillCooldown(list, cfg, data)
			}
		}
	}
	if s.flags["mine"] {
		base := 1 / list.SumCfg(cfg, "BASE", "MineLayingTime")
		if m := calcMod(list, cfg, "SkillMineThrowingTime"); m > 0 {
			base /= m
		}
		speed := base * calcMod(list, cfg, "MineLayingSpeed") * action
		count := valCfg(list, cfg, "MineThrowCount")
		if hasCD || hasTrapCD {
			count = 1
		}
		if o, ok := actor.Override("MineThrowCount"); ok {
			count = o
		}
		out["MineThrowCount"] = count
		if count >= 1 {
			speed /= 1 + (count-1)*0.1
		}
		out["MineLayingSpeed"] = math.Min(speed, serverTickRate)
		out["MineLayingTime"] = 1 / out["MineLayingSpeed"]
		if s.flags["trap"] {
			data.timeOverride = optF{out["MineLayingTime"] / count / out["TrapThrowCount"], true}
		} else {
			data.timeOverride = optF{out["MineLayingTime"] / count, true}
		}
	}
}

// offenceDuration is the "Skill duration" part of calcs.offence: the primary
// duration (output.Duration), rounded up to a server tick.
func offenceDuration(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, cfg, data := s.mods, s.cfg, s.data
	names := []string{"Duration", "PrimaryDuration"}
	if truthy(data.val("mineDurationAppliesToSkill")) {
		names = append(names, "MineDuration")
	}
	out["DurationMod"] = math.Max(calcMod(list, cfg, names...), 0)
	base := data.duration.or(0) + list.SumCfg(cfg, "BASE", "Duration", "PrimaryDuration")
	if base <= 0 {
		return
	}
	d := base * out["DurationMod"]
	// CalcOffence.lua:1493-1496: debuffDurationMult only with
	// env.mode_effective.
	if truthy(data.val("debuff")) && modeEffective(actor) {
		d /= math.Max(0.25, calcMod(actor.enemy, cfg, "BuffExpireFaster")) // BuffExpirationSlowCap
	}
	out["Duration"] = math.Ceil(d*serverTickRate) / serverTickRate
}

// impaleDuration is ImpaleDurationMod and ImpaleDuration of the "Skill
// duration" part of calcs.offence.
func impaleDuration(actor *ModDB, s *activeSkill, out map[string]float64) {
	const impaledDebuffBaseDuration = 8 // characterConstants impaled_debuff_base_duration_ms / 1000
	out["ImpaleDurationMod"] = math.Max(calcMod(s.mods, s.cfg, "ImpaleDuration"), 0)
	out["ImpaleDuration"] = math.Ceil(impaledDebuffBaseDuration*out["ImpaleDurationMod"]*serverTickRate*debuffDurationMult(actor, s.cfg)) / serverTickRate
}

// enemyExplode is EnemyExplode's preDamageFunc (Data/Skills/other.lua): the
// explode chance and the <Type>EffectiveExplodePercentage of this skill's
// source (parts 1 and 2) or of all sources (part 3).
func enemyExplode(s *activeSkill, out map[string]float64) {
	data := s.data
	entries := s.mods.List(s.cfg, "ExplodeMod")
	explodeVal := func(e ListEntry) (string, float64, float64) {
		v, _ := e.Value.(map[string]any)
		t, _ := v["type"].(string)
		return t, numOr(v["amount"], 0), numOr(v["chance"], 0)
	}
	chance := 0.0
	if s.part != 3 {
		src := explodeSource(s)
		for _, e := range entries {
			if e.Mod == nil || e.Mod.Source != src {
				continue
			}
			t, amount, c := explodeVal(e)
			if t == "RandomElement" {
				for _, el := range []string{"Fire", "Cold", "Lightning"} {
					data.set(el+"EffectiveExplodePercentage", amount/3)
				}
			} else {
				data.set(t+"EffectiveExplodePercentage", amount)
			}
			chance = c
			if s.part == 2 {
				chance = 1
			}
		}
	} else {
		// typeAmountChances[type][amount] = summed chance (Lua pairs order
		// does not change the sums or the combined chance).
		type key struct {
			t      string
			amount float64
		}
		sums := map[key]float64{}
		var order []key
		for _, e := range entries {
			t, amount, c := explodeVal(e)
			k := key{t, amount}
			if _, ok := sums[k]; !ok {
				order = append(order, k)
			}
			sums[k] += c
		}
		phys := 0.0
		for _, k := range order {
			c := sums[k]
			ac := k.amount * c
			if k.t == "RandomElement" {
				for _, el := range []string{"Fire", "Cold", "Lightning"} {
					data.set(el+"EffectiveExplodePercentage", numOr(data.val(el+"EffectiveExplodePercentage"), 0)+ac/3)
				}
			} else {
				data.set(k.t+"EffectiveExplodePercentage", numOr(data.val(k.t+"EffectiveExplodePercentage"), 0)+ac)
			}
			if k.t == "Physical" {
				phys = 1 - (1-phys)*(1-c)
			}
			chance = 1 - (1-chance)*(1-c)
		}
		if phys != 0 {
			s.mods.NewMod("CalcArmourAsThoughDealing", "MORE", 100/math.Min(phys, 1)-100, "Skill:EnemyExplode")
		}
	}
	out["ExplodeChance"] = math.Min(chance*100, 100)
}

// explodeSource is activeEffect.srcInstance.explodeSource's mod source
// (Data/Skills/other.lua: explodeSource.modSource or "Tree:"..id): the
// n-th of env.explodeSources, n being this gem's place among the group's
// EnemyExplode gems, which CalcSetup.lua:1586-1601 creates one per source.
func explodeSource(s *activeSkill) string {
	db := s.mods
	for db != nil && db.parent != nil {
		db = db.parent
	}
	n := 0
	if s.group != nil {
		for i := range s.group.Gems {
			g := &s.group.Gems[i]
			if g == s.gem {
				break
			}
			if g.SkillID == "EnemyExplode" {
				n++
			}
		}
	}
	if db != nil && n < len(db.explodeSources) {
		return db.explodeSources[n]
	}
	return ""
}

// roundSymmetric is Common.lua roundSymmetric (no decimals).
func roundSymmetric(v float64) float64 {
	if v >= 0 {
		return math.Floor(v + 0.5)
	}
	return math.Ceil(v - 0.5)
}

// calcAreaMod is CalcOffence.lua calcAreaMod.
func calcAreaMod(inc, more float64) float64 {
	p := roundSymmetric(inc*100) * roundSymmetric(math.Max(0, more)*100) / 100
	if p >= 0 {
		p = math.Floor(p)
	} else {
		p = math.Ceil(p)
	}
	return math.Max(0, p) / 100
}

// calcRadius is CalcOffence.lua calcRadius.
func calcRadius(base, areaMod, radiusMod float64) float64 {
	pct := math.Floor(100 * math.Sqrt(areaMod))
	pct = math.Floor(pct * radiusMod)
	return math.Floor(base * pct / 100)
}

// areaOfEffect is calcs.offence's calcAreaOfEffect: AreaOfEffectMod and
// the primary, secondary and tertiary radii (Molten Strike's tertiary
// radius from projectile speed; random base radius margins; the
// weapon-range radius of radiusIsWeaponRange).
func areaOfEffect(s *activeSkill, out map[string]float64) {
	list, cfg, data := s.mods, s.cfg, s.data
	mods := func(names ...string) (float64, float64) {
		return 1 + list.SumCfg(cfg, "INC", names...)/100, list.MoreCfg(cfg, names...)
	}
	inc, more := mods("AreaOfEffect", "AreaOfEffectPrimary")
	out["AreaOfEffectMod"] = calcAreaMod(inc, more)
	radiusMod := calcMod(list, cfg, "AreaOfEffectRadius")
	if truthy(data.val("radiusIsWeaponRange")) {
		r := 0.0
		if s.flags["weapon1Attack"] {
			r = math.Max(r, s.weaponRange1)
		}
		if s.flags["weapon2Attack"] {
			r = math.Max(r, s.weaponRange2)
		}
		data.set("radius", r+2)
	}
	radius, ok := num(data.val("radius"))
	if !ok {
		return
	}
	s.flags["area"] = true
	extra := numOr(data.val("radiusExtra"), 0)
	out["AreaOfEffectRadius"] = calcRadius(radius+extra+list.SumCfg(cfg, "BASE", "AreaOfEffect"), out["AreaOfEffectMod"], radiusMod)
	out["AreaOfEffectRadiusMetres"] = out["AreaOfEffectRadius"] / 10
	secondary, hasSecondary := num(data.val("radiusSecondary"))
	if hasSecondary {
		i, m := mods("AreaOfEffect", "AreaOfEffectSecondary")
		out["AreaOfEffectModSecondary"] = calcAreaMod(i, m)
		out["AreaOfEffectRadiusSecondary"] = calcRadius(secondary+extra, out["AreaOfEffectModSecondary"], radiusMod)
		out["AreaOfEffectRadiusSecondaryMetres"] = out["AreaOfEffectRadiusSecondary"] / 10
	}
	tertiary, ok := num(data.val("radiusTertiary"))
	if !ok {
		return
	}
	i, m := mods("AreaOfEffect", "AreaOfEffectTertiary")
	areaMod := calcAreaMod(i, m)
	out["AreaOfEffectModTertiary"] = areaMod
	base := tertiary + extra
	switch {
	case truthy(data.val("projectileSpeedAppliesToMSAreaOfEffect")):
		si, sm := mods("ProjectileSpeed")
		speedMod := round(round(si*sm, 10), 2)
		out["SpeedModTertiary"] = speedMod
		// calcMoltenStrikeTertiaryRadius: the deadzone is the secondary radius.
		maxDist := math.Sqrt(base*base*areaMod - secondary*secondary*(areaMod-1))
		out["AreaOfEffectRadiusTertiary"] = math.Floor((maxDist-secondary)*speedMod + secondary)
	case data.val("radiusTertiaryBaseMargin") != nil:
		marginPct := numOr(data.val("radiusTertiaryBaseMargin"), 0)
		margin := marginPct / 100
		width := marginPct*2 + 1
		occ := map[float64]float64{}
		for d := 1 - margin; d <= 1+margin+0.01; d += 0.01 {
			occ[math.Floor(base*d)]++
		}
		sum := 0.0
		for b, n := range occ {
			sum += calcRadius(b, areaMod, radiusMod) * n
		}
		out["AreaOfEffectRadiusTertiary"] = sum / width
	default:
		out["AreaOfEffectRadiusTertiary"] = calcRadius(base, areaMod, radiusMod)
	}
	out["AreaOfEffectRadiusTertiaryMetres"] = out["AreaOfEffectRadiusTertiary"] / 10
}

// weaponRange is the melee block of calcs.offence: actor.weaponRange1/2
// (the weapon's range plus MeleeWeaponRange, or the unarmed range) and,
// for MeleeSingleTarget skills, WeaponRange and StrikeTargets. PoB keeps
// the ranges on the actor; here they live on the skill, which is the only
// reader (radiusIsWeaponRange).
func weaponRange(s *activeSkill, w1 hitSource, w2 *hitSource, out map[string]float64) {
	if !s.flags["melee"] {
		return
	}
	list, cfg := s.mods, s.cfg
	rangeOf := func(w *hitSource, wcfg *Cfg) float64 {
		if w != nil && w.hasRange {
			return w.rng + list.SumCfg(wcfg, "BASE", "MeleeWeaponRange") + 10*list.SumCfg(wcfg, "BASE", "MeleeWeaponRangeMetre")
		}
		return 6 + list.SumCfg(cfg, "BASE", "UnarmedRange") + 10*list.SumCfg(cfg, "BASE", "UnarmedRangeMetre")
	}
	if s.flags["weapon1Attack"] {
		s.weaponRange1 = rangeOf(&w1, s.weapon1Cfg)
	}
	if s.flags["weapon2Attack"] {
		s.weaponRange2 = rangeOf(w2, s.weapon2Cfg)
	}
	if s.types[stMeleeSingleTarget] {
		r := 100.0
		if s.flags["weapon1Attack"] {
			r = math.Min(r, s.weaponRange1)
		}
		if s.flags["weapon2Attack"] {
			r = math.Min(r, s.weaponRange2)
		}
		out["WeaponRange"] = r + 2
		out["WeaponRangeMetre"] = out["WeaponRange"] / 10
		out["StrikeTargets"] = 1 + list.SumCfg(cfg, "BASE", "AdditionalStrikeTarget")
	}
}
