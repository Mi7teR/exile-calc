package engine

import (
	"fmt"
	"math"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// Combat DPS (spec "DPS в бою", section "Расчёт"): the main skill's DPS
// against one boss and against a pack, from PoB's per-hit outputs plus the
// hit geometry, the sources (mines, traps, totems, ballistas) and the DoT
// stacking rules. Calc and its outputs are left as they are.

// CombatOptions sets the boss and the pack. The zero value takes the
// defaults: a medium boss, the default pack, seed 0.
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
	// Components: "Hit", "Ignite", "Poison", "Bleed", "Impale", "Dot".
	Components map[string]float64
	// HitsPerCast is the hits one use lands, summed over the targets.
	HitsPerCast float64
}

// CombatResult holds combat DPS against the boss and against the pack.
type CombatResult struct {
	Boss, Pack  CombatDPS
	Skill       string // the main skill's gem name
	Assumptions []string
}

// combatComponents maps each CombatDPS component to the PoB output it
// starts from. Ignite takes TotalIgniteDPS where PoB sets it (stacking
// ignite), as combinedDPS does.
var combatComponents = []struct{ name, out string }{
	{"Hit", "TotalDPS"},
	{"Ignite", "IgniteDPS"},
	{"Poison", "TotalPoisonDPS"},
	{"Bleed", "BleedDPS"},
	{"Impale", "ImpaleDPS"},
	{"Dot", "TotalDot"},
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
	boss := bossModel(opts.Boss)
	pack := opts.Pack.withDefaults()
	var bossNotes, packNotes []string
	r.Boss, bossNotes = combatDPS(bossRes, boss, true, opts.Seed)
	r.Pack, packNotes = combatDPS(packRes, packModel(pack), false, opts.Seed)

	bossName := map[BossSize]string{BossSmall: "маленький", BossMedium: "средний", BossLarge: "большой"}[opts.Boss]
	r.Assumptions = append(r.Assumptions,
		fmt.Sprintf("босс: %s, радиус хитбокса %.0f (%.1f м), конфиг врага из билда", bossName, boss.targets[0].r, boss.targets[0].r/10),
		fmt.Sprintf("пак: %d мобов радиуса %.0f в круге радиуса %.0f на дистанции %.0f, враг не босс (enemyIsBoss = None)",
			pack.Count, pack.MobRadius, pack.Radius, pack.Distance),
		fmt.Sprintf("попаданий за каст: по боссу %.2f, по паку %.2f (сумма по мобам)", r.Boss.HitsPerCast, r.Pack.HitsPerCast),
		"мобы бессмертны, сумма = пропускная способность",
	)
	r.Assumptions = append(r.Assumptions, bossNotes...)
	r.Assumptions = append(r.Assumptions, packNotes...)
	return r, nil
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
// capped per target at PoB's DoT DPS cap again. With pobOverlap PoB's
// dpsMultiplier already holds the overlap, so a boss the skill reaches
// counts one hit per use.
func combatDPS(res *Result, m targetModel, isBoss bool, seed uint64) (CombatDPS, []string) {
	s, out := res.main, res.Stats
	sh := shapeOf(s, out)
	src := sourcesOf(s, out)
	radius := out["AreaOfEffectRadius"]
	if sh.kind == meleeStrike {
		radius = out["WeaponRange"]
	}
	hits := expectedHits(sh, out["ProjectileCount"], out["PierceCount"], out["ChainMax"], out["ForkCountMax"],
		radius, out["StrikeTargets"], m, seed)
	if isBoss && sh.pobOverlap {
		hits[0] = 1
	}

	d := CombatDPS{PoB: out["CombinedDPS"], Components: map[string]float64{}}
	for _, h := range hits {
		d.HitsPerCast += h
		for _, cmp := range combatComponents {
			base := out[cmp.out]
			if cmp.name == "Ignite" {
				if v, ok := out["TotalIgniteDPS"]; ok {
					base = v
				}
			}
			if base == 0 {
				continue
			}
			v := base * src.sustain(cmp.out, s)
			if cmp.name == "Hit" {
				v *= h
			} else {
				v = math.Min(v*dotScale(cmp.out, h, s, out), dotDpsCap)
			}
			d.Components[cmp.name] += v
		}
	}
	for _, v := range d.Components {
		d.Sustained += v
	}
	// Burst: one use's hit damage is TotalDPS / hit rate, which keeps PoB's
	// dpsMultiplier once; hitsPerCast is 1 per target with pobOverlap.
	if src.kind != "" {
		if rate := hitSpeed(out); rate > 0 {
			d.Burst = out["TotalDPS"] / rate * d.HitsPerCast * src.burstHits
		}
	}

	if !isBoss {
		return d, nil
	}
	notes := []string{"форма: " + kindNames[sh.kind]}
	if sh.fromDefault {
		notes = append(notes, "форма по умолчанию (выведена из типов скилла, цифра грубая)")
	}
	if sh.pobOverlap {
		notes = append(notes, "перекрытие уже учтено PoB (dpsMultiplier): по боссу 1 попадание за каст, по мобу не больше одного")
		if sequentialProjectiles(s) {
			notes = append(notes, "последовательные снаряды (Barrage): геометрия под-ударов (взрывов) не применяется")
		}
	}
	if sh.subHits > 0 {
		notes = append(notes, fmt.Sprintf("под-удары: %.0f радиуса %.0f в разбросе %.0f", sh.subHits, sh.subRadius, sh.scatter))
	}
	if sh.canShotgun {
		notes = append(notes, fmt.Sprintf("шотган: снаряды веером %.0f° могут попасть в одну цель", sh.spreadDeg))
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
	notes = append(notes, "без culling, резервации, миражей (MirageDPS) и наземных DoT (caustic/burning ground)")
	return d, notes
}
