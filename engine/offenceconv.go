package engine

import "math"

// appliedMods is ModStore:Tabulate(typ, cfg, name) returning the mods
// themselves: those whose flags match cfg and whose tags pass.
func appliedMods(list *ModDB, cfg *Cfg, typ, name string) []Mod {
	var out []Mod
	for d := list; d != nil; d = d.parent {
		for i := range d.mods[name] {
			m := &d.mods[name][i]
			if !eligible(m, typ, cfg) {
				continue
			}
			if _, ok := list.eval(m, cfg); ok {
				out = append(out, *m)
			}
		}
	}
	return out
}

// replaceMod is ModStore:ReplaceMod: the first mod with the same name,
// type, flags, keyword flags and source (searching up the parent chain)
// is replaced, else the mod is added.
func (db *ModDB) replaceMod(m Mod) {
	for d := db; d != nil; d = d.parent {
		for i, cur := range d.mods[m.Name] {
			if cur.Type == m.Type && cur.Flags == m.Flags && cur.KeywordFlags == m.KeywordFlags && cur.Source == m.Source {
				d.mods[m.Name][i] = m
				return
			}
		}
	}
	db.Add(m)
}

// offenceStatConversions ports the stat conversions at the start of
// calcs.offence (CalcOffence.lua, from "account for Battlemage" to the
// random physical conversions): mods that apply to other stats.
func (c *Calculator) offenceStatConversions(actor *ModDB, s *activeSkill, w1 hitSource, w2 *hitSource, out map[string]float64) {
	list, cfg, data := s.mods, s.cfg, s.data
	mfClaw, mfUnarmed, mfBow := modFlagByName["Claw"], modFlagByName["Unarmed"], modFlagByName["Bow"]
	improved := func(name string) float64 {
		if v, ok := list.MaxCfg(cfg, name); ok {
			return v / 100
		}
		return 1
	}
	addDamage := func(src hitSource, mult float64, source string) {
		for _, t := range dmgTypeList {
			list.Add(Mod{Name: t + "Min", Type: "BASE", Value: math.Floor(src.min[t] * mult), Flags: mfSpell, Source: source})
			list.Add(Mod{Name: t + "Max", Type: "BASE", Value: math.Floor(src.max[t] * mult), Flags: mfSpell, Source: source})
		}
	}
	hasWeapon1 := actor.items["Weapon 1"] != nil && s.weapons[0] != ""
	// Battlemage.
	if list.Flag("Battlemage") && hasWeapon1 {
		mult := 1.0
		if v, ok := list.MaxCfg(cfg, "MainHandWeaponDamageAppliesToSpells"); ok {
			mult = v / 100
		}
		addDamage(w1, mult, "Battlemage")
	}
	// Spellblade.
	if v, ok := list.MaxCfg(cfg, "OneHandWeaponDamageAppliesToSpells"); ok && hasWeapon1 {
		if info := weaponTypeInfo[s.weapons[0]]; info.melee && info.oneHand {
			_, offHand := weaponTypeInfo[s.weapons[1]]
			mult := v / 100
			if offHand {
				mult *= 0.6
			}
			addDamage(w1, mult, "Spellblade Main Hand")
			if offHand && w2 != nil {
				addDamage(*w2, mult, "Spellblade Off Hand")
			}
		}
	}
	minionMods := func(fn func(m Mod)) {
		for _, e := range list.List(cfg, "MinionModifier") {
			v, _ := e.Value.(map[string]any)
			if mm, ok := v["mod"].(map[string]any); ok {
				fn(modFromMap(mm, e.Mod.Source))
			}
		}
	}
	if list.Flag("MinionDamageAppliesToPlayer") || list.FlagCfg(cfg, "MinionDamageAppliesToPlayer") {
		// Spiritual Aid, The Scourge.
		mult := improved("ImprovedMinionDamageAppliesToPlayer")
		minionMods(func(m Mod) {
			if m.Name == "Damage" && m.Type == "INC" {
				list.Add(Mod{Name: "Damage", Type: "INC", Value: mulValue(m.Value, mult), Flags: m.Flags, KeywordFlags: m.KeywordFlags,
					Source: m.Source, Tags: convertedModTags(m.Tags, mult, true)})
			}
		})
	}
	if list.Flag("MinionAttackSpeedAppliesToPlayer") {
		// Spiritual Command.
		mult := improved("ImprovedMinionAttackSpeedAppliesToPlayer")
		minionMods(func(m Mod) {
			if m.Name == "Speed" && m.Type == "INC" && (m.Flags == 0 || m.Flags&mfAttack != 0) {
				list.Add(Mod{Name: "Speed", Type: "INC", Value: mulValue(m.Value, mult), Flags: mfAttack, KeywordFlags: m.KeywordFlags,
					Source: m.Source, Tags: convertedModTags(m.Tags, mult, true)})
			}
		})
	}
	if list.Flag("MinionCastSpeedAppliesToPlayer") {
		// Spinehail.
		mult := improved("ImprovedMinionCastSpeedAppliesToPlayer")
		minionMods(func(m Mod) {
			if m.Name == "Speed" && m.Type == "INC" && (m.Flags == 0 || m.Flags&mfCast != 0) {
				list.Add(Mod{Name: "Speed", Type: "INC", Value: mulValue(m.Value, mult), Flags: mfCast, KeywordFlags: m.KeywordFlags,
					Source: m.Source, Tags: convertedModTags(m.Tags, mult, true)})
			}
		})
	}
	// copyAs adds each mod again under another name, type and flags, with
	// its tags.
	copyAs := func(mods []Mod, name string, value func(Mod) any, flags func(Mod) uint64) {
		for _, m := range mods {
			list.Add(Mod{Name: name, Type: m.Type, Value: value(m), Flags: flags(m), KeywordFlags: m.KeywordFlags, Source: m.Source, Tags: m.Tags})
		}
	}
	same := func(m Mod) any { return m.Value }
	if list.Flag("EvasionAppliesToSpellDamage") {
		// The Unblinking Eye.
		copyAs(appliedMods(list, &Cfg{}, "INC", "Evasion"), "Damage", same, func(Mod) uint64 { return mfSpell })
	}
	if list.Flag("SpellDamageAppliesToAttacks") || list.FlagCfg(cfg, "SpellDamageAppliesToAttacks") {
		// Crown of Eyes, Kinetic Bolt, Wandslinger.
		mult := improved("ImprovedSpellDamageAppliesToAttacks")
		for _, m := range appliedMods(list, &Cfg{Flags: mfSpell}, "INC", "Damage") {
			if m.Flags&mfSpell == 0 {
				continue
			}
			list.Add(Mod{Name: "Damage", Type: "INC", Value: mulValue(m.Value, mult), Flags: m.Flags&^mfSpell | mfAttack,
				KeywordFlags: m.KeywordFlags, Source: m.Source, Tags: convertedModTags(m.Tags, mult, false)})
			if m.Source == "Strength" {
				list.replaceMod(Mod{Name: "PhysicalDamage", Type: "INC", Value: 0.0, Source: "Strength", Flags: mfMelee})
				list.replaceMod(Mod{Name: "PhysicalDamage", Type: "INC", Value: 0.0, Source: "Strength", Flags: mfAttack | mfProjectile})
			}
		}
	}
	if list.Flag("CastSpeedAppliesToAttacks") {
		mult := improved("ImprovedCastSpeedAppliesToAttacks")
		for _, m := range appliedMods(list, &Cfg{Flags: mfCast}, "INC", "Speed") {
			if m.Flags&mfCast != 0 {
				list.Add(Mod{Name: "Speed", Type: "INC", Value: mulValue(m.Value, mult), Flags: m.Flags&^mfCast | mfAttack,
					KeywordFlags: m.KeywordFlags, Source: m.Source, Tags: convertedModTags(m.Tags, mult, false)})
			}
		}
	}
	if list.Flag("ProjectileSpeedAppliesToBowDamage") {
		copyAs(appliedMods(list, &Cfg{}, "INC", "ProjectileSpeed"), "Damage", same, func(Mod) uint64 { return mfBow | mfHit })
	}
	clawOnly := func(mods []Mod, extra uint64) []Mod {
		var out []Mod
		for _, m := range mods {
			if m.Flags&mfClaw != 0 && m.Flags&extra == extra {
				out = append(out, m)
			}
		}
		return out
	}
	if list.Flag("ClawDamageAppliesToUnarmed") {
		// Rigwald's Curse.
		copyAs(clawOnly(appliedMods(list, &Cfg{Flags: mfClaw | mfHit, KeywordFlags: kfHit}, "INC", "Damage"), 0), "Damage", same,
			func(m Mod) uint64 { return m.Flags&^mfClaw | mfUnarmed | mfMelee })
	}
	if list.Flag("ClawAttackSpeedAppliesToUnarmed") {
		copyAs(clawOnly(appliedMods(list, &Cfg{Flags: mfClaw | mfAttack | mfHit}, "INC", "Speed"), mfAttack), "Speed", same,
			func(m Mod) uint64 { return m.Flags&^mfClaw | mfUnarmed })
	}
	if list.Flag("ClawCritChanceAppliesToUnarmed") {
		copyAs(clawOnly(appliedMods(list, &Cfg{Flags: mfClaw | mfHit}, "INC", "CritChance"), 0), "CritChance", same,
			func(m Mod) uint64 { return m.Flags&^mfClaw | mfUnarmed })
	}
	for _, x := range []struct{ flag, out, stat string }{
		{"CritChanceIncreasedByUncappedLightningRes", "CritChance", "LightningResistTotal"},
		{"CritChanceIncreasedByLightningRes", "CritChance", "LightningResist"},
		{"CritChanceIncreasedByOvercappedLightningRes", "CritChance", "LightningResistOverCap"},
		{"CritChanceIncreasedBySpellSuppressChance", "CritChance", "SpellSuppressionChance"},
		{"FirePenIncreasedByUncappedFireRes", "FirePenetration", "FireResistOverCap"},
	} {
		if !list.Flag(x.flag) {
			continue
		}
		if rows := actor.tabulateType(x.flag, "FLAG"); len(rows) > 0 {
			typ := "INC"
			if x.out == "FirePenetration" {
				typ = "BASE"
			}
			list.Add(Mod{Name: x.out, Type: typ, Value: actor.output[x.stat], Source: rows[0].Source})
		}
	}
	keep := func(m Mod) uint64 { return m.Flags }
	if list.Flag("LightRadiusAppliesToAccuracy") {
		copyAs(appliedMods(list, &Cfg{}, "INC", "LightRadius"), "Accuracy", same, keep)
	}
	if list.Flag("LightRadiusAppliesToAreaOfEffect") {
		copyAs(appliedMods(list, &Cfg{}, "INC", "LightRadius"), "AreaOfEffect",
			func(m Mod) any { f, _ := num(m.Value); return math.Floor(f / 2) }, keep)
	}
	if list.Flag("LightRadiusAppliesToDamage") {
		copyAs(appliedMods(list, &Cfg{}, "INC", "LightRadius"), "Damage", same, keep)
	}
	if list.Flag("SequentialProjectiles") && !list.Flag("OneShotProj") && !list.Flag("NoAdditionalProjectiles") &&
		!list.Flag("SingleProjectile") && !list.Flag("TriggeredBySnipe") {
		data["dpsMultiplier"] = list.SumCfg(cfg, "BASE", "ProjectileCount")
	}
	repeats(actor, s, out)
	if list.Flag("WeaponPhysAppliesToSpells") {
		// Runegraft of the Spellbound.
		// PoB: (Sum(...) or 100) / 100, and Sum is never nil.
		mult := list.SumCfg(cfg, "BASE", "WeaponPhysAppliesToSpellsPercent") / 100
		list.Add(Mod{Name: "PhysicalMin", Type: "BASE", Value: math.Floor(w1.min["Physical"] * mult), Flags: mfSpell, Source: "Runegraft of the Spellbound"})
		list.Add(Mod{Name: "PhysicalMax", Type: "BASE", Value: math.Floor(w1.max["Physical"] * mult), Flags: mfSpell, Source: "Runegraft of the Spellbound"})
	}
	if pct, ok := num(data["gainPercentBaseWandDamageToSpells"]); ok {
		mult := pct / 100
		wand1, wand2 := s.weapons[0] == "Wand", s.weapons[1] == "Wand" && w2 != nil
		switch {
		case wand1 && wand2:
			for _, t := range dmgTypeList {
				list.Add(Mod{Name: t + "Min", Type: "BASE", Value: math.Floor((w1.min[t] + w2.min[t]) / 2 * mult), Flags: mfSpell, Source: "Spellslinger"})
				list.Add(Mod{Name: t + "Max", Type: "BASE", Value: math.Floor((w1.max[t] + w2.max[t]) / 2 * mult), Flags: mfSpell, Source: "Spellslinger"})
			}
		case wand1:
			addDamage(w1, mult, "Spellslinger")
		case wand2:
			addDamage(*w2, mult, "Spellslinger")
		}
	}
	if pct, ok := num(data["gainPercentBaseDaggerDamageToSpells"]); ok {
		d1, d2 := s.weapons[0] == "Dagger", s.weapons[1] == "Dagger" && w2 != nil
		mult := pct / 100
		if d1 && d2 {
			mult *= 0.5
		}
		for _, t := range dmgTypeList {
			var mn, mx float64
			if d1 {
				mn, mx = mn+w1.min[t], mx+w1.max[t]
			}
			if d2 {
				mn, mx = mn+w2.min[t], mx+w2.max[t]
			}
			list.Add(Mod{Name: t + "Min", Type: "BASE", Value: math.Floor(mn * mult), Flags: mfSpell, Source: "Blade Blast of Dagger Detonation"})
			list.Add(Mod{Name: t + "Max", Type: "BASE", Value: math.Floor(mx * mult), Flags: mfSpell, Source: "Blade Blast of Dagger Detonation"})
		}
	}
	unleashSeals(actor, s, out)
	c.randomPhysConversions(s)
}

// unleashSeals is the "Applies DPS multiplier based on seals count" block
// of calcs.offence (Unleash supports).
func unleashSeals(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, cfg, data := s.mods, s.cfg, s.data
	if !list.Flag("HasSeals") || !s.types[stCanRapidFire] || list.Flag("NoRepeatBonuses") {
		return
	}
	cooldown := list.SumCfg(cfg, "BASE", "SealGainFrequency") / calcMod(list, cfg, "SealGainFrequency")
	sealMax := list.SumCfg(cfg, "BASE", "SealCount")
	out["SealCooldown"], out["SealMax"], out["AverageBurstHits"], out["TimeMaxSeals"] = cooldown, sealMax, sealMax, cooldown*sealMax
	if _, ok := data["hitTimeOverride"]; ok {
		return
	}
	castRate := 1 / s.active.ge.CastTime * 1.1 * calcMod(list, cfg, "Speed") * actor.output["ActionSpeedMod"]
	if list.Flag("UseMaxUnleash") {
		for _, m := range appliedMods(list, &Cfg{}, "INC", "MaxSealCrit") {
			m.Name = "CritChance"
			list.Add(m)
		}
		for _, m := range appliedMods(list, &Cfg{}, "MORE", "MaxSealDamage") {
			v, _ := num(m.Value)
			m.Name, m.Value = "Damage", v*sealMax
			list.Add(m)
		}
		data["dpsMultiplier"] = 1 + sealMax*calcMod(list, cfg, "SealRepeatPenalty")
		data["hitTimeOverride"] = math.Max(cooldown*sealMax, castRate)
	} else {
		data["dpsMultiplier"] = 1 + 1/cooldown/castRate*calcMod(list, cfg, "SealRepeatPenalty")
	}
}

// randomPhysConversions is the random physical conversion block of
// calcs.offence with physMode "AVERAGE" (the default; the config option is
// not read).
func (c *Calculator) randomPhysConversions(s *activeSkill) {
	list := s.mods
	processed := map[*Mod]bool{}
	for _, cfg := range []*Cfg{s.cfg, s.weapon1Cfg, s.weapon2Cfg} {
		if cfg == nil || list.SumCfg(cfg, "BASE", "PhysicalDamageGainAsRandom", "PhysicalDamageConvertToRandom", "PhysicalDamageGainAsColdOrLightning") <= 0 {
			continue
		}
		s.flags["randomPhys"] = true
		for _, x := range []struct {
			name string
			to   []string
		}{
			{"PhysicalDamageGainAsRandom", []string{"PhysicalDamageGainAsFire", "PhysicalDamageGainAsCold", "PhysicalDamageGainAsLightning"}},
			{"PhysicalDamageConvertToRandom", []string{"PhysicalDamageConvertToFire", "PhysicalDamageConvertToCold", "PhysicalDamageConvertToLightning"}},
			{"PhysicalDamageGainAsColdOrLightning", []string{"PhysicalDamageGainAsCold", "PhysicalDamageGainAsLightning"}},
		} {
			for d := list; d != nil; d = d.parent {
				for i := range d.mods[x.name] {
					m := &d.mods[x.name][i]
					if processed[m] || !eligible(m, "BASE", cfg) {
						continue
					}
					if _, ok := list.eval(m, cfg); !ok {
						continue
					}
					processed[m] = true
					v := mulValue(m.Value, 1/float64(len(x.to)))
					for _, to := range x.to {
						list.Add(Mod{Name: to, Type: "BASE", Value: v, Flags: m.Flags, KeywordFlags: m.KeywordFlags, Source: m.Source, Tags: m.Tags})
					}
				}
			}
		}
	}
}

// mulValue multiplies a numeric mod value.
func mulValue(v any, mult float64) any {
	if f, ok := num(v); ok {
		return f * mult
	}
	return v
}

// repeats is the Repeats block of calcs.offence: the repeat count and the
// damage and area mods of repeating supports (Spell Echo, Multistrike),
// for the config's repeatMode (default AVERAGE).
func repeats(actor *ModDB, s *activeSkill, out map[string]float64) {
	list, cfg := s.mods, s.cfg
	out["Repeats"] = 1
	for _, t := range []int{stInstant, stChannel, stTriggered, stRetaliation, stNonRepeatable} {
		if s.types[t] {
			return
		}
	}
	if list.Flag("CannotRepeat") || !(s.types[stAttack] || s.types[stSpell]) {
		return
	}
	n := 1 + list.SumCfg(cfg, "BASE", "RepeatCount")
	out["Repeats"] = n
	if n <= 1 {
		return
	}
	out["RepeatCount"] = n
	mode := "AVERAGE"
	if b := actor.theBuild(); b != nil {
		mode = configString(b.ActiveConfig(), "repeatMode", "AVERAGE")
	}
	if mode == "NONE" {
		return
	}
	avg := mode == "AVERAGE"
	add := func(m Mod, name, typ string, v float64) {
		list.Add(Mod{Name: name, Type: typ, Value: v, Flags: m.Flags, KeywordFlags: m.KeywordFlags, Source: m.Source, Tags: m.Tags})
	}
	val := func(m Mod) float64 { f, _ := num(m.Value); return f }
	for _, m := range appliedMods(list, cfg, "INC", "RepeatFinalAreaOfEffect") {
		v := val(m)
		if avg {
			v /= n
		}
		add(m, "AreaOfEffect", "INC", v)
	}
	for _, m := range appliedMods(list, cfg, "INC", "RepeatPerRepeatAreaOfEffect") {
		v := val(m) * (n - 1)
		if avg {
			v /= 2
		}
		add(m, "AreaOfEffect", "INC", v)
	}
	for _, m := range appliedMods(list, cfg, "BASE", "RepeatFinalDoubleDamageChance") {
		v := val(m)
		if avg {
			v /= n
		}
		add(m, "DoubleDamageChance", "BASE", v)
	}
	finalMore, moreTotal := 1.0, 0.0
	for _, m := range appliedMods(list, cfg, "MORE", "RepeatFinalDamage") {
		v := val(m)
		finalMore *= 1 + v/100
		moreTotal += v
		if avg && !list.Flag("OnlyFinalRepeat") {
			v /= n
		}
		add(m, "Damage", "MORE", v)
	}
	for _, m := range appliedMods(list, cfg, "MORE", "RepeatPerRepeatDamage") {
		v := val(m) * (n - 1)
		if avg {
			if finalMore != 1 {
				v = ((100+val(m)*(n-2)/2)*(n-1)+(100+val(m)*(n-1))*finalMore)/(n+moreTotal/100) - 100
			} else {
				v /= 2
			}
		}
		add(m, "Damage", "MORE", v)
	}
	var last *Mod
	finalMore = moreTotal
	for i, name := range []string{"One", "Two", "Three"} {
		count := float64(i + 2)
		if count > n {
			break
		}
		if avg {
			for _, m := range appliedMods(list, cfg, "MORE", "Repeat"+name+"Damage") {
				moreTotal += val(m)
				mc := m
				last = &mc
			}
		} else if count == n {
			for _, m := range appliedMods(list, cfg, "MORE", "Repeat"+name+"Damage") {
				add(m, "Damage", "MORE", val(m))
			}
		}
	}
	if avg && last != nil {
		add(*last, "Damage", "MORE", (moreTotal/n+100)/(1+finalMore/n/100)-100)
	}
	if list.Flag("FinalRepeatSumsDamage") {
		for _, m := range appliedMods(list, cfg, "FLAG", "FinalRepeatSumsDamage") {
			add(m, "Damage", "MORE", (100*n+finalMore)/(1+finalMore/100)-100)
		}
	}
	if s.flags["trap"] || s.flags["mine"] {
		list.NewMod("DPS", "MORE", (n-1)*100, "Repeat Count")
	}
}

type convEntry struct {
	to   map[string]float64 // conversion + gain, per destination
	mult float64            // what stays
}

// conversionTable is buildConversionTable.
//
//nolint:mnd,intrange,modernize // moved verbatim from offence.go (Task 6 split); PoB's percent literals stay inline
func conversionTable(list *ModDB, cfg *Cfg) map[string]*convEntry {
	out := map[string]*convEntry{}
	for i := 0; i < 4; i++ {
		t := dmgTypeList[i]
		global, skill, add := map[string]float64{}, map[string]float64{}, map[string]float64{}
		var globalTotal, skillTotal float64
		for _, o := range dmgTypeList[i+1:] {
			names := []string{t + "DamageConvertTo" + o}
			gainNames := []string{t + "DamageGainAs" + o}
			if isElemental[t] {
				names = append(names, "ElementalDamageConvertTo"+o)
				gainNames = append(gainNames, "ElementalDamageGainAs"+o)
			}
			names = append(names, "NonChaosDamageConvertTo"+o)
			gainNames = append(gainNames, "NonChaosDamageGainAs"+o)
			global[o] = math.Max(list.SumCfg(cfg, "BASE", names...), 0)
			globalTotal += global[o]
			skill[o] = math.Max(list.SumCfg(cfg, "BASE", "Skill"+t+"DamageConvertTo"+o), 0)
			skillTotal += skill[o]
			add[o] = math.Max(list.SumCfg(cfg, "BASE", gainNames...), 0)
		}
		if skillTotal > 100 {
			for o := range skill {
				skill[o] *= 100 / skillTotal
				global[o] = 0
			}
		} else if globalTotal+skillTotal > 100 {
			f := (100 - skillTotal) / globalTotal
			for o := range global {
				global[o] *= f
			}
		}
		e := &convEntry{to: map[string]float64{}}
		converted := 0.0
		for o := range global {
			conv := round(global[o]+skill[o], 0)
			e.to[o] = (conv + add[o]) / 100
			converted += conv
		}
		e.mult = 1 - math.Min(converted/100, 1)
		out[t] = e
	}
	out["Chaos"] = &convEntry{to: map[string]float64{}, mult: 1}
	return out
}
