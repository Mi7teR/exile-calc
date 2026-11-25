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
	"github.com/Mi7teR/poe-lib/pobcode"
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
	classConds  map[string]string          // conditions[item title] = item.classRestriction
	build       *pobcode.Build             // the build being calculated (minion item sets)
	strDmgBonus float64                    // actor.strDmgBonus
	gearMods    map[string][]modparser.Mod // the player's parsed gear mods per slot (weaponData)
	parent      *ModDB                     // a skill's mod list reads through to the actor's ModDB
	isList      bool                       // a ModList (child): EvalMod without global limits
	owner       *ModDB                     // a minion's parent actor (tag actor "parent")
	player      *ModDB                     // actor.player: env.player for the player and its enemy
	// activeSkills is actor.activeSkillList.
	activeSkills []*activeSkill
	// minionSkillCache holds the skills of non-main minions, by summoning
	// skill, built once per calculation.
	minionSkillCache map[*activeSkill][]*activeSkill
	gameIDs          func(gemName string) string // calcLib.getGameIdFromGemName(name, true)

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
	c.isList = true
	c.parent, c.enemy, c.output, c.owner, c.build, c.enemyEnv = db, db.enemy, db.output, db.owner, db.build, db.enemyEnv
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
	GemCounts           map[string]int  // strengthGems, dexterityGems, intelligenceGems
	Actor               string          // cfg.actor: an ActorCondition without var matches it
	SkillCond           map[string]bool // cfg.skillCond: conditions of this query (CriticalStrike, MainHandAttack)
	SkillDist           float64         // cfg.skillDist: melee or projectile distance to the enemy
	HasSkillDist        bool
	SkillStats          map[string]float64 // cfg.skillStats: the hand pass output, read by GetStat when the actor has no such stat
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
	var limits map[string]float64
	for _, name := range names {
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !eligible(m, typ, cfg) {
				continue
			}
			v, ok := ctx.eval(m, cfg)
			if f, isNum := num(v); ok && isNum {
				if !db.isList {
					f = globalLimit(m, f, &limits)
				}
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
	var limits map[string]float64
	for _, name := range names {
		mod := 1.0
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !eligible(m, "MORE", cfg) {
				continue
			}
			v, ok := ctx.eval(m, cfg)
			if f, isNum := num(v); ok && isNum {
				if !db.isList {
					f = globalLimit(m, f, &limits)
				}
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
func (db *ModDB) GetStat(stat string) float64 { return db.getStat(stat, nil) }

// getStat is ModStoreClass:GetStat: ManaReservedPercent and
// LifeReservedPercent are the queried skill's own reservation (the skill
// whose buff has the cfg's skill name), else the actor's output.
func (db *ModDB) getStat(stat string, cfg *Cfg) float64 {
	if stat == "ManaReservedPercent" || stat == "LifeReservedPercent" {
		actor := db.root()
		pool := strings.TrimSuffix(stat, "ReservedPercent")
		total := actor.output[pool]
		if total == 0 {
			return 0
		}
		pct := 0.0
		for _, s := range actor.activeSkills {
			if !s.types[stHasReservation] || s.disabled || len(s.buffs) == 0 || cfg == nil {
				continue
			}
			match := false
			for _, b := range s.buffs {
				match = match || b.name == cfg.SkillName || (cfg.SummonSkillName != "" && b.name == cfg.SummonSkillName)
			}
			if match {
				pct = math.Floor(numOr(s.data.val(pool+"ReservedBase"), 0) / total * 100) //nolint:mnd // PoB literal
				break
			}
		}
		return math.Min(pct, 100)
	}
	// (self.actor.output and self.actor.output[stat]) or (cfg and cfg.skillStats and cfg.skillStats[stat]) or 0
	if v, ok := db.output[stat]; ok {
		return v
	}
	if cfg != nil {
		return cfg.SkillStats[stat]
	}
	return 0
}

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
	case "parent":
		return db.owner, db.owner != nil
	case "player":
		// getActor: actor.player, else the parent's or the enemy's.
		r := db.root()
		for _, a := range []*ModDB{r, r.owner, r.enemy} {
			if a != nil && a.root().player != nil {
				return a.root().player, true
			}
		}
	}
	return nil, false
}

// unportedTags are the remaining tag types ModStoreClass:EvalMod handles.
var unportedTags = map[string]bool{
	"MonsterTag": true,
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
	Flags  uint64
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

// MaxCfg is ModStore:Max: the largest MAX mod value (ok false without one
// above 0, as PoB's nil).
func (db *ModDB) MaxCfg(cfg *Cfg, names ...string) (float64, bool) {
	max, found := 0.0, false
	for _, n := range names {
		for _, r := range db.TabulateCfg(cfg, "MAX", n) {
			if f, ok := num(r.Value); ok && f > max {
				max, found = f, true
			}
		}
	}
	return max, found
}

// TabulateCfg is ModStore:Tabulate(typ, cfg, name) over the parent chain.
func (db *ModDB) TabulateCfg(cfg *Cfg, typ, name string) []Row {
	var rows []Row
	for d := db; d != nil; d = d.parent {
		for i := range d.mods[name] {
			m := &d.mods[name][i]
			if !eligible(m, typ, cfg) {
				continue
			}
			if v, ok := db.eval(m, cfg); ok {
				rows = append(rows, Row{Type: m.Type, Value: v, Source: m.Source, Flags: m.Flags})
			}
		}
	}
	return rows
}

// theBuild is env.build for any actor: a minion's ModDB has no build of
// its own, so this follows the owner chain to the player's.
func (db *ModDB) theBuild() *pobcode.Build {
	for d := db.root(); d != nil; d = d.owner {
		if d.build != nil {
			return d.build
		}
	}
	return nil
}

// root is the end of the parent chain: the actor's ModDB.
func (db *ModDB) root() *ModDB {
	for db.parent != nil {
		db = db.parent
	}
	return db
}

// globalLimit is the "Apply global limits" end of ModStore:EvalMod: tags
// with globalLimit and globalLimitKey share one cap across the mods of a
// single ModDB Sum/More call (limits is that call's table).
func globalLimit(m *Mod, v float64, limits *map[string]float64) float64 {
	for _, t := range m.Tags {
		limit, ok := tagNum(t, "globalLimit")
		key, _ := t["globalLimitKey"].(string)
		if !ok || key == "" {
			continue
		}
		if *limits == nil {
			*limits = map[string]float64{}
		}
		if (*limits)[key]+v > limit {
			v = limit - (*limits)[key]
		}
		(*limits)[key] += v
	}
	return v
}
