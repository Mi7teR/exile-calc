package engine

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// withTreeGrantedSkills ports the tree-node half of CalcSetup.lua's
// "Process extra skills granted by items or tree nodes" (lines 1478-1564):
// every ExtraSkill granted by an allocated node (node.grantedSkills,
// CalcSetup.lua:251-263, source "Tree:<id>") gets a socket group. A saved
// group matches when it has the same source, no slot and the same first
// skill at the granted level (raw or validated, getNormalizedSkillLevel);
// otherwise a new group is appended to the active skill set (t_insert into
// socketGroupList), so the saved main socket group keeps its index.
//
// b is returned unchanged when every grant has a group; otherwise the copy
// has its own skill set slice. Item-granted skills are not created here:
// saved builds carry their groups. Saved Tree groups that match no grant
// are disabled in setupSkills (treeGrants) rather than removed, which
// keeps the saved main socket group's index.
func withTreeGrantedSkills(db *ModDB, b *pobcode.Build, skills map[string]*gamedata.Skill) *pobcode.Build {
	set := b.ActiveSkills()
	if set == nil {
		return b
	}
	type grant struct {
		node   int
		source string
		id     string
		level  int
	}
	var grants []grant
	for _, e := range db.List(nil, "ExtraSkill") {
		idStr, ok := strings.CutPrefix(e.Mod.Source, "Tree:")
		if !ok {
			continue
		}
		node, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		v, _ := e.Value.(map[string]any)
		sid, _ := v["skillId"].(string)
		if sid == "" {
			continue
		}
		lvl, _ := num(v["level"])
		grants = append(grants, grant{node: node, source: e.Mod.Source, id: sid, level: int(lvl)})
	}
	// env.allocNodes is walked with pairs (no order); node id order keeps
	// the appended groups deterministic.
	sort.SliceStable(grants, func(i, j int) bool { return grants[i].node < grants[j].node })

	var added []pobcode.SocketGroup
	for _, gr := range grants {
		found := false
		for _, g := range append(slices.Clip(set.Groups), added...) {
			if treeGroupMatches(&g, gr.source, gr.id, gr.level, skills) {
				found = true
				break
			}
		}
		if found {
			continue
		}
		// group = { label = "", enabled = true, gemList = { }, source = ..., slot = ... }
		// with activeGemInstance { skillId, quality = 0, enabled = true,
		// level = grantedSkill.level, enableGlobal1 = true }.
		added = append(added, pobcode.SocketGroup{
			Enabled: true,
			Source:  gr.source,
			Gems: []pobcode.Gem{{
				SkillID: gr.id, Level: gr.level, Enabled: true, EnableGlobal1: true,
			}},
		})
	}
	if len(added) == 0 {
		return b
	}
	cp := *b
	cp.SkillSets = append([]pobcode.SkillSet(nil), b.SkillSets...)
	cs := cp.ActiveSkills()
	cs.Groups = append(append([]pobcode.SocketGroup(nil), set.Groups...), added...)
	return &cp
}

// treeGroupMatches is CalcSetup.lua:1527-1528: the group has the grant's
// source, no slot (a node has no slotName) and the granted skill first, at
// the granted level or at that level validated (getNormalizedSkillLevel).
func treeGroupMatches(g *pobcode.SocketGroup, source, id string, level int, skills map[string]*gamedata.Skill) bool {
	if g.Source != source || g.Slot != "" || len(g.Gems) == 0 || id == "" || g.Gems[0].SkillID != id {
		return false
	}
	lvl := g.Gems[0].Level
	return lvl == level || (skills[id] != nil && lvl == validateLevel(level, skills[id], nil))
}
