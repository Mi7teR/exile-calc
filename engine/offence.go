package engine

import (
	"math"
	"strings"

	"github.com/Mi7teR/poe-lib/modparser"
)

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
	enemyPhysicalDamageReductionCap         = 75 // monsterConstants maximum_physical_damage_reduction_%
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
// CritChance, CritMultiplier, AverageHit, AverageDamage, TotalDPS. An
// attack runs a pass per weapon (w2 nil: no off hand pass), combined as
// calcs.offence's combineStat does.
func (c *Calculator) offence(actor *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource) map[string]float64 {
	out := map[string]float64{}
	list, cfg, data := s.mods, s.cfg, s.data
	if s.disabled {
		out["CombinedDPS"] = 0
		return out
	}
	for _, e := range list.List(cfg, "SkillData") {
		skillData(data, e.Value)
	}
	// Additional stat bonuses.
	if list.Flag("IronGrip") {
		list.Add(Mod{Name: "PhysicalDamage", Type: "INC", Value: actor.strDmgBonus, Flags: mfAttack | mfProjectile, Source: "Strength"})
	}
	if list.Flag("IronWill") {
		list.Add(Mod{Name: "Damage", Type: "INC", Value: actor.strDmgBonus, Flags: mfSpell, Source: "Strength"})
	}
	const transfiguration = 0.3 // data.misc.Transfiguration
	for _, x := range []struct {
		flag, stat, source string
		flags              uint64
	}{
		{"TransfigurationOfBody", "Life", "Transfiguration of Body", mfAttack},
		{"TransfigurationOfMind", "Mana", "Transfiguration of Mind", 0},
		{"TransfigurationOfSoul", "EnergyShield", "Transfiguration of Soul", mfSpell},
	} {
		if list.Flag(x.flag) {
			list.Add(Mod{Name: "Damage", Type: "INC", Value: math.Floor(list.Sum("INC", x.stat) * transfiguration), Flags: x.flags, Source: x.source})
		}
	}
	c.offenceStatConversions(actor, s, w1, w2, out)
	costs := map[string]float64{}
	skillCosts(actor, s, costs)
	for k, v := range costs {
		actor.output[k] = v
		out[k] = v
	}
	// Sacrifice Support: added chaos damage from unreserved life.
	if pct, ok := num(data["sacrificeLifePercent"]); ok {
		life := actor.output["LifeUnreserved"]
		if s.flags["totem"] {
			life = c.totemLife(s)
		}
		asChaos, _ := num(data["sacrificeLifeAsChaos"])
		add := math.Floor(life * pct / 100 * (asChaos / 100))
		list.Add(Mod{Name: "ChaosMin", Type: "BASE", Value: add, Source: "Sacrifice Support"})
		list.Add(Mod{Name: "ChaosMax", Type: "BASE", Value: add, Source: "Sacrifice Support"})
	}
	preDamageFunc(actor, s)
	// Corpse and enemy explosions: base damage from the monster's life.
	monsterLife := 100.0
	if v, ok := num(data["corpseLife"]); ok {
		monsterLife = v
	} else if actor.enemyEnv != nil && actor.enemyEnv.level > 0 && actor.enemyEnv.level <= len(c.data.Monster.Life) {
		lv := actor.enemyEnv.level
		monsterLife = c.data.Monster.Life[lv-1]
	}
	if truthy(data["explodeCorpse"]) {
		t := "Fire"
		if v, ok := data["corpseExplosionDamageType"].(string); ok {
			t = v
		}
		m, ok := num(data["corpseExplosionLifeMultiplier"])
		if !ok {
			m, _ = num(data["selfFireExplosionLifeMultiplier"])
		}
		data[t+"BonusMin"], data[t+"BonusMax"] = monsterLife*m, monsterLife*m
	}
	if s.flags["monsterExplode"] {
		for _, t := range dmgTypeList {
			p, _ := num(data[t+"EffectiveExplodePercentage"])
			data[t+"Min"], data[t+"Max"] = p*monsterLife/100, p*monsterLife/100
		}
	}
	isAttack := s.flags["attack"]
	var passes []*hitPass
	if isAttack {
		if s.flags["weapon1Attack"] {
			passes = append(passes, &hitPass{src: w1, cfg: s.weapon1Cfg, out: map[string]float64{}})
		}
		if s.flags["weapon2Attack"] && w2 != nil {
			src := *w2
			if cc, ok := num(data["CritChance"]); ok {
				src.critChance = cc
			}
			if at, ok := num(data["attackTime"]); ok && at != 0 {
				src.attackRate = 1000 / at
			}
			passes = append(passes, &hitPass{src: src, cfg: s.weapon2Cfg, out: map[string]float64{}})
		}
		if len(passes) == 0 {
			return out
		}
	} else {
		// The skill pass's source is skillData: spell base damage
		// (skill("FireMin")) and crit chance.
		src := hitSource{min: map[string]float64{}, max: map[string]float64{}}
		for _, t := range dmgTypeList {
			mn, _ := num(data[t+"Min"])
			mx, _ := num(data[t+"Max"])
			bmn, _ := num(data[t+"BonusMin"])
			bmx, _ := num(data[t+"BonusMax"])
			src.min[t], src.max[t] = mn+bmn, mx+bmx
		}
		if cc, ok := num(data["CritChance"]); ok {
			src.critChance = cc
		}
		passes = append(passes, &hitPass{src: src, cfg: cfg, out: out})
	}
	both := len(passes) == 2
	// combineStat: AVERAGE, HARMONICMEAN and DPS modes.
	combine := func(stat, mode string) {
		if !isAttack {
			return
		}
		if !both {
			out[stat] = passes[0].out[stat]
			return
		}
		mh, oh := passes[0].out[stat], passes[1].out[stat]
		switch mode {
		case "AVERAGE":
			out[stat] = (mh + oh) / 2
		case "HARMONICMEAN":
			if mh == 0 || oh == 0 {
				out[stat] = 0
			} else {
				out[stat] = 2 / (1/mh + 1/oh)
			}
		case "DPS":
			out[stat] = mh + oh
			if !truthy(data["doubleHitsWhenDualWielding"]) {
				out[stat] /= 2
			}
		}
	}
	cooldown, hasCooldown := skillCooldown(list, cfg, data)
	if hasCooldown {
		out["Cooldown"] = cooldown
	}
	for _, p := range passes {
		p.conv = conversionTable(list, p.cfg)
		c.hitAndSpeed(actor, s, p)
		if hasCooldown && cooldown > 0 && p.out["Speed"] != 0 {
			p.out["Speed"] = math.Min(p.out["Speed"], 1/cooldown) // * Repeats (1)
		}
	}
	combine("AccuracyHitChance", "AVERAGE")
	combine("HitChance", "AVERAGE")
	combine("Speed", "HARMONICMEAN")
	if out["Speed"] != 0 {
		out["Time"] = 1 / out["Speed"]
	}
	dpsMult := 1.0
	if v, ok := num(data["dpsMultiplier"]); ok {
		dpsMult = v
	}
	dpsMult *= (1 + list.SumCfg(cfg, "INC", "DPS")/100) * list.MoreCfg(cfg, "DPS")
	quantity := math.Max(list.SumCfg(cfg, "BASE", "QuantityMultiplier"), 1)
	pacts(actor, s, out)
	for _, p := range passes {
		c.hitDamage(actor, s, p)
		p.out["TotalDPS"] = p.out["AverageDamage"] * out["Speed"] * dpsMult * quantity
	}
	combine("CritChance", "AVERAGE")
	combine("CritMultiplier", "AVERAGE")
	combine("AverageDamage", "DPS")
	combine("TotalDPS", "DPS")
	if isAttack {
		out["AverageHit"] = passes[0].out["AverageHit"]
		for k, v := range passes[0].out {
			if _, ok := out[k]; !ok {
				out[k] = v
			}
		}
	}
	out["CombinedDPS"] = out["TotalDPS"]
	return out
}

// skillCooldown is calcSkillCooldown, for skills with a cooldown (the
// "Calculate skill type stats" branch of calcs.offence; trap cooldowns are
// not ported).
func skillCooldown(list *ModDB, cfg *Cfg, data map[string]any) (float64, bool) {
	base, has := num(data["cooldown"])
	added := list.SumCfg(cfg, "BASE", "CooldownRecovery")
	if !has && added == 0 {
		return 0, false
	}
	if o, ok := list.OverrideCfg(cfg, "CooldownRecovery"); ok {
		return o, true
	}
	cd := (base + added) / math.Max(0, calcMod(list, cfg, "CooldownRecovery"))
	stored, _ := num(data["storedUses"])
	if stored > 1 || list.SumCfg(cfg, "BASE", "AdditionalCooldownUses") > 0 {
		return cd, true
	}
	return math.Ceil(cd*serverTickRate) / serverTickRate, true
}

// hitAndSpeed is the hit chance and attack/cast speed part of
// calcs.offence for one pass.
func (c *Calculator) hitAndSpeed(actor *ModDB, s *activeSkill, p *hitPass) {
	list, cfg, data, out := s.mods, s.cfg, s.data, p.out
	enemy := actor.enemy
	isAttack := s.flags["attack"]
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

	ge := s.active.ge
	if ge.CastTime == 0 && data["castTimeOverride"] == nil && !truthy(data["triggered"]) {
		out["Speed"] = 0
		return
	}
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

// hitDamage is the crit and hit damage part of calcs.offence for one pass:
// AverageHit and AverageDamage.
func (c *Calculator) hitDamage(actor *ModDB, s *activeSkill, p *hitPass) {
	list, cfg, data, out := s.mods, s.cfg, s.data, p.out
	enemy := actor.enemy
	ge := s.active.ge

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
		out[t+"MinBase"], out[t+"MaxBase"] = p.base[t][0], p.base[t][1]
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
				effMult := c.enemyEffMult(actor, list, p.cfg, s, t, avg)
				if pass == 2 {
					out[t+"EffMult"] = effMult
				}
				avg *= effMult
			}
			if pass == 1 {
				totalCritAvg += avg
				out[t+"CritAverage"] = avg
			} else {
				totalHitAvg += avg
				out[t+"HitAverage"] = avg
			}
		}
	}
	p.cfg.SkillCond = skillCond

	out["AverageHit"] = totalHitAvg*(1-out["CritChance"]/100) + totalCritAvg*out["CritChance"]/100
	out["AverageDamage"] = out["AverageHit"] * out["HitChance"] / 100
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

// unarmedWeaponData is data.unarmedWeaponData (Data.lua) by class id:
// attack rate 1.2, no crit, physical 2 to this.
var unarmedPhysicalMax = [...]float64{6, 8, 5, 5, 6, 6, 5}

// playerWeapons is env.player.weaponData1/2 (CalcSetup.lua): the equipped
// weapons, an unarmed main hand, and no off hand pass for a shield or
// quiver. Hollow Palm and countsAsDualWielding are not ported.
func playerWeapons(db *ModDB, gear map[string][]modparser.Mod, classID int) (hitSource, *hitSource) {
	w1, _, ok := itemWeapon(db.items["Weapon 1"], gear["Weapon 1"])
	if !ok {
		w1 = hitSource{min: map[string]float64{"Physical": 2}, max: map[string]float64{"Physical": 6}, attackRate: 1.2}
		if classID >= 0 && classID < len(unarmedPhysicalMax) {
			w1.max["Physical"] = unarmedPhysicalMax[classID]
		}
	}
	if w2, _, ok := itemWeapon(db.items["Weapon 2"], gear["Weapon 2"]); ok {
		return w1, &w2
	}
	return w1, nil
}

// preDamageFunc ports the skills' preDamageFunc (Data/Skills/*.lua) that
// set skill data before the damage passes.
func preDamageFunc(actor *ModDB, s *activeSkill) {
	data := s.data
	switch s.active.ge.Name {
	case "Bodyswap", "Bodyswap of Sacrifice":
		if s.part == 1 {
			m, _ := num(data["selfFireExplosionLifeMultiplier"])
			life := actor.output["Life"]
			if s.flags["totem"] {
				life = actor.output["TotemLife"]
			}
			data["FireBonusMin"], data["FireBonusMax"] = life*m, life*m
		}
	}
}

// pacts is the Pact part of the damage passes of calcs.offence: each
// "Pact of X" skill the actor has adds its damage scaled by its uptime to
// the spells it empowers. Pact of Beidat's extra projectiles, chains and
// cascades are not ported.
func pacts(actor *ModDB, s *activeSkill, out map[string]float64) {
	const stDamageOverTime, stCascadable, stBrand = 35, 56, 65
	if !s.types[stSpell] || s.types[stBrand] {
		return
	}
	list := s.mods
	done := map[string]bool{}
	for _, v := range actor.activeSkills {
		name := v.active.ge.Name
		key, ok := strings.CutPrefix(name, "Pact of ")
		if !ok {
			continue
		}
		if key == "K'Tash" {
			key = "Ktash"
		}
		vaal := s.types[stVaal]
		applies := false
		switch key {
		case "Beidat":
			applies = !vaal && !s.types[stChannel] && (s.flags["projectile"] || s.types[stCascadable] || s.flags["chaining"] && !s.flags["projectile"])
		case "Ghorr":
			applies = !vaal && s.types[stDamageOverTime]
		case "Ktash":
			applies = vaal && (s.types[stDamage] || s.types[stDamageOverTime])
		case "Lycia":
			applies = !vaal && s.types[stChannel] && s.flags["selfCast"]
		}
		if !applies || done[key] || v.disabled {
			continue
		}
		cooldown, _ := skillCooldown(v.mods, v.cfg, v.data)
		castRate := 1 / v.active.ge.CastTime * calcMod(v.mods, v.cfg, "Speed") * actionSpeedMod(actor)
		castTime := 1 / math.Min(castRate, serverTickRate)
		count := v.mods.SumCfg(v.cfg, "BASE", key+"EmpoweredSpells")
		stored, _ := num(v.data["storedUses"])
		stored += v.mods.SumCfg(v.cfg, "BASE", "AdditionalCooldownUses")
		rate := out["Speed"] / out["Repeats"]
		uptime := 100.0
		if rate != 0 {
			uptime = math.Min((count/rate)/(cooldown+castTime), 1) * 100
		}
		uptime = math.Min(100, uptime*stored)
		effect := uptime
		if list.Flag("Condition:PactMaxHit") {
			effect = 100
		}
		out[key+"UpTimeRatio"] = uptime
		if l := v.mods.List(v.cfg, key+"PactDamage"); len(l) > 0 {
			if mv, ok := l[0].Value.(map[string]any); ok {
				if mm, ok := mv["mod"].(map[string]any); ok {
					m := modFromMap(mm, "Uptime Scaled "+name)
					m.Value = mulValue(m.Value, effect/100)
					list.Add(m)
				}
			}
		}
		done[key] = true
	}
}
