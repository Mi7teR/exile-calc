package engine

import "math"

// lootBonuses is CalcPerform.lua:1511-1514, Bisco's Collar's quantity and
// rarity from normal and magic enemies.
func lootBonuses(db *ModDB) {
	out := db.output
	out["LootQuantityNormalEnemies"], out["LootRarityMagicEnemies"] = 0, 0
	if q := db.Sum("INC", "LootQuantityNormalEnemies"); q > 0 {
		out["LootQuantityNormalEnemies"] = q + db.Sum("INC", "LootQuantity")
	}
	if r := db.Sum("INC", "LootRarityMagicEnemies"); r > 0 {
		out["LootRarityMagicEnemies"] = r + db.Sum("INC", "LootRarity")
	}
}

// gemLevelQuality is CalcPerform.lua:3972-4013: the main skill gem's
// level and quality (GemLevel is activeEffect.level; GemQuality sums the
// base, support, item, global and socket quality mods).
func gemLevelQuality(db *ModDB, s *activeSkill) {
	if !s.active.hasSrc {
		return
	}
	out, list, cfg := db.output, s.mods, s.cfg
	out["GemHasLevel"], out["GemLevel"] = 1, float64(s.active.level)
	out["GemHasQuality"] = 1
	out["GemQuality"] = list.SumCfg(cfg, "BASE", "GemQuality") + list.SumCfg(cfg, "BASE", "GemSupportQuality") +
		list.SumCfg(cfg, "BASE", "GemItemQuality") + list.SumCfg(cfg, "BASE", "GemGlobalQuality") +
		list.SumCfg(cfg, "BASE", "GemSocketQuality")
}

// hexDoom is CalcPerform.lua:1380-1388: every hex curse (not Blasphemy's
// TotemCastsWhenNotDetached) raises HexDoomLimit to its MaxDoom and gets
// its configured doom as increased curse effect. Sum always returns a
// number, so the MaxDoom test in PoB's condition is always true.
func hexDoom(db *ModDB, skills []*activeSkill) {
	out := db.output
	for _, s := range skills {
		if !s.flags["hex"] || !s.flags["curse"] || s.types[stTotemCastsWhenNotDetached] {
			continue
		}
		stacks := db.Sum("BASE", "Multiplier:HexDoomStack")
		maxDoom := s.mods.Sum("BASE", "MaxDoom")
		effect := s.mods.More("DoomEffect")
		out["HexDoomLimit"] = math.Max(maxDoom, out["HexDoomLimit"])
		s.mods.NewMod("CurseEffect", "INC", math.Min(stacks, maxDoom)*effect, "Doom")
		db.multipliers["HexDoom"] = math.Min(math.Max(stacks, db.multipliers["HexDoom"]), out["HexDoomLimit"])
	}
}

// inherentRageLoss is CalcPerform.lua:934-935.
func inherentRageLoss(db *ModDB) {
	out := db.output
	out["InherentRageLossDelay"] = 2 + db.Sum("BASE", "InherentRageLossDelay")
	out["InherentRageLoss"] = 0
	if !db.Flag("InherentRageLossIsPrevented") {
		out["InherentRageLoss"] = 10 * (1 + db.Sum("INC", "InherentRageLoss")/100)
	}
}

// missingUnreservedLife is CalcDefence.lua:1228-1233.
func missingUnreservedLife(db *ModDB) {
	out := db.output
	missingLife := 0.0
	if db.GetCondition("AffectedByPetrifiedBlood") && out["LifeUnreserved"] > 0 {
		current := math.Min(out["LifeUnreserved"], out["Life"]*out["LowLifePercentage"]/100)
		missingLife = 100 * (out["LifeUnreserved"] - current) / out["LifeUnreserved"]
	}
	if o, set := db.Override("MissingUnreservedLifePercent"); set {
		missingLife = o
	}
	out["MissingUnreservedLifePercent"] = missingLife
}

// wardRechargeAndBlind is CalcDefence.lua:1551 (data.misc.WardRechargeDelay
// 2) and 1585-1587.
func wardRechargeAndBlind(db *ModDB) {
	out := db.output
	out["WardRechargeDelay"] = 2 / (1 + db.Sum("INC", "WardRechargeFaster")/100)
	if db.enemy.Flag("Blind") {
		out["BlindEffectMod"] = calcMod(db.enemy, nil, "BlindEffect", "BuffEffectOnSelf") * 100
	}
}

// rawArmourDefense is CalcDefence.lua:1637: nil when ArmourDefense is 0.
func rawArmourDefense(out map[string]float64) {
	if out["ArmourDefense"] > 0 {
		out["RawArmourDefense"] = (1 + out["ArmourDefense"]) * 100
	}
}

// selfDebuffs is CalcDefence.lua:1668-1699: debuff expiration, the
// durations and effects of ailments on the player, and Firesong's
// SelfIgniteDurationToElementalAilments.
func selfDebuffs(db *ModDB) {
	out, enemy := db.output, db.enemy
	out["DebuffExpirationRate"] = db.Sum("BASE", "SelfDebuffExpirationRate")
	out["DebuffExpirationModifier"] = 10000 / (100 + out["DebuffExpirationRate"])
	out["showDebuffExpirationModifier"] = b2f(out["DebuffExpirationModifier"] != 100)
	out["SelfBlindDuration"] = db.More("SelfBlindDuration") * (100 + db.Sum("INC", "SelfBlindDuration")) *
		out["DebuffExpirationModifier"] / 100
	igniteToAll := db.Flag("IgniteDurationAppliesToElementalAilments")
	if igniteToAll {
		if inc := db.Sum("INC", "SelfIgniteDuration"); inc != 0 {
			db.NewMod("SelfIgniteDurationToElementalAilments", "INC", inc, "Firesong")
		}
		if more := db.More("SelfIgniteDuration"); more != 1 {
			db.NewMod("SelfIgniteDurationToElementalAilments", "MORE", more, "Firesong")
		}
	}
	expiry := func(a string) float64 {
		return 100 + out["DebuffExpirationRate"] + db.Sum("BASE", "Self"+a+"DebuffExpirationRate")
	}
	for _, a := range []string{"Bleed", "Poison"} { // data.nonElementalAilmentTypeList
		more := db.More("Self"+a+"Duration", "SelfAilmentDuration")
		inc := (100 + db.Sum("INC", "Self"+a+"Duration", "SelfAilmentDuration")) * 100
		out["Self"+a+"Duration"] = inc * more / expiry(a)
	}
	for _, a := range elementalAilmentTypeList {
		all := igniteToAll && a != "Ignite"
		more := db.More("Self"+a+"Duration", "SelfAilmentDuration", "SelfElementalAilmentDuration")
		inc := 100 + db.Sum("INC", "Self"+a+"Duration", "SelfAilmentDuration", "SelfElementalAilmentDuration")
		if all {
			more *= db.More("SelfIgniteDuration")
			inc += db.Sum("INC", "SelfIgniteDuration")
		}
		out["Self"+a+"Duration"] = more * (inc * 100) / expiry(a)
	}
	for _, a := range ailmentTypeList {
		src := enemy
		if db.Flag("Condition:" + a + "edSelf") {
			src = db
		}
		out["Self"+a+"Effect"] = calcMod(db, nil, "Self"+a+"Effect") * calcMod(src, nil, "Enemy"+a+"Effect") * 100
	}
}

// totemResist is the totem half of the resistance loop (CalcDefence.lua:
// 600-611, 633-637): the Totem<Elem>Resist outputs from the player's
// Totem*Resist mods.
func totemResist(db *ModDB, elem string) {
	out := db.output
	maxNames, names := []string{"Totem" + elem + "ResistMax"}, []string{"Totem" + elem + "Resist"}
	if elem != "Chaos" {
		maxNames, names = append(maxNames, "TotemElementalResistMax"), append(names, "TotemElementalResist")
	}
	maxRes, ok := db.Override("Totem" + elem + "ResistMax")
	if !ok {
		maxRes = math.Min(maxResistCap, db.Sum("BASE", maxNames...))
	}
	total, ok := db.Override("Totem" + elem + "Resist")
	if !ok {
		total = db.Sum("BASE", names...) * math.Max(calcMod(db, nil, names...), 0)
	}
	total, maxRes = modfInt(total), modfInt(maxRes)
	final := math.Max(math.Min(total, maxRes), resistFloor)
	out["Totem"+elem+"Resist"] = final
	out["Totem"+elem+"ResistTotal"] = total
	out["Totem"+elem+"ResistOverCap"] = math.Max(0, total-maxRes)
	out["MissingTotem"+elem+"Resist"] = math.Max(0, maxRes-final)
}
