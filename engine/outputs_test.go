package engine

import (
	"os"
	"strings"
	"testing"
)

// The outputs below are compared with the oracle (PoB v2.68.1) on every
// corpus build, one test per family.

// TestDefenceMiscOutputs: CalcDefence.lua:528, 792, 1087-1090, 1125-1130,
// 1228-1233, 1367-1368, 1551, 1585-1587, 1618-1621, 1637.
func TestDefenceMiscOutputs(t *testing.T) {
	oracleStats(t, false, "PhysicalResist", "ShowBlockEffect", "Gear:Ward", "Gear:EnergyShield", "Gear:Armour",
		"Gear:Evasion", "splitEvade", "noSplitEvade", "MissingUnreservedLifePercent",
		"EnergyShieldRechargeAppliesToLife", "EnergyShieldRechargeAppliesToEnergyShield", "WardRechargeDelay",
		"BlindEffectMod", "CorruptedBloodImmunity", "MaimImmunity", "HinderImmunity", "KnockbackImmunity",
		"RawArmourDefense")
}

// TestTotemResists: CalcDefence.lua:600-611, 633-637 with the base mods of
// CalcSetup.lua:39-42, 621-624 and the totem aura mods of CalcPerform.lua:
// 2551-2581.
func TestTotemResists(t *testing.T) {
	var keys []string
	for _, e := range []string{"Fire", "Cold", "Lightning", "Chaos"} {
		keys = append(keys, "Totem"+e+"Resist", "Totem"+e+"ResistTotal", "Totem"+e+"ResistOverCap",
			"MissingTotem"+e+"Resist")
	}
	oracleStats(t, true, keys...)
}

// TestSelfDebuffs: CalcDefence.lua:1668-1699.
func TestSelfDebuffs(t *testing.T) {
	keys := []string{"DebuffExpirationRate", "DebuffExpirationModifier", "showDebuffExpirationModifier",
		"SelfBlindDuration"}
	for _, a := range ailmentTypeList {
		keys = append(keys, "Self"+a+"Duration", "Self"+a+"Effect")
	}
	oracleStats(t, true, keys...)
}

// TestPerformOutputs: CalcPerform.lua:83, 133-134, 934-935, 990-1004,
// 1380-1388, 1511-1514, 3375, 3972-4013 (and CalcActiveSkill.lua:573-575).
func TestPerformOutputs(t *testing.T) {
	oracleStats(t, false, "ChaosInoculation", "LowestOfMaximumLifeAndMaximumMana", "MaximumLifeManaDifference",
		"InherentRageLoss", "InherentRageLossDelay", "PowerChargesDuration", "FrenzyChargesDuration",
		"EnduranceChargesDuration", "HexDoomLimit", "LootQuantityNormalEnemies", "LootRarityMagicEnemies",
		"GuardSkillActive", "GemHasLevel", "GemLevel", "GemHasQuality", "GemQuality")
}

// TestEHPOutputs: CalcDefence.lua:1519-1533, 1955, 2183-2293, 2353-2510,
// 3008-3134, 3401-3546 (stun, bypass and mitigation flags, recoup over the
// survival time, Petrified Blood, DoT EHP, degens, net regeneration).
func TestEHPOutputs(t *testing.T) {
	keys := []string{"AnyTakenReflect", "StunThreshold", "StunAvoidChance", "StunDuration", "BlockDuration",
		"InterruptStunAvoidChance", "SelfStunChance", "CappingLife", "AnyBypass", "ehpSectionAnySpecificTypes",
		"OnlySharedMindOverMatter", "AnySpecificMindOverMatter", "AnyGuard", "OnlySharedGuard", "AnyAegis",
		"LifeLossLostMax", "LifeLossLostAvg", "netLifeRecoupAndLossLostOverTimeMax",
		"netLifeRecoupAndLossLostOverTimeAvg", "showNetRecoup", "TotalBuildDegen", "NetLifeRegen", "NetManaRegen",
		"NetEnergyShieldRegen", "TotalNetRegen"}
	for _, d := range dmgTypes {
		keys = append(keys, d+"DotEHP", d+"BuildDegen", d+"RecoupableDamageTaken")
	}
	for _, r := range recoupTypes {
		keys = append(keys, "Total"+r+"RecoupRecovery", "Total"+r+"PseudoRecoup", r+"RecoupRecoveryMax",
			r+"RecoupRecoveryAvg", "PhysicalDamageMitigated"+r+"PseudoRecoup",
			"PhysicalDamageMitigated"+r+"PseudoRecoupDuration")
	}
	oracleStats(t, false, keys...)
}

// TestSkillTypeOutputs: CalcOffence.lua:1068-1138 (chain, split, fork and
// pierce counts; the *String outputs only when PoB's value is a number),
// 1190-1250 (effect mods), 1314-1408 (trap and mine radii, stored uses),
// 1441-1445 (totem defences), 1527-1629 (secondary, soul gain prevention
// and totem durations), with the level fields of CalcActiveSkill.lua:
// 557-565, 615-622.
func TestSkillTypeOutputs(t *testing.T) {
	oracleStats(t, false, "ChainMaxString", "SplitCount", "SplitCountString", "ForkedCount", "ForkRemaining",
		"ForkCountString", "PierceCountString", "AuraEffectMod", "CurseEffectMod", "HeraldBuffEffectMod",
		"TrapTriggerRadius", "TrapTriggerRadiusMetre", "StoredUses", "MineDetonationRadius",
		"MineDetonationRadiusMetre", "MineAuraRadius", "MineAuraRadiusMetre", "TotemLifeMod", "TotemEnergyShield",
		"TotemBlockChance", "TotemArmour", "DurationSecondary", "SoulGainPreventionDuration", "TotemDurationMod",
		"TotemDuration")
}

// TestOffenceMiscOutputs: CalcOffence.lua:565-566 (with CalcSetup.lua:
// 949-952, 1071-1076), 2025, 2206-2210, 2267-2270, 2435-2446, 2502-2504,
// 2627-2630, 2945-2998 (pacts), 3022-3048 (Ruthless, Fist of War),
// 3315-3336, 3505-3519, 3834-3844, 6170, 6191-6193, 6304-6306, CalcPerform.
// lua:2186-2193 (Req*String when a number) and the skills' SkillDPSMultiplier.
func TestOffenceMiscOutputs(t *testing.T) {
	oracleStats(t, false, "enemyBlockChance", "CastRate", "BrandTicks", "QuantityMultiplier", "PreciseTechnique",
		"CreatePactOffensiveCalcSection", "PactOfBeidatCooldown", "PactOfBeidatCastTime", "PactOfBeidatCalculated",
		"BeidatEmpoweredCount", "PactOfGhorrCooldown", "PactOfGhorrCastTime", "PactOfGhorrCalculated",
		"GhorrEmpoweredCount", "PactOfLyciaCooldown", "PactOfLyciaCastTime", "PactOfLyciaCalculated",
		"LyciaEmpoweredCount", "RuthlessBlowHitMultiplier", "RuthlessBlowAilmentMultiplier", "FistOfWarCooldown",
		"FistOfWarDamageMultiplier", "FistOfWarUptimeRatio", "AvgFistOfWarDamage", "AvgFistOfWarDamageEffect",
		"MaxFistOfWarDamageEffect", "ScaledDamageEffect", "TripleDamageChanceOnCrit", "TripleDamageEffect",
		"DoubleDamageChanceOnCrit", "DoubleDamageEffect", "DamageRolls", "CritDamageRolls",
		"AverageHitToMonsterLifePercentage", "AverageBurstHits", "AverageBurstDamage", "ShowBurst", "CombinedAvg",
		"CombinedAvgToMonsterLife", "showTotalDotDPS", "ReqStrString", "ReqDexString", "ReqIntString",
		"ReqOmniString", "LifeFlaskRecovery", "LifeFlaskCharges", "SkillDPSMultiplier", "NormalHitsPerCast")
}

// TestSelfHitAndTriggerOutputs: CalcOffence.lua:1196-1202 (reserved mods),
// 1662-1690 (skill uptime), 6013-6139 with CalcDefence.lua:86-145 (self hit
// damage), CalcTriggers.lua:397, 720-729.
func TestSelfHitAndTriggerOutputs(t *testing.T) {
	oracleStats(t, false, "SelfHitDamage", "LifeReservedMod", "ManaReservedMod", "DurationUptime",
		"DurationSecondaryUptime", "ChannelTimeToTrigger", "VixensTooMuchCastSpeedWarn",
		"VixenModeNoVixenGlovesWarn")
}

// TestTripleDamageChance pins the user's decision to keep the game-correct
// triple damage sum where PoB's "Sum or 0 + self + onCrit" (CalcOffence.lua:
// 3317) keeps only Sum: 10 base, 5 from the enemy and 40 on crit at 50%
// crit chance give 35, not PoB's 10; the cap is 100.
func TestTripleDamageChance(t *testing.T) {
	if got := tripleDamageChance(10, 5, 40, 50); got != 35 {
		t.Errorf("tripleDamageChance = %v, want 35 (PoB: 10)", got)
	}
	if got := tripleDamageChance(90, 20, 0, 0); got != 100 {
		t.Errorf("tripleDamageChance = %v, want the cap 100", got)
	}
}

// TestPlayerOutputCrossCheck compares every key of
// testdata/golden/player-output-keys.txt with the oracle on all corpus
// builds. zKMTzbWQ2h1C's leech keys wait for map mods (TestLeechStats).
func TestPlayerOutputCrossCheck(t *testing.T) {
	raw, err := os.ReadFile("../testdata/golden/player-output-keys.txt")
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for l := range strings.SplitSeq(string(raw), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			keys = append(keys, l)
		}
	}
	var leech []string
	for _, k := range keys {
		if (strings.HasPrefix(k, "Life") || strings.HasPrefix(k, "Mana")) && strings.Contains(k, "Leech") &&
			!strings.HasPrefix(k, "Max") {
			leech = append(leech, k)
		}
	}
	oracleStatsExcept(t, false, map[string][]string{"zKMTzbWQ2h1C": leech}, keys...)
}
