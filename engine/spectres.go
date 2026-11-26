package engine

import "strings"

// spectreEffects is what calcs.perform takes from the modLists of the
// build's active spectres (CalcPerform.lua:2242-2305): the "Spectre" buff
// on the player (AllyModifier and PlayerModifier mods) and on the main
// minion (AllyModifier and MinionModifier mods), the minion curse limit
// and the beast spectre condition.
type spectreEffects struct {
	player, minion []Mod
	// curseLimit is minionCurses.limit: 1, or EnemyCurseLimit + 1 of a
	// spectre that has it.
	curseLimit int
	beast      bool
}

// spectreEffectsOf ports the active spectre loop of calcs.perform. A
// spectre is active when a Raise Spectre skill that is not disabled has
// it as its minion; each spectre of the build's list counts once.
func (c *Calculator) spectreEffectsOf(db *ModDB, skills []*activeSkill) spectreEffects {
	out := spectreEffects{curseLimit: 1}
	active, ok := activeSpectres(skills)
	b := db.theBuild()
	if !ok || b == nil {
		return out
	}
	for _, id := range b.SpectreList {
		data := c.data.Minions[id]
		// Build.lua Load drops ids that are not in data.minions.
		if data == nil || !contains(active, id) {
			continue
		}
		out.beast = out.beast || contains(data.MonsterTags, "beast")
		out.addModList(data.ModList, "Spectre:"+data.Name)
	}
	return out
}

// activeSpectres is activeSpectreList: the minion of each Raise Spectre
// skill that is not disabled; ok reports whether there is such a skill.
func activeSpectres(skills []*activeSkill) ([]string, bool) {
	var active []string
	ok := false
	for _, s := range skills {
		if s.disabled || !strings.HasPrefix(s.active.id, "RaiseSpectre") {
			continue
		}
		ok = true
		if t := minionType(s); t != "" {
			active = append(active, t)
		}
	}
	return active, ok
}

// addModList is the modList part of the active spectre loop: the curse
// limit, and the AllyModifier, MinionModifier and PlayerModifier mods.
func (e *spectreEffects) addModList(modList []map[string]any, source string) {
	for _, mm := range modList {
		m := modFromMap(mm, source)
		if m.Name == "EnemyCurseLimit" {
			v, _ := num(m.Value)
			e.curseLimit = int(v) + 1
			return // CalcPerform.lua:2279: the rest of the modList is skipped
		}
		inner, _ := m.Value.(map[string]any)
		im, isMod := inner["mod"].(map[string]any)
		if m.Type != modTypeList || !isMod {
			continue
		}
		mod := modFromMap(im, source)
		switch m.Name {
		case "AllyModifier":
			e.minion = append(e.minion, mod)
			e.player = append(e.player, mod)
		case "MinionModifier":
			e.minion = append(e.minion, mod)
		case "PlayerModifier":
			e.player = append(e.player, mod)
		}
	}
}

// modTypeList is the LIST mod type.
const modTypeList = "LIST"

// minionType is activeSkill.minion.type: the gem's skillMinion when it is
// in the skill's minion list, else the first; "" without minions.
func minionType(s *activeSkill) string {
	list, _ := minionList(s)
	if len(list) == 0 {
		return ""
	}
	if s.gem != nil && contains(list, s.gem.SkillMinion) {
		return s.gem.SkillMinion
	}
	return list[0]
}
