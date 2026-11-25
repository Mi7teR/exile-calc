package engine

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
)

func sumHits(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s
}

func TestCircleOverlapAnalytic(t *testing.T) {
	tg := target{x: 40, y: 0, r: 20}
	// A zone centred on the target always hits it.
	if !circleHits(tg.x, tg.y, 5, tg) {
		t.Errorf("zone centred on the target misses")
	}
	sh := skillShape{kind: targetArea}
	if h := expectedHits(sh, 1, 0, 0, 0, 5, 0, bossModel(BossMedium), 1); len(h) != 1 || h[0] != 1 {
		t.Errorf("target area on the boss: %v, want [1]", h)
	}
	// A zone of radius 5 with its centre uniform in a circle of 30 around
	// the target's centre hits when the centres are within 25: the chance is
	// the area ratio 25^2 / 30^2.
	rng := rand.New(rand.NewPCG(7, pcgStream))
	const n = 20000
	hits := 0
	for range n {
		x, y := uniformInDisk(rng, tg.x, tg.y, 30)
		if circleHits(x, y, 5, tg) {
			hits++
		}
	}
	got, want := float64(hits)/n, 25.0*25/(30*30)
	if math.Abs(got-want) > 0.02 {
		t.Errorf("Monte Carlo %v, analytic %v", got, want)
	}
}

func TestKineticBlastBoss(t *testing.T) {
	// Kinetic Blast: 4 explosions of radius 20 over a secondary radius of
	// 28 (base 14 and 20 in act_int.lua, both scaled by the same area).
	sh := skillShape{kind: projectile, subHits: 4, subRadius: 20, scatter: 28}
	h := expectedHits(sh, 1, 0, 0, 0, 20, 0, bossModel(BossMedium), 1)
	if len(h) != 1 || h[0] < 2.5 || h[0] > 4.0 {
		t.Errorf("Kinetic Blast on the boss: %v, want one value in [2.5, 4.0]", h)
	}
}

func TestPierceSingleTarget(t *testing.T) {
	sh := skillShape{kind: projectile}
	h := expectedHits(sh, 1, 3, 0, 0, 0, 0, bossModel(BossMedium), 1)
	if len(h) != 1 || h[0] != 1 {
		t.Errorf("pierce 3 on the boss: %v, want [1]", h)
	}
	// Chain and fork find no second target on the boss alone.
	h = expectedHits(sh, 1, 0, 3, 2, 0, 0, bossModel(BossMedium), 1)
	if h[0] != 1 {
		t.Errorf("chain/fork on the boss: %v, want [1]", h)
	}
}

func TestShotgunGMP(t *testing.T) {
	sh := skillShape{kind: projectile, canShotgun: true, spreadDeg: gmpSpreadDeg}
	h := expectedHits(sh, 5, 0, 0, 0, 0, 0, bossModel(BossLarge), 1)
	if h[0] <= 1 {
		t.Errorf("GMP shotgun on a large boss: %v, want > 1", h)
	}
	sh.canShotgun = false
	h = expectedHits(sh, 5, 0, 0, 0, 0, 0, bossModel(BossLarge), 1)
	if h[0] != 1 {
		t.Errorf("5 projectiles without shotgun: %v, want 1", h)
	}
}

func TestPackPierce(t *testing.T) {
	h := expectedHits(skillShape{kind: projectile}, 1, 100, 0, 0, 0, 0, packModel(PackParams{}), 1)
	if len(h) != 10 || sumHits(h) <= 1 {
		t.Errorf("pierce 100 through the pack: %v, want 10 mobs summing > 1", h)
	}
}

func TestDeterministic(t *testing.T) {
	sh := skillShape{kind: projectile, subHits: 4, subRadius: 20, scatter: 28, canShotgun: true, spreadDeg: 35}
	m := packModel(PackParams{})
	a := expectedHits(sh, 3, 1, 1, 1, 20, 0, m, 42)
	b := expectedHits(sh, 3, 1, 1, 1, 20, 0, m, 42)
	if !slices.Equal(a, b) {
		t.Errorf("same seed, different hits: %v vs %v", a, b)
	}
	if !slices.Equal(packModel(PackParams{}).targets, m.targets) {
		t.Errorf("pack layout is not deterministic")
	}
}

func TestBossPresets(t *testing.T) {
	for size, r := range map[BossSize]float64{BossSmall: 10, BossMedium: 20, BossLarge: 40} {
		m := bossModel(size)
		if len(m.targets) != 1 || m.targets[0] != (target{40, 0, r}) || m.distance != 40 {
			t.Errorf("boss %d: %+v", size, m)
		}
	}
	var zero BossSize
	if zero != BossMedium {
		t.Errorf("zero BossSize is not BossMedium")
	}
}

func TestPackDefaults(t *testing.T) {
	m := packModel(PackParams{})
	if len(m.targets) != 10 || m.distance != 60 {
		t.Fatalf("default pack %+v", m)
	}
	for _, tg := range m.targets {
		if tg.r != 4 || math.Hypot(tg.x-60, tg.y) > 30 {
			t.Errorf("mob %+v outside the default cluster", tg)
		}
	}
}

func TestTornadoShot(t *testing.T) {
	sh := skillShape{kind: projectile, subHits: 3} // secondary projectiles
	if h := expectedHits(sh, 1, 0, 0, 0, 0, 0, bossModel(BossMedium), 1); h[0] != 1 {
		t.Errorf("Tornado Shot on the boss: %v, want 1", h)
	}
	if h := expectedHits(sh, 1, 0, 0, 0, 0, 0, packModel(PackParams{}), 1); sumHits(h) <= 1 {
		t.Errorf("Tornado Shot on the pack: %v, want secondaries to hit other mobs", h)
	}
}

func TestPobOverlapOnePerTarget(t *testing.T) {
	sh := skillShape{kind: projectile, pobOverlap: true, canShotgun: true, spreadDeg: 35, subHits: 4, subRadius: 20, scatter: 28}
	if h := expectedHits(sh, 5, 0, 0, 0, 0, 0, bossModel(BossLarge), 1); h[0] != 1 {
		t.Errorf("pobOverlap on the boss: %v, want 1", h)
	}
	for _, x := range expectedHits(sh, 5, 2, 0, 0, 0, 0, packModel(PackParams{}), 1) {
		if x > 1 {
			t.Errorf("pobOverlap on the pack: a mob gets %v hits, want <= 1", x)
		}
	}
}

func TestPackChain(t *testing.T) {
	sh := skillShape{kind: projectile}
	m := packModel(PackParams{})
	one := sumHits(expectedHits(sh, 1, 0, 0, 0, 0, 0, m, 1))
	chained := sumHits(expectedHits(sh, 1, 0, 3, 0, 0, 0, m, 1))
	forked := sumHits(expectedHits(sh, 1, 0, 0, 1, 0, 0, m, 1))
	if chained <= one || forked <= one {
		t.Errorf("pack: plain %v, chain 3 %v, fork 1 %v; want chain and fork to add hits", one, chained, forked)
	}
}

func TestKindsOnBoss(t *testing.T) {
	boss := bossModel(BossMedium)
	for _, k := range []deliveryKind{selfArea, targetArea, meleeStrike, slam, channel, beam, cone, singleHit} {
		if h := expectedHits(skillShape{kind: k}, 1, 0, 0, 0, 15, 1, boss, 1); h[0] != 1 {
			t.Errorf("kind %d on the boss: %v, want 1", k, h)
		}
	}
}

func TestKindsWalkIntoPack(t *testing.T) {
	// A Cyclone-like channel of radius 30 from the pack centre covers most
	// of a cluster of radius 30; from the pack's edge it hit 5.
	pack := packModel(PackParams{})
	if h := sumHits(expectedHits(skillShape{kind: channel}, 1, 0, 0, 0, 30, 0, pack, 1)); h < 6 {
		t.Errorf("channel of radius 30 in the pack: %v of 10, want >= 6", h)
	}
	if h := sumHits(expectedHits(skillShape{kind: selfArea}, 1, 0, 0, 0, 30, 0, pack, 1)); h < 6 {
		t.Errorf("nova of radius 30 in the pack: %v of 10, want >= 6", h)
	}
	// The boss keeps contact range: a channel of radius 10 reaches a Medium
	// boss only when the player stands r+10 from its centre.
	if h := expectedHits(skillShape{kind: channel}, 1, 0, 0, 0, 10, 0, bossModel(BossMedium), 1); h[0] != 1 {
		t.Errorf("channel of radius 10 on the boss: %v, want 1", h)
	}
}

func TestKindsOnPack(t *testing.T) {
	pack := packModel(PackParams{})
	if h := sumHits(expectedHits(skillShape{kind: singleHit}, 1, 0, 0, 0, 30, 5, pack, 1)); h != 1 {
		t.Errorf("single hit on the pack: %v, want 1", h)
	}
	if h := sumHits(expectedHits(skillShape{kind: meleeStrike}, 1, 0, 0, 0, 1000, 3, pack, 1)); h != 3 {
		t.Errorf("melee strike with 3 targets and long reach: %v, want 3", h)
	}
	if h := sumHits(expectedHits(skillShape{kind: meleeStrike}, 1, 0, 0, 0, 0, 1, pack, 1)); h != 1 {
		t.Errorf("melee strike: %v, want 1", h)
	}
	if h := sumHits(expectedHits(skillShape{kind: channel}, 1, 0, 0, 0, 1000, 0, pack, 1)); h != 10 {
		t.Errorf("huge channel area: %v, want all 10 mobs", h)
	}
	if h := sumHits(expectedHits(skillShape{kind: targetArea}, 1, 0, 0, 0, 25, 0, pack, 1)); h <= 1 {
		t.Errorf("target area of 25 on the pack centre: %v, want > 1", h)
	}
}

func TestSingleHitChains(t *testing.T) {
	// Vaal Arc: a non-projectile hit chains like a projectile.
	pack := packModel(PackParams{})
	if h := sumHits(expectedHits(skillShape{kind: singleHit}, 1, 0, 3, 0, 0, 0, pack, 1)); h != 4 {
		t.Errorf("single hit chaining 3 times in the pack: %v, want 4", h)
	}
	if h := expectedHits(skillShape{kind: singleHit}, 1, 0, 16, 0, 0, 0, bossModel(BossMedium), 1); h[0] != 1 {
		t.Errorf("chain on the boss alone: %v, want [1]", h)
	}
}

func TestProjectileExplosion(t *testing.T) {
	// Fireball: an explosion where the projectile stops hits the mobs
	// around it; the boss still takes one hit per cast.
	sh := skillShape{kind: projectile}
	pack := packModel(PackParams{})
	ray := sumHits(expectedHits(sh, 1, 0, 0, 0, 0, 0, pack, 1))
	boom := sumHits(expectedHits(sh, 1, 0, 0, 0, 20, 0, pack, 1))
	if boom <= ray || boom <= 1 {
		t.Errorf("pack: ray %v, with explosion radius 20 %v; want the explosion to add hits", ray, boom)
	}
	if h := expectedHits(sh, 5, 0, 0, 0, 20, 0, bossModel(BossMedium), 1); h[0] != 1 {
		t.Errorf("5 exploding projectiles on the boss: %v, want [1]", h)
	}
}

func TestEvenProjectilesHitAhead(t *testing.T) {
	// Two projectiles fly close together: the one in front is not missed.
	for _, n := range []float64{2, 4} {
		if h := expectedHits(skillShape{kind: projectile}, n, 0, 0, 0, 0, 0, bossModel(BossSmall), 1); h[0] != 1 {
			t.Errorf("%v projectiles on a small boss: %v, want [1]", n, h)
		}
	}
}

func TestPackCountCap(t *testing.T) {
	if p := (PackParams{Count: 500}).withDefaults(); p.Count != maxPackCount {
		t.Errorf("Count 500: %d, want %d", p.Count, maxPackCount)
	}
	if !validBossSize(BossLarge) || validBossSize(BossSize(9)) {
		t.Error("validBossSize")
	}
	if m := bossModel(BossSize(9)); m.targets[0].r != 20 {
		t.Errorf("unknown boss size: radius %v, want medium 20", m.targets[0].r)
	}
}
