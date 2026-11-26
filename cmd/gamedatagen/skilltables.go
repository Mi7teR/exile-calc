package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/Mi7teR/exile-calc/gamedata"
	"github.com/Mi7teR/exile-calc/internal/luadata"
)

// skillFiles are loaded by Modules/Data.lua in this order.
var skillFiles = []string{"act_str", "act_dex", "act_int", "other", "glove", "minion", "spectre", "sup_str", "sup_dex", "sup_int"}

// grantedEffect is the part of a Data/Skills entry ModParser.lua reads.
type grantedEffect struct {
	Name       string          `lua:"name"`
	Hidden     bool            `lua:"hidden"`
	FromItem   bool            `lua:"fromItem"`
	FromTree   bool            `lua:"fromTree"`
	Support    bool            `lua:"support"`
	SkillTypes map[int]bool    `lua:"skillTypes"`
	BaseFlags  map[string]bool `lua:"baseFlags"`
}

func loadSkills(read func(string) (string, error), env map[string]luadata.Value) (map[string]*grantedEffect, error) {
	all := map[string]*grantedEffect{}
	for _, f := range skillFiles {
		rel := "Data/Skills/" + f + ".lua"
		src, err := read(rel)
		if err != nil {
			return nil, err
		}
		v, err := luadata.Eval(src, env)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		var skills map[string]*grantedEffect
		if err := luadata.Decode(v, &skills); err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		for id, s := range skills {
			s.Name = sanitiseText(s.Name)
			all[id] = s
		}
	}
	return all, nil
}

// sanitiseText ports Common.lua sanitiseText (UTF-8 replacements).
var sanitiser = strings.NewReplacer(
	"‐", "-", "‑", "-", "‒", "-", "–", "-", "—", "-", "―", "-",
	"​", "", "−", "-", "ä", "a", "ö", "o",
)

func sanitiseText(s string) string { return sanitiser.Replace(s) }

// luaLower is string.lower in the C locale (ASCII only).
func luaLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// camel is s:gsub("^%l", string.upper):gsub(" %l", string.upper):gsub(" ", "").
func camel(s string) string {
	r := []rune(s)
	for i := range r {
		if (i == 0 || r[i-1] == ' ') && r[i] >= 'a' && r[i] <= 'z' {
			r[i] = unicode.ToUpper(r[i])
		}
	}
	return strings.ReplaceAll(string(r), " ", "")
}

// Generated values use the generator's JSON encoding directly.
func jmod(name, typ string, value any, flags, kw float64, tags ...any) map[string]any {
	m := map[string]any{"name": name, "type": typ, "value": value, "flags": flags, "keywordFlags": kw}
	if len(tags) > 0 {
		m["@list"] = tags
	}
	return m
}

var arg1 = map[string]any{"$arg": 1.0}

func tmpl(v any) map[string]any { return map[string]any{"$template": v} }

// addSkillTables ports the gem loop of ModParser.lua: skill names become
// tags in skillNameList and preSkillNameList, and some gem tags add special
// mods. These keys are added after specialModList is anchored, so they keep
// their own anchors.
func addSkillTables(out map[string]any, gems map[string]*gamedata.Gem, skills map[string]*grantedEffect, skillTypeBuff, kwArrow float64) {
	skillNames := out["skillNameList"].(map[string]any)
	pre := map[string]any{}
	special := out["specialModList"].(map[string]any)

	ids := make([]string, 0, len(gems))
	for id := range gems {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		gem := gems[id]
		ge := skills[gem.GrantedEffectID]
		if ge == nil || ge.Hidden || ge.Support {
			continue
		}
		name := ge.Name
		low := luaLower(name)
		tags := map[string]bool{}
		for _, t := range gem.Tags {
			tags[t] = true
		}
		nameTag := func() map[string]any {
			return map[string]any{"type": "SkillName", "skillName": name, "includeTransfigured": true}
		}
		tag := func() map[string]any { return map[string]any{"tag": nameTag()} }
		buffTag := func() map[string]any {
			return map[string]any{"addToSkill": nameTag(), "tag": map[string]any{"type": "GlobalEffect", "effectType": "Buff"}}
		}
		extraSkill := func(m map[string]any) []any {
			return []any{jmod("ExtraSkillMod", "LIST", map[string]any{"mod": m}, 0, 0, nameTag())}
		}

		skillNames[" "+low+" "] = tag()
		pre["^"+low+" "] = tag()
		pre["^"+low+" has ?a? "] = tag()
		pre["^"+low+" deals "] = tag()
		pre["^"+low+" damage "] = tag()
		if tags["totem"] {
			pre["^"+low+" totem deals "] = tag()
			pre["^"+low+" totem grants "] = buffTag()
		}
		if ge.SkillTypes[int(skillTypeBuff)] || ge.BaseFlags["buff"] {
			pre["^"+low+" grants "] = buffTag()
			pre["^"+low+" grants a?n? ?additional "] = buffTag()
		}
		if tags["aura"] || tags["herald"] {
			cond := map[string]any{"tag": map[string]any{"type": "Condition", "var": "AffectedBy" + strings.ReplaceAll(name, " ", "")}}
			skillNames["while affected by "+low] = cond
			skillNames["while using "+low] = map[string]any{"tag": map[string]any{"type": "Condition", "var": "AffectedBy" + strings.ReplaceAll(name, " ", "")}}
		}
		if tags["curse"] {
			skillNames["if you've cast "+low+" in the past (%d+) seconds"] = map[string]any{"tag": map[string]any{"type": "Condition", "var": "SelfCast" + camel(name)}}
		}
		if tags["mine"] {
			special["^"+low+" has (%d+)%% increased throwing speed"] = tmpl(extraSkill(jmod("MineLayingSpeed", "INC", arg1, 0, 0)))
		}
		if tags["trap"] {
			special["(%d+)%% increased "+low+" throwing speed"] = tmpl(extraSkill(jmod("TrapThrowingSpeed", "INC", arg1, 0, 0)))
		}
		if tags["chaining"] {
			special["^"+low+" chains an additional time"] = extraSkill(jmod("ChainCountMax", "BASE", 1.0, 0, 0))
			special["^"+low+" chains an additional (%d+) times"] = tmpl(extraSkill(jmod("ChainCountMax", "BASE", arg1, 0, 0)))
			special["^"+low+" chains (%d+) additional times"] = tmpl(extraSkill(jmod("ChainCountMax", "BASE", arg1, 0, 0)))
		}
		if tags["bow"] {
			special["^"+low+" fires an additional arrow"] = extraSkill(jmod("ProjectileCount", "BASE", 1.0, 0, kwArrow))
			special["^"+low+" fires (%d+) additional arrows?"] = tmpl(extraSkill(jmod("ProjectileCount", "BASE", arg1, 0, kwArrow)))
		}
		if tags["projectile"] {
			special["^"+low+" pierces an additional target"] = []any{jmod("PierceCount", "BASE", 1.0, 0, 0, nameTag())}
			special["^"+low+" pierces (%d+) additional targets?"] = tmpl([]any{jmod("PierceCount", "BASE", arg1, 0, 0, nameTag())})
		}
		if tags["bow"] || tags["projectile"] {
			special["^"+low+" fires an additional projectile"] = extraSkill(jmod("ProjectileCount", "BASE", 1.0, 0, 0))
			special["^"+low+" fires (%d+) additional projectiles"] = tmpl(extraSkill(jmod("ProjectileCount", "BASE", arg1, 0, 0)))
			special["^"+low+" fires (%d+) additional shard projectiles"] = tmpl(extraSkill(jmod("ProjectileCount", "BASE", arg1, 0, 0)))
		}
	}
	out["preSkillNameList"] = pre
}

// addSkillLookups exports what ModParser.lua's extraSupport and
// grantedExtraSkill read from data: gemIdLookup (skill name -> skill id) and,
// per support skill id, the gem data.gemForBaseName resolves it to.
func addSkillLookups(out map[string]any, gems map[string]*gamedata.Gem, skills map[string]*grantedEffect) {
	lookup := map[string]any{
		"power charge on critical strike": "SupportPowerChargeOnCritical",
		"snipe support":                   "ChannelledSnipeSupport",
	}
	ids := make([]string, 0, len(skills))
	for id := range skills {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ge := skills[id]
		if !ge.Hidden || ge.FromItem || ge.FromTree {
			lookup[luaLower(ge.Name)] = id
		}
	}
	out["gemIdLookup"] = lookup

	// Modules/Data.lua setupGem: gemForBaseName.
	gemForBaseName := map[string]string{}
	gemIDs := make([]string, 0, len(gems))
	for id := range gems {
		gemIDs = append(gemIDs, id)
	}
	sort.Strings(gemIDs)
	for _, id := range gemIDs {
		gem := gems[id]
		ge := skills[gem.GrantedEffectID]
		if ge == nil {
			continue
		}
		baseName := sanitiseText(gem.Name)
		if ge.Support && gem.GrantedEffectID != "SupportBarrage" {
			baseName += " Support"
		}
		gemForBaseName[luaLower(baseName)] = id
		if gem.BaseTypeName != "" && gem.BaseTypeName != baseName {
			gemForBaseName[luaLower(gem.BaseTypeName)] = id
		}
	}
	support := map[string]any{}
	for _, id := range ids {
		gemID, ok := gemForBaseName[luaLower(skills[id].Name+" Support")]
		if !ok {
			continue
		}
		gem := gems[gemID]
		info := map[string]any{"grantedEffectId": gem.GrantedEffectID}
		if sec := skills[gem.SecondaryGrantedEffectID]; gem.SecondaryGrantedEffectID != "" && sec != nil {
			info["secondaryGrantedEffectId"] = gem.SecondaryGrantedEffectID
			info["secondaryIsSupport"] = sec.Support
		}
		support[id] = info
	}
	out["supportGems"] = support
}
