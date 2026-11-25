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
	add := numOr(data["triggerSourceTotemLife"], 0) * numOr(data["lifeDealtAsFire"], 0) / 100
	data["FireMin"] = numOr(data["FireMin"], 0) + add
	data["FireMax"] = numOr(data["FireMax"], 0) + add
}

// Data/Skills/act_dex.lua Bladefall of Trarthus preDamageFunc.
func bladefallOfTrarthusPreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	s.data["hitTimeOverride"] = numOr(s.data["hitFrequency"], 0) / (1 + numOr(s.data["incVolleyFrequency"], 0)/100)
}

// Data/Skills/act_dex.lua Tornado of Elemental Turbulence preDamageFunc.
func tornadoOfElementalTurbulencePreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	s.data["hitTimeOverride"] = s.data["damageInterval"]
	mulDPS(s.data, s.mods.SumCfg(s.cfg, "BASE", "Multiplier:TornadoofElementalTurbulenceStage"))
}

// Data/Skills/act_int.lua Bodyswap preDamageFunc.
func bodyswapPreDamage(actor *ModDB, s *activeSkill, out map[string]float64) {
	if s.part == 1 {
		m, _ := num(s.data["selfFireExplosionLifeMultiplier"])
		life := actor.output["Life"]
		if s.flags["totem"] {
			life = out["TotemLife"]
		}
		s.data["FireBonusMin"], s.data["FireBonusMax"] = life*m, life*m
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
