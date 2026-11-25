package engine

import (
	"math"
	"reflect"
	"strings"

	"github.com/Mi7teR/poe-lib/itemtext"
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

// actorMisc ports the player buffs and debuffs of doActorMisc
// (CalcPerform.lua) before its rage part, which rage() ports.
func actorMisc(db *ModDB, main *activeSkill, silverFlask bool, silverFlaskInc float64) {
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

	if main != nil && main.mods != nil && main.mods.FlagCfg(main.cfg, "Cruelty") {
		if o, ok := db.Override("Cruelty"); ok {
			db.multipliers["Cruelty"] = o
		} else {
			db.multipliers["Cruelty"] = 40
		}
	}
	if m := db.Sum("BASE", "MinimumRage"); m > db.multipliers["Rage"] {
		db.multipliers["Rage"] = m
	}
	if db.Sum("BASE", "MinimumFortification") > 0 {
		cond["Fortified"] = true
	}
	if db.Flag("Fortified") || db.Sum("BASE", "Multiplier:Fortification") > 0 {
		maxStacks, ok := db.Override("MaximumFortification")
		if !ok {
			maxStacks = db.SumCfg(mainCfg, "BASE", "MaximumFortification")
		}
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
	}
	if db.Flag("UnholyMight") {
		effect := buffEffect()
		db.NewMod("PhysicalDamageConvertToChaos", "BASE", math.Floor(100*effect), "Unholy Might")
		db.Add(Mod{Name: "Condition:CanWither", Type: "FLAG", Value: true, Source: "Unholy Might"})
		if v := db.Sum("BASE", "UnholyMightAlsoChaosPenetration"); v != 0 {
			db.NewMod("ChaosPenetration", "BASE", v*effect, "Unholy Might")
		}
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
}

// selfAilment is the effect of Chill, Shock or Scorch on the actor;
// shockAvoid says whether ShockAvoidAppliesToElementalAilments counts.
// PoB quirk: m_max(Sum(Self<X>Override), Override(<X>Val)) or default: with
// no override the call returns no values, so m_max gets the sum alone, and
// a number is never nil, so the default never applies.
func selfAilment(db *ModDB, name string, shockAvoid bool) (float64, bool) {
	a := nonDamagingAilment[name]
	value := db.Sum("BASE", "Self"+name+"Override")
	if o, ok := db.Override(name + "Val"); ok {
		value = math.Max(value, o)
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
func playerBaseMods(db *ModDB) {
	endurance := map[string]any{"type": "Multiplier", "var": "EnduranceCharge"}
	db.NewMod("PhysicalDamageReduction", "BASE", 4, "Base", endurance) // physical_damage_reduction_%_per_endurance_charge
	db.NewMod("ElementalDamageReduction", "BASE", 4, "Base", endurance)
	db.NewMod("MaximumFortification", "BASE", 20, "Base") // base_max_fortification
	db.NewMod("ActiveTotemLimit", "BASE", 1, "Base")      // base_number_of_totems_allowed
	db.NewMod("EnemyCurseLimit", "BASE", 1, "Base")
	db.NewMod("SocketedCursesHexLimitValue", "BASE", 1, "Base")
	db.NewMod("ImpaleStacksMax", "BASE", 5, "Base") // impaled_debuff_number_of_reflected_hits
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

// heraldAuraCounts is "Calculate number of active heralds and auras
// affecting self" of calcs.perform (mode_buffs).
func heraldAuraCounts(db *ModDB, skills []*activeSkill) {
	const stHerald, stAuraAffectsEnemies = 52, 53
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

// impaleStacks is "Fix the configured impale stacks on the enemy" of
// calcs.perform: the maximum when not configured, capped otherwise.
func impaleStacks(db *ModDB) {
	max := db.Sum("BASE", "ImpaleStacksMax") * (1 + db.Sum("BASE", "ImpaleAdditionalDurationChance")/100)
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

// totemCounts is the totem part of calcs.perform's active skill loop:
// with any totem skill, ActiveTotemLimit is the main skill's limit and
// TotemsSummoned (read by "per summoned totem" mods) the configured count
// or that limit.
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
				limit, ok := db.Override("ShockStacksMax")
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
				limit, ok := db.Override("ScorchStacksMax")
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

const (
	stNonHitChill = 67 // SkillType.NonHitChill
	stChillArea   = 68 // SkillType.ChillingArea
)

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
func chillSkills(db *ModDB, skills []*activeSkill) (guaranteed []*activeSkill, guaranteedBonechill bool) {
	out := db.output
	for _, s := range skills {
		if s.mods == nil {
			continue
		}
		bonechill := truthy(s.data["supportBonechill"])
		if bonechill && (s.types[stChillArea] || s.types[stNonHitChill] || !s.mods.Flag("CannotChill")) {
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
		} else if s.flags["guaranteedChill"] || s.types[stChillArea] || (s.types[stNonHitChill] && !s.mods.Flag("CannotChill")) {
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
