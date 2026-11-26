package engine

import (
	"math"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// leechRateBase is data.misc.LeechRateBase (Data.lua:260).
const leechRateBase = 0.02

// The PvP damage scaling constants (Data.lua:299-302).
const (
	pvpElemental1    = 0.55
	pvpElemental2    = 150
	pvpNonElemental1 = 0.57
	pvpNonElemental2 = 90
	pvpTvalueMax     = 2147483647 // CalcOffence.lua:3931
	pvpTvalueScale   = 1000       // multiplierPvpTvalueOverride is in milliseconds
)

// leechPools are the leech resources in CalcOffence's order.
var leechPools = [...]string{"Life", "EnergyShield", "Mana"}

// leechCaps is the "Leech caps" block of calcs.defence
// (CalcDefence.lua:1243-1275).
func leechCaps(db *ModDB) {
	out := db.output
	out["MaxLifeLeechInstance"] = out["Life"] * calcVal(db, "MaxLifeLeechInstance") / 100
	pct := calcVal(db, "MaxLifeLeechRate")
	if db.Flag("MaximumLifeLeechIsEqualToParent") && db.owner != nil {
		pct = db.owner.output["MaxLifeLeechRatePercent"]
	} else if db.Flag("MaximumLifeLeechIsEqualToPartyMember") && db.partyMembers != nil {
		pct = db.partyMembers.output["MaxLifeLeechRatePercent"]
	}
	out["MaxLifeLeechRatePercent"] = pct
	out["MaxLifeLeechRate"] = out["Life"] * pct / 100
	out["MaxEnergyShieldLeechInstance"] = out["EnergyShield"] * calcVal(db, "MaxEnergyShieldLeechInstance") / 100
	out["MaxEnergyShieldLeechRate"] = out["EnergyShield"] * calcVal(db, "MaxEnergyShieldLeechRate") / 100
	out["MaxManaLeechInstance"] = out["Mana"] * calcVal(db, "MaxManaLeechInstance") / 100
	out["MaxManaLeechRate"] = out["Mana"] * calcVal(db, "MaxManaLeechRate") / 100
}

// leechPass is the leech of one crit/non-crit pass of a hit pass
// (CalcOffence.lua:3438-3443): lifeLeechTotal and its siblings, and
// whether the skill can leech each resource.
type leechPass struct {
	life, es, mana       float64
	noLife, noES, noMana bool
}

func newLeechPass(list, enemy *ModDB, cfg *Cfg) *leechPass {
	return &leechPass{
		noLife: list.FlagCfg(cfg, "CannotLeechLife") || enemy.Flag("CannotLeechLifeFromSelf") ||
			list.FlagCfg(cfg, "CannotGainLife"),
		noES: list.FlagCfg(cfg, "CannotLeechEnergyShield") || enemy.Flag("CannotLeechEnergyShieldFromSelf") ||
			list.FlagCfg(cfg, "CannotGainEnergyShield"),
		noMana: list.FlagCfg(cfg, "CannotLeechMana") || enemy.Flag("CannotLeechManaFromSelf") ||
			list.FlagCfg(cfg, "CannotGainMana"),
	}
}

// addType is the "Beginning of Leech Calculation for this DamageType"
// block (CalcOffence.lua:3657-3684): the leech of avg, the type's hit.
func (l *leechPass) addType(s *activeSkill, enemy *ModDB, cfg *Cfg, t string, avg float64) {
	list := s.mods
	var life, es, mana float64
	ele := ""
	if t == "Lightning" || t == "Cold" || t == "Fire" {
		ele = "Elemental"
	}
	switch {
	case s.flags["mine"] || s.flags["trap"] || s.flags["totem"]:
		life = list.SumCfg(cfg, "BASE", "DamageLifeLeechToPlayer")
	case list.Flag("LifeLeechBasedOnChaosDamage"):
		if t == "Chaos" {
			life = list.SumCfg(cfg, "BASE", "DamageLeech", "DamageLifeLeech", "PhysicalDamageLifeLeech",
				"LightningDamageLifeLeech", "ColdDamageLifeLeech", "FireDamageLifeLeech", "ChaosDamageLifeLeech",
				"ElementalDamageLifeLeech") + enemy.SumCfg(cfg, "BASE", "SelfDamageLifeLeech")/100
		}
	default:
		life = list.SumCfg(cfg, "BASE", leechNames("DamageLeech", "DamageLifeLeech", t+"DamageLifeLeech",
			ele, "DamageLifeLeech")...) + enemy.SumCfg(cfg, "BASE", "SelfDamageLifeLeech")/100
	}
	if !s.flags["mine"] && !s.flags["trap"] && !s.flags["totem"] {
		es = list.SumCfg(cfg, "BASE", leechNames("", "DamageEnergyShieldLeech", t+"DamageEnergyShieldLeech",
			ele, "DamageEnergyShieldLeech")...) + enemy.SumCfg(cfg, "BASE", "SelfDamageEnergyShieldLeech")/100
		mana = list.SumCfg(cfg, "BASE", leechNames("DamageLeech", "DamageManaLeech", t+"DamageManaLeech",
			ele, "DamageManaLeech")...) + enemy.SumCfg(cfg, "BASE", "SelfDamageManaLeech")/100
	}
	if life > 0 && !l.noLife {
		l.life += avg * life / 100
	}
	if mana > 0 && !l.noMana {
		l.mana += avg * mana / 100
	}
	if es > 0 && !l.noES {
		l.es += avg * es / 100
	}
}

// leechNames is the name list of a leech Sum: first (when set), the
// generic and the type's name, and the elemental one for an element
// (Lua's `isElemental[damageType] and ... or nil` drops it otherwise).
func leechNames(first, generic, typed, ele, suffix string) []string {
	names := make([]string, 0, 4)
	if first != "" {
		names = append(names, first)
	}
	names = append(names, generic, typed)
	if ele != "" {
		names = append(names, ele+suffix)
	}
	return names
}

// finish is CalcOffence.lua:3703-3733, the end of a crit (pass 1) or
// non-crit (pass 2) pass: per-use leech, the per-instance caps and the
// pass's share into out.
func (l *leechPass) finish(s *activeSkill, actor *ModDB, out map[string]float64, pass int, ghostReaver bool,
	ghostLeech *float64,
) {
	global := actor.output
	if v, ok := num(s.data.val("lifeLeechPerUse")); ok && !l.noLife {
		l.life += v
	}
	if v, ok := num(s.data.val("manaLeechPerUse")); ok {
		l.mana += v
	}
	if ghostReaver {
		l.life = math.Min(l.life, global["MaxEnergyShieldLeechInstance"])
	} else {
		l.life = math.Min(l.life, global["MaxLifeLeechInstance"])
	}
	l.es = math.Min(l.es, global["MaxEnergyShieldLeechInstance"])
	l.mana = math.Min(l.mana, global["MaxManaLeechInstance"])
	if ghostReaver && l.noES {
		l.life = 0
	}
	portion := 1 - out["CritChance"]/100
	if pass == 1 {
		portion = out["CritChance"] / 100
	}
	if ghostReaver {
		*ghostLeech += float64(l.life * portion)
	} else {
		out["LifeLeech"] += float64(l.life * portion)
	}
	out["EnergyShieldLeech"] += float64(l.es * portion)
	out["ManaLeech"] += float64(l.mana * portion)
}

// damageTypeConditions is CalcOffence.lua:3736-3763 after a hit pass:
// Elemental Equilibrium's enemy conditions from the hit, then the
// <Type>HasDamage and <Type>IsHighestDamageType conditions.
func damageTypeConditions(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, enemy := s.mods, actor.enemy
	if list.FlagCfg(s.cfg, "ElementalEquilibrium") && !configFlag(actor, "EEIgnoreHitDamage") &&
		out["FireHitAverage"]+out["ColdHitAverage"]+out["LightningHitAverage"] > 0 {
		for _, t := range []string{"Fire", "Cold", "Lightning"} {
			enemy.conditions["HitBy"+t+"Damage"] = out[t+"HitAverage"] > 0
		}
	}
	highest := "Physical"
	for _, t := range dmgTypeList {
		if out[t+"HitAverage"] > 0 {
			list.NewMod("Condition:"+t+"HasDamage", "FLAG", 1, "Config")
			if out[t+"HitAverage"] > out[highest+"HitAverage"] {
				highest = t
			}
		}
	}
	if !list.Flag("IsHighestDamageTypeOVERRIDE") {
		list.NewMod("Condition:"+highest+"IsHighestDamageType", "FLAG", 1, "Config")
	}
}

// leechAndGain is CalcOffence.lua:3765-3830 for a hit pass: instant
// leech, leech instances, life/ES/mana on hit and on kill and the enemy's
// regeneration. hitRate is the pass's hit rate (3338).
func leechAndGain(actor *ModDB, s *activeSkill, p *hitPass, ghostLeech, hitRate float64) {
	list, cfg, out, enemy := s.mods, p.cfg, p.out, actor.enemy
	global := actor.output
	instant := func(name string) float64 {
		return math.Max(math.Min(list.SumCfg(cfg, "BASE", name), 100), 0) / 100
	}
	out["LifeLeechInstantProportion"] = instant("InstantLifeLeech")
	if prop := out["LifeLeechInstantProportion"]; prop > 0 {
		out["LifeLeechInstant"] = out["LifeLeech"] * prop
		out["LifeLeech"] *= 1 - prop
	}
	if list.Flag("GhostReaver") && ghostLeech > 0 {
		out["EnergyShieldLeech"] += ghostLeech
		out["LifeLeech"], out["LifeLeechInstant"] = 0, 0
	}
	for _, r := range leechPools[1:] {
		out[r+"LeechInstantProportion"] = instant("Instant" + r + "Leech")
		if prop := out[r+"LeechInstantProportion"]; prop > 0 {
			out[r+"LeechInstant"] = out[r+"Leech"] * prop
			out[r+"Leech"] *= 1 - prop
		}
	}
	for _, r := range leechPools {
		// getLeechInstances(amount, total).
		dur, inst := 0.0, 0.0
		if total := global[r]; total != 0 {
			dur = out[r+"Leech"] / total / leechRateBase
			inst = dur * hitRate
		}
		out[r+"LeechDuration"], out[r+"LeechInstances"] = dur, inst
		out[r+"LeechInstantRate"] = out[r+"LeechInstant"] * hitRate
	}

	// Calculate gain on hit / on kill.
	noGain := s.flags["mine"] || s.flags["trap"] || s.flags["totem"]
	noLife := list.FlagCfg(cfg, "CannotGainLife") || list.FlagCfg(cfg, "CannotRecoverLifeOutsideLeech")
	noES, noMana := list.FlagCfg(cfg, "CannotGainEnergyShield"), list.FlagCfg(cfg, "CannotGainMana")
	for _, x := range []struct {
		r  string
		no bool
	}{{"Life", noLife}, {"EnergyShield", noES}, {"Mana", noMana}} {
		onHit, onKill := 0.0, 0.0
		if !noGain && !x.no {
			onHit = list.SumCfg(cfg, "BASE", x.r+"OnHit") + enemy.SumCfg(cfg, "BASE", "Self"+x.r+"OnHit")
			onKill = math.Floor(list.SumCfg(cfg, "BASE", x.r+"OnKill"))
		}
		out[x.r+"OnHit"], out[x.r+"OnKill"] = onHit, onKill
		out[x.r+"OnHitRate"] = onHit * hitRate
	}

	// Enemy Regeneration Rate.
	out["EnemyLifeRegen"] = enemy.SumCfg(cfg, "INC", "LifeRegen")
	out["EnemyManaRegen"] = enemy.SumCfg(cfg, "INC", "ManaRegen")
	out["EnemyEnergyShieldRegen"] = enemy.SumCfg(cfg, "INC", "EnergyShieldRegen")
}

// pvpDamage is "Calculate PvP values" (CalcOffence.lua:3906-3960) for a
// hit pass with the PvpScaling config option. speed is the skill's hit
// rate (globalOutput.HitSpeed or Speed), dpsMult skillData.dpsMultiplier.
func pvpDamage(actor *ModDB, s *activeSkill, p *hitPass, speed, dpsMult, totalHitMin, totalHitMax float64) {
	if !configFlag(actor, "PvpScaling") {
		return
	}
	list, cfg, out := s.mods, p.cfg, p.out
	actionSpeed := actor.output["ActionSpeedMod"]
	var tv float64
	if in, ok := configInput(actor.theBuild().ActiveConfig(), "multiplierPvpTvalueOverride"); ok &&
		in.Kind == pobcode.KindNumber {
		tv = in.Number / pvpTvalueScale
	} else {
		switch {
		case s.data.cooldown.set:
			tv = s.data.cooldown.v
		case s.flags["mine"]:
			tv = outOr(out, "MineLayingTime", 1) / actionSpeed
		case s.flags["trap"]:
			tv = outOr(out, "TrapThrowingTime", 1) / actionSpeed
		default:
			tv = 1 / (speed / actionSpeed) * list.MoreCfg(cfg, "PvpTvalueMultiplier")
		}
		if tv > pvpTvalueMax {
			tv = 1
		}
	}
	mult := list.MoreCfg(cfg, "PvpDamageMultiplier")
	nonEle := (out["PhysicalHitAverage"] + out["ChaosHitAverage"]) / (totalHitMin + totalHitMax) * 2
	ele := 1 - nonEle
	portionNonEle := math.Pow(out["AverageHit"]/tv/pvpNonElemental2, pvpNonElemental1) * tv * pvpNonElemental2 * nonEle
	portionEle := math.Pow(out["AverageHit"]/tv/pvpElemental2, pvpElemental1) * tv * pvpElemental2 * ele
	out["PvpAverageHit"] = (float64(portionNonEle) + float64(portionEle)) * mult
	out["PvpAverageDamage"] = out["PvpAverageHit"] * out["HitChance"] / 100
	out["PvpTotalDPS"] = out["PvpAverageDamage"] * speed * dpsMult
	// "fix for these being nan".
	for _, k := range []string{"PvpAverageHit", "PvpAverageDamage", "PvpTotalDPS"} {
		if math.IsNaN(out[k]) {
			out[k] = 0
		}
	}
}

// outOr is `output[name] or def`.
func outOr(out map[string]float64, name string, def float64) float64 {
	if v, ok := out[name]; ok {
		return v
	}
	return def
}

// leechRates is "Calculate leech rates" (CalcOffence.lua:4144-4185) on
// the skill's combined output.
func leechRates(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, cfg, global := s.mods, s.cfg, actor.output
	for _, r := range leechPools {
		out[r+"LeechInstanceRate"] = global[r] * leechRateBase * calcMod(list, cfg, r+"LeechRate")
		out[r+"LeechRate"] = out[r+"LeechInstances"] * out[r+"LeechInstanceRate"]
		out[r+"LeechPerHit"] = out[r+"LeechInstanceRate"]
	}
	// On full life, Immortal Ambition treats life leech as energy shield
	// leech.
	if list.Flag("ImmortalAmbition") {
		out["EnergyShieldLeechRate"] += out["LifeLeechRate"]
		out["EnergyShieldLeechPerHit"] += out["LifeLeechPerHit"]
		out["LifeLeechRate"], out["LifeLeechPerHit"] = 0, 0
	}
	if list.Flag("UnaffectedByNonInstantLifeLeech") {
		out["LifeLeechRate"], out["LifeLeechPerHit"], out["LifeLeechInstances"] = 0, 0, 0
	}
	for _, r := range leechPools {
		maxRate, recovery := global["Max"+r+"LeechRate"], global[r+"RecoveryRateMod"]
		out[r+"LeechRate"] = out[r+"LeechInstantRate"] + float64(math.Min(out[r+"LeechRate"], maxRate)*recovery)
		out[r+"LeechPerHit"] = out[r+"LeechInstant"] +
			float64(math.Min(out[r+"LeechPerHit"], maxRate)*out[r+"LeechDuration"]*recovery)
	}
	s.flags["leechLife"] = out["LifeLeechRate"] > 0
	s.flags["leechES"] = out["EnergyShieldLeechRate"] > 0
	s.flags["leechMana"] = out["ManaLeechRate"] > 0
	for _, r := range leechPools {
		if truthy(s.data.val("showAverage")) {
			out[r+"LeechGainPerHit"] = out[r+"LeechPerHit"] + out[r+"OnHit"]
		} else {
			out[r+"LeechGainRate"] = out[r+"LeechRate"] + out[r+"OnHitRate"]
		}
	}
}

// configFlag is env.configInput[name] for a check box.
func configFlag(actor *ModDB, name string) bool {
	b := actor.theBuild()
	if b == nil {
		return false
	}
	in, ok := configInput(b.ActiveConfig(), name)
	return ok && in.Kind == pobcode.KindBool && in.Bool
}
