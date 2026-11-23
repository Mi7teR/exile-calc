package main

import (
	"errors"
	"fmt"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/internal/luadata"
)

// convertMinions reads Data/Minions.lua and Data/Spectres.lua into
// Data.lua's data.minions (spectres get limit ActiveSpectreLimit).
func convertMinions(read func(string) (string, error), env map[string]luadata.Value) (map[string]*gamedata.Minion, error) {
	out := map[string]*gamedata.Minion{}
	for _, f := range []string{"Data/Minions.lua", "Data/Spectres.lua"} {
		src, err := read(f)
		if err != nil {
			return nil, err
		}
		v, err := luadata.Eval(src, env)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		t, ok := v.(*luadata.Table)
		if !ok {
			return nil, fmt.Errorf("%s: not a table", f)
		}
		for _, k := range t.Keys {
			id := keyText(k)
			mt, ok := t.Get(k).(*luadata.Table)
			if !ok {
				return nil, fmt.Errorf("%s: %s is not a table", f, id)
			}
			m, err := convertMinion(mt)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", f, id, err)
			}
			if f == "Data/Spectres.lua" {
				m.Limit = "ActiveSpectreLimit"
			}
			out[id] = m
		}
	}
	return out, nil
}

func convertMinion(t *luadata.Table) (*gamedata.Minion, error) {
	str := func(k string) string { v, _ := t.Get(k).(luadata.String); return string(v) }
	f := func(k string) float64 { v, _ := t.Get(k).(luadata.Number); return float64(v) }
	b, _ := t.Get("hostile").(luadata.Bool)
	m := &gamedata.Minion{
		Name: sanitiseText(str("name")), Life: f("life"), LifeScaling: str("lifeScaling"),
		EnergyShield: f("energyShield"), Armour: f("armour"), Evasion: f("evasion"),
		FireResist: f("fireResist"), ColdResist: f("coldResist"), LightningResist: f("lightningResist"),
		ChaosResist: f("chaosResist"), Limit: str("limit"), Hostile: bool(b),
		Damage: f("damage"), DamageSpread: f("damageSpread"), AttackTime: f("attackTime"),
		AttackRange: f("attackRange"), Accuracy: f("accuracy"), WeaponType1: str("weaponType1"),
	}
	ign, _ := t.Get("baseDamageIgnoresAttackSpeed").(luadata.Bool)
	m.BaseDamageIgnoresAttackSpeed = bool(ign)
	if err := luadata.Decode(t.Get("skillList"), &m.SkillList); err != nil {
		return nil, fmt.Errorf("skillList: %w", err)
	}
	if ml, ok := t.Get("modList").(*luadata.Table); ok {
		for _, v := range ml.List {
			mod, err := skillMod(v)
			if errors.Is(err, errBadFlags) {
				continue
			}
			if err != nil {
				return nil, err
			}
			m.ModList = append(m.ModList, mod)
		}
	}
	return m, nil
}
