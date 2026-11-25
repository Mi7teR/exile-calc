package engine

import (
	"math"
	"strings"

	"github.com/Mi7teR/poe-lib/itemtext"
)

// heraldAuraCounts is "Calculate number of active heralds and auras
// affecting self" of calcs.perform (mode_buffs).
func heraldAuraCounts(db *ModDB, skills []*activeSkill) {
	heralds, auras := map[string]bool{}, map[string]bool{}
	for _, s := range skills {
		name := s.cfg.SkillName
		switch {
		case s.types[stHerald] && !heralds[name]:
			heralds[name] = true
			db.multipliers["Herald"]++
			db.conditions["AffectedByHerald"] = true
		case !s.types[stHerald] && s.types[stAura] && !s.types[stAuraAffectsEnemies] && !truthy(s.data["auraCannotAffectSelf"]) && !auras[name]:
			auras[name] = true
			db.multipliers["AuraAffectingSelf"]++
		}
	}
}

// manaAppliesToShock is calcs.perform's "Maximum Mana conversion from
// Lightning Mastery": increases to maximum mana also apply to shock effect.
func manaAppliesToShock(db *ModDB) {
	if !db.Flag("ManaAppliesToShockEffect") {
		return
	}
	mult := 100.0
	if v, ok := db.MaxCfg(nil, "ImprovedManaAppliesToShockEffect"); ok {
		mult = v
	}
	convertIncMods(db, []string{"Mana"}, "EnemyShockEffect", mult/100)
}

// convertIncMods adds, for each applicable INC mod of the names, an INC mod
// of to at mult of its value (floored), as the "X also applies to Y"
// conversions of calcs.perform and calcs.defence do.
func convertIncMods(db *ModDB, names []string, to string, mult float64) {
	var add []Mod
	for d := db; d != nil; d = d.parent {
		for _, name := range names {
			for i := range d.mods[name] {
				m := &d.mods[name][i]
				if !eligible(m, "INC", nil) {
					continue
				}
				if _, ok := db.eval(m, nil); !ok {
					continue
				}
				v, _ := num(m.Value)
				add = append(add, Mod{Name: to, Type: "INC", Value: math.Floor(v * mult), Source: m.Source,
					Flags: m.Flags, KeywordFlags: m.KeywordFlags, Tags: convertedModTags(m.Tags, mult, false)})
			}
		}
	}
	for _, m := range add {
		db.Add(m)
	}
}

// impaleStacks is "Fix the configured impale stacks on the enemy" of
// calcs.perform: the maximum when not configured, capped otherwise.
func impaleStacks(db *ModDB) {
	max := db.Sum("BASE", "ImpaleStacksMax") * (1 + db.Sum("BASE", "ImpaleAdditionalDurationChance")/100) //nolint:predeclared // legacy, kept by pure refactor; see refactor plan part 2
	combat := map[string]any{"type": "Condition", "var": "Combat"}
	mods := db.enemy.mods["Multiplier:ImpaleStacks"]
	has := false
	for _, m := range mods {
		has = has || m.Type == "BASE"
	}
	switch {
	case !has:
		db.enemy.NewMod("Multiplier:ImpaleStacks", "BASE", max, "Config", combat)
	case db.enemy.Sum("BASE", "Multiplier:ImpaleStacks") > max:
		for i := range mods {
			if mods[i].Type == "BASE" && mods[i].Source == "Config" {
				mods[i].Value = max
			}
		}
	}
}

// damageToAuraEffect is the Breach ring conversion of calcs.perform:
// increases to <Element> Damage also apply to the effect of <Element>
// skills' auras at a share of their value, up to a limit.
func damageToAuraEffect(db *ModDB) {
	kf := map[string]uint64{"Lightning": kfLightning, "Fire": kfFire, "Cold": kfCold, "Chaos": kfChaos, "Physical": kfPhysical}
	for _, el := range []string{"Lightning", "Fire", "Cold", "Chaos", "Physical"} {
		name := el + "DamageAppliesTo" + el + "AuraEffect"
		if !db.Flag(name) {
			continue
		}
		mult := db.Sum("BASE", "Improved"+name) / 100
		if mult == 0 {
			mult = 1
		}
		limit, hasLimit := db.MaxCfg(nil, name+"Limit")
		total := 0.0
		cfg := &Cfg{}
		var add []Mod
		for d := db; d != nil; d = d.parent {
			for i := range d.mods[el+"Damage"] {
				m := &d.mods[el+"Damage"][i]
				if !eligible(m, "INC", cfg) {
					continue
				}
				if _, ok := db.eval(m, cfg); !ok {
					continue
				}
				v, _ := num(m.Value)
				conv := math.Floor(v * mult)
				if hasLimit && conv > 0 {
					if rem := limit - total; rem <= 0 {
						conv = 0
					} else {
						conv = math.Min(conv, rem)
						total += conv
					}
				}
				if conv != 0 {
					add = append(add, Mod{Name: "AuraEffect", Type: m.Type, Value: conv, Source: m.Source, Flags: m.Flags,
						KeywordFlags: kf[el], Tags: convertedModTags(m.Tags, mult, true)})
				}
			}
		}
		for _, m := range add {
			db.Add(m)
		}
	}
}

// convertedModTags is calcLib.getConvertedModTags.
func convertedModTags(tags []map[string]any, mult float64, minionMods bool) []map[string]any {
	out := make([]map[string]any, len(tags))
	for i, t := range tags {
		c := make(map[string]any, len(t))
		for k, v := range t {
			c[k] = v
		}
		switch {
		case minionMods && t["type"] == "ActorCondition" && t["actor"] == "parent":
			c = map[string]any{"type": "Condition", "var": t["var"]}
		case truthy(t["limitTotal"]):
			if l, ok := num(t["limit"]); ok {
				c["limit"] = l * mult
			}
		}
		out[i] = c
	}
	return out
}

// totemCounts is the totem part of calcs.perform's active skill loop:
// with any totem skill, ActiveTotemLimit is the main skill's limit and
// TotemsSummoned (read by "per summoned totem" mods) the configured count
// or that limit.
// brandCounts is the Brand part of calcs.perform's active skill loop: each
// brand's attached count (the configured number up to the attach limit,
// else the limit) and the ActiveBrand, BrandsAttachedToEnemy and enemy
// BrandsAttached multipliers.
func brandCounts(db *ModDB, skills []*activeSkill) {
	for _, s := range skills {
		if !s.types[stBrand] || s.mods == nil {
			continue
		}
		limit := math.Min(s.mods.SumCfg(s.cfg, "BASE", "BrandsAttachedLimit"), s.mods.SumCfg(s.cfg, "BASE", "ActiveBrandLimit"))
		attached := limit
		if configured := db.Sum("BASE", "Multiplier:ConfigBrandsAttachedToEnemy"); configured > 0 {
			attached = math.Min(configured, limit)
		}
		s.data["attachedBrandCount"] = attached
		active := math.Min(db.Sum("BASE", "Multiplier:ConfigActiveBrands"), s.mods.SumCfg(s.cfg, "BASE", "ActiveBrandLimit"))
		db.multipliers["ActiveBrand"] = math.Max(active, db.multipliers["ActiveBrand"])
		db.multipliers["BrandsAttachedToEnemy"] = math.Max(attached, db.multipliers["BrandsAttachedToEnemy"])
		db.enemy.multipliers["BrandsAttached"] = math.Max(attached, db.enemy.multipliers["BrandsAttached"])
	}
}

func totemCounts(db *ModDB, skills []*activeSkill) {
	main := mainSkill(skills)
	if main == nil || main.mods == nil {
		return
	}
	out := db.output
	for _, s := range skills {
		if !s.flags["totem"] {
			continue
		}
		limit := main.mods.SumCfg(main.cfg, "BASE", "ActiveTotemLimit", "ActiveBallistaLimit")
		out["ActiveTotemLimit"] = math.Max(limit, out["ActiveTotemLimit"])
		if o, ok := db.Override("TotemsSummoned"); ok {
			out["TotemsSummoned"] = o
		} else {
			out["TotemsSummoned"] = out["ActiveTotemLimit"]
		}
		db.enemy.multipliers["TotemsSummoned"] = math.Max(out["TotemsSummoned"], db.enemy.multipliers["TotemsSummoned"])
	}
}

// abyssCounts is CalcSetup.lua's abyss jewel bookkeeping: Have<Base> and
// the jewel counts by type, rarity and parent item slot.
func abyssCounts(db *ModDB, it *itemtext.Item, parent string) {
	if it.Base == nil || it.Base.Type != "Jewel" || it.Base.SubType != "Abyss" {
		return
	}
	base := strings.ReplaceAll(it.BaseName, " ", "")
	cond := "Have" + base
	if !db.conditions[cond] {
		db.conditions[cond] = true
		db.multipliers["AbyssJewelType"]++
	}
	if parent != "" {
		db.conditions[cond+"In"+parent] = true
		db.multipliers[base+"In"+parent]++
	}
	db.multipliers["AbyssJewel"]++
	switch it.Rarity {
	case itemtext.Normal:
		db.multipliers["NormalAbyssJewels"]++
	case itemtext.Magic:
		db.multipliers["MagicAbyssJewels"]++
	case itemtext.Rare:
		db.multipliers["RareAbyssJewels"]++
	case itemtext.Unique, itemtext.Relic:
		db.multipliers["UniqueAbyssJewels"]++
	}
	db.multipliers[base]++
}

// warcryPower is the WarcryPower output and multiplier of calcs.perform.
func warcryPower(db *ModDB) {
	if db.Flag("CryWolfMinimumPower") && db.Sum("BASE", "WarcryPower") < 10 {
		db.NewMod("WarcryPower", "OVERRIDE", 10, "Minimum Warcry Power from CryWolf")
	}
	if db.Flag("WarcryInfinitePower") {
		db.NewMod("WarcryPower", "OVERRIDE", 999999, "Warcries have infinite power")
	}
	p, ok := db.Override("WarcryPower")
	if !ok {
		p = db.Sum("BASE", "WarcryPower")
	}
	db.output["WarcryPower"] = p
	db.multipliers["WarcryPower"] = p
}
