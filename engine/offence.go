package engine

import "math"

// The hit damage part of calcs.offence (Modules/CalcOffence.lua) for one
// pass: the main hand of an attack or the skill itself. Not ported yet:
// off hand and dual wielding, triggers, totems/traps/mines, cooldowns,
// projectiles count, area, ailments, damage over time, leech, culling,
// exerts and warcry effects, Ruthless, Fist of War, PvP.

// dmgTypeList is the conversion order.
var dmgTypeList = []string{"Physical", "Lightning", "Cold", "Fire", "Chaos"}

// dmgTypeFlags are CalcOffence.lua's dmgTypeFlags.
var dmgTypeFlags = map[string]int{"Physical": 0x01, "Lightning": 0x02, "Cold": 0x04, "Fire": 0x08, "Elemental": 0x0E, "Chaos": 0x10}

// damageStatsForTypes is CalcOffence.lua damageStatsForTypes: the damage
// mod names for a set of type flags.
func damageStatsForTypes(k int) []string {
	names := []string{"Damage"}
	for _, t := range []string{"Physical", "Lightning", "Cold", "Fire", "Elemental", "Chaos"} {
		if k&dmgTypeFlags[t] != 0 {
			names = append(names, t+"Damage")
		}
	}
	return names
}

// Data/Misc.lua data.misc.
const (
	serverTickRate                          = 1 / 0.033
	enemyMaxResist                          = 75
	enemyPhysicalDamageReductionCap         = 90
	enemyResistCap                          = 90 // data.misc.MaxResistCap
	baseCritMultiplierMonster       float64 = 130
)

type convEntry struct {
	to   map[string]float64 // conversion + gain, per destination
	mult float64            // what stays
}

// conversionTable is buildConversionTable.
func conversionTable(list *ModDB, cfg *Cfg) map[string]*convEntry {
	out := map[string]*convEntry{}
	for i := 0; i < 4; i++ {
		t := dmgTypeList[i]
		global, skill, add := map[string]float64{}, map[string]float64{}, map[string]float64{}
		var globalTotal, skillTotal float64
		for _, o := range dmgTypeList[i+1:] {
			names := []string{t + "DamageConvertTo" + o}
			gainNames := []string{t + "DamageGainAs" + o}
			if isElemental[t] {
				names = append(names, "ElementalDamageConvertTo"+o)
				gainNames = append(gainNames, "ElementalDamageGainAs"+o)
			}
			names = append(names, "NonChaosDamageConvertTo"+o)
			gainNames = append(gainNames, "NonChaosDamageGainAs"+o)
			global[o] = math.Max(list.SumCfg(cfg, "BASE", names...), 0)
			globalTotal += global[o]
			skill[o] = math.Max(list.SumCfg(cfg, "BASE", "Skill"+t+"DamageConvertTo"+o), 0)
			skillTotal += skill[o]
			add[o] = math.Max(list.SumCfg(cfg, "BASE", gainNames...), 0)
		}
		if skillTotal > 100 {
			for o := range skill {
				skill[o] *= 100 / skillTotal
				global[o] = 0
			}
		} else if globalTotal+skillTotal > 100 {
			f := (100 - skillTotal) / globalTotal
			for o := range global {
				global[o] *= f
			}
		}
		e := &convEntry{to: map[string]float64{}}
		converted := 0.0
		for o := range global {
			conv := round(global[o]+skill[o], 0)
			e.to[o] = (conv + add[o]) / 100
			converted += conv
		}
		e.mult = 1 - math.Min(converted/100, 1)
		out[t] = e
	}
	out["Chaos"] = &convEntry{to: map[string]float64{}, mult: 1}
	return out
}

// hitSource is a pass's source: weaponData or skillData damage and rates.
type hitSource struct {
	min, max   map[string]float64
	attackRate float64
	critChance float64
}

type hitPass struct {
	src  hitSource
	cfg  *Cfg
	conv map[string]*convEntry
	out  map[string]float64
	base map[string][2]float64 // <Type>MinBase, <Type>MaxBase
}

// calcDamage is CalcOffence.lua calcDamage.
func calcDamage(list *ModDB, p *hitPass, t string, typeFlags int, convDst bool) (float64, float64) {
	typeFlags |= dmgTypeFlags[t]
	var addMin, addMax float64
	for _, o := range dmgTypeList {
		if o == t {
			break
		}
		if m := p.conv[o].to[t]; m > 0 {
			mn, mx := calcDamage(list, p, o, typeFlags, true)
			addMin += mn * m
			addMax += mx * m
		}
	}
	b := p.base[t]
	if b[0] == 0 && b[1] == 0 {
		if convDst {
			return addMin, addMax
		}
		return round(addMin, 0), round(addMax, 0)
	}
	names := damageStatsForTypes(typeFlags)
	inc := 1 + list.SumCfg(p.cfg, "INC", names...)/100
	more := list.MoreCfg(p.cfg, names...)
	mn := ((b[0]*inc*more)*list.MoreCfg(p.cfg, "MinDamage") + addMin) *
		list.MoreCfg(p.cfg, "Min"+t+"Damage") * (1 + list.SumCfg(p.cfg, "INC", "Min"+t+"Damage")/100)
	mx := ((b[1]*inc*more)*list.MoreCfg(p.cfg, "MaxDamage") + addMax) *
		list.MoreCfg(p.cfg, "Max"+t+"Damage") * (1 + list.SumCfg(p.cfg, "INC", "Max"+t+"Damage")/100)
	if convDst {
		return mn, mx
	}
	return round(mn, 0), round(mx, 0)
}

// calcResistForType is CalcOffence.lua calcResistForType without the
// "resist equal to yours" mods.
func calcResistForType(enemy *ModDB, cfg *Cfg, t string, configMax float64) float64 {
	if r, ok := enemy.OverrideCfg(cfg, t+"Resist"); ok {
		return math.Max(math.Min(r, enemyMaxResistFor(configMax)), resistFloor)
	}
	names := []string{t + "Resist"}
	if isElemental[t] {
		names = append(names, "ElementalResist")
	}
	r := enemy.SumCfg(cfg, "BASE", names...) * math.Max(calcMod(enemy, cfg, names...), 0)
	return math.Max(math.Min(r, enemyMaxResistFor(configMax)), resistFloor)
}

func enemyMaxResistFor(configMax float64) float64 {
	if configMax == 0 {
		return enemyMaxResist
	}
	return math.Min(math.Max(configMax, enemyMaxResist), enemyResistCap)
}

// offence computes a skill's hit DPS into out: Speed, HitChance,
// CritChance, CritMultiplier, AverageHit, AverageDamage, TotalDPS.
func (c *Calculator) offence(actor *ModDB, s *activeSkill, weapon hitSource) map[string]float64 {
	out := map[string]float64{}
	list, cfg, data := s.mods, s.cfg, s.data
	enemy := actor.enemy
	if s.disabled {
		out["CombinedDPS"] = 0
		return out
	}
	for _, e := range list.List(cfg, "SkillData") {
		skillData(data, e.Value)
	}
	isAttack := s.flags["attack"]
	p := &hitPass{cfg: cfg, out: out}
	if isAttack {
		if !s.flags["weapon1Attack"] {
			return out
		}
		p.src, p.cfg = weapon, s.weapon1Cfg
	} else {
		p.src = hitSource{min: map[string]float64{}, max: map[string]float64{}}
	}
	if cc, ok := num(data["CritChance"]); ok && !isAttack {
		p.src.critChance = cc
	}
	p.conv = conversionTable(list, p.cfg)

	// Hit chance.
	accVsEnemy := math.Max(0, math.Floor(list.SumCfg(p.cfg, "BASE", "Accuracy", "AccuracyVsEnemy")*
		(1+list.SumCfg(p.cfg, "INC", "Accuracy", "AccuracyVsEnemy")/100)*list.MoreCfg(p.cfg, "Accuracy", "AccuracyVsEnemy")))
	if !isAttack || list.FlagCfg(p.cfg, "CannotBeEvaded") || truthy(data["cannotBeEvaded"]) || enemy.Flag("CannotEvade") {
		out["AccuracyHitChance"] = 100
	} else {
		ev := math.Max(round(val(enemy, "Evasion"), 0), 0)
		out["AccuracyHitChance"] = hitChance(ev, accVsEnemy) * calcMod(list, p.cfg, "HitChance")
	}
	block := math.Max(math.Min(enemy.SumCfg(p.cfg, "BASE", "BlockChance"), 100)-list.SumCfg(p.cfg, "BASE", "reduceEnemyBlock"), 0)
	if enemy.Flag("CannotBlockAttacks") && isAttack {
		block = 0
	}
	out["HitChance"] = out["AccuracyHitChance"] * (1 - block/100)

	// Attack/cast speed.
	ge := s.active.ge
	switch {
	case ge.CastTime == 0 && data["castTimeOverride"] == nil && !truthy(data["triggered"]):
		out["Speed"] = 0
	default:
		var baseTime float64
		if isAttack {
			rate := p.src.attackRate
			if m, ok := num(data["attackSpeedMultiplier"]); ok && rate != 0 {
				rate *= 1 + m/100
			}
			if rate == 0 {
				rate = 1
			}
			baseTime = 1/rate + list.SumCfg(p.cfg, "BASE", "Speed")
			if m := calcMod(list, cfg, "SkillAttackTime"); m > 0 {
				baseTime *= m
			}
		} else {
			baseTime = ge.CastTime
			if v, ok := num(data["castTimeOverride"]); ok {
				baseTime = v
			}
		}
		inc := list.SumCfg(p.cfg, "INC", "Speed")
		more := list.MoreCfg(p.cfg, "Speed")
		out["Speed"] = 1 / (baseTime/round((1+inc/100)*more, 2) + list.SumCfg(p.cfg, "BASE", "TotalAttackTime") + list.SumCfg(p.cfg, "BASE", "TotalCastTime"))
		if s.flags["selfCast"] {
			out["Speed"] *= actor.output["ActionSpeedMod"]
		}
		if !s.types[stChannel] {
			out["Speed"] = math.Min(out["Speed"], serverTickRate)
		}
	}
	if out["Speed"] != 0 {
		out["Time"] = 1 / out["Speed"]
	}
	dpsMult := 1.0
	if v, ok := num(data["dpsMultiplier"]); ok {
		dpsMult = v
	}
	dpsMult *= (1 + list.SumCfg(cfg, "INC", "DPS")/100) * list.MoreCfg(cfg, "DPS")
	quantity := math.Max(list.SumCfg(cfg, "BASE", "QuantityMultiplier"), 1)

	// Crit.
	c.crit(list, p, enemy)

	// Double and triple damage.
	triple := math.Min(list.SumCfg(p.cfg, "BASE", "TripleDamageChance")+enemy.SumCfg(p.cfg, "BASE", "SelfTripleDamageChance")+
		math.Min(list.SumCfg(p.cfg, "BASE", "TripleDamageChanceOnCrit"), 100)*out["CritChance"]/100, 100)
	double := math.Min(list.SumCfg(p.cfg, "BASE", "DoubleDamageChance")+enemy.SumCfg(p.cfg, "BASE", "SelfDoubleDamageChance")+
		math.Min(list.SumCfg(p.cfg, "BASE", "DoubleDamageChanceOnCrit"), 100)*out["CritChance"]/100, 100)
	if triple > 0 {
		double = math.Max(double-triple*double/100, 0)
	}
	scaled := 1 + double/100 + 2*triple/100

	// Base hit damage.
	baseMult, eff := 1.0, 1.0
	if lv := ge.Levels[s.active.level]; lv != nil && lv.BaseMultiplier != 0 {
		baseMult = lv.BaseMultiplier
	} else if v, ok := num(data["baseMultiplier"]); ok {
		baseMult = v
	}
	if lv := ge.Levels[s.active.level]; lv != nil && lv.DamageEffectiveness != 0 {
		eff = lv.DamageEffectiveness
	} else if v, ok := num(data["damageEffectiveness"]); ok {
		eff = v
	}
	p.base = map[string][2]float64{}
	for _, t := range dmgTypeList {
		addedMin := list.SumCfg(p.cfg, "BASE", t+"Min") + enemy.SumCfg(p.cfg, "BASE", "Self"+t+"Min")
		addedMax := list.SumCfg(p.cfg, "BASE", t+"Max") + enemy.SumCfg(p.cfg, "BASE", "Self"+t+"Max")
		addedMult := calcMod(list, p.cfg, "Added"+t+"Damage", "AddedDamage")
		p.base[t] = [2]float64{
			p.src.min[t]*baseMult + addedMin*eff*addedMult,
			p.src.max[t]*baseMult + addedMax*eff*addedMult,
		}
	}

	// Hit damage per type: pass 1 crits, pass 2 non-crits.
	var totalHitAvg, totalCritAvg float64
	skillCond := p.cfg.SkillCond
	for pass := 1; pass <= 2; pass++ {
		cond := map[string]bool{}
		for k, v := range skillCond {
			cond[k] = v
		}
		cond["CriticalStrike"] = pass == 1
		p.cfg.SkillCond = cond
		for _, t := range dmgTypeList {
			if !s.flags["hit"] || list.FlagCfg(cfg, "DealNo"+t, "DealNoDamage") {
				continue
			}
			mn, mx := calcDamage(list, p, t, 0, false)
			allMult := p.conv[t].mult * scaled
			if pass == 1 {
				allMult *= out["CritMultiplier"]
			}
			mn, mx = mn*allMult, mx*allMult
			lucky := 0.0
			if list.FlagCfg(cfg, "LuckyHits") || (pass == 1 && list.FlagCfg(cfg, "CritLucky")) {
				lucky = 1
			} else {
				lucky = math.Min(list.SumCfg(cfg, "BASE", "LuckyHitsChance"), 100) / 100
			}
			if list.FlagCfg(cfg, "UnluckyHits") {
				lucky--
			}
			rolls := 3.0
			if list.FlagCfg(cfg, "ExtremeLuck") {
				rolls = 4
			}
			if list.FlagCfg(cfg, "Unexciting") {
				rolls, lucky = 2, 0
			}
			avg := (mn + mx) / 2
			if lucky >= 0 {
				avg = avg*(1-lucky) + (mn+(rolls-1)*mx)/rolls*lucky
			} else {
				avg = avg*(1+lucky) + ((rolls-1)*mn+mx)/rolls*(-lucky)
			}
			if mn != 0 || mx != 0 {
				avg *= c.enemyEffMult(actor, list, p.cfg, s, t, avg)
			}
			if pass == 1 {
				totalCritAvg += avg
			} else {
				totalHitAvg += avg
			}
		}
	}
	p.cfg.SkillCond = skillCond

	out["AverageHit"] = totalHitAvg*(1-out["CritChance"]/100) + totalCritAvg*out["CritChance"]/100
	out["AverageDamage"] = out["AverageHit"] * out["HitChance"] / 100
	out["TotalDPS"] = out["AverageDamage"] * out["Speed"] * dpsMult * quantity
	out["CombinedDPS"] = out["TotalDPS"]
	return out
}

// crit is the crit chance and multiplier part of calcs.offence.
func (c *Calculator) crit(list *ModDB, p *hitPass, enemy *ModDB) {
	out, cfg := p.out, p.cfg
	if list.FlagCfg(cfg, "NeverCrit") {
		out["CritChance"], out["CritMultiplier"], out["CritEffect"] = 0, 0, 1
		return
	}
	override, hasOverride := list.OverrideCfg(cfg, "CritChance")
	baseCrit := p.src.critChance
	if hasOverride {
		baseCrit = override
	}
	if hasOverride && override == 100 {
		out["CritChance"] = 100
	} else {
		var base, inc, more = 0.0, 0.0, 1.0
		if !hasOverride {
			base = list.SumCfg(cfg, "BASE", "CritChance") + enemy.Sum("BASE", "SelfCritChance")
			inc = list.SumCfg(cfg, "INC", "CritChance") + enemy.Sum("INC", "SelfCritChance")
			more = list.MoreCfg(cfg, "CritChance") * enemy.More("SelfCritChance")
		}
		cc := round((baseCrit+base)*(1+inc/100)*more*100, 0) / 100
		capv, ok := list.Override("CritChanceCap")
		if !ok {
			capv = list.SumCfg(cfg, "BASE", "CritChanceCap")
		}
		cc = math.Min(cc, capv)
		if baseCrit+base > 0 {
			cc = math.Max(cc, 0)
		}
		rolls := 0.0
		if list.FlagCfg(cfg, "CritChanceLucky") {
			rolls++
		}
		if list.FlagCfg(cfg, "ExtremeLuck") {
			rolls *= 2
		}
		if rolls != 0 {
			cc = (1 - math.Pow(1-cc/100, rolls+1)) * 100
		}
		if list.FlagCfg(cfg, "BifurcateCrit") {
			cc = (1 - math.Pow(1-cc/100, 2)) * 100
		}
		for _, x := range []struct {
			flag string
			n    float64
		}{{"Every3UseCrit", 3}, {"Every5UseCrit", 5}, {"Every6UseCrit", 6}} {
			if list.FlagCfg(cfg, x.flag) {
				cc = ((x.n-1)*cc + 100) / x.n
			}
		}
		out["CritChance"] = cc * out["AccuracyHitChance"] / 100
	}
	if list.FlagCfg(cfg, "NoCritMultiplier") {
		out["CritMultiplier"] = 1
	} else {
		extra := list.SumCfg(cfg, "BASE", "CritMultiplier") / 100
		if o, ok := list.OverrideCfg(cfg, "CritMultiplier"); ok {
			extra = (o - 100) / 100
		}
		enemyInc := 1 + enemy.Sum("INC", "SelfCritMultiplier")/100
		extra += enemy.Sum("BASE", "SelfCritMultiplier") / 100
		extra = round(extra*enemyInc, 2)
		out["CritMultiplier"] = 1 + math.Max(0, extra)
	}
	cc := out["CritChance"] / 100
	out["CritEffect"] = 1 - cc + cc*out["CritMultiplier"]
}

// enemyEffMult is the effective damage multiplier of a hit of type t on
// the enemy: damage taken, resistance and penetration, armour.
func (c *Calculator) enemyEffMult(actor, list *ModDB, cfg *Cfg, s *activeSkill, t string, avg float64) float64 {
	enemy := actor.enemy
	takenInc := enemy.SumCfg(cfg, "INC", "DamageTaken", t+"DamageTaken")
	takenMore := enemy.MoreCfg(cfg, "DamageTaken", t+"DamageTaken")
	useResChance := 1.0
	resNames := []string{"Ignore" + t + "Resistance"}
	chanceNames := []string{"ChanceToIgnore" + t + "Resistance"}
	if isElemental[t] {
		resNames = append(resNames, "IgnoreElementalResistances")
		chanceNames = append(chanceNames, "ChanceToIgnoreElementalResistances")
	}
	if list.FlagCfg(cfg, resNames...) || enemy.Flag("SelfIgnore"+t+"Resistance") {
		useResChance = 0
	} else {
		useResChance = 1 - math.Min(math.Max(list.SumCfg(cfg, "BASE", chanceNames...), 0), 100)/100
	}
	var resist, pen float64
	if t == "Physical" {
		armour := math.Max(val(enemy, "Armour"), 0)
		red := armourReductionF(armour, avg*list.MoreCfg(cfg, "CalcArmourAsThoughDealing"))
		ignore := math.Min(list.SumCfg(cfg, "BASE", "ChanceToIgnoreEnemyPhysicalDamageReduction"), 100)
		if list.FlagCfg(cfg, "IgnoreEnemyPhysicalDamageReduction") || ignore >= 100 {
			resist = 0
		} else {
			resist = math.Min(math.Max(0, enemy.Sum("BASE", "PhysicalDamageReduction")+list.SumCfg(cfg, "BASE", "EnemyPhysicalDamageReduction")+red), enemyPhysicalDamageReductionCap)
			if resist > 0 {
				resist *= 1 - (list.Sum("BASE", "PartialIgnoreEnemyPhysicalDamageReduction")/100 + ignore/100)
			}
		}
	} else {
		configMax := 0.0
		if actor.enemyEnv != nil {
			configMax, _ = actor.enemyEnv.num("enemy" + t + "Resist")
		}
		resist = calcResistForType(enemy, cfg, t, configMax)
		switch {
		case isElemental[t]:
			pen = list.SumCfg(cfg, "BASE", t+"Penetration", "ElementalPenetration")
			takenInc += enemy.SumCfg(cfg, "INC", "ElementalDamageTaken")
		case t == "Chaos":
			pen = list.SumCfg(cfg, "BASE", "ChaosPenetration")
		}
	}
	if s.flags["projectile"] {
		takenInc += enemy.Sum("INC", "ProjectileDamageTaken")
		if s.flags["attack"] {
			takenInc += enemy.Sum("INC", "ProjectileAttackDamageTaken")
		}
	}
	if s.flags["trap"] || s.flags["mine"] {
		takenInc += enemy.Sum("INC", "TrapMineDamageTaken")
	}
	eff := (1 + takenInc/100) * takenMore
	if useResChance > 0 {
		eff *= 1 - (resist-pen)*useResChance/100
	}
	return eff
}
