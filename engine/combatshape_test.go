package engine

import (
	"os"
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// calcCorpus runs Calc on a golden corpus build.
func calcCorpus(t *testing.T, id string) *Result {
	t.Helper()
	raw, err := os.ReadFile("../testdata/golden/builds/" + id + ".code")
	if err != nil {
		t.Fatal(err)
	}
	b, err := pobcode.FromCode(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	calc, err := New()
	if err != nil {
		t.Fatal(err)
	}
	res, err := calc.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if res.main == nil {
		t.Fatalf("%s: no main skill in Result", id)
	}
	return res
}

func TestShapeKineticBlast(t *testing.T) {
	res := calcCorpus(t, "hlasMvXnooQc") // Kinetic Blast Deadeye, gem quality 30
	s := res.main
	if s.active.ge.Name != "Kinetic Blast" {
		t.Fatalf("main skill %q, want Kinetic Blast", s.active.ge.Name)
	}
	sh := shapeOf(s, res.Stats)
	stat := skillStats(s.active, s.active.actorLevel)["cluster_burst_spawn_amount"]
	if sh.kind != projectile || sh.fromDefault || sh.pobOverlap {
		t.Errorf("shape %+v, want table projectile", sh)
	}
	// 4 base explosions + 1 from 30 quality (0.05 per quality).
	if sh.subHits != stat || stat != 5 {
		t.Errorf("subHits %v, gem stat %v, want 5", sh.subHits, stat)
	}
	// Explosion radius is the primary radius (active_skill_area_of_effect_
	// description_mode 1: "explosion radius"), spread over the secondary one.
	if sh.subRadius != res.Stats["AreaOfEffectRadius"] || sh.subRadius == 0 {
		t.Errorf("subRadius %v, want AreaOfEffectRadius %v", sh.subRadius, res.Stats["AreaOfEffectRadius"])
	}
	if sh.scatter != res.Stats["AreaOfEffectRadiusSecondary"] || sh.scatter == 0 {
		t.Errorf("scatter %v, want AreaOfEffectRadiusSecondary %v", sh.scatter, res.Stats["AreaOfEffectRadiusSecondary"])
	}
}

func TestShapeKineticBlastClustering(t *testing.T) {
	// Kinetic Blast of Clustering: 4 explosions at 20 quality, and its
	// projectile count modifiers (one extra here) add clusters instead.
	res := calcCorpus(t, "mKBO1UbFLlKF")
	sh := shapeOf(res.main, res.Stats)
	if sh.kind != projectile || sh.fromDefault || sh.subHits != 5 {
		t.Errorf("shape %+v, want table projectile with 5 sub-hits", sh)
	}
}

func TestShapeDefaultProjectile(t *testing.T) {
	res := calcCorpus(t, "SE4GCSBAhJxM") // Ethereal Knives
	sh := shapeOf(res.main, res.Stats)
	if sh.kind != projectile || !sh.fromDefault || sh.pobOverlap {
		t.Errorf("shape %+v, want default projectile", sh)
	}
}

func TestShapeDefaultNova(t *testing.T) {
	for _, id := range []string{
		"uLlom8wOQPLi", // Divine Retribution: Area, no projectiles
		"NrNHKFKiSl5D", // Vortex of Projection: Nova
	} {
		res := calcCorpus(t, id)
		sh := shapeOf(res.main, res.Stats)
		if (sh.kind != selfArea && sh.kind != targetArea) || !sh.fromDefault {
			t.Errorf("%s (%s): shape %+v, want default area", id, res.main.active.ge.Name, sh)
		}
	}
}

func TestShapePobOverlap(t *testing.T) {
	for _, id := range []string{
		"gzVTJCkWXFan", // Toxic Rain, dpsMultiplier 5
		"Q-q1GtKC_F_P", // Spark of the Nova part 2
		"gZyyYnl6zA-W", // Ice Spear part 4
		"yvENgQrpk-61", // Kinetic Fusillade part 1
	} {
		res := calcCorpus(t, id)
		if !shapeOf(res.main, res.Stats).pobOverlap {
			t.Errorf("%s (%s part %d): pobOverlap false", id, res.main.active.ge.Name, res.main.part)
		}
	}
	// Spark part 1 (single projectile) is not modelled by PoB.
	res := calcCorpus(t, "5g9VlzQd4BVS")
	if sh := shapeOf(res.main, res.Stats); sh.pobOverlap || sh.kind != projectile {
		t.Errorf("Spark part %d: shape %+v, want plain projectile", res.main.part, sh)
	}
}

func TestShapeShotgunGMP(t *testing.T) {
	active := &skillEffect{ge: &gamedata.Skill{Name: "Ethereal Knives"}}
	s := &activeSkill{active: active, types: map[int]bool{stProjectile: true},
		effects: []*skillEffect{active, {ge: &gamedata.Skill{Name: "Greater Multiple Projectiles"}}}}
	sh := shapeOf(s, nil)
	if !sh.canShotgun || sh.spreadDeg != gmpSpreadDeg || !sh.fromDefault {
		t.Errorf("shape %+v, want GMP shotgun", sh)
	}
	s.effects = s.effects[:1]
	if sh := shapeOf(s, nil); sh.canShotgun || sh.spreadDeg != 0 {
		t.Errorf("shape %+v without GMP, want no shotgun", sh)
	}
}

// pobOverlap only where this engine applies the overlap multiplier.
func TestShapePobOverlapEngineOnly(t *testing.T) {
	skill := func(name string, part int) *activeSkill {
		active := &skillEffect{ge: &gamedata.Skill{Name: name}}
		return &activeSkill{active: active, effects: []*skillEffect{active}, part: part,
			types: map[int]bool{stProjectile: true}, mods: NewModDB(), cfg: &Cfg{}}
	}
	// Not ported to offence.go's preDamageFunc.
	for _, s := range []*activeSkill{skill("Tornado Shot", 2), skill("Storm Rain", 2), skill("Kinetic Fusillade of Detonation", 1)} {
		if shapeOf(s, nil).pobOverlap {
			t.Errorf("%s part %d: pobOverlap, engine has no overlap multiplier", s.active.ge.Name, s.part)
		}
	}
	// Perforate: SkillData dpsMultiplier only in Blood Stance.
	p := skill("Perforate", 0)
	if shapeOf(p, nil).pobOverlap {
		t.Error("Perforate in Sand Stance: pobOverlap")
	}
	p.mods.conditions["BloodStance"] = true
	if shapeOf(p, nil).pobOverlap {
		t.Error("Perforate in Blood Stance without spike overlap: pobOverlap")
	}
	p.mods.multipliers["PerforateSpikeOverlap"] = 3
	if !shapeOf(p, nil).pobOverlap {
		t.Error("Perforate in Blood Stance: no pobOverlap")
	}
	b := skill("Perforate of Bloodshed", 0)
	if shapeOf(b, nil).pobOverlap {
		t.Error("Perforate of Bloodshed without spike overlap: pobOverlap")
	}
	b.mods.multipliers["PerforateSpikeOverlap"] = 3
	if !shapeOf(b, nil).pobOverlap {
		t.Error("Perforate of Bloodshed: no pobOverlap")
	}
}

func TestShapeShrapnelBallista(t *testing.T) {
	newSkill := func() *activeSkill {
		active := &skillEffect{ge: &gamedata.Skill{Name: "Shrapnel Ballista"}}
		return &activeSkill{active: active, effects: []*skillEffect{active}, data: map[string]any{},
			types: map[int]bool{stProjectile: true}, mods: NewModDB(), cfg: &Cfg{}}
	}
	// Default: offence.go applies min(1, ProjectileCount) = 1, no overlap.
	if sh := shapeOf(newSkill(), nil); sh.pobOverlap || sh.kind != projectile {
		t.Errorf("default Shrapnel Ballista: shape %+v, want projectile without pobOverlap", sh)
	}
	rain := newSkill()
	rain.types[stRain] = true
	cfgOverlap := newSkill()
	cfgOverlap.data["ShrapnelBallistaProjectileOverlap"] = 3.0
	seq := newSkill()
	seq.mods.Add(Mod{Name: "SequentialProjectiles", Type: "FLAG", Value: true})
	for name, s := range map[string]*activeSkill{"Rain type": rain, "overlap config": cfgOverlap, "SequentialProjectiles": seq} {
		if !shapeOf(s, nil).pobOverlap {
			t.Errorf("Shrapnel Ballista with %s: no pobOverlap", name)
		}
	}
}

func TestShapeMoltenStrikeAndChannel(t *testing.T) {
	res := calcCorpus(t, "tyLAO6f9GS0_") // Molten Strike of the Zenith part 6
	if sh := shapeOf(res.main, res.Stats); !sh.pobOverlap {
		t.Errorf("Molten Strike of the Zenith part %d: shape %+v, want pobOverlap", res.main.part, sh)
	}
	res = calcCorpus(t, "NCgd_ieVLUw7") // Cyclone: Melee, Area, Channel
	if sh := shapeOf(res.main, res.Stats); sh.kind != channel || !sh.fromDefault {
		t.Errorf("Cyclone: shape %+v, want default channel", sh)
	}
}
