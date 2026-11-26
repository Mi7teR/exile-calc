package engine

// Hooks of skills outside the other groups, and the warcry share.

func init() {
	registerSkill([]string{"On Kill Monster Explosion"}, skillHooks{preDamage: onKillMonsterExplosionPreDamage})
	registerSkill([]string{"Avenging Flame"}, skillHooks{preDamage: avengingFlamePreDamage})
	registerSkill([]string{"Bladefall of Trarthus"}, skillHooks{preDamage: bladefallOfTrarthusPreDamage})
	registerSkill(
		[]string{"Tornado of Elemental Turbulence"},
		skillHooks{preDamage: tornadoOfElementalTurbulencePreDamage},
	)
	registerSkill([]string{"Bodyswap", "Bodyswap of Sacrifice"}, skillHooks{preDamage: bodyswapPreDamage})
	registerSkill([]string{"Blade Blast", "Blade Blast of Unloading"}, skillHooks{preDamage: bladeBlastPreDamage})
	registerSkill([]string{"Bane", "Bane of Condemnation"}, skillHooks{preSkillType: banePreSkillType})
	registerSkill([]string{"Tornado"}, skillHooks{preDamage: tornadoPreDamage})
	registerSkill([]string{"Righteous Fire"}, skillHooks{preDamage: righteousFirePreDamage})
	registerSkill(
		[]string{"Righteous Fire of Arcane Devotion"},
		skillHooks{preDamage: righteousFireOfArcaneDevotionPreDamage},
	)
	registerSkill([]string{"Vaal Righteous Fire"}, skillHooks{preDamage: vaalRighteousFirePreDamage})
	registerSkill([]string{"Rallying Cry"}, skillHooks{warcryShare: rallyingCryWarcryShare})
	registerSkill([]string{"Seismic Cry"}, skillHooks{warcryShare: seismicCryWarcryShare})
}

// Data/Skills/other.lua On Kill Monster Explosion preDamageFunc.
func onKillMonsterExplosionPreDamage(_ *ModDB, s *activeSkill, out map[string]float64) {
	enemyExplode(s, out)
}

// Data/Skills/sup_str.lua Avenging Flame preDamageFunc.
//
// The trigger source's totem life (triggerSourceUUID's TotemLife).
func avengingFlamePreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	data := s.data
	add := numOr(data.val("triggerSourceTotemLife"), 0) * numOr(data.val("lifeDealtAsFire"), 0) / 100
	data.set("FireMin", numOr(data.val("FireMin"), 0)+add)
	data.set("FireMax", numOr(data.val("FireMax"), 0)+add)
}

// Data/Skills/act_dex.lua Bladefall of Trarthus preDamageFunc.
func bladefallOfTrarthusPreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	freq := numOr(s.data.val("hitFrequency"), 0)
	s.data.hitTimeOverride = optF{freq / (1 + numOr(s.data.val("incVolleyFrequency"), 0)/100), true}
}

// Data/Skills/act_dex.lua Tornado of Elemental Turbulence preDamageFunc.
func tornadoOfElementalTurbulencePreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	s.data.set("hitTimeOverride", s.data.val("damageInterval"))
	mulDPS(s.data, s.mods.SumCfg(s.cfg, "BASE", "Multiplier:TornadoofElementalTurbulenceStage"))
}

// Data/Skills/act_dex.lua:18037-18039 Tornado preDamageFunc: one hit per
// damage interval.
func tornadoPreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	s.data.set("hitTimeOverride", s.data.val("damageInterval"))
}

// Data/Skills/act_int.lua Bodyswap preDamageFunc.
func bodyswapPreDamage(actor *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 1 {
		m, _ := num(s.data.val("selfFireExplosionLifeMultiplier"))
		life := actor.output["Life"]
		if s.flags["totem"] {
			life = out["TotemLife"]
		}
		s.data.set("FireBonusMin", life*m)
		s.data.set("FireBonusMax", life*m)
	}
}

// Data/Skills/act_int.lua Righteous Fire preDamageFunc: the burn is a
// share of the totem's life and energy shield, or the player's while more
// than 1 life is unreserved.
func righteousFirePreDamage(actor *ModDB, s *activeSkill, out map[string]float64) {
	lifeMult, esMult := numOr(s.data.val("RFLifeMultiplier"), 0), numOr(s.data.val("RFESMultiplier"), 0)
	switch {
	case s.flags["totem"] && out["TotemLife"] > 1:
		es := s.mods.SumCfg(s.cfg, "BASE", "TotemEnergyShield") // CalcOffence.lua output.TotemEnergyShield
		s.data.set("FireDot", out["TotemLife"]*lifeMult+es*esMult)
	case actor.output["LifeUnreserved"] > 1:
		s.data.set("FireDot", actor.output["Life"]*lifeMult+actor.output["EnergyShield"]*esMult)
	}
}

// Data/Skills/act_int.lua Righteous Fire of Arcane Devotion preDamageFunc:
// the burn is a share of the player's mana.
func righteousFireOfArcaneDevotionPreDamage(actor *ModDB, s *activeSkill, _ map[string]float64) {
	if actor.output["LifeUnreserved"] > 1 {
		s.data.set("FireDot", actor.output["Mana"]*numOr(s.data.val("RFManaMultiplier"), 0))
	}
}

// Data/Skills/act_int.lua Vaal Righteous Fire preDamageFunc: the burn is a
// share of the life and energy shield sacrificed on use.
func vaalRighteousFirePreDamage(actor *ModDB, s *activeSkill, out map[string]float64) {
	m := numOr(s.data.val("percentSacrificed"), 0) * numOr(s.data.val("RFMultiplier"), 0)
	if s.flags["totem"] {
		s.data.set("FireDot", out["TotemLife"]*m)
	} else {
		s.data.set("FireDot", (actor.output["Life"]+actor.output["EnergyShield"])*m)
	}
}

// Data/Skills/act_str.lua Rallying Cry warcryShare (Modules/CalcPerform.lua
// mode_buffs): the exert damage per ally, read with the main skill's cfg.
func rallyingCryWarcryShare(db *ModDB, s, main *activeSkill) {
	if !s.mods.Flag("CannotShareWarcryBuffs") && !db.Flag("RallyingActive") {
		db.NewMod(
			"RallyingExertMoreDamagePerAlly",
			"BASE",
			s.mods.SumCfg(main.cfg, "BASE", "RallyingCryExertDamageBonus"),
			"",
		)
		db.Add(Mod{Name: "RallyingActive", Type: "FLAG", Value: true})
	}
}

// Data/Skills/act_str.lua Seismic Cry warcryShare (Modules/CalcPerform.lua
// mode_buffs): the area, read with the main skill's cfg.
func seismicCryWarcryShare(db *ModDB, s, main *activeSkill) {
	if !db.Flag("SeismicActive") {
		db.NewMod("SeismicMoreAoE", "BASE", s.mods.SumCfg(main.cfg, "BASE", "SeismicAoEMoreMultiplier"), "")
		db.Add(Mod{Name: "SeismicActive", Type: "FLAG", Value: true})
	}
}

// Data/Skills/act_dex.lua:1374-1379 Blade Blast (and Blade Blast of
// Unloading, :1468-1473) preDamageFunc: one hit per detonated blade (the
// stage count, dpsBaseMultiplier), once per second for "Blade Hits Per
// Sec".
func bladeBlastPreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	mulDPS(s.data, numOr(s.data.val("dpsBaseMultiplier"), 0))
	if s.part == 2 {
		s.data.hitTimeOverride = optF{1, true}
	}
}

// Data/Skills/act_int.lua:1470-1481 Bane (and Bane of Condemnation,
// :1647-1658) preSkillTypeFunc: the curses of its group it applies
// (Condition:AppliedByBane), up to the enemy curse limit.
func banePreSkillType(actor *ModDB, s *activeSkill) {
	count := 0.0
	limit, hasLimit := actor.output["EnemyCurseLimit"]
	for _, x := range actor.activeSkills {
		if x.group == s.group && x.mods.GetCondition("AppliedByBane") {
			count++
			if hasLimit && count == limit {
				break
			}
		}
	}
	s.mods.NewMod("Multiplier:CurseApplied", "BASE", count, "Base")
}
