package engine

import (
	"math"
	"strings"
)

const fortifyBaseDuration = 6.0 // Data.lua data.misc.FortifyBaseDuration

// nonDamagingAilment is the part of Data.lua data.nonDamagingAilment that
// self ailments read: the default and maximum effect.
var nonDamagingAilment = map[string]struct{ def, max float64 }{
	"Chill": {10, 30}, "Shock": {15, 50}, "Scorch": {10, 30},
}

// maxSource is ModStore:Max: the largest MAX mod, or false if there is
// none above zero. With source set, only mods from that source kind
// ("Skill" for "Skill:<id>") count.
func maxSource(db *ModDB, source string, name string) (float64, bool) {
	best, found := 0.0, false
	for _, r := range db.tabulateType(name, "MAX") {
		if source != "" && strings.SplitN(r.Source, ":", 2)[0] != source {
			continue
		}
		if f, ok := num(r.Value); ok && f > best {
			best, found = f, true
		}
	}
	return best, found
}

// actorMisc ports the buffs and debuffs of doActorMisc (CalcPerform.lua
// 646-975) for an actor with main skill main (actor.mainSkill), before its
// rage part, which rage() ports. pm is env.player.mainSkill, whose base
// mods doActorMisc reads for Cruelty whichever actor it runs for.
//
//nolint:gocognit,gocyclo,cyclop,funlen // one branch per buff or debuff, in doActorMisc's order
func actorMisc(db *ModDB, main, pm *activeSkill, silverFlask bool, silverFlaskInc float64) {
	out := db.output
	cond := db.conditions
	mainMods, mainCfg := db, (*Cfg)(nil)
	if main != nil && main.mods != nil {
		mainMods, mainCfg = main.mods, main.cfg
	}
	buffEffect := func(names ...string) float64 {
		return 1 + db.Sum("INC", append(names, "BuffEffectOnSelf")...)/100
	}
	speed := func(typ string, v float64, source string) {
		db.Add(Mod{Name: "Speed", Type: typ, Value: v, Flags: mfAttack, Source: source})
		db.Add(Mod{Name: "Speed", Type: typ, Value: v, Flags: mfCast, Source: source})
	}

	// CalcPerform.lua:648: env.player.mainSkill.baseSkillModList.
	if pm != nil && pm.mods != nil && pm.mods.FlagCfg(pm.cfg, "Cruelty") {
		if o, ok := db.Override("Cruelty"); ok {
			db.multipliers["Cruelty"] = o
		} else {
			db.multipliers["Cruelty"] = 40
		}
	}
	if m := db.Sum("BASE", "MinimumRage"); m > db.multipliers["Rage"] {
		db.multipliers["Rage"] = m
	}
	allied := alliedFortify(db)
	if db.Sum("BASE", "MinimumFortification") > 0 || allied > 0 {
		cond["Fortified"] = true
	}
	if db.Flag("Fortified") || db.Sum("BASE", "Multiplier:Fortification") > 0 {
		maxStacks, ok := db.Override("MaximumFortification")
		if !ok {
			maxStacks = db.SumCfg(mainCfg, "BASE", "MaximumFortification")
		}
		maxStacks = math.Max(maxStacks, allied)
		minStacks := db.Sum("BASE", "MinimumFortification")
		if db.Flag("Condition:HaveMaxFortification") {
			minStacks = maxStacks
		}
		minStacks = math.Min(minStacks, maxStacks)
		set, ok := db.Override("FortificationStacks")
		if !ok {
			set = maxStacks
		}
		stacks := math.Min(set, maxStacks)
		dur := fortifyBaseDuration
		if o, ok := mainMods.OverrideCfg(nil, "FortifyDuration"); ok {
			dur = o
		}
		out["MaximumFortification"] = maxStacks
		out["MinimumFortification"] = minStacks
		out["RemovableFortification"] = math.Min(maxStacks-minStacks, set-minStacks)
		out["FortificationStacks"] = stacks
		out["FortificationStacksOver20"] = math.Min(math.Max(0, stacks-20), maxStacks-20)
		out["FortifyDuration"] = dur * (1 + mainMods.SumCfg(nil, "INC", "FortifyDuration")/100)
		if !db.Flag("Condition:NoFortificationMitigation") {
			out["FortificationEffect"] = stacks
			db.NewMod("DamageTakenWhenHit", "MORE", -stacks, "Fortification")
		}
		if stacks >= maxStacks {
			db.Add(Mod{Name: "Condition:HaveMaximumFortification", Type: "FLAG", Value: true})
		}
		db.multipliers["BuffOnSelf"]++
	}
	if db.Flag("Onslaught") {
		inc := db.Sum("INC", "OnslaughtEffect", "BuffEffectOnSelf") / 100
		if silverFlask {
			inc += silverFlaskInc
		}
		effect := math.Floor(20 * (1 + inc))
		speed("INC", effect, "Onslaught")
		db.NewMod("MovementSpeed", "INC", effect, "Onslaught")
	}
	if cond["AffectedByArcaneSurge"] || db.Flag("Condition:ArcaneSurge") {
		cond["AffectedByArcaneSurge"] = true
		effect := buffEffect("ArcaneSurgeEffect")
		effect += db.SumCfg(&Cfg{Actor: "player"}, "INC", "FlaskEffect") / 100 * db.Sum("BASE", "FlaskEffectToArcaneSurgeEffect") / 100
		regen, ok := maxSource(db, "", "ArcaneSurgeManaRegen")
		if !ok {
			regen = 30
		}
		db.NewMod("ManaRegen", "INC", regen*effect, "Arcane Surge")
		cast, ok := maxSource(db, "", "ArcaneSurgeCastSpeed")
		if !ok {
			cast = 20
		}
		cast *= effect
		db.Add(Mod{Name: "Speed", Type: "INC", Value: cast, Flags: mfCast, Source: "Arcane Surge"})
		if db.Flag("ArcaneSurgeCastSpeedToMovementSpeed") {
			db.NewMod("MovementSpeed", "INC", cast, "Arcane Surge")
		}
		if d, ok := maxSource(db, "", "ArcaneSurgeDamage"); ok {
			db.Add(Mod{Name: "Damage", Type: "MORE", Value: d * effect, Flags: mfSpell, Source: "Arcane Surge"})
		}
		for _, x := range []struct{ name, typ string }{{"LifeRegen", "INC"}, {"ManaRecoup", "BASE"}, {"ManaCostEfficiency", "INC"}} {
			if v := db.Sum(x.typ, "ArcaneSurgeAlso"+x.name); v != 0 {
				db.NewMod(x.name, x.typ, v*effect, "Arcane Surge")
			}
		}
	}
	if db.Flag("Fanaticism") && main != nil && main.flags["selfCast"] {
		effect := math.Floor(75 * buffEffect())
		db.Add(Mod{Name: "Speed", Type: "MORE", Value: effect, Flags: mfCast, Source: "Fanaticism"})
		db.Add(Mod{Name: "Cost", Type: "MORE", Value: -effect, Flags: mfCast, Source: "Fanaticism"})
		db.Add(Mod{Name: "AreaOfEffect", Type: "INC", Value: effect, Flags: mfCast, Source: "Fanaticism"})
	}
	if db.Flag("Condition:CanGainSpiritInfusion") {
		mult := map[string]any{"type": "Multiplier", "var": "SpiritInfusion"}
		global := map[string]any{"type": "GlobalEffect", "effectType": "Buff", "effectName": "Spirit Infusion", "unscalable": true}
		db.NewMod("EnergyShieldRechargeFaster", "INC", 15, "Spirit Infusion", mult, global)
		channel := map[string]any{"type": "SkillType", "skillType": float64(stChannel)} //nolint:goconst // PoB tag
		for _, x := range []struct {
			name string
			v    float64
		}{{"Damage", 5}, {"Cost", 10}} {
			db.Add(Mod{Name: x.name, Type: "MORE", Value: x.v, KeywordFlags: kfSpell, Source: "Spirit Infusion",
				Tags: []map[string]any{channel, mult, global}})
		}
	}
	if db.Flag("UnholyMight") {
		effect := buffEffect()
		db.NewMod("PhysicalDamageConvertToChaos", "BASE", math.Floor(100*effect), "Unholy Might")
		db.Add(Mod{Name: "Condition:CanWither", Type: "FLAG", Value: true, Source: "Unholy Might"})
		if v := db.Sum("BASE", "UnholyMightAlsoChaosPenetration"); v != 0 {
			db.NewMod("ChaosPenetration", "BASE", v*effect, "Unholy Might")
		}
	}
	if db.Flag("ShepherdOfSouls") {
		vaal := map[string]any{"type": "SkillType", "skillType": float64(stVaal)}
		//nolint:goconst // PoB tag
		notAura := map[string]any{"type": "SkillType", "skillType": float64(stAura), "neg": true}
		db.NewMod("SoulCost", "MORE", -80, "Shepherd of Souls", vaal, notAura)
		db.NewMod("SoulCost", "INC", 100, "Shepherd of Souls", vaal, notAura, //nolint:mnd // PoB literal
			map[string]any{"type": "Multiplier", "var": "VaalSkillsUsedInPast8Seconds"})
	}
	if db.Flag("ChaoticMight") {
		db.NewMod("PhysicalDamageGainAsChaos", "BASE", math.Floor(30*buffEffect()), "Chaotic Might")
	}
	if db.Flag("Tailwind") {
		db.NewMod("ActionSpeed", "INC", math.Floor(8*buffEffect("TailwindEffectOnSelf")), "Tailwind")
	}
	if db.Flag("Condition:TotemTailwind") {
		db.NewMod("TotemActionSpeed", "INC", 8, "Tailwind")
	}
	if db.Flag("Adrenaline") {
		e := buffEffect()
		db.NewMod("Damage", "INC", math.Floor(100*e), "Adrenaline")
		speed("INC", math.Floor(25*e), "Adrenaline")
		db.NewMod("MovementSpeed", "INC", math.Floor(25*e), "Adrenaline")
		db.NewMod("PhysicalDamageReduction", "BASE", math.Floor(10*e), "Adrenaline")
	}
	if db.Flag("Condition:WildSavagery") && db.Flag("WildSavagery") {
		db.NewMod("PhysicalDamage", "INC", 100, "Wild Savagery")
		db.NewMod("ActionSpeed", "INC", 10, "Wild Savagery")
		db.Add(Mod{Name: "IgnoreEnemyPhysicalDamageReduction", Type: "FLAG", Value: true, Source: "Wild Savagery"})
		db.Add(Mod{Name: "StunImmune", Type: "FLAG", Value: true, Source: "Wild Savagery"})
	}
	if db.Flag("Convergence") {
		db.NewMod("ElementalDamage", "MORE", math.Floor(30*buffEffect()), "Convergence")
	}
	if db.Flag("HerEmbrace") {
		cond["HerEmbrace"] = true
		for _, n := range []string{"AvoidStun", "AvoidFreeze", "AvoidChill", "AvoidIgnite"} {
			db.NewMod(n, "BASE", 100, "Her Embrace")
		}
		speed("INC", 20, "Her Embrace")
		db.NewMod("MovementSpeed", "INC", 20, "Her Embrace")
	}
	if db.Flag("Condition:OnConsecratedGround") {
		effect := 1 + db.Sum("INC", "ConsecratedGroundEffect")/100
		db.NewMod("LifeRegenPercent", "BASE", 5*effect, "Consecrated Ground")
		db.NewMod("Accuracy", "INC", math.Floor(db.Sum("INC", "ConsecratedGroundAlsoAccuracy")*effect), "Consecrated Ground")
	}
	if db.Flag("Condition:PhantasmalMight") {
		limit, ok := out["ActivePhantasmLimit"]
		if !ok {
			limit = 1
		}
		db.multipliers["BuffOnSelf"] += limit - 1
	}
	if db.Flag("Elusive") {
		elusive(db, main)
	}
	if effect, ok := db.MaxCfg(nil, "WitherEffectStack"); ok {
		db.Add(Mod{Name: "Condition:CanWither", Type: "FLAG", Value: true, Source: "Config"})
		db.enemy.NewMod("ChaosDamageTaken", "INC", effect, "Withered", map[string]any{"type": "Multiplier", "var": "WitheredStack", "limit": 15.0})
	}
	if db.Flag("Condition:CanBeWithered") {
		effect := 6 * (100 + db.Sum("INC", "WitherEffectOnSelf")) / 100 * db.More("WitherEffectOnSelf")
		db.NewMod("ChaosDamageTaken", "INC", effect, "Withered", map[string]any{"type": "Multiplier", "var": "WitheredStack", "limit": 15.0})
	}
	if e := db.Sum("BASE", "CoveredInAshEffect"); e > 0 && db.enemy != nil {
		db.enemy.NewMod("FireDamageTaken", "INC", math.Min(e, 20), "Covered in Ash")
	}
	if e := db.Sum("BASE", "CoveredInFrostEffect"); e > 0 && db.enemy != nil {
		db.enemy.NewMod("ColdDamageTaken", "INC", math.Min(e, 20), "Covered in Frost")
	}
	if db.Flag("Excommunicated") {
		db.NewMod("ChaosDamage", "MORE", -100, "Excommunicated")
	}
	if db.Flag("Blind") && !db.Flag("CannotBeBlinded") && !db.Flag("UnaffectedByBlind") {
		effect := buffEffect("BlindEffect")
		if o, ok := db.Override("BlindEffect"); ok {
			effect = math.Min(o/100, effect)
		}
		db.NewMod("Accuracy", "MORE", math.Floor(-20*effect), "Blind")
		db.NewMod("Evasion", "MORE", math.Floor(-20*effect), "Blind")
	}
	if effect, ok := selfAilment(db, "Chill", true); ok && db.Flag("Chill") {
		sign := -1.0
		if db.Flag("SelfChillEffectIsReversed") {
			sign = 1
		}
		if db.Flag("SkitterbotBonechill") {
			db.NewMod("ColdDamageTaken", "INC", -sign*effect, "Bonechill")
		}
		db.NewMod("ActionSpeed", "INC", sign*effect, "Chill")
	}
	if effect, ok := selfAilment(db, "Shock", false); ok && db.Flag("Shock") {
		db.NewMod("DamageTaken", "INC", effect, "Shock")
	}
	if effect, ok := selfAilment(db, "Scorch", true); ok && db.Flag("Scorch") {
		db.NewMod("ElementalResist", "BASE", -effect, "Scorch")
	}
	if db.Flag("Freeze") {
		effect := math.Max(math.Floor(70*calcMod(db, nil, "SelfChillEffect")), 0)
		db.NewMod("ActionSpeed", "INC", -effect, "Freeze")
	}
	if db.Flag("CanLeechLifeOnFullLife") && !db.Flag("GhostReaver") {
		cond["Leeching"], cond["LeechingLife"] = true, true
	}
	if db.Flag("CanLeechEnergyShieldOnFullEnergyShield") {
		cond["Leeching"], cond["LeechingEnergyShield"] = true, true
	}
	if db.Flag("HasMalediction") {
		db.NewMod("DamageTaken", "INC", 10, "Malediction")
		db.NewMod("Damage", "INC", -10, "Malediction")
	}
	if db.Flag("HasMaddeningPresence") {
		db.NewMod("ActionSpeed", "INC", -10, "Maddening Presence")
		db.NewMod("Damage", "INC", -10, "Maddening Presence")
	}
	if db.Flag("HasShapersPresence") {
		db.NewMod("BuffExpireFaster", "MORE", -20, "Shapers Presence")
	}
	// CalcPerform.lua:937-950: the configured Mana Burn stacks, on every
	// actor.
	if n := configNum(db, "multiplierManaBurnStacks"); n > 0 {
		maxBurn := db.Sum("BASE", "MaxManaBurnStacks")
		if maxBurn == 0 {
			maxBurn = 9999
		}
		stacks := math.Min(n, maxBurn)
		db.NewMod("Multiplier:ManaBurnStacks", "BASE", stacks, "Config")
		stacks += db.SumCfg(&Cfg{Actor: "player"}, "BASE", "EffectiveManaBurnStacks")
		if db.Flag("Condition:WeepingWoundsInsteadOfManaBurn") {
			db.NewMod("Multiplier:WeepingWoundsStacks", "BASE", stacks, "Config")
		} else {
			db.NewMod("Multiplier:EffectiveManaBurnStacks", "BASE", stacks, "Config")
		}
	}
	// CalcPerform.lua doActorMisc: Soul Eater, 5% attack and cast speed per
	// stack up to SoulEaterMax.
	if db.Flag("Condition:CanHaveSoulEater") {
		limit, ok := db.Override("SoulEaterMax")
		if !ok {
			limit = db.Sum("BASE", "SoulEaterMax")
		}
		for _, f := range []uint64{mfAttack, mfCast} {
			tag := map[string]any{"type": "Multiplier", "var": "SoulEaterStack", "limit": limit}
			db.Add(Mod{
				Name: "Speed", Type: "INC", Value: soulEaterSpeedPerStack, Flags: f, Tags: []map[string]any{tag},
				Source: "Base", //nolint:goconst // PoB source
			})
		}
	}
}

// silverFlask is an active Silver Flask of env.flasks: its own effect
// (flaskData.effectInc) and whether it is magic.
type silverFlask struct {
	localInc float64
	magic    bool
}

// silverOnslaught is doActorMisc's Silver Flask check for the Onslaught
// effect (CalcPerform.lua:686-703) on an actor other than the player: the
// player's active Silver Flasks with the actor's own flask effect mods.
func silverOnslaught(db *ModDB, flasks []silverFlask) (bool, float64) {
	inc := -1.0 // -100%, so that reduced effect still applies
	player := &Cfg{Actor: "player"}
	for _, f := range flasks {
		cur := f.localInc + db.SumCfg(player, "INC", "FlaskEffect")
		if f.magic {
			cur += db.SumCfg(player, "INC", "MagicUtilityFlaskEffect")
		}
		inc = math.Max(inc, cur/100) //nolint:mnd // percent
	}
	return len(flasks) > 0, inc
}

// alliedFortify is doActorMisc's alliedFortify (CalcPerform.lua:655): the
// parent's fortification for a minion with YourFortifyEqualToParent, else
// the party member's with PartyMemberFortifyEqualToYours, else 0.
func alliedFortify(db *ModDB) float64 {
	if db.Flag("YourFortifyEqualToParent") && db.owner != nil {
		if v, ok := db.owner.output["FortificationStacks"]; ok {
			return v
		}
	}
	p := db.root()
	if db.owner != nil {
		p = db.owner.root() // env.partyMembers belongs to the env
	}
	if pm := p.partyMembers; pm != nil && pm.Flag("PartyMemberFortifyEqualToYours") {
		if v, ok := pm.output["FortificationStacks"]; ok {
			return v
		}
	}
	return 0
}

// selfAilment is the effect of Chill, Shock or Scorch on the actor;
// shockAvoid says whether ShockAvoidAppliesToElementalAilments counts.
// The effect is the larger of Self<X>Override and the <X>Val override, or,
// for the player, the ailment's default effect when neither is set (the
// enemy's ailments come from enemyAilments, which already applied them).
// PoB differs: its m_max(Sum, Override()) or default never reaches the
// default (Lua passes one argument when there is no override, and a number
// is never nil), so a configured ailment without a value has no effect.
func selfAilment(db *ModDB, name string, shockAvoid bool) (float64, bool) {
	a := nonDamagingAilment[name]
	value := db.Sum("BASE", "Self"+name+"Override")
	if o, ok := db.Override(name + "Val"); ok {
		value = math.Max(value, o)
	} else if value == 0 && db.build != nil && db.owner == nil { // the player
		value = a.def
	}
	if db.Flag(name+"Immune", "ElementalAilmentImmune") {
		return 0, true
	}
	avoid := db.Sum("BASE", "Avoid"+name, "AvoidAilments", "AvoidElementalAilments")
	if shockAvoid && db.Flag("ShockAvoidAppliesToElementalAilments") {
		avoid += db.Sum("BASE", "AvoidShock")
	}
	if db.Flag("SpellSuppressionAppliesToAilmentAvoidance") {
		avoid += db.Sum("BASE", "SpellSuppressionChance") / 2
	}
	if math.Floor(math.Min(avoid, 100)) == 100 {
		return 0, true
	}
	max := a.max
	if o, ok := db.Override(name + "Max"); ok {
		max = o
	}
	return math.Min(math.Max(math.Floor(value*calcMod(db, nil, "Self"+name+"Effect")), 0), max), true
}

// elusive is the Elusive part of doActorMisc.
func elusive(db *ModDB, main *activeSkill) {
	out := db.output
	maxSkillInc, _ := maxSource(db, "Skill", "ElusiveEffect")
	inc := db.Sum("INC", "ElusiveEffect", "BuffEffectOnSelf")
	if main != nil && main.mods != nil && main.mods.Flag("SupportedByNightblade") {
		inc += db.Sum("INC", "NightbladeSupportedElusiveEffect")
	}
	inc += maxSkillInc
	mod := (1 + inc/100) * db.More("ElusiveEffect", "BuffEffectOnSelf") * 100
	minThreshold, _ := db.Override("ElusiveEffectMinThreshold")
	incDuration := db.Sum("BASE", "ElusiveEffectIncreaseDuration")
	peak := mod
	if incDuration > 0 {
		rate := 20 / (1 + db.Sum("INC", "ElusiveEffectLossSlower")/100)
		peak = mod + rate*incDuration
		decDuration := (peak - minThreshold) / rate
		out["ElusiveEffectMod"] = ((mod+peak)/2*incDuration + (peak+minThreshold)/2*decDuration) / (incDuration + decDuration)
	} else {
		out["ElusiveEffectMod"] = (mod + minThreshold) / 2
	}
	db.NewMod("ElusiveEffect", "INC", maxSkillInc, "Max Skill Effect")
	if o, ok := db.Override("ElusiveEffect"); ok {
		out["ElusiveEffectMod"] = math.Min(o, peak)
	}
	effect := out["ElusiveEffectMod"] / 100
	db.conditions["Elusive"] = true
	db.NewMod("AvoidAllDamageFromHitsChance", "BASE", math.Floor(15*effect), "Elusive")
	db.NewMod("MovementSpeed", "INC", math.Floor(30*effect), "Elusive")
}

// initConditionMods is the part of calcs.initModDB (CalcSetup.lua) that
// turns config conditions into buff flags and base debuffs; it runs for
// the player and the enemy.
func initConditionMods(db *ModDB) {
	when := func(v string) map[string]any { return map[string]any{"type": "Condition", "var": v} }
	for _, x := range [][2]string{
		{"Blind", "Blinded"}, {"Chill", "Chilled"}, {"Freeze", "Frozen"}, {"Fortify", "Fortify"},
		{"Fortified", "Fortified"}, {"Excommunicated", "Excommunicated"}, {"Fanaticism", "Fanaticism"},
		{"Onslaught", "Onslaught"}, {"UnholyMight", "UnholyMight"}, {"ChaoticMight", "ChaoticMight"},
		{"Tailwind", "Tailwind"}, {"Adrenaline", "Adrenaline"}, {"AlchemistsGenius", "AlchemistsGenius"},
		{"LuckyHits", "LuckyHits"}, {"Convergence", "Convergence"},
	} {
		db.Add(Mod{Name: x[0], Type: "FLAG", Value: true, Source: "Base", Tags: []map[string]any{when(x[1])}})
	}
	db.NewMod("MovementSpeed", "INC", -30, "Base", when("Maimed"))
	db.Add(Mod{Name: "DamageTaken", Type: "INC", Value: 10.0, Flags: mfAttack, Source: "Base", Tags: []map[string]any{when("Intimidated")}})
	db.Add(Mod{Name: "DamageTaken", Type: "INC", Value: 10.0, Flags: mfSpell, Source: "Base", Tags: []map[string]any{when("Unnerved")}})
	debilitated := map[string]any{"type": "GlobalEffect", "effectName": "Debilitated", "effectType": "Debuff"}
	db.NewMod("Damage", "MORE", -10, "Base", when("Debilitated"), debilitated)
	db.NewMod("MovementSpeed", "MORE", -20, "Base", when("Debilitated"), debilitated)
	madness := map[string]any{"type": "GlobalEffect", "effectName": "Malignant Madness", "effectType": "Debuff"}
	db.NewMod("Damage", "MORE", -10, "Base", when("MalignantMadness"), madness)
	db.NewMod("ActionSpeed", "MORE", -10, "Base", when("MalignantMadness"), madness)
	db.NewMod("PhysicalDamageReduction", "BASE", -15, "Base", when("Crushed"))
	db.NewMod("CritChanceCap", "BASE", 100, "Base")
	// Skill use times and stack limits of calcs.initModDB.
	db.NewMod("TrapThrowingTime", "BASE", 0.6, "Base")
	db.NewMod("MineLayingTime", "BASE", 0.3, "Base")
	db.NewMod("WarcryCastTime", "BASE", 0.8, "Base")
	db.NewMod("TotemPlacementTime", "BASE", 0.6, "Base")
	db.NewMod("BallistaPlacementTime", "BASE", 0.5, "Base")
	db.NewMod("BleedStacksMax", "BASE", 1, "Base")
	// IgnoreCond only hides these from the config tab's condition list.
	ignore := map[string]any{"type": "IgnoreCond"}
	db.Add(Mod{Name: "Condition:Burning", Type: "FLAG", Value: true, Source: "Base", Tags: []map[string]any{ignore, when("Ignited")}})
	db.Add(Mod{Name: "Condition:Poisoned", Type: "FLAG", Value: true, Source: "Base", Tags: []map[string]any{ignore,
		{"type": "MultiplierThreshold", "var": "PoisonStack", "threshold": 1.0}}})
}

// playerBaseMods are the defensive player base mods of calcs.initEnv.
// Soul Eater: Data/Misc.lua characterConstants soul_eater_maximum_stacks
// and CalcPerform.lua doActorMisc's speed per stack.
const (
	soulEaterMaxStacks     = 45
	soulEaterSpeedPerStack = 5.0
)

func playerBaseMods(db *ModDB) {
	endurance := map[string]any{"type": "Multiplier", "var": "EnduranceCharge"}
	db.NewMod("PhysicalDamageReduction", "BASE", 4, "Base", endurance) // physical_damage_reduction_%_per_endurance_charge
	db.NewMod("ElementalDamageReduction", "BASE", 4, "Base", endurance)
	db.NewMod("MaximumFortification", "BASE", 20, "Base") // base_max_fortification
	db.NewMod("ActiveTotemLimit", "BASE", 1, "Base")      // base_number_of_totems_allowed
	db.NewMod("EnemyCurseLimit", "BASE", 1, "Base")
	db.NewMod("SocketedCursesHexLimitValue", "BASE", 1, "Base")
	db.NewMod("ImpaleStacksMax", "BASE", 5, "Base") // impaled_debuff_number_of_reflected_hits
	db.NewMod("SoulEaterMax", "BASE", soulEaterMaxStacks, "Base")
	db.NewMod("ShockStacksMax", "BASE", 1, "Base")  // CalcSetup.lua initModDB
	db.NewMod("ScorchStacksMax", "BASE", 1, "Base") // CalcSetup.lua initModDB
	// Offence base mods of calcs.initEnv (characterConstants).
	when := func(v string, neg bool) map[string]any {
		t := map[string]any{"type": "Condition", "var": v}
		if neg {
			t["neg"] = true
		}
		return t
	}
	db.NewMod("Accuracy", "BASE", 2, "Base", map[string]any{"type": "Multiplier", "var": "Level", "base": -2.0}) // accuracy_rating_per_level
	db.NewMod("CritMultiplier", "BASE", 50, "Base")                                                              // base_critical_strike_multiplier 150
	db.NewMod("DotMultiplier", "BASE", 50, "Base", when("CriticalStrike", false))                                // critical_ailment_dot_multiplier_+
	db.NewMod("CritChance", "INC", 50, "Base", map[string]any{"type": "Multiplier", "var": "PowerCharge"})
	frenzy := map[string]any{"type": "Multiplier", "var": "FrenzyCharge"}
	db.Add(Mod{Name: "Speed", Type: "INC", Value: 4.0, Flags: mfAttack, Source: "Base", Tags: []map[string]any{frenzy}})
	db.Add(Mod{Name: "Speed", Type: "INC", Value: 4.0, Flags: mfCast, Source: "Base", Tags: []map[string]any{frenzy}})
	db.NewMod("Damage", "MORE", 4, "Base", frenzy)
	db.NewMod("Multiplier:GaleForce", "BASE", 0, "Base")
	db.NewMod("MaximumGaleForce", "BASE", 10, "Base")
	db.NewMod("Multiplier:IntensityLimit", "BASE", 3, "Base")
	db.NewMod("Damage", "INC", 2, "Base", map[string]any{"type": "Multiplier", "var": "Rampage", "limit": 50.0, "div": 20.0})
	db.NewMod("ActiveTrapLimit", "BASE", 15, "Base")
	db.NewMod("ActiveMineLimit", "BASE", 15, "Base")
	db.NewMod("MineThrowCount", "BASE", 1, "Base")
	db.NewMod("TrapThrowCount", "BASE", 1, "Base")
	db.NewMod("ActiveBrandLimit", "BASE", 3, "Base")
	db.NewMod("ProjectileCount", "BASE", 1, "Base")
	// dual_wield_inherent_attack_speed_+%_final 10
	db.Add(Mod{Name: "Speed", Type: "MORE", Value: 10.0, Flags: mfAttack, Source: "Base", Tags: []map[string]any{when("DualWielding", false), when("DoubledInherentDualWieldingSpeed", true)}})
	db.Add(Mod{Name: "Speed", Type: "MORE", Value: 20.0, Flags: mfAttack, Source: "Base", Tags: []map[string]any{when("DualWielding", false), when("DoubledInherentDualWieldingSpeed", false)}})
	db.Add(Mod{Name: "Damage", Type: "MORE", Value: 200.0, KeywordFlags: 0x400000, Source: "Base", // KeywordFlag.Bleed
		Tags: []map[string]any{{"type": "ActorCondition", "actor": "enemy", "var": "Moving"}, when("NoExtraBleedDamageToMovingEnemy", true)}})
	db.Add(Mod{Name: "Condition:BloodStance", Type: "FLAG", Value: true, Source: "Base", Tags: []map[string]any{when("SandStance", true)}})
	db.Add(Mod{Name: "Condition:PrideMinEffect", Type: "FLAG", Value: true, Source: "Base", Tags: []map[string]any{when("PrideMaxEffect", true)}})
	db.NewMod("PerBrutalTripleDamageChance", "BASE", 3, "Base")
	db.NewMod("PerAfflictionAilmentDamage", "BASE", 8, "Base")
	db.NewMod("PerAfflictionNonDamageEffect", "BASE", 8, "Base")
	db.NewMod("PerAbsorptionElementalEnergyShieldRecoup", "BASE", 12, "Base")
	db.NewMod("TinctureLimit", "BASE", 1, "Base")
	db.NewMod("PresenceRadius", "BASE", 80, "Base")
	// movement_velocity_+%_per_10_rampage_stacks, max_rampage_stacks 1000
	db.NewMod("MovementSpeed", "INC", 1, "Base", map[string]any{"type": "Multiplier", "var": "Rampage", "limit": 50.0, "div": 20.0})
}

// consecratedGround is "Handle consecrated ground effects on enemies" of
// calcs.perform.
func consecratedGround(db *ModDB) {
	if !db.enemy.Flag("Condition:OnConsecratedGround") {
		return
	}
	effect := 1 + db.Sum("INC", "ConsecratedGroundEffect")/100
	db.enemy.NewMod("DamageTaken", "INC", math.Floor(db.enemy.Sum("INC", "DamageTakenConsecratedGround")*effect), "Consecrated Ground")
}

// exposures is "Apply exposures" of calcs.perform: the strongest exposure
// of each element on the enemy, changed by extra exposure and scaled by
// exposure effect, as an enemy resistance.
func exposures(db *ModDB) {
	for _, el := range []string{"Fire", "Cold", "Lightning"} {
		if db.Flag("ElementalEquilibrium") && db.enemy.Flag("Condition:HitBy"+el+"Damage") {
			continue
		}
		min, source, found := 0.0, "", false
		for _, r := range db.enemy.TabulateCfg(nil, "BASE", el+"Exposure") {
			if f, ok := num(r.Value); ok && (!found || f < min) {
				min, source, found = f, r.Source, true
			}
		}
		if !found {
			continue
		}
		min += db.Sum("BASE", "ExtraExposure", "Extra"+el+"Exposure")
		if inc := db.Sum("INC", "ExposureEffect", el+"ExposureEffect"); inc != 0 {
			min *= 1 + inc/100
		}
		db.enemy.Add(Mod{Name: "Condition:Has" + el + "Exposure", Type: "FLAG", Value: true})
		if o, ok := db.Override("ExposureMin"); ok {
			min = math.Min(min, o)
		}
		db.enemy.NewMod(el+"Resist", "BASE", min, source)
		db.Add(Mod{Name: "Condition:AppliedExposureRecently", Type: "FLAG", Value: true})
	}
}
