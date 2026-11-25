package engine

import (
	"math"
	"strings"
)

// Channelled skill hooks.

func init() {
	registerSkill([]string{"Flameblast", "Flameblast of Celerity", "Flameblast of Contraction"},
		skillHooks{preDamage: flameblastPreDamage})
	registerSkill(
		[]string{"Lightning Tendrils of Eccentricity"},
		skillHooks{
			preDamage: lightningTendrilsOfEccentricityPreDamage,
			postCrit:  lightningTendrilsOfEccentricityPostCrit,
		},
	)
	registerSkill([]string{"Storm Burst"}, skillHooks{preDamage: stormBurstPreDamage})
	registerSkill([]string{"Blood Sacrament"}, skillHooks{initial: bloodSacramentInitial})
	registerSkill([]string{"Cyclone", "Vaal Cyclone"}, skillHooks{initial: cycloneInitial})
	registerSkill([]string{"Cyclone of Tumult"}, skillHooks{initial: cycloneOfTumultInitial})
}

// Data/Skills/act_int.lua Flameblast preDamageFunc.
//
// One release per channelled stage beyond the starting stages.
func flameblastPreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	stage := "Multiplier:" + strings.Join(strings.Fields(s.active.ge.Name), "") + "Stage"
	s.data.set("hitTimeMultiplier", math.Max(
		s.mods.SumCfg(s.cfg, "BASE", stage)-s.mods.SumCfg(s.cfg, "BASE", "Multiplier:FlameblastMinimumStage"),
		1,
	))
}

// Data/Skills/act_int.lua Lightning Tendrils of Eccentricity preDamageFunc.
//
// The DPS of each pulse part.
func lightningTendrilsOfEccentricityPreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	interval := numOr(s.data.val("pulseInterval"), 0)
	part := []map[string]any{{"type": "SkillPart", "skillPart": float64(s.part)}}
	switch s.part {
	case 2:
		s.mods.Add(Mod{Name: "DPS", Type: "MORE", Value: -(1 / interval) * 100, Source: "Normal pulse", Tags: part})
	case 3:
		s.mods.Add(
			Mod{
				Name:   "DPS",
				Type:   "MORE",
				Value:  -(interval - 1) / interval * 100,
				Source: "Stronger pulse",
				Tags:   part,
			},
		)
	}
}

// Data/Skills/act_int.lua Lightning Tendrils of Eccentricity postCritFunc.
//
// The 5th pulse's more damage as an average over the interval.
func lightningTendrilsOfEccentricityPostCrit(s *activeSkill, out, _ map[string]float64) {
	if s.part != 1 {
		return
	}
	interval := numOr(s.data.val("pulseInterval"), 0)
	pulse := numOr(s.data.val("pulseDamage"), 0) / 100
	crit, eff, multi := out["PreEffectiveCritChance"]/100, out["CritChance"]/100, out["CritMultiplier"]
	avg := 100 * (((interval-1)*(1+crit*(multi-1))+(1+pulse)*multi)/(interval*((1-eff)+multi*eff)) - 1)
	s.mods.Add(
		Mod{Name: "Damage", Type: "MORE", Value: avg, Source: "Average Pulse Damage", KeywordFlags: kfHit | kfAilment,
			Tags: []map[string]any{{"type": "SkillPart", "skillPart": 1.0}}},
	)
}

// Data/Skills/act_int.lua Storm Burst preDamageFunc.
func stormBurstPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	data := &s.data
	freq, _ := num(data.val("repeatFrequency"))
	dur := data.duration.or(0) * out["DurationMod"]
	switch s.part {
	case 2:
		// duration * 10 / (jump * 10) avoids floating point issues; +1 tick on spawn.
		mulDPS(data, 1+math.Floor(dur*10/(freq*10)))
	case 3:
		jumps := math.Floor((dur + 0.001) / freq)
		per, _ := num(data.val("remainingDurationDamage"))
		s.mods.Add(Mod{Name: "Damage", Type: "MORE", Value: math.Max(per*jumps-100, 0), Source: "Skill:StormBurst"})
	}
}

// Data/Skills/other.lua Blood Sacrament initialFunc.
//
// BloodSacramentUnique: life reserved per stage, from the skill's own life
// reservation (skillData LifeReservedPercent / LifeReservedBase, set by the
// reservation pass). PoB differs: with no reservation stored it falls back
// to math.huge life per stage, i.e. infinite damage; here an unknown
// reservation adds nothing.
func bloodSacramentInitial(actor *ModDB, s *activeSkill, _ hitSource, _ *hitSource) {
	if actor.output["LifeReservedPercent"] >= 100 {
		return
	}
	pct, base := numOr(s.data.val("LifeReservedPercent"), 3), numOr(s.data.val("LifeReservedBase"), 0)
	s.mods.NewMod("Multiplier:ChannelledLifeReservedPercentPerStage", "BASE", pct, "Blood Sacrament")
	s.mods.NewMod("Multiplier:ChannelledLifeReservedPerStage", "BASE", base, "Blood Sacrament")
}

// Data/Skills/act_dex.lua Cyclone initialFunc (also Vaal Cyclone).
func cycloneInitial(_ *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource) {
	cycloneMeleeRange(s, w1, w2, "Skill:Cyclone")
}

// Data/Skills/act_dex.lua Cyclone of Tumult initialFunc.
func cycloneOfTumultInitial(_ *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource) {
	cycloneMeleeRange(s, w1, w2, "Skill:CycloneAltX")
}

// cycloneMeleeRange is the Cyclone initialFuncs' body: AdditionalMeleeRange
// is the weapon's range bonus plus MeleeWeaponRange (averaged when dual
// wielding), or the unarmed range bonus.
func cycloneMeleeRange(s *activeSkill, w1 hitSource, w2 *hitSource, source string) {
	list := s.mods
	bonus := func(w *hitSource, wcfg *Cfg) float64 {
		weaponRange := list.SumCfg(wcfg, "BASE", "MeleeWeaponRange")
		return weaponRange + 10*list.SumCfg(wcfg, "BASE", "MeleeWeaponRangeMetre") + w.rangeBonus
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
