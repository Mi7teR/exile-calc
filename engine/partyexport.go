package engine

import (
	"maps"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// partyExports is calcs.perform's buffExports (CalcPerform.lua:2241), the
// buffs the build gives a party member through the Party tab, filled while
// partyTabEnableExportBuffs (the Party tab exports and env.mode is not
// CALCULATOR, CalcPerform.lua:1314), then the enemy and player stats
// (3848-3967).
type partyExports struct {
	aura   map[string]*partyExport // buffExports.Aura, but otherEffects and extraAura
	other  map[string]*partyExport // buffExports.Aura.otherEffects
	curse  map[string]*partyExport // buffExports.Curse
	warcry map[string]*partyExport // buffExports.Warcry
	link   map[string]*partyExport // buffExports.Link
	// extraAura is buffExports.Aura.extraAura's mod list (3452-3469).
	extraAura  []Mod
	curseLimit float64 // buffExports.CurseLimit (3252)
	// enemyMods is buffExports.EnemyMods: one mod, or several (MultiStat).
	enemyMods  map[string][]Mod
	enemyConds map[string]bool // buffExports.EnemyConditions
	playerMods map[string]bool // buffExports.PlayerMods
}

// partyExport is one buffExports entry: its effect multiplier and mods.
type partyExport struct {
	effectMult float64
	isMark     bool
	mods       []Mod
}

func newPartyExports() *partyExports {
	return &partyExports{aura: map[string]*partyExport{}, other: map[string]*partyExport{},
		curse: map[string]*partyExport{}, warcry: map[string]*partyExport{}, link: map[string]*partyExport{},
		enemyMods: map[string][]Mod{}, enemyConds: map[string]bool{}, playerMods: map[string]bool{}}
}

// setAura is an Aura's buffExports["Aura"][name] (CalcPerform.lua
// 2533-2536, 2930-2933): an earlier entry of that name moves to
// name_Debuff.
func (e *partyExports) setAura(name string, x *partyExport) {
	if old, ok := e.aura[name]; ok {
		e.aura[name+"_Debuff"] = old
	}
	e.aura[name] = x
}

// setAuraDebuff is buffExports["Aura"][name .. (buffExports["Aura"][name]
// and "_Debuff" or "")] (CalcPerform.lua:2546, 2612, 2669, 3002).
func (e *partyExports) setAuraDebuff(name string, x *partyExport) {
	if _, ok := e.aura[name]; ok {
		name += "_Debuff"
	}
	e.aura[name] = x
}

// The Party tab's list names (PartyTab.lua exportBuffs).
const (
	partyAura            = "Aura"
	partyCurse           = "Curse"
	partyWarcry          = "Warcry"
	partyLink            = "Link"
	partyEnemyMods       = "EnemyMods"
	partyEnemyConditions = "EnemyConditions"
	partyPlayerMods      = "PlayerMods"
	partyOtherEffects    = "otherEffects"
	partyExtraAura       = "extraAura"
)

// text is PartyTabClass:exportBuffs (Classes/PartyTab.lua:996-1043): the
// Party tab text of one buff type: "PlayerMods", "Aura", "Curse",
// "Warcry", "Link", "EnemyConditions" or "EnemyMods".
//
//nolint:gocognit,cyclop,funlen // one branch per buff type, as exportBuffs
func (e *partyExports) text(buffType string) string {
	var buf strings.Builder
	var names []string
	switch buffType {
	case partyAura:
		for n := range e.aura {
			names = append(names, n)
		}
		if len(e.other) > 0 {
			names = append(names, partyOtherEffects)
		}
		names = append(names, partyExtraAura)
	case partyCurse:
		buf.WriteString("--- Curse Limit ---\n" + luaToString(e.curseLimit))
		names = sortedKeys(e.curse)
	case partyWarcry:
		names = sortedKeys(e.warcry)
	case partyLink:
		names = sortedKeys(e.link)
	case partyEnemyMods:
		names = sortedKeys(e.enemyMods)
	case partyEnemyConditions:
		names = sortedKeys(e.enemyConds)
	case partyPlayerMods:
		names = sortedKeys(e.playerMods)
	default:
		return ""
	}
	sort.Strings(names)
	for _, n := range names {
		if buffType == partyAura && n == partyExtraAura && len(e.extraAura) == 0 {
			continue
		}
		if buf.Len() > 0 {
			buf.WriteString("\n")
		}
		buf.WriteString(n)
		var x *partyExport
		switch buffType {
		case partyAura:
			x = e.aura[n]
		case partyCurse:
			x = e.curse[n]
		case partyWarcry:
			x = e.warcry[n]
		case partyLink:
			x = e.link[n]
		}
		switch {
		case buffType == partyEnemyMods:
			for _, m := range e.enemyMods[n] {
				buf.WriteString("\n" + formatSourceMod(m))
			}
			continue
		case buffType == partyPlayerMods || buffType == partyEnemyConditions:
			continue
		case n == partyOtherEffects && buffType == partyAura:
			e.writeOther(&buf)
			buf.WriteString("\n---")
			continue
		case n == partyExtraAura && buffType == partyAura:
			for _, m := range e.extraAura {
				buf.WriteString("\n" + formatSourceMod(m))
			}
			buf.WriteString("\n---")
			continue
		}
		buf.WriteString("\n" + luaToString(x.effectMult*100)) //nolint:mnd // percent
		if buffType == partyCurse {
			buf.WriteString("\n" + strconv.FormatBool(x.isMark))
		}
		for _, m := range x.mods {
			buf.WriteString("\n" + formatSourceMod(m))
		}
		buf.WriteString("\n---")
	}
	return buf.String()
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// writeOther writes the otherEffects block's lines: "buff|effect|mod".
func (e *partyExports) writeOther(buf *strings.Builder) {
	for _, k := range sortedKeys(e.other) {
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

// finish is the end of calcs.perform's export (CalcPerform.lua
// 3848-3967): the enemy's conditions and mods, the player stats party
// members read, the mine auras' stacks and the stats the links need. main
// is env.player.mainSkill and w1 env.player.weaponData1.
//
//nolint:gocognit,gocyclo,cyclop,funlen,goconst // the Lua block in its order
func (e *partyExports) finish(db *ModDB, main *activeSkill, w1 hitSource) {
	enemy, out := db.enemy, db.output
	value := func(name string) bool {
		l := enemy.mods[name]
		return len(l) > 0 && l[0].Value != nil && l[0].Value != false
	}
	for _, k := range sortedKeys(enemy.mods) {
		switch {
		case strings.Contains(k, "Condition") && !strings.Contains(k, "Party"):
			e.enemyConds[k] = true
		case strings.Contains(k, "Resist") && !strings.Contains(k, "Totem") && !strings.Contains(k, "Max") ||
			strings.Contains(k, "Damage") || strings.Contains(k, "ActionSpeed") || strings.Contains(k, "SelfCrit") ||
			strings.Contains(k, "Multiplier") && !strings.Contains(k, "Max") && !strings.Contains(k, "Impale"):
			for _, v := range enemy.mods[k] {
				if f, isNum := v.Value.(float64); isNum && f == 0 {
					continue
				}
				if v.Source == "EnemyConfig" || v.Source == "Base" || strings.Contains(v.Source, "Delirious") ||
					strings.HasPrefix(v.Source, "Party") {
					continue
				}
				skip := false
				for _, t := range v.Tags {
					if et := tagStr(t, "effectType"); et == "Curse" || et == "AuraDebuff" {
						skip = true
						break
					}
				}
				if skip {
					continue
				}
				if len(v.Tags) > 0 {
					t := v.Tags[0]
					switch tagStr(t, "type") {
					case "Condition":
						vars := tagList(t, "varList")
						v1 := tagStr(t, "var")
						set := v1 != "" && value("Condition:"+v1)
						if v1 == "" && len(vars) > 0 {
							set = value("Condition:" + vars[0])
						}
						if !set {
							continue
						}
					case "Multiplier":
						if !value("Multiplier:" + tagStr(t, "var")) {
							continue
						}
					}
				}
				e.enemyMods[k] = append(e.enemyMods[k], v)
			}
		}
	}
	for _, t := range []string{"Physical", "Lightning", "Cold", "Fire", "Chaos"} {
		if v, ok := out[t+"Resist"]; ok && db.Flag("Enemy"+t+"ResistEqualToYours") {
			e.playerMods["Enemy"+t+"ResistEqualToYours"] = true
			e.playerMods[t+"Resist="+luaToString(v)] = true
		}
	}
	if v, ok := out["EnduranceChargesMax"]; ok && db.Flag("PartyMemberMaximumEnduranceChargesEqualToYours") {
		e.playerMods["PartyMemberMaximumEnduranceChargesEqualToYours"] = true
		e.playerMods["EnduranceChargesMax="+luaToString(v)] = true
	}
	e.playerMods["MovementSpeedMod|percent|max="+luaToString(out["MovementSpeedMod"]*100)] = true //nolint:mnd // percent
	for _, m := range e.extraAura {
		if m.Name == "YourFortifyEqualToParent" {
			e.playerMods["PartyMemberFortifyEqualToYours"] = true
			e.playerMods["FortificationStacks="+luaToString(out["FortificationStacks"])] = true
		}
	}
	// The mine auras' stacks (3904-3942).
	for _, name := range sortedKeys(e.aura) {
		if !strings.Contains(name, "Mine") || strings.Contains(name, " Limit") {
			continue
		}
		stack := 0.0
		if l := e.enemyMods["Multiplier:"+name+"Stack"]; len(l) == 1 {
			stack, _ = num(l[0].Value)
		}
		delete(e.enemyMods, "Multiplier:"+name+"Stack")
		for _, key := range []string{name, name + " Limit"} {
			x, ok := e.aura[key]
			if !ok {
				continue
			}
			if stack == 0 {
				delete(e.aura, key)
				continue
			}
			cp := &partyExport{effectMult: x.effectMult, isMark: x.isMark}
			for _, m := range x.mods {
				for i, t := range m.Tags {
					if tagStr(t, "effectStackVar") == "ActiveMineCount" {
						nt := maps.Clone(t)
						delete(nt, "effectStackVar")
						m.Tags = append(append(append([]map[string]any(nil), m.Tags[:i]...), nt), m.Tags[i+1:]...)
						if f, isNum := num(m.Value); isNum {
							m.Value = f * stack
						}
						break
					}
				}
				cp.mods = append(cp.mods, m)
			}
			e.aura[key] = cp
		}
	}
	// The links (3944-3967). MaximumLifeLeechIsEqualToPartyMember has no
	// "Parent" in its name, so PoB never exports it.
	for _, name := range sortedKeys(e.link) {
		if name == "Flame Link" {
			e.playerMods["Life="+luaToString(out["Life"])] = true
		}
		for _, m := range e.link[name].mods {
			if !strings.Contains(m.Name, "Parent") {
				continue
			}
			switch m.Name {
			case "MaximumBlockAttackChanceIsEqualToParent":
				e.playerMods["BlockChanceMax="+luaToString(out["BlockChanceMax"])] = true
			case "BlockAttackChanceIsEqualToParent":
				e.playerMods["BlockChance="+luaToString(out["BlockChance"])] = true
			case "TakenFromParentESBeforeYou":
				e.playerMods["EnergyShieldRecoveryCap="+luaToString(out["EnergyShieldRecoveryCap"])] = true
			case "MainHandCritIsEqualToParent":
				crit := w1.critChance
				if main != nil && main.weapon1Cfg != nil && main.weapon1Cfg.SkillStats != nil {
					crit = main.weapon1Cfg.SkillStats["CritChance"]
				}
				e.playerMods["MainHand.CritChance="+luaToString(crit)] = true
			}
		}
	}
}

// partyExportCache is the Party tab export of a Calc result. PoB builds
// the MAIN output and then the CALCS one (Classes/CalcsTab.lua:459-461);
// both fill buffExports while the Party tab exports, so the CALCS pass is
// the one the tab keeps. It is computed on first use.
type partyExportCache struct {
	calc  *Calculator
	build *pobcode.Build
	once  sync.Once
	e     *partyExports
}

func (p *partyExportCache) exports() *partyExports {
	p.once.Do(func() {
		b := calcsModeBuild(p.build)
		// The main skill takes its own *Calcs part, minion and item set
		// (CalcActiveSkill.lua:268, 713, 755: activeSkill ==
		// env.player.mainSkill); find its gem first.
		r, err := p.calc.calc(b, calcOpts{})
		if err != nil {
			return
		}
		if g, k, ok := mainGem(r); ok {
			gm := &b.ActiveSkills().Groups[g-1].Gems[k]
			gm.SkillPart, gm.SkillMinion, gm.SkillMinionItemSet = gm.SkillPartCalcs, gm.SkillMinionCalcs, gm.SkillMinionItemSetCalcs
			if r, err = p.calc.calc(b, calcOpts{}); err != nil {
				return
			}
		}
		p.e = r.DB.partyExports
	})
	return p.e
}

// mainGem is the input build's group (1-based) and gem index of r's main
// skill's gem.
func mainGem(r *Result) (int, int, bool) {
	if r.main == nil || r.main.gem == nil {
		return 0, 0, false
	}
	set := r.build.ActiveSkills()
	if set == nil {
		return 0, 0, false
	}
	for gi := range set.Groups {
		for k := range set.Groups[gi].Gems {
			if &set.Groups[gi].Gems[k] == r.main.gem {
				return r.callerGroup(gi + 1), k, true
			}
		}
	}
	return 0, 0, false
}

// calcsModeBuild is b as PoB's CALCS mode sees it: the Calcs tab's socket
// group (skill_number, CalcSetup.lua:1646-1648) as the main one, each
// group's mainActiveSkillCalcs (1959-1961), and each gem's stage count,
// mine count and minion skill *Calcs selections (CalcActiveSkill.lua:642,
// 668, 944); the main skill's part, minion and item set come in
// exports. The Calcs tab's buff mode (misc_buffMode, CalcSetup.lua
// 578-599) other than EFFECTIVE is not ported.
func calcsModeBuild(b *pobcode.Build) *pobcode.Build {
	cp := *b
	cp.SkillSets = append([]pobcode.SkillSet(nil), b.SkillSets...)
	set := cp.ActiveSkills()
	if set == nil {
		return &cp
	}
	set.Groups = append([]pobcode.SocketGroup(nil), set.Groups...)
	n := 1
	for _, in := range b.CalcsInputs {
		if in.Name == "skill_number" && in.Kind == pobcode.KindNumber {
			n = int(in.Number)
		}
	}
	cp.MainSocketGroup = min(max(len(set.Groups), 1), n)
	for i := range set.Groups {
		g := &set.Groups[i]
		g.MainActiveSkill = max(g.MainActiveSkillCalcs, 1)
		g.Gems = append([]pobcode.Gem(nil), g.Gems...)
		for k := range g.Gems {
			gm := &g.Gems[k]
			gm.SkillStageCount, gm.SkillMineCount, gm.SkillMinionSkill = gm.SkillStageCountCalcs, gm.SkillMineCountCalcs, gm.SkillMinionSkillCalcs
		}
	}
	return &cp
}
