package engine

import "math"

// Trap skill hooks.

func init() {
	registerSkill([]string{"Seismic Trap"}, skillHooks{preDamage: seismicTrapPreDamage})
	registerSkill([]string{"Seismic Trap of Swells"}, skillHooks{preDamage: seismicTrapOfSwellsPreDamage})
}

// seismicOverlap is Seismic Trap's hitChance: the chance that a wave
// (secondary radius) lands where it reaches an enemy of the configured
// radius inside the trap's full radius.
func seismicOverlap(s *activeSkill, out map[string]float64) float64 {
	enemyRadius, ok := s.mods.OverrideCfg(s.cfg, "EnemyRadius")
	if !ok {
		enemyRadius = s.mods.SumCfg(s.cfg, "BASE", "EnemyRadius")
	}
	r := out["AreaOfEffectRadiusSecondary"] + enemyRadius - 1
	full := out["AreaOfEffectRadius"]
	overlap := math.Min(r*r/(full*full), 1)
	out["OverlapChance"] = overlap * 100
	return overlap
}

// applySeismicMultiplier is the end of both Seismic Trap preDamageFuncs.
func applySeismicMultiplier(s *activeSkill, out map[string]float64, mult float64) {
	if mult == 1 {
		return
	}
	mulDPS(s.data, mult)
	if v, ok := out["SkillDPSMultiplier"]; ok {
		out["SkillDPSMultiplier"] = v * mult
	} else {
		out["SkillDPSMultiplier"] = mult
	}
}

// Data/Skills/act_dex.lua:13383-13495 Seismic Trap preDamageFunc: the
// wave pulse rate sets the hit time; parts multiply the damage by the
// waves that hit and the average number of active traps.
func seismicTrapPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	list, cfg, data := s.mods, s.cfg, s.data
	freq := (1 + list.SumCfg(cfg, "INC", "TrapThrowingSpeed", "SeismicPulseFrequency")/100) *
		list.MoreCfg(cfg, "TrapThrowingSpeed", "SeismicPulseFrequency")
	pulseRate := freq / numOr(data.val("repeatInterval"), 0)
	data.hitTimeOverride = optF{1 / pulseRate, true}
	out["WavePulseRate"] = pulseRate
	duration := data.duration.or(0) * calcMod(list, cfg, "Duration")
	pulses := math.Floor(duration * pulseRate)
	out["PulsesPerTrap"] = pulses
	activeTraps := pulses / pulseRate / out["TrapCooldown"]
	out["AverageActiveTraps"] = activeTraps
	overlap := seismicOverlap(s, out)
	maxWaves := list.SumCfg(cfg, "BASE", "MaximumWaves")
	mult := 1.0
	switch s.part {
	case 2:
		mult = maxWaves * overlap
	case 3:
		mult = maxWaves
	case 4:
		mult = activeTraps
	case 5:
		mult = activeTraps * maxWaves * overlap
	case 6:
		mult = activeTraps * maxWaves
	}
	applySeismicMultiplier(s, out, mult)
}

// Data/Skills/act_dex.lua:13624-13673 Seismic Trap of Swells
// preDamageFunc: the waves that hit, without the active trap parts.
func seismicTrapOfSwellsPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	overlap := seismicOverlap(s, out)
	maxWaves := s.mods.SumCfg(s.cfg, "BASE", "MaximumWaves")
	mult := 1.0
	switch s.part {
	case 2:
		mult = maxWaves * overlap
	case 3:
		mult = maxWaves
	}
	applySeismicMultiplier(s, out, mult)
}
