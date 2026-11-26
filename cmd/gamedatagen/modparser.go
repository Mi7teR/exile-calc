package main

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/Mi7teR/exile-calc/internal/luadata"
)

// Tables of Modules/ModParser.lua exported as data. Their Lua functions are
// exported as named hooks ({"$hook": "modTagList[per (%d+) strength]"}) that
// modparser implements in Go one by one.
// templateCount is reported by the generator log.
var templateCount int

var parserTables = []string{
	"formList", "modNameList", "modFlagList", "preFlagList", "modTagList", "specialModList",
	"unsupportedModList", "suffixTypes", "penTypes", "dmgTypes", "resourceTypes", "flagTypes",
	"skillNameList", "conquerorList",
}

// parserEnv is what the extracted tables see: Global.lua plus the helpers
// ModParser.lua uses to build data (bor, mod = modLib.createMod, flag).
func parserEnv(globalSrc string) (map[string]luadata.Value, error) {
	env, err := luadata.EvalGlobals(globalSrc, nil)
	if err != nil {
		return nil, fmt.Errorf("Global.lua: %w", err)
	}
	bor := luadata.Builtin(func(args []luadata.Value) (luadata.Value, error) {
		out := uint64(0)
		for _, a := range args {
			n, ok := a.(luadata.Number)
			if !ok {
				return nil, luadata.ErrKeepCall
			}
			out |= uint64(n)
		}
		return luadata.Number(out), nil
	})
	bit := &luadata.Table{}
	bit.Set("bor", bor)
	env["bor"], env["bit"] = bor, bit
	env["mod"] = luadata.Builtin(func(args []luadata.Value) (luadata.Value, error) {
		return createMod(args)
	})
	env["flag"] = luadata.Builtin(func(args []luadata.Value) (luadata.Value, error) {
		if len(args) == 0 {
			return nil, errors.New("flag needs a name")
		}
		return createMod(append([]luadata.Value{args[0], luadata.String("FLAG"), luadata.Bool(true)}, args[1:]...))
	})
	return env, nil
}

// createMod ports modLib.createMod (Modules/ModTools.lua):
// mod(name, type, value, [source string], [flags number], [keywordFlags number], tags...).
func createMod(args []luadata.Value) (luadata.Value, error) {
	if len(args) < 3 {
		args = append(args, make([]luadata.Value, 3-len(args))...)
	}
	rest := args[3:]
	m := &luadata.Table{}
	m.Set("name", args[0])
	m.Set("type", args[1])
	m.Set("value", args[2])
	flags, kw := luadata.Value(luadata.Number(0)), luadata.Value(luadata.Number(0))
	tagStart := 0
	if len(rest) >= 1 {
		if s, ok := rest[0].(luadata.String); ok {
			m.Set("source", s)
			tagStart = 1
		}
	}
	if len(rest) >= 2 && isNumberish(rest[1]) {
		flags, tagStart = rest[1], 2
	}
	if len(rest) >= 3 && isNumberish(rest[2]) {
		kw, tagStart = rest[2], 3
	}
	m.Set("flags", flags)
	m.Set("keywordFlags", kw)
	for _, tag := range rest[tagStart:] {
		if tag != nil {
			m.List = append(m.List, tag)
		}
	}
	return m, nil
}

// isNumberish accepts numbers and flag expressions still waiting on a Ref.
func isNumberish(v luadata.Value) bool {
	switch x := v.(type) {
	case luadata.Number:
		return true
	case luadata.Ref:
		return strings.Contains(string(x), "Flag.")
	case *luadata.Call:
		return x.Func == "bor" || x.Func == "bit.bor"
	}
	return false
}

// firstToUpper mirrors ModParser.lua's keystone flag naming:
// firstToUpper(name):gsub(" %l", string.upper):gsub(" ", "").
func keystoneFlag(name string) string {
	r := []rune(name)
	if len(r) > 0 {
		r[0] = unicode.ToUpper(r[0])
	}
	for i := 1; i < len(r); i++ {
		if r[i-1] == ' ' && unicode.IsLower(r[i]) {
			r[i] = unicode.ToUpper(r[i])
		}
	}
	return "Condition:Have" + strings.ReplaceAll(string(r), " ", "")
}

// clusterJewels is the part of Data/ClusterJewels.lua ModParser.lua reads.
type clusterJewels struct {
	Jewels map[string]struct {
		Skills map[string]struct {
			Enchant []string `lua:"enchant"`
		} `lua:"skills"`
	} `lua:"jewels"`
	NotableSortOrder map[string]float64 `lua:"notableSortOrder"`
	Keystones        []string           `lua:"keystones"`
}

func str(s string) luadata.Value { return luadata.String(s) }

func listMod(args ...luadata.Value) *luadata.Table {
	m, _ := createMod(args)
	return &luadata.Table{List: []luadata.Value{m}}
}

// ailmentDefaults are Data.lua's data.nonDamagingAilment[...].default, which
// ModParser.lua reads in a few mod tables; they are substituted into the
// source so the rest of data stays unresolved.
var ailmentDefaults = map[string]string{"Chill": "10", "Shock": "15", "Scorch": "10", "Brittle": "2", "Sap": "6"}

func convertParserTables(modParserSrc, globalSrc, clusterSrc string, keystones []string) (map[string]any, []string, error) {
	for name, v := range ailmentDefaults {
		modParserSrc = strings.ReplaceAll(modParserSrc, `data.nonDamagingAilment["`+name+`"].default`, v)
	}
	env, err := parserEnv(globalSrc)
	if err != nil {
		return nil, nil, err
	}
	tables := map[string]*luadata.Table{}
	for _, name := range parserTables {
		v, err := luadata.ExtractLocal(modParserSrc, name, env)
		if err != nil {
			return nil, nil, fmt.Errorf("ModParser.lua %s: %w", name, err)
		}
		t, ok := v.(*luadata.Table)
		if !ok {
			return nil, nil, fmt.Errorf("ModParser.lua %s: not a table", name)
		}
		tables[name] = t
	}

	// ModParser.lua: the keystones of data.keystones become special mods,
	// then every special pattern is anchored ("^"..k.."$").
	special := tables["specialModList"]
	for _, name := range keystones {
		ks, _ := createMod([]luadata.Value{luadata.String("Keystone"), luadata.String("LIST"), luadata.String(name)})
		fl, _ := createMod([]luadata.Value{luadata.String(keystoneFlag(name)), luadata.String("FLAG"), luadata.Bool(true)})
		special.Set(strings.ToLower(name), &luadata.Table{List: []luadata.Value{ks, fl}})
	}
	var cj clusterJewels
	if err := evalInto("Data/ClusterJewels.lua", clusterSrc, &cj); err != nil {
		return nil, nil, err
	}
	for _, name := range cj.Keystones {
		special.Set(strings.ToLower(name), listMod(str("Keystone"), str("LIST"), str(name)))
	}

	// ModParser.lua: clusterJewelSkills, looked up by the whole lower-cased line.
	cjs := &luadata.Table{}
	for _, base := range sortedMapKeys(cj.Jewels) {
		jewel := cj.Jewels[base]
		for _, id := range sortedMapKeys(jewel.Skills) {
			value := &luadata.Table{}
			value.Set("key", str("clusterJewelSkill"))
			value.Set("value", str(id))
			cjs.Set(strings.ToLower(strings.Join(jewel.Skills[id].Enchant, " ")), listMod(str("JewelData"), str("LIST"), value))
		}
	}
	for _, notable := range sortedMapKeys(cj.NotableSortOrder) {
		cjs.Set("1 added passive skill is "+strings.ToLower(notable), listMod(str("ClusterJewelNotable"), str("LIST"), str(notable)))
	}
	for _, ks := range cj.Keystones {
		value := &luadata.Table{}
		value.Set("key", str("clusterJewelKeystone"))
		value.Set("value", str(ks))
		cjs.Set("adds "+strings.ToLower(ks), listMod(str("JewelData"), str("LIST"), value))
	}
	tables["clusterJewelSkills"] = cjs
	anchored := &luadata.Table{}
	for _, k := range special.Keys {
		s, ok := k.(luadata.String)
		if !ok {
			return nil, nil, fmt.Errorf("specialModList: non-string key %v", k)
		}
		anchored.Set("^"+string(s)+"$", special.Get(k))
	}
	tables["specialModList"] = anchored

	// ModParser.lua: resourceTypes gains "maximum ..." keys; regen/degen/cost
	// tables are appendMod(resourceTypes, suffix).
	res := tables["resourceTypes"]
	for _, k := range append([]luadata.Value(nil), res.Keys...) {
		res.Set("maximum "+keyText(k), res.Get(k))
	}
	for name, suffix := range map[string]string{
		"regenTypes": "Regen", "degenTypes": "Degen", "costTypes": "Cost", "baseCostTypes": "CostNoMult",
	} {
		t := &luadata.Table{}
		for _, k := range res.Keys {
			switch v := res.Get(k).(type) {
			case luadata.String:
				t.Set(k, luadata.String(string(v)+suffix))
			case *luadata.Table:
				l := &luadata.Table{}
				for _, item := range v.List {
					l.List = append(l.List, luadata.String(fmt.Sprint(item)+suffix))
				}
				t.Set(k, l)
			}
		}
		tables[name] = t
	}

	if st, ok := env["SkillType"].(*luadata.Table); ok {
		tables["SkillType"] = st // for SkillType.Banner and friends
	}

	out := map[string]any{}
	conv := &jsonConv{env: env}
	for name, t := range tables {
		out[name] = conv.enc(t, name)
	}
	sort.Strings(conv.hooks)
	templateCount = conv.templates
	return out, conv.hooks, nil
}

func keyText(k luadata.Value) string {
	switch x := k.(type) {
	case luadata.String:
		return string(x)
	case luadata.Number:
		return strconv.FormatFloat(float64(x), 'f', -1, 64)
	case luadata.Bool:
		return strconv.FormatBool(bool(x))
	case luadata.Ref:
		return "$ref:" + string(x)
	}
	return fmt.Sprint(k)
}

// jsonConv encodes Lua values for the generated data:
//   - a table with only positional entries (or keys 1..n) is an array;
//   - any other table is an object, its positional entries under "@list";
//   - a function that only builds data from its arguments becomes
//     {"$template": value} with {"$arg": n} where argument n goes;
//   - any other function is {"$hook": "<table>[<key>]"}, ported by hand;
//   - an unresolved call is {"$call": name, "args": [...]}, an unresolved
//     name {"$ref": name}.
type jsonConv struct {
	env       map[string]luadata.Value
	hooks     []string
	templates int
}

func (c *jsonConv) enc(v luadata.Value, path string) any {
	switch x := v.(type) {
	case nil:
		return nil
	case luadata.Bool:
		return bool(x)
	case luadata.Number:
		return float64(x)
	case luadata.String:
		return string(x)
	case luadata.Ref:
		if n, ok := strings.CutPrefix(string(x), "$arg:"); ok {
			i, _ := strconv.Atoi(n)
			return map[string]any{"$arg": float64(i)}
		}
		return map[string]any{"$ref": string(x)}
	case *luadata.Func:
		if t, ok := c.template(x, path); ok {
			c.templates++
			return map[string]any{"$template": t}
		}
		c.hooks = append(c.hooks, path)
		return map[string]any{"$hook": path}
	case *luadata.Call:
		args := make([]any, len(x.Args))
		for i, a := range x.Args {
			args[i] = c.enc(a, fmt.Sprintf("%s(%d)", path, i+1))
		}
		return map[string]any{"$call": x.Func, "args": args}
	case *luadata.Table:
		list := append([]luadata.Value(nil), x.List...)
		used := map[luadata.Value]bool{}
		for n := len(list) + 1; x.Has(luadata.Number(n)); n++ {
			list = append(list, x.Get(luadata.Number(n)))
			used[luadata.Number(n)] = true
		}
		items := make([]any, len(list))
		for i, item := range list {
			items[i] = c.enc(item, fmt.Sprintf("%s[%d]", path, i+1))
		}
		if len(used) == len(x.Keys) {
			return items
		}
		obj := map[string]any{}
		for _, k := range x.Keys {
			if !used[k] {
				ks := keyText(k)
				obj[ks] = c.enc(x.Get(k), path+"["+ks+"]")
			}
		}
		if len(items) > 0 {
			obj["@list"] = items
		}
		return obj
	}
	return fmt.Sprint(v)
}

// template runs a function with placeholder arguments. It succeeds when the
// result is plain data in which the arguments are only passed through.
func (c *jsonConv) template(f *luadata.Func, path string) (any, bool) {
	args := make([]luadata.Value, len(f.Params()))
	for i := range args {
		args[i] = luadata.Ref(fmt.Sprintf("$arg:%d", i+1))
	}
	v, err := f.Call(args, c.env)
	if err != nil {
		return nil, false
	}
	sub := &jsonConv{env: c.env}
	out := sub.enc(v, path)
	if len(sub.hooks) > 0 || unresolved(out) {
		return nil, false
	}
	c.templates += sub.templates
	return out, true
}

// unresolved reports data that still needs code: names and calls PoB
// resolves at run time.
func unresolved(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		if _, ok := x["$ref"]; ok {
			return true
		}
		if _, ok := x["$call"]; ok {
			return true
		}
		for _, e := range x {
			if unresolved(e) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if unresolved(e) {
				return true
			}
		}
	}
	return false
}

// convertModCache exports Data/ModCache.lua, PoB's cache of parsed mod
// lines, as the reference for modparser's tests: line -> [mods, extra].
func convertModCache(src string) (map[string]any, error) {
	v, err := luadata.Eval(src, nil)
	if err != nil {
		return nil, fmt.Errorf("ModCache.lua: %w", err)
	}
	t, ok := v.(*luadata.Table)
	if !ok {
		return nil, errors.New("ModCache.lua: not a table")
	}
	out := make(map[string]any, len(t.Keys))
	conv := &jsonConv{}
	for _, k := range t.Keys {
		out[keyText(k)] = conv.enc(t.Get(k), "ModCache")
	}
	if len(conv.hooks) > 0 {
		return nil, fmt.Errorf("ModCache.lua: unexpected functions at %v", conv.hooks[:1])
	}
	return out, nil
}

func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
