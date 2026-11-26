package engine

import "math"

// Projectile skill hooks.

func init() {
	registerSkill([]string{"Barrage", "Barrage of Volley Fire", "Blast Rain", "Vaal Lightning Arrow"},
		skillHooks{preDamage: barragePreDamage})
	registerSkill([]string{"Blazing Salvo"}, skillHooks{preDamage: blazingSalvoPreDamage})
	registerSkill(
		[]string{"Forbidden Rite", "Forbidden Rite of Soul Sacrifice"},
		skillHooks{preDamage: forbiddenRitePreDamage},
	)
	registerSkill([]string{"Freezing Pulse"}, skillHooks{preDamage: freezingPulsePreDamage})
	registerSkill([]string{"Kinetic Fusillade"},
		skillHooks{preDamage: kineticFusilladePreDamage, postCrit: kineticFusilladePostCrit})
	registerSkill([]string{"Spark", "Spark of the Nova"}, skillHooks{preDamage: sparkPreDamage})
	registerSkill([]string{"Ice Spear", "Ice Spear of Splitting"}, skillHooks{preDamage: iceSpearPreDamage})
	registerSkill([]string{"Toxic Rain", "Toxic Rain of Sporeburst", "Toxic Rain of Withering"},
		skillHooks{preDamage: toxicRainPreDamage})
	registerSkill(
		[]string{"Shrapnel Ballista", "Shrapnel Ballista of Steel"},
		skillHooks{preDamage: shrapnelBallistaPreDamage},
	)
	registerSkill([]string{"Ball Lightning of Orbiting"}, skillHooks{preDamage: ballLightningOfOrbitingPreDamage})
	registerSkill([]string{"Ball Lightning"}, skillHooks{preDamage: ballLightningPreDamage})
	registerSkill([]string{"Eye of Winter"}, skillHooks{preDamage: eyeOfWinterPreDamage})
}

// Data/Skills/act_dex.lua Barrage/Blast Rain/Vaal Lightning Arrow preDamageFunc.
func barragePreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 2 {
		s.data.dpsMultiplier = optF{out["ProjectileCount"], true}
	}
}

// Data/Skills/act_int.lua Blazing Salvo preDamageFunc.
func blazingSalvoPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 2 {
		mulDPS(s.data, out["ProjectileCount"])
	}
}

// Data/Skills/act_int.lua Forbidden Rite and Forbidden Rite of Soul
// Sacrifice preDamageFunc: chaos damage from life (base gem only) and
// energy shield, the self damage per cast (FRDamageTaken) and, for all
// primary projectiles, one hit per projectile.
func forbiddenRitePreDamage(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, data := s.mods, s.data
	names := []string{"DamageTaken", "ChaosDamageTaken", "DamageTakenWhenHit", "ChaosDamageTakenWhenHit"}
	takenFlat := list.Sum("BASE", names...)
	taken := math.Max((1+list.Sum("INC", names...)/100)*list.More(names...), 0)
	chaosFlat := math.Floor(round(takenFlat*taken, 0))
	life, es, chaosRes := actor.output["Life"], actor.output["EnergyShield"], actor.output["ChaosResist"]
	if s.flags["totem"] {
		life, es = out["TotemLife"], list.SumCfg(s.cfg, "BASE", "TotemEnergyShield")
		chaosRes = actor.output["TotemChaosResist"]
	}
	lifeAs, esAs := numOr(data.val("lifeDealtAsChaos"), 0), numOr(data.val("energyShieldDealtAsChaos"), 0)
	add := life*lifeAs + es*esAs
	selfTaken := func(pool float64, key string) float64 {
		return math.Floor(round(pool*numOr(data.val(key), 0), 0) * (100 - chaosRes) / 100 * taken)
	}
	data.set("ChaosMin", numOr(data.val("ChaosMin"), 0)+add)
	data.set("ChaosMax", numOr(data.val("ChaosMax"), 0)+add)
	if s.part == 2 {
		mulDPS(data, out["ProjectileCount"]+1)
	}
	if s.active.ge.Name == "Forbidden Rite" {
		out["FRDamageTaken"] = selfTaken(life, "SelfDamageTakenLife") + selfTaken(es, "SelfDamageTakenES") + chaosFlat
	} else {
		out["FRDamageTaken"] = selfTaken(es, "SelfDamageTakenES") + chaosFlat
	}
}

// Data/Skills/act_int.lua Freezing Pulse preDamageFunc.
func freezingPulsePreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	r := 60 * out["ProjectileSpeedMod"]
	s.mods.Add(Mod{Name: "Damage", Type: "MORE", Value: -50.0, Source: "Skill:FreezingPulse",
		Tags: []map[string]any{{"type": "DistanceRamp", "ramp": []any{[]any{0.0, 0.0}, []any{r, 1.0}}}}})
	s.mods.Add(Mod{
		Name:   "EnemyFreezeChance",
		Type:   "BASE",
		Value:  25.0,
		Source: "Skill:FreezingPulse",
		Tags: []map[string]any{
			{"type": "DistanceRamp", "ramp": []any{[]any{0.0, 1.0}, []any{15 * out["ProjectileSpeedMod"], 0.0}}},
		},
	})
}

// Data/Skills/act_int.lua Kinetic Fusillade preDamageFunc.
func kineticFusilladePreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 1 {
		proj := out["ProjectileCount"]
		s.data.dpsMultiplier = optF{proj, true}
		// Each projectile deals more damage per earlier one: the average.
		if per := numOr(s.data.val("damagePerProjectile"), 0); per != 0 && proj > 1 {
			avg := per * (proj - 1) / 2
			s.mods.NewMod("Damage", "MORE", avg, "Skill:KineticFusillade")
			out["KineticFusilladeAvgMoreMult"] = avg
		}
	}
}

// Data/Skills/act_int.lua Kinetic Fusillade postCritFunc.
//
// Attacking faster than the projectiles fire wastes damage.
func kineticFusilladePostCrit(s *activeSkill, _, global map[string]float64) {
	n := 1.0
	if s.part == 1 {
		n = global["ProjectileCount"]
	}
	delay := (s.data.duration.or(0) + numOr(s.data.val("delayPerProjectile"), 0)*(n-1)) * global["DurationMod"]
	maxAPS := 1 / (math.Ceil(delay/serverTickTime) * serverTickTime)
	// PoB differs: it scales the current dpsMultiplier on every pass, so
	// a dual wielder's off hand gets the efficiency twice. It applies
	// once here, to the multiplier before the first pass.
	if s.part == 1 && global["Speed"] > maxAPS {
		base, ok := num(s.data.val("kineticFusilladeBaseDps"))
		if !ok {
			base = s.data.dpsMultiplier.or(global["ProjectileCount"])
			s.data.set("kineticFusilladeBaseDps", base)
		}
		s.data.dpsMultiplier = optF{base * maxAPS / global["Speed"], true}
	}
	global["KineticFusilladeMaxEffectiveAPS"] = maxAPS
}

// Data/Skills/act_int.lua Spark preDamageFunc.
func sparkPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 2 {
		mulDPS(s.data, 1+math.Floor(out["Duration"]/0.66))
		out["SkillDPSMultiplier"] = s.data.dpsMultiplier.or(1) // the whole multiplier, as PoB
	}
}

// Data/Skills/act_int.lua Ice Spear preDamageFunc.
func iceSpearPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 3 || s.part == 4 {
		mulDPS(s.data, out["ProjectileCount"])
	}
}

// Data/Skills/act_dex.lua Toxic Rain preDamageFunc.
func toxicRainPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	s.data.dpsMultiplier = optF{math.Min(numOr(s.data.val("podOverlapMultiplier"), 1), out["ProjectileCount"]), true}
}

// Data/Skills/act_dex.lua Shrapnel Ballista preDamageFunc.
func shrapnelBallistaPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	if !s.mods.Flag("SequentialProjectiles") {
		proj := out["ProjectileCount"]
		overlap := 1.0
		if v, ok := num(s.data.val("ShrapnelBallistaProjectileOverlap")); ok {
			overlap = v
		} else if s.types[stRain] {
			overlap = proj
		}
		mulDPS(s.data, math.Min(overlap, proj))
	}
}

// Data/Skills/act_int.lua Ball Lightning of Orbiting preDamageFunc.
func ballLightningOfOrbitingPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	data := s.data
	if s.part == 2 || s.part == 3 {
		interval, _ := num(data.val("strikeInterval"))
		dur, _ := data.duration.get()
		if interval > 0 {
			n := math.Floor(dur / interval)
			if n != 1 {
				applySkillDPSMultiplier(s, out, n)
			}
			out["NormalHitsPerCast"] = n
		}
	}
}

// projectileDistanceCap is data.misc.ProjectileDistanceCap (Modules/Data.lua:273).
const projectileDistanceCap = 150

// Data/Skills/act_int.lua:1086-1165 Ball Lightning preDamageFunc: one
// strike per ball (part 1), or the strikes of a ball travelling past the
// enemy from the cast distance (part 2, "All Bolts in Range").
//
// PoB divides by the strike interval and the ball's distance per strike;
// when either is not positive (missing data, no projectile speed) the
// multiplier stays 1 and the actor notes it as incomplete instead of an
// Inf or NaN DPS.
func ballLightningPreDamage(actor *ModDB, s *activeSkill, out map[string]float64) {
	data := s.data
	mult := 0.0
	switch s.part {
	case 1:
		mult = 1
	case 2:
		perStrike := numOr(data.val("strikeInterval"), 0)
		distPerStrike := numOr(data.val("projectileSpeed"), 0) * calcMod(s.mods, s.cfg, "ProjectileSpeed") * perStrike
		if perStrike <= 0 || distPerStrike <= 0 {
			noteIncomplete(actor, "Ball Lightning: no strike interval or projectile speed, bolts in range not counted")
			return
		}
		maxStrikes := math.Floor(data.duration.or(0) / perStrike)
		baseRadius := out["AreaOfEffectRadius"]
		strikeRadius := baseRadius + enemyRadius(s)
		var castDist float64
		switch {
		case s.cfg != nil && s.cfg.HasSkillDist:
			castDist = s.cfg.SkillDist
		case s.flags["triggered"]:
			castDist = math.Floor(baseRadius / 2)
		default:
			castDist = baseRadius
		}
		first := math.Max(1, math.Ceil((castDist-strikeRadius)/distPerStrike))
		last := math.Floor(math.Min(projectileDistanceCap, castDist+strikeRadius) / distPerStrike)
		mult = math.Max(0, math.Min(maxStrikes, last+1-first))
	}
	if mult != 1 {
		applySkillDPSMultiplier(s, out, mult)
	}
}

// Data/Skills/act_int.lua:5519-5521 Eye of Winter preDamageFunc: more
// damage ramping up with the distance travelled.
func eyeOfWinterPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	r := 60 * out["ProjectileSpeedMod"]
	s.mods.Add(Mod{Name: "Damage", Type: "MORE", Value: s.mods.SumCfg(s.cfg, "BASE", "EyeOfWinterRamp"),
		Source: "Skill:EyeOfWinter",
		Tags:   []map[string]any{{"type": "DistanceRamp", "ramp": []any{[]any{0.0, 0.0}, []any{r, 1.0}}}}})
}
