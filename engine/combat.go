package engine

import (
	"fmt"
	"github.com/Mi7teR/poe-lib/gamedata"
	"math"
	"strings"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// Combat DPS (spec "DPS в бою", section "Расчёт"): the main skill's DPS
// against one boss and against a pack, from PoB's per-hit outputs plus the
// hit geometry, the sources (mines, traps, totems, ballistas) and the DoT
// stacking rules. Calc and its outputs are left as they are.

// CombatOptions sets the boss and the pack. The zero value takes the
// defaults: a medium boss, the default pack, seed 0. An unknown Boss is a
// medium boss; Pack.Count is capped at 50.
type CombatOptions struct {
	Boss BossSize
	Pack PackParams
	Seed uint64 // Monte Carlo seed of the hit geometry
}

// CombatDPS is the combat DPS against one enemy setup.
type CombatDPS struct {
	PoB       float64 // PoB's CombinedDPS for the same enemy (boss or non-boss config)
	Sustained float64 // combat DPS: the sum of Components
	// Burst is the damage (not per second) of every active mine, trap,
	// totem or ballista going off at once; 0 for other skills.
	Burst float64
	// Components: the parts of PoB's CombinedDPS ("Hit", "Mirage",
	// "Impale", "Ignite", "Poison", "Bleed", "Dot", "Ground",
	// "CorruptingBlood", "Decay"), each with culling and reservation
	// applied; zero parts are left out.
	Components map[string]float64
	// HitsPerCast is the hits one use lands, summed over the targets.
	HitsPerCast float64
}

// CombatResult holds combat DPS against the boss and against the pack.
type CombatResult struct {
	Boss, Pack CombatDPS
	// PackPerMob is Pack averaged over the mobs of the pack: Sustained,
	// Burst, Components and HitsPerCast divided by the mob count. PoB stays
	// the pack run's CombinedDPS, which is already per target.
	PackPerMob  CombatDPS
	Skill       string // the main skill's gem name
	Assumptions []string
}

// combatComponents are the parts of PoB's CombinedDPS (combinedDPS in
// ailments.go), each with the PoB output it starts from. hit: scales with
// the hits like TotalDPS (the mirage repeats the skill). dot: one of the
// DoTs PoB sums, and caps, into TotalDotDPS. Impale scales by dotScale but
// is outside TotalDotDPS, as in PoB.
var combatComponents = []struct {
	name, out string
	hit, dot  bool
}{
	{"Hit", "TotalDPS", true, false},
	{"Mirage", "MirageDPS", true, false},
	{"Impale", "ImpaleDPS", false, false},
	{"Ignite", "IgniteDPS", false, true},
	{"Poison", "TotalPoisonDPS", false, true},
	{"Bleed", "BleedDPS", false, true},
	{"Dot", "TotalDot", false, true},
	{"Ground", "GroundDPS", false, true},
	{"CorruptingBlood", "CorruptingBloodDPS", false, true},
	{"Decay", "DecayDPS", false, true},
}

// componentBase is PoB's value of a component: Ignite takes TotalIgniteDPS
// where set (stacking ignite) and Ground the stronger of the player's and
// the mirage's caustic and burning ground, as combinedDPS does.
func componentBase(name, key string, out map[string]float64) float64 {
	switch name {
	case "Ignite":
		if v, ok := out["TotalIgniteDPS"]; ok {
			return v
		}
	case "Ground":
		return math.Max(out["CausticGroundDPS"], out["MirageCausticGroundDPS"]) +
			math.Max(out["BurningGroundDPS"], out["MirageBurningGroundDPS"])
	}
	return out[key]
}

// combinedMult is the culling and reservation multiplier combinedDPS
// applies to CombinedDPS last: the better of the player's and the mirage's
// culling, times ReservationDpsMultiplier.
func combinedMult(s *activeSkill, out map[string]float64) float64 {
	cull, ok := out["CullMultiplier"]
	if !ok {
		cull = 1
	}
	if s.mirage != nil {
		if v, ok := s.mirage.out["TotalDPS"]; ok && v >= 0 {
			cull = math.Max(cull, math.Max(1, s.mirage.out["CullMultiplier"]))
		}
	}
	res, ok := out["ReservationDpsMultiplier"]
	if !ok {
		res = 1
	}
	return cull * res
}

var kindNames = map[deliveryKind]string{
	selfArea: "область вокруг игрока", targetArea: "область в точке", projectile: "снаряды",
	meleeStrike: "удар в ближнем бою", slam: "слэм", channel: "поддерживаемый", beam: "луч",
	cone: "конус", singleHit: "одиночный хит",
}

// Combat computes combat DPS for the build's main skill. The boss run is
// Calc on the build as it is; the pack run is Calc with enemyIsBoss set to
// "None" on a copy of the active config set. b is not modified.
func (c *Calculator) Combat(b *pobcode.Build, opts CombatOptions) (*CombatResult, error) {
	bossRes, err := c.Calc(b)
	if err != nil {
		return nil, err
	}
	r := &CombatResult{}
	s := bossRes.main
	if s == nil {
		r.Assumptions = []string{"нет главного скилла: DPS в бою не считается"}
		return r, nil
	}
	packRes, err := c.Calc(nonBossBuild(b))
	if err != nil {
		return nil, err
	}
	r.Skill = s.active.ge.Name
	r.Boss.PoB, r.Pack.PoB = bossRes.Stats["CombinedDPS"], packRes.Stats["CombinedDPS"]
	r.PackPerMob.PoB = r.Pack.PoB
	// A skill whose damage is its minions' (Dominating Blow strikes itself,
	// so its CombinedDPS is not 0) or that deals none: PoB's player
	// CombinedDPS is 0 and so is combat DPS.
	if r.Boss.PoB == 0 && r.Pack.PoB == 0 {
		if s.flags["minion"] {
			r.Assumptions = []string{"главный скилл — миньоны: DPS в бою для миньонов не считается"}
		} else {
			r.Assumptions = []string{"у главного скилла CombinedDPS PoB = 0 (скилл не наносит урона): DPS в бою 0"}
		}
		return r, nil
	}
	size := opts.Boss
	if !validBossSize(size) {
		r.Assumptions = append(r.Assumptions, fmt.Sprintf("неизвестный размер босса %d: взят средний", size))
		size = BossMedium
	}
	if opts.Pack.Count > maxPackCount {
		r.Assumptions = append(r.Assumptions, fmt.Sprintf("мобов в паке %d: ограничено %d", opts.Pack.Count, maxPackCount))
	}
	boss := bossModel(size)
	pack := opts.Pack.withDefaults()
	var bossNotes, packNotes []string
	r.Boss, bossNotes = combatDPS(bossRes, boss, true, opts.Seed)
	pm := packModel(pack)
	spread := contagionSpread{}
	if spreadByContagion[r.Skill] {
		spread = c.contagionOf(nonBossBuild(b))
	}
	r.Pack, packNotes = combatDPSSpread(packRes, pm, false, opts.Seed, spread)
	r.PackPerMob = perMob(r.Pack, float64(pack.Count))

	bossName := map[BossSize]string{BossSmall: "маленький", BossMedium: "средний", BossLarge: "большой"}[size]
	r.Assumptions = append(r.Assumptions,
		fmt.Sprintf("босс: %s, радиус хитбокса %.0f (%.1f м), конфиг врага из билда", bossName, boss.targets[0].r, boss.targets[0].r/10),
		fmt.Sprintf("пак: %d мобов радиуса %.0f в круге радиуса %.0f на дистанции %.0f, враг не босс (enemyIsBoss = None)",
			pack.Count, pack.MobRadius, pack.Radius, pack.Distance),
		fmt.Sprintf("попаданий за каст: по боссу %.2f, по паку %.2f (сумма по мобам)", r.Boss.HitsPerCast, r.Pack.HitsPerCast),
		"мобы бессмертны, сумма = пропускная способность",
		fmt.Sprintf("в среднем на моба = сумма / %d (устойчивый DPS и бурст пака)", pack.Count),
	)
	if spread.radius > 0 {
		r.Assumptions = append(r.Assumptions, fmt.Sprintf(
			"пак: %s разносит DoT %s с цели на мобов в радиусе %.0f (%.1f м); считается, что разнос уже случился (мобы бессмертны)",
			spread.name, r.Skill, spread.radius, spread.radius/10))
	}
	r.Assumptions = append(r.Assumptions, bossNotes...)
	r.Assumptions = append(r.Assumptions, packNotes...)
	return r, nil
}

// perMob is d divided over n mobs; PoB, already per target, is kept.
func perMob(d CombatDPS, n float64) CombatDPS {
	m := CombatDPS{PoB: d.PoB, Sustained: d.Sustained / n, Burst: d.Burst / n,
		HitsPerCast: d.HitsPerCast / n, Components: map[string]float64{}}
	for k, v := range d.Components {
		m.Components[k] = v / n
	}
	return m
}

// areaKind reports whether a kind lands an area of AreaOfEffectRadius.
func areaKind(k deliveryKind) bool {
	switch k {
	case selfArea, targetArea, channel, slam, cone:
		return true
	}
	return false
}

// nonBossBuild is a shallow copy of b whose active config set is a copy
// with enemyIsBoss = "None"; nothing b points to is changed.
func nonBossBuild(b *pobcode.Build) *pobcode.Build {
	nb := *b
	nb.ConfigSets = append([]pobcode.ConfigSet(nil), b.ConfigSets...)
	none := pobcode.Input{Name: "enemyIsBoss", Kind: pobcode.KindString, String: "None"}
	set := nb.ActiveConfig()
	if set == nil {
		nb.ConfigSets = append(nb.ConfigSets, pobcode.ConfigSet{ID: nb.ActiveConfigSetID, Inputs: []pobcode.Input{none}})
		return &nb
	}
	inputs := make([]pobcode.Input, 0, len(set.Inputs)+1)
	replaced := false
	for _, in := range set.Inputs {
		if in.Name == none.Name {
			in, replaced = none, true
		}
		inputs = append(inputs, in)
	}
	if !replaced {
		inputs = append(inputs, none)
	}
	set.Inputs = inputs
	return &nb
}

// combatDPS is combat DPS of res's main skill against the targets of m.
// notes are the assumptions of this run (the shape notes only for the boss
// run, which shares them with the pack run).
//
// Per target with h expected hits per use, each component is PoB's value
// × scale × the sources' sustain multiplier: hits scale with h, DoTs with
// dotScale(h) (stacking DoTs with h, the strongest-instance ones with
// min(h, 1), so an area DoT counts once per mob it touches). DoTs are
// capped per target at PoB's DoT DPS cap again, one by one and summed, and
// every component takes PoB's culling and reservation multiplier, so a
// skill whose shape adds nothing gets CombinedDPS. With pobOverlap PoB's
// dpsMultiplier already holds the overlap, so a boss the skill reaches
// counts one hit per use. On the pack the overlap is taken back out: the
// parts that scale with the hits start from PoB's value / dpsMultiplier (one
// projectile or pod) times the geometric hits on each mob.
//
// An area kind with no radius (AreaOfEffectRadius 0, the engine does not
// know it) is counted as a single hit rather than an area that touches
// nothing.
func combatDPS(res *Result, m targetModel, isBoss bool, seed uint64) (CombatDPS, []string) {
	return combatDPSSpread(res, m, isBoss, seed, contagionSpread{})
}

// combatDPSSpread is combatDPS where the main skill's own DoT (TotalDot)
// also reaches every mob Contagion spreads it to (spread.radius > 0).
func combatDPSSpread(res *Result, m targetModel, isBoss bool, seed uint64, spread contagionSpread) (CombatDPS, []string) {
	s, out := res.main, res.Stats
	sh := shapeOf(s, out)
	src := sourcesOf(s, out)
	radius := out["AreaOfEffectRadius"]
	if sh.kind == meleeStrike {
		radius = out["WeaponRange"]
	}
	// A projectile hits an area only when the gem has an area radius stat
	// (Fireball's explosion, Ball Lightning's bolts). s.flags["area"] and the
	// Area skill type are no guide: the engine sets the flag for any skill
	// with a radius, and Essence Drain has both, yet its projectile only
	// applies the debuff to what it hits (PoB's skill("radius", 8)).
	if sh.kind == projectile && !hasAreaRadiusStat(s.active.ge) {
		radius = 0
	}
	noRadius := areaKind(sh.kind) && radius <= 0 && (sh.subHits <= 0 || sh.subRadius <= 0)
	if noRadius {
		sh.kind = singleHit
	}
	unoverlap := 1.0
	if !isBoss && sh.pobOverlap {
		if dm := numOr(s.data["dpsMultiplier"], 1); dm > 0 {
			unoverlap = 1 / dm
		}
	}
	geo := sh
	if !isBoss && sh.pobOverlap {
		// The overlap PoB counts is several projectiles of one use hitting
		// one target; on the pack each projectile hits on its own, from one
		// projectile's damage (unoverlap).
		geo.pobOverlap, geo.subHits = false, 0
		geo.canShotgun = sh.kind == projectile
	}
	hits := expectedHits(geo, out["ProjectileCount"], out["PierceCount"], out["ChainMax"], out["ForkCountMax"],
		radius, out["StrikeTargets"], m, seed)
	if isBoss && sh.pobOverlap {
		hits[0] = 1
	}

	// ovs is each target's overlap factor for hits and stacking DoTs. On the
	// pack each projectile counts once (unoverlap), but the most-hit mob is
	// the target PoB's dpsMultiplier describes, so it gets at least PoB's
	// single-target value: the pack sum never drops below PoB when anything
	// is hit, and shapes whose geometry cannot repeat hits (singleHit,
	// one area per use) are not cut to PoB / dpsMultiplier.
	ovs := make([]float64, len(hits))
	top := 0
	for i, h := range hits {
		ovs[i] = unoverlap
		if h > hits[top] {
			top = i
		}
	}
	if unoverlap < 1 && hits[top] > 0 {
		if h := hits[top]; h*unoverlap < math.Min(h, 1) {
			ovs[top] = math.Min(h, 1) / h
		}
	}

	// Contagion spreads the debuff from the main target (the most-hit mob)
	// to the mobs within its radius: their skill DoT counts as applied.
	spreadTo := make([]bool, len(hits))
	if spread.radius > 0 && len(hits) > 0 && hits[top] > 0 {
		c := m.targets[top]
		for j, t := range m.targets {
			if math.Hypot(t.x-c.x, t.y-c.y) <= spread.radius+t.r {
				spreadTo[j] = true
			}
		}
	}

	d := CombatDPS{PoB: out["CombinedDPS"], Components: map[string]float64{}}
	mult := combinedMult(s, out)
	vals := make([]float64, len(combatComponents))
	effHits := 0.0 // sum of hits × overlap factor, for the burst
	for t, h := range hits {
		d.HitsPerCast += h
		effHits += h * ovs[t]
		dots := 0.0
		for i, cmp := range combatComponents {
			v := componentBase(cmp.name, cmp.out, out) * src.sustain(cmp.out, s)
			if cmp.hit || stackingDot(cmp.out, s) {
				v *= ovs[t]
			}
			switch {
			case cmp.hit:
				v *= h
			case cmp.dot:
				hd := h
				if cmp.out == "TotalDot" && spreadTo[t] {
					hd = math.Max(h, 1)
				}
				v = math.Min(v*dotScale(cmp.out, hd, s, out), dotDpsCap)
			default: // Impale: PoB does not cap it
				v *= dotScale(cmp.out, h, s, out)
			}
			if cmp.dot {
				dots += v
			}
			vals[i] = v
		}
		// PoB caps the DoT sum (TotalDotDPS) per enemy too.
		capScale := 1.0
		if dots > dotDpsCap {
			capScale = dotDpsCap / dots
		}
		for i, cmp := range combatComponents {
			v := vals[i] * mult
			if cmp.dot {
				v *= capScale
			}
			if v != 0 {
				d.Components[cmp.name] += v
			}
		}
	}
	for _, v := range d.Components {
		d.Sustained += v
	}
	// Burst: one use's hit damage is TotalDPS / hit rate, which keeps PoB's
	// dpsMultiplier once (taken out on the pack like the sustained hits);
	// hitsPerCast is 1 per target with pobOverlap. Culling and reservation
	// apply as for Sustained.
	if src.kind != "" {
		if rate := hitSpeed(out); rate > 0 {
			d.Burst = out["TotalDPS"] / rate * effHits * src.burstHits * mult
		}
	}

	if !isBoss {
		return d, nil
	}
	notes := []string{"форма: " + kindNames[sh.kind]}
	if noRadius {
		notes = append(notes, "радиус области неизвестен (AreaOfEffectRadius 0): считается как одиночный хит")
	}
	if sh.fromDefault {
		notes = append(notes, "форма по умолчанию (выведена из типов скилла, цифра грубая)")
	}
	if sh.pobOverlap {
		notes = append(notes, "перекрытие уже учтено PoB (dpsMultiplier): по боссу 1 попадание за каст; на паке урон одного снаряда (DPS PoB / dpsMultiplier) × попадания по мобу")
		if sequentialProjectiles(s) {
			notes = append(notes, "последовательные снаряды (Barrage): геометрия под-ударов (взрывов) не применяется")
		}
	}
	if sh.subHits > 0 && sh.subRadius > 0 && !sh.pobOverlap {
		proj := max(math.Round(out["ProjectileCount"]), 1)
		notes = append(notes, fmt.Sprintf("по боссу попадают %.1f из %.0f взрывов за каст (%.0f снарядов × %.0f взрывов радиуса %.0f в разбросе %.0f)",
			hits[0], proj*sh.subHits, proj, sh.subHits, sh.subRadius, sh.scatter))
	} else if sh.subHits > 0 && !sh.pobOverlap {
		notes = append(notes, fmt.Sprintf("вторичные снаряды: %.0f с каждой точки попадания", sh.subHits))
	}
	if sh.canShotgun {
		notes = append(notes, fmt.Sprintf("шотган: взрывы снарядов веером %.0f° могут попасть в одну цель", sh.spreadDeg))
	}
	if sh.kind == projectile && sh.subHits == 0 && radius > 0 {
		notes = append(notes, fmt.Sprintf("снаряд взрывается при попадании (радиус %.0f): цель за каст поражается не больше одного раза", radius))
	}
	if src.kind != "" {
		srcName := map[string]string{"mine": "мины", "trap": "ловушки", "totem": "тотемы", "ballista": "баллисты"}[src.kind]
		notes = append(notes, fmt.Sprintf("источники: %s, активных %.0f; бурст = урон всех %.0f разом (урон, не в секунду), только хиты", srcName, src.active, src.burstHits))
		if src.kind == "mine" || src.kind == "trap" {
			notes = append(notes, "устойчивый DPS ограничен скоростью установки/броска")
		} else {
			notes = append(notes, fmt.Sprintf("устойчивый DPS: хиты и стакающиеся DoT × %.0f, сильнейший DoT один", src.sustainMul))
		}
	}
	if truthy(s.data["showAverage"]) {
		notes = append(notes, "PoB показывает для скилла средний урон (showAverage), а не DPS: цифра PoB не сравнима")
	}
	notes = append(notes, fmt.Sprintf("состав как у CombinedDPS PoB: миражи как хиты, наземные DoT по числу задетых целей, culling и резервация × %.2f", mult))
	return d, notes
}

// hasAreaRadiusStat reports whether the gem's own stats give it an area
// (active_skill_base_area_of_effect_radius), as opposed to a radius PoB
// hard-codes for display.
func hasAreaRadiusStat(ge *gamedata.Skill) bool {
	const stat = "active_skill_base_area_of_effect_radius"
	if contains(ge.Stats, stat) {
		return true
	}
	for _, cs := range ge.ConstantStats {
		if cs.Stat == stat {
			return true
		}
	}
	return false
}

// spreadByContagion are the skills whose damaging debuff Contagion spreads
// (gem descriptions: "The debuff is spread by Contagion"; Essence Drain of
// Wickedness lacks the line).
var spreadByContagion = map[string]bool{
	"Essence Drain": true, "Essence Drain of Desperation": true, "Blight of Contagion": true,
}

// contagionSpread is a Contagion skill in the build: the radius it spreads
// debuffs over (0 = none) and its name.
type contagionSpread struct {
	radius float64
	name   string
}

// contagionOf finds an enabled Contagion in b's active skill set and
// returns its area radius, from a Calc with that skill as the main skill.
// Contagion and Contagion of Subsiding spread when an affected enemy dies,
// Contagion of Transference when it is hit; on the immortal pack both are
// taken as having spread.
func (c *Calculator) contagionOf(b *pobcode.Build) contagionSpread {
	set := b.ActiveSkills()
	if set == nil {
		return contagionSpread{}
	}
	for gi, g := range set.Groups {
		if !g.Enabled || gi+1 == b.MainSocketGroup {
			continue
		}
		for k := 1; k <= len(g.Gems); k++ {
			res, err := c.Calc(withMainSkill(b, gi+1, k))
			if err != nil || res.main == nil || !strings.HasPrefix(res.main.active.ge.Name, "Contagion") {
				continue
			}
			if r := res.Stats["AreaOfEffectRadius"]; r > 0 {
				return contagionSpread{radius: r, name: res.main.active.ge.Name}
			}
		}
	}
	return contagionSpread{}
}
