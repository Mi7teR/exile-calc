package engine

import (
	"maps"
	"math"
	"strings"
)

// nonDamagingAilmentInfo is Data.lua data.nonDamagingAilment: the associated
// damage type, base effect, maximum and duration of each non-damaging
// ailment.
type nonDamagingAilmentInfo struct {
	associated string
	def, max   float64
	duration   float64
}

// nonDamagingAilmentData returns data.nonDamagingAilment (Data.lua:424-431).
func nonDamagingAilmentData(name string) nonDamagingAilmentInfo {
	switch name {
	case "Chill":
		return nonDamagingAilmentInfo{"Cold", 10, 30, 2}
	case "Freeze":
		return nonDamagingAilmentInfo{associated: "Cold", max: 3}
	case "Shock":
		return nonDamagingAilmentInfo{"Lightning", 15, 50, 2}
	case "Scorch":
		return nonDamagingAilmentInfo{"Fire", 10, 30, 4}
	case "Brittle":
		return nonDamagingAilmentInfo{"Cold", 2, 6, 4}
	case "Sap":
		return nonDamagingAilmentInfo{"Lightning", 6, 20, 4}
	}
	return nonDamagingAilmentInfo{}
}

// knockbackChance is output.KnockbackChanceOnHit/OnCrit
// (CalcOffence.lua:4256-4260, 4307-4311).
func knockbackChance(s *activeSkill, cfg *Cfg) float64 {
	if !s.flags["hit"] || s.mods.FlagCfg(cfg, "CannotKnockback") {
		return 0
	}
	return s.mods.SumCfg(cfg, "BASE", "EnemyKnockbackChance")
}

// averageSourceDamage is calcAverageSourceDamage (CalcOffence.lua:4359-4375):
// the pass's hit and crit average of the damage types that can inflict
// the ailment.
func averageSourceDamage(s *activeSkill, cfg *Cfg, out map[string]float64, ailment string) (float64, float64) {
	assoc := nonDamagingAilmentData(ailment).associated
	if assoc == "" {
		assoc = map[string]string{"Bleed": "Physical", "Poison": "Chaos", "Ignite": "Fire"}[ailment]
	}
	var hit, crit float64
	for _, t := range dmgTypeList {
		if !canDeal(s, t) {
			continue
		}
		can := s.mods.FlagCfg(cfg, t+"Can"+ailment)
		if t == assoc {
			can = !s.mods.FlagCfg(cfg, t+"Cannot"+ailment)
		}
		if can {
			hit += out[t+"HitAverage"]
			crit += out[t+"CritAverage"]
		}
	}
	return hit, crit
}

// nonDamagingAilments is calcs.offence's "Calculate non-damaging ailments
// effect and duration modifiers", knockback and stun part of a pass
// (CalcOffence.lua:5376-5605): chilling areas, freeze's duration
// modifier, each non-damaging ailment's duration and effect modifier, the
// knockback chance and distance and the enemy's stun threshold and
// duration. The ailment threshold tables only feed the breakdown.
func nonDamagingAilments(actor *ModDB, s *activeSkill, p *hitPass) {
	list, cfg, out, enemy := s.mods, p.cfg, p.out, actor.enemy
	// Lua: Chilling Area like floor Frost has a different effect.
	if s.types[stChillingArea] || s.types[stNonHitChill] {
		s.flags["chill"] = true
		inc := list.SumCfg(cfg, "INC", "EnemyChillEffect")
		more := list.MoreCfg(cfg, "EnemyChillEffect")
		out["ChillEffectMod"] = (1 + inc/100) * more * calcMod(enemy, nil, "SelfChillEffect")
		durNames := []string{"EnemyChillDuration", "EnemyAilmentDuration", "EnemyElementalAilmentDuration"}
		selfNames := []string{"SelfChillDuration", "SelfAilmentDuration", "SelfElementalAilmentDuration"}
		incDur := list.SumCfg(cfg, "INC", durNames...) + enemy.Sum("INC", selfNames...)
		moreDur := list.MoreCfg(cfg, durNames...) * enemy.More(selfNames...)
		out["ChillDurationMod"] = (1 + incDur/100) * moreDur
		chill := nonDamagingAilmentData("Chill")
		chillMax, ok := list.Override("ChillMax")
		if !ok {
			chillMax = chill.max + list.Sum("BASE", "ChillMax")
		}
		out["ChillSourceEffect"] = math.Min(chillMax, math.Floor(chill.def*out["ChillEffectMod"]))
	}
	// Crit ailments are done differently for Freeze.
	if out["FreezeChanceOnHit"]+out["FreezeChanceOnCrit"] > 0 {
		hit, crit := averageSourceDamage(s, cfg, out, "Freeze")
		if ailmentDamage(out, "Freeze", out["CritChance"], hit, crit)*list.MoreCfg(cfg, "FreezeAsThoughDealing") > 0 {
			s.flags["freeze"] = true
			inc := list.SumCfg(
				cfg,
				"INC",
				"EnemyFreezeDuration",
				"EnemyAilmentDuration",
				"EnemyElementalAilmentDuration",
			)
			self := enemy.Sum(
				"INC",
				"SelfFreezeDuration",
				"SelfElementalAilmentDuration",
				"SelfAilmentDuration",
				"HoarfrostFreezeDuration",
			)
			out["FreezeDurationMod"] = 1 + inc/100 + self/100
		}
	}
	ddm := debuffDurationMult(actor, s.cfg)
	// pairs(ailments): the order does not change the results.
	for _, a := range []string{"Chill", "Shock", "Scorch", "Brittle", "Sap"} {
		if out[a+"ChanceOnHit"]+out[a+"ChanceOnCrit"] <= 0 {
			continue
		}
		// Sets the crit strike condition to match ailment mode; it stays
		// set for the rest of the pass, as in PoB.
		cond := maps.Clone(cfg.SkillCond)
		if cond == nil {
			cond = map[string]bool{}
		}
		cond["CriticalStrike"] = ailmentModeCrit(actor) && !actor.Flag("AilmentsAreNeverFromCrit")
		cfg.SkillCond = cond
		hit, crit := averageSourceDamage(s, cfg, out, a)
		// damage > 0: false for the NaN of a chance that is 0 once crit
		// chance weighs it (0 / 0 in calcAilmentDamage).
		if !(ailmentDamage(out, a, out["CritChance"], hit, crit)*list.MoreCfg(cfg, a+"AsThoughDealing") > 0) {
			continue
		}
		s.flags[strings.ToLower(a)] = true
		durNames := []string{"Enemy" + a + "Duration", "EnemyElementalAilmentDuration", "EnemyAilmentDuration"}
		selfNames := []string{"Self" + a + "Duration", "SelfElementalAilmentDuration", "SelfAilmentDuration"}
		incDur := list.SumCfg(cfg, "INC", durNames...) + enemy.Sum("INC", selfNames...)
		moreDur := list.MoreCfg(cfg, durNames...) * enemy.More(selfNames...)
		out[a+"Duration"] = nonDamagingAilmentData(a).duration * (1 + incDur/100) * moreDur * ddm
		out[a+"EffectMod"] = calcMod(list, cfg, "Enemy"+a+"Effect") * calcMod(enemy, nil, "Self"+a+"Effect")
	}

	// Calculate knockback chance/distance.
	crit := out["CritChance"]
	out["KnockbackChance"] = math.Min(
		100,
		float64(out["KnockbackChanceOnHit"]*(1-crit/100))+out["KnockbackChanceOnCrit"]*crit/100+
			enemy.Sum("BASE", "SelfKnockbackChance"),
	)
	if out["KnockbackChance"] > 0 {
		out["KnockbackDistance"] = round(4*calcMod(list, cfg, "EnemyKnockbackDistance"), 0)
	}

	// Calculate enemy stun modifiers.
	red := -list.SumCfg(cfg, "INC", "EnemyStunThreshold")
	if red > 75 {
		out["EnemyStunThresholdMod"] = 1 - (75+(red-75)*25/(red-50))/100
	} else {
		out["EnemyStunThresholdMod"] = 1 - red/100
	}
	base := 0.35
	if v, ok := num(s.data.val("baseStunDuration")); ok {
		base = v
	}
	incDur := list.SumCfg(cfg, "INC", "EnemyStunDuration")
	incDurCrit := list.SumCfg(cfg, "INC", "EnemyStunDurationOnCrit")
	moreDur := list.MoreCfg(cfg, "EnemyStunDuration")
	double := math.Min(
		list.SumCfg(
			cfg,
			"BASE",
			"DoubleEnemyStunDurationChance",
		)+enemy.SumCfg(
			cfg,
			"BASE",
			"SelfDoubleStunDurationChance",
		),
		100,
	)
	incRecov := enemy.Sum("INC", "StunRecovery")
	minimum := base * moreDur / (1 + incRecov/100)
	out["EnemyStunDuration"] = minimum
	switch {
	case incDurCrit != 0 && crit != 0 && crit == 100:
		out["EnemyStunDuration"] = minimum * (1 + (incDur+incDurCrit)/100)
	case incDurCrit != 0 && crit != 0:
		out["EnemyStunDuration"] *= 1 + (incDur+incDurCrit*crit/100)/100
	default:
		out["EnemyStunDuration"] = minimum * (1 + incDur/100)
	}
	if double != 0 {
		out["EnemyStunDuration"] *= 1 + double/100
	}
}
