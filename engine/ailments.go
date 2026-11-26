package engine

import (
	"maps"
	"math"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// The ailment, skill damage over time and combined DPS parts of
// calcs.offence (Modules/CalcOffence.lua): ailment chances, bleed, poison,
// ignite, impale, skill DoT, culling and combined DPS. Not ported yet:
// decay, non-damaging ailment effects, knockback, preDotFunc, the legacy
// Perfect Agony (CritMultiplierAppliesToDegen) and ailmentMode CRIT.

// Data/Global.lua ModFlag and KeywordFlag bits for ailments.
const (
	mfAilment    = 0x800
	mfMeleeHit   = 0x1000
	mfWeaponMask = 0x2FFF0000

	kfAilment = 0x80000
	kfPoison  = 0x200000
	kfBleed   = 0x400000
	kfIgnite  = 0x800000
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

// debugCombine is a test hook for combineAilment.
var debugCombine func(stat string, mh, oh, mc, oc float64, double bool)

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
	// CalcOffence.lua:4250-4254: impale only with env.mode_effective.
	out["ImpaleChanceOnCrit"] = 0
	if hit && modeEffective(actor) {
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
	// CalcOffence.lua:4312: impale, and 4322-4331: the enemy's ailment
	// avoidance, only with env.mode_effective.
	avoided := ailmentTypeList
	if !modeEffective(actor) {
		out["ImpaleChance"] = 0
		avoided = nil
	}
	for _, a := range avoided {
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

// critDotMulti is the "if breakdown" part of the ailment sections that
// replaces output.<Type>DotMulti with the mean of the non-crit and crit DoT
// multipliers, weighted by the instances each creates (CalcOffence.lua:4645-4652
// bleed, 4944-4951 poison, 5340-5347 ignite). It only changes the displayed
// stat: the damage has already been computed. hitCrit is globalOutput.CritChance,
// ailmentCrit the chance the crit share uses (ailmentCritChance for bleed and
// ignite, globalOutput.CritChance for poison).
func critDotMulti(actor *ModDB, out map[string]float64, typ string, hitCrit, ailmentCrit float64) {
	if !actor.hasBreakdown() {
		return
	}
	critMulti, ok := out["Crit"+typ+"DotMulti"]
	if !ok || critMulti == out[typ+"DotMulti"] {
		return
	}
	fromHit := out[typ+"ChanceOnHit"] / 100 * (1 - hitCrit/100)   //nolint:mnd // percent
	fromCrit := out[typ+"ChanceOnCrit"] / 100 * ailmentCrit / 100 //nolint:mnd // percent
	totalFromHit := fromHit / (fromHit + fromCrit)
	totalFromCrit := fromCrit / (fromHit + fromCrit)
	out[typ+"DotMulti"] = out[typ+"DotMulti"]*totalFromHit + critMulti*totalFromCrit
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
// It uses the hit's (the hand's) conversion table. PoB differs: its
// ailment cfg has no conversionTable, so calcDamage falls back to the
// skillCfg table and per-hand conversion and gain mods miss ailments.
func ailmentSourceDamage(list *ModDB, p *hitPass, dotCfg *Cfg, t string, typeFlags int) (float64, float64) {
	q := *p
	q.cfg = dotCfg
	mn, mx := calcDamage(list, &q, t, typeFlags, false)
	m := p.conv[t].mult
	return mn * m, mx * m
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
	if debugCombine != nil {
		debugCombine(stat, mh, oh, passes[0].out[chanceStat], passes[1].out[chanceStat], doubleHits)
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
	double := truthy(s.data.val("doubleHitsWhenDualWielding"))
	sum := func(stat string) {
		if len(passes) < 2 {
			if v, ok := passes[0].out[stat]; ok {
				out[stat] = v
			}
			return
		}
		out[stat] = passes[0].out[stat] + passes[1].out[stat]
		if !double {
			out[stat] /= 2
		}
	}
	avg("BleedChance")
	combineAilment(out, passes, "BleedDPS", "BleedChance", true, double)
	avg("PoisonChance")
	combineAilment(out, passes, "PoisonDPS", "PoisonChance", false, double)
	combineAilment(out, passes, "CausticGroundDPS", "PoisonChance", false, double)
	sum("TotalPoisonDPS")
	combineAilment(out, passes, "PoisonDamage", "PoisonChance", false, double)
	if truthy(s.data.val("showAverage")) {
		sum("TotalPoisonAverageDamage")
	} else {
		sum("TotalPoisonStacks")
	}
	avg("IgniteChance")
	combineAilment(out, passes, "IgniteDPS", "IgniteChance", true, double)
	if s.flags["igniteCanStack"] {
		combineAilment(out, passes, "IgniteDamage", "IgniteChance", false, double)
		sum("IgniteStacksMax")
		sum("TotalIgniteDPS")
	}
	avg("ImpaleChance")
	avg("ImpaleStoredDamage")
	combineAilment(out, passes, "ImpaleModifier", "ImpaleChance", false, double)
	sum("impaleStoredHitAvg")
}

// Data/Misc.lua data.misc bleed and poison numbers.
const (
	bleedPercentBase   = 70
	bleedDurationBase  = 5
	poisonPercentBase  = 0.30
	poisonDurationBase = 2
)

// configNum is env.configInput[name] as a number (0 when unset).
func configNum(actor *ModDB, name string) float64 {
	b := actor.theBuild()
	if b == nil {
		return 0
	}
	if in, ok := configInput(b.ActiveConfig(), name); ok && in.Kind == pobcode.KindNumber {
		return in.Number
	}
	return 0
}

// minCfg is ModStore:Min: the smallest MIN mod value (ok false without one).
func minCfg(db *ModDB, cfg *Cfg, name string) (float64, bool) {
	v, found := 0.0, false
	for _, r := range db.TabulateCfg(cfg, "MIN", name) {
		if f, ok := num(r.Value); ok && (!found || f < v) {
			v, found = f, true
		}
	}
	return v, found
}

// dotMultiFor is 1 + DotMultiplier (or its override) + <typ>DotMultiplier.
func dotMultiFor(list *ModDB, cfg *Cfg, typ string) float64 {
	if v, ok := list.OverrideCfg(cfg, "DotMultiplier"); ok {
		return 1 + v/100
	}
	return 1 + (list.SumCfg(cfg, "BASE", "DotMultiplier")+list.SumCfg(cfg, "BASE", typ+"DotMultiplier"))/100
}

// hitSpeed is globalOutput.HitSpeed or globalOutput.Speed.
func hitSpeed(global map[string]float64) float64 {
	if v, ok := global["HitSpeed"]; ok {
		return v
	}
	return global["Speed"]
}

// dotTakenMult is the effective DoT multiplier of type t on the enemy
// (resist or, for physical, damage reduction; damage taken).
func dotTakenMult(actor *ModDB, cfg *Cfg, t string) float64 {
	enemy := actor.enemy
	var resist float64
	if t == "Physical" {
		resist = math.Min(math.Max(0, enemy.Sum("BASE", "PhysicalDamageReduction")), enemyPhysicalDamageReductionCap)
	} else {
		resist = enemyResist(actor, cfg, t)
	}
	names := []string{"DamageTaken", "DamageTakenOverTime", t + "DamageTaken", t + "DamageTakenOverTime"}
	return (1 - resist/100) * (1 + enemy.SumCfg(cfg, "INC", names...)/100) * enemy.MoreCfg(cfg, names...)
}
