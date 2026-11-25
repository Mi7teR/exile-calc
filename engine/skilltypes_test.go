package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

// TestSkillTypesMatchGlobalLua checks skillTypeNames against the SkillType
// table in PoB's Data/Global.lua. POB_SRC overrides the PoB source directory.
func TestSkillTypesMatchGlobalLua(t *testing.T) {
	// skillTypeNames maps each PoB SkillType name to its id.
	skillTypeNames := map[string]int{
		"Attack":                               stAttack,
		"Spell":                                stSpell,
		"Projectile":                           stProjectile,
		"DualWieldOnly":                        stDualWieldOnly,
		"Buff":                                 stBuff,
		"Minion":                               stMinion,
		"Damage":                               stDamage,
		"Area":                                 stArea,
		"Duration":                             stDuration,
		"RequiresShield":                       stRequiresShield,
		"ProjectileSpeed":                      stProjectileSpeed,
		"HasReservation":                       stHasReservation,
		"ReservationBecomesCost":               stReservationBecomesCost,
		"Trappable":                            stTrappable,
		"Totemable":                            stTotemable,
		"Mineable":                             stMineable,
		"ElementalStatus":                      stElementalStatus,
		"MinionsCanExplode":                    stMinionsCanExplode,
		"Chains":                               stChains,
		"Melee":                                stMelee,
		"MeleeSingleTarget":                    stMeleeSingleTarget,
		"Multicastable":                        stMulticastable,
		"TotemCastsAlone":                      stTotemCastsAlone,
		"Multistrikeable":                      stMultistrikeable,
		"CausesBurning":                        stCausesBurning,
		"SummonsTotem":                         stSummonsTotem,
		"TotemCastsWhenNotDetached":            stTotemCastsWhenNotDetached,
		"Physical":                             stPhysical,
		"Fire":                                 stFire,
		"Cold":                                 stCold,
		"Lightning":                            stLightning,
		"Triggerable":                          stTriggerable,
		"Trapped":                              stTrapped,
		"Movement":                             stMovement,
		"DamageOverTime":                       stDamageOverTime,
		"RemoteMined":                          stRemoteMined,
		"Triggered":                            stTriggered,
		"Vaal":                                 stVaal,
		"Aura":                                 stAura,
		"CanTargetUnusableCorpse":              stCanTargetUnusableCorpse,
		"RangedAttack":                         stRangedAttack,
		"Chaos":                                stChaos,
		"FixedSpeedProjectile":                 stFixedSpeedProjectile,
		"ThresholdJewelArea":                   stThresholdJewelArea,
		"ThresholdJewelProjectile":             stThresholdJewelProjectile,
		"ThresholdJewelDuration":               stThresholdJewelDuration,
		"ThresholdJewelRangedAttack":           stThresholdJewelRangedAttack,
		"Channel":                              stChannel,
		"DegenOnlySpellDamage":                 stDegenOnlySpellDamage,
		"InbuiltTrigger":                       stInbuiltTrigger,
		"Golem":                                stGolem,
		"Herald":                               stHerald,
		"AuraAffectsEnemies":                   stAuraAffectsEnemies,
		"NoRuthless":                           stNoRuthless,
		"ThresholdJewelSpellDamage":            stThresholdJewelSpellDamage,
		"Cascadable":                           stCascadable,
		"ProjectilesFromUser":                  stProjectilesFromUser,
		"MirageArcherCanUse":                   stMirageArcherCanUse,
		"ProjectileSpiral":                     stProjectileSpiral,
		"SingleMainProjectile":                 stSingleMainProjectile,
		"MinionsPersistWhenSkillRemoved":       stMinionsPersistWhenSkillRemoved,
		"ProjectileNumber":                     stProjectileNumber,
		"Warcry":                               stWarcry,
		"Instant":                              stInstant,
		"Brand":                                stBrand,
		"DestroysCorpse":                       stDestroysCorpse,
		"NonHitChill":                          stNonHitChill,
		"ChillingArea":                         stChillingArea,
		"AppliesCurse":                         stAppliesCurse,
		"CanRapidFire":                         stCanRapidFire,
		"AuraDuration":                         stAuraDuration,
		"AreaSpell":                            stAreaSpell,
		"OR":                                   stOR,
		"AND":                                  stAND,
		"NOT":                                  stNOT,
		"AppliesMaim":                          stAppliesMaim,
		"CreatesMinion":                        stCreatesMinion,
		"Guard":                                stGuard,
		"Travel":                               stTravel,
		"Blink":                                stBlink,
		"CanHaveBlessing":                      stCanHaveBlessing,
		"ProjectilesNotFromUser":               stProjectilesNotFromUser,
		"AttackInPlaceIsDefault":               stAttackInPlaceIsDefault,
		"Nova":                                 stNova,
		"InstantNoRepeatWhenHeld":              stInstantNoRepeatWhenHeld,
		"InstantShiftAttackForLeftMouse":       stInstantShiftAttackForLeftMouse,
		"AuraNotOnCaster":                      stAuraNotOnCaster,
		"Banner":                               stBanner,
		"Rain":                                 stRain,
		"Cooldown":                             stCooldown,
		"ThresholdJewelChaining":               stThresholdJewelChaining,
		"Slam":                                 stSlam,
		"Stance":                               stStance,
		"NonRepeatable":                        stNonRepeatable,
		"OtherThingUsesSkill":                  stOtherThingUsesSkill,
		"Steel":                                stSteel,
		"Hex":                                  stHex,
		"Mark":                                 stMark,
		"Aegis":                                stAegis,
		"Orb":                                  stOrb,
		"KillNoDamageModifiers":                stKillNoDamageModifiers,
		"RandomElement":                        stRandomElement,
		"LateConsumeCooldown":                  stLateConsumeCooldown,
		"Arcane":                               stArcane,
		"FixedCastTime":                        stFixedCastTime,
		"RequiresOffHandNotWeapon":             stRequiresOffHandNotWeapon,
		"Link":                                 stLink,
		"Blessing":                             stBlessing,
		"ZeroReservation":                      stZeroReservation,
		"DynamicCooldown":                      stDynamicCooldown,
		"Microtransaction":                     stMicrotransaction,
		"OwnerCannotUse":                       stOwnerCannotUse,
		"ProjectilesNumberModifiersNotApplied": stProjectilesNumberModifiersNotApplied,
		"TotemsAreBallistae":                   stTotemsAreBallistae,
		"SkillGrantedBySupport":                stSkillGrantedBySupport,
		"PreventHexTransfer":                   stPreventHexTransfer,
		"MinionsAreUndamagable":                stMinionsAreUndamagable,
		"InnateTrauma":                         stInnateTrauma,
		"DualWieldRequiresDifferentTypes":      stDualWieldRequiresDifferentTypes,
		"NoVolley":                             stNoVolley,
		"Retaliation":                          stRetaliation,
		"NeverExertable":                       stNeverExertable,
		"DisallowTriggerSupports":              stDisallowTriggerSupports,
		"ProjectileCannotReturn":               stProjectileCannotReturn,
		"Offering":                             stOffering,
		"SupportedByBane":                      stSupportedByBane,
		"WandAttack":                           stWandAttack,
		"GainsIntensity":                       stGainsIntensity,
		"CreatesSentinelMinion":                stCreatesSentinelMinion,
		"SupportedByAutoExertion":              stSupportedByAutoExertion,
		"SupportedByCrabTotem":                 stSupportedByCrabTotem,
		"SupportedBySpellTotem":                stSupportedBySpellTotem,
		"CreatesCorpse":                        stCreatesCorpse,
		"RequiresStaff":                        stRequiresStaff,
		"Pact":                                 stPact,
	}

	src := os.Getenv("POB_SRC")
	if src == "" {
		home, _ := os.UserHomeDir()
		src = filepath.Join(home, "GoProjects/poe-pob-oracle/.pob/v2.68.1/src")
	}
	raw, err := os.ReadFile(filepath.Join(src, "Data/Global.lua"))
	if err != nil {
		t.Skipf("PoB source not available: %v", err)
	}
	block := regexp.MustCompile(`(?s)\nSkillType = \{(.*?)\n\}`).FindSubmatch(raw)
	if block == nil {
		t.Fatal("SkillType table not found in Global.lua")
	}
	entry := regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*(\d+),`)
	want := map[string]int{}
	for _, m := range entry.FindAllSubmatch(block[1], -1) {
		n, _ := strconv.Atoi(string(m[2]))
		want[string(m[1])] = n
	}
	if len(want) == 0 {
		t.Fatal("no SkillType entries parsed")
	}
	for name, id := range want {
		if got, ok := skillTypeNames[name]; !ok || got != id {
			t.Errorf("SkillType.%s: Lua %d, Go %d (present %v)", name, id, got, ok)
		}
	}
	for name := range skillTypeNames {
		if _, ok := want[name]; !ok {
			t.Errorf("SkillType.%s is in Go but not in Global.lua", name)
		}
	}
}
