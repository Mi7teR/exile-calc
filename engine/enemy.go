package engine

import (
	"math"

	"github.com/Mi7teR/exile-calc/gamedata"
	"github.com/Mi7teR/exile-calc/pobcode"
)

const (
	maxEnemyLevel       = 85 // Data.lua data.misc.MaxEnemyLevel
	stdBossDPSMult      = 4 / 4.40
	pinnacleBossDPSMult = 8 / 4.40
	pinnacleBossPen     = 15.0 / 5
	uberBossDPSMult     = 10 / 4.25
	uberBossPen         = 40.0 / 5
	monsterBaseCritMult = 130 // monsterConstants base_critical_strike_multiplier
	// Data.lua:988 data.bossStats: 100 + the mean armourMult/evasionMult of
	// Data/Bosses.lua (all bosses for Pinnacle, isUber ones for Uber).
	monsterDefenceShare = 100.0 // None and Boss: the plain monster tables
	pinnacleArmourMean  = 150.0
	pinnacleEvasionMean = 100 + 274.0/11 // 124.909...
	uberArmourMean      = 125.0
	uberEvasionMean     = 100 + 116.0/7 // 116.571...
)

// enemyEnv is what the config tab gives the calculation about the enemy:
// its level (ConfigTabClass:UpdateLevel) and each enemy option's value,
// the input or else the placeholder that enemyIsBoss's apply sets.
type enemyEnv struct {
	level      int
	damageType string  // enemyDamageType
	worstOf    float64 // EHPUnluckyWorstOf
	// physConversion is monsterPhysConversionMultiTable at the level.
	physConversion float64
	inputs         map[string]float64
	checks         map[string]bool // check inputs that are on
	holders        map[string]float64
}

// num is the input if set, else the placeholder (tonumber(input) or placeholder).
func (e *enemyEnv) num(name string) (float64, bool) {
	if v, ok := e.inputs[name]; ok {
		return v, true
	}
	v, ok := e.holders[name]
	return v, ok
}

// value is num or 0.
func (e *enemyEnv) value(name string) float64 {
	v, _ := e.num(name)
	return v
}

// newEnemyEnv ports the placeholder side of ConfigOptions.lua enemyIsBoss
// and ConfigTabClass:UpdateLevel.
func newEnemyEnv(set *pobcode.ConfigSet, opts []gamedata.ConfigOption, charLevel int, mon *gamedata.MonsterTables, res *Result) *enemyEnv {
	e := &enemyEnv{inputs: map[string]float64{}, checks: map[string]bool{}, holders: map[string]float64{}, damageType: "Average", worstOf: 1}
	if set != nil {
		for _, in := range set.Inputs {
			switch in.Kind {
			case pobcode.KindNumber:
				e.inputs[in.Name] = in.Number
			case pobcode.KindBool:
				e.checks[in.Name] = in.Bool
			}
		}
	}
	if v := configString(set, "enemyDamageType", ""); v != "" {
		e.damageType = v
	}
	if v, ok := e.inputs["EHPUnluckyWorstOf"]; ok && v > 0 {
		e.worstOf = v
	}
	if v := configString(set, "presetBossSkills", "None"); v != "None" {
		res.Incomplete = append(res.Incomplete, "boss skill preset "+v+" not applied to enemy damage")
	}
	boss := "Pinnacle"
	for _, o := range opts {
		if o.Var == "enemyIsBoss" {
			if s, ok := o.DefaultValue.(string); ok {
				boss = s
			}
		}
	}
	boss = configString(set, "enemyIsBoss", boss)

	h := e.holders
	h["enemySpeed"], h["enemyCritChance"], h["enemyCritDamage"] = 700, 5, monsterBaseCritMult-100
	updateLevel := func() {
		switch {
		case e.inputs["enemyLevel"] > 0:
			e.level = int(math.Min(maxEnemyLevel, e.inputs["enemyLevel"]))
		case h["enemyLevel"] > 0:
			e.level = int(math.Min(maxEnemyLevel, h["enemyLevel"]))
		default:
			e.level = min(maxEnemyLevel, charLevel)
		}
	}
	dmg := func(level int) float64 {
		level = max(1, min(level, len(mon.Damage)))
		return mon.Damage[level-1]
	}
	updateLevel()
	// enemyIsBoss also sets the enemy armour and evasion placeholders from
	// the monster tables (ConfigOptions.lua 2134-2135, 2166-2167, 2200-2201,
	// 2234-2235), the bosses' at Data.lua:988 bossStats' mean share.
	defences := func(level int, armourMean, evasionMean float64) {
		level = max(1, min(level, len(mon.Armour), len(mon.Evasion)))
		h["enemyArmour"] = round(mon.Armour[level-1]*armourMean/100, 0)    //nolint:mnd // percent
		h["enemyEvasion"] = round(mon.Evasion[level-1]*evasionMean/100, 0) //nolint:mnd // percent
	}
	elements := []string{"Lightning", "Cold", "Fire"}
	switch boss {
	case "None":
		updateLevel()
		h["enemyPhysicalDamage"] = round(dmg(e.level)*1.5, 0)
		defences(e.level, monsterDefenceShare, monsterDefenceShare)
	case "Boss":
		updateLevel()
		defences(e.level, monsterDefenceShare, monsterDefenceShare)
		d := round(dmg(e.level)*1.5*stdBossDPSMult, 0)
		h["enemyPhysicalDamage"] = d
		for _, t := range elements {
			h["enemy"+t+"Damage"] = d
		}
		h["enemyChaosDamage"] = round(d/2.5, 0)
		for _, t := range elements {
			h["enemy"+t+"Resist"] = 40
		}
		h["enemyChaosResist"] = 25
	case "Pinnacle", "Uber":
		lvl, mult, pen, chaosDiv := 84, pinnacleBossDPSMult, pinnacleBossPen, 2.5
		if boss == "Uber" {
			lvl, mult, pen, chaosDiv = 85, uberBossDPSMult, uberBossPen, 4
		}
		h["enemyLevel"] = float64(lvl)
		updateLevel()
		lvl = max(e.level, lvl)
		d := round(dmg(lvl)*1.5*mult, 0)
		h["enemyPhysicalDamage"] = d
		for _, t := range elements {
			h["enemy"+t+"Damage"] = d
			h["enemy"+t+"Pen"] = pen
			h["enemy"+t+"Resist"] = 50
		}
		h["enemyChaosDamage"] = round(d/chaosDiv, 0)
		h["enemyChaosResist"] = 30
		if boss == "Uber" {
			defences(lvl, uberArmourMean, uberEvasionMean)
		} else {
			defences(lvl, pinnacleArmourMean, pinnacleEvasionMean)
		}
	}
	if l := max(1, min(e.level, len(mon.PhysConversionMulti))); l > 0 {
		e.physConversion = mon.PhysConversionMulti[l-1]
	}
	return e
}

// enemyAccuracy is CalcSetup.lua's base enemy accuracy for the level.
func enemyAccuracy(mon *gamedata.MonsterTables, level int) float64 {
	level = max(1, min(level, len(mon.Accuracy)))
	return mon.Accuracy[level-1]
}
