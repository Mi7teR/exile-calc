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
	// dpsMod is the DPS INC/MORE (and brand) part of skillData.dpsMultiplier.
	dpsMod float64
	// totalHitMin, totalHitMax: the non-crit hit's range (output.TotalMin/Max).
	totalHitMin, totalHitMax float64
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
	initialFunc(actor, s, w1, w2)
	for _, e := range list.List(cfg, "SkillData") {
		applySkillData(data, e.Value)
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
	preSkillTypeFunc(actor, s)
	chainStats(actor, s, out)
	projectileStats(s, out)
	pierceForkStats(actor, s, out)
	weaponRange(s, w1, w2, out)
	if s.flags["area"] || s.data.val("radius") != nil || (s.flags["mine"] && s.types[stAura]) {
		areaOfEffect(s, out)
	}
	if s.types[stBrand] {
		out["BrandAttachmentRange"] = 30 * calcMod(list, s.cfg, "BrandAttachmentRange") // data.misc.BrandAttachmentRangeBase
		out["BrandAttachmentRangeMetre"] = out["BrandAttachmentRange"] / 10
		out["ActiveBrandLimit"] = list.SumCfg(s.cfg, "BASE", "ActiveBrandLimit")
		out["AttachedBrandCount"] = numOr(s.data.val("attachedBrandCount"), 0)
	}
	trapMineStats(actor, s, out)
	offenceDuration(actor, s, out)
	skillTypeOutputs(actor, s, out)
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
	// CalcOffence.lua:1903-1904: Eldritch Battery adds maximum Energy
	// Shield to the Mana available for payable-cost damage.
	pool := actor.output["ManaUnreserved"]
	if actor.Flag("EnergyShieldProtectsMana") {
		pool += actor.output["EnergyShield"]
	}
	actor.output["ManaCostPayablePool"], out["ManaCostPayablePool"] = pool, pool
	// Sacrifice Support: added chaos damage from unreserved life.
	if pct, ok := num(data.val("sacrificeLifePercent")); ok {
		life := actor.output["LifeUnreserved"]
		if s.flags["totem"] {
			life = c.totemLife(s)
		}
		asChaos, _ := num(data.val("sacrificeLifeAsChaos"))
		add := math.Floor(life * pct / 100 * (asChaos / 100))
		list.Add(Mod{Name: "ChaosMin", Type: "BASE", Value: add, Source: "Sacrifice Support"})
		list.Add(Mod{Name: "ChaosMax", Type: "BASE", Value: add, Source: "Sacrifice Support"})
	}
	if s.flags["totem"] {
		// CalcOffence.lua: totem (or ballista) placement speed.
		base := list.SumCfg(s.cfg, "BASE", "TotemPlacementTime")
		if s.flags["ballista"] {
			base = list.SumCfg(s.cfg, "BASE", "BallistaPlacementTime")
		}
		out["TotemPlacementSpeed"] = 1 / base * calcMod(list, s.cfg, "TotemPlacementSpeed") *
			actor.output["ActionSpeedMod"]
		out["TotemPlacementTime"] = 1 / out["TotemPlacementSpeed"]
		out["TotemLife"] = c.totemLife(s)
	}
	preDamageFunc(actor, s, out)
	// Corpse and enemy explosions: base damage from the monster's life.
	monsterLife := 100.0
	if v, ok := num(data.val("corpseLife")); ok {
		monsterLife = v
	} else if actor.enemyEnv != nil && actor.enemyEnv.level > 0 && actor.enemyEnv.level <= len(c.data.Monster.Life) {
		lv := actor.enemyEnv.level
		monsterLife = c.data.Monster.Life[lv-1]
	}
	if truthy(data.val("explodeCorpse")) {
		t := "Fire"
		if v, ok := data.val("corpseExplosionDamageType").(string); ok {
			t = v
		}
		m, ok := num(data.val("corpseExplosionLifeMultiplier"))
		if !ok {
			m, _ = num(data.val("selfFireExplosionLifeMultiplier"))
		}
		data.set(t+"BonusMin", monsterLife*m)
		data.set(t+"BonusMax", monsterLife*m)
	}
	if s.flags["monsterExplode"] {
		for _, t := range dmgTypeList {
			p, _ := num(data.val(t + "EffectiveExplodePercentage"))
			data.set(t+"Min", p*monsterLife/100) //nolint:mnd // PoB literal
			data.set(t+"Max", p*monsterLife/100) //nolint:mnd // PoB literal
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
			if cc, ok := num(data.val("CritChance")); ok {
				src.critChance = cc
			}
			for _, t := range []string{"Physical", "Fire", "Cold"} {
				mn, ok1 := num(data.val("setOffHand" + t + "Min"))
				mx, ok2 := num(data.val("setOffHand" + t + "Max"))
				if ok1 && ok2 {
					src.min, src.max = maps.Clone(src.min), maps.Clone(src.max)
					src.min[t], src.max[t] = mn, mx
				}
			}
			if at, ok := num(data.val("attackTime")); ok && at != 0 {
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
			mn, _ := num(data.val(t + "Min"))
			mx, _ := num(data.val(t + "Max"))
			bmn, _ := num(data.val(t + "BonusMin"))
			bmx, _ := num(data.val(t + "BonusMax"))
			src.min[t], src.max[t] = mn+bmn, mx+bmx
		}
		if cc, ok := num(data.val("CritChance")); ok {
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
			// output.MainHand[stat] or output.OffHand[stat]: nil stays unset.
			if v, ok := passes[0].out[stat]; ok {
				out[stat] = v
			}
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
			if !truthy(data.val("doubleHitsWhenDualWielding")) {
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
		p.global = out // globalOutput: TotemPlacementTime for fire-once totems
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
	if s.types[stBrand] && !truthy(data.val("countsAttachedBrandsInDamage")) {
		dpsMod *= out["AttachedBrandCount"]
		mulSkillDPSOutput(out, out["AttachedBrandCount"]) // CalcOffence.lua:2520
	}
	dpsMult := data.dpsMultiplier.or(1) * dpsMod
	quantity := math.Max(list.SumCfg(cfg, "BASE", "QuantityMultiplier"), 1)
	if quantity > 1 {
		out["QuantityMultiplier"] = quantity // CalcOffence.lua:2627-2630
	}
	warcries(actor, s, passes[0].cfg, out)
	pacts(actor, s, out)
	for _, p := range passes {
		ruthlessAndFistOfWar(s, p, out)
		p.global, p.dpsMod = out, dpsMod
		c.hitDamage(actor, s, p)
		burstOutputs(s, p, out, monsterLife)
		// After hitDamage: postCritFunc may change it too (CalcOffence reads
		// skillData.dpsMultiplier for TotalDPS after postCritFunc).
		dpsMult = data.dpsMultiplier.or(1) * dpsMod
		speed := out["Speed"]
		if hs, ok := out["HitSpeed"]; ok {
			speed = hs
		}
		p.out["TotalDPS"] = p.out["AverageDamage"] * speed * dpsMult * quantity
		pvpDamage(actor, s, p, speed, dpsMult, p.totalHitMin, p.totalHitMax)
		cullAndReservation(actor, s, p, out, dpsMult)
	}
	if debugPasses != nil {
		debugPasses(passes)
	}
	combine("PreEffectiveCritChance", "AVERAGE") // CalcOffence.lua:4003
	combine("CritChance", "AVERAGE")
	combine("CritMultiplier", "AVERAGE")
	combine("AverageDamage", "DPS")
	combine("PvpAverageDamage", "DPS")
	combine("TotalDPS", "DPS")
	combine("PvpTotalDPS", "DPS")
	for _, r := range leechPools { // CalcOffence.lua:4010-4031
		for _, st := range []string{"LeechDuration", "LeechInstances", "LeechInstant", "LeechInstantRate",
			"LeechInstantProportion"} {
			combine(r+st, "DPS")
		}
	}
	for _, r := range leechPools {
		for _, st := range []string{"OnHit", "OnHitRate", "OnKill"} {
			combine(r+st, "DPS")
		}
	}
	leechRates(actor, s, out)
	// Ailments and debuffs, per pass.
	s.flags["igniteCanStack"] = list.FlagCfg(cfg, "IgniteCanStack")
	for _, p := range passes {
		ailmentChances(actor, s, p)
		bleed(actor, s, p, out, dpsMult)
		poison(actor, s, p, out, dpsMult, quantity)
		ignite(actor, s, p, out, dpsMult)
		nonDamagingAilments(actor, s, p)
		impale(actor, s, p, out)
	}
	combineAilments(s, out, passes)
	decay(actor, s, out)
	// Attacks: PoB keeps each hand pass's stats in output.MainHand and
	// output.OffHand; only combineStat's stats reach the skill's output.
	dropsBurningGround(actor, s, out)
	skillDot(actor, s, out, dpsMult, quantity)
	combinedDPS(s, out, passes, dpsMult, quantity, monsterLife)
	perSecondCosts(actor, s, out)
	return out
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
	if h := hooksOf(s); h != nil && h.preDamage != nil {
		h.preDamage(actor, s, out)
	}
}

// pacts is the Pact part of the damage passes of calcs.offence: each
// "Pact of X" skill the actor has adds its damage scaled by its uptime to
// the spells it empowers, and Pact of Beidat its extra projectiles, beam
// chains and cascades the same way.
func pacts(actor *ModDB, s *activeSkill, out map[string]float64) {
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
		stored, _ := v.data.storedUses.get()
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
		// CalcOffence.lua:2945-2948, 2998.
		out["CreatePactOffensiveCalcSection"] = 1
		out["PactOf"+key+"Cooldown"], out["PactOf"+key+"CastTime"] = cooldown, castTime
		out[key+"EmpoweredCount"] = count
		if l := v.mods.List(v.cfg, key+"PactDamage"); len(l) > 0 {
			if mv, ok := l[0].Value.(map[string]any); ok {
				if mm, ok := mv["mod"].(map[string]any); ok {
					m := modFromMap(mm, "Uptime Scaled "+name)
					m.Value = mulValue(m.Value, effect/100)
					list.Add(m)
				}
			}
		}
		if key == "Beidat" {
			// Coverage bonuses are averaged with the same uptime as the damage bonus.
			mult := effect / 100 //nolint:mnd // percent
			if s.flags["projectile"] && !list.FlagCfg(s.cfg, "NoAdditionalProjectiles") &&
				!list.FlagCfg(s.cfg, "SingleProjectile") {
				out["BeidatAdditionalProjectiles"] = v.mods.SumCfg(v.cfg, "BASE", "BeidatAdditionalProjectiles") * mult
				out["ProjectileCount"] += out["BeidatAdditionalProjectiles"]
				// globalOutput is the actor's output.
				actor.output["BeidatAdditionalProjectiles"] = out["BeidatAdditionalProjectiles"]
				actor.output["ProjectileCount"] = out["ProjectileCount"]
			}
			if s.flags["chaining"] && !s.flags["projectile"] && !list.FlagCfg(s.cfg, "CannotChain") &&
				!list.FlagCfg(s.cfg, "NoAdditionalChains") {
				out["BeidatAdditionalBeamChains"] = v.mods.SumCfg(v.cfg, "BASE", "BeidatAdditionalBeamChains") * mult *
					list.MoreCfg(s.cfg, "ChainCountMax")
				out["ChainMax"] += out["BeidatAdditionalBeamChains"]
				out["ChainMaxString"] = out["ChainMax"]
				out["ChainRemaining"] = math.Max(0, out["ChainMax"]-out["Chain"])
				// globalOutput is the actor's output: PerStat ChainRemaining (Arc) reads it.
				actor.output["ChainMax"], actor.output["ChainRemaining"] = out["ChainMax"], out["ChainRemaining"]
			}
			if s.types[stCascadable] {
				out["BeidatAdditionalCascades"] = v.mods.SumCfg(v.cfg, "BASE", "BeidatAdditionalCascades") * mult
				actor.output["BeidatAdditionalCascades"] = out["BeidatAdditionalCascades"]
			}
		}
		done[key] = true
		out["PactOf"+key+"Calculated"] = 1
	}
}

// preSkillTypeFunc ports the skills' preSkillTypeFunc (Data/Skills/*.lua).
func preSkillTypeFunc(actor *ModDB, s *activeSkill) {
	if h := hooksOf(s); h != nil && h.preSkillType != nil {
		h.preSkillType(actor, s)
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
// these do not depend on them): Blight's channelled stages and Penance
// Brand of Dissipation's ticks, on skill part 2. PoB sets them on every
// active skill; here only the skill being calculated gets them (Scorching
// Ray's: scorchingRayStages).
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
	}
}

// scorchingRayStages is Scorching Ray's part of CalcPerform.lua's "maximum
// sustainable stages" block: on skill part 2 every active skill, not only
// the main one, gets the 7 stages before the buff loop, so its Fire
// Exposure debuff (effectCond ScorchingRayMaxStages) applies.
func scorchingRayStages(skills []*activeSkill) {
	for _, s := range skills {
		name := s.active.ge.Name
		if !s.disabled && s.part == 2 && (name == "Scorching Ray" || name == "Scorching Ray of Immolation") {
			stage := "Multiplier:" + strings.Join(strings.Fields(name), "")
			const maximum = 7 // CalcPerform.lua: local maximum = 7
			s.mods.NewMod(stage+"MaxStages", "BASE", maximum, "Base")
			s.mods.NewMod(stage+"StageAfterFirst", "BASE", maximum, "Base")
		}
	}
}

// initialFunc ports the skills' initialFunc (Data/Skills/*.lua), run first
// in calcs.offence.
func initialFunc(actor *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource) {
	if h := hooksOf(s); h != nil && h.initial != nil {
		h.initial(actor, s, w1, w2)
	}
}
