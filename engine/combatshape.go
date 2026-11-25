package engine

// The shape of the main skill: how one use turns into hits on targets.
// Combat DPS (spec "DPS в бою", section "Форма скилла") reads it to add the
// overlap PoB does not model. Named skills come from shapeTable; the rest
// are inferred from their skill types and marked fromDefault.

// deliveryKind is how a skill use reaches its targets.
type deliveryKind int

const (
	selfArea    deliveryKind = iota // area around the player (novas)
	targetArea                      // area at a target point
	projectile                      // projectiles from the player
	meleeStrike                     // melee hit without an area
	slam                            // melee slam area
	channel                         // channelled skill
	beam                            // beam or line
	cone                            // cone in front of the player
	singleHit                       // one hit on one target: nothing for geometry to add
)

type skillShape struct {
	kind        deliveryKind
	subHits     float64 // sub-hits per projectile/impact (Kinetic Blast explosions)
	subRadius   float64 // radius of each sub-hit, PoB units
	scatter     float64 // radius the sub-hits spread over around the impact point
	spreadDeg   float64 // projectile fan width (GMP/Scattershot shotgun)
	canShotgun  bool    // several projectiles of one cast may hit the same target
	pobOverlap  bool    // PoB's dpsMultiplier already models overlap
	fromDefault bool    // inferred from skill types, not from the table
}

// shapeEntry is one row of shapeTable.
type shapeEntry struct {
	kind deliveryKind
	// subHitsStat is the gem stat giving the sub-hits per impact.
	subHitsStat string
	// subRadiusOut and scatterOut are the outputs holding the sub-hit
	// radius and the radius the sub-hits spread over.
	subRadiusOut, scatterOut string
	// pobOverlap: this engine's preDamageFunc port or a statMap SkillData
	// mod sets skillData.dpsMultiplier for the overlap, in overlapParts (all
	// parts when nil) and while overlapCond holds (always when empty). Only
	// skills whose overlap the engine applies are flagged: the PoB value
	// combat DPS starts from has no overlap otherwise.
	pobOverlap   bool
	overlapParts []int
	overlapCond  string
}

// shapeTable holds the skills whose shape is not what their skill types say,
// keyed by the active gem name (transfigured gems listed on their own).
var shapeTable = func() map[string]shapeEntry {
	t := map[string]shapeEntry{}
	add := func(e shapeEntry, names ...string) {
		for _, n := range names {
			t[n] = e
		}
	}
	// Kinetic Blast: each projectile's impact makes cluster_burst_spawn_amount
	// explosions of the primary radius (active_skill_area_of_effect_
	// description_mode 1 reads "explosion radius") within the secondary
	// radius around the point of impact (Data/Skills/act_int.lua KineticBlast).
	add(shapeEntry{kind: projectile, subHitsStat: "cluster_burst_spawn_amount",
		subRadiusOut: "AreaOfEffectRadius", scatterOut: "AreaOfEffectRadiusSecondary"},
		"Kinetic Blast", "Kinetic Blast of Clustering")
	// Tornado Shot: each projectile releases tornado_shot_num_of_secondary_
	// projectiles when it reaches the target point; they cannot hit the
	// enemy the primary projectile hit. Not pobOverlap until PoB's part 2
	// preDamageFunc (act_dex.lua TornadoShot) is ported to offence.go.
	add(shapeEntry{kind: projectile, subHitsStat: "tornado_shot_num_of_secondary_projectiles"}, "Tornado Shot")
	add(shapeEntry{kind: projectile}, "Split Arrow", "Split Arrow of Splitting")
	add(shapeEntry{kind: selfArea}, "Ice Nova", "Ice Nova of Deep Freeze")
	// Ice Nova of Frostbolts is cast from Frostbolt projectiles.
	add(shapeEntry{kind: targetArea}, "Ice Nova of Frostbolts")
	add(shapeEntry{kind: targetArea}, "Frost Bomb", "Frost Bomb of Instability", "Frost Bomb of Forthcoming")
	// Overlap PoB models through skillData.dpsMultiplier (preDamageFunc in
	// offence.go and Data/Skills/*.lua).
	add(shapeEntry{kind: targetArea, pobOverlap: true},
		"Toxic Rain", "Toxic Rain of Sporeburst", "Toxic Rain of Withering")
	add(shapeEntry{kind: projectile, pobOverlap: true}, "Shrapnel Ballista", "Shrapnel Ballista of Steel")
	// Storm Rain: not pobOverlap until PoB's part 2 beam overlap
	// (act_dex.lua StormRain preDamageFunc) is ported to offence.go.
	add(shapeEntry{kind: targetArea}, "Storm Rain", "Storm Rain of the Conduit", "Storm Rain of the Fence")
	// Perforate: the statMap SkillData mod dpsMultiplier per
	// PerforateSpikeOverlap (act_str.lua), in Blood Stance except for Bloodshed.
	add(shapeEntry{kind: slam, pobOverlap: true, overlapCond: "BloodStance"}, "Perforate", "Perforate of Duality")
	add(shapeEntry{kind: slam, pobOverlap: true}, "Perforate of Bloodshed")
	// Kinetic Fusillade of Detonation is not in offence.go's preDamageFunc.
	add(shapeEntry{kind: projectile, pobOverlap: true, overlapParts: []int{1}}, "Kinetic Fusillade")
	add(shapeEntry{kind: projectile}, "Kinetic Fusillade of Detonation")
	add(shapeEntry{kind: projectile, pobOverlap: true, overlapParts: []int{2}}, "Barrage", "Barrage of Volley Fire")
	add(shapeEntry{kind: projectile, pobOverlap: true, overlapParts: []int{3, 4}}, "Ice Spear", "Ice Spear of Splitting")
	add(shapeEntry{kind: projectile, pobOverlap: true, overlapParts: []int{2}}, "Spark", "Spark of the Nova")
	return t
}()

// gmpSpreadDeg is the fan width of Greater Multiple Projectiles: 35 degrees
// (poewiki, Greater Multiple Projectiles Support).
const gmpSpreadDeg = 35

// shotgunSupports lets several projectiles of one use hit the same target.
// Scattershot is not in PoB v2.68.1's support data.
var shotgunSupports = map[string]float64{
	"Greater Multiple Projectiles":          gmpSpreadDeg,
	"Awakened Greater Multiple Projectiles": gmpSpreadDeg,
}

// shapeOf returns the main skill's shape; out is its offence output.
func shapeOf(s *activeSkill, out map[string]float64) skillShape {
	name := s.active.ge.Name
	e, ok := shapeTable[name]
	if !ok {
		return withShotgun(s, skillShape{kind: kindByTypes(s.types), fromDefault: true})
	}
	sh := skillShape{kind: e.kind}
	if e.pobOverlap && (e.overlapParts == nil || containsInt(e.overlapParts, s.part)) &&
		(e.overlapCond == "" || skillCondition(s, e.overlapCond)) {
		sh.pobOverlap = true
		return sh
	}
	if e.subHitsStat != "" {
		stats := skillStats(s.active, s.active.actorLevel)
		sh.subHits = stats[e.subHitsStat]
		// Kinetic Blast of Clustering: "modifiers to number of projectiles
		// instead apply to number of clusters" (NoAdditionalProjectiles keeps
		// ProjectileCount at 1); the base count of 1 is not a modifier.
		if stats["kinetic_blast_modifiers_to_number_of_projectiles_instead_apply_to_number_of_clusters"] != 0 {
			sh.subHits += s.mods.SumCfg(s.cfg, "BASE", "ProjectileCount") - 1
		}
	}
	if e.subRadiusOut != "" {
		sh.subRadius = out[e.subRadiusOut]
	}
	if e.scatterOut != "" {
		sh.scatter = out[e.scatterOut]
	}
	return withShotgun(s, sh)
}

// kindByTypes infers the delivery from the skill types, in this order:
// Projectile, Melee without Area, Slam, Nova, Area, Channel.
func kindByTypes(types map[int]bool) deliveryKind {
	switch {
	case types[stProjectile]:
		return projectile
	case types[stMelee] && !types[stArea]:
		return meleeStrike
	case types[stSlam]:
		return slam
	case types[stNova]:
		return selfArea
	case types[stArea]:
		return targetArea
	case types[stChannel]:
		return channel
	}
	return singleHit
}

// withShotgun marks projectile shapes supported by a shotgun support.
func withShotgun(s *activeSkill, sh skillShape) skillShape {
	if sh.kind != projectile || sh.pobOverlap {
		return sh
	}
	for _, e := range s.effects {
		if deg, ok := shotgunSupports[e.ge.Name]; ok {
			sh.canShotgun, sh.spreadDeg = true, deg
		}
	}
	return sh
}

// skillCondition is a Condition tag check on the skill's mod list
// (ModDB tag evaluation, case "Condition").
func skillCondition(s *activeSkill, v string) bool {
	return (s.mods != nil && s.mods.getCondition(v, s.cfg, false)) || (s.cfg != nil && s.cfg.SkillCond[v])
}
