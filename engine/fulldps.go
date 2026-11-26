package engine

import (
	"math"
	"strings"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// SkillDPS is one entry of PoB's output.SkillDPS (calcs.calcFullDPS
// fullDPS.skills): a skill, a minion or a mirage counted in the Full DPS,
// or one of the totals added after them ("Best Ignite DPS", "Full Poison
// DPS", ...), which have no group. Name carries the line PoB shows under
// the entry (its skillPart) in parentheses: "Static Strike (Beams)",
// "Raise Zombie (Raised Zombie: Default Attack)".
type SkillDPS struct {
	Name              string  // skill name as PoB shows it (with part name when the skill has parts)
	Group, GroupSkill int     // 1-based, as SkillInfo (the input build's groups)
	Count             float64 // PoB fullDPS.skills[i].count
	DPS               float64 // combined DPS of one instance
	Trigger           string  // PoB trigger label, "" if none
}

// FullDPSResult is PoB's Full DPS: the sum over the skills of socket
// groups with "Include in Full DPS", each calculated as the main skill.
type FullDPSResult struct {
	FullDPS, FullDotDPS float64 // PoB output.FullDPS / FullDotDPS
	Skills              []SkillDPS
}

// fullDPSAcc is the fullDPS table of calcs.calcFullDPS.
type fullDPSAcc struct {
	combined, poison, causticGround, impale, ignite, burningGround float64
	bleed, corruptingBlood, decay, dot, cullingMulti               float64
	skills                                                         []SkillDPS
}

// FullDPS is calcs.calcFullDPS (Modules/Calcs.lua): every active skill of
// a socket group with includeInFullDPS is calculated as the main skill
// (one Calc each, the main skill's own Calc reused) and its DPS, its
// minion's and its mirage's are summed with their counts; the best bleed,
// ignite, burning and caustic ground and the summed poison, decay and
// skill DoT are added once, capped at the DoT DPS cap, then impale and
// culling. A build with no such skill has FullDPS 0 and no skills.
//
// PoB runs this pass in CALCULATOR mode on an env it rebuilds from cached
// mod DBs (calcs.initEnv with accelerate) and only overrides mainSkill;
// that is the likely source of its 0.1-0.3% offsets between a skill's
// Full DPS entry and its own main-skill DPS, which a fresh Calc per skill
// does not reproduce.
//
// Labels differ from PoB's in places, never the numbers: the skillPart
// line uses infoMessage2 only for "Skill Damage" (the other setters, the
// trigger and mirage messages, are not ported); the totals' "source"
// (the skill giving the best bleed, ignite or ground) is not exposed; and
// Trigger is set only by the ported default trigger handler (no Saviour,
// Focus, or unported trigger configs).
func (c *Calculator) FullDPS(b *pobcode.Build) (*FullDPSResult, error) {
	base, err := c.Calc(b)
	if err != nil {
		return nil, err
	}
	return c.fullDPSFrom(base)
}

// fullDPSFrom is FullDPS on base, a Calc result. calcs.calcFullDPS runs in
// CALCULATOR mode on the build's socket groups as the MAIN pass left them,
// so each skill is calculated on base.build, the calculated build (Group
// mapped to its index), without the granted skill block again: that
// block would change nothing on base.build.
func (c *Calculator) fullDPSFrom(base *Result) (*FullDPSResult, error) {
	var err error
	b := base.build
	acc := &fullDPSAcc{}
	// calcs.calcFullDPS: for _, activeSkill in ipairs(fullEnv.player.activeSkillList).
	for i, s := range base.DB.activeSkills {
		if s.group == nil || !s.group.IncludeInFullDPS {
			continue
		}
		count, enabled := c.activeSkillCount(s)
		if !enabled {
			continue
		}
		info := base.Skills[i]
		r := base
		if !s.main {
			cp := withMainSkill(b, base.buildGroup(info.Group), info.GroupSkill)
			if r, err = c.calc(keepMainGroup(b, cp), calcOpts{calculator: true, compared: base.compared}); err != nil {
				return nil, err
			}
		}
		if r.main == nil {
			continue
		}
		acc.addSkill(r, info, count)
	}
	return acc.finish(), nil
}

// keepMainGroup keeps b's main socket group in the copy cp made with
// another main skill. PoB sets fullEnv.player.mainSkill on the env built
// for b, whose activeSkillList holds the main group even when it is
// disabled (CalcSetup.lua: index == env.mainSocketGroup or group.enabled
// ...); cp's own main group moved, so a disabled b main group is enabled
// in cp. A main group in an inactive weapon swap slot is not kept.
func keepMainGroup(b, cp *pobcode.Build) *pobcode.Build {
	set := b.ActiveSkills()
	if set == nil || len(set.Groups) == 0 {
		return cp
	}
	m := min(len(set.Groups), max(b.MainSocketGroup, 1)) // setupSkills' mainGroup
	if set.Groups[m-1].Enabled || m == cp.MainSocketGroup {
		return cp
	}
	// withMainSkill copied the group slice, so this does not touch b.
	cp.ActiveSkills().Groups[m-1].Enabled = true
	return cp
}

// activeSkillCount is getActiveSkillCount (Modules/Calcs.lua): the group's
// count, else the count of the gem granting the skill (for a Vaal gem,
// enabled only with the matching enableGlobal), else 1.
func (c *Calculator) activeSkillCount(s *activeSkill) (float64, bool) {
	if n := s.group.GroupCount; n != nil {
		return float64(*n), true
	}
	id := s.active.id
	for gi := range s.group.Gems {
		gm := &s.group.Gems[gi]
		gem, _, ok := c.resolveGem(gm)
		if !ok || gem == nil {
			continue
		}
		if gem.VaalGem {
			if id == gem.GrantedEffectID {
				return float64(gm.Count), gm.EnableGlobal1
			}
			if id == gem.SecondaryGrantedEffectID {
				return float64(gm.Count), gm.EnableGlobal2
			}
			continue
		}
		ge := c.data.Skills.Effects[gem.GrantedEffectID]
		if (id == gem.GrantedEffectID && ge != nil && !ge.Support) || id == gem.SecondaryGrantedEffectID {
			return float64(gm.Count), true
		}
	}
	return 1, true
}

// partName is activeSkill.skillPartName: the part's name of a multi-part
// skill (CalcActiveSkill.lua), the minion's main skill for a minion skill
// (CalcPerform.lua).
func partName(s *activeSkill, minion *MinionResult) string {
	if minion != nil && minion.mainSkill != "" {
		return minion.mainSkill
	}
	if s.part > 0 && s.part <= len(s.active.ge.Parts) && len(s.active.ge.Parts) > 1 {
		return s.active.ge.Parts[s.part-1].Name
	}
	return ""
}

func withPart(name, part string) string {
	if part == "" {
		return name
	}
	return name + " (" + part + ")"
}

// addSkill is the body of calcFullDPS's skill loop for a skill whose Calc
// as the main skill is r: its minion, its mirage, then the skill itself.
func (a *fullDPSAcc) addSkill(r *Result, info SkillInfo, count float64) {
	s := r.main
	name := s.active.ge.Name
	entry := func(n string, dps, cnt float64, trigger string) {
		a.skills = append(a.skills, SkillDPS{
			Name: n, Group: info.Group, GroupSkill: info.GroupSkill, Count: cnt, DPS: dps, Trigger: trigger,
		})
	}
	minionPart, minionCounted := "", false
	if m := r.Minion; m != nil && m.out != nil {
		mo := m.out
		if mo["TotalDPS"] > 0 {
			minionCounted = true
			entry(withPart(name, m.Minion+": "+partName(s, m)), mo["TotalDPS"], count, s.infoTrigger)
			a.combined += mo["TotalDPS"] * count
		}
		a.addAilments(mo, count)
		a.dot += math.Max(mo["TotalDot"], 0)
		// Absolution, Dominating Blow and Holy Strike: the skill damage is
		// counted once with their config option.
		if countedOnce(r.DB, name) {
			count, minionPart = 1, "Skill Damage" // infoMessage2
		}
	}

	if mr := s.mirage; mr != nil {
		mo, n := mr.out, mr.count*count
		if mo["TotalDPS"] > 0 {
			// trigger = activeSkill.mirage.infoTrigger: CalcMirages.lua never sets
			// it in v2.68.1 (only the mirage's infoMessage2), so it is "".
			entry(withPart(name+" (Mirage)", partName(s, nil)), mo["TotalDPS"], n, "")
			a.combined += mo["TotalDPS"] * n
		}
		a.addAilments(mo, n)
		// (usedEnv.player.output.TotalDot and usedEnv.player.output.TotalDot == 0): absent is not 0.
		playerDot, hasDot := r.Stats["TotalDot"]
		if s.flags["DotCanStack"] || (hasDot && playerDot == 0) {
			a.addDot(mo, s, n)
		}
		a.addGround(mo)
	}

	out := r.Stats
	if out["TotalDPS"] > 0 {
		// skillPart = minionName and infoMessage2 or skillPartName.
		part := partName(s, r.Minion)
		if minionCounted && minionPart != "" {
			part = minionPart
		}
		entry(withPart(name, part), out["TotalDPS"], count, s.infoTrigger)
		a.combined += out["TotalDPS"] * count
	}
	a.addAilments(out, count)
	a.corruptingBlood = math.Max(a.corruptingBlood, out["CorruptingBloodDPS"])
	a.addGround(out)
	a.addDot(out, s, count)
}

// countedOnce is calcFullDPS's check of the "Count skill damage once"
// config options (Condition:<Skill>SkillDamageCountedOnce).
func countedOnce(db *ModDB, name string) bool {
	for _, k := range []string{"Absolution", "Dominating Blow", "Holy Strike"} {
		if strings.Contains(name, k) && db.Flag("Condition:"+strings.ReplaceAll(k, " ", "")+"SkillDamageCountedOnce") {
			return true
		}
	}
	return false
}

// addAilments takes the best bleed and ignite and adds poison, impale
// (n instances), decay and the cull multiplier of one output.
func (a *fullDPSAcc) addAilments(o map[string]float64, n float64) {
	a.bleed = math.Max(a.bleed, o["BleedDPS"])
	a.ignite = math.Max(a.ignite, o["IgniteDPS"])
	if o["PoisonDPS"] > 0 {
		a.poison += o["TotalPoisonDPS"] * n
	}
	if o["ImpaleDPS"] > 0 {
		a.impale += o["ImpaleDPS"] * n
	}
	a.decay += math.Max(o["DecayDPS"], 0)
	if o["CullMultiplier"] > 1 {
		a.cullingMulti = math.Max(a.cullingMulti, o["CullMultiplier"])
	}
}

// addGround takes the best burning and caustic ground.
func (a *fullDPSAcc) addGround(o map[string]float64) {
	a.burningGround = math.Max(a.burningGround, o["BurningGroundDPS"])
	a.causticGround = math.Max(a.causticGround, o["CausticGroundDPS"])
}

// addDot adds the skill DoT: n instances when it stacks (DotCanStack).
func (a *fullDPSAcc) addDot(o map[string]float64, s *activeSkill, n float64) {
	if o["TotalDot"] <= 0 {
		return
	}
	if s.flags["DotCanStack"] {
		a.dot += o["TotalDot"] * n
	} else {
		a.dot += o["TotalDot"]
	}
}

// finish is the end of calcFullDPS: "Re-Add ailment DPS components".
func (a *fullDPSAcc) finish() *FullDPSResult {
	total := func(n string, v float64) {
		a.skills = append(a.skills, SkillDPS{Name: n, Count: 1, DPS: v})
	}
	dot := 0.0
	for _, d := range []struct {
		name string
		v    float64
	}{{"Best Bleed DPS", a.bleed}, {"Corrupting Blood DPS", a.corruptingBlood}, {"Best Ignite DPS", a.ignite},
		{"Best Burning Ground DPS", a.burningGround}} {
		if d.v > 0 {
			total(d.name, d.v)
			dot += d.v
		}
	}
	if a.poison > 0 {
		a.poison = math.Min(a.poison, dotDpsCap)
		total("Full Poison DPS", a.poison)
		dot += a.poison
	}
	if a.causticGround > 0 {
		total("Best Caustic Ground DPS", a.causticGround)
		dot += a.causticGround
	}
	if a.impale > 0 {
		total("Full Impale DPS", a.impale)
		a.combined += a.impale
	}
	if a.decay > 0 {
		total("Full Decay DPS", a.decay)
		dot += a.decay
	}
	if a.dot > 0 {
		total("Full DoT DPS", a.dot)
		dot += a.dot
	}
	dot = math.Min(dot, dotDpsCap)
	a.combined += dot
	if a.cullingMulti > 0 {
		cull := a.combined * (a.cullingMulti - 1)
		total("Full Culling DPS", cull)
		a.combined += cull
	}
	return &FullDPSResult{FullDPS: a.combined, FullDotDPS: dot, Skills: a.skills}
}
