package engine

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// partyExports is calcs.perform's buffExports (CalcPerform.lua:2241), the
// buffs the build gives a party member through the Party tab, filled while
// partyTabEnableExportBuffs (the Party tab exports and env.mode is not
// CALCULATOR, CalcPerform.lua:1314). Only the entries of the minions'
// skills are ported (CalcPerform.lua:2846-2851, 2923-2933, 3001-3005);
// the player's own Aura, Curse, Warcry and Link entries, extraAura,
// EnemyMods, EnemyConditions, PlayerMods and the export post-processing
// (3850-3967) are not.
type partyExports struct {
	aura  map[string]*partyExport // buffExports.Aura, but otherEffects
	other map[string]*partyExport // buffExports.Aura.otherEffects
}

// partyExport is one buffExports entry: its effect multiplier and mods.
type partyExport struct {
	effectMult float64
	mods       []Mod
}

func newPartyExports() *partyExports {
	return &partyExports{aura: map[string]*partyExport{}, other: map[string]*partyExport{}}
}

// setAura is a minion Aura's buffExports["Aura"][name] (CalcPerform.lua
// 2930-2933): an earlier entry of that name moves to name_Debuff.
func (e *partyExports) setAura(name string, x *partyExport) {
	if old, ok := e.aura[name]; ok {
		e.aura[name+"_Debuff"] = old
	}
	e.aura[name] = x
}

// setAuraDebuff is a minion AuraDebuff's buffExports["Aura"][name ..
// (buffExports["Aura"][name] and "_Debuff" or "")] (CalcPerform.lua:3002).
func (e *partyExports) setAuraDebuff(name string, x *partyExport) {
	if _, ok := e.aura[name]; ok {
		name += "_Debuff"
	}
	e.aura[name] = x
}

// The Party tab's list names (PartyTab.lua exportBuffs).
const (
	partyAura         = "Aura"
	partyOtherEffects = "otherEffects"
)

// text is PartyTabClass:exportBuffs (Classes/PartyTab.lua:996-1043): the
// Party tab text of one buff type. Only "Aura" has entries here.
func (e *partyExports) text(buffType string) string {
	if buffType != partyAura {
		return ""
	}
	names := make([]string, 0, len(e.aura)+1)
	for n := range e.aura {
		names = append(names, n)
	}
	if len(e.other) > 0 {
		names = append(names, partyOtherEffects)
	}
	sort.Strings(names)
	var buf strings.Builder
	for _, n := range names {
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(n)
		if n == partyOtherEffects {
			e.writeOther(&buf)
		} else {
			x := e.aura[n]
			buf.WriteString("\n" + luaToString(x.effectMult*100)) //nolint:mnd // percent
			for _, m := range x.mods {
				buf.WriteString("\n" + formatSourceMod(m))
			}
		}
		buf.WriteString("\n---")
	}
	return buf.String()
}

// writeOther writes the otherEffects block's lines: "buff|effect|mod".
func (e *partyExports) writeOther(buf *strings.Builder) {
	inner := make([]string, 0, len(e.other))
	for k := range e.other {
		inner = append(inner, k)
	}
	sort.Strings(inner)
	for _, k := range inner {
		effect := luaToString(e.other[k].effectMult * 100) //nolint:mnd // percent
		for _, m := range e.other[k].mods {
			buf.WriteString("\n" + k + "|" + effect + "|" + formatSourceMod(m))
		}
	}
}

// luaToString is Lua's tostring for the values of mods and tags (LuaJIT
// formats numbers with "%.14g").
func luaToString(v any) string {
	switch x := v.(type) {
	case float64:
		switch {
		case math.IsInf(x, 1):
			return "inf"
		case math.IsInf(x, -1):
			return "-inf"
		case math.IsNaN(x):
			return "nan"
		}
		return strconv.FormatFloat(x, 'g', 14, 64)
	case int:
		return strconv.Itoa(x)
	case bool:
		return strconv.FormatBool(x)
	case string:
		return x
	case nil:
		return "nil"
	}
	return ""
}

// formatSourceMod is modLib.formatSourceMod (Modules/ModTools.lua:235):
// "value|source|name|type|flags|keywordFlags|tags".
func formatSourceMod(m Mod) string {
	return formatValue(m.Value) + "|" + m.Source + "|" + partyModParams(m)
}

// partyModParams is modLib.formatModParams (ModTools.lua:227) as the Party
// tab writes it.
func partyModParams(m Mod) string {
	return m.Name + "|" + m.Type + "|" + formatFlags(m.Flags, modFlagNames) + "|" +
		formatFlags(m.KeywordFlags, keywordFlagNames) + "|" + formatTags(m.Tags)
}

// formatFlags is modLib.formatFlags (ModTools.lua:136): the sorted names of
// the flags fully set, or "-".
func formatFlags(flags uint64, names map[string]uint64) string {
	var set []string
	for n, v := range names {
		if flags&v == v {
			set = append(set, n)
		}
	}
	if len(set) == 0 {
		return "-"
	}
	sort.Strings(set)
	return strings.Join(set, ",")
}

// formatTags is modLib.formatTags (ModTools.lua:188).
func formatTags(tags []map[string]any) string {
	if len(tags) == 0 {
		return "-"
	}
	parts := make([]string, len(tags))
	for i, t := range tags {
		parts[i] = formatTag(t)
	}
	return strings.Join(parts, ",")
}

// sortedParams is the parameter order of modLib.formatTag and formatValue:
// "type" first, then the other keys sorted.
func sortedParams(m map[string]any) []string {
	var names []string
	_, haveType := m["type"]
	for k := range m {
		if k != "type" { //nolint:goconst // the tag key
			names = append(names, k)
		}
	}
	sort.Strings(names)
	if haveType {
		names = append([]string{"type"}, names...)
	}
	return names
}

// formatTag is modLib.formatTag (ModTools.lua:151).
func formatTag(tag map[string]any) string {
	names := sortedParams(tag)
	parts := make([]string, len(names))
	for i, n := range names {
		val := luaToString(tag[n])
		switch x := tag[n].(type) {
		case []any:
			var tags []map[string]any
			var items []string
			for _, e := range x {
				if t, ok := e.(map[string]any); ok {
					tags = append(tags, t)
				} else {
					items = append(items, luaToString(e))
				}
			}
			if len(tags) > 0 {
				val = "{" + formatTags(tags) + "}"
			} else {
				val = "{" + strings.Join(items, ",") + "}"
			}
		case map[string]any:
			val = "{" + formatTag(x) + "}"
		}
		parts[i] = n + "=" + val
	}
	return strings.Join(parts, "/")
}

// formatValue is modLib.formatValue (ModTools.lua:196).
func formatValue(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return luaToString(v)
	}
	names := sortedParams(m)
	parts := make([]string, len(names))
	for i, n := range names {
		if inner, isMod := m[n].(map[string]any); isMod && n == "mod" { //nolint:goconst // the value key
			src, _ := inner["source"].(string)
			im := modFromMap(inner, src)
			parts[i] = n + "=[" + formatValue(im.Value) + " = " + partyModParams(im) + "]"
			continue
		}
		parts[i] = n + "=" + formatValue(m[n])
	}
	return "{" + strings.Join(parts, "/") + "}"
}
