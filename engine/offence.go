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
// the spells it empowers. Pact of Beidat's extra projectiles, chains and
// cascades are not ported.
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

// preSkillTypeFunc ports the skills' preSkillTypeFunc (Data/Skills/*.lua).
func preSkillTypeFunc(s *activeSkill) {
	if h := hooksOf(s); h != nil && h.preSkillType != nil {
		h.preSkillType(s)
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

// initialFunc ports the skills' initialFunc (Data/Skills/*.lua), run first
// in calcs.offence.
func initialFunc(actor *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource) {
	if h := hooksOf(s); h != nil && h.initial != nil {
		h.initial(actor, s, w1, w2)
	}
}
