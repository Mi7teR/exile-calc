package engine

import "math"

// allyLifeRedirects are CalcPerform.lua allyLifeRedirects: the mods that
// send damage to an ally's life before yours.
var allyLifeRedirects = []string{
	"takenFromSpectresBeforeYou", "takenFromMinionBeforeYou", "takenFromRadianceSentinelBeforeYou",
	"takenFromVoidSpawnBeforeYou", "takenFromStoneGolemBeforeYou", "takenFromTotemsBeforeYou",
	"takenFromVaalRejuvenationTotemsBeforeYou",
}

// allyLifeMods ports the ally life block of calcs.perform: the life of
// the allies that take damage before you, as BASE mods for the defence
// estimations. Only totems are ported; minion life needs minion actors.
func (c *Calculator) allyLifeMods(db *ModDB, skills []*activeSkill, res *Result) {
	if db.Sum("BASE", allyLifeRedirects...) == 0 {
		return
	}
	for _, r := range []string{"takenFromSpectresBeforeYou", "takenFromMinionBeforeYou", "takenFromRadianceSentinelBeforeYou",
		"takenFromVoidSpawnBeforeYou", "takenFromStoneGolemBeforeYou"} {
		if db.Sum("BASE", r) != 0 {
			res.Incomplete = append(res.Incomplete, "minion life for "+r+" not calculated")
		}
	}
	type totem struct {
		name string
		life float64
	}
	var normal, vaal *totem
	for _, s := range skills {
		if !s.flags["totem"] || s.disabled {
			continue
		}
		t := &totem{name: s.active.ge.Name, life: c.totemLife(s)}
		if t.life == 0 {
			continue
		}
		if t.name == "Vaal Rejuvenation Totem" {
			vaal = t
		} else if normal == nil || t.life > normal.life {
			normal = t
		}
	}
	// PoB cannot know which totem is nearest: the one with the most life.
	nearest := normal
	if vaal != nil && (normal == nil || vaal.life > normal.life) {
		nearest = vaal
	}
	_, totemOverride := db.Override("TotalTotemLife")
	redirect := db.Sum("BASE", "takenFromTotemsBeforeYou")
	nearestIsVaal := nearest != nil && nearest == vaal && !totemOverride
	if nearest != nil && !nearestIsVaal && redirect != 0 && !totemOverride {
		db.NewMod("TotalTotemLife", "BASE", nearest.life, "Totem")
	}
	if _, ok := db.Override("TotalVaalRejuvenationTotemLife"); vaal != nil && !ok &&
		(db.Sum("BASE", "takenFromVaalRejuvenationTotemsBeforeYou") != 0 || nearestIsVaal && redirect != 0) {
		db.NewMod("TotalVaalRejuvenationTotemLife", "BASE", vaal.life, "Vaal Rejuvenation Totem")
	}
}

// totemLife is calcs.calcTotemLife (CalcOffence.lua), with the totem level
// and skillTotemId of CalcActiveSkill.lua; 0 when the skill has no level.
func (c *Calculator) totemLife(s *activeSkill) float64 {
	ge := s.active.ge
	lvl := ge.Levels[s.active.level]
	mon := c.data.Monster
	if lvl == nil || lvl.LevelRequirement < 1 || lvl.LevelRequirement > len(mon.AllyLife) {
		return 0
	}
	id := ge.SkillTotemID
	if id == 0 {
		switch ge.Color {
		case 2, 3:
			id = ge.Color
		default:
			id = 1
		}
	}
	lifeMod := (1 + s.mods.SumCfg(s.cfg, "INC", "TotemLife")/100) * s.mods.MoreCfg(s.cfg, "TotemLife")
	return round(math.Floor(mon.AllyLife[lvl.LevelRequirement-1]*mon.TotemLifeMult[id])*lifeMod, 0)
}
