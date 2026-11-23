package engine

import "math"

var regularShrines = []string{
	"AccelerationShrine", "BrutalShrine", "DiamondShrine", "DivineShrine", "EchoingShrine", "GloomShrine",
	"GreaterFreezingShrine", "GreaterShockingShrine", "GreaterSkeletalShrine", "ImpenetrableShrine",
	"MassiveShrine", "ReplenishingShrine", "ResistanceShrine", "ResonatingShrine",
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

// shrineBuffs ports the defensive part of CalcPerform.lua's shrine buffs
// (Life, resistances, defences); offence-only shrines are not ported.
func shrineBuffs(db *ModDB) {
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
	src := func(name, typ string, v float64, source string) Mod {
		return Mod{Name: name, Type: typ, Value: v, Source: source}
	}
	add("ImpenetrableShrine", src("Armour", "INC", f(100), "Impenetrable Shrine"),
		src("Evasion", "INC", f(100), "Impenetrable Shrine"), src("EnergyShield", "INC", f(100), "Impenetrable Shrine"))
	add("MassiveShrine", src("Life", "INC", f(40), "Massive Shrine"), src("AreaOfEffect", "INC", f(40), "Massive Shrine"))
	add("ResistanceShrine", src("ElementalResist", "BASE", f(50), "Resistance Shrine"),
		src("ElementalResistMax", "BASE", f(10), "Resistance Shrine"), src("ChaosResistMax", "BASE", f(10), "Resistance Shrine"))
	add("ResonatingShrine", src("PowerChargesMax", "BASE", f(1), "Resonating Shrine"),
		src("FrenzyChargesMax", "BASE", f(1), "Resonating Shrine"), src("EnduranceChargesMax", "BASE", f(1), "Resonating Shrine"))
	add("LesserImpenetrableShrine", src("Armour", "INC", f(50), "Lesser Impenetrable Shrine"),
		src("Evasion", "INC", f(50), "Lesser Impenetrable Shrine"), src("EnergyShield", "INC", f(50), "Lesser Impenetrable Shrine"))
	add("LesserMassiveShrine", src("Life", "INC", f(20), "Lesser Massive Shrine"), src("AreaOfEffect", "INC", f(20), "Lesser Massive Shrine"))
	add("LesserResistanceShrine", src("ElementalResist", "BASE", f(25), "Lesser Resistance Shrine"),
		src("ElementalResistMax", "BASE", f(2), "Lesser Resistance Shrine"), src("ChaosResistMax", "BASE", f(2), "Lesser Resistance Shrine"))
}
