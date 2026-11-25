package engine

import (
	"math"
	"math/rand/v2"
)

// Hit geometry for combat DPS (spec "DPS в бою", section "Модель цели"):
// how many hits one use of the main skill lands on each target. Distances
// are PoB units, 10 = 1 metre. The player stands at (0, 0) facing +x.

// BossSize picks the boss hitbox preset. The zero value is BossMedium, the
// default.
type BossSize int

const (
	BossMedium BossSize = iota // radius 20 (2 m), the default
	BossSmall                  // radius 10 (1 m)
	BossLarge                  // radius 40 (4 m)
)

// PackParams describes a pack of mobs. Zero fields take the defaults: 10
// mobs of radius 4 spread uniformly over a cluster of radius 30 whose centre
// is 60 in front of the player.
type PackParams struct {
	Count     int     // mobs in the pack
	Radius    float64 // cluster radius the mob centres fall in
	MobRadius float64 // hitbox radius of each mob
	Distance  float64 // distance from the player to the cluster centre
}

const (
	bossDistance       = 40                 // boss centre for ranged and spell shapes
	meleeGap           = 10                 // gap between a melee player and the nearest target's hitbox
	defaultPackCount   = 10                 // mobs in the default pack
	defaultPackRadius  = 30                 // ~3 m cluster
	defaultMobRadius   = 4                  // a trash mob hitbox
	defaultPackDist    = 60                 // pack centre distance
	geomTrials         = 4000               // Monte Carlo trials: the brief's count, ~1% noise on a mean
	pcgStream          = 0x9e3779b97f4a7c15 // PCG stream constant from the brief
	packSeed           = 1                  // fixed seed of the pack layout: packModel takes no seed
	aimScatter         = 5                  // aim point spread per trial for ranged shapes
	chainRange         = 40                 // max gap between hitboxes a chain or fork reaches (approximation)
	defaultFanDeg      = 60                 // fan width of several projectiles without a shotgun support's spread
	coneHalfDeg        = 45                 // half-angle of a cone shape (approximation)
	packPlacementTries = 100                // rejection tries before a mob may overlap another
)

// target is a circular hitbox, PoB units.
type target struct{ x, y, r float64 }

// targetModel is what the skill is used against.
type targetModel struct {
	targets  []target
	distance float64 // aim point distance for ranged/spell shapes
	// inside: a pack the player walks into: self-centred and melee kinds
	// stand at the cluster centre (distance, 0) instead of at its edge.
	inside bool
}

// bossModel is one boss at (40, 0); melee shapes walk up to it (see stance).
func bossModel(size BossSize) targetModel {
	r := 20.0
	switch size {
	case BossSmall:
		r = 10
	case BossLarge:
		r = 40
	}
	return targetModel{targets: []target{{bossDistance, 0, r}}, distance: bossDistance}
}

// packModel lays the mobs out uniformly in the cluster circle, without
// overlapping hitboxes where possible. The layout is fixed per PackParams.
func packModel(p PackParams) targetModel {
	p = p.withDefaults()
	rng := rand.New(rand.NewPCG(packSeed, pcgStream))
	ts := make([]target, 0, p.Count)
	for range p.Count {
		var x, y float64
		for try := 0; try < packPlacementTries; try++ {
			x, y = uniformInDisk(rng, p.Distance, 0, p.Radius)
			if !overlapsAny(x, y, p.MobRadius, ts) {
				break
			}
		}
		ts = append(ts, target{x, y, p.MobRadius})
	}
	return targetModel{targets: ts, distance: p.Distance, inside: true}
}

// withDefaults fills the zero fields with the default pack.
func (p PackParams) withDefaults() PackParams {
	if p.Count <= 0 {
		p.Count = defaultPackCount
	}
	if p.Radius <= 0 {
		p.Radius = defaultPackRadius
	}
	if p.MobRadius <= 0 {
		p.MobRadius = defaultMobRadius
	}
	if p.Distance <= 0 {
		p.Distance = defaultPackDist
	}
	return p
}

func overlapsAny(x, y, r float64, ts []target) bool {
	for _, t := range ts {
		if circleHits(x, y, r, t) {
			return true
		}
	}
	return false
}

// expectedHits returns, per target, the mean number of hits one cast lands,
// over geomTrials Monte Carlo trials seeded by seed. radius is the area
// radius for area shapes and the weapon range (reach) for meleeStrike;
// strikeTargets is the number of targets a meleeStrike hits (at least 1).
//
// Per kind:
//   - projectile: a fan of rays from the player towards the aim point.
//     Without canShotgun a target takes at most one projectile hit per cast;
//     pierce continues the ray, fork (two projectiles) and chain go to the
//     nearest not-yet-hit target within chainRange. Sub-hits with a radius
//     (Kinetic Blast) explode around the impact point and only the
//     explosions count; sub-hits without one (Tornado Shot) are secondary
//     projectiles from the impact point in random directions that cannot hit
//     what the primary hit.
//   - selfArea, channel: an area of radius around the player.
//   - slam: an area centred meleeGap in front of the player.
//   - targetArea: an area at the aim point.
//   - cone: a sector of radius and coneHalfDeg in front of the player.
//   - beam: an unbounded line towards the aim point hitting everything on it.
//   - meleeStrike: the strikeTargets nearest targets within reach.
//   - singleHit: one hit on the target nearest the player.
//
// Melee-range kinds (selfArea, channel, slam, cone, meleeStrike) put the
// player meleeGap from the nearest target's hitbox (r+10 from the boss
// centre); in a pack selfArea, channel, slam and meleeStrike stand at the
// cluster centre instead. The rest stay at (0, 0).
// With pobOverlap the engine's dpsMultiplier already counts the overlap, so
// each target counts at most one hit per cast, without shotgun or sub-hits.
func expectedHits(sh skillShape, projectiles, pierce, chain, fork, radius, strikeTargets float64, m targetModel, seed uint64) []float64 {
	out := make([]float64, len(m.targets))
	if len(m.targets) == 0 {
		return out
	}
	if sh.pobOverlap {
		sh.canShotgun, sh.subHits = false, 0
	}
	s := &hitSim{
		sh: sh, ts: m.targets, aim: m.distance, inside: m.inside, radius: radius,
		rng:    rand.New(rand.NewPCG(seed, pcgStream)),
		pierce: int(pierce), chain: int(chain), fork: int(fork),
		hits: make([]float64, len(m.targets)),
	}
	n := max(int(math.Round(projectiles)), 1)
	strikes := max(int(strikeTargets), 1)
	for range geomTrials {
		clear(s.hits)
		s.cast(n, strikes)
		for i, h := range s.hits {
			if sh.pobOverlap {
				h = min(h, 1)
			}
			out[i] += h
		}
	}
	for i := range out {
		out[i] /= geomTrials
	}
	return out
}

// hitSim runs one trial at a time, adding the trial's hits to hits.
type hitSim struct {
	sh                  skillShape
	ts                  []target
	aim, radius         float64
	inside              bool
	rng                 *rand.Rand
	pierce, chain, fork int
	hits                []float64
}

func (s *hitSim) cast(projectiles, strikes int) {
	switch s.sh.kind {
	case projectile:
		s.projectiles(projectiles)
	case selfArea, channel:
		px, py, _, _ := s.stance(true)
		s.area(px, py)
	case slam:
		px, py, dx, dy := s.stance(true)
		s.area(px+dx*meleeGap, py+dy*meleeGap)
	case targetArea:
		s.area(uniformInDisk(s.rng, s.aim, 0, aimScatter))
	case cone:
		px, py, dx, dy := s.stance(false)
		for i, t := range s.ts {
			if inCone(px, py, dx, dy, s.radius, t) {
				s.hits[i]++
			}
		}
	case beam:
		ax, ay := uniformInDisk(s.rng, s.aim, 0, aimScatter)
		d := math.Hypot(ax, ay)
		for i, t := range s.ts {
			if _, ok := rayEntry(0, 0, ax/d, ay/d, t); ok {
				s.hits[i]++
			}
		}
	case meleeStrike:
		s.strike(strikes)
	default: // singleHit
		s.hits[s.nearest(0, 0)]++
	}
}

// nearest is the target whose hitbox is closest to (x, y).
func (s *hitSim) nearest(x, y float64) int {
	best, bd := 0, math.Inf(1)
	for i, t := range s.ts {
		if d := math.Hypot(t.x-x, t.y-y) - t.r; d < bd {
			best, bd = i, d
		}
	}
	return best
}

// stance places a melee player and faces the nearest target; it returns
// the position and the unit direction. With walkIn in a pack the player
// stands at the cluster centre, else meleeGap from the nearest hitbox.
func (s *hitSim) stance(walkIn bool) (px, py, dx, dy float64) {
	inPack := walkIn && s.inside
	if inPack {
		px = s.aim
	}
	t := s.ts[s.nearest(px, py)]
	if d := math.Hypot(t.x, t.y); !inPack && d > t.r+meleeGap {
		k := 1 - (t.r+meleeGap)/d
		px, py = t.x*k, t.y*k
	}
	vx, vy := t.x-px, t.y-py
	if d := math.Hypot(vx, vy); d > 0 {
		return px, py, vx / d, vy / d
	}
	return px, py, 1, 0
}

// strike hits the n targets nearest the melee player within reach; the
// nearest one is always in reach because the player walked up to it.
func (s *hitSim) strike(n int) {
	px, py, _, _ := s.stance(true)
	reach := max(s.radius, meleeGap)
	done := make([]bool, len(s.ts))
	for k := 0; k < n; k++ {
		best, bd := -1, math.Inf(1)
		for i, t := range s.ts {
			if d := math.Hypot(t.x-px, t.y-py) - t.r; !done[i] && d < bd {
				best, bd = i, d
			}
		}
		if best < 0 || (k > 0 && bd > reach) {
			return
		}
		done[best] = true
		s.hits[best]++
	}
}

// area lands one area of radius at (x, y), or the shape's sub-hits around it.
func (s *hitSim) area(x, y float64) {
	if s.sh.subHits > 0 && s.sh.subRadius > 0 {
		s.subHitsAround(x, y)
		return
	}
	for i, t := range s.ts {
		if circleHits(x, y, s.radius, t) {
			s.hits[i]++
		}
	}
}

// subHitsAround scatters the sub-hits uniformly within sh.scatter of (x, y);
// a fractional sub-hit count weighs the last one by its fraction.
func (s *hitSim) subHitsAround(x, y float64) {
	for _, w := range weights(s.sh.subHits) {
		ex, ey := uniformInDisk(s.rng, x, y, s.sh.scatter)
		for i, t := range s.ts {
			if circleHits(ex, ey, s.sh.subRadius, t) {
				s.hits[i] += w
			}
		}
	}
}

// weights splits a count into whole units and a trailing fraction.
func weights(n float64) []float64 {
	whole := math.Floor(n)
	w := make([]float64, int(whole), int(whole)+1)
	for i := range w {
		w[i] = 1
	}
	if f := n - whole; f > 0 {
		w = append(w, f)
	}
	return w
}

// projectiles fires n projectiles in a fan centred on a scattered aim point.
func (s *hitSim) projectiles(n int) {
	ax, ay := uniformInDisk(s.rng, s.aim, 0, aimScatter)
	flight := math.Hypot(ax, ay)
	base := math.Atan2(ay, ax)
	fan := s.sh.spreadDeg
	if fan == 0 {
		fan = defaultFanDeg
	}
	shared := make([]bool, len(s.ts))
	for k := range n {
		ang := base
		if n > 1 {
			ang += fan * math.Pi / 180 * (float64(k)/float64(n-1) - 0.5)
		}
		hit := shared
		if s.sh.canShotgun {
			hit = make([]bool, len(s.ts))
		}
		dx, dy := math.Cos(ang), math.Sin(ang)
		switch {
		case s.sh.subHits > 0 && s.sh.subRadius > 0:
			// Explodes at the first target it can hit, else at the end of
			// its flight; the explosions are the hits.
			ex, ey := dx*flight, dy*flight
			if i, d := s.firstOnRay(0, 0, dx, dy, flight, hit); i >= 0 {
				hit[i] = true
				ex, ey = dx*d, dy*d
			}
			s.subHitsAround(ex, ey)
		case s.sh.subHits > 0:
			// Splits where it stops or at the aim point; the secondary
			// projectiles share its hit list, so they skip what it hit.
			ex, ey := s.travel(0, 0, dx, dy, flight, 1, hit)
			for _, w := range weights(s.sh.subHits) {
				a := 2 * math.Pi * s.rng.Float64()
				s.travel(ex, ey, math.Cos(a), math.Sin(a), math.Inf(1), w, hit)
			}
		default:
			s.travel(0, 0, dx, dy, math.Inf(1), 1, hit)
		}
	}
}

// travel moves a projectile along the ray up to maxLen, hitting targets not
// in hit with weight w, piercing, then forking or chaining. It returns where
// the projectile stopped.
func (s *hitSim) travel(x, y, dx, dy, maxLen, w float64, hit []bool) (float64, float64) {
	pierce := s.pierce
	for {
		i, d := s.firstOnRay(x, y, dx, dy, maxLen, hit)
		if i < 0 {
			if math.IsInf(maxLen, 1) {
				return x, y
			}
			return x + dx*maxLen, y + dy*maxLen
		}
		hit[i] = true
		s.hits[i] += w
		x, y, maxLen = x+dx*d, y+dy*d, maxLen-d
		if pierce > 0 {
			pierce--
			continue
		}
		s.afterHit(i, s.chain, s.fork, w, hit)
		return x, y
	}
}

// afterHit forks (two projectiles) or else chains from target i to the
// nearest targets not in hit within chainRange.
func (s *hitSim) afterHit(i, chain, fork int, w float64, hit []bool) {
	if fork > 0 {
		for range 2 {
			j := s.nextInRange(i, hit)
			if j < 0 {
				return
			}
			hit[j] = true
			s.hits[j] += w
			s.afterHit(j, chain, fork-1, w, hit)
		}
		return
	}
	if chain > 0 {
		if j := s.nextInRange(i, hit); j >= 0 {
			hit[j] = true
			s.hits[j] += w
			s.afterHit(j, chain-1, 0, w, hit)
		}
	}
}

// nextInRange is the target nearest target i not in hit whose hitbox is
// within chainRange of i's, or -1.
func (s *hitSim) nextInRange(i int, hit []bool) int {
	from := s.ts[i]
	best, bd := -1, float64(chainRange)
	for j, t := range s.ts {
		if hit[j] {
			continue
		}
		if d := math.Hypot(t.x-from.x, t.y-from.y) - t.r - from.r; d <= bd {
			best, bd = j, d
		}
	}
	return best
}

// firstOnRay is the nearest target not in hit the ray enters within maxLen,
// with the distance to its hitbox, or -1.
func (s *hitSim) firstOnRay(x, y, dx, dy, maxLen float64, hit []bool) (int, float64) {
	best, bd := -1, maxLen
	for i, t := range s.ts {
		if hit[i] {
			continue
		}
		if d, ok := rayEntry(x, y, dx, dy, t); ok && d <= bd {
			best, bd = i, d
		}
	}
	return best, bd
}

// rayEntry is the distance along the unit ray from (x, y) to where it enters
// t's hitbox (0 when it starts inside), and whether it meets it at all.
func rayEntry(x, y, dx, dy float64, t target) (float64, bool) {
	cx, cy := t.x-x, t.y-y
	tca := cx*dx + cy*dy
	d2 := cx*cx + cy*cy - tca*tca
	if d2 > t.r*t.r {
		return 0, false
	}
	thc := math.Sqrt(t.r*t.r - d2)
	if tca+thc < 0 {
		return 0, false
	}
	return max(tca-thc, 0), true
}

// inCone reports whether t's hitbox reaches into the sector of radius r and
// half-angle coneHalfDeg from (px, py) along the unit direction (dx, dy).
func inCone(px, py, dx, dy, r float64, t target) bool {
	vx, vy := t.x-px, t.y-py
	d := math.Hypot(vx, vy)
	if d-t.r > r {
		return false
	}
	if d <= t.r {
		return true
	}
	ang := math.Acos(math.Max(-1, math.Min(1, (vx*dx+vy*dy)/d)))
	return ang-math.Asin(t.r/d) <= coneHalfDeg*math.Pi/180
}

// circleHits reports whether a circle of radius r at (x, y) touches t.
func circleHits(x, y, r float64, t target) bool {
	return math.Hypot(t.x-x, t.y-y) <= r+t.r
}

// uniformInDisk draws a point uniformly from the disk of radius r at (x, y).
func uniformInDisk(rng *rand.Rand, x, y, r float64) (float64, float64) {
	rho := r * math.Sqrt(rng.Float64())
	a := 2 * math.Pi * rng.Float64()
	return x + rho*math.Cos(a), y + rho*math.Sin(a)
}
