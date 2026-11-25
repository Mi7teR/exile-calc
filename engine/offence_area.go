package engine

import (
	"math"
	"strings"
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
// skill type stats: ForkCountMax and PierceCount (100 means all targets).
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
	}
	if list.FlagCfg(cfg, "CannotPierce") {
		out["PierceCount"] = 0
		return
	}
	if list.FlagCfg(cfg, "PierceAllTargets") || actor.enemy.Flag("AlwaysPierceSelf") {
		out["PierceCount"] = 100
		return
	}
	out["PierceCount"] = list.SumCfg(cfg, "BASE", "PierceCount")
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
	_, hasCD := data["cooldown"]
	_, hasTrapCD := data["trapCooldown"]
	if (s.flags["trap"] || s.flags["mine"]) && !hasCD && !hasTrapCD {
		data["showAverage"] = false
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
		data["timeOverride"] = out["TrapThrowingTime"] / count
		baseCD, ok := num(data["trapCooldown"])
		if !ok {
			baseCD, ok = num(data["cooldown"])
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
			data["timeOverride"] = out["MineLayingTime"] / count / out["TrapThrowCount"]
		} else {
			data["timeOverride"] = out["MineLayingTime"] / count
		}
	}
}

// offenceDuration is the "Skill duration" part of calcs.offence: the primary
// duration (output.Duration), rounded up to a server tick.
func offenceDuration(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, cfg, data := s.mods, s.cfg, s.data
	names := []string{"Duration", "PrimaryDuration"}
	if truthy(data["mineDurationAppliesToSkill"]) {
		names = append(names, "MineDuration")
	}
	out["DurationMod"] = math.Max(calcMod(list, cfg, names...), 0)
	base := numOr(data["duration"], 0) + list.SumCfg(cfg, "BASE", "Duration", "PrimaryDuration")
	if base <= 0 {
		return
	}
	d := base * out["DurationMod"]
	if truthy(data["debuff"]) {
		d /= math.Max(0.25, calcMod(actor.enemy, cfg, "BuffExpireFaster")) // BuffExpirationSlowCap
	}
	out["Duration"] = math.Ceil(d*serverTickRate) / serverTickRate
}

// impaleDuration is ImpaleDurationMod and ImpaleDuration of the "Skill
// duration" part of calcs.offence.
func impaleDuration(actor *ModDB, s *activeSkill, out map[string]float64) {
	const impaledDebuffBaseDuration = 8 // characterConstants impaled_debuff_base_duration_ms / 1000
	out["ImpaleDurationMod"] = math.Max(calcMod(s.mods, s.cfg, "ImpaleDuration"), 0)
	debuffDurationMult := 1 / math.Max(buffExpirationSlowCap, calcMod(actor.enemy, s.cfg, "BuffExpireFaster"))
	out["ImpaleDuration"] = math.Ceil(impaledDebuffBaseDuration*out["ImpaleDurationMod"]*serverTickRate*debuffDurationMult) / serverTickRate
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
		src := explodeSource(s, entries)
		for _, e := range entries {
			if e.Mod == nil || e.Mod.Source != src {
				continue
			}
			t, amount, c := explodeVal(e)
			if t == "RandomElement" {
				for _, el := range []string{"Fire", "Cold", "Lightning"} {
					data[el+"EffectiveExplodePercentage"] = amount / 3
				}
			} else {
				data[t+"EffectiveExplodePercentage"] = amount
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
					data[el+"EffectiveExplodePercentage"] = numOr(data[el+"EffectiveExplodePercentage"], 0) + ac/3
				}
			} else {
				data[k.t+"EffectiveExplodePercentage"] = numOr(data[k.t+"EffectiveExplodePercentage"], 0) + ac
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

// explodeSource is activeEffect.srcInstance.explodeSource's mod source: the
// n-th of env.explodeSources (CalcSetup: allocated nodes, then items), n
// being this gem's place among the group's EnemyExplode gems.
func explodeSource(s *activeSkill, entries []ListEntry) string {
	var tree, items []string
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Mod == nil || seen[e.Mod.Source] {
			continue
		}
		seen[e.Mod.Source] = true
		if strings.HasPrefix(e.Mod.Source, "Tree:") {
			tree = append(tree, e.Mod.Source)
		} else {
			items = append(items, e.Mod.Source)
		}
	}
	sources := append(tree, items...) //nolint:gocritic // legacy, kept by pure refactor; see refactor plan part 2
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
	if n < len(sources) {
		return sources[n]
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
	if truthy(data["radiusIsWeaponRange"]) {
		r := 0.0
		if s.flags["weapon1Attack"] {
			r = math.Max(r, s.weaponRange1)
		}
		if s.flags["weapon2Attack"] {
			r = math.Max(r, s.weaponRange2)
		}
		data["radius"] = r + 2
	}
	radius, ok := num(data["radius"])
	if !ok {
		return
	}
	s.flags["area"] = true
	extra := numOr(data["radiusExtra"], 0)
	out["AreaOfEffectRadius"] = calcRadius(radius+extra+list.SumCfg(cfg, "BASE", "AreaOfEffect"), out["AreaOfEffectMod"], radiusMod)
	out["AreaOfEffectRadiusMetres"] = out["AreaOfEffectRadius"] / 10
	secondary, hasSecondary := num(data["radiusSecondary"])
	if hasSecondary {
		i, m := mods("AreaOfEffect", "AreaOfEffectSecondary")
		out["AreaOfEffectModSecondary"] = calcAreaMod(i, m)
		out["AreaOfEffectRadiusSecondary"] = calcRadius(secondary+extra, out["AreaOfEffectModSecondary"], radiusMod)
		out["AreaOfEffectRadiusSecondaryMetres"] = out["AreaOfEffectRadiusSecondary"] / 10
	}
	tertiary, ok := num(data["radiusTertiary"])
	if !ok {
		return
	}
	i, m := mods("AreaOfEffect", "AreaOfEffectTertiary")
	areaMod := calcAreaMod(i, m)
	out["AreaOfEffectModTertiary"] = areaMod
	base := tertiary + extra
	switch {
	case truthy(data["projectileSpeedAppliesToMSAreaOfEffect"]):
		si, sm := mods("ProjectileSpeed")
		speedMod := round(round(si*sm, 10), 2)
		out["SpeedModTertiary"] = speedMod
		// calcMoltenStrikeTertiaryRadius: the deadzone is the secondary radius.
		maxDist := math.Sqrt(base*base*areaMod - secondary*secondary*(areaMod-1))
		out["AreaOfEffectRadiusTertiary"] = math.Floor((maxDist-secondary)*speedMod + secondary)
	case data["radiusTertiaryBaseMargin"] != nil:
		marginPct := numOr(data["radiusTertiaryBaseMargin"], 0)
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
