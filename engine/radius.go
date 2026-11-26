package engine

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// radiusFunc is a jewelOtherFuncs entry (Modules/ModParser.lua): given a
// node in the jewel's radius, its own mods (node.modList) and the mods
// gathered so far for it (out, with what earlier jewels added), it returns
// mods to add.
type radiusFunc func(node *gamedata.Node, own, out []Mod) []Mod

// nodeType is PassiveTree.lua's node.type.
func nodeType(n *gamedata.Node) string {
	switch {
	case n.ClassStartIndex != nil:
		return "ClassStart"
	case n.IsAscendancyStart:
		return "AscendClassStart"
	case n.IsJewelSocket:
		return "Socket"
	case n.IsMastery:
		return "Mastery"
	case n.IsKeystone:
		return "Keystone"
	case n.IsNotable:
		return "Notable"
	}
	return "Normal"
}

type radiusPattern struct {
	re   *regexp.Regexp
	make func(caps []string, source string) radiusFunc
}

func numCap(caps []string, i int) float64 {
	f, _ := strconv.ParseFloat(caps[i], 64)
	return f
}

// grant adds mods to every node in radius except keystones, sockets and
// class starts ("Passive Skills in Radius also grant ...").
func grant(source string, mods func(caps []string) []Mod) func(caps []string, source string) radiusFunc {
	return func(caps []string, src string) radiusFunc {
		out := mods(caps)
		return func(n *gamedata.Node, _, _ []Mod) []Mod {
			switch nodeType(n) {
			case "Keystone", "Socket", "ClassStart":
				return nil
			}
			added := make([]Mod, len(out))
			for i, m := range out {
				m.Source = src
				added[i] = m
			}
			return added
		}
	}
}

func mod(name, typ string, v float64) Mod { return Mod{Name: name, Type: typ, Value: v} }

// simpleConv ports getSimpleConv(srcList, dst, type, remove, factor,
// srcType): each node mod of a src name and type (srcType when set) is
// removed when remove is set and re-added as dst, times factor and floored
// when a factor is given. Attributes from tattoos are not converted.
func simpleConv(src []string, dst, typ string, remove bool, factor float64, srcType string) func(caps []string, source string) radiusFunc {
	return func(_ []string, _ string) radiusFunc {
		return func(n *gamedata.Node, own, _ []Mod) []Mod {
			var out []Mod
			for _, s := range src {
				for _, m := range own {
					v, ok := num(m.Value)
					want := typ
					if srcType != "" {
						want = srcType
					}
					if m.Name != s || (m.Type != want && m.Type != typ) || !ok {
						continue
					}
					if remove {
						neg := m
						neg.Type, neg.Value = typ, -v
						out = append(out, neg)
					}
					conv := m
					conv.Name, conv.Type = dst, typ
					if factor != 0 {
						conv.Value = math.Floor(v * factor)
					}
					out = append(out, conv)
				}
			}
			return out
		}
	}
}

// pctConv is a jewelOtherFuncs entry with "at (%d+)% of its value".
func pctConv(src []string, dst, typ string, remove bool, srcType string) func(caps []string, source string) radiusFunc {
	return func(caps []string, source string) radiusFunc {
		return simpleConv(src, dst, typ, remove, numCap(caps, 1)/100, srcType)(caps, source)
	}
}

var radiusPatterns = func() []radiusPattern {
	type def struct {
		pat  string
		make func(caps []string, source string) radiusFunc
	}
	defs := []def{
		{`^strength from passives in radius is transformed to dexterity$`, simpleConv([]string{"Str"}, "Dex", "BASE", true, 0, "")},
		{`^dexterity from passives in radius is transformed to strength$`, simpleConv([]string{"Dex"}, "Str", "BASE", true, 0, "")},
		{`^strength from passives in radius is transformed to intelligence$`, simpleConv([]string{"Str"}, "Int", "BASE", true, 0, "")},
		{`^intelligence from passives in radius is transformed to strength$`, simpleConv([]string{"Int"}, "Str", "BASE", true, 0, "")},
		{`^dexterity from passives in radius is transformed to intelligence$`, simpleConv([]string{"Dex"}, "Int", "BASE", true, 0, "")},
		{`^intelligence from passives in radius is transformed to dexterity$`, simpleConv([]string{"Int"}, "Dex", "BASE", true, 0, "")},
		{`^increases and reductions to life in radius are transformed to apply to energy shield$`, simpleConv([]string{"Life"}, "EnergyShield", "INC", true, 0, "")},
		{`^increases and reductions to evasion rating in radius are transformed to apply to armour$`, simpleConv([]string{"Evasion"}, "Armour", "INC", true, 0, "")},
		{`^increases and reductions to energy shield in radius are transformed to apply to armour at (\d+)% of their value$`, pctConv([]string{"EnergyShield"}, "Armour", "INC", true, "")},
		{`^increases and reductions to life in radius are transformed to apply to mana at (\d+)% of their value$`, pctConv([]string{"Life"}, "Mana", "INC", true, "")},
		{`^passives granting lightning resistance or all elemental resistances in radius also grant chance to block spells? ?(?:damage)? at (\d+)% of its value$`, pctConv([]string{"LightningResist", "ElementalResist"}, "SpellBlockChance", "BASE", false, "")},
		{`^passives granting lightning resistance or all elemental resistances in radius also grant increased maximum energy shield at (\d+)% of its value$`, pctConv([]string{"LightningResist", "ElementalResist"}, "EnergyShield", "INC", false, "BASE")},
		{`^passives granting cold resistance or all elemental resistances in radius also grant chance to suppress spell damage at (\d+)% of its value$`, pctConv([]string{"ColdResist", "ElementalResist"}, "SpellSuppressionChance", "BASE", false, "")},
		{`^passives granting cold resistance or all elemental resistances in radius also grant increased maximum mana at (\d+)% of its value$`, pctConv([]string{"ColdResist", "ElementalResist"}, "Mana", "INC", false, "BASE")},
		{`^passives granting fire resistance or all elemental resistances in radius also grant chance to block(?: attack damage)? at (\d+)% of its value$`, pctConv([]string{"FireResist", "ElementalResist"}, "BlockChance", "BASE", false, "")},
		{`^passives granting fire resistance or all elemental resistances in radius also grant increased maximum life at (\d+)% of its value$`, pctConv([]string{"FireResist", "ElementalResist"}, "Life", "INC", false, "BASE")},
		{`^(\d+)% increased effect of non-keystone passive skills in radius$`, func(caps []string, src string) radiusFunc {
			v := numCap(caps, 1)
			return func(n *gamedata.Node, _, _ []Mod) []Mod {
				if t := nodeType(n); t == "Keystone" || t == "ClassStart" {
					return nil
				}
				m := mod("PassiveSkillEffect", "INC", v)
				m.Source = src
				return []Mod{m}
			}
		}},
		{`^melee and melee weapon type modifiers in radius are transformed to bow modifiers$`, bowTransform},
		{`^notable passive skills in radius grant nothing$`, flagFor("PassiveSkillHasNoEffect", "Notable")},
		{`^allocated small passive skills in radius grant nothing$`, flagFor("AllocatedPassiveSkillHasNoEffect", "Normal")},
		{`^allocated notable passive skills in radius grant nothing$`, flagFor("AllocatedPassiveSkillHasNoEffect", "Notable")},
		{`^passive skills in radius also grant (\d+)% increased global critical strike chance$`, grant("", func(c []string) []Mod { return []Mod{mod("CritChance", "INC", numCap(c, 1))} })},
		{`^passive skills in radius also grant \+(\d+) to maximum life$`, grant("", func(c []string) []Mod { return []Mod{mod("Life", "BASE", numCap(c, 1))} })},
		{`^passive skills in radius also grant \+(\d+) to maximum mana$`, grant("", func(c []string) []Mod { return []Mod{mod("Mana", "BASE", numCap(c, 1))} })},
		{`^passive skills in radius also grant (\d+)% increased energy shield$`, grant("", func(c []string) []Mod { return []Mod{mod("EnergyShield", "INC", numCap(c, 1))} })},
		{`^passive skills in radius also grant (\d+)% increased armour$`, grant("", func(c []string) []Mod { return []Mod{mod("Armour", "INC", numCap(c, 1))} })},
		{`^passive skills in radius also grant (\d+)% increased evasion rating$`, grant("", func(c []string) []Mod { return []Mod{mod("Evasion", "INC", numCap(c, 1))} })},
		{`^passive skills in radius also grant \+(\d+) to all attributes$`, grant("", func(c []string) []Mod {
			v := numCap(c, 1)
			return []Mod{mod("Str", "BASE", v), mod("Dex", "BASE", v), mod("Int", "BASE", v), mod("All", "BASE", v)}
		})},
		{`^passive skills in radius also grant \+(\d+)% to chaos resistance$`, grant("", func(c []string) []Mod { return []Mod{mod("ChaosResist", "BASE", numCap(c, 1))} })},
		{`^passive skills in radius also grant (\d+)% increased (\w+) damage$`, grant("", func(c []string) []Mod {
			return []Mod{mod(strings.ToUpper(c[2][:1])+c[2][1:]+"Damage", "INC", numCap(c, 1))}
		})},
		// ModParser.lua:6469-6493: notables in radius grant these mods
		// instead of their own (finishNode, CalcSetup.lua:233-249).
		{
			`^notable passive skills in radius are transformed to instead grant: ` +
				`(\d+)% increased mana cost of skills and (\d+)% increased spell damage$`,
			transformNotable(func(c []string, src string) []Mod {
				cost := mod("ManaCost", "INC", numCap(c, 1))
				cost.Source = src
				dmg := mod("Damage", "INC", numCap(c, 2)) //nolint:mnd // the second capture
				dmg.Flags, dmg.Source = mfSpell, src
				return []Mod{cost, dmg}
			}),
		},
		{
			`^notable passive skills in radius are transformed to instead grant: minions take (\d+)% increased damage$`,
			transformNotable(func(c []string, src string) []Mod {
				taken := mod("DamageTaken", "INC", numCap(c, 1))
				taken.Source = src
				return []Mod{minionModifier(taken)}
			}),
		},
		{
			`^notable passive skills in radius are transformed to instead grant: minions have (\d+)% reduced movement speed$`,
			transformNotable(func(c []string, src string) []Mod {
				speed := mod("MovementSpeed", "INC", -numCap(c, 1))
				speed.Source = src
				return []Mod{minionModifier(speed)}
			}),
		},
	}
	out := make([]radiusPattern, len(defs))
	for i, d := range defs {
		out[i] = radiusPattern{regexp.MustCompile(d.pat), d.make}
	}
	return out
}()

// flagModType is the FLAG mod type.
const flagModType = "FLAG"

// minionModifierName is the MinionModifier mod name.
const minionModifierName = "MinionModifier"

// transformNotable is a "Notable Passive Skills in Radius are Transformed
// to instead grant" entry (ModParser.lua:6469-6493): on a notable it adds
// the PassiveSkillHasOtherEffect flag and one NodeModifier per mod, which
// finishNode puts in place of the node's mods.
func transformNotable(mods func(caps []string, source string) []Mod) func(caps []string, source string) radiusFunc {
	return func(caps []string, src string) radiusFunc {
		return func(n *gamedata.Node, _, _ []Mod) []Mod {
			if nodeType(n) != "Notable" {
				return nil
			}
			out := []Mod{{Name: "PassiveSkillHasOtherEffect", Type: flagModType, Value: true, Source: src}}
			for _, m := range mods(caps, src) {
				out = append(out, Mod{Name: "NodeModifier", Type: modTypeList, Value: modValue(m), Source: src})
			}
			return out
		}
	}
}

// minionModifier is mod("MinionModifier", "LIST", { mod = m }), without a
// source as in ModParser.lua:6482 and 6490.
func minionModifier(m Mod) Mod {
	return Mod{Name: minionModifierName, Type: modTypeList, Value: modValue(m)}
}

// nestedModKey is the key of a LIST value's nested mod ({ mod = ... }).
const nestedModKey = "mod"

// modValue is a LIST value { mod = m }.
func modValue(m Mod) map[string]any { return map[string]any{nestedModKey: nestedMod(m)} }

// nestedMod is modToMap with the mod's source.
func nestedMod(m Mod) map[string]any {
	out := modToMap(m)
	if m.Source != "" {
		out["source"] = m.Source
	}
	return out
}

func flagFor(name, onType string) func(caps []string, source string) radiusFunc {
	return func(_ []string, src string) radiusFunc {
		return func(n *gamedata.Node, _, _ []Mod) []Mod {
			if nodeType(n) != onType {
				return nil
			}
			return []Mod{{Name: name, Type: "FLAG", Value: true, Source: src}}
		}
	}
}

func isRadiusFuncLine(line string) bool {
	l := asciiLowerTrim(line)
	if _, ok := collectPatterns[l]; ok {
		return true
	}
	if _, ok := selfFuncs[l]; ok {
		return true
	}
	for _, p := range radiusPatterns {
		if p.re.MatchString(l) {
			return true
		}
	}
	return false
}

func asciiLowerTrim(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// radiusJewels is CalcSetup.lua's env.radiusJewelList for "Other" functions:
// the functions per node, every node in a radius (the unallocated ones are
// PoB's extraRadiusNodeList), and the finalisers buildModListForNodeList
// runs with node = nil.
type radiusJewels struct {
	byNode   map[int][]radiusFunc
	second   map[int][]secondPassFunc // Self and SelfUnalloc functions
	inRadius map[int]bool
	finals   []func() []Mod
}

// collectUnallocated ports "Grants all bonuses of Unallocated Small/Notable
// Passive Skills in Radius", a jewelSelfUnallocFuncs entry: in the second
// pass of unallocated nodes it keeps the node's final (scaled) mods of that
// node type, without Condition:ConnectedTo*, and the finaliser adds them.
func collectUnallocated(onType string) (secondPassFunc, func() []Mod) {
	var kept []Mod
	f := func(n *gamedata.Node, mods []Mod) {
		if nodeType(n) != onType {
			return
		}
		for _, m := range mods {
			if !strings.HasPrefix(m.Name, "Condition:ConnectedTo") {
				kept = append(kept, m)
			}
		}
	}
	return secondPassFunc{unalloc: true, f: f}, func() []Mod { return kept }
}

var collectPatterns = map[string]string{
	"grants all bonuses of unallocated small passive skills in radius":   "Normal",
	"grants all bonuses of unallocated notable passive skills in radius": "Notable",
}

// radiusFuncs collects the radius functions of the jewels socketed in
// allocated sockets: each jewel's own, then the extra jewel functions that
// apply to it (override.extraJewelFuncs, CalcSetup.lua:919-944), each with
// its own radius.
func (c *Calculator) radiusFuncs(
	spec *pobcode.Spec, b *pobcode.Build, alloc map[int]bool, res *Result, extra []Mod,
) radiusJewels {
	out := radiusJewels{byNode: map[int][]radiusFunc{}, second: map[int][]secondPassFunc{}, inRadius: map[int]bool{}}
	var pos map[int]point
	add := func(socket int, r radius, funcs []radiusFunc, seconds []secondPassFunc) {
		if len(funcs) == 0 && len(seconds) == 0 {
			return
		}
		if pos == nil {
			pos = c.nodePositions()
		}
		out.add(nodesInRadius(c.data.Tree, pos, socket, r), funcs, seconds)
	}
	for _, s := range spec.Sockets {
		if s.ItemID == 0 || !alloc[s.NodeID] {
			continue
		}
		item := b.Item(s.ItemID)
		if item == nil {
			continue
		}
		it, lines := c.itemLines(item, res)
		if it == nil {
			continue
		}
		src := fmt.Sprintf("Tree:%d", s.NodeID)
		if r, ok := radiusByLabel(it.Radius); ok {
			funcs, seconds := ownRadiusFuncs(lines, src, &out)
			add(s.NodeID, r, funcs, seconds)
		}
		for _, m := range extra {
			if r, f, ok := extraJewelFunc(m, it, src); ok {
				add(s.NodeID, r, []radiusFunc{f}, nil)
			}
		}
	}
	return out
}

// add gives the nodes ids the functions.
func (out *radiusJewels) add(ids []int, funcs []radiusFunc, seconds []secondPassFunc) {
	for _, id := range ids {
		if len(funcs) > 0 {
			out.byNode[id] = append(out.byNode[id], funcs...)
		}
		if len(seconds) > 0 {
			out.second[id] = append(out.second[id], seconds...)
		}
		out.inRadius[id] = true
	}
}

// ownRadiusFuncs is a radius jewel's own functions (jewelData.funcList)
// from its mod lines; the finalisers go to out.
func ownRadiusFuncs(lines []string, src string, out *radiusJewels) ([]radiusFunc, []secondPassFunc) {
	var funcs []radiusFunc
	var seconds []secondPassFunc
	for i := 0; i < len(lines); i++ {
		l := asciiLowerTrim(lines[i])
		// Two-line jewel mods are matched together (ItemClass:ParseRaw).
		if !isRadiusFuncLine(l) && i+1 < len(lines) && isRadiusFuncLine(l+" "+asciiLowerTrim(lines[i+1])) {
			l = l + " " + asciiLowerTrim(lines[i+1])
			i++
		}
		for _, p := range radiusPatterns {
			if m := p.re.FindStringSubmatch(l); m != nil {
				funcs = append(funcs, p.make(m, src))
			}
		}
		if t, ok := collectPatterns[l]; ok {
			f, final := collectUnallocated(t)
			seconds = append(seconds, f)
			out.finals = append(out.finals, final)
		}
		if p, ok := selfFuncs[l]; ok {
			f, final := newPerStat(p, src)
			seconds = append(seconds, f)
			out.finals = append(out.finals, final)
		}
	}
	return funcs, seconds
}

// extraJewelFunc is one ExtraJewelFunc entry for a jewel
// (extraJewelFuncs:List({ item = item }, "ExtraJewelFunc")): its radius and
// function, when its tags hold for the item. Its ItemCondition tags with
// itemSlot "{SlotName}" read cfg.item, the jewel (ModStore.lua:691-760).
func extraJewelFunc(m Mod, it *itemtext.Item, src string) (radius, radiusFunc, bool) {
	for _, t := range m.Tags {
		if tagStr(t, "type") != "ItemCondition" {
			return radius{}, nil, false // no other tag occurs on these mods
		}
		if rc := tagStr(t, "rarityCond"); rc != "" {
			match := strings.EqualFold(string(it.Rarity), rc)
			if match == tagBool(t, "neg") {
				return radius{}, nil, false
			}
		}
	}
	v, _ := m.Value.(map[string]any)
	label, _ := v["radius"].(string)
	r, ok := radiusByLabel(label)
	fn, _ := v["func"].(map[string]any)
	if !ok || fn == nil {
		return radius{}, nil, false
	}
	switch name, _ := fn["$jewelFunc"].(string); name {
	case "convertDamageTypes":
		// getSimpleConv({ "PhysicalDamage", ..., "ElementalDamage" }, dst, "INC", true) (ModParser.lua:2658-2665).
		dst, _ := fn["dst"].(string)
		var types []string
		for _, t := range append(slices.Clone(dmgTypeList), "Elemental") {
			types = append(types, t+"Damage")
		}
		conv := simpleConv(types, dst, "INC", true, 0, "")(nil, src)
		return r, conv, true
	case "grantAttribute":
		// out:NewMod(stat, "BASE", val, data.modSource) on small and notable nodes (ModParser.lua:2666-2672).
		stat, _ := fn["stat"].(string)
		val, _ := num(fn["value"])
		return r, func(n *gamedata.Node, _, _ []Mod) []Mod {
			if t := nodeType(n); t != "Notable" && t != "Normal" {
				return nil
			}
			grant := mod(stat, "BASE", val)
			grant.Source = src
			return []Mod{grant}
		}, true
	}
	return radius{}, nil, false
}

// bowTransform is Lioneye's Fall: melee and melee weapon type mods of the
// nodes in radius become bow mods, and Using<melee weapon> conditions
// become UsingBow.
func bowTransform(_ []string, _ string) radiusFunc {
	f := modFlagByName
	mask1 := f["Axe"] | f["Claw"] | f["Dagger"] | f["Mace"] | f["Staff"] | f["Sword"] | uint64(mfMelee)
	mask2 := uint64(mfWeapon1H | mfWeaponMelee)
	mask3 := uint64(mfWeapon2H | mfWeaponMelee)
	using := map[string]bool{"UsingAxe": true, "UsingClaw": true, "UsingDagger": true, "UsingMace": true, "UsingStaff": true, "UsingSword": true, "UsingMeleeWeapon": true}
	return func(_ *gamedata.Node, own, _ []Mod) []Mod {
		var out []Mod
		for _, m := range own {
			v, ok := num(m.Value)
			if !ok {
				continue
			}
			neg := m
			neg.Value = -v
			if m.Flags&mask1 != 0 || m.Flags&mask2 == mask2 || m.Flags&mask3 == mask3 {
				bow := m
				bow.Flags = m.Flags&^(mask1|mask2|mask3) | f["Bow"]
				out = append(out, neg, bow)
				continue
			}
			for i, t := range m.Tags {
				if tagStr(t, "type") == "Condition" && using[tagStr(t, "var")] {
					conv := m
					conv.Tags = append([]map[string]any(nil), m.Tags...)
					nt := map[string]any{}
					for k, x := range t {
						nt[k] = x
					}
					nt["var"] = "UsingBow"
					conv.Tags[i] = nt
					out = append(out, neg, conv)
					break
				}
			}
		}
		return out
	}
}
