package engine

import (
	"maps"
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
	// rng is weaponData.range and rangeBonus weaponData.rangeBonus (an
	// item's local range mods); hasRange is false when range is nil
	// (unarmed, shield, monster without attackRange).
	rng, rangeBonus float64
	hasRange        bool
}

type passShared struct {
	mainAcc, mainAccVsEnemy float64
	hasMainAcc              bool
}

type hitPass struct {
	// repeats, cooldown: the skill's Repeats and cooldown (if any).
	repeats, cooldown float64
	hasCooldown       bool
	trapCooldown      float64 // output.TrapCooldown (0: none)
	// mainHandCrit is the main hand weapon's crit chance (BaseCritFromMainHand).
	mainHandCrit float64
	// label is the pass label ("Main Hand", "Off Hand", "Skill").
	label string
	// shared holds the main hand's accuracy for the off hand pass.
	shared *passShared
	// global is the skill's output (globalOutput).
	global map[string]float64
	src    hitSource
	cfg    *Cfg
	conv   map[string]*convEntry
	out    map[string]float64
	base   map[string][2]float64 // <Type>MinBase, <Type>MaxBase
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
	if debugCalcDamage != nil {
		debugCalcDamage(list, p, t, inc, more, names)
	}
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

// enemyResist is calcs.offence's calcResistForType: the enemy's resist of
// type t, or the actor's own with "resist equal to yours".
func enemyResist(actor *ModDB, cfg *Cfg, t string) float64 {
	enemy := actor.enemy
	cm := 0.0
	if actor.enemyEnv != nil {
		cm, _ = actor.enemyEnv.num("enemy" + t + "Resist")
	}
	if _, ok := enemy.OverrideCfg(cfg, t+"Resist"); !ok && actor.Flag("Enemy"+t+"ResistEqualToYours") {
		return math.Max(math.Min(actor.output[t+"Resist"], enemyMaxResistFor(cm)), resistFloor)
	}
	return calcResistForType(enemy, cfg, t, cm)
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
	initialFunc(s, w1, w2)
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
	if actor.Flag("Elusive") && list.Flag("SupportedByNightblade") {
		effect := actor.output["ElusiveEffectMod"] / 100
		list.Add(Mod{Name: "CritMultiplier", Type: "BASE", Value: math.Floor(list.Sum("BASE", "NightbladeElusiveCritMultiplier") * effect), Source: "Nightblade"})
	}
	// CalcOffence "set other limits".
	out["ActiveTrapLimit"] = list.SumCfg(cfg, "BASE", "ActiveTrapLimit")
	out["ActiveMineLimit"] = list.SumCfg(cfg, "BASE", "ActiveMineLimit")
	c.offenceStatConversions(actor, s, w1, w2, out)
	preSkillTypeFunc(s)
	chainStats(actor, s, out)
	projectileStats(s, out)
	pierceForkStats(actor, s, out)
	weaponRange(s, w1, w2, out)
	if s.flags["area"] || s.data["radius"] != nil || (s.flags["mine"] && s.types[stAura]) {
		areaOfEffect(s, out)
	}
	if s.types[stBrand] {
		out["BrandAttachmentRange"] = 30 * calcMod(list, s.cfg, "BrandAttachmentRange") // data.misc.BrandAttachmentRangeBase
		out["BrandAttachmentRangeMetre"] = out["BrandAttachmentRange"] / 10
		out["ActiveBrandLimit"] = list.SumCfg(s.cfg, "BASE", "ActiveBrandLimit")
		out["AttachedBrandCount"] = numOr(s.data["attachedBrandCount"], 0)
	}
	trapMineStats(actor, s, out)
	offenceDuration(actor, s, out)
	// CalcPerform.lua's "maximum sustainable stages" block, from the
	// skill's own cached outputs: Earthquake of Amplification's aftershock
	// scales per 100ms of duration (the rate-based stages are set after the
	// speed, see sustainableStages).
	if s.active.ge.Name == "Earthquake of Amplification" && s.part == 2 {
		list.NewMod("Multiplier:100msEarthquakeDuration", "BASE", math.Floor(out["Duration"]*10), "Skill:EarthquakeAltX")
	}
	impaleDuration(actor, s, out)
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
	if s.flags["totem"] {
		out["TotemLife"] = c.totemLife(s)
	}
	preDamageFunc(actor, s, out)
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
			p := &hitPass{src: w1, cfg: s.weapon1Cfg, out: map[string]float64{}, label: "Main Hand"}
			s.weapon1Cfg.SkillStats = p.out // activeSkill.weapon1Cfg.skillStats = output.MainHand
			passes = append(passes, p)
		}
		if s.flags["shieldAttack"] && w2 == nil {
			// weaponData2 of a shield is empty: the skill sets its damage.
			w2 = &hitSource{min: map[string]float64{}, max: map[string]float64{}}
		}
		if s.flags["weapon2Attack"] && w2 != nil {
			src := *w2
			if cc, ok := num(data["CritChance"]); ok {
				src.critChance = cc
			}
			for _, t := range []string{"Physical", "Fire", "Cold"} {
				mn, ok1 := num(data["setOffHand"+t+"Min"])
				mx, ok2 := num(data["setOffHand"+t+"Max"])
				if ok1 && ok2 {
					src.min, src.max = maps.Clone(src.min), maps.Clone(src.max)
					src.min[t], src.max[t] = mn, mx
				}
			}
			if at, ok := num(data["attackTime"]); ok && at != 0 {
				src.attackRate = 1000 / at
			}
			p := &hitPass{src: src, cfg: s.weapon2Cfg, out: map[string]float64{}, label: "Off Hand"}
			s.weapon2Cfg.SkillStats = p.out
			passes = append(passes, p)
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
		passes = append(passes, &hitPass{src: src, cfg: cfg, out: out, label: "Skill"})
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
	if tc, ok := out["TrapCooldown"]; ok {
		// Traps: the throwing time is limited by the trap cooldown instead.
		cooldown, hasCooldown = tc, false
	} else if hasCooldown {
		out["Cooldown"] = cooldown
	}
	shared := &passShared{}
	for _, p := range passes {
		p.conv = conversionTable(list, p.cfg)
		p.repeats, p.cooldown, p.hasCooldown = out["Repeats"], cooldown, hasCooldown && cooldown > 0
		if _, ok := out["TrapCooldown"]; ok {
			p.trapCooldown = cooldown
		}
		p.mainHandCrit = w1.critChance
		p.shared = shared
		c.hitAndSpeed(actor, s, p)
	}
	combine("AccuracyHitChance", "AVERAGE")
	combine("HitChance", "AVERAGE")
	combine("Speed", "HARMONICMEAN")
	if isAttack {
		// CalcOffence: UseOffhandAttackSpeed (Dual Strike of Ambidexterity) takes
		// the off hand's speed; forceMainHand is not ported.
		if list.Flag("UseOffhandAttackSpeed") {
			for _, p := range passes {
				if p.label == "Off Hand" {
					out["Speed"] = p.out["Speed"]
				}
			}
		}
		for _, st := range []string{"HitSpeed", "HitTime", "Cooldown"} {
			for _, p := range passes {
				if v, ok := p.out[st]; ok {
					out[st] = v
					break
				}
			}
		}
	}
	if out["Speed"] != 0 {
		out["Time"] = 1 / out["Speed"]
	}
	if isAttack {
		hitTime(s, out, true)
	}
	sustainableStages(s, out)
	// skillData.dpsMultiplier is read when used: Ruthless (MAX) changes it
	// in the damage passes.
	dpsMod := (1 + list.SumCfg(cfg, "INC", "DPS")/100) * list.MoreCfg(cfg, "DPS")
	if s.types[stBrand] && !truthy(data["countsAttachedBrandsInDamage"]) {
		dpsMod *= out["AttachedBrandCount"]
		if v, ok := out["SkillDPSMultiplier"]; ok {
			out["SkillDPSMultiplier"] = v * out["AttachedBrandCount"]
		} else {
			out["SkillDPSMultiplier"] = out["AttachedBrandCount"]
		}
	}
	dpsMult := numOr(data["dpsMultiplier"], 1) * dpsMod
	quantity := math.Max(list.SumCfg(cfg, "BASE", "QuantityMultiplier"), 1)
	warcries(actor, s, passes[0].cfg, out)
	pacts(actor, s, out)
	for _, p := range passes {
		ruthlessAndFistOfWar(s, p, out)
		p.global = out
		c.hitDamage(actor, s, p)
		// After hitDamage: postCritFunc may change it too (CalcOffence reads
		// skillData.dpsMultiplier for TotalDPS after postCritFunc).
		dpsMult = numOr(data["dpsMultiplier"], 1) * dpsMod
		speed := out["Speed"]
		if hs, ok := out["HitSpeed"]; ok {
			speed = hs
		}
		p.out["TotalDPS"] = p.out["AverageDamage"] * speed * dpsMult * quantity
		cullAndReservation(actor, s, p, out, dpsMult)
	}
	if debugPasses != nil {
		debugPasses(passes)
	}
	combine("CritChance", "AVERAGE")
	combine("CritMultiplier", "AVERAGE")
	combine("AverageDamage", "DPS")
	combine("TotalDPS", "DPS")
	// Ailments and debuffs, per pass.
	s.flags["igniteCanStack"] = list.FlagCfg(cfg, "IgniteCanStack")
	for _, p := range passes {
		ailmentChances(actor, s, p)
		bleed(actor, s, p, out, dpsMult)
		poison(actor, s, p, out, dpsMult, quantity)
		ignite(actor, s, p, out, dpsMult)
		impale(actor, s, p, out)
	}
	combineAilments(s, out, passes)
	decay(actor, s, out)
	if isAttack {
		out["AverageHit"] = passes[0].out["AverageHit"]
		for k, v := range passes[0].out {
			if _, ok := out[k]; !ok {
				out[k] = v
			}
		}
	}
	skillDot(actor, s, out, dpsMult, quantity)
	combinedDPS(s, out, passes, dpsMult, quantity)
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
	// PoB calls skillModList:More("MORE", cfg, ...): the cfg is the string
	// "MORE", so only mods without flags count.
	out["Accuracy"] = math.Max(0, math.Floor(list.SumCfg(p.cfg, "BASE", "Accuracy")*
		(1+list.SumCfg(p.cfg, "INC", "Accuracy")/100)*list.MoreCfg(nil, "Accuracy")))
	accVsEnemy := math.Max(0, math.Floor(list.SumCfg(p.cfg, "BASE", "Accuracy", "AccuracyVsEnemy")*
		(1+list.SumCfg(p.cfg, "INC", "Accuracy", "AccuracyVsEnemy")/100)*list.MoreCfg(nil, "Accuracy", "AccuracyVsEnemy")))
	// Precise Technique: this pass's accuracy against the player's life.
	player := actor
	if actor.owner != nil {
		player = actor.owner
	}
	list.conditions[strings.ReplaceAll(p.label, " ", "")+"AccRatingHigherThanMaxLife"] = out["Accuracy"] > player.output["Life"]
	if list.Flag("Condition:OffHandAccuracyIsMainHandAccuracy") {
		if p.label == "Main Hand" {
			p.shared.mainAcc, p.shared.mainAccVsEnemy, p.shared.hasMainAcc = out["Accuracy"], accVsEnemy, true
		} else if p.label == "Off Hand" && p.shared.hasMainAcc {
			out["Accuracy"], accVsEnemy = p.shared.mainAcc, p.shared.mainAccVsEnemy
		}
	}
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
	repeats := math.Max(1, p.repeats)
	triggered := truthy(data["triggered"])
	switch {
	case ge.CastTime == 0 && !ge.NoCastTime && data["castTimeOverride"] == nil && !triggered:
		out["Time"], out["Speed"] = 0, 0
		hitTime(s, out, false)
		return
	case data["timeOverride"] != nil:
		t, _ := num(data["timeOverride"])
		if p.trapCooldown > 0 {
			t = math.Max(t, p.trapCooldown)
		} else if p.hasCooldown {
			t = math.Max(t, p.cooldown)
		}
		out["Time"], out["Speed"] = t, 1/t
		hitTime(s, out, false)
		return
	case truthy(data["fixedCastTime"]):
		out["Time"], out["Speed"] = ge.CastTime, 1/ge.CastTime
		hitTime(s, out, false)
		return
	case data["triggerTime"] != nil && triggered:
		t, _ := num(data["triggerTime"])
		t /= 1 + list.SumCfg(p.cfg, "INC", "CooldownRecovery")/100
		if n := list.SumCfg(p.cfg, "BASE", "ActiveSkillsLinkedToTrigger"); n > 0 {
			t *= n
		}
		out["Time"], out["TriggerTime"], out["Speed"] = t, t, 1/t
		hitTime(s, out, false)
		return
	case data["triggerRate"] != nil && triggered:
		r, _ := num(data["triggerRate"])
		out["Time"], out["TriggerTime"], out["Speed"] = 1/r, 1/r, r
		data["showAverage"] = false
		hitTime(s, out, false)
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
		switch {
		case truthy(data["castTimeOverridesAttackTime"]):
			baseTime = ge.CastTime
		case calcMod(list, cfg, "SkillAttackTime") > 0:
			baseTime = (1/rate + list.SumCfg(p.cfg, "BASE", "Speed")) * calcMod(list, cfg, "SkillAttackTime")
		default:
			baseTime = 1/rate + list.SumCfg(p.cfg, "BASE", "Speed")
		}
	} else {
		baseTime = ge.CastTime
		if v, ok := num(data["castTimeOverride"]); ok {
			baseTime = v
		}
		if ge.NoCastTime && data["castTimeOverride"] == nil {
			baseTime = 1 // castTimeOverride or castTime or 1
		}
	}
	// Trauma (HasTrauma) is not ported.
	inc := list.SumCfg(p.cfg, "INC", "Speed")
	more := list.MoreCfg(p.cfg, "Speed")
	if s.flags["warcry"] {
		out["WarcryCastTime"] = warcryCastTime(actor, s)
		out["Speed"] = 1 / out["WarcryCastTime"]
	} else {
		out["Speed"] = 1 / (baseTime/round((1+inc/100)*more, 2) + list.SumCfg(p.cfg, "BASE", "TotalAttackTime") + list.SumCfg(p.cfg, "BASE", "TotalCastTime"))
	}
	if s.flags["selfCast"] {
		out["Speed"] *= actor.output["ActionSpeedMod"]
	}
	if s.flags["totem"] {
		out["TotemActionSpeed"] = 1 + actor.Sum("INC", "TotemActionSpeed")/100
		out["Speed"] *= out["TotemActionSpeed"]
	}
	if p.hasCooldown {
		out["Cooldown"] = p.cooldown
		out["Speed"] = math.Min(out["Speed"], 1/p.cooldown*repeats)
		if s.flags["selfCast"] {
			data["showAverage"] = false
		}
	}
	if !s.types[stChannel] {
		out["Speed"] = math.Min(out["Speed"], serverTickRate*repeats)
	}
	if out["Speed"] == 0 {
		out["Time"] = 0
	} else {
		out["Time"] = 1 / out["Speed"]
	}
	hitTime(s, out, false)
}

// hitTime is the HitTime and HitSpeed part of the speed calculation:
// skills that hit on their own timer (hitTimeOverride, hitTimeMultiplier).
func hitTime(s *activeSkill, out map[string]float64, tickCap bool) {
	data := s.data
	if truthy(data["triggeredOnDeath"]) {
		return
	}
	if v, ok := num(data["hitTimeOverride"]); ok && v != 0 {
		out["HitTime"], out["HitSpeed"] = v, 1/v
		return
	}
	m, ok := num(data["hitTimeMultiplier"])
	if !ok || out["Time"] == 0 {
		return
	}
	out["HitTime"] = out["Time"] * m
	cd, hasCD := out["Cooldown"]
	switch {
	case hasCD && truthy(data["triggered"]):
		out["HitSpeed"] = 1 / math.Max(out["HitTime"], cd)
	case hasCD:
		out["HitSpeed"] = 1 / (out["HitTime"] + cd)
	case tickCap:
		out["HitSpeed"] = math.Min(1/out["HitTime"], serverTickRate)
	default:
		out["HitSpeed"] = 1 / out["HitTime"]
	}
}

// hitDamage is the crit and hit damage part of calcs.offence for one pass:
// AverageHit and AverageDamage.
func (c *Calculator) hitDamage(actor *ModDB, s *activeSkill, p *hitPass) {
	list, cfg, data, out := s.mods, s.cfg, s.data, p.out
	enemy := actor.enemy
	ge := s.active.ge

	c.crit(s, p, enemy, actor)

	// Double and triple damage. PoB differs: its
	// Sum(TripleDamageChance) or 0 + self + onCrit parses as
	// Sum or (0 + ...), so it drops the enemy's SelfTripleDamageChance and
	// triple damage on crit; both count here.
	triple := math.Min(list.SumCfg(p.cfg, "BASE", "TripleDamageChance")+enemy.SumCfg(p.cfg, "BASE", "SelfTripleDamageChance")+
		math.Min(list.SumCfg(p.cfg, "BASE", "TripleDamageChanceOnCrit"), 100)*out["CritChance"]/100, 100)
	double := math.Min(list.SumCfg(p.cfg, "BASE", "DoubleDamageChance")+enemy.SumCfg(p.cfg, "BASE", "SelfDoubleDamageChance")+
		math.Min(list.SumCfg(p.cfg, "BASE", "DoubleDamageChanceOnCrit"), 100)*out["CritChance"]/100, 100)
	if r, ok := p.global["IntimidatingUpTimeRatio"]; ok {
		if list.Flag("Condition:WarcryMaxHit") {
			double = 100
		} else {
			double = math.Min(double+r, 100)
		}
	}
	if triple > 0 {
		double = math.Max(double-triple*double/100, 0)
	}
	out["DoubleDamageChance"], out["TripleDamageChance"] = double, triple
	scaled := 1 + double/100 + 2*triple/100
	warcry := p.global["OffensiveWarcryEffect"]
	if list.Flag("Condition:WarcryMaxHit") {
		warcry = p.global["MaxOffensiveWarcryEffect"]
	}
	if warcry == 0 {
		warcry = 1
	}
	scaled *= out["RuthlessBlowHitEffect"] * out["FistOfWarDamageEffect"] * warcry
	postCritFunc(s, out, p.global)
	redirectAddedDamage(list, p.cfg)

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
	out["impaleStoredHitAvg"] = 0
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
			if pass == 2 {
				out["allMult"] = allMult
			}
			if pass == 1 {
				allMult *= out["CritMultiplier"]
			}
			mn, mx = mn*allMult, mx*allMult
			lucky := 0.0
			ele := t == "Lightning" || t == "Cold" || t == "Fire"
			if list.FlagCfg(cfg, "LuckyHits") ||
				(pass == 2 && t == "Lightning" && list.FlagCfg(cfg, "LightningNoCritLucky")) ||
				(pass == 1 && list.FlagCfg(cfg, "CritLucky")) ||
				(t == "Lightning" && actor.Flag("LightningLuckHits")) ||
				(t == "Chaos" && actor.Flag("ChaosLuckyHits")) ||
				(t == "Fire" && actor.Flag("FireLuckyHits")) ||
				(t == "Cold" && actor.Flag("ColdLuckyHits")) ||
				(ele && list.FlagCfg(cfg, "ElementalLuckHits")) {
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
				if t == "Physical" {
					// Pre-armour physical damage for impale.
					if pass == 1 {
						out["impaleStoredHitAvg"] += avg * out["CritChance"] / 100
					} else {
						out["impaleStoredHitAvg"] += avg * (1 - out["CritChance"]/100)
					}
				}
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
	if s.flags["monsterExplode"] && truthy(s.data["hitChanceIsExplodeChance"]) {
		out["HitChance"] = out["ExplodeChance"]
	}
	out["AverageDamage"] = out["AverageHit"] * out["HitChance"] / 100
}

// crit is the crit chance, crit multiplier and crit effect part of
// calcs.offence's damage pass (mode_effective). Parent and party member
// crit links are not ported.
func (c *Calculator) crit(s *activeSkill, p *hitPass, enemy *ModDB, actor *ModDB) {
	list := s.mods
	out, cfg := p.out, p.cfg
	zero := func() {
		out["PreEffectiveCritChance"], out["CritChance"], out["CritMultiplier"], out["BonusCritDotMultiplier"], out["CritEffect"] = 0, 0, 0, 0, 1
	}
	done := false
	switch {
	case list.FlagCfg(cfg, "NeverCrit"):
		zero()
		done = true
	case list.FlagCfg(cfg, "SpellSkillsCannotDealCriticalStrikesExceptOnFinalRepeat"):
		if p.repeats <= 1 {
			zero()
			done = true
		} else if list.FlagCfg(cfg, "SpellSkillsAlwaysDealCriticalStrikesOnFinalRepeat") {
			mode := "AVERAGE"
			if b := actor.theBuild(); b != nil {
				mode = configString(b.ActiveConfig(), "repeatMode", "AVERAGE")
			}
			switch mode {
			case "None":
				out["PreEffectiveCritChance"], out["CritChance"] = 0, 0
			case "AVERAGE":
				out["PreEffectiveCritChance"], out["CritChance"] = 100/p.repeats, 100/p.repeats
			default:
				out["PreEffectiveCritChance"], out["CritChance"] = 100, 100
			}
		}
	default:
		override, hasOverride := list.OverrideCfg(cfg, "CritChance")
		baseCrit := p.src.critChance
		if hasOverride {
			baseCrit = override
		}
		if list.FlagCfg(cfg, "BaseCritFromMainHand") {
			baseCrit = p.mainHandCrit
		}
		if hasOverride && override == 100 {
			out["PreEffectiveCritChance"], out["PreBifurcateCritChance"], out["CritChance"] = 100, 100, 100
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
			out["PreEffectiveCritChance"] = cc
			rolls := 0.0
			if list.FlagCfg(cfg, "CritChanceLucky") {
				rolls++
			}
			if list.FlagCfg(s.cfg, "ExtremeLuck") {
				rolls *= 2
			}
			if rolls != 0 {
				if actor.Flag("Unexciting") {
					cc = (3*math.Pow(cc/100, 2) - 2*math.Pow(cc/100, 3)) * 100
				} else {
					cc = (1 - math.Pow(1-cc/100, rolls+1)) * 100
				}
			}
			out["PreBifurcateCritChance"] = cc
			if list.FlagCfg(cfg, "BifurcateCrit") {
				cc = (1 - math.Pow(1-cc/100, 2)) * 100
			}
			for _, x := range []struct {
				flag string
				n    float64
			}{{"Every3UseCrit", 3}, {"Every5UseCrit", 5}, {"Every6UseCrit", 6}} {
				if list.FlagCfg(s.cfg, x.flag) {
					cc = ((x.n-1)*cc + 100) / x.n
				}
			}
			out["CritChance"] = cc * out["AccuracyHitChance"] / 100
		}
	}
	if !done {
		if list.FlagCfg(cfg, "NoCritMultiplier") {
			out["CritMultiplier"] = 1
		} else {
			extra := list.SumCfg(cfg, "BASE", "CritMultiplier") / 100
			if o, ok := list.OverrideCfg(s.cfg, "CritMultiplier"); ok {
				extra = (o - 100) / 100
			}
			enemyInc := 1 + enemy.Sum("INC", "SelfCritMultiplier")/100
			extra += enemy.Sum("BASE", "SelfCritMultiplier") / 100
			extra = round(extra*enemyInc, 2)
			override, hasOverride := list.OverrideCfg(cfg, "CritChance")
			if list.FlagCfg(cfg, "BifurcateCrit") && !(hasOverride && override == 100) {
				pre := out["PreBifurcateCritChance"]
				multiChance := pre * pre / 100
				use := 1.0
				for _, x := range []struct {
					flag string
					f    float64
				}{{"Every3UseCrit", 2.0 / 3}, {"Every5UseCrit", 4.0 / 5}, {"Every6UseCrit", 5.0 / 6}} {
					if list.FlagCfg(s.cfg, x.flag) {
						use *= x.f
					}
				}
				multiChance *= use
				cond := 0.0
				if out["CritChance"] > 0 {
					cond = multiChance / out["CritChance"]
				}
				out["CritBifurcates"] = 1 + cond
				extra += cond * extra
				list.Add(Mod{Name: "CritMultiplier", Type: "MORE", Value: floorTo(cond*100, 2), Flags: mfHit, Source: "Bifurcated Crit Damage Bonus"})
			}
			out["CritMultiplier"] = 1 + math.Max(0, extra)
		}
		cc := out["CritChance"] / 100
		out["CritEffect"] = 1 - cc + cc*out["CritMultiplier"]
		out["BonusCritDotMultiplier"] = (list.SumCfg(cfg, "BASE", "CritMultiplier") - 50) * list.SumCfg(cfg, "BASE", "CritMultiplierAppliesToDegen") / 10000
	}
	if out["CritChance"] != 0 {
		list.conditions["CritInPast8Sec"] = true
	}
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
		resistOf := func(t string) float64 { return enemyResist(actor, cfg, t) }
		resist = resistOf(t)
		ignores := func(e string) bool {
			names := []string{"Ignore" + e + "Resistance"}
			if isElemental[e] {
				names = append(names, "IgnoreElementalResistances")
			}
			if list.FlagCfg(cfg, names...) || enemy.Flag("SelfIgnore"+e+"Resistance") {
				return true
			}
			ch := []string{"ChanceToIgnore" + e + "Resistance"}
			if isElemental[e] {
				ch = append(ch, "ChanceToIgnoreElementalResistances")
			}
			return math.Min(math.Max(list.SumCfg(cfg, "BASE", ch...), 0), 100) >= 100
		}
		lowestChaos := t == "Chaos" && (list.FlagCfg(cfg, "ChaosDamageUsesLowestResistance") || list.FlagCfg(cfg, "ChaosDamageUsesHighestResistance"))
		if lowestChaos || (isElemental[t] && list.FlagCfg(cfg, "ElementalDamageUsesLowestResistance")) {
			used := t
			if isElemental[t] {
				takenInc += enemy.SumCfg(cfg, "INC", "ElementalDamageTaken")
			}
			highest := list.FlagCfg(cfg, "ChaosDamageUsesHighestResistance")
			for _, e := range dmgTypeList {
				if !isElemental[e] || e == t || ignores(e) {
					continue
				}
				r := resistOf(e)
				if (highest && resist < r) || (!highest && resist > r) {
					resist, used = r, e
				}
			}
			if isElemental[used] {
				pen = list.SumCfg(cfg, "BASE", used+"Penetration", "ElementalPenetration")
			} else if used == "Chaos" {
				pen = list.SumCfg(cfg, "BASE", "ChaosPenetration")
			}
		} else {
			switch {
			case isElemental[t]:
				pen = list.SumCfg(cfg, "BASE", t+"Penetration", "ElementalPenetration")
				takenInc += enemy.SumCfg(cfg, "INC", "ElementalDamageTaken")
			case t == "Chaos":
				pen = list.SumCfg(cfg, "BASE", "ChaosPenetration")
			}
		}
	}
	invert := 0.0
	if isElemental[t] {
		invert = math.Max(math.Min(list.SumCfg(cfg, "CHANCE", "HitsInvertEleResChance", "HitsInvert"+t+"ResChance"), 1), 0)
	} else if t == "Chaos" {
		invert = math.Max(math.Min(list.SumCfg(cfg, "CHANCE", "HitsInvertChaosResChance"), 1), 0)
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
	switch {
	case isElemental[t] && list.FlagCfg(cfg, "CannotElePenIgnore"):
		r := resist
		if invert > 0 {
			r = resist - 2*invert*resist
		}
		eff *= 1 - r/100
	case useResChance > 0:
		r := (resist - pen) * useResChance
		if invert > 0 {
			r = ((resist-pen)*(1-invert) + (-resist-pen)*invert) * useResChance
		}
		eff *= 1 - r/100
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
	if !ok || db.Flag("DisableWeapons") {
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
func preDamageFunc(actor *ModDB, s *activeSkill, out map[string]float64) {
	data := s.data
	proj := out["ProjectileCount"]
	mulDPS := func(m float64) { data["dpsMultiplier"] = numOr(data["dpsMultiplier"], 1) * m }
	switch s.active.ge.Name {
	case "On Kill Monster Explosion":
		enemyExplode(s, out)
	case "Barrage", "Barrage of Volley Fire", "Blast Rain", "Vaal Lightning Arrow":
		if s.part == 2 {
			data["dpsMultiplier"] = proj
		}
	case "Blazing Salvo":
		if s.part == 2 {
			mulDPS(proj)
		}
	case "Forbidden Rite", "Forbidden Rite of Soul Sacrifice":
		if s.part == 2 {
			mulDPS(proj + 1)
		}
	case "Freezing Pulse":
		r := 60 * out["ProjectileSpeedMod"]
		s.mods.Add(Mod{Name: "Damage", Type: "MORE", Value: -50.0, Source: "Skill:FreezingPulse",
			Tags: []map[string]any{{"type": "DistanceRamp", "ramp": []any{[]any{0.0, 0.0}, []any{r, 1.0}}}}})
		s.mods.Add(Mod{Name: "EnemyFreezeChance", Type: "BASE", Value: 25.0, Source: "Skill:FreezingPulse",
			Tags: []map[string]any{{"type": "DistanceRamp", "ramp": []any{[]any{0.0, 1.0}, []any{15 * out["ProjectileSpeedMod"], 0.0}}}}})
	case "Lightning Tendrils of Eccentricity":
		// The DPS of each pulse part.
		interval := numOr(data["pulseInterval"], 0)
		part := []map[string]any{{"type": "SkillPart", "skillPart": float64(s.part)}}
		switch s.part {
		case 2:
			s.mods.Add(Mod{Name: "DPS", Type: "MORE", Value: -(1 / interval) * 100, Source: "Normal pulse", Tags: part})
		case 3:
			s.mods.Add(Mod{Name: "DPS", Type: "MORE", Value: -(interval - 1) / interval * 100, Source: "Stronger pulse", Tags: part})
		}
	case "Kinetic Fusillade":
		if s.part == 1 {
			data["dpsMultiplier"] = proj
			// Each projectile deals more damage per earlier one: the average.
			if per := numOr(data["damagePerProjectile"], 0); per != 0 && proj > 1 {
				avg := per * (proj - 1) / 2
				s.mods.NewMod("Damage", "MORE", avg, "Skill:KineticFusillade")
				out["KineticFusilladeAvgMoreMult"] = avg
			}
		}
	case "Molten Strike", "Molten Strike of the Zenith":
		if s.part == 1 {
			break
		}
		enemyRadius, ok := s.mods.OverrideCfg(s.cfg, "EnemyRadius")
		if !ok {
			enemyRadius = s.mods.SumCfg(s.cfg, "BASE", "EnemyRadius")
		}
		inner, outer := out["AreaOfEffectRadiusSecondary"], out["AreaOfEffectRadiusTertiary"]
		overlap := math.Min(1, (enemyRadius+out["AreaOfEffectRadius"]-inner)/(outer-inner))
		out["OverlapChance"] = overlap * 100
		mult := 1.0
		if s.part == 3 || s.part == 5 || s.part == 6 {
			mult = overlap * proj
			if s.part == 6 {
				// Zenith: the weighted average of normal and 5th attack balls.
				fifth := 1 + numOr(data["FifthStrikeDamage"], 0)/100
				mult = 0.8*mult + 0.2*fifth*overlap*(proj+numOr(data["FifthStrikeProjectiles"], 0))
			}
		}
		if mult != 1 {
			mulDPS(mult)
			if v, ok := out["SkillDPSMultiplier"]; ok {
				mult *= v
			}
			out["SkillDPSMultiplier"] = mult
		}
	case "Avenging Flame":
		// The trigger source's totem life (triggerSourceUUID's TotemLife).
		add := numOr(data["triggerSourceTotemLife"], 0) * numOr(data["lifeDealtAsFire"], 0) / 100
		data["FireMin"] = numOr(data["FireMin"], 0) + add
		data["FireMax"] = numOr(data["FireMax"], 0) + add
	case "Bladefall of Trarthus":
		data["hitTimeOverride"] = numOr(data["hitFrequency"], 0) / (1 + numOr(data["incVolleyFrequency"], 0)/100)
	case "Tornado of Elemental Turbulence":
		data["hitTimeOverride"] = data["damageInterval"]
		mulDPS(s.mods.SumCfg(s.cfg, "BASE", "Multiplier:TornadoofElementalTurbulenceStage"))
	case "Storm Burst":
		freq, _ := num(data["repeatFrequency"])
		dur := numOr(data["duration"], 0) * out["DurationMod"]
		switch s.part {
		case 2:
			// duration * 10 / (jump * 10) avoids floating point issues; +1 tick on spawn.
			mulDPS(1 + math.Floor(dur*10/(freq*10)))
		case 3:
			jumps := math.Floor((dur + 0.001) / freq)
			per, _ := num(data["remainingDurationDamage"])
			s.mods.Add(Mod{Name: "Damage", Type: "MORE", Value: math.Max(per*jumps-100, 0), Source: "Skill:StormBurst"})
		}
	case "Spark", "Spark of the Nova":
		if s.part == 2 {
			mulDPS(1 + math.Floor(out["Duration"]/0.66))
		}
	case "Ice Spear", "Ice Spear of Splitting":
		if s.part == 3 || s.part == 4 {
			mulDPS(proj)
		}
	case "Toxic Rain", "Toxic Rain of Sporeburst", "Toxic Rain of Withering":
		data["dpsMultiplier"] = math.Min(numOr(data["podOverlapMultiplier"], 1), proj)
	case "Shrapnel Ballista", "Shrapnel Ballista of Steel":
		if !s.mods.Flag("SequentialProjectiles") {
			overlap := 1.0
			if v, ok := num(data["ShrapnelBallistaProjectileOverlap"]); ok {
				overlap = v
			} else if s.types[89] { // Rain
				overlap = proj
			}
			mulDPS(math.Min(overlap, proj))
		}
	case "Bodyswap", "Bodyswap of Sacrifice":
		if s.part == 1 {
			m, _ := num(data["selfFireExplosionLifeMultiplier"])
			life := actor.output["Life"]
			if s.flags["totem"] {
				life = out["TotemLife"]
			}
			data["FireBonusMin"], data["FireBonusMax"] = life*m, life*m
		}
	case "Ball Lightning of Orbiting":
		if s.part == 2 || s.part == 3 {
			interval, _ := num(data["strikeInterval"])
			dur, _ := num(data["duration"])
			if interval > 0 {
				if n := math.Floor(dur / interval); n != 1 {
					data["dpsMultiplier"] = numOr(data["dpsMultiplier"], 1) * n
				}
			}
		}
	case "Arcanist Brand", "Armageddon Brand", "Armageddon Brand of Recall", "Penance Brand",
		"Penance Brand of Dissipation", "Penance Brand of Conduction", "Storm Brand", "Storm Brand of Indecision":
		// Brands hit once per activation: repeatFrequency / (1 + inc Speed and
		// BrandActivationFrequency) / more BrandActivationFrequency. Penance Brand
		// activates once per its max energy.
		f, _ := num(data["repeatFrequency"])
		if s.active.ge.Name == "Penance Brand" {
			f *= s.mods.SumCfg(s.cfg, "BASE", "Multiplier:PenanceBrandMaxEnergy")
		}
		data["hitTimeOverride"] = f / (1 + s.mods.SumCfg(s.cfg, "INC", "Speed", "BrandActivationFrequency")/100) / s.mods.MoreCfg(s.cfg, "BrandActivationFrequency")
	case "Wintertide Brand":
		// act_int.lua WintertideBrand preDamageFunc: part 1 averages the stages
		// gained per activation over the attached duration, plus the
		// strongest maximum-stage Wintertide's End debuff for its uptime.
		data["countsAttachedBrandsInDamage"] = s.part == 1
		f, _ := num(data["repeatFrequency"])
		hitTime := f / (1 + s.mods.SumCfg(s.cfg, "INC", "Speed", "BrandActivationFrequency")/100) / s.mods.MoreCfg(s.cfg, "BrandActivationFrequency")
		data["hitTimeOverride"] = hitTime
		if s.part == 1 && hitTime > 0 {
			list, cfg := s.mods, s.cfg
			skillMaxStages := list.SumCfg(cfg, "BASE", "Multiplier:WintertideBrandMaxStages")
			debuffDurationMult := 1 / math.Max(buffExpirationSlowCap, calcMod(actor.enemy, cfg, "BuffExpireFaster"))
			duration := skillDuration(list, cfg, data, nil) * debuffDurationMult
			if duration <= 0 {
				break
			}
			maxStages := math.Min(math.Floor(duration/hitTime), skillMaxStages)
			timeAtMaxStages := math.Max(duration-maxStages*hitTime, 0)
			damagePerStage := list.SumCfg(cfg, "BASE", "Multiplier:WintertideBrandDamagePerStage")
			averageStages := (maxStages*(maxStages-1)/2*hitTime + maxStages*timeAtMaxStages) / duration
			endBase := numOr(data["durationTertiary"], 0) + list.SumCfg(cfg, "BASE", "Duration", "TertiaryDuration")
			endMod := math.Max(calcMod(list, cfg, "Duration", "TertiaryDuration"), 0)
			endDuration := math.Ceil(endBase*endMod*debuffDurationMult*serverTickRate) / serverTickRate
			endUptime := math.Min(endDuration/duration, 1)
			attached := numOr(data["attachedBrandCount"], 0)
			mult := attached*(100+averageStages*damagePerStage) + endUptime*(100+maxStages*damagePerStage) - 100
			list.NewMod("Damage", "MORE", mult, "Wintertide Brand Average Multiplier")
		}
	case "Flameblast", "Flameblast of Celerity", "Flameblast of Contraction":
		// One release per channelled stage beyond the starting stages.
		stage := "Multiplier:" + strings.Join(strings.Fields(s.active.ge.Name), "") + "Stage"
		data["hitTimeMultiplier"] = math.Max(s.mods.SumCfg(s.cfg, "BASE", stage)-s.mods.SumCfg(s.cfg, "BASE", "Multiplier:FlameblastMinimumStage"), 1)
	case "Static Strike":
		if s.part == 2 {
			f, _ := num(data["repeatFrequency"])
			data["hitTimeOverride"] = f / (1 + s.mods.SumCfg(s.cfg, "INC", "StaticStrikeFrequency")/100)
		}
	}
}

// numOr is num(v) with a default.
func numOr(v any, def float64) float64 {
	if f, ok := num(v); ok {
		return f
	}
	return def
}

// pacts is the Pact part of the damage passes of calcs.offence: each
// "Pact of X" skill the actor has adds its damage scaled by its uptime to
// the spells it empowers. Pact of Beidat's extra projectiles, chains and
// cascades are not ported.
func pacts(actor *ModDB, s *activeSkill, out map[string]float64) {
	const stDamageOverTime, stCascadable = 35, 56
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

// warcryCastTime is calcWarcryCastTime (CalcOffence.lua).
func warcryCastTime(actor *ModDB, v *activeSkill) float64 {
	if v.mods.FlagCfg(v.cfg, "InstantWarcry") || truthy(v.data["triggeredByAutoexertion"]) {
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
	const stMelee, stMeleeSingleTarget, stSummonsTotem, stSlam, stOtherThingUsesSkill, stNeverExertable, stRetaliation = 20, 21, 26, 92, 95, 122, 121
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
		cd, _ := skillCooldown(v.mods, v.cfg, v.data)
		r := 100.0
		if speed != 0 {
			r = math.Min((count/speed)/(cd+warcryCastTime(actor, v)), 1) * 100
		}
		stored := numOr(v.data["storedUses"], 0) + v.mods.SumCfg(v.cfg, "BASE", "AdditionalCooldownUses")
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
	const stSlam, stOtherThingUsesSkill = 92, 95
	list, cfg, out := s.mods, p.cfg, p.out
	out["RuthlessBlowHitEffect"], out["FistOfWarDamageEffect"] = 1, 1
	mode := "AVERAGE"
	if b := list.theBuild(); b != nil {
		mode = configString(b.ActiveConfig(), "ruthlessSupportMode", "AVERAGE")
	}
	chance := 0.0
	n := list.SumCfg(cfg, "BASE", "RuthlessBlowMaxCount")
	out["RuthlessBlowMaxCount"] = n
	if n > 0 && (!cfg.SkillCond["usedByMirage"] || numOr(s.data["mirageUses"], 0) > n) {
		switch mode {
		case "AVERAGE":
			chance = round(100/n, 0)
		case "MAX":
			chance = 100
			s.data["dpsMultiplier"] = numOr(s.data["dpsMultiplier"], 1) / n
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

// preSkillTypeFunc ports the skills' preSkillTypeFunc (Data/Skills/*.lua).
func preSkillTypeFunc(s *activeSkill) {
	list, cfg, data := s.mods, s.cfg, s.data
	switch s.active.ge.Name {
	case "Rage Vortex":
		if s.part == 2 {
			maxRage := list.SumCfg(cfg, "BASE", "MaximumRage")
			pct := numOr(data["MaxRageVortexSacrificePercentage"], 0) / 100
			override := list.SumCfg(cfg, "BASE", "Multiplier:RageSacrificedStacks")
			maxSac := math.Floor(pct * maxRage)
			stacks := maxSac
			if override > 0 {
				stacks = math.Min(override, maxSac)
			}
			list.NewMod("Multiplier:RageSacrificed", "BASE", stacks, "Skill:RageVortex")
		}
	}
}

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
	sources := append(tree, items...)
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

// postCritFunc ports the skills' postCritFunc (Data/Skills/*.lua), run
// after the crit chance of each damage pass. PoB passes the global output
// (runSkillFunc's upvalue); out is the pass's, which is the same for spells.
func postCritFunc(s *activeSkill, out, global map[string]float64) {
	switch s.active.ge.Name {
	case "Kinetic Fusillade":
		// Attacking faster than the projectiles fire wastes damage.
		n := 1.0
		if s.part == 1 {
			n = global["ProjectileCount"]
		}
		delay := (numOr(s.data["duration"], 0) + numOr(s.data["delayPerProjectile"], 0)*(n-1)) * global["DurationMod"]
		maxAPS := 1 / (math.Ceil(delay/serverTickTime) * serverTickTime)
		// PoB differs: it scales the current dpsMultiplier on every pass, so
		// a dual wielder's off hand gets the efficiency twice. It applies
		// once here, to the multiplier before the first pass.
		if s.part == 1 && global["Speed"] > maxAPS {
			base, ok := num(s.data["kineticFusilladeBaseDps"])
			if !ok {
				base = numOr(s.data["dpsMultiplier"], global["ProjectileCount"])
				s.data["kineticFusilladeBaseDps"] = base
			}
			s.data["dpsMultiplier"] = base * maxAPS / global["Speed"]
		}
		global["KineticFusilladeMaxEffectiveAPS"] = maxAPS
	case "Lightning Tendrils of Eccentricity":
		// The 5th pulse's more damage as an average over the interval.
		if s.part != 1 {
			return
		}
		interval := numOr(s.data["pulseInterval"], 0)
		pulse := numOr(s.data["pulseDamage"], 0) / 100
		crit, eff, multi := out["PreEffectiveCritChance"]/100, out["CritChance"]/100, out["CritMultiplier"]
		avg := 100 * (((interval-1)*(1+crit*(multi-1))+(1+pulse)*multi)/(interval*((1-eff)+multi*eff)) - 1)
		s.mods.Add(Mod{Name: "Damage", Type: "MORE", Value: avg, Source: "Average Pulse Damage", KeywordFlags: kfHit | kfAilment,
			Tags: []map[string]any{{"type": "SkillPart", "skillPart": 1.0}}})
	}
}

// redirectAddedDamage is calcs.offence's "Added damage redirection"
// (Cryogenesis): with AllAddedDamageAsLightning or AllAddedDamageAsCold,
// the applicable added damage mods of the other types become that type
// (ModList:ConvertMod, in place), except Elemental Hit's own. PoB's marker
// tag ("Cryogenesis Added Damage") is display only and not added.
func redirectAddedDamage(list *ModDB, cfg *Cfg) {
	to := ""
	switch {
	case list.FlagCfg(cfg, "AllAddedDamageAsLightning"):
		to = "Lightning"
	case list.FlagCfg(cfg, "AllAddedDamageAsCold"):
		to = "Cold"
	default:
		return
	}
	for _, t := range dmgTypeList {
		if t == to {
			continue
		}
		for _, suffix := range []string{"Min", "Max"} {
			from, dst := t+suffix, to+suffix
			for d := list; d != nil; d = d.parent {
				kept := d.mods[from][:0]
				for _, m := range d.mods[from] {
					if _, ok := list.eval(&m, cfg); ok && eligible(&m, "BASE", cfg) && !strings.Contains(m.Source, "ElementalHit") {
						m.Name = dst
						d.mods[dst] = append(d.mods[dst], m)
						continue
					}
					kept = append(kept, m)
				}
				d.mods[from] = kept
			}
		}
	}
}

// sustainableStages is the rest of CalcPerform.lua's "maximum sustainable
// stages" block, from the skill's cached Speed, HitSpeed and Duration
// (getCachedOutputValue; the cached run has no stage mods of its own, and
// these do not depend on them): Blight's channelled stages, Penance Brand
// of Dissipation's ticks and Scorching Ray's maximum, on skill part 2.
func sustainableStages(s *activeSkill, out map[string]float64) {
	if s.part != 2 {
		return
	}
	name := s.active.ge.Name
	stage := "Multiplier:" + strings.Join(strings.Fields(name), "")
	set := func(n float64) {
		s.mods.NewMod(stage+"MaxStages", "BASE", n, "Base")
		s.mods.NewMod(stage+"StageAfterFirst", "BASE", n, "Base")
	}
	switch name {
	case "Blight", "Blight of Contagion", "Blight of Atrophy":
		base := s.mods.SumCfg(s.cfg, "BASE", "BlightBaseMaxStages")
		set(math.Min(math.Floor(out["Speed"]*out["Duration"])-1, base-1))
	case "Penance Brand of Dissipation":
		// HitSpeed is the brand's activation frequency.
		set(math.Max(math.Min(math.Floor(out["HitSpeed"]*out["Duration"])-1, 19), 0))
	case "Scorching Ray", "Scorching Ray of Immolation":
		set(7)
	}
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
	const stMeleeSingleTarget = 21
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

// initialFunc ports the skills' initialFunc (Data/Skills/*.lua), run first
// in calcs.offence. Cyclone: AdditionalMeleeRange is the weapon's range
// bonus plus MeleeWeaponRange (averaged when dual wielding), or the
// unarmed range bonus. Not ported: Blood Sacrament.
func initialFunc(s *activeSkill, w1 hitSource, w2 *hitSource) {
	var source string
	switch s.active.ge.Name {
	case "Cyclone", "Vaal Cyclone":
		source = "Skill:Cyclone"
	case "Cyclone of Tumult":
		source = "Skill:CycloneAltX"
	default:
		return
	}
	list := s.mods
	bonus := func(w *hitSource, wcfg *Cfg) float64 {
		return list.SumCfg(wcfg, "BASE", "MeleeWeaponRange") + 10*list.SumCfg(wcfg, "BASE", "MeleeWeaponRangeMetre") + w.rangeBonus
	}
	var r float64
	if s.flags["weapon1Attack"] && w1.hasRange {
		r = bonus(&w1, s.weapon1Cfg)
		if s.flags["weapon2Attack"] && w2 != nil && w2.hasRange {
			r = (r + bonus(w2, s.weapon2Cfg)) / 2
		}
	} else {
		r = list.SumCfg(s.cfg, "BASE", "UnarmedRange") + 10*list.SumCfg(s.cfg, "BASE", "UnarmedRangeMetre")
	}
	list.NewMod("Multiplier:AdditionalMeleeRange", "BASE", r, source)
}
