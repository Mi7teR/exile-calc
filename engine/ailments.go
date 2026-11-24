package engine

import (
	"maps"
	"math"
)

// The ailment, skill damage over time and combined DPS parts of
// calcs.offence (Modules/CalcOffence.lua). Ported so far: ailment chances,
// ignite, skill DoT and TotalDotDPS. Not ported yet: bleed, poison,
// impale, decay, non-damaging ailment effects, knockback, the legacy
// Perfect Agony (CritMultiplierAppliesToDegen), ailmentMode CRIT, mirages,
// culling and the reservation DPS multiplier.

// Data/Global.lua ModFlag and KeywordFlag bits for ailments.
const (
	mfAilment    = 0x800
	mfMeleeHit   = 0x1000
	mfWeaponMask = 0x2FFF0000

	kfAilment = 0x80000
	kfPoison  = 0x200000
	kfBleed   = 0x400000
	kfIgnite  = 0x800000

	stBrand  = 65
	stBanner = 88
)

// kfDot is KeywordFlag[<Type>Dot].
var kfDot = map[string]uint64{"Physical": 0x1000000, "Lightning": 0x2000000, "Cold": 0x4000000, "Fire": 0x8000000, "Chaos": 0x10000000}

// Data/Misc.lua data.misc ailment numbers.
const (
	dotDpsCap          = 35791394
	ignitePercentBase  = 0.9
	igniteDurationBase = 4
)

var (
	ailmentTypeList          = []string{"Bleed", "Poison", "Ignite", "Chill", "Freeze", "Shock", "Scorch", "Brittle", "Sap"}
	elementalAilmentTypeList = []string{"Ignite", "Chill", "Freeze", "Shock", "Scorch", "Brittle", "Sap"}
	altAilment               = map[string]bool{"Scorch": true, "Brittle": true, "Sap": true}
)

// debugAilmentCfg is a test hook: the ailment cfg of each ignite.
var debugAilmentCfg func(actor *ModDB, s *activeSkill, cfg *Cfg)

// canDeal is calcs.offence's canDeal: damage types the skill deals.
func canDeal(s *activeSkill, t string) bool {
	return !s.mods.FlagCfg(s.cfg, "DealNo"+t, "DealNoDamage")
}

// ailmentChances is the "Calculate chance to inflict secondary dots/status
// effects" part of the ailment loop for one pass.
func ailmentChances(actor *ModDB, s *activeSkill, p *hitPass) {
	list, cfg, out, enemy := s.mods, p.cfg, p.out, actor.enemy
	// Perfect Agony.
	if list.Flag("DotMultiplierIsCritMultiplier") {
		mult, ok := list.OverrideCfg(cfg, "CritMultiplier")
		if ok {
			mult -= 100
		} else {
			mult = list.SumCfg(cfg, "BASE", "CritMultiplier")
		}
		tags := []map[string]any{{"type": "Condition", "var": "CriticalStrike"}}
		switch p.label {
		case "Main Hand":
			tags = append(tags, map[string]any{"type": "Condition", "var": "MainHandAttack"})
		case "Off Hand":
			tags = append(tags, map[string]any{"type": "Condition", "var": "OffHandAttack"})
		}
		list.Add(Mod{Name: "DotMultiplier", Type: "OVERRIDE", Value: mult + enemy.SumCfg(cfg, "BASE", "SelfCritMultiplier"),
			Flags: mfAilment, Tags: tags, Source: "Perfect Agony"})
	}
	hit, attack := s.flags["hit"], s.flags["attack"]
	cond := cfg.SkillCond
	withCrit := func(crit bool) {
		m := maps.Clone(cond)
		if m == nil {
			m = map[string]bool{}
		}
		m["CriticalStrike"] = crit
		cfg.SkillCond = m
	}
	chance := func(ok bool, name, self string) float64 {
		if !ok {
			return 0
		}
		return math.Min(100, list.SumCfg(cfg, "BASE", name)+enemy.Sum("BASE", self))
	}
	withCrit(true)
	out["BleedChanceOnCrit"] = chance(attack && !list.FlagCfg(cfg, "CannotBleed"), "BleedChance", "SelfBleedChance")
	out["PoisonChanceOnCrit"] = chance(hit && !list.FlagCfg(cfg, "CannotPoison"), "PoisonChance", "SelfPoisonChance")
	out["ImpaleChanceOnCrit"] = 0
	if hit {
		out["ImpaleChanceOnCrit"] = math.Min(100, list.SumCfg(cfg, "BASE", "ImpaleChance"))
	}
	withCrit(false)
	out["BleedChanceOnHit"] = chance(attack && !list.FlagCfg(cfg, "CannotBleed"), "BleedChance", "SelfBleedChance")
	out["PoisonChanceOnHit"] = chance(hit && !list.FlagCfg(cfg, "CannotPoison"), "PoisonChance", "SelfPoisonChance")
	out["ChaosPoisonChance"] = 0
	if hit && !list.FlagCfg(cfg, "CannotPoison") {
		out["ChaosPoisonChance"] = math.Min(100, list.SumCfg(cfg, "BASE", "ChaosPoisonChance"))
	}
	for _, a := range elementalAilmentTypeList {
		ch := list.SumCfg(cfg, "BASE", "Enemy"+a+"Chance") + enemy.Sum("BASE", "Self"+a+"Chance")
		// Lua: Flag(...) and 100 or 0 + Sum(...)
		av := enemy.Sum("BASE", "Avoid"+a, "AvoidAilments", "AvoidElementalAilments")
		if enemy.Flag(a+"Immune", "ElementalAilmentImmune") {
			av = 100
		}
		avoid := 1 - math.Min(av, 100)/100
		if a == "Chill" {
			ch = 100
		}
		ch *= avoid
		if hit && !list.FlagCfg(cfg, "Cannot"+a) && avoid != 0 {
			out[a+"ChanceOnHit"] = math.Min(100, ch)
			if list.FlagCfg(cfg, "CritsDontAlways"+a) || (altAilment[a] && !list.FlagCfg(cfg, "CritAlwaysAltAilments")) {
				out[a+"ChanceOnCrit"] = out[a+"ChanceOnHit"]
			} else {
				out[a+"ChanceOnCrit"] = 100
			}
		} else {
			out[a+"ChanceOnHit"], out[a+"ChanceOnCrit"] = 0, 0
		}
	}
	out["ImpaleChance"] = math.Min(100, list.SumCfg(cfg, "BASE", "ImpaleChance"))
	// mode_effective: enemy avoidance.
	for _, a := range ailmentTypeList {
		mult := 1 - enemy.Sum("BASE", "Avoid"+a)/100
		if enemy.Flag(a + "Immune") {
			mult = 0
		}
		out[a+"ChanceOnHit"] *= mult
		out[a+"ChanceOnCrit"] *= mult
		if a == "Poison" {
			out["ChaosPoisonChance"] *= mult
		}
	}
	if actor.Flag("AilmentsOnlyFromCrit") {
		for _, a := range ailmentTypeList {
			out[a+"ChanceOnHit"] = 0
		}
	}
	if actor.Flag("AilmentsAreNeverFromCrit") {
		for _, a := range ailmentTypeList {
			if actor.Flag("AilmentsOnlyFromCrit") {
				out[a+"ChanceOnCrit"] = 0
			} else {
				out[a+"ChanceOnCrit"] = out[a+"ChanceOnHit"]
			}
		}
	}
	cfg.SkillCond = cond
}

// ailmentDamage is calcAilmentDamage: the chance of type over hits and
// crits (into out[type+"Chance"]) and the base damage weighted by it.
func ailmentDamage(out map[string]float64, typ string, critChance, hitDmg, critDmg float64) float64 {
	fromHit := out[typ+"ChanceOnHit"] * (1 - critChance/100)
	fromCrit := out[typ+"ChanceOnCrit"] * critChance / 100
	out[typ+"Chance"] = fromHit + fromCrit
	return hitDmg*fromHit/(fromHit+fromCrit) + critDmg*fromCrit/(fromHit+fromCrit)
}

// ailmentDotCfg is the igniteCfg/bleedCfg/poisonCfg of a pass: a DoT +
// ailment cfg with the pass's weapon flags and the given keywords.
func ailmentDotCfg(s *activeSkill, p *hitPass, keywords uint64) *Cfg {
	sc := s.cfg
	// checkWeapon1HFlags: Weapon1H is outside ModFlag.WeaponMask.
	flags := uint64(mfDot|mfAilment) | p.cfg.Flags&(mfWeaponMask|mfWeapon1H)
	if p.cfg.Flags&mfMelee != 0 {
		flags |= mfMeleeHit
	}
	cond := map[string]bool{}
	for k, v := range p.cfg.SkillCond {
		cond[k] = v
	}
	for k, v := range sc.SkillCond {
		if v {
			cond[k] = true
		}
	}
	return &Cfg{
		Flags: flags, KeywordFlags: p.cfg.KeywordFlags&^kfHit | keywords | kfAilment,
		SkillName: sc.SkillName, SkillPart: sc.SkillPart, SkillTypes: sc.SkillTypes,
		SummonSkillName: sc.SummonSkillName, SlotName: sc.SlotName,
		SkillDist: sc.SkillDist, HasSkillDist: sc.HasSkillDist, SkillCond: cond,
	}
}

// ailmentSourceDamage is calcAilmentSourceDamage: the min and max damage
// of type t with the ailment cfg, less what is converted away.
func ailmentSourceDamage(list *ModDB, p *hitPass, dotCfg *Cfg, t string, typeFlags int) (float64, float64) {
	q := *p
	q.cfg = dotCfg
	mn, mx := calcDamage(list, &q, t, typeFlags, false)
	m := p.conv[t].mult
	return mn * m, mx * m
}

// ignite is the "Calculate ignite chance and damage" part of the ailment
// loop for one pass.
func ignite(actor *ModDB, s *activeSkill, p *hitPass, global map[string]float64, dpsMult float64) {
	list, cfg, out, enemy := s.mods, p.cfg, p.out, actor.enemy
	if !canDeal(s, "Fire") || out["IgniteChanceOnHit"]+out["IgniteChanceOnCrit"] <= 0 {
		return
	}
	dotCfg := ailmentDotCfg(s, p, kfIgnite|kfDot["Fire"])
	critChance := out["CritChance"]
	global["IgniteChancePerHit"] = out["IgniteChanceOnHit"]*(1-critChance/100) + out["IgniteChanceOnCrit"]*critChance/100
	maxStacks := 1.0
	if s.flags["igniteCanStack"] {
		if v, ok := list.OverrideCfg(cfg, "IgniteStacks"); ok {
			maxStacks = v
		} else {
			maxStacks += list.SumCfg(cfg, "BASE", "IgniteStacks")
		}
	}
	global["IgniteStacksMax"] = maxStacks
	rateMod := (calcMod(list, cfg, "IgniteBurnFaster") + enemy.Sum("INC", "SelfIgniteBurnFaster")/100) / calcMod(list, cfg, "IgniteBurnSlower")
	durationBase := float64(igniteDurationBase)
	if v, ok := list.OverrideCfg(dotCfg, "IgniteDurationBase"); ok {
		durationBase = v
	}
	durationMod := math.Max(calcMod(list, dotCfg, "EnemyIgniteDuration", "EnemyAilmentDuration", "EnemyElementalAilmentDuration", "DamagingAilmentDuration")*
		calcMod(enemy, nil, "SelfIgniteDuration", "SelfAilmentDuration", "SelfElementalAilmentDuration"), 0)
	debuffDurationMult := 1 / math.Max(buffExpirationSlowCap, calcMod(enemy, s.cfg, "BuffExpireFaster"))
	global["IgniteDuration"] = durationBase * durationMod / rateMod * debuffDurationMult
	chance := out["IgniteChanceOnHit"]/100*(1-critChance/100) + out["IgniteChanceOnCrit"]/100*critChance/100
	stacks := out["HitChance"] / 100 * chance * dpsMult
	if !truthy(s.data["triggeredOnDeath"]) {
		speed := global["Speed"]
		if v, ok := global["HitSpeed"]; ok {
			speed = v
		}
		if cd, ok := out["Cooldown"]; ok {
			t, ok := out["HitTime"]
			if !ok {
				t = out["Time"]
			}
			stacks *= global["IgniteDuration"] / math.Max(cd, t)
		} else if speed > 0 {
			stacks *= global["IgniteDuration"] * speed
		}
	}
	if v, ok := list.OverrideCfg(nil, "IgniteStackPotentialOverride"); ok {
		global["IgniteStackPotential"] = v / maxStacks
	} else {
		global["IgniteStackPotential"] = stacks / maxStacks
	}
	rollAvg := 50.0
	if global["IgniteStackPotential"] > 1 {
		rollAvg = (stacks - (maxStacks-1)/2) / (stacks + 1) * 100
	}
	global["IgniteRollAverage"] = rollAvg

	dotMulti := func() float64 {
		if v, ok := list.OverrideCfg(dotCfg, "DotMultiplier"); ok {
			return 1 + v/100
		}
		return 1 + (list.SumCfg(dotCfg, "BASE", "DotMultiplier")+list.SumCfg(dotCfg, "BASE", "FireDotMultiplier"))/100
	}
	var hitDmg, critDmg, maxCritDmg, minHitDmg, maxHitDmg float64
	for sub := 1; sub <= 2; sub++ {
		dotCfg.SkillCond["CriticalStrike"] = sub != 1
		var totalMin, totalMax float64
		add := func(t string, ok bool, typeFlags int) {
			if !ok || !canDeal(s, t) {
				return
			}
			mn, mx := ailmentSourceDamage(list, p, dotCfg, t, typeFlags)
			out["Ignite"+t+"Min"], out["Ignite"+t+"Max"] = mn, mx
			totalMin += mn
			totalMax += mx
		}
		fire := dmgTypeFlags["Fire"]
		add("Physical", list.FlagCfg(cfg, "PhysicalCanIgnite"), fire)
		add("Lightning", list.FlagCfg(cfg, "LightningCanIgnite"), fire)
		add("Cold", list.FlagCfg(cfg, "ColdCanIgnite"), fire)
		add("Fire", !list.FlagCfg(cfg, "FireCannotIgnite"), 0)
		add("Chaos", list.FlagCfg(cfg, "ChaosCanIgnite"), fire)
		if sub == 2 {
			if actor.Flag("AilmentsAreNeverFromCrit") {
				dotCfg.SkillCond["CriticalStrike"] = false
				out["CritIgniteDotMulti"] = dotMulti()
				dotCfg.SkillCond["CriticalStrike"] = true
			} else {
				out["CritIgniteDotMulti"] = dotMulti()
			}
			critDmg = (totalMin + (totalMax-totalMin)*rollAvg/100) * out["CritIgniteDotMulti"]
			maxCritDmg = totalMax * out["CritIgniteDotMulti"]
		} else {
			out["IgniteDotMulti"] = dotMulti()
			hitDmg = (totalMin + (totalMax-totalMin)*rollAvg/100) * out["IgniteDotMulti"]
			minHitDmg = totalMin * out["IgniteDotMulti"]
			maxHitDmg = totalMax * out["IgniteDotMulti"]
		}
		out["IgniteTotalMin"], out["IgniteTotalMax"] = totalMin, totalMax
	}
	ailmentCrit := 100 * (1 - math.Pow(1-critChance/100, math.Max(1, stacks)))
	effects := out["RuthlessBlowAilmentEffect"] * out["FistOfWarDamageEffect"] * global["AilmentWarcryEffect"]
	ailmentDamage(out, "Ignite", ailmentCrit, minHitDmg, 0)
	ailmentDamage(out, "Ignite", 100, maxHitDmg, maxCritDmg)
	baseVal := ailmentDamage(out, "Ignite", ailmentCrit, hitDmg, critDmg) * ignitePercentBase * effects
	if !(baseVal > 0) {
		return
	}
	s.flags["ignite"] = true
	fireEff := func() float64 {
		r := enemyResist(actor, dotCfg, "Fire")
		inc := enemy.SumCfg(dotCfg, "INC", "DamageTaken", "DamageTakenOverTime", "FireDamageTaken", "FireDamageTakenOverTime", "ElementalDamageTaken")
		more := enemy.MoreCfg(dotCfg, "DamageTaken", "DamageTakenOverTime", "FireDamageTaken", "FireDamageTakenOverTime", "ElementalDamageTaken")
		return (1 - r/100) * (1 + inc/100) * more
	}
	var effMult float64
	if list.FlagCfg(cfg, "IgniteToChaos") {
		r := enemyResist(actor, dotCfg, "Chaos")
		inc := enemy.SumCfg(dotCfg, "INC", "DamageTaken", "DamageTakenOverTime", "ChaosDamageTaken", "ChaosDamageTakenOverTime")
		more := enemy.MoreCfg(dotCfg, "DamageTaken", "DamageTakenOverTime", "ChaosDamageTaken", "ChaosDamageTakenOverTime")
		effMult = (1 - r/100) * (1 + inc/100) * more
	} else {
		effMult = fireEff()
	}
	global["IgniteEffMult"] = effMult
	if debugAilmentCfg != nil {
		debugAilmentCfg(actor, s, dotCfg)
	}
	effectMod := calcMod(list, dotCfg, "AilmentEffect")
	active := math.Min(stacks, maxStacks)
	out["IgniteDPS"] = math.Min(baseVal*effectMod*rateMod*active*effMult, dotDpsCap)
	g1, _ := list.MaxCfg(nil, "IgniteDpsAsBurningGround")
	g2, _ := enemy.MaxCfg(nil, "IgniteDpsAsBurningGround")
	if ground := math.Max(g1, g2); ground > 0 {
		global["BurningGroundDPS"] = math.Min(baseVal*effectMod*rateMod*fireEff()*ground/100, dotDpsCap)
		global["BurningGroundFromIgnite"] = 1
	}
	global["IgniteDamage"] = out["IgniteDPS"] * global["IgniteDuration"]
	if s.flags["igniteCanStack"] {
		out["IgniteDamage"] = out["IgniteDPS"] * global["IgniteDuration"]
		out["IgniteStacksMax"] = maxStacks
		out["TotalIgniteDPS"] = out["IgniteDPS"]
	}
}

// combineAilment is combineStat's CHANCE and CHANCE_AILMENT modes over the
// two hands (a stat missing from one hand takes the other's).
func combineAilment(out map[string]float64, passes []*hitPass, stat, chanceStat string, ailmentMode bool, doubleHits bool) {
	if len(passes) < 2 {
		if v, ok := passes[0].out[stat]; ok {
			out[stat] = v
		}
		return
	}
	mh, mok := passes[0].out[stat]
	oh, ook := passes[1].out[stat]
	if !mok || !ook {
		if mok {
			out[stat] = mh
		} else if ook {
			out[stat] = oh
		}
		return
	}
	mc := passes[0].out[chanceStat] * passes[0].out["HitChance"]
	oc := passes[1].out[chanceStat] * passes[1].out["HitChance"]
	if ailmentMode {
		stacks, ok := out[stat[:len(stat)-3]+"Stacks"]
		if !ok {
			stacks = 1
		}
		smax, ok := out[stat[:len(stat)-3]+"StacksMax"]
		if !ok {
			smax = 1
		}
		k := math.Min(1, stacks/smax)
		out[stat] = math.Max(mh, oh)*k + math.Min(mh, oh)*(1-k)
		return
	}
	if doubleHits {
		out[stat] = mh*mc/10000 + oh*oc/10000
		return
	}
	out[stat] = mh*mc/(mc+oc) + oh*oc/(mc+oc)
}

// combineAilments is "Combine secondary effect stats" for attacks (the
// ported ailments only).
func combineAilments(s *activeSkill, out map[string]float64, passes []*hitPass) {
	if !s.flags["attack"] {
		return
	}
	avg := func(stat string) {
		if len(passes) < 2 {
			if v, ok := passes[0].out[stat]; ok {
				out[stat] = v
			}
			return
		}
		out[stat] = (passes[0].out[stat] + passes[1].out[stat]) / 2
	}
	double := truthy(s.data["doubleHitsWhenDualWielding"])
	avg("IgniteChance")
	combineAilment(out, passes, "IgniteDPS", "IgniteChance", true, double)
	if s.flags["igniteCanStack"] {
		combineAilment(out, passes, "IgniteDamage", "IgniteChance", false, double)
		if len(passes) < 2 {
			out["IgniteStacksMax"] = passes[0].out["IgniteStacksMax"]
			out["TotalIgniteDPS"] = passes[0].out["TotalIgniteDPS"]
		} else {
			out["IgniteStacksMax"] = passes[0].out["IgniteStacksMax"] + passes[1].out["IgniteStacksMax"]
			out["TotalIgniteDPS"] = passes[0].out["TotalIgniteDPS"] + passes[1].out["TotalIgniteDPS"]
			if !double {
				out["IgniteStacksMax"] /= 2
				out["TotalIgniteDPS"] /= 2
			}
		}
	}
	for _, a := range []string{"Bleed", "Poison", "Impale"} {
		avg(a + "Chance")
	}
}

// skillDot is "Calculate skill DOT components": the skill's own damage
// over time (TotalDotInstance, TotalDot). preDotFunc is not ported.
func skillDot(actor *ModDB, s *activeSkill, out map[string]float64, dpsMult, quantity float64) {
	list, data, enemy, sc := s.mods, s.data, actor.enemy, s.cfg
	flags := sc.Flags | mfDot
	for _, x := range []struct {
		flag uint64
		key  string
	}{{mfArea, "dotIsArea"}, {mfProjectile, "dotIsProjectile"}, {mfSpell, "dotIsSpell"}, {mfAttack, "dotIsAttack"}, {mfHit, "dotIsHit"}} {
		if !truthy(data[x.key]) {
			flags &^= x.flag
		}
	}
	base := *sc
	base.Flags, base.KeywordFlags = flags, sc.KeywordFlags&^kfHit
	taken := base
	if truthy(data["dotIsSpell"]) {
		taken.Flags &^= mfSpell
	}
	out["TotalDotInstance"] = 0
	for _, t := range dmgTypeList {
		tc := base
		tc.KeywordFlags |= kfDot[t]
		baseVal := 0.0
		if canDeal(s, t) {
			baseVal, _ = num(data[t+"Dot"])
		}
		if !(baseVal > 0 || out[t+"Dot"] > 0) {
			continue
		}
		s.flags["dot"] = true
		names := []string{"DamageTaken", "DamageTakenOverTime", t + "DamageTaken", t + "DamageTakenOverTime"}
		inc := enemy.SumCfg(&taken, "INC", names...)
		more := enemy.MoreCfg(&taken, names...)
		if isElemental[t] {
			inc += enemy.SumCfg(&taken, "INC", "ElementalDamageTaken")
			more *= enemy.MoreCfg(&taken, "ElementalDamageTaken")
		}
		var resist float64
		if t == "Physical" {
			resist = math.Max(0, math.Min(enemy.Sum("BASE", "PhysicalDamageReduction"), enemyPhysicalDamageReductionCap))
		} else {
			resist = enemyResist(actor, &tc, t)
		}
		effMult := (1 - resist/100) * (1 + inc/100) * more
		out[t+"DotEffMult"] = effMult
		dmgNames := []string{"Damage", t + "Damage"}
		if isElemental[t] {
			dmgNames = append(dmgNames, "ElementalDamage")
		}
		dinc := list.SumCfg(&tc, "INC", dmgNames...)
		if list.Flag("dotIsHeraldOfAsh") {
			dinc = math.Max(dinc-list.SumCfg(sc, "INC", dmgNames...), 0)
		}
		dmore := list.MoreCfg(&tc, dmgNames...)
		mult, ok := list.OverrideCfg(&tc, "DotMultiplier")
		if !ok {
			mult = list.SumCfg(&tc, "BASE", "DotMultiplier") + list.SumCfg(&tc, "BASE", t+"DotMultiplier")
		}
		aura := 1.0
		if s.types[stAura] && !s.types[stRemoteMined] && !s.types[stBanner] {
			aura = calcMod(list, &tc, "AuraEffect")
		}
		total := baseVal * (1 + dinc/100) * dmore * (1 + mult/100) * aura * effMult
		if out[t+"Dot"] == 0 {
			out[t+"Dot"] = total
			out["TotalDotInstance"] = math.Min(out["TotalDotInstance"]+total, dotDpsCap)
		} else {
			out["TotalDotInstance"] = math.Min(out["TotalDotInstance"]+total+out[t+"Dot"], dotDpsCap)
		}
	}
	switch {
	case list.Flag("DotCanStack"):
		s.flags["DotCanStack"] = true
		speed := out["Speed"]
		if base.KeywordFlags&kfMine != 0 {
			speed = out["MineLayingSpeed"]
		} else if base.KeywordFlags&kfTrap != 0 {
			speed = out["TrapThrowingSpeed"]
		}
		out["TotalDot"] = math.Min(out["TotalDotInstance"]*speed*out["Duration"]*dpsMult*quantity, dotDpsCap)
		out["TotalDotCalcSection"] = out["TotalDot"]
	case list.Flag("dotIsBurningGround"):
		out["TotalDot"] = 0
		out["TotalDotCalcSection"] = out["TotalDotInstance"]
		if v, ok := out["BurningGroundDPS"]; !ok || v < out["TotalDotInstance"] {
			out["BurningGroundDPS"] = math.Max(v, out["TotalDotInstance"])
			out["BurningGroundFromIgnite"] = 0
		}
	case list.Flag("dotIsCausticGround"):
		out["TotalDot"] = 0
		out["TotalDotCalcSection"] = out["TotalDotInstance"]
		if v, ok := out["CausticGroundDPS"]; !ok || v < out["TotalDotInstance"] {
			out["CausticGroundDPS"] = math.Max(v, out["TotalDotInstance"])
			out["CausticGroundFromPoison"] = 0
		}
	case list.Flag("dotIsCorruptingBlood"):
		out["TotalDot"] = 0
		out["TotalDotCalcSection"] = out["TotalDotInstance"]
		if v, ok := out["CorruptingBloodDPS"]; !ok || v < out["TotalDotInstance"] {
			out["CorruptingBloodDPS"] = math.Max(v, out["TotalDotInstance"])
		}
	default:
		if list.Flag("DotCanStackAsTotems") && s.flags["totem"] {
			s.flags["DotCanStack"] = true
		}
		brands := 1.0
		if s.types[stBrand] && !truthy(data["countsAttachedBrandsInDamage"]) {
			if v, ok := out["AttachedBrandCount"]; ok {
				brands = v
			}
		}
		out["TotalDot"] = out["TotalDotInstance"]
		if brands > 1 {
			out["TotalDot"] = math.Min(out["TotalDotInstance"]*brands, dotDpsCap)
		}
		out["TotalDotCalcSection"] = out["TotalDot"]
	}
}

// cullAndReservation is the culling and reservation DPS part of the damage
// pass (CullMultiplier, ReservationDpsMultiplier).
func cullAndReservation(actor *ModDB, s *activeSkill, p *hitPass, global map[string]float64, dpsMult float64) {
	list, cfg, out := s.mods, p.cfg, p.out
	speed := global["Speed"]
	if v, ok := global["HitSpeed"]; ok {
		speed = v
	}
	hitRate := out["HitChance"] / 100 * speed * dpsMult
	critical, _ := list.MaxCfg(cfg, "CriticalCullPercent")
	if critical > 0 {
		critical = math.Min(critical, critical*(1-math.Pow(1-out["CritChance"]/100, hitRate)))
	}
	regular, _ := list.MaxCfg(cfg, "CullPercent")
	global["CullPercent"] = math.Max(critical, regular)
	global["CullMultiplier"] = 100 / (100 - global["CullPercent"])
	global["ReservationDpsMultiplier"] = 100 / (100 - actor.enemy.Sum("BASE", "LifeReservationPercent"))
}

// combinedDPS is "Calculate combined DPS estimate, including DoTs" without
// mirages.
func combinedDPS(s *activeSkill, out map[string]float64) {
	baseDPS := out["TotalDPS"]
	showAverage := truthy(s.data["showAverage"])
	if showAverage {
		baseDPS = out["AverageDamage"]
	}
	out["CombinedDPS"], out["CombinedAvg"] = baseDPS, baseDPS
	if s.flags["dot"] {
		out["WithDotDPS"] = baseDPS + out["TotalDot"]
	}
	if showAverage {
		out["WithPoisonDPS"] = baseDPS + out["TotalPoisonAverageDamage"]
	} else {
		out["WithPoisonDPS"] = baseDPS + out["TotalPoisonDPS"]
	}
	out["WithIgniteDPS"] = baseDPS
	if s.flags["ignite"] {
		switch {
		case s.flags["igniteCanStack"]:
			if showAverage {
				out["CombinedAvg"] = out["CombinedDPS"] + out["IgniteDamage"]
			} else {
				out["WithIgniteDPS"] = baseDPS + out["TotalIgniteDPS"]
			}
		case showAverage:
			out["WithIgniteDPS"] = baseDPS + out["IgniteDamage"]
			out["CombinedAvg"] += out["IgniteDamage"]
		default:
			out["WithIgniteDPS"] = baseDPS + out["IgniteDPS"]
		}
	}
	out["WithBleedDPS"] = baseDPS
	ignite, ok := out["TotalIgniteDPS"]
	if !ok {
		ignite = out["IgniteDPS"]
	}
	total := out["TotalDot"] + out["TotalPoisonDPS"] + out["CausticGroundDPS"] + ignite + out["BurningGroundDPS"] +
		out["BleedDPS"] + out["CorruptingBloodDPS"] + out["DecayDPS"]
	out["TotalDotDPS"] = math.Min(total, dotDpsCap)
	if !showAverage {
		out["CombinedDPS"] += out["TotalDotDPS"]
	}
	cull := math.Max(1, out["CullMultiplier"])
	resMult, ok := out["ReservationDpsMultiplier"]
	if !ok {
		resMult = 1
	}
	out["CullingDPS"] = out["CombinedDPS"] * (cull - 1)
	out["ReservationDPS"] = out["CombinedDPS"] * (resMult - 1)
	out["CombinedDPS"] *= cull * resMult
}
