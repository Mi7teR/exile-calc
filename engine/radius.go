package engine

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// radiusFunc is a jewelOtherFuncs entry (Modules/ModParser.lua): given a
// node in the jewel's radius and its own mods, it returns mods to add.
type radiusFunc func(node *gamedata.Node, own []Mod) []Mod

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
		return func(n *gamedata.Node, _ []Mod) []Mod {
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

// simpleConv ports getSimpleConv: the node's own src mods are removed and
// re-added as dst (times factor, floored).
func simpleConv(src []string, dst, typ string, factor float64) func(caps []string, source string) radiusFunc {
	return func(_ []string, _ string) radiusFunc {
		return func(n *gamedata.Node, own []Mod) []Mod {
			var out []Mod
			for _, s := range src {
				for _, m := range own {
					v, ok := num(m.Value)
					if m.Name != s || m.Type != typ || !ok {
						continue
					}
					neg, conv := m, m
					neg.Value = -v
					conv.Name = dst
					if factor != 1 {
						conv.Value = math.Floor(v * factor)
					}
					out = append(out, neg, conv)
				}
			}
			return out
		}
	}
}

var radiusPatterns = func() []radiusPattern {
	type def struct {
		pat  string
		make func(caps []string, source string) radiusFunc
	}
	defs := []def{
		{`^strength from passives in radius is transformed to dexterity$`, simpleConv([]string{"Str"}, "Dex", "BASE", 1)},
		{`^dexterity from passives in radius is transformed to strength$`, simpleConv([]string{"Dex"}, "Str", "BASE", 1)},
		{`^strength from passives in radius is transformed to intelligence$`, simpleConv([]string{"Str"}, "Int", "BASE", 1)},
		{`^intelligence from passives in radius is transformed to strength$`, simpleConv([]string{"Int"}, "Str", "BASE", 1)},
		{`^dexterity from passives in radius is transformed to intelligence$`, simpleConv([]string{"Dex"}, "Int", "BASE", 1)},
		{`^intelligence from passives in radius is transformed to dexterity$`, simpleConv([]string{"Int"}, "Dex", "BASE", 1)},
		{`^increases and reductions to life in radius are transformed to apply to energy shield$`, simpleConv([]string{"Life"}, "EnergyShield", "INC", 1)},
		{`^increases and reductions to evasion rating in radius are transformed to apply to armour$`, simpleConv([]string{"Evasion"}, "Armour", "INC", 1)},
		{`^(\d+)% increased effect of non-keystone passive skills in radius$`, func(caps []string, src string) radiusFunc {
			v := numCap(caps, 1)
			return func(n *gamedata.Node, _ []Mod) []Mod {
				if t := nodeType(n); t == "Keystone" || t == "ClassStart" {
					return nil
				}
				m := mod("PassiveSkillEffect", "INC", v)
				m.Source = src
				return []Mod{m}
			}
		}},
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
	}
	out := make([]radiusPattern, len(defs))
	for i, d := range defs {
		out[i] = radiusPattern{regexp.MustCompile(d.pat), d.make}
	}
	return out
}()

func flagFor(name, onType string) func(caps []string, source string) radiusFunc {
	return func(_ []string, src string) radiusFunc {
		return func(n *gamedata.Node, _ []Mod) []Mod {
			if nodeType(n) != onType {
				return nil
			}
			return []Mod{{Name: name, Type: "FLAG", Value: true, Source: src}}
		}
	}
}

func isRadiusFuncLine(line string) bool {
	l := asciiLowerTrim(line)
	for _, p := range radiusPatterns {
		if p.re.MatchString(l) {
			return true
		}
	}
	return false
}

func asciiLowerTrim(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// radiusFuncs collects, per tree node, the radius functions of the jewels
// socketed in allocated sockets (CalcSetup.lua env.radiusJewelList, type "Other").
func (c *Calculator) radiusFuncs(spec *pobcode.Spec, b *pobcode.Build, alloc map[int]bool, res *Result) map[int][]radiusFunc {
	out := map[int][]radiusFunc{}
	var pos map[int]point
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
		r, ok := radiusByLabel(it.Radius)
		if !ok {
			continue
		}
		src := fmt.Sprintf("Tree:%d", s.NodeID)
		var funcs []radiusFunc
		for _, line := range lines {
			l := asciiLowerTrim(line)
			for _, p := range radiusPatterns {
				if m := p.re.FindStringSubmatch(l); m != nil {
					funcs = append(funcs, p.make(m, src))
				}
			}
		}
		if len(funcs) == 0 {
			continue
		}
		if pos == nil {
			pos = nodePositions(c.data.Tree)
		}
		for _, id := range nodesInRadius(c.data.Tree, pos, s.NodeID, r) {
			out[id] = append(out[id], funcs...)
		}
	}
	return out
}
