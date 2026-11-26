package engine

import "math"

// Data/Misc.lua characterConstants for charges.
const (
	maxEnduranceCharges = 3
	maxFrenzyCharges    = 3
	maxPowerCharges     = 3
	maxRighteousCharges = 5 // InspirationChargesMax
	maxBloodCharges     = 5 // maximum_blood_scythe_charges
)

// chargeBaseMods are the charge maximums of calcs.initModDB.
func chargeBaseMods(db *ModDB) {
	db.NewMod("ChargeDuration", "BASE", 10, "Base")
	db.NewMod("PowerChargesMax", "BASE", maxPowerCharges, "Base")
	db.NewMod("FrenzyChargesMax", "BASE", maxFrenzyCharges, "Base")
	db.NewMod("EnduranceChargesMax", "BASE", maxEnduranceCharges, "Base")
	db.NewMod("InspirationChargesMax", "BASE", maxRighteousCharges, "Base")
	db.NewMod("BloodChargesMax", "BASE", maxBloodCharges, "Base")
	brine := map[string]any{"type": "Multiplier", "var": "BrineCharge"}
	db.NewMod("PhysicalDamageGainAsCold", "BASE", 4, "Base", brine) // physical_damage_%_to_add_as_cold_per_brine_charge
	db.NewMod("PhysicalDamageGainAsLightning", "BASE", 4, "Base", brine)
}

// playerChargeBaseMods are the player's own rage and valour maximums,
// which calcs.initEnv adds to the player only (CalcSetup.lua:631, 635),
// not calcs.initModDB to every actor: a minion has no base maximum rage.
func playerChargeBaseMods(db *ModDB) {
	db.NewMod("MaximumRage", "BASE", 30, "Base") // characterConstants maximum_rage
	db.NewMod("MaximumValour", "BASE", 50, "Base")
}

// banners is the "Banners" block of calcs.perform: a planted banner uses
// the configured valour up to the maximum.
func banners(db *ModDB) {
	if db.Flag("Condition:BannerPlanted") {
		db.NewMod("Multiplier:BannerValour", "BASE", math.Min(db.Sum("BASE", "Multiplier:ValourStacks"), db.Sum("BASE", "MaximumValour")), "Base")
	}
}

func overrideOr(db *ModDB, name string, def float64) float64 {
	if o, ok := db.Override(name); ok {
		return o
	}
	return def
}

// charges ports doActorCharges (Modules/CalcPerform.lua) for the player:
// charge counts from the config's Use<X>Charges flags and overrides, set
// as multipliers. Siphoning, challenger, blitz, brutal, absorption,
// affliction, brine and spirit charges follow the same rules.
func charges(db *ModDB) {
	out := db.output
	sumMax := func(name string) float64 { return math.Max(db.Sum("BASE", name), 0) }
	out["PowerChargesMin"] = sumMax("PowerChargesMin")
	out["PowerChargesMax"] = overrideOr(db, "PowerChargesMax", sumMax("PowerChargesMax"))
	// CalcPerform.lua:990, 997, 1004.
	chargeDuration := func(name string) float64 {
		return math.Floor(db.Sum("BASE", "ChargeDuration") * calcMod(db, nil, name, "ChargeDuration"))
	}
	out["PowerChargesDuration"] = chargeDuration("PowerChargesDuration")
	if db.Flag("MaximumFrenzyChargesIsMaximumPowerCharges") {
		replaceMod(db, "FrenzyChargesMax", "OVERRIDE", out["PowerChargesMax"])
	}
	out["FrenzyChargesMin"] = sumMax("FrenzyChargesMin")
	frenzyMax := sumMax("FrenzyChargesMax")
	if db.Flag("MaximumFrenzyChargesIsMaximumPowerCharges") {
		frenzyMax = out["PowerChargesMax"]
	}
	out["FrenzyChargesMax"] = overrideOr(db, "FrenzyChargesMax", frenzyMax)
	out["FrenzyChargesDuration"] = chargeDuration("FrenzyChargesDuration")
	if db.Flag("MaximumEnduranceChargesIsMaximumFrenzyCharges") {
		replaceMod(db, "EnduranceChargesMax", "OVERRIDE", out["FrenzyChargesMax"])
	}
	out["EnduranceChargesMin"] = sumMax("EnduranceChargesMin")
	enduranceMax := sumMax("EnduranceChargesMax")
	if db.Flag("MaximumEnduranceChargesIsMaximumFrenzyCharges") {
		enduranceMax = out["FrenzyChargesMax"]
	}
	out["EnduranceChargesMax"] = overrideOr(db, "EnduranceChargesMax", enduranceMax)
	out["EnduranceChargesDuration"] = chargeDuration("EnduranceChargesDuration")
	for _, n := range []string{"SiphoningChargesMax", "ChallengerChargesMax", "BlitzChargesMax", "InspirationChargesMax", "CrabBarriersMax", "BloodChargesMax", "SpiritChargesMax"} {
		out[n] = sumMax(n)
	}
	flagMax := func(flag string, v float64) float64 {
		if db.Flag(flag) {
			return math.Max(v, 0)
		}
		return 0
	}
	minOrMax := func(minFlag, isMaxFlag string, max, min float64) float64 {
		if !db.Flag(minFlag) {
			return 0
		}
		if db.Flag(isMaxFlag) {
			return math.Max(max, 0)
		}
		return math.Max(min, 0)
	}
	out["BrutalChargesMin"] = minOrMax("MinimumEnduranceChargesEqualsMinimumBrutalCharges", "MinimumEnduranceChargesIsMaximumEnduranceCharges", out["EnduranceChargesMax"], out["EnduranceChargesMin"])
	out["BrutalChargesMax"] = flagMax("MaximumEnduranceChargesEqualsMaximumBrutalCharges", out["EnduranceChargesMax"])
	out["BrineChargesMax"] = math.Max(out["EnduranceChargesMax"], 0)
	out["AbsorptionChargesMin"] = minOrMax("MinimumPowerChargesEqualsMinimumAbsorptionCharges", "MinimumPowerChargesIsMaximumPowerCharges", out["PowerChargesMax"], out["PowerChargesMin"])
	out["AbsorptionChargesMax"] = flagMax("MaximumPowerChargesEqualsMaximumAbsorptionCharges", out["PowerChargesMax"])
	out["AfflictionChargesMin"] = minOrMax("MinimumFrenzyChargesEqualsMinimumAfflictionCharges", "MinimumFrenzyChargesIsMaximumFrenzyCharges", out["FrenzyChargesMax"], out["FrenzyChargesMin"])
	out["AfflictionChargesMax"] = flagMax("MaximumFrenzyChargesEqualsMaximumAfflictionCharges", out["FrenzyChargesMax"])
	out["SpiritInfusionsMax"] = db.Sum("BASE", "SpiritInfusionsMax")
	if db.Flag("Condition:CanGainSpiritInfusion") {
		out["SpiritInfusionsMax"] += 10
	}

	for _, n := range []string{"PowerCharges", "FrenzyCharges", "EnduranceCharges", "SiphoningCharges", "ChallengerCharges", "BlitzCharges",
		"InspirationCharges", "GhostShrouds", "BrutalCharges", "BrineCharges", "AbsorptionCharges", "AfflictionCharges", "BloodCharges", "SpiritCharges", "SpiritInfusions"} {
		out[n] = 0
	}
	if db.Flag("MinimumFrenzyChargesIsMaximumFrenzyCharges") {
		out["FrenzyChargesMin"] = out["FrenzyChargesMax"]
	}
	if db.Flag("MinimumEnduranceChargesIsMaximumEnduranceCharges") {
		out["EnduranceChargesMin"] = out["EnduranceChargesMax"]
	}
	if db.Flag("MinimumPowerChargesIsMaximumPowerCharges") {
		out["PowerChargesMin"] = out["PowerChargesMax"]
	}
	// One charge type: configured count, conversion, then at least the minimum.
	charge := func(kind, convFlag, conv string) {
		if db.Flag("Use" + kind + "Charges") {
			out[kind+"Charges"] = overrideOr(db, kind+"Charges", out[kind+"ChargesMax"])
			if kind == "Endurance" && db.Flag("CanGainBrineCharges") {
				out["BrineCharges"] = out["BrineChargesMax"]
			}
		}
		if db.Flag(convFlag) {
			out[conv+"Charges"] = math.Max(out[kind+"Charges"], math.Min(out[conv+"ChargesMax"], out[conv+"ChargesMin"]))
			out[kind+"Charges"] = 0
		} else {
			out[kind+"Charges"] = math.Max(out[kind+"Charges"], math.Min(out[kind+"ChargesMax"], out[kind+"ChargesMin"]))
		}
		out["Removable"+kind+"Charges"] = math.Max(out[kind+"Charges"]-out[kind+"ChargesMin"], 0)
	}
	charge("Power", "PowerChargesConvertToAbsorptionCharges", "Absorption")
	charge("Frenzy", "FrenzyChargesConvertToAfflictionCharges", "Affliction")
	charge("Endurance", "EnduranceChargesConvertToBrutalCharges", "Brutal")
	for _, k := range []string{"Siphoning", "Challenger", "Blitz"} {
		if db.Flag("Use" + k + "Charges") {
			out[k+"Charges"] = overrideOr(db, k+"Charges", out[k+"ChargesMax"])
		}
	}
	if db.owner == nil { // actor == env.player: minions do not gain inspiration charges
		out["InspirationCharges"] = overrideOr(db, "InspirationCharges", out["InspirationChargesMax"])
	}
	if db.Flag("UseGhostShrouds") {
		out["GhostShrouds"] = overrideOr(db, "GhostShrouds", 3)
	}
	out["BloodCharges"] = math.Min(overrideOr(db, "BloodCharges", out["BloodChargesMax"]), out["BloodChargesMax"])
	out["SpiritCharges"] = math.Min(overrideOr(db, "SpiritCharges", 0), out["SpiritChargesMax"])
	out["SpiritInfusions"] = math.Min(overrideOr(db, "SpiritInfusion", 0), out["SpiritInfusionsMax"])
	out["CrabBarriers"] = math.Min(overrideOr(db, "CrabBarriers", out["CrabBarriersMax"]), out["CrabBarriersMax"])
	for _, k := range []string{"Power", "Frenzy", "Endurance"} {
		if db.Flag("HaveMaximum" + k + "Charges") {
			out[k+"Charges"] = out[k+"ChargesMax"]
		}
	}
	out["TotalCharges"] = out["PowerCharges"] + out["FrenzyCharges"] + out["EnduranceCharges"]
	out["RemovableTotalCharges"] = out["RemovableEnduranceCharges"] + out["RemovableFrenzyCharges"] + out["RemovablePowerCharges"]
	m := db.multipliers
	m["PowerCharge"], m["PowerChargeMax"], m["RemovablePowerCharge"] = out["PowerCharges"], out["PowerChargesMax"], out["RemovablePowerCharges"]
	m["FrenzyCharge"], m["RemovableFrenzyCharge"] = out["FrenzyCharges"], out["RemovableFrenzyCharges"]
	m["EnduranceCharge"], m["RemovableEnduranceCharge"] = out["EnduranceCharges"], out["RemovableEnduranceCharges"]
	m["TotalCharges"], m["RemovableTotalCharges"] = out["TotalCharges"], out["RemovableTotalCharges"]
	for k, o := range map[string]string{"SiphoningCharge": "SiphoningCharges", "ChallengerCharge": "ChallengerCharges", "BlitzCharge": "BlitzCharges",
		"InspirationCharge": "InspirationCharges", "GhostShroud": "GhostShrouds", "CrabBarrier": "CrabBarriers", "BrutalCharge": "BrutalCharges",
		"BrineCharge": "BrineCharges", "AbsorptionCharge": "AbsorptionCharges", "AfflictionCharge": "AfflictionCharges", "BloodCharge": "BloodCharges",
		"SpiritCharge": "SpiritCharges", "SpiritInfusion": "SpiritInfusions"} {
		m[k] = out[o]
	}
}

// replaceMod is ModDB:ReplaceMod for a numeric mod without tags.
func replaceMod(db *ModDB, name, typ string, value float64) {
	for i, m := range db.mods[name] {
		if m.Type == typ && len(m.Tags) == 0 {
			db.mods[name][i].Value = value
			return
		}
	}
	db.NewMod(name, typ, value, "")
}

// rage ports the Rage part of doActorMisc: with Condition:CanGainRage or
// rage regeneration, the configured stacks within the main skill's maximum.
func rage(db *ModDB, mainCfg *Cfg) {
	// Inside doActorMisc's mode_combat block (CalcPerform.lua:647).
	if !modeCombat(db) {
		return
	}
	if m := db.Sum("BASE", "MinimumRage"); m > db.multipliers["Rage"] {
		db.multipliers["Rage"] = m
	}
	if !db.Flag("Condition:CanGainRage") && db.Sum("BASE", "RageRegen") <= 0 {
		return
	}
	out := db.output
	maxStacks := math.Floor(db.SumCfg(mainCfg, "BASE", "MaximumRage") * db.MoreCfg(mainCfg, "MaximumRage"))
	minStacks := math.Min(db.Sum("BASE", "MinimumRage"), maxStacks)
	stacks := math.Min(db.Sum("BASE", "Multiplier:RageStack"), maxStacks)
	if minStacks > 0 {
		stacks = math.Max(stacks, minStacks)
	} else {
		stacks = math.Max(stacks, 0)
	}
	out["RageEffect"] = math.Floor(stacks * calcMod(db, nil, "RageEffect"))
	db.NewMod("Multiplier:RageEffect", "BASE", out["RageEffect"], "Base")
	out["Rage"], out["MaximumRage"] = stacks, maxStacks
	db.NewMod("Multiplier:Rage", "BASE", stacks, "Base")
	if db.Flag("Condition:RageSpellDamage") {
		db.Add(Mod{Name: "Damage", Type: "MORE", Value: out["RageEffect"], Flags: mfSpell, Source: "Rage"})
	} else {
		db.Add(Mod{Name: "Damage", Type: "MORE", Value: out["RageEffect"], Flags: mfAttack, Source: "Rage"})
	}
	if stacks == maxStacks {
		db.Add(Mod{Name: "Condition:HaveMaximumRage", Type: "FLAG", Value: true})
	}
	inherentRageLoss(db)
}

// alternateCharges is calcs.perform's "Process stats from alternate
// charges" (mode_combat): Brutal Charges grant triple damage chance and
// Affliction Charges ailment damage and non-damaging ailment effect.
func alternateCharges(db *ModDB) {
	if !modeCombat(db) { // CalcPerform.lua:3159
		return
	}
	if db.Flag("UseEnduranceCharges") && db.Flag("EnduranceChargesConvertToBrutalCharges") {
		db.NewMod("TripleDamageChance", "BASE", db.Sum("BASE", "PerBrutalTripleDamageChance"), "",
			map[string]any{"type": "Multiplier", "var": "BrutalCharge"})
	}
	if db.Flag("UseFrenzyCharges") && db.Flag("FrenzyChargesConvertToAfflictionCharges") {
		per := map[string]any{"type": "Multiplier", "var": "AfflictionCharge"}
		db.Add(Mod{Name: "Damage", Type: "MORE", Value: db.Sum("BASE", "PerAfflictionAilmentDamage"), Source: "Affliction Charges",
			KeywordFlags: kfAilment, Tags: []map[string]any{per}})
		effect := db.Sum("BASE", "PerAfflictionNonDamageEffect")
		for _, n := range []string{"EnemyChillEffect", "EnemyShockEffect", "EnemyFreezeEffect", "EnemyScorchEffect", "EnemyBrittleEffect", "EnemySapEffect"} {
			db.NewMod(n, "MORE", effect, "Affliction Charges", per)
		}
	}
}
