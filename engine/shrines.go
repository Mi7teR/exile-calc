package engine

import "math"

var regularShrines = []string{
	"AccelerationShrine", "BrutalShrine", "DiamondShrine", "DivineShrine", "EchoingShrine", "GloomShrine",
	"GreaterFreezingShrine", "GreaterShockingShrine", "GreaterSkeletalShrine", "ImpenetrableShrine",
	"MassiveShrine", "ReplenishingShrine", "ResistanceShrine", "ResonatingShrine",
	"BloodShrineOfRats", "BloodShrineOfLocusts", "BloodShrineOfToads", "BloodShrineOfCrows", "BloodShrineOfBats",
}

// lesserShrines maps each lesser shrine to the regular one that overrides it.
var lesserShrines = [][2]string{
	{"LesserAccelerationShrine", "AccelerationShrine"}, {"LesserBrutalShrine", "BrutalShrine"},
	{"LesserImpenetrableShrine", "ImpenetrableShrine"}, {"LesserMassiveShrine", "MassiveShrine"},
	{"LesserReplenishingShrine", "ReplenishingShrine"}, {"LesserResistanceShrine", "ResistanceShrine"},
}

// shrineBaseMods are CalcSetup.lua's base shrine flags: <Shrine> is set by
// Condition:<Shrine>, a lesser shrine only without its regular one.
func shrineBaseMods(db *ModDB) {
	flag := func(name string, tags ...map[string]any) {
		db.Add(Mod{Name: name, Type: "FLAG", Value: true, Source: "Base", Tags: tags})
	}
	for _, s := range regularShrines {
		flag(s, map[string]any{"type": "Condition", "var": s})
	}
	for _, p := range lesserShrines {
		flag(p[0], map[string]any{"type": "Condition", "var": p[0]}, map[string]any{"type": "Condition", "var": p[1], "neg": true})
	}
}

// shrineBuffs ports CalcPerform.lua's shrine buffs (in the mode_combat
// block, 297).
func shrineBuffs(db *ModDB) {
	if !modeCombat(db) {
		return
	}
	eff := 1 + db.Sum("INC", "BuffEffectOnSelf", "ShrineBuffEffect")/100
	f := func(v float64) float64 { return math.Floor(v * eff) }
	add := func(flag string, mods ...Mod) {
		if !db.Flag(flag) {
			return
		}
		for _, m := range mods {
			db.Add(m)
		}
	}
	src := func(name, typ string, v any, source string) Mod {
		return Mod{Name: name, Type: typ, Value: v, Source: source}
	}
	flagged := func(m Mod, flags uint64) Mod { m.Flags = flags; return m }
	add("AccelerationShrine", src("ActionSpeed", "INC", f(50), "Acceleration Shrine"),
		src("ProjectileSpeed", "INC", f(80), "Acceleration Shrine"))
	add("BrutalShrine", src("Damage", "INC", f(50), "Brutal Shrine"),
		src("EnemyStunDuration", "INC", f(30), "Brutal Shrine"), src("EnemyKnockbackChance", "INC", 100, "Brutal Shrine"))
	add("DiamondShrine", src("CritChance", "OVERRIDE", 100, "Diamond Shrine"))
	add("DivineShrine", src("DamageTaken", "MORE", -100, "Divine Shrine"))
	add("EchoingShrine", flagged(src("Speed", "MORE", f(100), "Echoing Shrine"), mfAttack),
		flagged(src("Speed", "MORE", f(100), "Echoing Shrine"), mfCast), src("RepeatCount", "BASE", f(1), "Echoing Shrine"))
	add("GloomShrine", src("NonChaosDamageGainAsChaos", "BASE", f(10), "Gloom Shrine"))
	add("GreaterFreezingShrine", src("PhysicalDamageGainAsCold", "BASE", f(30), "Greater Freezing Shrine"))
	add("GreaterShockingShrine", src("PhysicalDamageGainAsLightning", "BASE", f(30), "Greater Shocking Shrine"))
	add("GreaterSkeletalShrine", src("PhysicalDamageGainAsChaos", "BASE", f(30), "Greater Skeletal Shrine"))
	add("ImpenetrableShrine", src("Armour", "INC", f(100), "Impenetrable Shrine"),
		src("Evasion", "INC", f(100), "Impenetrable Shrine"), src("EnergyShield", "INC", f(100), "Impenetrable Shrine"))
	add("MassiveShrine", src("Life", "INC", f(40), "Massive Shrine"), src("AreaOfEffect", "INC", f(40), "Massive Shrine"))
	add("ReplenishingShrine", src("ManaRegen", "INC", 200*eff, "Replenishing Shrine"),
		src("LifeRegenPercent", "BASE", 6.7*eff, "Replenishing Shrine"))
	add("ResistanceShrine", src("ElementalResist", "BASE", f(50), "Resistance Shrine"),
		src("ElementalResistMax", "BASE", f(10), "Resistance Shrine"), src("ChaosResistMax", "BASE", f(10), "Resistance Shrine"))
	add("ResonatingShrine", src("PowerChargesMax", "BASE", f(1), "Resonating Shrine"),
		src("FrenzyChargesMax", "BASE", f(1), "Resonating Shrine"), src("EnduranceChargesMax", "BASE", f(1), "Resonating Shrine"))
	if !db.Flag("AccelerationShrine") {
		add("LesserAccelerationShrine", src("ActionSpeed", "INC", f(10), "Lesser Acceleration Shrine"),
			src("ProjectileSpeed", "INC", f(30), "Lesser Acceleration Shrine"))
	}
	add("LesserBrutalShrine", src("Damage", "INC", f(20), "Lesser Brutal Shrine"),
		src("EnemyStunDuration", "INC", f(20), "Lesser Brutal Shrine"), src("EnemyKnockbackChance", "INC", 100, "Lesser Brutal Shrine"))
	add("LesserImpenetrableShrine", src("Armour", "INC", f(50), "Lesser Impenetrable Shrine"),
		src("Evasion", "INC", f(50), "Lesser Impenetrable Shrine"), src("EnergyShield", "INC", f(50), "Lesser Impenetrable Shrine"))
	add("LesserMassiveShrine", src("Life", "INC", f(20), "Lesser Massive Shrine"), src("AreaOfEffect", "INC", f(20), "Lesser Massive Shrine"))
	add("LesserReplenishingShrine", src("ManaRegen", "INC", 100*eff, "Lesser Replenishing Shrine"),
		src("LifeRegenPercent", "BASE", 3.3*eff, "Lesser Replenishing Shrine"))
	add("LesserResistanceShrine", src("ElementalResist", "BASE", f(25), "Lesser Resistance Shrine"),
		src("ElementalResistMax", "BASE", f(2), "Lesser Resistance Shrine"), src("ChaosResistMax", "BASE", f(2), "Lesser Resistance Shrine"))
	add("BloodShrineOfRats", src("ElementalDamageGainAsChaos", "BASE", f(30), "Blood Shrine of Rats"))
	add("BloodShrineOfLocusts", src("DotMultiplier", "BASE", f(30), "Blood Shrine of Locusts"))
	add("BloodShrineOfToads", src("BloodShrineExplodingToadTriggerChance", "BASE", f(25), "Blood Shrine of Toads"))
	add("BloodShrineOfCrows", src("ChaosResist", "BASE", f(50), "Blood Shrine of Crows"),
		src("ElementalDamageFromHitsTakenAsChaos", "BASE", f(10), "Blood Shrine of Crows"),
		src("PhysicalDamageFromHitsTakenAsChaos", "BASE", f(10), "Blood Shrine of Crows"))
	add("BloodShrineOfBats", src("EnemyCurseLimit", "BASE", f(1), "Blood Shrine of Bats"),
		Mod{Name: "ExtraCurse", Type: "LIST", Value: map[string]any{"skillId": "Enfeeble", "level": 1.0}, Source: "Blood Shrine of Bats"})
}
