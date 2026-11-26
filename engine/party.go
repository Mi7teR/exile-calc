package engine

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Mi7teR/exile-calc/pobcode"
)

// partyBuff is one buff of a Party tab list (Classes/PartyTab.lua
// ParseBuffs: listElement): the exporter's effect in percent and its mods.
type partyBuff struct {
	name     string
	effect   float64
	noEffect bool // the effect line was not a number (Lua nil)
	isMark   bool
	mods     []Mod
}

// effectOr100 is (effectMult or 100) of the party Warcry and Link loops
// (CalcPerform.lua:3126, 3146).
func (b *partyBuff) effectOr100() float64 {
	if b.noEffect {
		return 100
	}
	return b.effect
}

// partyBuffList is list[currentModType] of ParseBuffs: the buffs by name in
// file order, with Vaal buffs in their own table (list[type]["Vaal"]).
type partyBuffList struct {
	buffs, vaal []*partyBuff
}

// get is list[type][name]: a Vaal buff is only found through vaal.
func (l *partyBuffList) get(name string) *partyBuff {
	if l == nil {
		return nil
	}
	for _, b := range l.buffs {
		if b.name == name {
			return b
		}
	}
	return nil
}

// partyActor is env.build.partyTab.actor (env.partyMembers) and the tab's
// enemyModList, read from the build's Party tab. ParseBuffs' rewrite of
// unknown "Skill:" sources to a known skill id (PartyTab.lua:872-881) is not
// ported: it only changes mod sources. The Party tab's buffs
// apply whether or not the build exports its own (enableExportBuffs only
// fills buffExports, CalcPerform.lua partyTabEnableExportBuffs).
type partyActor struct {
	aura, curse, warcry, link map[string]*partyBuffList
	curseLimit                float64
	enemyMods                 []Mod
	output                    map[string]float64 // actor.output from PartyMemberStats ("k1.k2" for nested keys)
	flags                     []Mod              // actor.modDB from PartyMemberStats
}

// newPartyActor runs PartyTabClass:Load over the build's imported blocks.
func newPartyActor(p *pobcode.Party) *partyActor {
	if p == nil || len(p.Buffs) == 0 {
		return nil
	}
	a := &partyActor{
		aura: map[string]*partyBuffList{}, curse: map[string]*partyBuffList{},
		warcry: map[string]*partyBuffList{}, link: map[string]*partyBuffList{},
		output: map[string]float64{},
	}
	for _, b := range p.Buffs {
		switch b.Name {
		case "PartyMemberStats":
			a.parseMemberStats(b.Text)
		case "Aura":
			parsePartyBuffs(a.aura, b.Text, "Aura", nil)
		case "Curse":
			parsePartyBuffs(a.curse, b.Text, "Curse", &a.curseLimit)
		case "Warcry Skills":
			parsePartyBuffs(a.warcry, b.Text, "Warcry", nil)
		case "Link Skills":
			parsePartyBuffs(a.link, b.Text, "Link", nil)
		case "EnemyConditions":
			for _, line := range luaLines(b.Text) {
				if line != "" {
					a.enemyMods = append(
						a.enemyMods,
						Mod{
							Name:   strings.ReplaceAll(line, "Condition:", "Condition:Party:"),
							Type:   "FLAG",
							Value:  true,
							Source: "Party",
						},
					)
				}
			}
		case "EnemyMods":
			for _, line := range luaLines(b.Text) {
				if !strings.Contains(line, "|") {
					continue
				}
				if m, ok := parseFormattedSourceMod(line); ok {
					m.Source = "Party" + m.Source
					a.enemyMods = append(a.enemyMods, m)
				}
			}
		}
	}
	return a
}

// partyStat is actor.partyMembers.output[name] (only the player has a party).
func (db *ModDB) partyStat(name string) (float64, bool) {
	if db == nil || db.partyMembers == nil {
		return 0, false
	}
	v, ok := db.partyMembers.output[name]
	return v, ok
}

// luaLines is buf:gmatch("([^\n]*)\n?"): the lines, with the empty match
// at the end.
func luaLines(s string) []string {
	return append(strings.Split(s, "\n"), "")
}

var (
	partyNestedStat = regexp.MustCompile(`^([\w ]*?\w+)\.([\w ]*?\w+)=(.+)$`)
	partyTaggedStat = regexp.MustCompile(`([\w ]*?\w+)\|(.+)=(.+)`)
	partyPlainStat  = regexp.MustCompile(`([\w ]*?\w+)=(.+)`)
	partyOtherLine  = regexp.MustCompile(`([\w ']*?\w+)\|(\w+)\|(.+)`)
)

// parseMemberStats is the PartyMemberStats branch of ParseBuffs: "k=v"
// lines go to actor.output, other lines are flags on the actor's ModDB.
func (a *partyActor) parseMemberStats(text string) {
	for _, line := range luaLines(text) {
		switch {
		case strings.Contains(line, "="):
			if m := partyNestedStat.FindStringSubmatch(line); m != nil {
				a.output[m[1]+"."+m[2]] = luaToNumber(m[3])
			} else if strings.Contains(line, "|") {
				t := partyTaggedStat.FindStringSubmatch(line)
				if t == nil {
					continue
				}
				v := luaToNumber(t[3])
				for tag := range strings.SplitSeq(t[2], "|") {
					switch tag {
					case "percent":
						v /= 100
					case "max":
						prev, ok := a.output[t[1]]
						if !ok {
							prev = 1
						}
						v = max(v, prev)
					}
				}
				a.output[t[1]] = v
			} else if kv := partyPlainStat.FindStringSubmatch(line); kv != nil {
				a.output[kv[1]] = luaToNumber(kv[2])
			}
		case line != "":
			a.flags = append(a.flags, Mod{Name: line, Type: "FLAG", Value: true, Source: "Party"})
		}
	}
}

// luaNumber is tonumber(s); ok is false for Lua's nil.
func luaNumber(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v, err == nil
}

// luaToNumber is tonumber(s), nil read as 0.
func luaToNumber(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// parsePartyBuffs is the Aura/Curse/Warcry/Link branch of
// PartyTabClass:ParseBuffs: blocks of a name, the exporter's effect (and
// for curses whether it is a mark), then formatted mods, separated by
// "---". The first block of the Curse tab is the party's curse limit.
func parsePartyBuffs(list map[string]*partyBuffList, text, buffType string, limit *float64) {
	p := &partyParser{list: list, buffType: buffType, limit: limit, mode: "Name", modType: "Unknown"}
	if buffType == "Curse" {
		p.mode = "CurseLimit"
	}
	if buffType == "Link" || buffType == "Warcry" {
		p.modType = buffType
	}
	for _, line := range luaLines(text) {
		p.line(line)
	}
}

// partyParser is the state of one ParseBuffs call: the mode, and the
// current buff's name, effect, mark flag and mod type.
type partyParser struct {
	list           map[string]*partyBuffList
	buffType, mode string
	name, modType  string
	effect         float64
	noEffect       bool
	isMark         bool
	limit          *float64
}

func (p *partyParser) line(line string) {
	switch {
	case line != "---" && strings.Contains(line, "---"):
		// A comment, not a divider.
	case p.mode == "CurseLimit" && line != "":
		*p.limit = luaToNumber(line)
		p.mode = "Name"
	case p.mode == "Name" && line != "":
		p.name = strings.ReplaceAll(line, "_Debuff", "")
		p.effect, p.noEffect = 0, false
		if line == "extraAura" || line == "otherEffects" {
			p.modType = line
			p.mode = "Stats"
		} else {
			p.mode = "Effect"
			p.modType = "Unknown"
		}
	case p.mode == "Effect":
		v, ok := luaNumber(line)
		p.effect, p.noEffect = v, !ok
		if p.buffType == "Curse" {
			p.mode = "isMark"
		} else {
			p.mode = "Stats"
		}
	case p.mode == "isMark":
		p.isMark = line == "true"
		p.mode = "Stats"
	case line == "---":
		p.mode = "Name"
	default:
		if strings.Contains(line, "|") && p.name != "SKIP" && !strings.Contains(line, "MinionModifier|LIST") {
			p.mod(line)
		}
	}
}

// mod adds a formatted mod line to the current buff. The first GlobalEffect
// tag sets the list it goes to; a weaker duplicate of a buff is skipped and
// a stronger one replaces it.
func (p *partyParser) mod(line string) {
	if p.modType == "otherEffects" {
		m := partyOtherLine.FindStringSubmatch(line)
		if m == nil {
			return
		}
		p.name, p.effect, line = m[1], luaToNumber(m[2]), m[3]
		p.noEffect = false
	}
	mod, ok := parseFormattedSourceMod(line)
	if !ok {
		return
	}
	for _, tag := range mod.Tags {
		if tagStr(tag, "type") == "GlobalEffect" && p.modType != "Link" && p.modType != "Warcry" &&
			p.modType != "otherEffects" {
			p.modType = tagStr(tag, "effectType")
		}
	}
	elem := p.element()
	if elem == nil {
		return
	}
	if strings.Contains(mod.Source, "Item") {
		if m := partyItemSource.FindStringSubmatch(mod.Source); m != nil {
			mod.Source = "Party - " + m[1]
		}
	}
	if p.buffType == "Link" {
		mod.Name = strings.ReplaceAll(mod.Name, "Parent", "PartyMember")
		for _, t := range mod.Tags {
			if t["actor"] == "parent" {
				t["actor"] = "partyMembers"
			}
		}
	}
	elem.mods = append(elem.mods, mod)
}

// element is listElement[currentName] of the current list, created or
// replaced by a stronger effect; nil (and the name becomes "SKIP") for a
// weaker one.
func (p *partyParser) element() *partyBuff {
	l := p.list[p.modType]
	if l == nil {
		l = &partyBuffList{}
		p.list[p.modType] = l
	}
	elems := &l.buffs
	if strings.HasPrefix(p.name, "Vaal") {
		elems = &l.vaal
	}
	for i, b := range *elems {
		if b.name != p.name {
			continue
		}
		switch {
		case b.effect == p.effect:
			return b
		case b.effect < p.effect:
			(*elems)[i] = &partyBuff{name: p.name, effect: p.effect, noEffect: p.noEffect}
			return (*elems)[i]
		default:
			p.name = "SKIP"
			return nil
		}
	}
	b := &partyBuff{name: p.name, effect: p.effect, noEffect: p.noEffect, isMark: p.isMark}
	*elems = append(*elems, b)
	return b
}

var partyItemSource = regexp.MustCompile(`Item:\d+:(.+)`)

// Data/Global.lua ModFlag and KeywordFlag by name, for
// modLib.parseFormattedSourceMod.
var (
	modFlagNames = map[string]uint64{
		"Attack": 0x1, "Spell": 0x2, "Hit": 0x4, "Dot": 0x8, "Cast": 0x10,
		"Melee": 0x100, "Area": 0x200, "Projectile": 0x400, "SourceMask": 0x600, "Ailment": 0x800,
		"MeleeHit": 0x1000, "Weapon": 0x2000,
		"Axe": 0x10000, "Bow": 0x20000, "Claw": 0x40000, "Dagger": 0x80000, "Mace": 0x100000,
		"Staff": 0x200000, "Sword": 0x400000, "Wand": 0x800000, "Unarmed": 0x1000000, "Fishing": 0x2000000,
		"WeaponMelee": 0x4000000, "WeaponRanged": 0x8000000, "Weapon1H": 0x10000000, "Weapon2H": 0x20000000,
		"WeaponMask": 0x2FFF0000,
	}
	keywordFlagNames = map[string]uint64{
		"Aura": 0x1, "Curse": 0x2, "Warcry": 0x4, "Movement": 0x8, "Physical": 0x10, "Fire": 0x20,
		"Cold": 0x40, "Lightning": 0x80, "Chaos": 0x100, "Vaal": 0x200, "Bow": 0x400, "Arrow": 0x800,
		"Trap": 0x1000, "Mine": 0x2000, "Totem": 0x4000, "Minion": 0x8000, "Attack": 0x10000,
		"Spell": 0x20000, "Hit": 0x40000, "Ailment": 0x80000, "Brand": 0x100000, "Poison": 0x200000,
		"Bleed": 0x400000, "Ignite": 0x800000, "PhysicalDot": 0x1000000, "LightningDot": 0x2000000,
		"ColdDot": 0x4000000, "FireDot": 0x8000000, "ChaosDot": 0x10000000, "MatchAll": 0x40000000,
	}
	partyTagPart = regexp.MustCompile(`^([A-Za-z]+)=(.+)`)
)

// parseFormattedSourceMod is ModTools.lua modLib.parseFormattedSourceMod:
// "value|source|name|type|flags|keywordFlags|tags", the format of
// modLib.formatSourceMod.
func parseFormattedSourceMod(line string) (Mod, bool) {
	parts := strings.Split(line, "|")
	if len(parts) < 4 {
		return Mod{}, false
	}
	m := Mod{Source: parts[1], Name: parts[2], Type: parts[3]}
	if parts[0] == "true" {
		m.Value = true
	} else {
		v, err := strconv.ParseFloat(parts[0], 64)
		if err != nil {
			v = 0
		}
		m.Value = v
	}
	// PoB quirk, copied: ModFlag[name] reads one flag name, so a mod
	// exported with several flags ("Attack,Hit") loses them all.
	if len(parts) > 4 {
		m.Flags = modFlagNames[parts[4]]
	}
	if len(parts) > 5 {
		m.KeywordFlags = keywordFlagNames[parts[5]]
	}
	if len(parts) > 6 {
		m.Tags = parseFormattedTags(parts[6])
	}
	return m, true
}

// parseFormattedTags is modLib.parseTags: tags separated by ",", their
// fields by "/". Values stay strings, as in PoB (Lua arithmetic coerces
// them, num does here), except threshold and "true".
func parseFormattedTags(s string) []map[string]any {
	if s == "" || s == "-" {
		return nil
	}
	var tags []map[string]any
	for group := range strings.SplitSeq(s, ",") {
		if group == "" {
			continue
		}
		tag := map[string]any{}
		for part := range strings.SplitSeq(group, "/") {
			m := partyTagPart.FindStringSubmatch(part)
			if m == nil {
				continue
			}
			var v any = m[2]
			switch {
			case m[1] == "threshold":
				if f, err := strconv.ParseFloat(m[2], 64); err == nil {
					v = f
				} else {
					v = nil
				}
			case m[2] == "true":
				v = true
			}
			tag[m[1]] = v
		}
		tags = append(tags, tag)
	}
	return tags
}

// auraBeats is the allyBuffs check of calcs.perform's Aura branches: the
// party has a non-Vaal aura of that name stronger than mult.
func (a *partyActor) auraBeats(typ, name string, mult float64) bool {
	if a == nil {
		return false
	}
	b := a.aura[typ].get(name)
	return b != nil && b.effect/100 > mult
}

// partyBuffsOnPlayer ports the party part of calcs.perform's "Combine
// buffs/debuffs" after the skill loop: the allies' otherEffects, auras
// (Vaal ones after the others), aura debuffs, warcries and links.
func (a *partyActor) partyBuffsOnPlayer(db *ModDB, add func([]Mod, string), addDebuff func([]Mod, string)) {
	if a == nil {
		return
	}
	cond := func(name string) string { return "AffectedBy" + strings.ReplaceAll(name, " ", "") }
	if l := a.aura["otherEffects"]; l != nil {
		// PoB quirk, copied: (effectMult + inc) / 100 * more adds the
		// effect on self as percentage points to the exporter's effect.
		inc := db.Sum("INC", "BuffEffectOnSelf", "AuraEffectOnSelf")
		more := db.More("BuffEffectOnSelf", "AuraEffectOnSelf")
		for _, b := range append(append([]*partyBuff(nil), l.buffs...), l.vaal...) {
			db.conditions[cond(b.name)] = true
			add(scaleMods(b.mods, (b.effect+inc)/100*more), b.name)
		}
	}
	if l := a.aura["Aura"]; l != nil {
		for _, b := range l.buffs {
			if !db.Flag("AlliesAurasCannotAffectSelf") && !db.conditions[cond(b.name)] {
				db.conditions["AffectedByAura"] = true
				db.conditions[cond(b.name)] = true
				add(scaleMods(b.mods, b.effect/100), b.name)
			}
		}
		for _, b := range l.vaal {
			if !db.Flag("AlliesAurasCannotAffectSelf") && !db.conditions[cond(b.name)] {
				db.conditions["AffectedByAura"] = true
				db.conditions[cond(b.name[min(5, len(b.name)):])] = true
				db.conditions[cond(b.name)] = true
				add(scaleMods(b.mods, b.effect/100), b.name)
			}
		}
	}
	if l := a.aura["AuraDebuff"]; l != nil && modeEffective(db) { // CalcPerform.lua:3090
		for _, b := range append(append([]*partyBuff(nil), l.buffs...), l.vaal...) {
			if !db.enemy.conditions[cond(b.name)] {
				db.enemy.conditions[cond(b.name)] = true
				db.conditions[cond(b.name)] = true
				addDebuff(scaleMods(b.mods, b.effect/100), b.name)
			}
		}
	}
	if l := a.warcry["Warcry"]; l != nil {
		for _, b := range l.buffs {
			if !db.conditions[cond(b.name)] {
				db.conditions["AffectedByWarcry"] = true
				db.conditions[cond(b.name)] = true
				add(partyWarcryMods(b), b.name)
			}
		}
	}
	if l := a.link["Link"]; l != nil {
		for _, b := range l.buffs {
			if !db.conditions[cond(b.name)] {
				db.conditions["AffectedByLink"] = true
				db.conditions[cond(b.name)] = true
				add(scaleMods(b.mods, b.effectOr100()/100), b.name)
			}
		}
	}
}

// partyWarcryMods scales each warcry mod by the warcry's effect and its
// first tag's warcryPowerBonus.
func partyWarcryMods(b *partyBuff) []Mod {
	out := make([]Mod, 0, len(b.mods))
	for _, m := range b.mods {
		bonus := 1.0
		if len(m.Tags) > 0 {
			if v, ok := num(m.Tags[0]["warcryPowerBonus"]); ok {
				bonus = v
			}
		}
		out = append(out, scaleMod(m, b.effectOr100()/100*bonus))
	}
	return out
}

// partyBuffsOnMinion is the env.minion side of partyBuffsOnPlayer: the
// allies' otherEffects, auras and warcries reach the main minion too.
func (a *partyActor) partyBuffsOnMinion(mdb *ModDB, out map[string][]Mod) {
	if a == nil {
		return
	}
	cond := func(name string) string { return "AffectedBy" + strings.ReplaceAll(name, " ", "") }
	if l := a.aura["otherEffects"]; l != nil {
		inc := mdb.Sum("INC", "BuffEffectOnSelf", "AuraEffectOnSelf")
		more := mdb.More("BuffEffectOnSelf", "AuraEffectOnSelf")
		for _, b := range append(append([]*partyBuff(nil), l.buffs...), l.vaal...) {
			mdb.conditions[cond(b.name)] = true
			mergeBuff(scaleMods(b.mods, (b.effect+inc)/100*more), out, b.name)
		}
	}
	if l := a.aura["Aura"]; l != nil {
		for _, b := range append(append([]*partyBuff(nil), l.buffs...), l.vaal...) {
			if !mdb.conditions[cond(b.name)] {
				mdb.conditions["AffectedByAura"] = true
				mdb.conditions[cond(b.name)] = true
				mergeBuff(scaleMods(b.mods, b.effect/100), out, b.name)
			}
		}
	}
	if l := a.warcry["Warcry"]; l != nil {
		for _, b := range l.buffs {
			if !mdb.conditions[cond(b.name)] {
				mdb.conditions["AffectedByWarcry"] = true
				mdb.conditions[cond(b.name)] = true
				mergeBuff(partyWarcryMods(b), out, b.name)
			}
		}
	}
}

// partyExtraAuras is calcs.perform's allyBuffs["extraAura"]: the allies'
// extra aura mods, scaled by the actor's buff and aura effect on self.
func (a *partyActor) partyExtraAuras(db *ModDB) {
	if a == nil {
		return
	}
	l := a.aura["extraAura"]
	if l == nil {
		return
	}
	inc := db.Sum("INC", "BuffEffectOnSelf", "AuraEffectOnSelf")
	scale := (1 + inc/100) * db.More("BuffEffectOnSelf", "AuraEffectOnSelf")
	for _, b := range append(append([]*partyBuff(nil), l.buffs...), l.vaal...) {
		for _, m := range scaleMods(b.mods, scale) {
			db.Add(m)
		}
	}
}

// partyCurses is calcs.perform's allyCurses: the party's curses with
// priority 0, scaled by their effect (and the enemy's curse effect on self
// for hexes), with the party's curse limit.
func (a *partyActor) partyCurses(enemy *ModDB) ([]*enemyCurse, int) {
	if a == nil || a.curse["Curse"] == nil {
		return nil, 0
	}
	var out []*enemyCurse
	l := a.curse["Curse"]
	for _, b := range append(append([]*partyBuff(nil), l.buffs...), l.vaal...) {
		mult := b.effect / 100
		if !b.isMark {
			mult *= enemy.More("CurseEffectOnSelf")
		}
		out = append(out, &enemyCurse{name: b.name, isMark: b.isMark, mods: scaleMods(b.mods, mult)})
	}
	return out, int(a.curseLimit)
}

// linkModNames are the mod names of the party's Link buffs.
func (a *partyActor) linkModNames() []string {
	var out []string
	if l := a.link["Link"]; l != nil {
		for _, b := range l.buffs {
			for _, m := range b.mods {
				out = append(out, m.Name)
			}
		}
	}
	return out
}
