package main

import (
	"errors"
	"fmt"
	"sort"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/internal/luadata"
)

// convertSkills exports what Modules/Data.lua loads as data.skills and
// data.skillStatMap. Skill files and SkillStatMap.lua are functions of
// (skills, mod, flag, skill); evaluated without those, mod(...) calls stay
// calls and are turned into mods here (Data.lua makeSkillMod, makeFlagMod,
// makeSkillDataMod). Fields the engine does not read are left out.
func convertSkills(read func(string) (string, error), env map[string]luadata.Value) (*gamedata.Skills, error) {
	out := &gamedata.Skills{Effects: map[string]*gamedata.Skill{}}
	if bit, ok := env["bit"].(*luadata.Table); ok {
		bit.Set("band", luadata.Builtin(func(args []luadata.Value) (luadata.Value, error) {
			out := ^uint64(0)
			for _, a := range args {
				n, ok := a.(luadata.Number)
				if !ok {
					return nil, luadata.ErrKeepCall
				}
				out &= uint64(n)
			}
			return luadata.Number(out), nil
		}))
	}
	for _, f := range skillFiles {
		rel := "Data/Skills/" + f + ".lua"
		src, err := read(rel)
		if err != nil {
			return nil, err
		}
		v, err := luadata.Eval(src, env)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		t, ok := v.(*luadata.Table)
		if !ok {
			return nil, fmt.Errorf("%s: not a table", rel)
		}
		for _, k := range t.Keys {
			id := keyText(k)
			st, ok := t.Get(k).(*luadata.Table)
			if !ok {
				return nil, fmt.Errorf("%s: %s is not a table", rel, id)
			}
			s, err := convertSkill(st)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", rel, id, err)
			}
			out.Effects[id] = s
		}
	}
	src, err := read("Data/SkillStatMap.lua")
	if err != nil {
		return nil, err
	}
	v, err := luadata.Eval(src, env)
	if err != nil {
		return nil, fmt.Errorf("SkillStatMap.lua: %w", err)
	}
	t, ok := v.(*luadata.Table)
	if !ok {
		return nil, fmt.Errorf("SkillStatMap.lua: not a table")
	}
	if out.StatMap, err = convertStatMaps(t); err != nil {
		return nil, fmt.Errorf("SkillStatMap.lua: %w", err)
	}
	return out, nil
}

func convertSkill(t *luadata.Table) (*gamedata.Skill, error) {
	s := &gamedata.Skill{}
	str := func(k string) string { v, _ := t.Get(k).(luadata.String); return string(v) }
	b := func(k string) bool { v, _ := t.Get(k).(luadata.Bool); return bool(v) }
	f := func(k string) float64 { v, _ := t.Get(k).(luadata.Number); return float64(v) }
	s.Name = sanitiseText(str("name"))
	s.Color = int(f("color"))
	s.Support, s.IgnoreMinionTypes, s.IsTrigger = b("support"), b("ignoreMinionTypes"), b("isTrigger")
	s.PlusVersionOf = str("plusVersionOf")
	s.CannotBeSupported, s.SupportGemsOnly = b("cannotBeSupported"), b("supportGemsOnly")
	s.Unsupported, s.FromItem = b("unsupported"), b("fromItem")
	s.BaseEffectiveness, s.IncrementalEffectiveness = f("baseEffectiveness"), f("incrementalEffectiveness")
	s.SkillTotemID = int(f("skillTotemId"))
	s.MinionHasItemSet = b("minionHasItemSet")
	s.CastTime = f("castTime")
	_, hasCast := t.Get("castTime").(luadata.Number)
	s.NoCastTime = !hasCast
	if uses, ok := t.Get("minionUses").(*luadata.Table); ok {
		for _, k := range uses.Keys {
			s.MinionUses = append(s.MinionUses, keyText(k))
		}
		sort.Strings(s.MinionUses)
	}
	for name, dst := range map[string]*[]string{"minionList": &s.MinionList, "addMinionList": &s.AddMinionList} {
		if err := luadata.Decode(t.Get(name), dst); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}

	var err error
	if s.SkillTypes, err = intKeys(t.Get("skillTypes")); err != nil {
		return nil, fmt.Errorf("skillTypes: %w", err)
	}
	if s.MinionSkillTypes, err = intKeys(t.Get("minionSkillTypes")); err != nil {
		return nil, fmt.Errorf("minionSkillTypes: %w", err)
	}
	for name, dst := range map[string]*[]int{"addSkillTypes": &s.AddSkillTypes, "requireSkillTypes": &s.RequireSkillTypes, "excludeSkillTypes": &s.ExcludeSkillTypes} {
		if err := luadata.Decode(t.Get(name), dst); err != nil {
			return nil, err
		}
	}
	if parts, ok := t.Get("parts").(*luadata.Table); ok {
		for _, pv := range parts.List {
			pt, ok := pv.(*luadata.Table)
			if !ok {
				continue
			}
			part := gamedata.SkillPart{}
			for _, k := range pt.Keys {
				switch v := pt.Get(k).(type) {
				case luadata.String:
					if keyText(k) == "name" {
						part.Name = sanitiseText(string(v))
					}
				case luadata.Bool:
					if part.Flags == nil {
						part.Flags = map[string]bool{}
					}
					part.Flags[keyText(k)] = bool(v)
				}
			}
			s.Parts = append(s.Parts, part)
		}
	}
	s.AddFlags = trueKeys(t.Get("addFlags"))
	s.WeaponTypes = trueKeys(t.Get("weaponTypes"))
	s.BaseFlags = trueKeys(t.Get("baseFlags"))
	if err := luadata.Decode(t.Get("stats"), &s.Stats); err != nil {
		return nil, err
	}
	if err := luadata.Decode(t.Get("notMinionStat"), &s.NotMinionStat); err != nil {
		return nil, err
	}
	if s.QualityStats, err = statValues(t.Get("qualityStats")); err != nil {
		return nil, fmt.Errorf("qualityStats: %w", err)
	}
	if s.ConstantStats, err = statValues(t.Get("constantStats")); err != nil {
		return nil, fmt.Errorf("constantStats: %w", err)
	}
	if bm, ok := t.Get("baseMods").(*luadata.Table); ok {
		for _, m := range bm.List {
			mod, err := skillMod(m)
			if errors.Is(err, errBadFlags) {
				continue
			}
			if err != nil {
				return nil, fmt.Errorf("baseMods: %w", err)
			}
			s.BaseMods = append(s.BaseMods, mod)
		}
	}
	if sm, ok := t.Get("statMap").(*luadata.Table); ok {
		if s.StatMap, err = convertStatMaps(sm); err != nil {
			return nil, fmt.Errorf("statMap: %w", err)
		}
	}
	if s.Levels, err = skillLevels(t.Get("levels")); err != nil {
		return nil, fmt.Errorf("levels: %w", err)
	}
	// Data.lua processMod: a GlobalEffect tag in baseMods or the skill's own
	// stat map marks the effect (qualityMods and levelMods are unused in 3.x data).
	for _, m := range s.BaseMods {
		s.HasGlobalEffect = s.HasGlobalEffect || hasGlobalEffect(m)
	}
	for _, sm := range s.StatMap {
		s.HasGlobalEffect = s.HasGlobalEffect || statMapGlobal(sm)
	}
	return s, nil
}

func statMapGlobal(sm *gamedata.StatMap) bool {
	for _, it := range sm.Items {
		if it.Mod != nil && hasGlobalEffect(it.Mod) || it.Group != nil && statMapGlobal(it.Group) {
			return true
		}
	}
	return false
}

func hasGlobalEffect(m map[string]any) bool {
	tags, _ := m["@list"].([]any)
	for _, t := range tags {
		if tm, ok := t.(map[string]any); ok && tm["type"] == "GlobalEffect" {
			return true
		}
	}
	return false
}

// intKeys turns { [SkillType.Spell] = true, ... } into sorted type ids.
func intKeys(v luadata.Value) ([]int, error) {
	t, ok := v.(*luadata.Table)
	if !ok {
		return nil, nil
	}
	var out []int
	for _, k := range t.Keys {
		n, ok := k.(luadata.Number)
		if !ok {
			return nil, fmt.Errorf("key %v is not a number", k)
		}
		if t.Get(k) == luadata.Bool(true) {
			out = append(out, int(n))
		}
	}
	sort.Ints(out)
	return out, nil
}

func trueKeys(v luadata.Value) []string {
	t, ok := v.(*luadata.Table)
	if !ok {
		return nil
	}
	var out []string
	for _, k := range t.Keys {
		if t.Get(k) == luadata.Bool(true) {
			out = append(out, keyText(k))
		}
	}
	sort.Strings(out)
	return out
}

func statValues(v luadata.Value) ([]gamedata.StatValue, error) {
	t, ok := v.(*luadata.Table)
	if !ok {
		return nil, nil
	}
	var out []gamedata.StatValue
	for _, e := range t.List {
		et, ok := e.(*luadata.Table)
		if !ok || len(et.List) < 1 {
			return nil, fmt.Errorf("bad entry %v", e)
		}
		name, ok := et.List[0].(luadata.String)
		if !ok {
			return nil, fmt.Errorf("bad stat %v", et.List[0])
		}
		sv := gamedata.StatValue{Stat: string(name)}
		if len(et.List) > 1 {
			n, ok := et.List[1].(luadata.Number)
			if !ok {
				return nil, fmt.Errorf("%s: value %v", name, et.List[1])
			}
			sv.Value = float64(n)
		}
		out = append(out, sv)
	}
	return out, nil
}

func skillLevels(v luadata.Value) (map[int]*gamedata.SkillLevel, error) {
	t, ok := v.(*luadata.Table)
	if !ok {
		return nil, nil
	}
	out := map[int]*gamedata.SkillLevel{}
	add := func(n int, lv luadata.Value) error {
		lt, ok := lv.(*luadata.Table)
		if !ok {
			return fmt.Errorf("level %d is not a table", n)
		}
		l := &gamedata.SkillLevel{}
		for i, x := range lt.List {
			f, ok := x.(luadata.Number)
			if !ok {
				return fmt.Errorf("level %d value %d is %v", n, i+1, x)
			}
			l.Values = append(l.Values, float64(f))
		}
		if err := luadata.Decode(lt.Get("statInterpolation"), &l.StatInterpolation); err != nil {
			return fmt.Errorf("level %d: %w", n, err)
		}
		num := func(k string) float64 { f, _ := lt.Get(k).(luadata.Number); return float64(f) }
		l.LevelRequirement = int(num("levelRequirement"))
		l.ManaMultiplier = num("manaMultiplier")
		l.ManaReservationPercent = num("manaReservationPercent")
		l.ManaReservationFlat = num("manaReservationFlat")
		l.LifeReservationPercent = num("lifeReservationPercent")
		l.LifeReservationFlat = num("lifeReservationFlat")
		l.Cooldown = num("cooldown")
		l.CritChance, l.BaseMultiplier = num("critChance"), num("baseMultiplier")
		l.DamageEffectiveness, l.DamageMultiplier = num("damageEffectiveness"), num("damageMultiplier")
		l.AttackTime, l.AttackSpeedMultiplier = num("attackTime"), num("attackSpeedMultiplier")
		l.StoredUses = num("storedUses")
		if ct, ok := lt.Get("cost").(*luadata.Table); ok {
			for _, k := range ct.Keys {
				if f, ok := ct.Get(k).(luadata.Number); ok {
					if l.Cost == nil {
						l.Cost = map[string]float64{}
					}
					l.Cost[keyText(k)] = float64(f)
				}
			}
		}
		out[n] = l
		return nil
	}
	for i, lv := range t.List {
		if err := add(i+1, lv); err != nil {
			return nil, err
		}
	}
	for _, k := range t.Keys {
		n, ok := k.(luadata.Number)
		if !ok {
			return nil, fmt.Errorf("level key %v", k)
		}
		if err := add(int(n), t.Get(k)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func convertStatMaps(t *luadata.Table) (map[string]*gamedata.StatMap, error) {
	out := map[string]*gamedata.StatMap{}
	for _, k := range t.Keys {
		name := keyText(k)
		mt, ok := t.Get(k).(*luadata.Table)
		if !ok {
			return nil, fmt.Errorf("%s is not a table", name)
		}
		sm, err := statMap(mt)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = sm
	}
	return out, nil
}

// statMap converts one entry: positional mods (or groups of mods) and the
// value, mult, div, base and skillFlag fields mergeSkillInstanceMods reads.
func statMap(t *luadata.Table) (*gamedata.StatMap, error) {
	sm := &gamedata.StatMap{}
	for _, k := range t.Keys {
		ks := keyText(k)
		v := t.Get(k)
		if ks == "skillFlag" {
			s, _ := v.(luadata.String)
			sm.SkillFlag = string(s)
			continue
		}
		n, ok := v.(luadata.Number)
		if !ok {
			return nil, fmt.Errorf("field %s is %v", ks, v)
		}
		f := float64(n)
		switch ks {
		case "value":
			sm.Value = &f
		case "mult":
			sm.Mult = &f
		case "div":
			sm.Div = &f
		case "base":
			sm.Base = &f
		default:
			return nil, fmt.Errorf("unknown field %s", ks)
		}
	}
	for _, item := range t.List {
		if _, isCall := item.(*luadata.Call); isCall {
			m, err := skillMod(item)
			if errors.Is(err, errBadFlags) {
				continue
			}
			if err != nil {
				return nil, err
			}
			sm.Items = append(sm.Items, gamedata.StatMapItem{Mod: m})
			continue
		}
		gt, ok := item.(*luadata.Table)
		if !ok {
			return nil, fmt.Errorf("item %v", item)
		}
		g, err := statMap(gt)
		if err != nil {
			return nil, err
		}
		sm.Items = append(sm.Items, gamedata.StatMapItem{Group: g})
	}
	return sm, nil
}

// skillMod ports Data.lua makeSkillMod(name, type, value, flags,
// keywordFlags, ...tags), makeFlagMod(name, ...tags) and
// makeSkillDataMod(key, value, ...tags).
func skillMod(v luadata.Value) (map[string]any, error) {
	c, ok := v.(*luadata.Call)
	if !ok {
		return nil, fmt.Errorf("not a mod call: %v", v)
	}
	conv := &jsonConv{}
	args := make([]any, len(c.Args))
	for i, a := range c.Args {
		args[i] = conv.enc(a, "mod")
	}
	m, err := buildSkillMod(c.Func, args)
	if err != nil {
		return nil, err
	}
	if unresolved(m) || len(conv.hooks) > 0 {
		return nil, fmt.Errorf("%v: unresolved value %v", m["name"], m)
	}
	return m, nil
}

func buildSkillMod(fn string, args []any) (map[string]any, error) {
	for i, a := range args {
		args[i] = nestedMods(a)
	}
	arg := func(i int) any {
		if i < len(args) {
			return args[i]
		}
		return nil
	}
	var name, typ, value, flags, kw any
	var tags []any
	switch fn {
	case "mod":
		name, typ, value, flags, kw = arg(0), arg(1), arg(2), arg(3), arg(4)
		tags = rest(args, 5)
	case "flag":
		name, typ, value = arg(0), "FLAG", true
		tags = rest(args, 1)
	case "skill":
		name, typ, value = "SkillData", "LIST", map[string]any{"key": arg(0), "value": arg(1)}
		tags = rest(args, 2)
	default:
		return nil, fmt.Errorf("unknown mod builder %s", fn)
	}
	if flags == nil {
		flags = 0.0
	}
	if kw == nil {
		kw = 0.0
	}
	_, okFlags := flags.(float64)
	_, okKw := kw.(float64)
	if !okFlags || !okKw {
		// A tag passed where makeSkillMod expects flags (a data error in
		// PoB, e.g. SupportBloodsoakedBanner); PoB cannot sum such a mod.
		skippedMods++
		return nil, errBadFlags
	}
	m := map[string]any{"name": name, "type": typ, "value": value, "flags": flags, "keywordFlags": kw}
	var list []any
	for _, t := range tags {
		if t != nil {
			list = append(list, t)
		}
	}
	if len(list) > 0 {
		m["@list"] = list
	}
	return m, nil
}

// skippedMods counts mods with a non-number flags argument.
var skippedMods int

var errBadFlags = errors.New("mod flags are not a number")

func rest(l []any, from int) []any {
	if from >= len(l) {
		return nil
	}
	return l[from:]
}

// nestedMods builds the mods of mod(...) calls inside values, such as
// mod("MinionModifier", "LIST", { mod = mod(...) }).
func nestedMods(v any) any {
	switch x := v.(type) {
	case map[string]any:
		if fn, ok := x["$call"].(string); ok && (fn == "mod" || fn == "flag" || fn == "skill") {
			args, _ := x["args"].([]any)
			if m, err := buildSkillMod(fn, args); err == nil || errors.Is(err, errBadFlags) {
				return m
			}
			return x
		}
		for k, e := range x {
			x[k] = nestedMods(e)
		}
	case []any:
		for i, e := range x {
			x[i] = nestedMods(e)
		}
	}
	return v
}
