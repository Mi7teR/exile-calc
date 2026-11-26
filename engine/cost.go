package engine

import (
	"math"
	"strings"
)

// costResource is an entry of calcs.offence's costs table.
type costResource struct {
	name, typ                                     string
	upfront, percent, unaffectedByGenericMults    bool
	text                                          string
	divisor                                       float64
	baseCost, baseCostRaw, totalCost, noMult, fin float64
	hasRaw                                        bool
}

// skillCosts is the "Calculate costs" part of calcs.offence
// (CalcOffence.lua): <Resource>Cost, <Resource>CostRaw and
// <Resource>HasCost into out (per minute costs as per second).
func skillCosts(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, cfg := s.mods, s.cfg
	if list.FlagCfg(cfg, "HasNoCost") {
		return
	}
	order := []*costResource{
		{name: "Mana", typ: "Mana", upfront: true, text: "mana", divisor: 1, hasRaw: true},
		{name: "Life", typ: "Life", upfront: true, text: "life", divisor: 1},
		{name: "ES", typ: "ES", upfront: true, text: "ES", divisor: 1},
		{name: "Soul", typ: "Soul", upfront: true, unaffectedByGenericMults: true, text: "soul", divisor: 1},
		{name: "Rage", typ: "Rage", upfront: true, text: "rage", divisor: 1},
		{name: "ManaPercent", typ: "Mana", upfront: true, percent: true, text: "mana", divisor: 1},
		{name: "LifePercent", typ: "Life", upfront: true, percent: true, text: "life", divisor: 1},
		{name: "ManaPerMinute", typ: "Mana", text: "mana/s", divisor: 60},
		{name: "LifePerMinute", typ: "Life", text: "life/s", divisor: 60},
		{name: "ManaPercentPerMinute", typ: "Mana", percent: true, text: "mana/s", divisor: 60},
		{name: "LifePercentPerMinute", typ: "Life", percent: true, text: "life/s", divisor: 60},
		{name: "ESPerMinute", typ: "ES", text: "ES/s", divisor: 60},
		{name: "ESPercentPerMinute", typ: "ES", percent: true, text: "ES/s", divisor: 60},
	}
	costs := map[string]*costResource{}
	for _, c := range order {
		costs[c.name] = c
	}
	lvCost := s.active.levelCost()
	mult := floorTo(list.MoreCfg(cfg, "SupportManaMultiplier"), 4)
	additionalLife := list.SumCfg(cfg, "BASE", "ManaCostAsLifeCost") / 100
	additionalES := list.SumCfg(cfg, "BASE", "ManaCostAsEnergyShieldCost") / 100
	hybridLife := list.SumCfg(cfg, "BASE", "HybridManaAndLifeCost_Life") / 100
	// costs[type].baseCostRaw = 0 (CalcOffence.lua:1754, 1780, 1784) gives
	// a cost table that had none a raw cost, so its <Resource>CostRaw is
	// output too.
	zero := func(c *costResource) { c.baseCost, c.baseCostRaw, c.fin, c.noMult, c.hasRaw = 0, 0, 0, 0, true }
	for _, val := range order {
		skillCost, has := list.OverrideCfg(cfg, "Base"+val.name+"CostOverride")
		if !has {
			skillCost, has = lvCost[val.name]
		}
		baseCost := 0.0
		if has {
			baseCost = round(skillCost/val.divisor, 2)
		}
		noMult := list.SumCfg(cfg, "BASE", val.name+"CostNoMult")
		correction := 0.0
		if val.upfront {
			baseCost += list.SumCfg(cfg, "BASE", val.name+"CostBase")
			val.totalCost = list.SumCfg(cfg, "BASE", val.name+"Cost", "Cost")
			if val.name == "Mana" && s.types[stReservationBecomesCost] && !list.FlagCfg(cfg, "CostESInsteadOfManaOrLife") && !list.FlagCfg(cfg, "CostLifeInsteadOfMana") {
				lv := s.active.ge.Levels[s.active.level]
				flat, ok := num(s.data.val(val.text + "ReservationFlat"))
				if !ok && lv != nil {
					flat = lv.ManaReservationFlat
				}
				pct, ok := num(s.data.val(val.text + "ReservationPercent"))
				if !ok && lv != nil {
					pct = lv.ManaReservationPercent
				}
				pool := actor.output[val.name]
				baseCost += flat + math.Floor(pool*pct/100)
				correction = round(pool*pct/100*mult, 0) - math.Floor(pool*pct/100*mult)
			}
		}
		val.baseCost += baseCost
		val.noMult += noMult
		val.fin = math.Floor(val.baseCost*mult) + val.noMult + correction
		if val.hasRaw {
			val.baseCostRaw = val.baseCost*mult + val.noMult + correction
		}
		switch val.typ {
		case "Life":
			mana := costs[strings.Replace(val.name, "Life", "Mana", 1)]
			if list.FlagCfg(cfg, "CostLifeInsteadOfMana") {
				if val.upfront {
					t := list.SumCfg(cfg, "BASE", mana.name+"Cost")
					val.totalCost += math.Max(0, t)
					mana.totalCost -= t
				}
				val.baseCost += mana.baseCost
				val.noMult += mana.noMult
				val.fin += mana.fin
				zero(mana)
			} else if (additionalLife > 0 || hybridLife > 0) && !list.FlagCfg(cfg, "CostESInsteadOfManaOrLife") {
				val.baseCost = mana.baseCost
				val.fin += round(mana.fin*(hybridLife+additionalLife), 0)
				if val.upfront && hybridLife > 0 {
					val.totalCost += round(math.Max(0, list.SumCfg(cfg, "BASE", mana.name+"Cost"))*hybridLife, 0)
				}
			}
		case "ES":
			mana := costs[strings.Replace(val.name, "ES", "Mana", 1)]
			life := costs[strings.Replace(val.name, "ES", "Life", 1)]
			if list.FlagCfg(cfg, "CostESInsteadOfManaOrLife") {
				if val.upfront {
					mt := list.SumCfg(cfg, "BASE", mana.name+"Cost")
					lt := list.SumCfg(cfg, "BASE", life.name+"Cost")
					val.totalCost += math.Max(0, mt) + math.Max(0, lt)
					mana.totalCost -= mt
					life.totalCost -= lt
				}
				val.baseCost += mana.baseCost + life.baseCost
				val.noMult += mana.noMult + life.noMult
				val.fin += mana.fin + life.fin
				zero(mana)
				zero(life)
			} else if additionalES > 0 {
				val.baseCost = mana.baseCost
				val.fin += round(mana.fin*additionalES, 0)
			}
		case "Rage":
			if list.FlagCfg(cfg, "CostRageInsteadOfSouls") {
				soul := costs["Soul"]
				val.baseCost = soul.baseCost
				val.noMult += soul.noMult
				val.fin = soul.baseCost
				mult = 1
				soul.baseCost, soul.noMult, soul.fin = 0, 0, 0
			}
		}
	}
	for _, val := range order {
		res := val.name
		if !val.upfront {
			res = strings.Replace(res, "Minute", "Second", 1)
		}
		out[res+"HasCost"] = b2f(val.baseCost > 0 || val.totalCost > 0 || val.noMult > 0 || val.fin > 0)
		name := res + "Cost"
		eff := calcMod(list, cfg, val.typ+"CostEfficiency", "CostEfficiency")
		if !val.unaffectedByGenericMults {
			out[name] = val.fin
			moreType := list.MoreCfg(cfg, val.typ+"Cost")
			moreCost := list.MoreCfg(cfg, "Cost")
			inc := list.SumCfg(cfg, "INC", val.typ+"Cost", "Cost")
			if val.hasRaw {
				out[name+"Raw"] = math.Max(0, math.Max(0, (1+inc/100)*val.baseCostRaw*moreType*moreCost/eff)+val.totalCost)
			}
			out[name] = roundCost(out[name], 1+inc/100)
			out[name] = roundCost(out[name], moreType)
			out[name] = roundCost(out[name], moreCost)
			out[name] = math.Max(0, out[name]/eff)
			if val.upfront && !val.percent {
				out[name] = round(out[name], 0)
			}
			out[name] = math.Max(0, out[name]+val.totalCost)
			if val.typ == "Mana" && hybridLife > 0 {
				out[name] = math.Max(0, math.Floor((1-hybridLife)*out[name]))
				if val.hasRaw {
					out[name+"Raw"] = math.Max(0, (1-hybridLife)*out[name+"Raw"])
				}
			}
		} else {
			moreType := list.MoreCfg(cfg, val.typ+"Cost")
			inc := list.SumCfg(cfg, "INC", val.typ+"Cost")
			c := math.Floor(val.baseCost + val.noMult)
			c = math.Max(0, (1+inc/100)*c)
			c = math.Max(0, moreType*c) / eff
			if val.upfront && !val.percent {
				c = round(c, 0)
			}
			out[name] = math.Max(0, c+val.totalCost)
		}
	}
}

// roundCost applies a cost multiplier the way calcs.offence does: down for
// increases, up for reductions.
func roundCost(v, mult float64) float64 {
	if mult < 1 {
		return math.Max(0, math.Ceil(mult*v))
	}
	return math.Max(0, math.Floor(mult*v))
}

// perSecondCosts is CalcOffence.lua:5961-6010 "Calculates and displays
// cost per second for skills that don't already have one": the upfront
// costs times the rate the skill is used, as <Resource>PerSecondCost (ES
// for mana with EnergyShieldProtectsMana), unless the skill already has
// that per second cost.
func perSecondCosts(actor *ModDB, s *activeSkill, out map[string]float64) {
	list := s.mods
	player := actor
	if actor.player != nil {
		player = actor.player
	}
	eb := player.Flag("EnergyShieldProtectsMana")
	_, hasCD := out["Cooldown"]
	for _, res := range []string{"Mana", "Life", "ES", "Soul", "Rage", "ManaPercent", "LifePercent"} {
		perSecondSet := out[res+"PerSecondHasCost"] != 0
		if out[res+"HasCost"] == 0 || !(out[res+"Cost"] > 0) ||
			(perSecondSet && !(eb && list.SumCfg(s.cfg, "BASE", "ManaCostAsEnergyShieldCost") != 0)) ||
			!(out["Speed"] > 0 || hasCD) {
			continue
		}
		used := res
		if eb && res == "Mana" {
			used = "ES"
		}
		speed := costUseSpeed(player, s, out)
		refund := 1.0
		if used == "Mana" && list.FlagCfg(s.cfg, "Every2UseManaCostRefund") {
			refund = 0.5
		}
		out[used+"PerSecondHasCost"] = 1
		out[used+"PerSecondCost"] += out[res+"Cost"] * speed * refund
	}
}

// costUseSpeed is useSpeed of perSecondCosts (CalcOffence.lua:5971-5991):
// the rate the skill is used at.
func costUseSpeed(player *ModDB, s *activeSkill, out map[string]float64) float64 {
	cd, hasCD := out["Cooldown"]
	repeats := 1.0
	if r, ok := out["Repeats"]; ok {
		repeats = r
	}
	// (cooldown > 0 and (rate > 0 and rate or 1 / cooldown) or rate)
	rateOr := func(rate float64) float64 {
		if hasCD && cd > 0 && !(rate > 0) {
			return 1 / cd
		}
		return rate
	}
	switch {
	case s.flags["trap"] || s.flags["mine"]:
		pre, ok := out["TrapThrowingSpeed"]
		if !ok {
			pre = out["MineLayingSpeed"]
		}
		c, ok := out["TrapCooldown"]
		if !ok {
			c, ok = cd, hasCD
		}
		if ok && c > 0 {
			return 1 / c
		}
		return pre
	case s.flags["totem"]:
		return rateOr(out["TotemPlacementSpeed"]) / repeats
	case s.mods.Flag("HasSeals") && s.mods.Flag("UseMaxUnleash") && mainHitTimeOverride(player, s).set:
		// env.player.mainSkill.skillData.hitTimeOverride
		return mainHitTimeOverride(player, s).v / repeats
	}
	return rateOr(out["Speed"]) / repeats
}

// mainHitTimeOverride is env.player.mainSkill.skillData.hitTimeOverride.
func mainHitTimeOverride(player *ModDB, s *activeSkill) optF {
	if m := mainSkill(player.activeSkills); m != nil {
		return m.data.hitTimeOverride
	}
	return s.data.hitTimeOverride
}
