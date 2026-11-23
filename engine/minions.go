package engine

import (
	"math"

	"github.com/Mi7teR/poe-lib/gamedata"
)

// skillMinion is the minion part of CalcActiveSkill.lua buildActiveSkillModList.
type skillMinion struct {
	typ   string
	data  *gamedata.Minion
	level int
}

// minionList is the skill's activeSkill.minionList: the granted effect's
// minions, then those its supports add (with their level requirement).
// Spectres (an empty minionList) need the build's spectre list, which is
// not ported.
func minionList(s *activeSkill) ([]string, map[string]int) {
	list := append([]string(nil), s.active.ge.MinionList...)
	supportLevel := map[string]int{}
	for _, e := range s.effects {
		if !e.ge.Support || len(e.ge.AddMinionList) == 0 {
			continue
		}
		for _, t := range e.ge.AddMinionList {
			if !contains(list, t) {
				if lv := e.ge.Levels[e.level]; lv != nil {
					supportLevel[t] = lv.LevelRequirement
				}
				list = append(list, t)
			}
		}
	}
	return list, supportLevel
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

// minionOf picks the skill's minion (the gem's skillMinion, else the first)
// and its level.
func (c *Calculator) minionOf(s *activeSkill, charLevel, enemyLevel int) *skillMinion {
	list, supportLevel := minionList(s)
	if len(list) == 0 {
		return nil
	}
	typ := list[0]
	if s.gem != nil && contains(list, s.gem.SkillMinion) {
		typ = s.gem.SkillMinion
	}
	data := c.data.Minions[typ]
	if data == nil {
		return nil
	}
	m := &skillMinion{typ: typ, data: data}
	req := 1
	if lv := s.active.ge.Levels[s.active.level]; lv != nil {
		req = lv.LevelRequirement
	}
	switch {
	case truthy(s.data["minionLevelIsEnemyLevel"]):
		m.level = enemyLevel
	case truthy(s.data["minionLevelIsPlayerLevel"]):
		limit, ok := num(s.data["minionLevelIsPlayerLevel"])
		if !ok {
			limit = math.Inf(1)
		}
		m.level = int(math.Min(float64(charLevel), limit))
	case supportLevel[typ] != 0:
		m.level = supportLevel[typ]
	default:
		if v, ok := num(s.data["minionLevel"]); ok {
			m.level = int(v)
		} else {
			m.level = req
		}
	}
	m.level = min(max(m.level, 1), 100)
	return m
}

// minionCounts ports the minion count part of calcs.perform: the summoned
// minion multipliers and OnlyMinion (Companionship).
func (c *Calculator) minionCounts(db *ModDB, skills []*activeSkill) {
	type count struct{ total, nonVaal, permanent float64 }
	counts := map[string]*count{}
	var order []string
	for _, s := range skills {
		if s.disabled {
			continue
		}
		list, _ := minionList(s)
		for _, t := range list {
			data := c.data.Minions[t]
			if data == nil || data.Hostile {
				continue
			}
			key := data.Limit
			n := 1.0
			if key == "" {
				key = s.active.id + ":" + t
			} else {
				if o, ok := db.Override(data.Limit); ok {
					n = o
				} else {
					n = val(s.mods, data.Limit) * s.mods.MoreCfg(s.cfg, "ActiveMinionLimit")
				}
				n = math.Floor(n)
				db.output[data.Limit] = math.Max(n, db.output[data.Limit])
			}
			ct := counts[key]
			if ct == nil {
				ct = &count{}
				counts[key] = ct
				order = append(order, key)
			}
			ct.total = math.Max(n, ct.total)
			if !s.types[stVaal] {
				ct.nonVaal = math.Max(n, ct.nonVaal)
			}
			if s.flags["permanentMinion"] {
				ct.permanent = math.Max(n, ct.permanent)
			}
		}
	}
	combat := map[string]any{"type": "Condition", "var": "Combat"}
	summoned := 0.0
	for _, k := range order {
		ct := counts[k]
		summoned += ct.total
		db.NewMod("Multiplier:SummonedMinion", "BASE", ct.total, "Config", combat)
		if ct.nonVaal != 0 {
			db.NewMod("Multiplier:NonVaalSummonedMinion", "BASE", ct.nonVaal, "Config", combat)
		}
		if ct.permanent != 0 {
			db.NewMod("Multiplier:PermanentMinion", "BASE", ct.permanent, "Config", combat)
		}
	}
	if o := db.enemyEnv.value("multiplierSummonedMinion"); o != 0 {
		summoned = o
	}
	db.conditions["OnlyMinion"] = summoned == 1
}

// minionLife is the life pool branch of calcs.perform for a minion that
// is not env.minion: initMinionModDB, the skill's and buffs' minion
// modifiers, keystones, then doActorAttribsConditions.
func (c *Calculator) minionLife(db *ModDB, s *activeSkill, m *skillMinion, buffs map[string][]Mod, res *Result) float64 {
	mdb, _ := c.initMinionDB(db, s, m, res)
	if mdb == nil {
		return 0
	}
	addMinionModifiers(mdb, s.mods.List(s.cfg, "MinionModifier"), m.typ)
	for _, l := range buffMinionModifiers(buffs, s.cfg) {
		addMinionModifiers(mdb, l, m.typ)
	}
	c.mergeKeystones(mdb, map[string]bool{}, res)
	c.attributes(mdb)
	c.lifeMana(mdb)
	return mdb.output["Life"]
}

// minionLifePoolBySkill and the spectre and Companionship pools are
// CalcPerform.lua's tables of minions whose life is spent before yours.
var minionLifePoolBySkill = map[string]allyLifePool{
	"Summon Sentinel of Radiance":        {life: "TotalRadianceSentinelLife", redirect: "takenFromRadianceSentinelBeforeYou", source: "Sentinel of Radiance"},
	"Summon Void Spawn":                  {life: "TotalVoidSpawnLife", redirect: "takenFromVoidSpawnBeforeYou", source: "Void Spawns", limit: "ActiveVoidSpawnLimit"},
	"Summon Stone Golem of Safeguarding": {life: "TotalStoneGolemLife", redirect: "takenFromStoneGolemBeforeYou", source: "Stone Golem"},
}

var (
	spectreLifePool       = allyLifePool{life: "TotalSpectreLife", redirect: "takenFromSpectresBeforeYou", source: "Spectres", limit: "ActiveSpectreLimit"}
	companionshipLifePool = allyLifePool{life: "TotalMinionLife", redirect: "takenFromMinionBeforeYou", source: "Minion"}
)

type allyLifePool struct{ life, redirect, source, limit string }

// minionLifeMods is the minion part of the ally life block of
// calcs.perform: each redirect's minion life as a BASE mod.
func (c *Calculator) minionLifeMods(db *ModDB, skills []*activeSkill, buffs map[string][]Mod, charLevel int, res *Result) {
	calculated := map[string]bool{}
	spectreCount := 0.0
	spectreLimit := db.output["ActiveSpectreLimit"]
	if o := db.enemyEnv.value("multiplierSummonedMinion"); o != 0 {
		spectreLimit = math.Min(spectreLimit, o)
	}
	var firstSpectre float64
	for _, s := range skills {
		if s.disabled || s.types[stMinionsAreUndamagable] {
			continue
		}
		m := c.minionOf(s, charLevel, db.enemyEnv.level)
		if m == nil {
			continue
		}
		var pools []allyLifePool
		skillPool, hasSkillPool := minionLifePoolBySkill[s.active.ge.Name]
		if s.flags["spectre"] {
			skillPool, hasSkillPool = spectreLifePool, true
		}
		if hasSkillPool {
			pools = append(pools, skillPool)
		}
		companionship := false
		for _, b := range s.buffs {
			for _, x := range b.mods {
				if x.Name == companionshipLifePool.redirect {
					companionship = true
				}
			}
		}
		if companionship {
			pools = append(pools, companionshipLifePool)
			if hasSkillPool && db.Sum("BASE", skillPool.redirect) != 0 && db.Sum("BASE", companionshipLifePool.redirect) != 0 {
				pools = []allyLifePool{companionshipLifePool}
				db.Add(Mod{Name: "MinionLifeShares" + skillPool.life, Type: "FLAG", Value: true, Source: "Companionship"})
			}
		}
		life := -1.0
		for _, p := range pools {
			_, overridden := db.Override(p.life)
			isSpectre := p == spectreLifePool
			if (isSpectre && spectreCount >= spectreLimit) || (!isSpectre && calculated[p.life]) ||
				db.Sum("BASE", p.redirect) == 0 || overridden {
				continue
			}
			if life < 0 {
				life = c.minionLife(db, s, m, buffs, res)
			}
			n := 1.0
			if p.limit != "" {
				if v, ok := db.output[p.limit]; ok {
					n = v
				} else {
					n = val(s.mods, p.limit)
				}
			}
			if isSpectre {
				n = 1
				spectreCount++
				if firstSpectre == 0 {
					firstSpectre = life
				}
			} else {
				calculated[p.life] = true
			}
			db.NewMod(p.life, "BASE", life*n, p.source)
		}
	}
	if firstSpectre != 0 && spectreCount < spectreLimit {
		db.NewMod(spectreLifePool.life, "BASE", firstSpectre*(spectreLimit-spectreCount), spectreLifePool.source)
	}
}
