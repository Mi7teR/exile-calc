package engine

import (
	"math"
	"strings"

	"github.com/Mi7teR/exile-calc/pobcode"
)

// costWarnPools is the first pairs table of Calcs.lua:426: the pool each
// cost is paid from.
var costWarnPools = [...]struct{ pool, cost string }{
	{"LifeUnreserved", "LifeCost"},
	{"ManaUnreserved", "ManaCost"},
	{"Rage", "RageCost"},
	{"EnergyShield", "ESCost"},
}

// costWarnPercentPools is the second pairs table (Calcs.lua:444).
var costWarnPercentPools = [...]struct{ pool, cost string }{
	{"LifeUnreservedPercent", "LifePercentCost"},
	{"ManaUnreservedPercent", "ManaPercentCost"},
}

// CostWarnings is the output of Calcs.lua:418-454 that the stats leave
// out: the <Cost>WarningList tables, keyed by PoB's output name
// ("ManaCostWarningList", "LifeCostWarningList", "RageCostWarningList",
// "ESCostWarningList", "LifePercentCostPercentCostWarningList",
// "ManaPercentCostPercentCostWarningList"). Each lists, in active skill
// order, the skills whose cost the build's pool cannot pay when that
// skill is the main skill.
type CostWarnings map[string][]string

// CostWarnings runs the MAIN-mode cost check of calcs.buildOutput
// (Calcs.lua:418-454): every active skill is calculated as the main skill
// (calcs.buildActiveSkill in MAIN mode, one Calc each, the main skill's
// own Calc reused) and its costs are compared with the build's pools.
// Calc sets the stats this check gives (<Cost>Warning for the main
// skill's name, EnergyShieldProtectsMana) from the skills that can set
// them; this method also calculates the others, for the lists.
func (c *Calculator) CostWarnings(b *pobcode.Build) (CostWarnings, error) {
	base, err := c.Calc(b)
	if err != nil {
		return nil, err
	}
	lists := CostWarnings{}
	err = c.costWarnings(base, func(*activeSkill) bool { return true }, lists)
	return lists, err
}

// buildOutput is the MAIN-mode part of calcs.buildOutput (Calcs.lua:418-
// 464) that gives numbers: the cost warnings and the ExtraPoints and
// Spec:* sums. The full DPS pass before it is FullDPS.
//
// PoB calculates every active skill as the main skill for the warnings,
// but only a skill named like the main skill can set a <Cost>Warning
// stat (436); the other skills only add to the WarningList tables. Calc
// therefore calculates only those skills, and CostWarnings the rest: one
// extra Calc per active skill would make every Calc several times slower.
// With no main skill no <Cost>Warning is set, but any calculated skill
// sets EnergyShieldProtectsMana (425), so then every skill is calculated.
func (c *Calculator) buildOutput(base *Result) error {
	keep := func(*activeSkill) bool { return true }
	if m := base.main; m != nil {
		keep = func(s *activeSkill) bool { return s.main || s.active.ge.Name == m.active.ge.Name }
	}
	if err := c.costWarnings(base, keep, nil); err != nil {
		return err
	}
	// Calcs.lua:456-464.
	db, out := base.DB, base.Stats
	out["ExtraPoints"] = db.Sum("BASE", "ExtraPoints")
	out["Spec:LifeInc"] = treeInc(db, "Life")
	out["Spec:ManaInc"] = treeInc(db, "Mana")
	out["Spec:ArmourInc"] = treeInc(db, "Armour", "ArmourAndEvasion")
	out["Spec:EvasionInc"] = treeInc(db, "Evasion", "ArmourAndEvasion")
	out["Spec:EnergyShieldInc"] = treeInc(db, "EnergyShield")
	return nil
}

// costWarnings is the skill loop of Calcs.lua:419-453 over base, a MAIN
// Calc, for the active skills keep accepts. lists, when not nil, gets the
// WarningList tables.
func (c *Calculator) costWarnings(base *Result, keep func(*activeSkill) bool, lists CostWarnings) error {
	b := base.build
	for i, s := range base.DB.activeSkills {
		if !keep(s) {
			continue
		}
		r := base
		if !s.main {
			// calcs.buildActiveSkill(env, mode, skill, uuid): as fullDPSFrom,
			// on the calculated build, in MAIN mode.
			if s.group == nil {
				continue
			}
			info := base.Skills[i]
			cp := withMainSkill(b, base.buildGroup(info.Group), info.GroupSkill)
			opts := calcOpts{skillPass: true, compared: base.compared, grants: base.grants}
			var err error
			if r, err = c.calc(keepMainGroup(b, cp), opts); err != nil {
				return err
			}
		}
		// Calcs.lua:424: buildActiveSkill always caches the skill's env,
		// main skill or not, so every kept skill is checked.
		checkCosts(base, r, s.active.ge.Name, lists)
	}
	return nil
}

// checkCosts is the body of the skill loop (Calcs.lua:425-452) for the
// skill name, whose Calc as the main skill is r.
func checkCosts(base, r *Result, name string, lists CostWarnings) {
	out := base.Stats
	// Calcs.lua:425: output.EnergyShieldProtectsMana = env.modDB:Flag(...),
	// assigned for every calculated skill; FlagInternal returns true or
	// nil, so the key exists only while the flag is set.
	protects := base.DB.Flag("EnergyShieldProtectsMana")
	if protects {
		out["EnergyShieldProtectsMana"] = 1
	} else {
		delete(out, "EnergyShieldProtectsMana")
	}
	for _, p := range costWarnPools {
		if !cannotPay(out, r, p.pool, p.cost, protects) {
			continue
		}
		if base.main != nil && base.main.active.ge.Name == name {
			out[p.cost+"Warning"] = 1
		}
		if lists != nil {
			lists[p.cost+"WarningList"] = append(lists[p.cost+"WarningList"], name)
		}
	}
	if lists == nil {
		return
	}
	for _, p := range costWarnPercentPools {
		if cost, ok := r.Stats[p.cost]; ok && out[p.pool] < cost {
			k := p.cost + "PercentCostWarningList"
			lists[k] = append(lists[k], name)
		}
	}
}

// cannotPay is Calcs.lua:427-434 for one pool: whether r, a skill's Calc
// as the main skill, has a cost the build's pool in out cannot pay.
func cannotPay(out map[string]float64, r *Result, pool, costName string, protects bool) bool {
	cost, ok := r.Stats[costName]
	if !ok {
		return false
	}
	total := out[pool]
	if protects && costName == "ManaCost" {
		total += out["EnergyShield"]
	}
	if total >= cost {
		return false
	}
	// Calcs.lua:431-434: a skill that also reserves this pool warns only
	// when the pool before its reservation cannot pay either.
	raw := strings.TrimSuffix(pool, "Unreserved")
	// Calcs.lua:432: no main skill in the cached env is no reservation.
	if r.main == nil {
		return true
	}
	pct, reserves := num(r.main.data.val(raw + "ReservedPercent"))
	return !reserves || total+math.Ceil(out[raw]*pct/100) < cost
}

// treeInc is ModDB:Sum("INC", { source = "Tree" }, names...): only mods
// whose source up to the first ":" is "Tree" (ModDB.lua:144). The cfg
// goes into EvalMod too, so a Multiplier or Condition tag counts only
// the tree's mods (ModStore.lua:294, 302).
func treeInc(db *ModDB, names ...string) float64 {
	return db.SumCfg(&Cfg{source: "Tree"}, "INC", names...)
}
