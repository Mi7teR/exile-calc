package engine

import (
	"math"
	"strings"
)

// skillCooldown is calcSkillCooldown, for skills with a cooldown (the
// "Calculate skill type stats" branch of calcs.offence; trap cooldowns are
// not ported).
func skillCooldown(list *ModDB, cfg *Cfg, data *skillData) (float64, bool) {
	base, has := data.cooldown.get()
	added := list.SumCfg(cfg, "BASE", "CooldownRecovery")
	if !has && added == 0 {
		return 0, false
	}
	if o, ok := list.OverrideCfg(cfg, "CooldownRecovery"); ok {
		return o, true
	}
	cd := (base + added) / math.Max(0, calcMod(list, cfg, "CooldownRecovery"))
	stored, _ := data.storedUses.get()
	if stored > 1 || list.SumCfg(cfg, "BASE", "AdditionalCooldownUses") > 0 {
		return cd, true
	}
	return math.Ceil(cd*serverTickRate) / serverTickRate, true
}

// hitAndSpeed is the hit chance and attack/cast speed part of
// calcs.offence for one pass.
func (c *Calculator) hitAndSpeed(actor *ModDB, s *activeSkill, p *hitPass) {
	list, cfg, data, out := s.mods, s.cfg, s.data, p.out
	enemy := actor.enemy
	isAttack := s.flags["attack"]
	// PoB calls skillModList:More("MORE", cfg, ...): the cfg is the string
	// "MORE", so only mods without flags count.
	out["Accuracy"] = math.Max(0, math.Floor(list.SumCfg(p.cfg, "BASE", "Accuracy")*
		(1+list.SumCfg(p.cfg, "INC", "Accuracy")/100)*list.MoreCfg(nil, "Accuracy")))
	accVsEnemy := math.Max(0, math.Floor(list.SumCfg(p.cfg, "BASE", "Accuracy", "AccuracyVsEnemy")*
		(1+list.SumCfg(p.cfg, "INC", "Accuracy", "AccuracyVsEnemy")/100)*list.MoreCfg(nil, "Accuracy", "AccuracyVsEnemy")))
	// Precise Technique: this pass's accuracy against the player's life.
	player := actor
	if actor.owner != nil {
		player = actor.owner
	}
	list.conditions[strings.ReplaceAll(p.label, " ", "")+"AccRatingHigherThanMaxLife"] = out["Accuracy"] > player.output["Life"]
	if list.Flag("Condition:OffHandAccuracyIsMainHandAccuracy") {
		if p.label == "Main Hand" {
			p.shared.mainAcc, p.shared.mainAccVsEnemy, p.shared.hasMainAcc = out["Accuracy"], accVsEnemy, true
		} else if p.label == "Off Hand" && p.shared.hasMainAcc {
			out["Accuracy"], accVsEnemy = p.shared.mainAcc, p.shared.mainAccVsEnemy
		}
	}
	if !isAttack || list.FlagCfg(p.cfg, "CannotBeEvaded") || truthy(data.val("cannotBeEvaded")) ||
		enemy.Flag("CannotEvade") {
		out["AccuracyHitChance"] = 100
	} else {
		ev := math.Max(round(val(enemy, "Evasion"), 0), 0)
		out["AccuracyHitChance"] = hitChance(ev, accVsEnemy) * calcMod(list, p.cfg, "HitChance")
	}
	block := math.Max(math.Min(enemy.SumCfg(p.cfg, "BASE", "BlockChance"), 100)-list.SumCfg(p.cfg, "BASE", "reduceEnemyBlock"), 0)
	if enemy.Flag("CannotBlockAttacks") && isAttack {
		block = 0
	}
	out["HitChance"] = out["AccuracyHitChance"] * (1 - block/100)

	ge := s.active.ge
	repeats := math.Max(1, p.repeats)
	triggered := truthy(data.val("triggered"))
	switch {
	case ge.CastTime == 0 && !ge.NoCastTime && data.val("castTimeOverride") == nil && !triggered:
		out["Time"], out["Speed"] = 0, 0
		hitTime(s, out, false)
		return
	case data.timeOverride.set:
		t, _ := data.timeOverride.get()
		if p.trapCooldown > 0 {
			t = math.Max(t, p.trapCooldown)
		} else if p.hasCooldown {
			t = math.Max(t, p.cooldown)
		}
		out["Time"], out["Speed"] = t, 1/t
		hitTime(s, out, false)
		return
	case truthy(data.val("fixedCastTime")):
		out["Time"], out["Speed"] = ge.CastTime, 1/ge.CastTime
		hitTime(s, out, false)
		return
	case data.val("triggerTime") != nil && triggered:
		t, _ := num(data.val("triggerTime"))
		t /= 1 + list.SumCfg(p.cfg, "INC", "CooldownRecovery")/100
		if n := list.SumCfg(p.cfg, "BASE", "ActiveSkillsLinkedToTrigger"); n > 0 {
			t *= n
		}
		out["Time"], out["TriggerTime"], out["Speed"] = t, t, 1/t
		hitTime(s, out, false)
		return
	case data.val("triggerRate") != nil && triggered:
		r, _ := num(data.val("triggerRate"))
		out["Time"], out["TriggerTime"], out["Speed"] = 1/r, 1/r, r
		data.set("showAverage", false)
		hitTime(s, out, false)
		return
	}
	var baseTime float64
	if isAttack {
		rate := p.src.attackRate
		if m, ok := num(data.val("attackSpeedMultiplier")); ok && rate != 0 {
			rate *= 1 + m/100
		}
		if rate == 0 {
			rate = 1
		}
		switch {
		case truthy(data.val("castTimeOverridesAttackTime")):
			baseTime = ge.CastTime
		case calcMod(list, cfg, "SkillAttackTime") > 0:
			baseTime = (1/rate + list.SumCfg(p.cfg, "BASE", "Speed")) * calcMod(list, cfg, "SkillAttackTime")
		default:
			baseTime = 1/rate + list.SumCfg(p.cfg, "BASE", "Speed")
		}
	} else {
		baseTime = ge.CastTime
		if v, ok := num(data.val("castTimeOverride")); ok {
			baseTime = v
		}
		if ge.NoCastTime && data.val("castTimeOverride") == nil {
			baseTime = 1 // castTimeOverride or castTime or 1
		}
	}
	// Trauma (HasTrauma) is not ported.
	inc := list.SumCfg(p.cfg, "INC", "Speed")
	more := list.MoreCfg(p.cfg, "Speed")
	if s.flags["warcry"] {
		out["WarcryCastTime"] = warcryCastTime(actor, s)
		out["Speed"] = 1 / out["WarcryCastTime"]
	} else {
		out["Speed"] = 1 / (baseTime/round((1+inc/100)*more, 2) + list.SumCfg(p.cfg, "BASE", "TotalAttackTime") + list.SumCfg(p.cfg, "BASE", "TotalCastTime"))
	}
	if s.flags["selfCast"] {
		out["Speed"] *= actor.output["ActionSpeedMod"]
	}
	if s.flags["totem"] {
		out["TotemActionSpeed"] = 1 + actor.Sum("INC", "TotemActionSpeed")/100
		out["Speed"] *= out["TotemActionSpeed"]
		// Totems that fire once (Siege Ballista of Trarthus) hit once per
		// placement and attack.
		if truthy(data.val("totemFireOnce")) {
			out["HitTime"] = 1/out["Speed"] + p.global["TotemPlacementTime"]
			out["HitSpeed"] = 1 / out["HitTime"]
		}
	}
	if p.hasCooldown {
		out["Cooldown"] = p.cooldown
		out["Speed"] = math.Min(out["Speed"], 1/p.cooldown*repeats)
		if s.flags["selfCast"] {
			data.set("showAverage", false)
		}
	}
	if !s.types[stChannel] {
		out["Speed"] = math.Min(out["Speed"], serverTickRate*repeats)
	}
	if out["Speed"] == 0 {
		out["Time"] = 0
	} else {
		out["Time"] = 1 / out["Speed"]
	}
	hitTime(s, out, false)
}

// hitTime is the HitTime and HitSpeed part of the speed calculation:
// skills that hit on their own timer (hitTimeOverride, hitTimeMultiplier).
func hitTime(s *activeSkill, out map[string]float64, tickCap bool) {
	data := s.data
	if truthy(data.val("triggeredOnDeath")) {
		return
	}
	if v, ok := data.hitTimeOverride.get(); ok && v != 0 {
		out["HitTime"], out["HitSpeed"] = v, 1/v
		return
	}
	m, ok := num(data.val("hitTimeMultiplier"))
	if !ok || out["Time"] == 0 {
		return
	}
	out["HitTime"] = out["Time"] * m
	cd, hasCD := out["Cooldown"]
	switch {
	case hasCD && truthy(data.val("triggered")):
		out["HitSpeed"] = 1 / math.Max(out["HitTime"], cd)
	case hasCD:
		out["HitSpeed"] = 1 / (out["HitTime"] + cd)
	case tickCap:
		out["HitSpeed"] = math.Min(1/out["HitTime"], serverTickRate)
	default:
		out["HitSpeed"] = 1 / out["HitTime"]
	}
}
