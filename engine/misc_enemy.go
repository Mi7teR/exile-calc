package engine

import (
	"math"
	"reflect"
	"strings"
)

// enemyModifiers is applyEnemyModifiers (CalcPerform.lua): the mods an
// actor's EnemyModifier lists give its enemy, each added once.
type enemyModifiers map[uintptr]bool

func (applied enemyModifiers) apply(db *ModDB) {
	for _, e := range db.List(nil, "EnemyModifier") {
		v, _ := e.Value.(map[string]any)
		mm, ok := v["mod"].(map[string]any)
		if !ok {
			continue
		}
		key := reflect.ValueOf(mm).Pointer()
		if applied[key] {
			continue
		}
		applied[key] = true
		source, _ := mm["source"].(string)
		if source == "" {
			source = e.Mod.Source
		}
		db.enemy.Add(modFromMap(mm, source))
	}
}

// selfAilmentData is Data.lua data.nonDamagingAilment for the ailments the
// player applies to the enemy: maximum effect and display precision.
var enemyAilmentData = []struct {
	name, condition string
	max             float64
	precision       int
}{
	{"Chill", "Chilled", 30, 0}, {"Shock", "Shocked", 50, 0}, {"Scorch", "Scorched", 30, 0},
	{"Brittle", "Brittle", 6, 2}, {"Sap", "Sapped", 20, 0},
}

// enemyAilments ports calcs.perform's "Calculate maximum and apply the
// strongest non-damaging ailments": the player's chill, shock, scorch,
// brittle and sap on the enemy, active while the enemy has the condition.
func enemyAilments(db *ModDB, skills []*activeSkill, guaranteedBonechill bool) {
	out, enemy := db.output, db.enemy
	main := mainSkill(skills)
	for _, a := range enemyAilmentData {
		if enemy.Flag("Condition:Already"+a.condition) || enemy.Flag(a.name+"Immune", "ElementalAilmentImmune") ||
			enemy.Sum("BASE", "Avoid"+a.name, "AvoidAilments", "AvoidElementalAilments") >= 100 {
			continue
		}
		override, minimum := 0.0, 0.0
		var rows []Mod
		for _, n := range []string{a.name + "Base", a.name + "Override", a.name + "Minimum"} {
			for i := range db.mods[n] {
				m := db.mods[n][i]
				if m.Type != "BASE" || m.Flags != 0 || m.KeywordFlags != 0 {
					continue
				}
				if v, ok := db.eval(&m, nil); ok {
					if f, isNum := num(v); isNum && f == 0 {
						continue
					}
					rows = append(rows, m)
				}
			}
		}
		for _, m := range rows {
			effect, _ := num(m.Value)
			scales := m.Name == a.name+"Base" || m.Name == a.name+"Minimum"
			if m.Name == a.name+"Override" {
				enemy.Add(Mod{Name: "Condition:" + a.condition, Type: "FLAG", Value: true, Source: m.Source})
			}
			if scales {
				src := db
				if a.name != "Scorch" && a.name != "Sap" && a.name != "Brittle" && main != nil && main.mods != nil &&
					!main.mods.Flag("Cannot"+a.name) && main.flags["hit"] && db.Flag("ChecksHighestDamage") {
					src = main.mods
				}
				effect *= calcMod(src, nil, "Enemy"+a.name+"Effect")
			}
			effect *= calcMod(enemy, nil, "Self"+a.name+"Effect")
			if scales {
				db.Add(Mod{Name: a.name + "Override", Type: "BASE", Value: effect, Flags: m.Flags, KeywordFlags: m.KeywordFlags, Tags: m.Tags, Source: m.Source})
				if m.Name == a.name+"Minimum" {
					minimum += effect
				}
			}
			override = math.Max(math.Max(override, effect), minimum)
		}
		maxAilment, ok := db.Override(a.name + "Max")
		if !ok {
			maxAilment = 0
			for _, s := range skills {
				v := a.max
				if s.mods != nil {
					v += s.mods.Sum("BASE", a.name+"Max")
				}
				maxAilment = math.Max(maxAilment, v)
			}
		}
		out["Maximum"+a.name] = maxAilment
		p := math.Pow(10, float64(a.precision))
		cur := math.Floor(math.Min(math.Max(override, enemy.Sum("BASE", a.name+"Val")), maxAilment)*p) / p
		out["Current"+a.name] = cur
		when := map[string]any{"type": "Condition", "var": a.condition}
		switch a.name {
		case "Chill":
			enemy.NewMod("ActionSpeed", "INC", -cur, "Chill", when)
			switch {
			case db.Flag("ChillEffectIncDamageTaken"):
				enemy.NewMod("DamageTaken", "INC", cur, "Ahuana's Bite", when)
			case db.Flag("ChillEffectIncColdDamageTaken"):
				enemy.NewMod("ColdDamageTaken", "INC", cur, "Chilled by Hits", when)
			case db.Flag("ChillingAreaIncColdDamageTaken"):
				enemy.NewMod("ColdDamageTaken", "INC", cur, "Chilling Area", when)
			case out["HasBonechill"] != 0 && (guaranteedBonechill || enemy.Sum("BASE", "ChillVal") > 0):
				enemy.NewMod("ColdDamageTaken", "INC", cur, "Bonechill", when)
			}
			if db.Flag("ChillEffectLessDamageDealt") {
				enemy.NewMod("Damage", "MORE", -cur/2, "Shaper of Winter", when)
			}
		case "Shock":
			if db.Flag("ShockCanStack") {
				limit, ok := db.Override("ShockStacksMax") //nolint:govet // legacy, kept by pure refactor; see refactor plan part 2
				if !ok {
					limit = db.Sum("BASE", "ShockStacksMax")
				}
				enemy.NewMod("DamageTaken", "INC", cur, "Shock", when, map[string]any{"type": "Multiplier", "var": "ShockStacks", "limit": limit})
				out["CurrentShock"] = cur * math.Min(enemy.Sum("BASE", "Multiplier:ShockStacks"), limit)
			} else {
				enemy.NewMod("DamageTaken", "INC", cur, "Shock", when)
			}
		case "Scorch":
			if db.Flag("ScorchCanStack") {
				limit, ok := db.Override("ScorchStacksMax") //nolint:govet // legacy, kept by pure refactor; see refactor plan part 2
				if !ok {
					limit = db.Sum("BASE", "ScorchStacksMax")
				}
				enemy.NewMod("ElementalResist", "BASE", -cur, "Scorch", when, map[string]any{"type": "Multiplier", "var": "ScorchStacks", "limit": limit})
				out["CurrentScorch"] = cur * math.Min(enemy.Sum("BASE", "Multiplier:ScorchStacks"), limit)
			} else {
				enemy.NewMod("ElementalResist", "BASE", -cur, "Scorch", when)
			}
		case "Brittle":
			enemy.NewMod("SelfCritChance", "BASE", cur, "Brittle", when)
		case "Sap":
			enemy.NewMod("Damage", "MORE", -cur, "Sap", when)
		}
		enemy.Add(Mod{Name: "Condition:Already" + a.condition, Type: "FLAG", Value: true, Tags: []map[string]any{when}})
	}
	// Update chill and shock multipliers.
	for _, n := range []string{"Chill", "Shock"} {
		cur, ok := db.output["Current"+n]
		if have := enemy.Sum("BASE", "Multiplier:"+n+"Effect"); ok && have < cur {
			enemy.Add(Mod{Name: "Multiplier:" + n + "Effect", Type: "BASE", Value: cur - have})
		}
	}
}

// sumSkillSource is skillModList:Sum("INC", { source = "Skill" }, name):
// only mods whose source kind is "Skill".
func sumSkillSource(db *ModDB, name string) float64 {
	total := 0.0
	for d := db; d != nil; d = d.parent {
		for i := range d.mods[name] {
			m := &d.mods[name][i]
			if m.Type != "INC" || m.Flags != 0 || m.KeywordFlags != 0 || strings.SplitN(m.Source, ":", 2)[0] != "Skill" {
				continue
			}
			if v, ok := db.eval(m, nil); ok {
				f, _ := num(v)
				total += f
			}
		}
	}
	return total
}

// chillSkills is the chill part of calcs.perform's active skill loop:
// Skitterbots' ailments and the skills that always chill, returned for
// guaranteedChills.
func chillSkills(db *ModDB, skills []*activeSkill) (guaranteed []*activeSkill, guaranteedBonechill bool) { //nolint:nonamedreturns // legacy, kept by pure refactor; see refactor plan part 2
	out := db.output
	for _, s := range skills {
		if s.mods == nil {
			continue
		}
		bonechill := truthy(s.data.val("supportBonechill"))
		if bonechill && (s.types[stChillingArea] || s.types[stNonHitChill] || !s.mods.Flag("CannotChill")) {
			out["HasBonechill"] = 1
		}
		name := s.active.ge.Name
		if name == "Summon Skitterbots" {
			ailmentEffect := s.mods.Sum("INC", "SkitterbotAilmentEffect")
			for _, a := range []struct{ name, cond, cannot string }{
				{"Shock", "Shocked", "SkitterbotsCannotShock"}, {"Chill", "Chilled", "SkitterbotsCannotChill"},
			} {
				if s.mods.Flag(a.cannot) {
					continue
				}
				def := nonDamagingAilment[a.name].def
				effect := def * (1 + (sumSkillSource(s.mods, "Enemy"+a.name+"Effect")+ailmentEffect)/100)
				db.NewMod(a.name+"Override", "BASE", effect, name)
				db.enemy.Add(Mod{Name: "Condition:" + a.cond, Type: "FLAG", Value: true, Source: name})
				if s.mods.Flag("SkitterbotAffectPlayer") {
					db.Add(Mod{Name: a.name, Type: "FLAG", Value: true, Source: name})
					db.NewMod("Self"+a.name+"Override", "BASE", effect, name)
				}
				if a.name == "Chill" && bonechill {
					guaranteedBonechill = true
					db.Add(Mod{Name: "SkitterbotBonechill", Type: "FLAG", Value: true, Source: name})
				}
			}
			if s.mods.Flag("ScorchingSkitterbot") {
				effect := nonDamagingAilment["Scorch"].def * (1 + (sumSkillSource(s.mods, "EnemyScorchEffect")+ailmentEffect)/100)
				db.NewMod("ScorchOverride", "BASE", effect, name)
				db.enemy.Add(Mod{Name: "Condition:Scorched", Type: "FLAG", Value: true, Source: name})
				if s.mods.Flag("SkitterbotAffectPlayer") {
					db.Add(Mod{Name: "Scorch", Type: "FLAG", Value: true, Source: name})
					db.NewMod("SelfScorchOverride", "BASE", effect, name)
				}
			}
		} else if s.flags["guaranteedChill"] || s.types[stChillingArea] || (s.types[stNonHitChill] && !s.mods.Flag("CannotChill")) {
			guaranteed = append(guaranteed, s)
			db.enemy.Add(Mod{Name: "Condition:Chilled", Type: "FLAG", Value: true, Source: name})
			if bonechill {
				guaranteedBonechill = true
			}
		}
	}
	return guaranteed, guaranteedBonechill
}

// guaranteedChills is "Calculate guaranteed chills after buffs".
func guaranteedChills(db *ModDB, skills []*activeSkill) {
	for _, s := range skills {
		effect := nonDamagingAilment["Chill"].def * calcMod(s.mods, s.cfg, "EnemyChillEffect")
		db.NewMod("ChillOverride", "BASE", effect, s.active.ge.Name)
	}
}
