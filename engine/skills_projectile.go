package engine

import "math"

// Projectile skill hooks.

func init() {
	registerSkill([]string{"Barrage", "Barrage of Volley Fire", "Blast Rain", "Vaal Lightning Arrow"},
		skillHooks{preDamage: barragePreDamage})
	registerSkill([]string{"Blazing Salvo"}, skillHooks{preDamage: blazingSalvoPreDamage})
	registerSkill([]string{"Forbidden Rite", "Forbidden Rite of Soul Sacrifice"}, skillHooks{preDamage: forbiddenRitePreDamage})
	registerSkill([]string{"Freezing Pulse"}, skillHooks{preDamage: freezingPulsePreDamage})
	registerSkill([]string{"Kinetic Fusillade"},
		skillHooks{preDamage: kineticFusilladePreDamage, postCrit: kineticFusilladePostCrit})
	registerSkill([]string{"Spark", "Spark of the Nova"}, skillHooks{preDamage: sparkPreDamage})
	registerSkill([]string{"Ice Spear", "Ice Spear of Splitting"}, skillHooks{preDamage: iceSpearPreDamage})
	registerSkill([]string{"Toxic Rain", "Toxic Rain of Sporeburst", "Toxic Rain of Withering"},
		skillHooks{preDamage: toxicRainPreDamage})
	registerSkill([]string{"Shrapnel Ballista", "Shrapnel Ballista of Steel"}, skillHooks{preDamage: shrapnelBallistaPreDamage})
	registerSkill([]string{"Ball Lightning of Orbiting"}, skillHooks{preDamage: ballLightningOfOrbitingPreDamage})
}

// Data/Skills/act_dex.lua Barrage/Blast Rain/Vaal Lightning Arrow preDamageFunc
func barragePreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 2 {
		s.data["dpsMultiplier"] = out["ProjectileCount"]
	}
}

// Data/Skills/act_int.lua Blazing Salvo preDamageFunc
func blazingSalvoPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 2 {
		mulDPS(s.data, out["ProjectileCount"])
	}
}

// Data/Skills/act_int.lua Forbidden Rite preDamageFunc
func forbiddenRitePreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 2 {
		mulDPS(s.data, out["ProjectileCount"]+1)
	}
}

// Data/Skills/act_int.lua Freezing Pulse preDamageFunc
func freezingPulsePreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	r := 60 * out["ProjectileSpeedMod"]
	s.mods.Add(Mod{Name: "Damage", Type: "MORE", Value: -50.0, Source: "Skill:FreezingPulse",
		Tags: []map[string]any{{"type": "DistanceRamp", "ramp": []any{[]any{0.0, 0.0}, []any{r, 1.0}}}}})
	s.mods.Add(Mod{Name: "EnemyFreezeChance", Type: "BASE", Value: 25.0, Source: "Skill:FreezingPulse",
		Tags: []map[string]any{{"type": "DistanceRamp", "ramp": []any{[]any{0.0, 1.0}, []any{15 * out["ProjectileSpeedMod"], 0.0}}}}})
}

// Data/Skills/act_int.lua Kinetic Fusillade preDamageFunc
func kineticFusilladePreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 1 {
		proj := out["ProjectileCount"]
		s.data["dpsMultiplier"] = proj
		// Each projectile deals more damage per earlier one: the average.
		if per := numOr(s.data["damagePerProjectile"], 0); per != 0 && proj > 1 {
			avg := per * (proj - 1) / 2
			s.mods.NewMod("Damage", "MORE", avg, "Skill:KineticFusillade")
			out["KineticFusilladeAvgMoreMult"] = avg
		}
	}
}

// Data/Skills/act_int.lua Kinetic Fusillade postCritFunc
//
// Attacking faster than the projectiles fire wastes damage.
func kineticFusilladePostCrit(s *activeSkill, _, global map[string]float64) {
	n := 1.0
	if s.part == 1 {
		n = global["ProjectileCount"]
	}
	delay := (numOr(s.data["duration"], 0) + numOr(s.data["delayPerProjectile"], 0)*(n-1)) * global["DurationMod"]
	maxAPS := 1 / (math.Ceil(delay/serverTickTime) * serverTickTime)
	// PoB differs: it scales the current dpsMultiplier on every pass, so
	// a dual wielder's off hand gets the efficiency twice. It applies
	// once here, to the multiplier before the first pass.
	if s.part == 1 && global["Speed"] > maxAPS {
		base, ok := num(s.data["kineticFusilladeBaseDps"])
		if !ok {
			base = numOr(s.data["dpsMultiplier"], global["ProjectileCount"])
			s.data["kineticFusilladeBaseDps"] = base
		}
		s.data["dpsMultiplier"] = base * maxAPS / global["Speed"]
	}
	global["KineticFusilladeMaxEffectiveAPS"] = maxAPS
}

// Data/Skills/act_int.lua Spark preDamageFunc
func sparkPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 2 {
		mulDPS(s.data, 1+math.Floor(out["Duration"]/0.66))
	}
}

// Data/Skills/act_int.lua Ice Spear preDamageFunc
func iceSpearPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 3 || s.part == 4 {
		mulDPS(s.data, out["ProjectileCount"])
	}
}

// Data/Skills/act_dex.lua Toxic Rain preDamageFunc
func toxicRainPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	s.data["dpsMultiplier"] = math.Min(numOr(s.data["podOverlapMultiplier"], 1), out["ProjectileCount"])
}

// Data/Skills/act_dex.lua Shrapnel Ballista preDamageFunc
func shrapnelBallistaPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if !s.mods.Flag("SequentialProjectiles") {
		proj := out["ProjectileCount"]
		overlap := 1.0
		if v, ok := num(s.data["ShrapnelBallistaProjectileOverlap"]); ok {
			overlap = v
		} else if s.types[stRain] {
			overlap = proj
		}
		mulDPS(s.data, math.Min(overlap, proj))
	}
}

// Data/Skills/act_int.lua Ball Lightning of Orbiting preDamageFunc
func ballLightningOfOrbitingPreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	data := s.data
	if s.part == 2 || s.part == 3 {
		interval, _ := num(data["strikeInterval"])
		dur, _ := num(data["duration"])
		if interval > 0 {
			if n := math.Floor(dur / interval); n != 1 {
				data["dpsMultiplier"] = numOr(data["dpsMultiplier"], 1) * n
			}
		}
	}
}
