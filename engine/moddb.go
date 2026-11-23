// Package engine computes character stats from a PoB build, porting PoB's
// Modules/Calc*.lua and Classes/ModDB.lua. Every formula names its source.
// What is not ported yet is reported, never approximated.
package engine

import (
	"math"

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
	output      map[string]float64 // the actor's computed stats, read by PerStat
	enemy       *ModDB

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
	}
	return 0, false
}

// Calls without a cfg (the defence and attribute paths) select only mods
// with no flags: ModDB SumInternal tests band(flags, mod.flags) == mod.flags
// with flags = 0.
func (db *ModDB) eligible(m *Mod, typ string) bool {
	return m.Type == typ && m.Flags == 0 && m.KeywordFlags == 0
}

// Sum is ModDB:Sum(modType, nil, names...).
func (db *ModDB) Sum(typ string, names ...string) float64 {
	total := 0.0
	for _, name := range names {
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !db.eligible(m, typ) {
				continue
			}
			v, ok := db.eval(m)
			if f, isNum := num(v); ok && isNum {
				total += f
			}
		}
	}
	return total
}

// More is ModDB:More(nil, names...): per name the product of (1 + v/100),
// rounded to two decimals, then multiplied across names.
func (db *ModDB) More(names ...string) float64 {
	result := 1.0
	for _, name := range names {
		mod := 1.0
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !db.eligible(m, "MORE") {
				continue
			}
			v, ok := db.eval(m)
			if f, isNum := num(v); ok && isNum {
				mod *= 1 + f/100
			}
		}
		// data.highPrecisionMods is not ported: every name rounds to 2 places.
		result *= round(mod, 2)
	}
	return result
}

// Flag is ModDB:Flag(nil, names...).
func (db *ModDB) Flag(names ...string) bool {
	for _, name := range names {
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !db.eligible(m, "FLAG") {
				continue
			}
			if v, ok := db.eval(m); ok && v != nil && v != false {
				return true
			}
		}
	}
	return false
}

// Override is ModDB:Override(nil, names...); ok is false when none applies.
func (db *ModDB) Override(names ...string) (float64, bool) {
	for _, name := range names {
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if !db.eligible(m, "OVERRIDE") {
				continue
			}
			if v, ok := db.eval(m); ok {
				if f, isNum := num(v); isNum {
					return f, true
				}
			}
		}
	}
	return 0, false
}

// GetMultiplier is ModStoreClass:GetMultiplier.
func (db *ModDB) GetMultiplier(v string) float64 {
	if o, ok := db.Override("Multiplier:" + v); ok {
		return o
	}
	return db.multipliers[v] + db.Sum("BASE", "Multiplier:"+v)
}

// GetCondition is ModStoreClass:GetCondition.
func (db *ModDB) GetCondition(v string) bool {
	return db.conditions[v] || db.Flag("Condition:"+v)
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
func (db *ModDB) eval(m *Mod) (any, bool) {
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
					base += target.GetMultiplier(v)
				}
			} else {
				base = target.GetMultiplier(tagStr(t, "var"))
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
					mult += target.GetMultiplier(v)
				}
			} else {
				mult = target.GetMultiplier(tagStr(t, "var"))
			}
			threshold, ok := tagNum(t, "threshold")
			if !ok {
				threshold = target.GetMultiplier(tagStr(t, "thresholdVar"))
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
				limit = db.GetMultiplier(tagStr(t, "limitVar"))
			}
			if f, isNum := num(value); isNum {
				value = math.Min(f, limit)
			}
		case "Condition":
			match := false
			if vars := tagList(t, "varList"); vars != nil {
				for _, v := range vars {
					if db.GetCondition(v) {
						match = true
						break
					}
				}
			} else {
				match = db.GetCondition(tagStr(t, "var"))
			}
			if tagBool(t, "neg") {
				match = !match
			}
			if !match {
				return nil, false
			}
		case "ActorCondition":
			target, _ := db.target(t)
			match := false
			if target != nil {
				if vars := tagList(t, "varList"); vars != nil {
					for _, v := range vars {
						if target.GetCondition(v) {
							match = true
							break
						}
					}
				} else if v := tagStr(t, "var"); v != "" {
					match = target.GetCondition(v)
				}
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
	"PercentStat": true, "DistanceRamp": true, "MeleeProximity": true, "ItemCondition": true,
	"SocketedIn": true, "SkillName": true, "SkillId": true, "SkillPart": true, "SkillType": true,
	"BaseFlag": true, "SlotName": true, "ModFlagOr": true, "KeywordFlagAnd": true, "MonsterTag": true,
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
		v, ok := db.eval(m)
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
