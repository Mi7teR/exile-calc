// Package engine computes character stats from a PoB build, porting PoB's
// Modules/Calc*.lua and Classes/ModDB.lua. Every formula names its source.
// What is not ported yet is reported, never approximated.
package engine

import (
	"math"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"

	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/modparser"
)

// Mod is a modifier in the database (Modules/ModTools.lua createMod).
type Mod struct {
	Name         string
	Type         string // BASE, INC, MORE, FLAG, OVERRIDE, LIST
	Value        any    // float64 for numeric types, bool for FLAG
	Flags        uint64
	KeywordFlags uint64
	Tags         []map[string]any
	Source       string
}

func fromParsed(m modparser.Mod, source string) Mod {
	return Mod{Name: m.Name, Type: m.Type, Value: m.Value, Flags: m.Flags, KeywordFlags: m.KeywordFlags, Tags: m.Tags, Source: source}
}

// ModDB is one actor's modifier store (Classes/ModDB.lua, ModStore.lua).
type ModDB struct {
	mods        map[string][]Mod
	conditions  map[string]bool
	multipliers map[string]float64
	enemyEnv    *enemyEnv          // the player's: config about the enemy
	output      map[string]float64 // the actor's computed stats, read by PerStat
	enemy       *ModDB
	items       map[string]*itemtext.Item // the actor's itemList, by slot name
	tags        itemTags
	classConds  map[string]string           // conditions[item title] = item.classRestriction
	parent      *ModDB                      // a skill's mod list reads through to the actor's ModDB
	gameIDs     func(gemName string) string // calcLib.getGameIdFromGemName(name, true)

	// UnsupportedTags counts tag types EvalMod does not port yet; mods
	// carrying them are left out of every sum.
	UnsupportedTags map[string]int
}

func NewModDB() *ModDB {
	return &ModDB{
		mods:            map[string][]Mod{},
		conditions:      map[string]bool{},
		multipliers:     map[string]float64{},
		output:          map[string]float64{},
		items:           map[string]*itemtext.Item{},
		classConds:      map[string]string{},
		UnsupportedTags: map[string]int{},
	}
}

func (db *ModDB) Add(m Mod) { db.mods[m.Name] = append(db.mods[m.Name], m) }

// NewMod is ModStoreClass:NewMod for a numeric value with no tags.
func (db *ModDB) NewMod(name, typ string, value float64, source string, tags ...map[string]any) {
	db.Add(Mod{Name: name, Type: typ, Value: value, Source: source, Tags: tags})
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case string:
		// Lua arithmetic coerces numeric strings (ModParser.lua keeps some
		// captures as strings, e.g. "Bone Offering has 11% increased Effect").
		if f, err := strconv.ParseFloat(strings.TrimSpace(x), 64); err == nil {
			return f, true
		}
	}
	return 0, false
}

// Enemy is the actor's enemy's mod database.
func (db *ModDB) Enemy() *ModDB { return db.enemy }

// child is new("ModList"):ModList(db): a list that reads through to db,
// as a skill's skillModList does (CalcActiveSkill.lua).
func (db *ModDB) child() *ModDB {
	c := NewModDB()
	c.parent, c.enemy, c.output = db, db.enemy, db.output
	c.items, c.tags, c.classConds, c.UnsupportedTags, c.gameIDs = db.items, db.tags, db.classConds, db.UnsupportedTags, db.gameIDs
	return c
}

// Cfg is PoB's ModCfg: the skill a query is made for. A nil Cfg is a
// query without one (the defence and attribute paths).
type Cfg struct {
	Flags, KeywordFlags uint64
	SkillName           string
	SummonSkillName     string
	SkillID             string // skillGrantedEffect.id
	BaseFlags           map[string]bool
	SkillTypes          map[int]bool
	SkillPart           int
	SlotName            string
	SkillGem            *gamedata.Gem
	SocketColor         string
	SocketNum           int
	GemCounts           map[string]int // strengthGems, dexterityGems, intelligenceGems
	Actor               string         // cfg.actor: an ActorCondition without var matches it
}

const keywordMatchAll = 0x40000000 // KeywordFlag.MatchAll

// matchKeywordFlags is Global.lua MatchKeywordFlags.
func matchKeywordFlags(keywordFlags, modKeywordFlags uint64) bool {
	matchAll := modKeywordFlags&keywordMatchAll != 0
	mod := modKeywordFlags &^ keywordMatchAll
	kw := keywordFlags &^ keywordMatchAll
	if matchAll {
		return kw&mod == mod
	}
	return mod == 0 || kw&mod != 0
}

// eligible is the selection of ModDB SumInternal and its siblings:
// band(flags, mod.flags) == mod.flags and MatchKeywordFlags.
func eligible(m *Mod, typ string, cfg *Cfg) bool {
	var flags, kw uint64
	if cfg != nil {
		flags, kw = cfg.Flags, cfg.KeywordFlags
	}
	return m.Type == typ && flags&m.Flags == m.Flags && matchKeywordFlags(kw, m.KeywordFlags)
}

// Sum is ModDB:Sum(modType, nil, names...).
func (db *ModDB) Sum(typ string, names ...string) float64 { return db.SumCfg(nil, typ, names...) }

// SumCfg is ModDB:Sum(modType, cfg, names...).
func (db *ModDB) SumCfg(cfg *Cfg, typ string, names ...string) float64 {
	return db.sumInternal(db, cfg, typ, names)
}

func (db *ModDB) sumInternal(ctx *ModDB, cfg *Cfg, typ string, names []string) float64 {
	total := 0.0
	for _, name := range names {
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !eligible(m, typ, cfg) {
				continue
			}
			v, ok := ctx.eval(m, cfg)
			if f, isNum := num(v); ok && isNum {
				total += f
			}
		}
	}
	if db.parent != nil {
		total += db.parent.sumInternal(ctx, cfg, typ, names)
	}
	return total
}

// More is ModDB:More(nil, names...): per name the product of (1 + v/100),
// rounded to two decimals, then multiplied across names.
func (db *ModDB) More(names ...string) float64 { return db.MoreCfg(nil, names...) }

// MoreCfg is ModDB:More(cfg, names...).
func (db *ModDB) MoreCfg(cfg *Cfg, names ...string) float64 {
	return db.moreInternal(db, cfg, names)
}

func (db *ModDB) moreInternal(ctx *ModDB, cfg *Cfg, names []string) float64 {
	result := 1.0
	for _, name := range names {
		mod := 1.0
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !eligible(m, "MORE", cfg) {
				continue
			}
			v, ok := ctx.eval(m, cfg)
			if f, isNum := num(v); ok && isNum {
				mod *= 1 + f/100
			}
		}
		// data.highPrecisionMods is not ported: every name rounds to 2 places.
		result *= round(mod, 2)
	}
	if db.parent != nil {
		result *= db.parent.moreInternal(ctx, cfg, names)
	}
	return result
}

// Flag is ModDB:Flag(nil, names...).
func (db *ModDB) Flag(names ...string) bool { return db.FlagCfg(nil, names...) }

// FlagCfg is ModDB:Flag(cfg, names...).
func (db *ModDB) FlagCfg(cfg *Cfg, names ...string) bool { return db.flagInternal(db, cfg, names) }

func (db *ModDB) flagInternal(ctx *ModDB, cfg *Cfg, names []string) bool {
	for _, name := range names {
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !eligible(m, "FLAG", cfg) {
				continue
			}
			if v, ok := ctx.eval(m, cfg); ok && v != nil && v != false {
				return true
			}
		}
	}
	return db.parent != nil && db.parent.flagInternal(ctx, cfg, names)
}

// Override is ModDB:Override(nil, names...); ok is false when none applies.
func (db *ModDB) Override(names ...string) (float64, bool) { return db.OverrideCfg(nil, names...) }

// OverrideCfg is ModDB:Override(cfg, names...).
func (db *ModDB) OverrideCfg(cfg *Cfg, names ...string) (float64, bool) {
	return db.overrideInternal(db, cfg, names)
}

func (db *ModDB) overrideInternal(ctx *ModDB, cfg *Cfg, names []string) (float64, bool) {
	for _, name := range names {
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !eligible(m, "OVERRIDE", cfg) {
				continue
			}
			if v, ok := ctx.eval(m, cfg); ok {
				if f, isNum := num(v); isNum {
					return f, true
				}
			}
		}
	}
	if db.parent != nil {
		return db.parent.overrideInternal(ctx, cfg, names)
	}
	return 0, false
}

// ListEntry is one row of Tabulate("LIST", ...): the evaluated value and
// the mod it came from.
type ListEntry struct {
	Value any
	Mod   *Mod
}

// List is ModDB:Tabulate("LIST", cfg, names...); its Value fields are
// ModDB:List(cfg, names...).
func (db *ModDB) List(cfg *Cfg, names ...string) []ListEntry {
	var out []ListEntry
	db.listInternal(db, cfg, names, &out)
	return out
}

func (db *ModDB) listInternal(ctx *ModDB, cfg *Cfg, names []string, out *[]ListEntry) {
	for _, name := range names {
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !eligible(m, "LIST", cfg) {
				continue
			}
			if v, ok := ctx.eval(m, cfg); ok && v != nil {
				*out = append(*out, ListEntry{Value: v, Mod: m})
			}
		}
	}
	if db.parent != nil {
		db.parent.listInternal(ctx, cfg, names, out)
	}
}

// GetMultiplier is ModStoreClass:GetMultiplier.
func (db *ModDB) GetMultiplier(v string) float64 { return db.getMultiplier(v, nil, false) }

func (db *ModDB) getMultiplier(v string, cfg *Cfg, noMod bool) float64 {
	if !noMod {
		if o, ok := db.OverrideCfg(cfg, "Multiplier:"+v); ok {
			return o
		}
	}
	total := db.multipliers[v]
	if db.parent != nil {
		total += db.parent.getMultiplier(v, cfg, true)
	}
	if !noMod {
		total += db.SumCfg(cfg, "BASE", "Multiplier:"+v)
	}
	return total
}

// GetCondition is ModStoreClass:GetCondition.
func (db *ModDB) GetCondition(v string) bool { return db.getCondition(v, nil, false) }

func (db *ModDB) getCondition(v string, cfg *Cfg, noMod bool) bool {
	return db.conditions[v] || (db.parent != nil && db.parent.getCondition(v, cfg, true)) ||
		(!noMod && db.FlagCfg(cfg, "Condition:"+v))
}

// GetStat is ModStoreClass:GetStat for plain output stats.
func (db *ModDB) GetStat(stat string) float64 { return db.output[stat] }

func tagNum(t map[string]any, k string) (float64, bool) { return num(t[k]) }

func tagStr(t map[string]any, k string) string { s, _ := t[k].(string); return s }

func tagBool(t map[string]any, k string) bool { b, _ := t[k].(bool); return b }

func tagList(t map[string]any, k string) []string {
	l, _ := t[k].([]any)
	var out []string
	for _, v := range l {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// target is the actor a tag names (ModStore getActor): self or the enemy.
func (db *ModDB) target(t map[string]any) (*ModDB, bool) {
	switch tagStr(t, "actor") {
	case "":
		return db, true
	case "enemy":
		return db.enemy, db.enemy != nil
	}
	return nil, false
}

// eval ports ModStoreClass:EvalMod for the tag types used by defence. It
// returns ok = false when a tag fails or is not ported (then counted).
func (db *ModDB) eval(m *Mod, cfg *Cfg) (any, bool) {
	value := m.Value
	scale := func(mult float64, t map[string]any) {
		base, _ := tagNum(t, "base")
		if f, ok := num(value); ok {
			value = f*mult + base
		}
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
			if !ok || div == 0 {
				div = 1
			}
			mult := math.Floor(base/div + 0.0001)
			if tagBool(t, "noFloor") {
				mult = base / div
			}
			if limit, ok := tagNum(t, "limit"); ok && !tagBool(t, "limitTotal") {
				mult = math.Min(mult, limit)
			}
			scale(mult, t)
			if limit, ok := tagNum(t, "limit"); ok && tagBool(t, "limitTotal") {
				if f, isNum := num(value); isNum {
					value = math.Min(f, limit)
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
					base += target.GetStat(s)
				}
			} else {
				base = target.GetStat(tagStr(t, "stat"))
			}
			div, ok := tagNum(t, "div")
			if !ok || div == 0 {
				div = 1
			}
			mult := math.Floor(base/div + 0.0001)
			if limit, ok := tagNum(t, "limit"); ok && !tagBool(t, "limitTotal") {
				mult = math.Min(mult, limit)
			}
			scale(mult, t)
			if limit, ok := tagNum(t, "limit"); ok && tagBool(t, "limitTotal") {
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
			if p, ok := tagNum(t, "percent"); ok {
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
				value = math.Ceil(f*mult + b)
				if hasLimit && tagBool(t, "limitTotal") {
					value = math.Min(value.(float64), limit)
				}
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
			if p, ok := tagNum(t, "thresholdPercent"); ok {
				threshold *= p / 100
			}
			if (tagBool(t, "upper") && stat > threshold) || (!tagBool(t, "upper") && stat < threshold) {
				return nil, false
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
			if vars := tagList(t, "varList"); vars != nil {
				for _, v := range vars {
					if db.getCondition(v, cfg, false) {
						match = true
						break
					}
				}
			} else {
				match = db.getCondition(tagStr(t, "var"), cfg, false)
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
					if f, ok := num(p); ok && int(f) == cfg.SkillPart {
						match = true
						break
					}
				}
			} else if f, ok := tagNum(t, "skillPart"); ok {
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
						if f, ok := num(ty); ok && cfg.SkillTypes[int(f)] {
							match = true
							break
						}
					}
				} else if f, ok := tagNum(t, "skillType"); ok {
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
			_, hasVars := t["varList"]
			if a := tagStr(t, "actor"); !(target != nil && (hasVars || tagStr(t, "var") != "")) && a != "" && cfg != nil && a == cfg.Actor {
				match = true
			}
			if tagBool(t, "neg") {
				match = !match
			}
			if !match {
				return nil, false
			}
		default:
			// EvalMod ignores tag types it has no branch for (Global,
			// GlobalEffect, ...); the types it does handle but we have not
			// ported yet leave the mod out and are counted.
			if typ := tagStr(t, "type"); unportedTags[typ] {
				db.UnsupportedTags[typ]++
				return nil, false
			}
		}
	}
	return value, true
}

// unportedTags are the remaining tag types ModStoreClass:EvalMod handles.
var unportedTags = map[string]bool{
	"DistanceRamp": true, "MeleeProximity": true, "MonsterTag": true,
}

// round is PoB's round(v, places) (Modules/Common.lua): floor(v*10^p + 0.5).
func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Floor(v*p+0.5) / p
}

// modfInt truncates toward zero like Lua's math.modf integer part.
func modfInt(v float64) float64 {
	i, _ := math.Modf(v)
	return i
}

// Row is one entry of Tabulate.
type Row struct {
	Type   string
	Value  any
	Source string
}

// Tabulate is ModStore:Tabulate(nil, nil, name): the mods that apply, with
// their evaluated values. Used to compare against PoB when debugging.
func (db *ModDB) Tabulate(name string) []Row {
	var rows []Row
	for i := range db.mods[name] {
		m := &db.mods[name][i]
		if m.Flags != 0 || m.KeywordFlags != 0 {
			continue
		}
		v, ok := db.eval(m, nil)
		if !ok {
			continue
		}
		if f, isNum := num(v); isNum && f == 0 && m.Type != "OVERRIDE" {
			continue
		}
		rows = append(rows, Row{Type: m.Type, Value: v, Source: m.Source})
	}
	return rows
}

// tabulateType is ModStore:Tabulate(typ, nil, name).
func (db *ModDB) tabulateType(name, typ string) []Row {
	var rows []Row
	for _, r := range db.Tabulate(name) {
		if r.Type == typ {
			rows = append(rows, r)
		}
	}
	return rows
}

// Conditions returns the actor's set conditions ("Condition:X" = 1) and
// multipliers ("Multiplier:X" = value), for comparing with PoB.
func (db *ModDB) Conditions() map[string]float64 {
	out := map[string]float64{}
	for k, v := range db.conditions {
		if v {
			out["Condition:"+k] = 1
		}
	}
	for k, v := range db.multipliers {
		if v != 0 {
			out["Multiplier:"+k] = v
		}
	}
	return out
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
