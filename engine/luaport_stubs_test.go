//go:build luaport

package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	lua "github.com/yuin/gopher-lua"
)

// The Lua side of TestLuaPort: a gopher-lua state with just enough of PoB
// loaded to run a skill's own Lua hook on a synthetic activeSkill.
//
// Everything that does arithmetic is PoB's code, not a reimplementation:
// Data/Global.lua is run whole (SkillType, ModFlag, KeywordFlag,
// MatchKeywordFlags), and the functions the hooks reach — ModStore/ModList
// Sum, More, Flag, Override, NewMod, modLib.createMod, round, copyTable,
// calcLib.mod, calcSkillDuration, Data.lua's skill mod makers — are cut out
// of their source files by text (pobFunc) and run with the file-local
// upvalues they expect. Loading those modules whole would pull in the GUI
// runtime (Common.lua requires lcurl, xml, ...) and ModParser.
//
// Stubbed: the class system (a plain metatable stands in for newClass),
// data.misc (only the two magic numbers the hooks read, copied from
// Data.lua's lines), and the activeSkill itself (luaPortRunHook). EvalMod is
// absent: case mods carry no tags, so ModList never calls it.

// pobSrcDir is the PoB src directory (env POB_SRC); it skips the test when
// the directory is missing.
func pobSrcDir(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("POB_SRC")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "GoProjects", "poe-pob-oracle", ".pob", "v2.68.1", "src")
	}
	if _, err := os.Stat(filepath.Join(dir, "Data", "Global.lua")); err != nil {
		t.Skipf("PoB source not found at %s (set POB_SRC): %v", dir, err)
	}
	return dir
}

// pobFunc cuts a top-level definition out of a PoB source file: from the
// first line starting with start through the first line that closes it at
// column 0 ("end" for a function, "}" for a table constructor).
func pobFunc(t *testing.T, dir, file, start string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	closer := "end"
	if strings.HasSuffix(start, "{") {
		closer = "}"
	}
	var out []string
	for line := range strings.SplitSeq(strings.ReplaceAll(string(src), "\r\n", "\n"), "\n") {
		if out == nil && !strings.HasPrefix(line, start) {
			continue
		}
		out = append(out, line)
		if len(out) > 1 && strings.TrimRight(line, " \t") == closer {
			return strings.Join(out, "\n")
		}
	}
	t.Fatalf("%s: no complete definition starting with %q", file, start)
	return ""
}

// pobField is one `Name = value,` line of a PoB table constructor, without
// the trailing comma.
func pobField(t *testing.T, dir, file, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s*` + name + `\s*=\s*([^,\n]+),`).FindSubmatch(src)
	if m == nil {
		t.Fatalf("%s: no field %s", file, name)
	}
	return name + " = " + string(m[1])
}

// luaBit is LuaJIT's bit library over 32-bit integers, which Global.lua and
// the skill files' statMaps use at load time.
func luaBit(ls *lua.LState) *lua.LTable {
	arg := func(ls *lua.LState, i int) int32 { return int32(int64(ls.CheckNumber(i))) }
	fold := func(op func(a, b int32) int32) lua.LGFunction {
		return func(ls *lua.LState) int {
			r := arg(ls, 1)
			for i := 2; i <= ls.GetTop(); i++ {
				r = op(r, arg(ls, i))
			}
			ls.Push(lua.LNumber(r))
			return 1
		}
	}
	shift := func(op func(a int32, n uint) int32) lua.LGFunction {
		return func(ls *lua.LState) int {
			ls.Push(lua.LNumber(op(arg(ls, 1), uint(arg(ls, 2))&31)))
			return 1
		}
	}
	return ls.SetFuncs(ls.NewTable(), map[string]lua.LGFunction{
		"band":    fold(func(a, b int32) int32 { return a & b }),
		"bor":     fold(func(a, b int32) int32 { return a | b }),
		"bxor":    fold(func(a, b int32) int32 { return a ^ b }),
		"bnot":    func(ls *lua.LState) int { ls.Push(lua.LNumber(^arg(ls, 1))); return 1 },
		"tobit":   func(ls *lua.LState) int { ls.Push(lua.LNumber(arg(ls, 1))); return 1 },
		"lshift":  shift(func(a int32, n uint) int32 { return a << n }),
		"rshift":  shift(func(a int32, n uint) int32 { return int32(uint32(a) >> n) }),
		"arshift": shift(func(a int32, n uint) int32 { return a >> n }),
	})
}

// luaPortGlue builds the activeSkill for a hook and runs it. Not PoB code.
const luaPortGlue = `
function newModList(mods)
	local list = setmetatable({ }, ModListClass)
	for _, m in ipairs(mods) do
		list:NewMod(m.name, m.type, m.value, m.source)
	end
	return list
end

-- luaPortRunHook runs skills[id][hook] (the first id, sorted, whose name is
-- skillName and which has the hook) on a synthetic activeSkill. output is
-- both the hook's output argument and actor.output, as in calcs.offence.
function luaPortRunHook(skillName, hook, part, stats, skillData, enemyMods, output)
	local ids = { }
	for id, grantedEffect in pairs(skills) do
		if grantedEffect.name == skillName and grantedEffect[hook] then
			t_insert(ids, id)
		end
	end
	if #ids == 0 then
		error("no skill " .. skillName .. " with " .. hook)
	end
	table.sort(ids)
	local activeSkill = {
		skillPart = part,
		skillCfg = { },
		skillData = skillData,
		skillModList = newModList(stats),
		actor = { enemy = { modDB = newModList(enemyMods) }, output = output },
	}
	skills[ids[1]][hook](activeSkill, output)
	return activeSkill
end

function loadSkillFile(path)
	local chunk = assert(loadfile(path))
	chunk()(skills, makeSkillMod, makeFlagMod, makeSkillDataMod)
end
`

// newPoBLua is a Lua state with PoB's globals, mod list and the skill files
// loaded. It fails the test if a part a hook needs does not load; skill
// files that fail to load are logged and skipped.
func newPoBLua(t *testing.T, dir string) *lua.LState {
	t.Helper()
	ls := lua.NewState()
	t.Cleanup(ls.Close)
	ls.SetGlobal("bit", luaBit(ls))
	if err := ls.DoString(pobFunc(t, dir, "Modules/Common.lua", "function copyTable(")); err != nil {
		t.Fatal(err)
	}
	if err := ls.DoFile(filepath.Join(dir, "Data", "Global.lua")); err != nil {
		t.Fatalf("Data/Global.lua: %v", err)
	}
	// One chunk, so the cut-out functions share the file-local upvalues
	// their source files declare.
	parts := []string{
		`local ipairs, pairs, select, t_insert = ipairs, pairs, select, table.insert
local m_floor, m_ceil, m_min, m_max, m_modf = math.floor, math.ceil, math.min, math.max, math.modf
local band, bor = bit.band, bit.bor
skills = { }
calcLib = { }
modLib = { }
ModStoreClass = { }
ModListClass = setmetatable({ }, { __index = ModStoreClass })
ModListClass.__index = ModListClass`,
		"data = { misc = { " + pobField(t, dir, "Modules/Data.lua", "ServerTickRate") + ", " +
			pobField(t, dir, "Modules/Data.lua", "BuffExpirationSlowCap") + " } }",
		pobFunc(t, dir, "Modules/Data.lua", "data.highPrecisionMods = {"),
		pobFunc(t, dir, "Modules/Common.lua", "function round("),
		pobFunc(t, dir, "Modules/ModTools.lua", "function modLib.createMod("),
		"local mod_createMod = modLib.createMod",
		pobFunc(t, dir, "Classes/ModStore.lua", "function ModStoreClass:NewMod("),
		pobFunc(t, dir, "Classes/ModStore.lua", "function ModStoreClass:Sum("),
		pobFunc(t, dir, "Classes/ModStore.lua", "function ModStoreClass:More("),
		pobFunc(t, dir, "Classes/ModStore.lua", "function ModStoreClass:Flag("),
		pobFunc(t, dir, "Classes/ModStore.lua", "function ModStoreClass:Override("),
		pobFunc(t, dir, "Classes/ModList.lua", "function ModListClass:AddMod("),
		pobFunc(t, dir, "Classes/ModList.lua", "function ModListClass:SumInternal("),
		pobFunc(t, dir, "Classes/ModList.lua", "function ModListClass:MoreInternal("),
		pobFunc(t, dir, "Classes/ModList.lua", "function ModListClass:FlagInternal("),
		pobFunc(t, dir, "Classes/ModList.lua", "function ModListClass:OverrideInternal("),
		pobFunc(t, dir, "Modules/CalcTools.lua", "function calcLib.mod("),
		pobFunc(t, dir, "Modules/CalcOffence.lua", "function calcSkillDuration("),
		pobFunc(t, dir, "Modules/Data.lua", "local function makeSkillMod("),
		pobFunc(t, dir, "Modules/Data.lua", "local function makeFlagMod("),
		pobFunc(t, dir, "Modules/Data.lua", "local function makeSkillDataMod("),
		luaPortGlue,
	}
	if err := ls.DoString(strings.Join(parts, "\n")); err != nil {
		t.Fatalf("PoB prelude: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "Data", "Skills", "act_*.lua"))
	sup, _ := filepath.Glob(filepath.Join(dir, "Data", "Skills", "sup_*.lua"))
	files = append(append(files, sup...), filepath.Join(dir, "Data", "Skills", "other.lua"))
	for _, f := range files {
		err := ls.CallByParam(lua.P{Fn: ls.GetGlobal("loadSkillFile"), Protect: true}, lua.LString(f))
		if err != nil {
			t.Logf("skipping %s: %v", filepath.Base(f), err)
		}
	}
	return ls
}

// luaToGo converts a Lua value to Go: numbers to float64, nested tables to
// map[string]any (keys formatted by Lua's tostring).
func luaToGo(v lua.LValue) any {
	switch x := v.(type) {
	case lua.LNumber:
		return float64(x)
	case lua.LBool:
		return bool(x)
	case lua.LString:
		return string(x)
	case *lua.LTable:
		m := map[string]any{}
		x.ForEach(func(k, v lua.LValue) { m[k.String()] = luaToGo(v) })
		return m
	case *lua.LNilType:
		return nil
	}
	return v.String()
}

// goToLua converts the case inputs (float64, bool, string, maps and slices
// of them) to Lua.
func goToLua(ls *lua.LState, v any) lua.LValue {
	switch x := v.(type) {
	case nil:
		return lua.LNil
	case float64:
		return lua.LNumber(x)
	case int:
		return lua.LNumber(x)
	case bool:
		return lua.LBool(x)
	case string:
		return lua.LString(x)
	case map[string]any:
		tbl := ls.NewTable()
		for k, e := range x {
			tbl.RawSetString(k, goToLua(ls, e))
		}
		return tbl
	case map[string]float64:
		tbl := ls.NewTable()
		for k, e := range x {
			tbl.RawSetString(k, lua.LNumber(e))
		}
		return tbl
	case []luaPortMod:
		tbl := ls.NewTable()
		for _, m := range x {
			tbl.Append(goToLua(ls, map[string]any{
				"name": m.Name, "type": m.Type, "value": m.Value, "source": m.Source,
			}))
		}
		return tbl
	}
	panic("goToLua: unsupported type")
}
