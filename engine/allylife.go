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
// estimations.
func (c *Calculator) allyLifeMods(db *ModDB, skills []*activeSkill, buffs map[string][]Mod, charLevel int, res *Result) {
	if db.Sum("BASE", allyLifeRedirects...) == 0 {
		return
	}
	c.minionLifeMods(db, skills, buffs, charLevel, res)
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
// and skillTotemId of CalcActiveSkill.lua: the active effect's level
// requirement, unless a totem support's totem_support_gem_level set
// skillData.totemLevel; 0 when the skill has no level.
func (c *Calculator) totemLife(s *activeSkill) float64 {
	ge := s.active.ge
	lvl := ge.Levels[s.active.level]
	mon := c.data.Monster
	if lvl == nil {
		return 0
	}
	level := lvl.LevelRequirement
	if v, ok := num(s.data.val("totemLevel")); ok && v > 0 {
		level = int(v)
	}
	if level < 1 || level > len(mon.AllyLife) {
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
	return round(math.Floor(mon.AllyLife[level-1]*mon.TotemLifeMult[id])*lifeMod, 0)
}
