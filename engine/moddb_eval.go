package engine

import (
	"math"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
)

// eval ports ModStoreClass:EvalMod for the tag types used by defence. It
// returns ok = false when a tag fails or is not ported (then counted).
func (db *ModDB) eval(m *Mod, cfg *Cfg) (any, bool) {
	value := m.Value
	// scale is EvalMod's value * mult + tag.base, which for a table value
	// scales its mod's value (LIST mods such as ExtraAura) or its value.
	scale := func(mult float64, t map[string]any) {
		base, _ := tagNum(t, "base")
		if f, ok := num(value); ok {
			value = f*mult + base
			return
		}
		v, ok := value.(map[string]any)
		if !ok {
			return
		}
		cp := make(map[string]any, len(v))
		for k, x := range v {
			cp[k] = x
		}
		if mm, isMod := v["mod"].(map[string]any); isMod {
			if f, isNum := num(mm["value"]); isNum {
				m2 := make(map[string]any, len(mm))
				for k, x := range mm {
					m2[k] = x
				}
				m2["value"] = f*mult + base
				cp["mod"] = m2
			}
		} else if f, isNum := num(v["value"]); isNum {
			cp["value"] = f*mult + base
		}
		value = cp
	}
	for _, t := range m.Tags {
		switch tagStr(t, "type") {
		case "Multiplier":
			target, ok := db.target(t)
			if !ok {
				return nil, false
			}
			base := 0.0
			if vars := tagList(t, "varList"); vars != nil {
				for _, v := range vars {
					base += target.getMultiplier(v, cfg, false)
				}
			} else {
				base = target.getMultiplier(tagStr(t, "var"), cfg, false)
			}
			div, ok := tagNum(t, "div")
			if dv := tagStr(t, "divVar"); dv != "" {
				// ModStore: tag.div = GetMultiplier(self, tag.divVar, cfg).
				div, ok = db.getMultiplier(dv, cfg, false), true
			}
			if !ok || div == 0 {
				div = 1
			}
			mult := math.Floor(base/div + 0.0001)
			if tagBool(t, "noFloor") {
				mult = base / div
			}
			// limitVar and limitStat read limitTarget: this actor, or limitActor.
			limitTarget := db
			if la := tagStr(t, "limitActor"); la != "" {
				lt, found := db.target(map[string]any{"actor": la})
				if !found {
					return nil, false
				}
				limitTarget = lt
			}
			limit, hasLimit := tagNum(t, "limit")
			if lv := tagStr(t, "limitVar"); !hasLimit && lv != "" {
				limit, hasLimit = limitTarget.getMultiplier(lv, cfg, false), true
			} else if ls := tagStr(t, "limitStat"); !hasLimit && ls != "" {
				limit, hasLimit = limitTarget.GetStat(ls), true
			}
			if hasLimit && !tagBool(t, "limitTotal") && !tagBool(t, "limitNegTotal") {
				mult = math.Min(mult, limit)
			}
			scale(mult, t)
			if f, isNum := num(value); isNum && hasLimit {
				if tagBool(t, "limitTotal") {
					value = math.Min(f, limit)
				} else if tagBool(t, "limitNegTotal") {
					value = math.Max(f, limit)
				}
			}
		case "MultiplierThreshold":
			target, ok := db.target(t)
			if !ok {
				return nil, false
			}
			mult := 0.0
			if vars := tagList(t, "varList"); vars != nil {
				for _, v := range vars {
					mult += target.getMultiplier(v, cfg, false)
				}
			} else {
				mult = target.getMultiplier(tagStr(t, "var"), cfg, false)
			}
			threshold, ok := tagNum(t, "threshold")
			if !ok {
				threshold = target.getMultiplier(tagStr(t, "thresholdVar"), cfg, false)
			}
			if (tagBool(t, "upper") && mult > threshold) || (tagBool(t, "equals") && mult != threshold) || (!tagBool(t, "upper") && mult < threshold) {
				return nil, false
			}
		case "PerStat":
			target, ok := db.target(t)
			if !ok {
				target = db
			}
			base := 0.0
			if stats := tagList(t, "statList"); stats != nil {
				for _, s := range stats {
					base += target.getStat(s, cfg)
				}
			} else {
				base = target.getStat(tagStr(t, "stat"), cfg)
			}
			div, ok := tagNum(t, "div")
			if dv := tagStr(t, "divVar"); dv != "" {
				// ModStore: tag.div = GetMultiplier(self, tag.divVar, cfg).
				div, ok = db.getMultiplier(dv, cfg, false), true
			}
			if !ok || div == 0 {
				div = 1
			}
			mult := math.Floor(base/div + 0.0001)
			limit, hasLimit := tagNum(t, "limit")
			if lv := tagStr(t, "limitVar"); !hasLimit && lv != "" {
				limit, hasLimit = db.getMultiplier(lv, cfg, false), true
			}
			if hasLimit && !tagBool(t, "limitTotal") {
				mult = math.Min(mult, limit)
			}
			scale(mult, t)
			if hasLimit && tagBool(t, "limitTotal") {
				if f, isNum := num(value); isNum {
					value = math.Min(f, limit)
				}
			}
		case "PercentStat":
			target, ok := db.target(t)
			if !ok {
				target = db
			}
			base := 0.0
			if stats := tagList(t, "statList"); stats != nil {
				for _, s := range stats {
					base += target.GetStat(s)
				}
			} else {
				base = target.GetStat(tagStr(t, "stat"))
			}
			mult := base
			if p, hasPercent := tagNum(t, "percent"); hasPercent {
				mult = base * p / 100
			} else if pv := tagStr(t, "percentVar"); pv != "" {
				mult = base * db.getMultiplier(pv, cfg, false) / 100
			}
			if tagBool(t, "floor") {
				mult = math.Floor(mult)
			}
			limit, hasLimit := tagNum(t, "limit")
			if lv := tagStr(t, "limitVar"); !hasLimit && lv != "" {
				limit, hasLimit = db.getMultiplier(lv, cfg, false), true
			}
			if hasLimit && !tagBool(t, "limitTotal") {
				mult = math.Min(mult, limit)
			}
			if f, isNum := num(value); isNum {
				b, _ := tagNum(t, "base")
				v := math.Ceil(f*mult + b)
				if hasLimit && tagBool(t, "limitTotal") {
					v = math.Min(v, limit)
				}
				value = v
			}
		case "StatThreshold":
			stat := 0.0
			if stats := tagList(t, "statList"); stats != nil {
				for _, s := range stats {
					stat += db.GetStat(s)
				}
			} else {
				stat = db.GetStat(tagStr(t, "stat"))
			}
			threshold, ok := tagNum(t, "threshold")
			if !ok {
				threshold = db.GetStat(tagStr(t, "thresholdStat"))
			}
			if p, hasPercent := tagNum(t, "thresholdPercent"); hasPercent {
				threshold *= p / 100
			}
			if (tagBool(t, "upper") && stat > threshold) || (!tagBool(t, "upper") && stat < threshold) {
				return nil, false
			}
		case "DistanceRamp":
			if cfg == nil || !cfg.HasSkillDist {
				return nil, false
			}
			ramp, _ := t["ramp"].([]any)
			pt := func(i int) (float64, float64) {
				p, _ := ramp[i].([]any)
				if len(p) < 2 {
					return 0, 0
				}
				a, _ := num(p[0])
				b, _ := num(p[1])
				return a, b
			}
			if len(ramp) == 0 {
				break
			}
			d := cfg.SkillDist
			d0, v0 := pt(0)
			dn, vn := pt(len(ramp) - 1)
			switch {
			case d <= d0:
				scale(v0, nil)
			case d >= dn:
				scale(vn, nil)
			default:
				for i := 0; i+1 < len(ramp); i++ {
					a, av := pt(i)
					b, bv := pt(i + 1)
					if d <= b {
						scale(av+(bv-av)*(d-a)/(b-a), nil)
						break
					}
				}
			}
		case "MeleeProximity":
			if cfg == nil || !cfg.HasSkillDist {
				return nil, false
			}
			ramp, _ := t["ramp"].([]any)
			mx := 0.0
			if len(ramp) > 0 {
				mx, _ = num(ramp[0])
			}
			switch d := cfg.SkillDist; {
			case d <= 15:
				scale(mx, nil)
			case d >= 16 && d <= 39:
				scale(mx-(mx/25)*(d-15), nil)
			case d >= 40:
				scale(0, nil)
			}
		case "Limit":
			limit, ok := tagNum(t, "limit")
			if !ok {
				limit = db.getMultiplier(tagStr(t, "limitVar"), cfg, false)
			}
			if f, isNum := num(value); isNum {
				value = math.Min(f, limit)
			}
		case "Condition":
			match := false
			// ModStore.lua:635-657: with a countsAsAll1H weapon, a negated
			// Using<type> condition that weapon added is ignored, and one it
			// found already set fails.
			neg := tagBool(t, "neg")
			added := db.root().all1HAdded
			if vars := tagList(t, "varList"); vars != nil {
				for _, v := range vars {
					if a, ok := added[v]; neg && ok {
						if !a {
							return nil, false
						}
					} else if db.getCondition(v, cfg, false) || (cfg != nil && cfg.SkillCond[v]) {
						match = true
						break
					}
				}
			} else {
				v := tagStr(t, "var")
				if a, ok := added[v]; neg && ok {
					if !a {
						return nil, false
					}
				} else {
					match = db.getCondition(v, cfg, false) || (cfg != nil && cfg.SkillCond[v])
				}
			}
			if tagBool(t, "neg") {
				match = !match
			}
			if !match {
				return nil, false
			}
		case "ItemCondition":
			if !db.itemCondition(t) {
				return nil, false
			}
		case "SocketedIn":
			if !socketedIn(t, cfg) {
				return nil, false
			}
		case "SkillName":
			if !db.skillNameTag(t, cfg) {
				return nil, false
			}
		case "SkillId":
			if cfg == nil || cfg.SkillID == "" || cfg.SkillID != tagStr(t, "skillId") {
				return nil, false
			}
		case "SkillPart":
			if cfg == nil {
				return nil, false
			}
			match := false
			if parts, ok := t["skillPartList"].([]any); ok {
				for _, p := range parts {
					if f, isNum := num(p); isNum && int(f) == cfg.SkillPart {
						match = true
						break
					}
				}
			} else if f, hasPart := tagNum(t, "skillPart"); hasPart {
				match = int(f) == cfg.SkillPart
			}
			if tagBool(t, "neg") {
				match = !match
			}
			if !match {
				return nil, false
			}
		case "SkillType":
			match := false
			if cfg != nil {
				if types, ok := t["skillTypeList"].([]any); ok {
					for _, ty := range types {
						if f, isNum := num(ty); isNum && cfg.SkillTypes[int(f)] {
							match = true
							break
						}
					}
				} else if f, hasType := tagNum(t, "skillType"); hasType {
					match = cfg.SkillTypes[int(f)]
				}
			}
			if tagBool(t, "neg") {
				match = !match
			}
			if !match {
				return nil, false
			}
		case "BaseFlag":
			match := cfg != nil && cfg.BaseFlags[tagStr(t, "baseFlag")]
			if tagBool(t, "neg") {
				match = !match
			}
			if !match {
				return nil, false
			}
		case "SlotName":
			if cfg == nil {
				return nil, false
			}
			match := false
			if slots := tagList(t, "slotNameList"); slots != nil {
				for _, sl := range slots {
					if sl == cfg.SlotName {
						match = true
						break
					}
				}
			} else {
				match = tagStr(t, "slotName") == cfg.SlotName
			}
			if tagBool(t, "neg") {
				match = !match
			}
			if !match {
				return nil, false
			}
		case "ModFlagOr":
			f, _ := tagNum(t, "modFlags")
			if cfg == nil || cfg.Flags&uint64(f) == 0 {
				return nil, false
			}
		case "KeywordFlagAnd":
			f, _ := tagNum(t, "keywordFlags")
			if cfg == nil || cfg.KeywordFlags&uint64(f) != uint64(f) {
				return nil, false
			}
		case "ActorCondition":
			target, _ := db.target(t)
			match := false
			if target != nil {
				if vars := tagList(t, "varList"); vars != nil {
					for _, v := range vars {
						if target.getCondition(v, cfg, false) {
							match = true
							break
						}
					}
				} else if v := tagStr(t, "var"); v != "" {
					match = target.getCondition(v, cfg, false)
				}
			}
			// PoB's elseif (ModStore.lua:682): the actor check applies only without a var test.
			_, hasVars := t["varList"]
			varTested := target != nil && (hasVars || tagStr(t, "var") != "")
			if a := tagStr(t, "actor"); !varTested && a != "" && cfg != nil && a == cfg.Actor {
				match = true
			}
			if tagBool(t, "neg") {
				match = !match
			}
			if !match {
				return nil, false
			}
		case "MonsterTag":
			if !monsterTagMatches(db.root().minionData, t) {
				return nil, false
			}
		default:
			// EvalMod ignores tag types it has no branch for (Global,
			// GlobalEffect, ...).
		}
	}
	return value, true
}

// socketedIn is the SocketedIn branch of ModStoreClass:EvalMod.
func socketedIn(t map[string]any, cfg *Cfg) bool {
	_, hasSlot := t["slotName"]
	_, hasKeyword := t["keyword"]
	_, hasColor := t["socketColor"]
	if cfg == nil || (!hasSlot && !hasKeyword && !hasColor) {
		return false
	}
	match := map[string]bool{}
	if hasSlot {
		match["slotName"] = tagStr(t, "slotName") == cfg.SlotName
	}
	sockets, hasSockets := t["sockets"]
	if hasKeyword {
		match["keyword"] = cfg.SkillGem != nil && gemIsType(cfg.SkillGem, tagStr(t, "keyword"), nil)
	} else if hasColor && sockets != "all" {
		match["socketColor"] = tagStr(t, "socketColor") == cfg.SocketColor
	}
	if hasSockets {
		attr := map[string]string{"R": "strengthGems", "G": "dexterityGems", "B": "intelligenceGems"}[tagStr(t, "socketColor")]
		count := cfg.GemCounts[attr]
		switch x := sockets.(type) {
		case string:
			if x != "all" {
				return false
			}
			total := cfg.GemCounts["intelligenceGems"] + cfg.GemCounts["dexterityGems"] + cfg.GemCounts["strengthGems"]
			match["sockets"] = total == count && total > 0
		case []any:
			if cfg.SocketNum == 0 {
				return false
			}
			found := false
			for _, v := range x {
				if f, ok := num(v); ok && int(f) == cfg.SocketNum {
					found = true
				}
			}
			match["sockets"] = found
		case float64:
			match["sockets"] = float64(count) < x
		default:
			return false
		}
	}
	neg := tagBool(t, "neg")
	for _, v := range match {
		if (!neg && !v) || (neg && v) {
			return false
		}
	}
	return true
}

// skillNameTag is the SkillName branch of ModStoreClass:EvalMod.
func (db *ModDB) skillNameTag(t map[string]any, cfg *Cfg) bool {
	name := ""
	if cfg != nil {
		name = cfg.SkillName
		if tagBool(t, "summonSkill") {
			name = cfg.SummonSkillName
		}
	}
	match := false
	if tagBool(t, "includeTransfigured") {
		if db.gameIDs == nil {
			db.UnsupportedTags["SkillName(includeTransfigured)"]++
			return false
		}
		id := db.gameIDs(name)
		if names := tagList(t, "skillNameList"); names != nil {
			for _, n := range names {
				if id != "" && id == db.gameIDs(n) {
					match = true
					break
				}
			}
		} else if sn := tagStr(t, "skillName"); sn != "" {
			match = id != "" && id == db.gameIDs(sn)
		}
	} else {
		name = strings.ToLower(name)
		if names := tagList(t, "skillNameList"); names != nil {
			for _, n := range names {
				if strings.ToLower(n) == name {
					match = true
					break
				}
			}
		} else if sn, ok := t["skillName"].(string); ok {
			match = strings.ToLower(sn) == name
		}
	}
	if tagBool(t, "neg") {
		match = !match
	}
	return match
}

// monsterTagMatches is the MonsterTag branch of ModStoreClass:EvalMod
// (ModStore.lua:925): the actor is a minion with a monster tag in the
// tag's monsterTagList (or equal to its monsterTag), case-insensitively.
func monsterTagMatches(md *gamedata.Minion, t map[string]any) bool {
	if md == nil || md.MonsterTags == nil {
		return false
	}
	match := false
	for _, tag := range md.MonsterTags {
		if l, ok := t["monsterTagList"].([]any); ok {
			for _, n := range l {
				if s, _ := n.(string); strings.EqualFold(s, tag) {
					match = true
					break
				}
			}
		} else {
			match = strings.EqualFold(tagStr(t, "monsterTag"), tag)
		}
		if match {
			break
		}
	}
	if tagBool(t, "neg") {
		match = !match
	}
	return match
}
