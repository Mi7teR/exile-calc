// Package modparser turns mod text into ModDB modifiers, the way PoB's
// Modules/ModParser.lua does. The pattern tables are PoB's own (exported by
// cmd/gamedatagen into data/); this file ports the algorithm: scan() and
// parseMod(). Lua functions in the tables are hooks implemented in Go
// (hooks.go); a line that needs a missing hook is reported as unsupported.
package modparser

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Mod is one ModDB modifier (Modules/ModTools.lua: modLib.createMod).
type Mod struct {
	Name         string
	Type         string // BASE, INC, MORE, FLAG, OVERRIDE, LIST
	Value        any    // float64, bool, string, or a table (map[string]any / []any)
	Flags        uint64 // ModFlag bits
	KeywordFlags uint64 // KeywordFlag bits
	Tags         []map[string]any
}

// Result is the outcome of parsing one line.
type Result struct {
	Mods []Mod
	// Extra is the part of the line that was not understood; empty when the
	// whole line was parsed. A line with Mods == nil was not recognised.
	Extra string
}

// Parsed reports whether the whole line became modifiers.
func (r Result) Parsed() bool { return r.Mods != nil && r.Extra == "" }

// Parser holds the compiled tables; it is safe for concurrent use.
type Parser struct {
	t *tables
	// cache maps a line to its Result. Parsing is a pure function of the
	// line, and the same tree and item texts are parsed on every Calc.
	// Entries are never handed out: Parse returns a deep copy.
	cache sync.Map // string -> Result
}

var (
	defaultOnce sync.Once
	defaultP    *Parser
	defaultErr  error
)

// PoE1 returns the parser for the embedded PoE1 tables.
func PoE1() (*Parser, error) {
	defaultOnce.Do(func() {
		t, err := loadTables(poe1Data)
		defaultP, defaultErr = &Parser{t: t}, err
	})
	return defaultP, defaultErr
}

// Parse parses one mod line (modLib.parseMod). Results are cached per
// line; the returned Result is the caller's own copy and may be modified.
func (p *Parser) Parse(line string) Result {
	if v, ok := p.cache.Load(line); ok {
		r, _ := v.(Result)
		return r.clone()
	}
	mods, extra := p.parseRaw(line)
	r := Result{Mods: decodeMods(mods), Extra: extra}
	p.cache.Store(line, r)
	return r.clone()
}

// clone deep-copies the mods: the slice, each Tags slice and map, and
// table values. Nil stays nil, so Parsed() is unchanged.
func (r Result) clone() Result {
	if r.Mods == nil {
		return r
	}
	mods := make([]Mod, len(r.Mods))
	for i, m := range r.Mods {
		m.Value = deepCopy(m.Value)
		if m.Tags != nil {
			tags := make([]map[string]any, len(m.Tags))
			for j, t := range m.Tags {
				tags[j], _ = deepCopy(t).(map[string]any)
			}
			m.Tags = tags
		}
		mods[i] = m
	}
	return Result{Mods: mods, Extra: r.Extra}
}

// parseRaw returns mods in the generic encoding used by the data and the
// tests: nil = not recognised; extra = leftover text ("" = none).
func (p *Parser) parseRaw(line string) ([]any, string) {
	mods, extra, _ := p.parseTracked(line)
	return mods, extra
}

// missInfo records the first unported hook a line needed.
type missInfo struct {
	hit  bool
	name string
}

func (m *missInfo) set(v any) {
	m.hit = true
	if mm, ok := v.(map[string]any); ok && m.name == "" {
		m.name, _ = mm["$hook"].(string)
	}
}

// parseTracked is parseRaw that also returns the missing hook, if any.
func (p *Parser) parseTracked(line string) ([]any, string, string) {
	miss := &missInfo{}
	mods, extra := p.parseOrdered(line, miss)
	// A missing hook means PoB would have parsed the line: do not retry with
	// rewritten wording, which could parse it differently.
	if !miss.hit && (mods == nil || extra != "") {
		// ModParser.lua: 3.30 replaced "nearby" with distances; retry with the old wording.
		lower := asciiLower(line)
		if withinMetre.MatchString(lower) {
			for _, rw := range nearbyRewrites {
				lower = rw.re.ReplaceAllString(lower, rw.repl)
			}
			if m, e := p.parseOrdered(lower, miss); m != nil && e == "" {
				return m, "", ""
			}
		}
	}
	return mods, extra, miss.name
}

var withinMetre = regexp.MustCompile(`within [0-9.]+ metre`)

var nearbyRewrites = func() []struct {
	re   *regexp.Regexp
	repl string
} {
	src := [][2]string{
		{`^enemy monsters within [%d%.]+ metres? have `, "nearby enemy monsters' "},
		{`while at least (%d+) enemies within [%d%.]+ metres?`, "while at least ${1} enemies are nearby"},
		{`while you have an ally within [%d%.]+ metres?`, "while you have at least 1 nearby ally"},
		{`when you or allies within [%d%.]+ metres?`, "when you or a nearby ally"},
		{`(rare or unique enem%a+) within [%d%.]+ metres?`, "${1} nearby"},
		{`(applies .- to enemies) within [%d%.]+ metres?`, "${1} near it"},
		{`(%a+) within [%d%.]+ metres?`, "nearby ${1}"},
	}
	out := make([]struct {
		re   *regexp.Regexp
		repl string
	}, len(src))
	for i, s := range src {
		re, err := translateLuaPattern(s[0])
		if err != nil {
			panic(err)
		}
		out[i].re, out[i].repl = re, s[1]
	}
	return out
}()

// parseModOrdered
func (p *Parser) parseOrdered(line string, miss *missInfo) ([]any, string) {
	mods, extra := p.parseMod(line, 1, miss)
	if mods != nil && extra != "" {
		mods, extra = p.parseMod(line, 2, miss)
	}
	return mods, extra
}

// asciiLower lowercases like Lua's string.lower in the C locale, keeping
// byte offsets valid for slicing the original line.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// scan ports ModParser.lua scan(): the earliest match wins, then the longest
// match, then the longest pattern. It returns the value, the line without
// the matched text and the captures.
func (p *Parser) scan(line, table string) (any, string, []string, bool) {
	lower := asciiLower(line)
	bestStart, bestEnd := -1, -1
	var best *entry
	var bestCaps []string
	for i := range p.t.t[table] {
		e := &p.t.t[table][i]
		var start, end int
		var caps []string
		if e.re == nil {
			start = strings.Index(lower, e.pattern)
			if start < 0 {
				continue
			}
			end = start + len(e.pattern)
		} else {
			m := e.re.FindStringSubmatchIndex(lower)
			if m == nil {
				continue
			}
			start, end = m[0], m[1]
			for j := 2; j+1 < len(m); j += 2 {
				if m[j] >= 0 {
					caps = append(caps, lower[m[j]:m[j+1]])
				} else {
					caps = append(caps, "")
				}
			}
		}
		if best == nil || start < bestStart || (start == bestStart && (end > bestEnd || (end == bestEnd && len(e.pattern) > len(best.pattern)))) {
			best, bestStart, bestEnd, bestCaps = e, start, end, caps
		}
	}
	if best == nil {
		return nil, line, nil, false
	}
	return best.value, line[:bestStart] + line[bestEnd:], bestCaps, true
}

func luaNumber(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f, err == nil
}

// numOrString is tonumber(x) or x.
func numOrString(s string) any {
	if f, ok := luaNumber(s); ok {
		return f
	}
	return s
}

func capArgs(caps []string) []any {
	out := make([]any, len(caps))
	for i, c := range caps {
		out[i] = c
	}
	return out
}

var hasDigit = regexp.MustCompile(`\d+`)

// parseMod ports ModParser.lua parseMod(line, order).
func (p *Parser) parseMod(line string, order int, miss *missInfo) ([]any, string) {
	lower := asciiLower(line)
	// jewelFuncList (radius jewel functions) is code in ModParser.lua and is
	// not exported yet.
	if v, ok := p.t.exact["clusterJewelSkills"][lower]; ok {
		return asList(deepCopy(v)), ""
	}
	if v, ok := p.t.exact["unsupportedModList"][lower]; ok && v != nil {
		return []any{}, line
	}
	if special, rest, caps, ok := p.scan(line, "specialModList"); ok && rest == "" {
		n, _ := luaNumber(first(caps))
		args := append([]any{nilIfNaN(n, first(caps))}, capArgs(caps)...)
		out, ok := p.call(special, args)
		if !ok {
			miss.set(special)
		}
		// A Lua function that returns nil leaves the line unparsed.
		if !ok || out == nil || unresolved(out) {
			return nil, line
		}
		return asList(deepCopy(out)), ""
	}
	if m := addToCluster.FindStringSubmatch(line); m != nil {
		return []any{newMod("AddToClusterJewelNode", "LIST", m[1], 0, 0)}, ""
	}

	line += " "

	preFlag, rest, caps, _ := p.scan(line, "preFlagList")
	line = rest
	if isFunc(preFlag) {
		out, ok := p.call(preFlag, capArgs(caps))
		if !ok {
			miss.set(preFlag)
			return nil, line
		}
		preFlag = out
	}

	// Skill name at the start of the line.
	skillTag, line, _, _ := p.scan(line, "preSkillNameList")

	modForm, rest, formCaps, ok := p.scan(line, "formList")
	line = rest
	if !ok {
		return nil, line
	}
	form, _ := modForm.(string)

	var modTag, modTag2 any
	if v, rest, tagCaps, ok := p.scan(line, "modTagList"); ok {
		line = rest
		if modTag, ok = p.tagValue(v, tagCaps); !ok {
			miss.set(v)
			return nil, line
		}
		if v2, rest2, tagCaps2, ok2 := p.scan(line, "modTagList"); ok2 {
			line = rest2
			if modTag2, ok = p.tagValue(v2, tagCaps2); !ok {
				miss.set(v2)
				return nil, line
			}
		}
	}

	var modName any
	if order == 2 && skillTag == nil {
		skillTag, line, _, _ = p.scan(line, "skillNameList")
	}
	switch form {
	case "PEN", "BASECOST", "TOTALCOST":
		table := map[string]string{"PEN": "penTypes", "BASECOST": "baseCostTypes", "TOTALCOST": "costTypes"}[form]
		modName, line, _, ok = p.scan(line, table)
		if !ok {
			return []any{}, line
		}
		_, line, _, _ = p.scan(line, "modNameList")
	case "FLAG":
		var fv any
		fv, line, _, ok = p.scan(line, "flagTypes")
		if !ok {
			return nil, line
		}
		// modNameList is scanned (it removes text) but the name comes from flagTypes.
		_, line, _, _ = p.scan(line, "modNameList")
		return p.finishFlag(fv, line, order, preFlag, modTag, modTag2, skillTag)
	default:
		modName, line, _, _ = p.scan(line, "modNameList")
	}
	if order == 1 && skillTag == nil {
		skillTag, line, _, _ = p.scan(line, "skillNameList")
	}

	var modFlag any
	modFlag, line, _, _ = p.scan(line, "modFlagList")

	var modValue any = numOrString(first(formCaps))
	modType := any("BASE")
	var modSuffix any
	var modExtraTags any
	neg := func() {
		if f, ok := modValue.(float64); ok {
			modValue = -f
		}
	}
	scanSuffix := func() { modSuffix, line, _, _ = p.scan(line, "suffixTypes") }
	switch form {
	case "INC":
		modType = "INC"
	case "RED":
		neg()
		modType = "INC"
	case "MORE":
		modType = "MORE"
	case "LESS":
		neg()
		modType = "MORE"
	case "BASE", "GAIN":
		scanSuffix()
	case "LOSE":
		neg()
		scanSuffix()
	case "GRANTS":
		modExtraTags = map[string]any{"tag": map[string]any{"type": "Condition", "var": "{Hand}Attack"}}
		scanSuffix()
	case "GRANTS_GLOBAL":
		scanSuffix()
	case "REMOVES":
		neg()
		modExtraTags = map[string]any{"tag": map[string]any{"type": "Condition", "var": "{Hand}Attack"}}
		scanSuffix()
	case "CHANCE":
	case "REGENPERCENT", "REGENFLAT", "DEGENPERCENT", "DEGENFLAT":
		table := "regenTypes"
		if strings.HasPrefix(form, "DEGEN") {
			table = "degenTypes"
		}
		modName = p.t.exact[table][capAt(formCaps, 2)]
		if strings.HasSuffix(form, "PERCENT") {
			modSuffix = "Percent"
		}
	case "DEGEN":
		dt, ok := p.t.exact["dmgTypes"][capAt(formCaps, 2)].(string)
		if !ok {
			return []any{}, line
		}
		modName, modSuffix = dt+"Degen", ""
	case "DMG", "DMGATTACKS", "DMGSPELLS", "DMGBOTH":
		dt, ok := p.t.exact["dmgTypes"][capAt(formCaps, 3)].(string)
		if !ok {
			return []any{}, line
		}
		lo, _ := luaNumber(capAt(formCaps, 1))
		hi, _ := luaNumber(capAt(formCaps, 2))
		modValue = []any{lo, hi}
		modName = []any{dt + "Min", dt + "Max"}
		if modFlag == nil {
			switch form {
			case "DMGATTACKS":
				modFlag = map[string]any{"keywordFlags": float64(kwAttack)}
			case "DMGSPELLS":
				modFlag = map[string]any{"keywordFlags": float64(kwSpell)}
			case "DMGBOTH":
				modFlag = map[string]any{"keywordFlags": float64(kwAttack | kwSpell)}
			}
		}
	case "OVERRIDE":
		modType = "OVERRIDE"
	case "DOUBLED":
		var nameString string
		switch n := modName.(type) {
		case []any:
			nameString, _ = n[0].(string)
			names := append([]any(nil), n...)
			if len(names) < 2 {
				names = append(names, nil)
			}
			names[1] = "Multiplier:" + nameString + "Doubled"
			modName = names
		case map[string]any:
			l := listOf(n)
			if len(l) > 0 {
				nameString, _ = l[0].(string)
				names := append([]any(nil), l...)
				if len(names) < 2 {
					names = append(names, nil)
				}
				names[1] = "Multiplier:" + nameString + "Doubled"
				c := deepCopy(n).(map[string]any)
				c["@list"] = names
				modName = c
			}
		case string:
			nameString = n
			modName = []any{n, "Multiplier:" + n + "Doubled"}
		}
		if modName != nil {
			modType = []any{"MORE", "OVERRIDE"}
			modValue = []any{100.0, 1.0}
			modExtraTags = map[string]any{
				"tag": true,
				"@list": []any{map[string]any{"tag": map[string]any{
					"type": "Multiplier", "var": nameString + "Doubled", "globalLimit": 100.0, "globalLimitKey": nameString + "DoubledLimit",
				}}},
			}
		}
	}
	if modName == nil {
		return []any{}, line
	}
	return p.assemble(modName, modType, modValue, modSuffix, line, preFlag, modFlag, modTag, modTag2, skillTag, modExtraTags)
}

// finishFlag handles the FLAG form: the value scanned from flagTypes is a
// name or a {name, type, value} table.
func (p *Parser) finishFlag(fv any, line string, order int, preFlag, modTag, modTag2, skillTag any) ([]any, string) {
	modName, modType, modValue := fv, any("FLAG"), any(true)
	if m, ok := fv.(map[string]any); ok {
		modName, modType, modValue = m["name"], m["type"], m["value"]
		if modType == nil {
			modType = "FLAG"
		}
		if modValue == nil {
			modValue = true
		}
	}
	if order == 1 && skillTag == nil {
		skillTag, line, _, _ = p.scan(line, "skillNameList")
	}
	var modFlag any
	modFlag, line, _, _ = p.scan(line, "modFlagList")
	if modName == nil {
		return []any{}, line
	}
	return p.assemble(modName, modType, modValue, nil, line, preFlag, modFlag, modTag, modTag2, skillTag, nil)
}

func (p *Parser) tagValue(v any, caps []string) (any, bool) {
	if !isFunc(v) {
		return v, true
	}
	// ModParser.lua: tonumber(cap1) if it has digits, else the string.
	var firstArg any = first(caps)
	if hasDigit.MatchString(first(caps)) {
		n, _ := luaNumber(first(caps))
		firstArg = n
	}
	return p.call(v, append([]any{firstArg}, capArgs(caps)...))
}

func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func capAt(s []string, i int) string {
	if i-1 < len(s) {
		return s[i-1]
	}
	return ""
}

func nilIfNaN(n float64, s string) any {
	if _, ok := luaNumber(s); ok {
		return n
	}
	return nil
}

var addToCluster = regexp.MustCompile(`^Added Small Passive Skills also grant: (.+)$`)

// KeywordFlag bits used by the DMG forms (Data/Global.lua).
const (
	kwAttack = 0x00010000
	kwSpell  = 0x00020000
)

func newMod(name, typ string, value any, flags, kw uint64) map[string]any {
	return map[string]any{"name": name, "type": typ, "value": value, "flags": float64(flags), "keywordFlags": float64(kw)}
}

// listOf returns the positional entries of a decoded table.
func listOf(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case map[string]any:
		l, _ := x["@list"].([]any)
		return l
	}
	return nil
}

func asList(v any) []any {
	if l := listOf(v); l != nil {
		return l
	}
	return []any{}
}

func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = deepCopy(e)
		}
		return m
	case []any:
		l := make([]any, len(x))
		for i, e := range x {
			l[i] = deepCopy(e)
		}
		return l
	}
	return v
}

func num(v any) uint64 {
	f, _ := v.(float64)
	return uint64(f)
}

// assemble ports the end of parseMod: combine flags and tags, build one mod
// per name, then apply the wrappers (auras, minions, skills, enemies).
func (p *Parser) assemble(modName, modType, modValue, modSuffix any, line string, parts ...any) ([]any, string) {
	var flags, kw uint64
	var tagList []any
	var modTagList [][]any
	misc := map[string]any{}
	for _, data := range append([]any{modName}, parts...) {
		d, ok := data.(map[string]any)
		if !ok {
			continue
		}
		flags |= num(d["flags"])
		kw |= num(d["keywordFlags"])
		entries := listOf(d)
		if d["tag"] != nil {
			if len(entries) > 0 && isMap(entries[0]) && entries[0].(map[string]any)["tag"] != nil {
				modTagList = make([][]any, len(entries))
				for i, e := range entries {
					if t := e.(map[string]any)["tag"]; t != nil {
						modTagList[i] = append(modTagList[i], deepCopy(t))
					}
				}
			} else {
				tagList = append(tagList, deepCopy(d["tag"]))
			}
		} else if tl := listOf(d["tagList"]); tl != nil {
			if len(entries) > 0 && isMap(entries[0]) && entries[0].(map[string]any)["tagList"] != nil {
				modTagList = make([][]any, len(entries))
				for i, e := range entries {
					for _, t := range listOf(e.(map[string]any)["tagList"]) {
						modTagList[i] = append(modTagList[i], deepCopy(t))
					}
				}
			} else {
				for _, t := range tl {
					tagList = append(tagList, deepCopy(t))
				}
			}
		}
		for k, v := range d {
			if k != "@list" {
				misc[k] = v
			}
		}
	}

	suffix := ""
	if s, ok := modSuffix.(string); ok {
		suffix = s
	} else if s, ok := misc["modSuffix"].(string); ok {
		suffix = s
	}
	var names []any
	switch n := modName.(type) {
	case string:
		names = []any{n}
	default:
		names = listOf(n)
	}
	var mods []any
	for i, n := range names {
		name, _ := n.(string)
		m := newMod(name+suffix, "", pick(modType, i), flags, kw)
		m["type"] = pick(modType, i)
		m["value"] = pick(modValue, i)
		var tags []any
		tags = append(tags, deepCopyList(tagList)...)
		if modTagList != nil && i < len(modTagList) {
			tags = append(tags, modTagList[i]...)
		}
		if len(tags) > 0 {
			m["@list"] = tags
		}
		mods = append(mods, m)
	}
	if len(mods) > 0 {
		misc["$skillTypeBanner"] = p.t.exact["SkillType"]["Banner"]
		mods = wrap(mods, misc)
	}
	if unresolved(mods) {
		return nil, line
	}
	if strings.TrimSpace(line) == "" {
		return mods, ""
	}
	return mods, line
}

func deepCopyList(l []any) []any {
	out := make([]any, len(l))
	for i, v := range l {
		out[i] = deepCopy(v)
	}
	return out
}

func isMap(v any) bool { _, ok := v.(map[string]any); return ok }

func pick(v any, i int) any {
	if l, ok := v.([]any); ok {
		if i < len(l) {
			return l[i]
		}
		return nil
	}
	return v
}

func withTags(m map[string]any, tags ...any) map[string]any {
	var l []any
	for _, t := range tags {
		if t != nil {
			l = append(l, t)
		}
	}
	if len(l) > 0 {
		m["@list"] = l
	}
	return m
}

// wrap ports the "Special handling for various modifier types" block.
func wrap(mods []any, misc map[string]any) []any {
	truthy := func(k string) bool { v, ok := misc[k]; return ok && v != nil && v != false }
	for i, em := range mods {
		effect := em.(map[string]any)
		switch {
		case truthy("addToAura"):
			if truthy("onlyAddToBanners") {
				mods[i] = withTags(newMod("ExtraAuraEffect", "LIST", map[string]any{"mod": effect}, 0, 0),
					map[string]any{"type": "SkillType", "skillType": misc["$skillTypeBanner"]})
			} else {
				mods[i] = newMod("ExtraAuraEffect", "LIST", map[string]any{"mod": effect}, 0, 0)
			}
		case truthy("newAura"):
			tags := listOf(effect)
			delete(effect, "@list")
			val := map[string]any{"mod": effect}
			if v, ok := misc["newAuraOnlyAllies"]; ok && v != nil {
				val["onlyAllies"] = v
			}
			mods[i] = withTags(newMod("ExtraAura", "LIST", val, 0, 0), tags...)
		case truthy("addToMinion"):
			var tags []any
			tags = append(tags, misc["playerTag"], misc["addToMinionTag"])
			tags = append(tags, listOf(misc["playerTagList"])...)
			mods[i] = withTags(newMod("MinionModifier", "LIST", map[string]any{"mod": effect}, 0, 0), tags...)
		case truthy("addToSkill"):
			mods[i] = withTags(newMod("ExtraSkillMod", "LIST", map[string]any{"mod": effect}, 0, 0), deepCopy(misc["addToSkill"]))
		case truthy("applyToEnemy"):
			var tags []any
			tags = append(tags, misc["playerTag"])
			tags = append(tags, listOf(misc["playerTagList"])...)
			newMod2 := any(effect)
			if l := listOf(effect); len(l) > 0 && truthy("actorEnemy") {
				c := deepCopy(effect).(map[string]any)
				if t, ok := listOf(c)[0].(map[string]any); ok {
					t["actor"] = "enemy"
				}
				newMod2 = c
			}
			mods[i] = withTags(newMod("EnemyModifier", "LIST", map[string]any{"mod": newMod2}, 0, 0), tags...)
		}
	}
	return mods
}

// decodeMods converts the generic encoding to Mod values.
func decodeMods(l []any) []Mod {
	if l == nil {
		return nil
	}
	out := make([]Mod, 0, len(l))
	for _, v := range l {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		mod := Mod{Value: m["value"], Flags: num(m["flags"]), KeywordFlags: num(m["keywordFlags"])}
		mod.Name, _ = m["name"].(string)
		mod.Type, _ = m["type"].(string)
		for _, t := range listOf(m) {
			if tm, ok := t.(map[string]any); ok {
				mod.Tags = append(mod.Tags, tm)
			}
		}
		out = append(out, mod)
	}
	return out
}

// Table returns one of ModParser.lua's or Data.lua's exact-lookup tables
// (itemTagSpecial, conquerorList, ...), or nil. Callers must not modify it.
func (p *Parser) Table(name string) map[string]any { return p.t.exact[name] }
