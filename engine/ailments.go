package engine

import (
	"maps"
	"math"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// The ailment, skill damage over time and combined DPS parts of
// calcs.offence (Modules/CalcOffence.lua): ailment chances, bleed, poison,
// ignite, impale, skill DoT, culling and combined DPS (decay is in
// offence_dps.go, the non-damaging ailments, knockback and stun in
// ailments_nondmg.go), with both the legacy (CritMultiplierAppliesToDegen)
// and the current Perfect Agony; preDotFunc is a skill hook.

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
//
//nolint:goconst // mod, tag and pass names as CalcOffence.lua spells them
func ailmentChances(actor *ModDB, s *activeSkill, p *hitPass) {
	list, cfg, out, enemy := s.mods, p.cfg, p.out, actor.enemy
	var handTag map[string]any
	switch p.label {
	case "Main Hand":
		handTag = map[string]any{"type": "Condition", "var": "MainHandAttack"}
	case "Off Hand":
		handTag = map[string]any{"type": "Condition", "var": "OffHandAttack"}
	}
	// CalcOffence.lua:4219-4227, the legacy Perfect Agony: each crit
	// multiplier mod but the base one also gives half its value, floored,
	// as ailment DoT multiplier on crits.
	if list.Sum("BASE", "CritMultiplierAppliesToDegen") > 0 {
		legacyPerfectAgony(list, cfg, handTag)
	}
	// Perfect Agony.
	if list.Flag("DotMultiplierIsCritMultiplier") {
		mult, ok := list.OverrideCfg(cfg, "CritMultiplier")
		if ok {
			mult -= 100
		} else {
			mult = list.SumCfg(cfg, "BASE", "CritMultiplier")
		}
		tags := []map[string]any{{"type": "Condition", "var": "CriticalStrike"}}
		if handTag != nil {
			tags = append(tags, handTag)
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
	if hit && modeEffective(actor) { // CalcOffence.lua:4254
		out["ImpaleChanceOnCrit"] = math.Min(100, list.SumCfg(cfg, "BASE", "ImpaleChance"))
	}
	out["KnockbackChanceOnCrit"] = knockbackChance(s, cfg) // CalcOffence.lua:4256-4260
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
		// CalcOffence.lua:4288-4292: Warden's Oath of Summer scorches
		// with the ignite chance (on the skill's mod list, so each pass
		// adds it again).
		player := actor
		if actor.player != nil {
			player = actor.player
		}
		if a == "Ignite" && player.Flag("IgniteCanScorch") {
			out["ScorchChance"] = math.Min(100, ch) //nolint:mnd // a chance cap
			list.NewMod("EnemyScorchChance", "BASE", ch, "Ignite Chance")
		}
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
		// CalcOffence.lua:4300-4305.
		if a == "Scorch" && player.Flag("IgniteCanScorch") && avoid != 0 {
			out["ScorchChanceOnCrit"] = 100
		}
		onCrit := out[a+"ChanceOnCrit"]
		if list.FlagCfg(cfg, "NeverCrit") {
			onCrit = 0
		}
		if out[a+"ChanceOnHit"]+onCrit > 0 {
			s.flags["inflict"+a] = true
		}
	}
	out["KnockbackChanceOnHit"] = knockbackChance(s, cfg) // CalcOffence.lua:4307-4311
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
	// CalcOffence.lua:4333-4338: ailmentMode "CRIT" (Configuration tab)
	// or AilmentsOnlyFromCrit: no ailment from non-crits.
	if ailmentModeCrit(actor) || actor.Flag("AilmentsOnlyFromCrit") {
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

// legacyPerfectAgony is CalcOffence.lua:4221-4226: for each mod of
// skillModList:Tabulate("BASE", cfg, "CritMultiplier") not from "Base",
// NewMod("DotMultiplier", "BASE", floor(mod.value / 2), mod.source,
// ModFlag.Ailment, { Condition CriticalStrike }, handCondition, unpack(mod)).
// For a spell handCondition is nil: createMod's tag list then has a hole
// after the crit tag, and EvalMod's ipairs never reads the mod's own tags.
//
//nolint:goconst // mod and source names as CalcOffence.lua spells them
func legacyPerfectAgony(list *ModDB, cfg *Cfg, handTag map[string]any) {
	var added []Mod
	for d := list; d != nil; d = d.parent {
		for i := range d.mods["CritMultiplier"] {
			m := &d.mods["CritMultiplier"][i]
			if !eligible(m, "BASE", cfg) || m.Source == "Base" {
				continue
			}
			if _, ok := list.eval(m, cfg); !ok {
				continue
			}
			v, _ := num(m.Value)
			tags := []map[string]any{{"type": "Condition", "var": "CriticalStrike"}}
			if handTag != nil {
				tags = append(append(tags, handTag), m.Tags...)
			}
			half := math.Floor(v / 2) //nolint:mnd // m_floor(mod.value / 2)
			added = append(added, Mod{Name: "DotMultiplier", Type: "BASE", Value: half, Flags: mfAilment,
				Tags: tags, Source: m.Source})
		}
	}
	for _, m := range added {
		list.Add(m)
	}
}

// ailmentModeCrit is env.configInput.ailmentMode == "CRIT".
func ailmentModeCrit(actor *ModDB) bool {
	b := actor.theBuild()
	return b != nil && configString(b.ActiveConfig(), "ailmentMode", "AVERAGE") == "CRIT"
}

// ailmentDamage is calcAilmentDamage: the chance of type over hits and
// crits (into out[type+"Chance"]) and the base damage weighted by it.
func ailmentDamage(out map[string]float64, typ string, critChance, hitDmg, critDmg float64) float64 {
	fromHit := float64(out[typ+"ChanceOnHit"] * (1 - critChance/100))
	fromCrit := float64(out[typ+"ChanceOnCrit"] * critChance / 100)
	out[typ+"Chance"] = float64(fromHit) + float64(fromCrit)
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
	fromHit := float64(out[typ+"ChanceOnHit"] / 100 * (1 - hitCrit/100))
	fromCrit := float64(out[typ+"ChanceOnCrit"] / 100 * ailmentCrit / 100)
	totalFromHit := fromHit / (float64(fromHit) + float64(fromCrit))
	totalFromCrit := fromCrit / (fromHit + fromCrit)
	out[typ+"DotMulti"] = float64(out[typ+"DotMulti"]*totalFromHit) + float64(critMulti*totalFromCrit)
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
		out[stat] = float64(math.Max(mh, oh)*k) + float64(math.Min(mh, oh)*(1-k))
		return
	}
	if doubleHits {
		out[stat] = mh*mc/10000 + oh*oc/10000
		return
	}
	out[stat] = float64(mh*mc/(float64(mc)+float64(oc))) + float64(oh*oc/(float64(mc)+float64(oc)))
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
	// CalcOffence.lua:5722-5737: the non-damaging ailments.
	for _, st := range []string{"ChillEffectMod", "ChillDuration", "ShockChance", "ShockDuration", "ShockEffectMod",
		"FreezeChance", "FreezeDurationMod", "ScorchChance", "ScorchEffectMod", "ScorchDuration", "BrittleChance",
		"BrittleEffectMod", "BrittleDuration", "SapChance", "SapEffectMod", "SapDuration"} {
		avg(st)
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
	if !modeEffective(actor) { // CalcOffence.lua:4624, 4894
		return 1
	}
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
