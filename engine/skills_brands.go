package engine

import "math"

// Brand skill hooks.

func init() {
	registerSkill([]string{
		"Arcanist Brand", "Armageddon Brand", "Armageddon Brand of Recall", "Penance Brand",
		"Penance Brand of Dissipation", "Penance Brand of Conduction", "Storm Brand", "Storm Brand of Indecision",
	}, skillHooks{preDamage: brandPreDamage})
	registerSkill([]string{"Wintertide Brand"}, skillHooks{preDamage: wintertideBrandPreDamage})
}

// Data/Skills/act_int.lua Arcanist/Armageddon/Penance/Storm Brand preDamageFunc.
//
// Brands hit once per activation: repeatFrequency / (1 + inc Speed and
// BrandActivationFrequency) / more BrandActivationFrequency. Penance Brand
// activates once per its max energy.
func brandPreDamage(_ *ModDB, s *activeSkill, _ map[string]float64) {
	data := s.data
	f, _ := num(data.val("repeatFrequency"))
	if s.active.ge.Name == "Penance Brand" {
		f *= s.mods.SumCfg(s.cfg, "BASE", "Multiplier:PenanceBrandMaxEnergy")
	}
	inc := s.mods.SumCfg(s.cfg, "INC", "Speed", "BrandActivationFrequency")
	data.hitTimeOverride = optF{f / (1 + inc/100) / s.mods.MoreCfg(s.cfg, "BrandActivationFrequency"), true}
}

// Data/Skills/act_int.lua Wintertide Brand preDamageFunc.
//
// Part 1 averages the stages gained per activation over the attached
// duration, plus the strongest maximum-stage Wintertide's End debuff for
// its uptime.
func wintertideBrandPreDamage(actor *ModDB, s *activeSkill, _ map[string]float64) {
	data := s.data
	data.set("countsAttachedBrandsInDamage", s.part == 1)
	f, _ := num(data.val("repeatFrequency"))
	inc := s.mods.SumCfg(s.cfg, "INC", "Speed", "BrandActivationFrequency")
	hitTime := f / (1 + inc/100) / s.mods.MoreCfg(s.cfg, "BrandActivationFrequency")
	data.hitTimeOverride = optF{hitTime, true}
	if s.part == 1 && hitTime > 0 {
		list, cfg := s.mods, s.cfg
		skillMaxStages := list.SumCfg(cfg, "BASE", "Multiplier:WintertideBrandMaxStages")
		debuffDurationMult := 1 / math.Max(buffExpirationSlowCap, calcMod(actor.enemy, cfg, "BuffExpireFaster"))
		duration := skillDuration(list, cfg, data, nil) * debuffDurationMult
		if duration <= 0 {
			return
		}
		maxStages := math.Min(math.Floor(duration/hitTime), skillMaxStages)
		timeAtMaxStages := math.Max(duration-maxStages*hitTime, 0)
		damagePerStage := list.SumCfg(cfg, "BASE", "Multiplier:WintertideBrandDamagePerStage")
		averageStages := (maxStages*(maxStages-1)/2*hitTime + maxStages*timeAtMaxStages) / duration
		endBase := numOr(data.val("durationTertiary"), 0) + list.SumCfg(cfg, "BASE", "Duration", "TertiaryDuration")
		endMod := math.Max(calcMod(list, cfg, "Duration", "TertiaryDuration"), 0)
		endDuration := math.Ceil(endBase*endMod*debuffDurationMult*serverTickRate) / serverTickRate
		endUptime := math.Min(endDuration/duration, 1)
		attached := numOr(data.val("attachedBrandCount"), 0)
		mult := attached*(100+averageStages*damagePerStage) + endUptime*(100+maxStages*damagePerStage) - 100
		list.NewMod("Damage", "MORE", mult, "Wintertide Brand Average Multiplier")
	}
}
