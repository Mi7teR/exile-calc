//go:build luaport

package engine

import (
	"fmt"
	"maps"
	"math"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	lua "github.com/yuin/gopher-lua"

	"github.com/Mi7teR/exile-calc/gamedata"
)

// TestLuaPort runs a skill hook twice on the same synthetic skill — PoB's
// Lua function (Data/Skills/*.lua) under gopher-lua, and our port from
// skillHookRegistry — and compares output, skillData and the skill's mod
// list afterwards. One hook at a time, without the Docker oracle:
//
//	make luaport   # go test -tags luaport ./engine -run TestLuaPort
//
// Path of Mastery (crates/calc/src/snapshot.rs, probe.rs; MIT) grades its
// port against state recorded from instrumented full PoB runs. That needs
// a running PoB per build; a hook only needs its inputs, so the cases here
// build them by hand instead.

// luaPortMod is an untagged mod of a case's skill or enemy mod list.
type luaPortMod struct {
	Name, Type string
	Value      float64
	Source     string
}

// luaPortCase is one hook run. stats is the skill's mod list
// (skillModList), data its skillData, enemy the enemy's mod list, output
// actor.output before the hook. finding, when set, names a known
// difference between PoB and our port: the case is skipped with the diff
// while it still reproduces.
type luaPortCase struct {
	skill   string
	hook    string // the Lua field: "preDamageFunc" or "initialFunc"
	part    int    // skillPart; 0 is nil (single part skill)
	stats   []luaPortMod
	data    map[string]any
	enemy   []luaPortMod
	output  map[string]float64
	finding string
}

func luaPortCases() []luaPortCase {
	brandSpeed := []luaPortMod{
		{"Speed", "INC", 30, "Tree"},
		{"BrandActivationFrequency", "INC", 25, "Gem"},
		// Two MORE mods of one name: PoB rounds their product to 2
		// decimals (ModList:MoreInternal), 1.15*0.9 = 1.035 -> 1.04.
		{"BrandActivationFrequency", "MORE", 15, "Support"},
		{"BrandActivationFrequency", "MORE", -10, "Support"},
	}
	wintertide := func(part int) luaPortCase {
		return luaPortCase{
			skill: "Wintertide Brand", hook: "preDamageFunc", part: part,
			stats: append([]luaPortMod{
				{"Multiplier:WintertideBrandMaxStages", "BASE", 20, "Gem"},
				{"Multiplier:WintertideBrandDamagePerStage", "BASE", 10, "Gem"},
				{"Duration", "INC", 40, "Tree"},
				{"PrimaryDuration", "MORE", 20, "Support"},
				{"TertiaryDuration", "BASE", 0.5, "Tree"},
			}, brandSpeed...),
			data: map[string]any{
				"repeatFrequency": 0.35, "duration": 4.0, "durationTertiary": 2.0,
				"attachedBrandCount": 2.0, "debuff": true,
			},
			enemy:  []luaPortMod{{"BuffExpireFaster", "INC", -40, "Config"}},
			output: map[string]float64{},
		}
	}
	return []luaPortCase{
		{
			skill: "Armageddon Brand", hook: "preDamageFunc", stats: brandSpeed,
			data: map[string]any{"repeatFrequency": 1.5}, output: map[string]float64{},
		},
		{
			skill: "Storm Brand", hook: "preDamageFunc",
			stats: []luaPortMod{{"Speed", "INC", 12, "Tree"}, {"BrandActivationFrequency", "MORE", 30, "Support"}},
			data:  map[string]any{"repeatFrequency": 0.35, "hitTimeOverride": 9.0}, output: map[string]float64{},
		},
		{
			skill: "Flameblast", hook: "preDamageFunc",
			stats: []luaPortMod{
				{"Multiplier:FlameblastStage", "BASE", 10, "Config"},
				{"Multiplier:FlameblastMinimumStage", "BASE", 1, "Gem"},
			},
			data: map[string]any{}, output: map[string]float64{},
		},
		{
			// Fewer stages than the minimum: one release.
			skill: "Flameblast", hook: "preDamageFunc",
			stats: []luaPortMod{{"Multiplier:FlameblastMinimumStage", "BASE", 3, "Gem"}},
			data:  map[string]any{}, output: map[string]float64{},
		},
		wintertide(1),
		wintertide(2),
		{
			skill: "Righteous Fire", hook: "preDamageFunc",
			data:   map[string]any{"RFLifeMultiplier": 0.7, "RFESMultiplier": 0.7},
			output: map[string]float64{"Life": 9272, "EnergyShield": 332, "LifeUnreserved": 9272},
		},
		{
			// 1 life unreserved: no burn.
			skill: "Righteous Fire", hook: "preDamageFunc",
			data:   map[string]any{"RFLifeMultiplier": 0.7, "RFESMultiplier": 0.7},
			output: map[string]float64{"Life": 9272, "EnergyShield": 332, "LifeUnreserved": 1},
		},
		{
			skill: "Righteous Fire of Arcane Devotion", hook: "preDamageFunc",
			data:   map[string]any{"RFManaMultiplier": 0.7},
			output: map[string]float64{"Mana": 8667, "LifeUnreserved": 20},
		},
		{
			skill: "Forbidden Rite of Soul Sacrifice", hook: "preDamageFunc", part: 2,
			stats: []luaPortMod{{"ChaosDamageTaken", "INC", 10, "Tree"}, {"DamageTaken", "BASE", -50, "Tree"}},
			data: map[string]any{"ChaosMin": 697.0, "ChaosMax": 1046.0, "energyShieldDealtAsChaos": 0.2,
				"SelfDamageTakenES": 0.5, "dpsMultiplier": 1.0},
			output: map[string]float64{"EnergyShield": 8147, "ChaosResist": 75, "ProjectileCount": 5, "Life": 1},
		},
		{
			skill: "Forbidden Rite", hook: "preDamageFunc", part: 1,
			data: map[string]any{"ChaosMin": 500.0, "ChaosMax": 800.0, "lifeDealtAsChaos": 0.1,
				"energyShieldDealtAsChaos": 0.1, "SelfDamageTakenLife": 0.25, "SelfDamageTakenES": 0.25},
			output: map[string]float64{"EnergyShield": 1200, "ChaosResist": 20, "ProjectileCount": 3, "Life": 4300},
		},
		{
			skill: "Divine Ire of Holy Lightning", hook: "preDamageFunc", part: 2,
			stats: []luaPortMod{{"Multiplier:DivineIreofHolyLightningStage", "BASE", 10, "Config"}},
			data:  map[string]any{}, output: map[string]float64{},
		},
		{
			skill: "Divine Ire of Disintegration", hook: "preDamageFunc", part: 1,
			stats: []luaPortMod{{"Multiplier:DivineIreofDisintegrationStage", "BASE", 7, "Config"}},
			data:  map[string]any{}, output: map[string]float64{},
		},
		{
			skill: "Vaal Righteous Fire", hook: "preDamageFunc",
			data:   map[string]any{"percentSacrificed": 0.6, "RFMultiplier": 1.58},
			output: map[string]float64{"Life": 6525, "EnergyShield": 120},
		},
		{
			skill: "Ball Lightning", hook: "preDamageFunc", part: 1,
			data:   map[string]any{"strikeInterval": 0.15, "projectileSpeed": 40.0, "duration": 2.0},
			output: map[string]float64{"AreaOfEffectRadius": 22},
		},
		{
			// All bolts in range: no skillDist in the case's skillCfg, so the
			// ball is cast at one strike radius from the enemy.
			skill: "Ball Lightning",
			hook:  "preDamageFunc",
			part:  2,
			stats: []luaPortMod{
				{"ProjectileSpeed", "INC", 40, "Tree"},
				{"ProjectileSpeed", "MORE", 33, "Gem"},
				{"EnemyRadius", "BASE", 5, "Config"},
			},
			data: map[string]any{
				"strikeInterval":  0.15,
				"projectileSpeed": 40.0,
				"duration":        2.0,
				"dpsMultiplier":   2.0,
			},
			output: map[string]float64{"AreaOfEffectRadius": 22},
		},
		{
			skill: "Seismic Trap", hook: "preDamageFunc", part: 5,
			stats: []luaPortMod{
				{"MaximumWaves", "BASE", 5, "Gem"},
				{"EnemyRadius", "BASE", 5, "Config"},
				{"SeismicPulseFrequency", "INC", 20, "Gem"},
				{"TrapThrowingSpeed", "MORE", 10, "Support"},
				{"Duration", "INC", 30, "Tree"},
			},
			data:   map[string]any{"repeatInterval": 0.9, "duration": 3.5},
			output: map[string]float64{"TrapCooldown": 4, "AreaOfEffectRadius": 18, "AreaOfEffectRadiusSecondary": 9},
		},
		{
			skill: "Seismic Trap of Swells", hook: "preDamageFunc", part: 2,
			stats:  []luaPortMod{{"MaximumWaves", "BASE", 3, "Gem"}},
			data:   map[string]any{},
			output: map[string]float64{"AreaOfEffectRadius": 20, "AreaOfEffectRadiusSecondary": 10},
		},
		{
			skill: "Blade Blast", hook: "preDamageFunc", part: 2,
			data: map[string]any{"dpsBaseMultiplier": 7.0, "dpsMultiplier": 1.5}, output: map[string]float64{},
		},
		{
			skill: "Blade Blast of Unloading", hook: "preDamageFunc", part: 1,
			data: map[string]any{"dpsBaseMultiplier": 4.0}, output: map[string]float64{},
		},
		{
			skill: "Tornado", hook: "preDamageFunc",
			data: map[string]any{"damageInterval": 0.25}, output: map[string]float64{},
		},
		{
			skill: "Blood Sacrament", hook: "initialFunc",
			data:   map[string]any{"LifeReservedPercent": 4.5, "LifeReservedBase": 312.0},
			output: map[string]float64{"LifeReservedPercent": 40},
		},
		{
			skill: "Blood Sacrament", hook: "initialFunc",
			data:   map[string]any{"LifeReservedPercent": 4.5, "LifeReservedBase": 312.0},
			output: map[string]float64{"LifeReservedPercent": 100},
		},
		{
			skill: "Blood Sacrament", hook: "initialFunc",
			data:   map[string]any{},
			output: map[string]float64{"LifeReservedPercent": 40},
			finding: "no LifeReservedBase: PoB adds math.huge life per stage, our port 0 " +
				"(documented in bloodSacramentInitial)",
		},
	}
}

// luaPortState is what the comparison looks at after a hook.
type luaPortState struct {
	Output    map[string]float64
	SkillData map[string]any
	Mods      []luaPortMod
}

func TestLuaPort(t *testing.T) {
	dir := pobSrcDir(t)
	ls := newPoBLua(t, dir)
	for _, c := range luaPortCases() {
		name := fmt.Sprintf("%s/%s/part%d/out%v", c.skill, c.hook, c.part, c.output["LifeReservedPercent"])
		t.Run(name, func(t *testing.T) {
			want := runLuaHook(t, ls, c)
			got := runGoHook(t, c)
			diff := cmp.Diff(want, got, cmpopts.EquateApprox(1e-9, 0), cmpopts.EquateEmpty())
			switch {
			case c.finding != "" && diff != "":
				t.Skipf("finding: %s (-lua +go):\n%s", c.finding, diff)
			case c.finding != "":
				t.Errorf("finding %q no longer reproduces", c.finding)
			case diff != "":
				t.Errorf("hook mismatch (-lua +go):\n%s", diff)
			}
		})
	}
}

func runLuaHook(t *testing.T, ls *lua.LState, c luaPortCase) luaPortState {
	t.Helper()
	output := goToLua(ls, maps.Clone(c.output))
	args := []lua.LValue{
		lua.LString(c.skill), lua.LString(c.hook), lua.LNil,
		goToLua(ls, c.stats), goToLua(ls, maps.Clone(c.data)), goToLua(ls, c.enemy), output,
	}
	if c.part != 0 {
		args[2] = lua.LNumber(c.part)
	}
	err := ls.CallByParam(lua.P{Fn: ls.GetGlobal("luaPortRunHook"), NRet: 1, Protect: true}, args...)
	if err != nil {
		t.Fatal(err)
	}
	activeSkill := ls.Get(-1).(*lua.LTable)
	ls.Pop(1)
	st := luaPortState{
		Output:    map[string]float64{},
		SkillData: luaToGo(activeSkill.RawGetString("skillData")).(map[string]any),
	}
	for k, v := range luaToGo(output).(map[string]any) {
		if f, ok := v.(float64); ok {
			st.Output[k] = f
		}
	}
	activeSkill.RawGetString("skillModList").(*lua.LTable).ForEach(func(k, v lua.LValue) {
		if _, ok := k.(lua.LNumber); !ok {
			return
		}
		m := v.(*lua.LTable)
		st.Mods = append(st.Mods, luaPortMod{
			Name: m.RawGetString("name").String(), Type: m.RawGetString("type").String(),
			Value: float64(m.RawGetString("value").(lua.LNumber)), Source: m.RawGetString("source").String(),
		})
	})
	sortLuaPortMods(st.Mods)
	return st
}

func runGoHook(t *testing.T, c luaPortCase) luaPortState {
	t.Helper()
	actor := NewModDB()
	actor.enemy = NewModDB()
	for _, m := range c.enemy {
		actor.enemy.NewMod(m.Name, m.Type, m.Value, m.Source)
	}
	actor.output = maps.Clone(c.output)
	s := &activeSkill{
		active: &skillEffect{ge: &gamedata.Skill{Name: c.skill}},
		mods:   NewModDB(),
		flags:  map[string]bool{},
		part:   c.part,
		data:   &skillData{},
	}
	for _, m := range c.stats {
		s.mods.NewMod(m.Name, m.Type, m.Value, m.Source)
	}
	for k, v := range c.data {
		s.data.set(k, v)
	}
	h := hooksOf(s)
	switch {
	case c.hook == "preDamageFunc" && h != nil && h.preDamage != nil:
		h.preDamage(actor, s, actor.output)
	case c.hook == "initialFunc" && h != nil && h.initial != nil:
		h.initial(actor, s, hitSource{}, nil)
	default:
		t.Fatalf("no Go %s hook for %s", c.hook, c.skill)
	}
	st := luaPortState{Output: actor.output, SkillData: map[string]any{}}
	keys := []string{"dpsMultiplier", "hitTimeOverride", "cooldown", "duration", "storedUses", "timeOverride"}
	for k := range s.data.extra {
		keys = append(keys, k)
	}
	for _, k := range keys {
		if v, ok := s.data.get(k); ok {
			st.SkillData[k] = v
		}
	}
	for _, list := range s.mods.mods {
		for _, m := range list {
			v, ok := m.Value.(float64)
			if !ok || len(m.Tags) > 0 || m.Flags != 0 || m.KeywordFlags != 0 {
				t.Fatalf("mod %+v: not comparable with the Lua side", m)
			}
			st.Mods = append(st.Mods, luaPortMod{Name: m.Name, Type: m.Type, Value: v, Source: m.Source})
		}
	}
	sortLuaPortMods(st.Mods)
	return st
}

// sortLuaPortMods orders mods independently of map and insertion order.
func sortLuaPortMods(mods []luaPortMod) {
	sort.Slice(mods, func(i, j int) bool {
		a, b := mods[i], mods[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.Value < b.Value || math.IsNaN(a.Value)
	})
}
