package engine

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/internal/luadata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// configApplies caches the parsed apply functions of ConfigOptions.lua.
type configApplies struct {
	once  sync.Once
	funcs map[string]*luadata.Func // by var; missing when it did not parse
}

func (c *configApplies) get(opts []gamedata.ConfigOption) map[string]*luadata.Func {
	c.once.Do(func() {
		c.funcs = map[string]*luadata.Func{}
		for _, o := range opts {
			if o.Apply == "" {
				continue
			}
			v, err := luadata.Eval("return { f = "+o.Apply+" }", nil)
			if err != nil {
				continue
			}
			if t, ok := v.(*luadata.Table); ok {
				if f, ok := t.Get("f").(*luadata.Func); ok {
					c.funcs[o.Var] = f
				}
			}
		}
	})
	return c.funcs
}

// bossResistPlaceholders are the options whose placeholder enemyIsBoss's
// apply sets (ConfigOptions.lua SetPlaceholder) and whose apply adds enemy
// mods.
var bossResistPlaceholders = map[string]bool{
	"enemyLightningResist": true, "enemyColdResist": true, "enemyFireResist": true, "enemyChaosResist": true,
}

// configValue is the value ConfigTab:BuildModList passes to apply, and
// whether apply runs: the saved input, else the option's default state;
// count-like options fall back to the placeholder and skip 0.
//
// enemyIsBoss's apply runs before the enemy resistance options and sets
// their placeholders (Boss 40/25, Pinnacle and Uber 50/30, None clears
// them), replacing the saved ones; e, when not nil, holds those values.
func configValue(o gamedata.ConfigOption, set *pobcode.ConfigSet, e *enemyEnv) (luadata.Value, bool) {
	var input, placeholder any
	input = o.DefaultState
	if o.DefaultValue != nil {
		input = o.DefaultValue
	}
	placeholder = o.DefaultPlaceholderState
	if set != nil {
		if in, ok := set.Input(o.Var); ok {
			input = inputValue(in)
		}
		for _, p := range set.Placeholders {
			if p.Name == o.Var {
				placeholder = inputValue(p)
			}
		}
	}
	if e != nil && bossResistPlaceholders[o.Var] {
		placeholder = nil
		if v, ok := e.holders[o.Var]; ok {
			placeholder = v
		}
	}
	switch o.Type {
	case "check":
		b, _ := input.(bool)
		return luadata.Bool(true), b
	case "count", "integer", "countAllowZero", "float":
		for _, v := range []any{input, placeholder} {
			if n, ok := v.(float64); ok && (n != 0 || o.Type == "countAllowZero") {
				return luadata.Number(n), true
			}
		}
		return nil, false
	case "list", "text":
		if input == nil {
			return nil, false
		}
		return toLua(input), true
	}
	return nil, false
}

func inputValue(in pobcode.Input) any {
	switch in.Kind {
	case pobcode.KindBool:
		return in.Bool
	case pobcode.KindNumber:
		return in.Number
	case pobcode.KindString:
		return in.String
	}
	return nil
}

func toLua(v any) luadata.Value {
	switch x := v.(type) {
	case bool:
		return luadata.Bool(x)
	case float64:
		return luadata.Number(x)
	case string:
		return luadata.String(x)
	}
	return nil
}

// applyConfig ports the option loop of ConfigTabClass:BuildModList by
// running each option's Lua apply function. Options whose apply uses what
// the engine does not provide (build, data tables, loops) are reported in
// Result.Incomplete when they are set.
func (c *Calculator) applyConfig(db *ModDB, res *Result, set *pobcode.ConfigSet) {
	funcs := c.config.get(c.data.Config)
	player, enemy := &modSink{db: db}, &modSink{db: db.enemy}
	env := configEnv(c.parser.Table("SkillType"))
	for _, o := range c.data.Config {
		if o.Apply == "" {
			continue
		}
		val, ok := configValue(o, set, db.enemyEnv)
		if !ok {
			continue
		}
		if o.Var == "enemyIsBoss" {
			enemyIsBoss(db, val)
			continue
		}
		f := funcs[o.Var]
		if f == nil {
			res.Incomplete = append(res.Incomplete, "config "+o.Var+": apply not supported")
			continue
		}
		pm, em := player.table(), enemy.table()
		if _, err := f.Call([]luadata.Value{val, pm, em, luadata.Ref("build")}, env); err != nil {
			res.Incomplete = append(res.Incomplete, fmt.Sprintf("config %s: %v", o.Var, err))
			continue
		}
		player.commit()
		enemy.commit()
	}
}

// enemyIsBoss ports the mods of ConfigOptions.lua's enemyIsBoss apply.
// Its other work sets placeholders on the config tab's enemy fields
// (damage, resistances, armour), which only enemy stats read.
func enemyIsBoss(db *ModDB, val luadata.Value) {
	v, _ := val.(luadata.String)
	effective := map[string]any{"type": "Condition", "var": "Effective"}
	enemyFlag := func(name string) {
		db.enemy.Add(Mod{Name: name, Type: "FLAG", Value: true, Source: "Config", Tags: []map[string]any{effective}})
	}
	switch string(v) {
	case "Boss":
		enemyFlag("Condition:RareOrUnique")
		db.enemy.NewMod("AilmentThreshold", "MORE", 488, "Boss")
	case "Pinnacle":
		enemyFlag("Condition:RareOrUnique")
		enemyFlag("Condition:PinnacleBoss")
		db.enemy.NewMod("AilmentThreshold", "MORE", 404, "Boss")
	case "Uber":
		enemyFlag("Condition:RareOrUnique")
		enemyFlag("Condition:PinnacleBoss")
		db.enemy.NewMod("DamageTaken", "MORE", -70, "Boss")
		db.enemy.NewMod("AilmentThreshold", "MORE", 404, "Boss")
	default:
		return
	}
	db.NewMod("WarcryPower", "BASE", 20, "Boss")
	db.NewMod("Multiplier:EnemyPower", "BASE", 20, "Boss")
}

// modSink collects one apply call's NewMod calls, committed only if the
// whole function ran.
type modSink struct {
	db      *ModDB
	pending []Mod
}

func (s *modSink) table() *luadata.Table {
	s.pending = nil
	t := &luadata.Table{}
	t.Set("NewMod", luadata.Builtin(func(args []luadata.Value) (luadata.Value, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("NewMod without self")
		}
		m, err := createMod(args[1:])
		if err != nil {
			return nil, err
		}
		s.pending = append(s.pending, m)
		return nil, nil
	}))
	return t
}

func (s *modSink) commit() {
	for _, m := range s.pending {
		s.db.Add(m)
	}
	s.pending = nil
}

func configEnv(skillTypes map[string]any) map[string]luadata.Value {
	num := func(args []luadata.Value, i int) (float64, error) {
		if i >= len(args) {
			return 0, fmt.Errorf("missing argument")
		}
		n, ok := args[i].(luadata.Number)
		if !ok {
			return 0, fmt.Errorf("want number, have %T", args[i])
		}
		return float64(n), nil
	}
	fold := func(pick func(a, b float64) float64) luadata.Builtin {
		return func(args []luadata.Value) (luadata.Value, error) {
			r, err := num(args, 0)
			if err != nil {
				return nil, err
			}
			for i := 1; i < len(args); i++ {
				n, err := num(args, i)
				if err != nil {
					return nil, err
				}
				r = pick(r, n)
			}
			return luadata.Number(r), nil
		}
	}
	unary := func(f func(float64) float64) luadata.Builtin {
		return func(args []luadata.Value) (luadata.Value, error) {
			n, err := num(args, 0)
			if err != nil {
				return nil, err
			}
			return luadata.Number(f(n)), nil
		}
	}
	mathT := &luadata.Table{}
	mathT.Set("min", fold(math.Min))
	mathT.Set("max", fold(math.Max))
	mathT.Set("floor", unary(math.Floor))
	mathT.Set("ceil", unary(math.Ceil))
	modLib := &luadata.Table{}
	modLib.Set("createMod", luadata.Builtin(func(args []luadata.Value) (luadata.Value, error) {
		m, err := createMod(args)
		if err != nil {
			return nil, err
		}
		return modTable(m), nil
	}))
	env := map[string]luadata.Value{
		"m_min": fold(math.Min), "m_max": fold(math.Max), "m_floor": unary(math.Floor), "m_ceil": unary(math.Ceil),
		"math": mathT, "modLib": modLib,
		"type": luadata.Builtin(func(args []luadata.Value) (luadata.Value, error) {
			if len(args) == 0 {
				return nil, fmt.Errorf("type needs an argument")
			}
			switch args[0].(type) {
			case nil:
				return luadata.String("nil"), nil
			case luadata.Bool:
				return luadata.String("boolean"), nil
			case luadata.Number:
				return luadata.String("number"), nil
			case luadata.String:
				return luadata.String("string"), nil
			case *luadata.Table:
				return luadata.String("table"), nil
			}
			return nil, fmt.Errorf("type of %T", args[0])
		}),
	}
	if skillTypes != nil {
		env["SkillType"] = fromGo(skillTypes)
	}
	return env
}

// createMod ports modLib.createMod(name, type, value, [source], [flags],
// [keywordFlags], tags...).
func createMod(args []luadata.Value) (Mod, error) {
	if len(args) < 3 {
		return Mod{}, fmt.Errorf("createMod needs name, type and value")
	}
	name, ok1 := args[0].(luadata.String)
	typ, ok2 := args[1].(luadata.String)
	if !ok1 || !ok2 {
		return Mod{}, fmt.Errorf("createMod: bad name or type")
	}
	val, err := toGo(args[2])
	if err != nil {
		return Mod{}, err
	}
	m := Mod{Name: string(name), Type: string(typ), Value: val}
	rest := args[3:]
	if len(rest) >= 1 {
		if s, ok := rest[0].(luadata.String); ok {
			m.Source = string(s)
			rest = rest[1:]
		}
	}
	if len(rest) >= 1 {
		if n, ok := rest[0].(luadata.Number); ok {
			m.Flags = uint64(n)
			rest = rest[1:]
			if len(rest) >= 1 {
				if n, ok := rest[0].(luadata.Number); ok {
					m.KeywordFlags = uint64(n)
					rest = rest[1:]
				}
			}
		}
	}
	for _, t := range rest {
		if t == nil {
			continue
		}
		g, err := toGo(t)
		if err != nil {
			return Mod{}, err
		}
		tag, ok := g.(map[string]any)
		if !ok {
			return Mod{}, fmt.Errorf("createMod: tag is %T", g)
		}
		m.Tags = append(m.Tags, tag)
	}
	return m, nil
}

// modTable encodes a mod the way modparser does inside LIST values.
func modTable(m Mod) *luadata.Table {
	t := &luadata.Table{}
	t.Set("name", luadata.String(m.Name))
	t.Set("type", luadata.String(m.Type))
	t.Set("value", fromGo(m.Value))
	t.Set("flags", luadata.Number(m.Flags))
	t.Set("keywordFlags", luadata.Number(m.KeywordFlags))
	for _, tag := range m.Tags {
		t.List = append(t.List, fromGo(tag))
	}
	return t
}

func fromGo(v any) luadata.Value {
	switch x := v.(type) {
	case map[string]any:
		t := &luadata.Table{}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if k == "@list" {
				for _, e := range x[k].([]any) {
					t.List = append(t.List, fromGo(e))
				}
				continue
			}
			t.Set(luadata.String(k), fromGo(x[k]))
		}
		return t
	case []any:
		t := &luadata.Table{}
		for _, e := range x {
			t.List = append(t.List, fromGo(e))
		}
		return t
	}
	return toLua(v)
}

// toGo converts plain Lua data into modparser's encoding: tables become
// map[string]any (positional entries under "@list") or []any.
func toGo(v luadata.Value) (any, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case luadata.Bool:
		return bool(x), nil
	case luadata.Number:
		return float64(x), nil
	case luadata.String:
		return string(x), nil
	case *luadata.Table:
		var list []any
		for _, e := range x.List {
			g, err := toGo(e)
			if err != nil {
				return nil, err
			}
			list = append(list, g)
		}
		if len(x.Keys) == 0 {
			if list == nil {
				return map[string]any{}, nil
			}
			return list, nil
		}
		m := map[string]any{}
		for _, k := range x.Keys {
			ks, ok := k.(luadata.String)
			if !ok {
				return nil, fmt.Errorf("table key %v is not a string", k)
			}
			g, err := toGo(x.Get(k))
			if err != nil {
				return nil, err
			}
			m[string(ks)] = g
		}
		if list != nil {
			m["@list"] = list
		}
		return m, nil
	}
	return nil, fmt.Errorf("value %s is not plain data", strings.TrimSpace(fmt.Sprintf("%T", v)))
}
