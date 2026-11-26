package engine

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/Mi7teR/exile-calc/gamedata"
	"github.com/Mi7teR/exile-calc/internal/luadata"
	"github.com/Mi7teR/exile-calc/pobcode"
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

// configPlaceholder is the option's placeholder: the saved one, else its
// defaultPlaceholderState, unless enemyIsBoss's apply sets it.
func configPlaceholder(o gamedata.ConfigOption, set *pobcode.ConfigSet, e *enemyEnv) any {
	if e != nil {
		if v, ok := bossPlaceholder(o.Var, e); ok {
			return v
		}
	}
	placeholder := o.DefaultPlaceholderState
	if set != nil {
		for _, p := range set.Placeholders {
			if p.Name == o.Var {
				placeholder = inputValue(p)
			}
		}
	}
	return placeholder
}

// bossPlaceholder returns the placeholder enemyIsBoss's apply sets for an
// enemy resistance option (ConfigOptions.lua SetPlaceholder), replacing the
// saved one: nil when it clears it. ok is false for other options.
func bossPlaceholder(name string, e *enemyEnv) (any, bool) {
	switch name {
	case "enemyLightningResist", "enemyColdResist", "enemyFireResist", "enemyChaosResist",
		"enemyArmour", "enemyEvasion":
	default:
		return nil, false
	}
	if v, ok := e.holders[name]; ok {
		return v, true
	}
	return nil, true
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
	if set != nil {
		if in, ok := set.Input(o.Var); ok {
			input = inputValue(in)
		}
	}
	placeholder = configPlaceholder(o, set, e)
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
		if o.MapAffix {
			c.mapAffix(db, res, o, set, player, enemy, env)
			continue
		}
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

// mapAffix is mapAffixDropDownFunction (ConfigOptions.lua:116-131) for
// a MapPrefix/MapSuffix option: the chosen affix's apply from
// Data/ModMap.lua, with the map tier (4 - the tier list's index, Red
// first) and 1 + multiplierMapModEffect / 100.
func (c *Calculator) mapAffix(
	db *ModDB, res *Result, o gamedata.ConfigOption, set *pobcode.ConfigSet,
	player, enemy *modSink, env map[string]luadata.Value,
) {
	val, ok := configValue(o, set, db.enemyEnv)
	name, isStr := val.(luadata.String)
	if !ok || !isStr || name == "NONE" {
		return
	}
	affix := c.data.MapMods[string(name)]
	if affix == nil || affix.Apply == "" {
		return
	}
	f := c.mapAffixes.get(string(name), affix.Apply)
	if f == nil {
		res.Incomplete = append(res.Incomplete, "map mod "+string(name)+": apply not supported")
		return
	}
	effect := luadata.Number(1 + configNum(db, "multiplierMapModEffect")/100)
	selIndex := 1 // varControls.multiplierMapModTier.selIndex
	if set != nil {
		if in, has := set.Input("multiplierMapModTier"); has && in.Kind == pobcode.KindString {
			for i, v := range []string{"HIGH", "MED", "LOW"} {
				if in.String == v {
					selIndex = i + 1
				}
			}
		}
	}
	tier := luadata.Number(4 - selIndex) //nolint:mnd // ConfigOptions.lua:124
	values := toLuaTable(affix.Values)
	pm, em := player.table(), enemy.table()
	var args []luadata.Value
	switch affix.Type {
	case "check":
		args = []luadata.Value{name, effect, pm, em}
	case "list":
		args = []luadata.Value{tier, effect, values, pm, em}
	case "count":
		roll := luadata.Number(100) //nolint:mnd // the top of the roll range, ConfigOptions.lua:126
		args = []luadata.Value{tier, roll, effect, values, pm, em}
	default:
		return
	}
	if _, err := f.Call(args, env); err != nil {
		res.Incomplete = append(res.Incomplete, fmt.Sprintf("map mod %s: %v", name, err))
		return
	}
	player.commit()
	enemy.commit()
}

// toLuaTable is plain JSON data (numbers, strings, nested lists) as a
// Lua value.
func toLuaTable(v any) luadata.Value {
	switch x := v.(type) {
	case []any:
		t := &luadata.Table{}
		for _, e := range x {
			t.List = append(t.List, toLuaTable(e))
		}
		return t
	case float64:
		return luadata.Number(x)
	case string:
		return luadata.String(x)
	case bool:
		return luadata.Bool(x)
	}
	return nil
}

// mapAffixFuncs caches the parsed apply functions of Data/ModMap.lua.
type mapAffixFuncs struct {
	mu    sync.Mutex
	funcs map[string]*luadata.Func
}

func (m *mapAffixFuncs) get(name, src string) *luadata.Func {
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.funcs[name]; ok {
		return f
	}
	if m.funcs == nil {
		m.funcs = map[string]*luadata.Func{}
	}
	var f *luadata.Func
	if v, err := luadata.Eval("return { f = "+src+" }", nil); err == nil {
		if t, isTable := v.(*luadata.Table); isTable {
			f, _ = t.Get("f").(*luadata.Func)
		}
	}
	m.funcs[name] = f
	return f
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
