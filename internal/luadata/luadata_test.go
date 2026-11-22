package luadata

import (
	"reflect"
	"strings"
	"testing"
)

func eval(t *testing.T, src string) Value {
	t.Helper()
	v, err := Eval(src, nil)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	return v
}

func TestLiterals(t *testing.T) {
	cases := map[string]Value{
		`return 42`:                Number(42),
		`return -1.5`:              Number(-1.5),
		`return 0x1F`:              Number(31),
		`return 1e3`:               Number(1000),
		`return .5`:                Number(0.5),
		`return true`:              Bool(true),
		`return nil`:               nil,
		`return "a\"b\n\\"`:        String("a\"b\n\\"),
		`return 'it\'s'`:           String("it's"),
		`return "\65\066"`:         String("AB"),
		"return [[line1\nline2]]":  String("line1\nline2"),
		"return [[\nskip first]]":  String("skip first"),
		"return [==[a]]b]==]":      String("a]]b"),
		`return "a" .. "b" .. 'c'`: String("abc"),
		`return 1 / 4 + 2 * 3 - 1`: Number(5.25),
		`return -(2 + 1)`:          Number(-3),
		"-- comment\nreturn 1 --[[ block\n comment ]]": Number(1),
	}
	for src, want := range cases {
		if got := eval(t, src); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: got %#v, want %#v", src, got, want)
		}
	}
}

func TestTables(t *testing.T) {
	got := eval(t, `return {
		affix = "of the Brute", "+(8-12) to Strength",
		statOrder = { 848 }, ["Spaced Key"] = 1, [4080418644] = { "x" },
		weightVal = { 1000, 0, }; empty = { },
	}`).(*Table)

	if s, _ := got.Get("affix").(String); s != "of the Brute" {
		t.Errorf("affix: %#v", got.Get("affix"))
	}
	if !reflect.DeepEqual(got.List, []Value{String("+(8-12) to Strength")}) {
		t.Errorf("list part: %#v", got.List)
	}
	if so := got.Get("statOrder").(*Table); !reflect.DeepEqual(so.List, []Value{Number(848)}) {
		t.Errorf("statOrder: %#v", so)
	}
	if got.Get("Spaced Key") != Number(1) {
		t.Errorf("string key: %#v", got.Get("Spaced Key"))
	}
	if th, ok := got.Get(Number(4080418644)).(*Table); !ok || th.List[0] != String("x") {
		t.Errorf("number key: %#v", got.Get(Number(4080418644)))
	}
	if wv := got.Get("weightVal").(*Table); len(wv.List) != 2 {
		t.Errorf("trailing separator: %#v", wv)
	}
	if e := got.Get("empty").(*Table); len(e.List) != 0 || len(e.Keys) != 0 {
		t.Errorf("empty: %#v", e)
	}
	// Key order is preserved for deterministic output.
	wantKeys := []Value{String("affix"), String("statOrder"), String("Spaced Key"), Number(4080418644), String("weightVal"), String("empty")}
	if !reflect.DeepEqual(got.Keys, wantKeys) {
		t.Errorf("key order: %#v", got.Keys)
	}
}

func TestRefsCallsAndFunctions(t *testing.T) {
	got := eval(t, `return {
		types = { [SkillType.Spell] = true },
		flags = bit.bor(KeywordFlag.Hit, KeywordFlag.Ailment),
		statMap = { mod("Damage", "MORE", nil, 0, 0, { type = "PerStat", stat = "ChainRemaining" }) },
		preDamageFunc = function(activeSkill, output)
			if activeSkill.x then output.y = 1 end
			for i = 1, 3 do local z = function() return i end end
		end,
		after = 1,
	}`).(*Table)

	types := got.Get("types").(*Table)
	if len(types.Keys) != 1 || !reflect.DeepEqual(types.Keys[0], Ref("SkillType.Spell")) {
		t.Errorf("ref key: %#v", types.Keys)
	}
	flags := got.Get("flags").(*Call)
	if flags.Func != "bit.bor" || !reflect.DeepEqual(flags.Args, []Value{Ref("KeywordFlag.Hit"), Ref("KeywordFlag.Ailment")}) {
		t.Errorf("call: %#v", flags)
	}
	m := got.Get("statMap").(*Table).List[0].(*Call)
	if m.Func != "mod" || len(m.Args) != 6 || m.Args[2] != nil || m.Args[0] != String("Damage") {
		t.Errorf("mod call: %#v", m)
	}
	fn, ok := got.Get("preDamageFunc").(*Func)
	if !ok || !strings.HasPrefix(fn.Source, "function(activeSkill, output)") || !strings.HasSuffix(fn.Source, "end") || fn.Line != 5 {
		t.Errorf("func: %#v", got.Get("preDamageFunc"))
	}
	if got.Get("after") != Number(1) {
		t.Error("parsing must continue after a function body")
	}
}

func TestReturnFunctionWrapper(t *testing.T) {
	// Bases/*.lua and Skills/*.lua: the file returns a function that fills tables.
	src := `-- generated
return function(itemBases)
itemBases["Iron Ring"] = { type = "Ring" }
itemBases["Coral Ring"] = {
	type = "Ring",
}
itemBases.Plain = { type = "Belt" }
end`
	got := eval(t, src).(*Table) // the first parameter, filled in
	if !reflect.DeepEqual(got.Keys, []Value{String("Iron Ring"), String("Coral Ring"), String("Plain")}) {
		t.Fatalf("keys: %#v", got.Keys)
	}
	if got.Get("Coral Ring").(*Table).Get("type") != String("Ring") {
		t.Error("value")
	}
}

func TestReturnFunctionOfConstructors(t *testing.T) {
	// Minions.lua / SkillStatMap.lua: parameters are constructors, the body returns the data.
	got := eval(t, `return function(mod, flag)
return { ["a"] = { mod("Armour", "INC", 40), flag("Condition") } }
end`).(*Table)
	a := got.Get("a").(*Table)
	if a.List[0].(*Call).Func != "mod" || a.List[1].(*Call).Func != "flag" {
		t.Errorf("calls: %#v", a.List)
	}
}

func TestGlobalsFunctionsAndVivify(t *testing.T) {
	// Global.lua and Uniques/Special/New.lua.
	got := eval(t, `colorCodes = { NORMAL = "^x1" }
colorCodes.PHYS = colorCodes.NORMAL
function updateColorCode(code, color) colorCodes[code] = color end
function util.helper() end
data.uniques.new = { "x" }
return { codes = colorCodes, new = data.uniques.new, fn = updateColorCode }`).(*Table)
	if got.Get("codes").(*Table).Get("PHYS") != String("^x1") {
		t.Errorf("field copy: %#v", got.Get("codes"))
	}
	if got.Get("new").(*Table).List[0] != String("x") {
		t.Errorf("vivified global: %#v", got.Get("new"))
	}
	if _, ok := got.Get("fn").(*Func); !ok {
		t.Errorf("function statement: %#v", got.Get("fn"))
	}
}

func TestModCacheShape(t *testing.T) {
	src := `local c = {}
(function()
c[""]={nil," "}
c["+10 to maximum Life"]={{[1]={flags=0,keywordFlags=0,name="Life",type="BASE",value=10}},nil}
end)();(function()
c["x"]={nil,"x "}
end)();
return c`
	got := eval(t, src).(*Table)
	if len(got.Keys) != 3 {
		t.Fatalf("keys: %#v", got.Keys)
	}
	life := got.Get("+10 to maximum Life").(*Table)
	mods := life.List[0].(*Table)
	if mods.Get(Number(1)).(*Table).Get("value") != Number(10) {
		t.Errorf("mod: %#v", mods)
	}
	if empty := got.Get("").(*Table); len(empty.List) != 2 || empty.List[0] != nil || empty.List[1] != String(" ") {
		t.Errorf("nil in list: %#v", empty.List)
	}
}

func TestEnvAndLocals(t *testing.T) {
	src := `local base = { a = 1 }
return { copy = base, other = Known.Thing }`
	env := map[string]Value{"Known": &Table{}}
	got, err := Eval(src, env)
	if err != nil {
		t.Fatal(err)
	}
	tb := got.(*Table)
	if tb.Get("copy").(*Table).Get("a") != Number(1) {
		t.Errorf("local: %#v", tb.Get("copy"))
	}
	if tb.Get("other") != Ref("Known.Thing") {
		t.Errorf("unresolved field stays a ref: %#v", tb.Get("other"))
	}
}

func TestErrorsHaveLine(t *testing.T) {
	for _, src := range []string{
		"return {\n a = ,\n}",
		"return \"unterminated",
		"return {\n x = 1 < 2 }",
		"while true do end",
	} {
		_, err := Eval(src, nil)
		if err == nil {
			t.Errorf("%q: expected error", src)
			continue
		}
		if !strings.Contains(err.Error(), "line ") {
			t.Errorf("%q: error without line: %v", src, err)
		}
	}
}

func TestBuiltins(t *testing.T) {
	bor := Builtin(func(args []Value) (Value, error) {
		out := 0
		for _, a := range args {
			n, ok := a.(Number)
			if !ok {
				return nil, ErrKeepCall
			}
			out |= int(n)
		}
		return Number(out), nil
	})
	env := map[string]Value{"bor": bor, "Flag": &Table{}}
	env["Flag"].(*Table).Set("A", Number(1))
	env["Flag"].(*Table).Set("B", Number(4))
	v, err := Eval(`return { a = bor(Flag.A, Flag.B), b = bor(Flag.A, Other.X) }`, env)
	if err != nil {
		t.Fatal(err)
	}
	tb := v.(*Table)
	if tb.Get("a") != Number(5) {
		t.Errorf("evaluated: %#v", tb.Get("a"))
	}
	if c, ok := tb.Get("b").(*Call); !ok || c.Func != "bor" {
		t.Errorf("kept call: %#v", tb.Get("b"))
	}
}

func TestExtractLocalAndGlobals(t *testing.T) {
	src := `local helper = 1
local function f() return 1 end
local formList = { ["^(%d+)%% increased"] = "INC", fn = function(x) return x end }
for i = 1, 3 do end`
	v, err := ExtractLocal(src, "formList", nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.(*Table).Get("^(%d+)%% increased") != String("INC") {
		t.Errorf("formList: %#v", v)
	}
	if _, err := ExtractLocal(src, "missing", nil); err == nil {
		t.Error("missing local must fail")
	}
	g, err := EvalGlobals("ModFlag = { }\nModFlag.Attack = 0x01\nModFlag.Hit = 0x04", nil)
	if err != nil {
		t.Fatal(err)
	}
	if g["ModFlag"].(*Table).Get("Hit") != Number(4) {
		t.Errorf("globals: %#v", g)
	}
}

func TestIndexUnresolvedName(t *testing.T) {
	v := eval(t, `return { a = data.list["x"], b = data.list[2].name }`).(*Table)
	if v.Get("a") != Ref("data.list[x]") || v.Get("b") != Ref("data.list[2].name") {
		t.Errorf("refs: %#v %#v", v.Get("a"), v.Get("b"))
	}
}

func TestFuncCall(t *testing.T) {
	v := eval(t, `return { f = function(num, kind) return { mod = { "Life", num }, kind = kind } end, g = function(n) if n then return 1 end end }`).(*Table)
	f := v.Get("f").(*Func)
	out, err := f.Call([]Value{Ref("$arg:1"), String("x")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tb := out.(*Table)
	if tb.Get("kind") != String("x") || tb.Get("mod").(*Table).List[1] != Ref("$arg:1") {
		t.Errorf("call result: %#v", tb)
	}
	if _, err := v.Get("g").(*Func).Call(nil, nil); err == nil {
		t.Error("if statements are not supported and must fail")
	}
}
