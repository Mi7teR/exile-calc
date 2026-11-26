package modparser

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestHookNamesExist guards against typos in hook keys: every specialModList
// port must name a function the data actually references.
func TestHookNamesExist(t *testing.T) {
	p, err := PoE1()
	if err != nil {
		t.Fatal(err)
	}
	inData := map[string]bool{}
	for _, es := range p.t.t {
		for _, e := range es {
			if m, isMap := e.value.(map[string]any); isMap {
				if h, ok := m["$hook"].(string); ok {
					inData[h] = true
				}
			}
		}
	}
	for name := range hooks {
		if strings.HasPrefix(name, "specialModList[") && !inData[name] {
			t.Errorf("hook %q is not in the data", name)
		}
	}
}

// TestEveryHookPorted: every Lua function the generated tables name has a
// Go port, so no line is unsupported for want of one.
func TestEveryHookPorted(t *testing.T) {
	p, err := PoE1()
	if err != nil {
		t.Fatal(err)
	}
	for _, es := range p.t.t {
		for _, e := range es {
			if m, isMap := e.value.(map[string]any); isMap {
				if h, ok := m["$hook"].(string); ok {
					if _, ported := hooks[h]; !ported {
						t.Errorf("hook %q has no port", h)
					}
				}
			}
		}
	}
}

func TestHooks(t *testing.T) {
	// hookCases maps a line to the mods ModParser.lua gives it (as JSON, in
	// the generic encoding), one or more per ported specialModList rule.
	hookCases := map[string]string{
		// "[ct][ar][si][tg]g?e?r?s? level (%d+) (.+) when equipped"
		"Trigger Level 20 Death Aura when Equipped": `[{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":20,"skillId":"DeathAura","triggered":true}}]`,
		// "grants (%D+)"
		"Grants Embrace Madness": `[{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":1,"skillId":"EmbraceMadness"}}]`,
		// "[ct][ar][si][tg]g?e?r?s? level (%d+) (.+) on %a+"
		"Trigger Level 1 Fire Burst on Kill": `[{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":1,"skillId":"FireBurstOnHit","triggered":true}}]`,
		// "trigger level (%d+) (.+) when you hit a rare or unique enemy and have no mark"
		"Trigger Level 10 Assassin's Mark when you Hit a Rare or Unique Enemy and have no Mark": `[{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":10,"skillId":"AssassinsMark","triggered":true}}]`,
		// "curse enemies with (%D+) on %a+"
		"Curse Enemies with Despair on Hit": `[{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":1,"noSupports":true,"skillId":"Despair","triggered":true}}]`,
		// "(.+) can trigger level (%d+) (.+)": sourceSkill option
		"Queen's Demand can Trigger Level 20 Flames of Judgement": `[{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":20,"skillId":"AtziriUniqueStaffFlameblast","source":"queen's demand","triggered":true}}]`,
		// "trigger level (%d+) (.+) on melee critical strike": onCrit option
		"Trigger Level 20 Moonfall on Melee Critical Strike": `[{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":20,"skillId":"TriggeredMoonfall","triggered":true}},
			{"@list":[{"skillId":"TriggeredMoonfall","type":"SkillId"}],"name":"ExtraSkillMod","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"mod":{"name":"SkillData","type":"LIST","flags":0,"keywordFlags":0,"value":{"key":"triggerOnCrit","value":true}}}}]`,
		// "%d+%% chance to curse n?o?n?%-?c?u?r?s?e?d? ?enemies with (%D+) on %a+": unknown skill keeps no skillId
		"10% chance to Curse non-Cursed Enemies with a random Hex on Hit": `[{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":1,"noSupports":true,"triggered":true}}]`,
		// "your (%w+) damage can (%w+)"
		"Your Chaos Damage can Chill": `[{"flags":0,"keywordFlags":0,"name":"ChaosCanChill","type":"FLAG","value":true}]`,
		// "you and your minions take (%d+)%% reduced reflected (%w+) damage"
		"You and your Minions take 100% reduced Reflected Elemental Damage": `[{"@list":[{"effectType":"Global","type":"GlobalEffect","unscalable":true}],
			"flags":0,"keywordFlags":0,"name":"ElementalReflectedDamageTaken","type":"INC","value":-100},
			{"flags":0,"keywordFlags":0,"name":"MinionModifier","type":"LIST","value":{"mod":{"@list":[{"effectType":"Global","type":"GlobalEffect","unscalable":true}],
			"flags":0,"keywordFlags":0,"name":"ElementalReflectedDamageTaken","type":"INC","value":-100}}}]`,
		// "([%a%s]+) can affect hexproof enemies"
		"Conductivity can affect Hexproof Enemies": `[{"@list":[{"skillId":"Conductivity","type":"SkillId"}],"flags":0,"keywordFlags":0,
			"name":"SkillData","type":"LIST","value":{"key":"ignoreHexproof","value":true}}]`,
		// "items and gems have (%d+)%% reduced attribute requirements"
		"Items and Gems have 10% reduced Attribute Requirements": `[{"flags":0,"keywordFlags":0,"name":"GlobalAttributeRequirements","type":"INC","value":-10}]`,
		// "every (%d+) seconds, regenerate (%d+)%% of life over one second"
		"Every 4 seconds, Regenerate 10% of Life over one second": `[{"@list":[{"type":"Condition","var":"LifeRegenBurstFull"}],"flags":0,"keywordFlags":0,
			"name":"LifeRegenPercent","type":"BASE","value":10},{"@list":[{"type":"Condition","var":"LifeRegenBurstAvg"}],"flags":0,"keywordFlags":0,
			"name":"LifeRegenPercent","type":"BASE","value":2.5}]`,
		// "socketed skill gems get a (%d+)%% cost & reservation multiplier"
		"Socketed Skill Gems get a 80% Cost & Reservation Multiplier": `[{"@list":[{"slotName":"{SlotName}","type":"SocketedIn"}],"flags":0,"keywordFlags":0,
			"name":"ExtraSkillMod","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"SupportManaMultiplier","type":"MORE","value":-20}}}]`,
		// "summoned arbalists have (%d+)%% chance to inflict (.+) exposure on hit"
		"Summoned Arbalists have 15% chance to inflict Cold Exposure on Hit": `[{"@list":[{"skillName":"Summon Arbalists","type":"SkillName"}],
			"flags":0,"keywordFlags":0,"name":"ColdExposureChance","type":"BASE","value":15}]`,
		// "(%d+)%% chance to [c?t?][a?r?][s?i?][t?g?]g?e?r? socketed spells when you spend at least (%d+) mana on an upfront cost ..."
		"0% chance to Trigger Socketed Spells when you Spend at least 100 Mana on an Upfront Cost to Use or Trigger a Skill, with a 0.1 second Cooldown": `[
			{"flags":0,"keywordFlags":0,"name":"KitavaTriggerChance","source":"Kitava's Thirst","type":"BASE","value":0},
			{"flags":0,"keywordFlags":0,"name":"KitavaRequiredManaCost","source":"Kitava's Thirst","type":"BASE","value":100},
			{"@list":[{"slotName":"{SlotName}","type":"SocketedIn"}],"flags":0,"keywordFlags":0,"name":"ExtraSupport","type":"LIST",
			"value":{"level":1,"skillId":"SupportCastOnManaSpent"}}]`,
		// "(%d+)%% chance to gain (%d+)%% of non%-chaos damage with hits as extra chaos damage"
		"10% chance to gain 25% of Non-Chaos Damage with Hits as Extra Chaos Damage": `[{"flags":0,"keywordFlags":0,"name":"NonChaosDamageGainAsChaos","type":"BASE","value":2.5}]`,
		// "([%+%-]%d+) to level of all (%a+) skill gems if (%d+) (%a+) items are equipped"
		"+1 to Level of all Chaos Skill Gems if 6 Hunter Items are Equipped": `[{"@list":[{"threshold":6,"type":"MultiplierThreshold","var":"HunterItem"}],
			"flags":0,"keywordFlags":0,"name":"GemProperty","type":"LIST","value":{"key":"level","keyOfScaledMod":"value","keyword":"chaos","value":1}}]`,
		// "nearby corpses explode when you warcry, dealing (%d+)%% of their life as (.+) damage"
		"Nearby corpses Explode when you Warcry, dealing 10% of their Life as Physical Damage": `[{"flags":0,"keywordFlags":0,"name":"ExplodeMod","type":"LIST",
			"value":{"amount":10,"chance":1,"keyOfScaledMod":"chance","type":"Physical"}},{"flags":0,"keywordFlags":0,"name":"CanExplode","type":"FLAG","value":true}]`,
		// "curse enemies with level (%d+) (%D+) on %a+, which can apply to hexproof enemies":
		// triggerExtraSkill(skill, num, {noSupports = true, ignoreHexproof = true})
		"Curse Enemies with Level 10 Despair on Hit, which can apply to Hexproof Enemies": `[
			{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":10,"noSupports":true,"skillId":"Despair","triggered":true}},
			{"@list":[{"skillId":"Despair","type":"SkillId"}],"name":"SkillData","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"key":"ignoreHexproof","value":true}}]`,
		// "trigger level (%d+) (.+), (.+) or (.+) every (%d+) seconds": three ExtraSkill mods, level = num
		"Trigger Level 20 Frostbite, Flammability or Conductivity every 5 seconds": `[
			{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,"value":{"level":20,"skillId":"Frostbite","triggered":true}},
			{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,"value":{"level":20,"skillId":"Flammability","triggered":true}},
			{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,"value":{"level":20,"skillId":"Conductivity","triggered":true}}]`,
		// "curse enemies with (%D+) on %a+, with (%d+)%% increased effect": skillName is
		// string.gsub(" "..skill, "%W%l", string.upper):sub(2)
		"Curse Enemies with Elemental Weakness on Hit, with 20% increased Effect": `[
			{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":1,"noSupports":true,"skillId":"ElementalWeakness","triggered":true}},
			{"@list":[{"skillName":"Elemental Weakness","type":"SkillName"}],"name":"CurseEffect","type":"INC","flags":0,"keywordFlags":0,"value":20}]`,
		// "curse enemies with (%D+) on %a+, with (%d+)%% reduced effect"
		"Curse Enemies with Temporal Chains on Hit, with 10% reduced Effect": `[
			{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":1,"noSupports":true,"skillId":"TemporalChains","triggered":true}},
			{"@list":[{"skillName":"Temporal Chains","type":"SkillName"}],"name":"CurseEffect","type":"INC","flags":0,"keywordFlags":0,"value":-10}]`,
		// "%d+%% chance to curse n?o?n?%-?c?u?r?s?e?d? ?enemies with (%D+) on %a+, with (%d+)%% increased effect"
		"20% chance to Curse Non-Cursed Enemies with Enfeeble on Hit, with 25% increased Effect": `[
			{"name":"ExtraSkill","type":"LIST","flags":0,"keywordFlags":0,
			"value":{"level":1,"noSupports":true,"skillId":"Enfeeble","triggered":true}},
			{"@list":[{"skillName":"Enfeeble","type":"SkillName"}],"name":"CurseEffect","type":"INC","flags":0,"keywordFlags":0,"value":25}]`,
		// "your (%w+) damage cannot (%w+)"
		"Your Lightning Damage cannot Shock": `[{"flags":0,"keywordFlags":0,"name":"LightningCannotShock","type":"FLAG","value":true}]`,

		// hooks_flasks.go
		// "flasks gain (%d+) charges? every (%d+) seconds" and its utility/life/mana forms: num / div
		"Flasks gain 2 Charges every 3 seconds": `[{"flags":0,"keywordFlags":0,"name":"FlaskChargesGenerated","type":"BASE","value":0.66666666666667}]`,
		"While on Low Life, Life Flasks gain 6 Charges every 3 seconds": `[{"@list":[{"type":"Condition","var":"LowLife"}],
			"flags":0,"keywordFlags":0,"name":"LifeFlaskChargesGenerated","type":"BASE","value":2}]`,
		// "... if you haven't used a life flask recently": UsingLifeFlask negated (Soul of Ryslatha)
		"Life Flasks gain 3 Charges every 3 seconds if you haven't used a Life Flask Recently": `[{"@list":[{"neg":true,
			"type":"Condition","var":"UsingLifeFlask"}],"flags":0,"keywordFlags":0,"name":"LifeFlaskChargesGenerated","type":"BASE","value":1}]`,
		// "(%d+)%% less flask charges gained from kills": source "from Kills"
		"80% less Flask Charges gained from Kills": `[{"flags":0,"keywordFlags":0,"name":"FlaskChargesGained","source":"from Kills","type":"MORE","value":-80}]`,
		// "tinctures have (%d+)%% increased effect while at or above (%d+) stacks of mana burn": varList
		"Tinctures have 40% increased effect while at or above 10 stacks of Mana Burn": `[{"@list":[{"threshold":10,"type":"MultiplierThreshold",
			"varList":["ManaBurnStacks","WeepingWoundsStacks"]}],"flags":0,"keywordFlags":0,"name":"TinctureEffect","type":"INC","value":40}]`,
		// "tinctures applied to you have (%d+)%% less mana burn rate": player ActorCondition
		"Tinctures applied to you have 50% less Mana Burn rate": `[{"@list":[{"actor":"player","type":"ActorCondition"}],
			"flags":0,"keywordFlags":0,"name":"TinctureManaBurnRate","type":"MORE","value":-50}]`,
		// "on non%-channelling attack, set a life flask ...": percent = 100 - chargesConsumedPercent
		"On non-channelling Attack, set a Life Flask with greater than 50% of maximum Charges remaining to 50% For each Charge removed this way, that Attack gains +2% to Damage over time Multiplier": `[
			{"@list":[{"floor":true,"percent":50,"stat":"LifeFlaskCharges","type":"PercentStat"},{"neg":true,"skillType":48,"type":"SkillType"},
			{"skillType":1,"type":"SkillType"}],"flags":0,"keywordFlags":0,"name":"DotMultiplier","type":"BASE","value":2}]`,
		// "grants level (%d+) (.+) curse aura during f?l?a?s?k? ?effect"
		"Grants Level 21 Despair Curse Aura during Effect": `[{"@list":[{"type":"Condition","var":"UsingFlask"}],"flags":0,"keywordFlags":0,
			"name":"ExtraCurse","type":"LIST","value":{"level":21,"skillId":"Despair"}}]`,

		// hooks_setitems.go
		// "... skill gems if a?t? ?l?e?a?s?t? ?(%d+) (%a+) (%a+) items are equipped": two-word influence
		"+2 to Level of all Cold Skill Gems if at least 4 Foulborn Unique Items are Equipped": `[{"@list":[{"threshold":4,"type":"MultiplierThreshold",
			"var":"FoulbornUniqueItem"}],"flags":0,"keywordFlags":0,"name":"GemProperty","type":"LIST",
			"value":{"key":"level","keyOfScaledMod":"value","keywordList":["cold","grants_active_skill"],"value":2}}]`,
		// "... support gems if (%d+) (%a+) items are equipped"
		"+1 to Level of all non-Exceptional Support Gems if 6 Shaper Items are Equipped": `[{"@list":[{"threshold":6,"type":"MultiplierThreshold",
			"var":"ShaperItem"}],"flags":0,"keywordFlags":0,"name":"GemProperty","type":"LIST",
			"value":{"key":"level","keyOfScaledMod":"value","keyword":"non-exceptional","value":1}}]`,
		// "cannot be frozen if (%d+) (%a+) items are equipped" and the other flag forms
		"Cannot be Frozen if 6 Redeemer Items are Equipped": `[{"@list":[{"threshold":6,"type":"MultiplierThreshold","var":"RedeemerItem"}],
			"flags":0,"keywordFlags":0,"name":"FreezeImmune","type":"FLAG","value":true}]`,
		// "projectiles pierce (%d+) additional targets if ...": the value stays a string, as in PoB
		"Projectiles Pierce 2 additional Targets if 2 Hunter Items are Equipped": `[{"@list":[{"threshold":2,"type":"MultiplierThreshold","var":"HunterItem"}],
			"flags":0,"keywordFlags":0,"name":"PierceCount","type":"BASE","value":"2"}]`,
		// "unaffected by (.+) if (%d+) (%a+) items are equipped"
		"Unaffected by Chill if 2 Redeemer Items are Equipped": `[{"@list":[{"threshold":2,"type":"MultiplierThreshold","var":"RedeemerItem"}],
			"flags":0,"keywordFlags":0,"name":"SelfChillEffect","type":"MORE","value":-100}]`,
		// "every (%d+) seconds, regenerate (%d+)%% of life over one second if ..."
		"Every 4 seconds, Regenerate 35% of Life over one second if 2 Hunter Items are Equipped": `[
			{"@list":[{"type":"Condition","var":"LifeRegenBurstFull"},{"threshold":2,"type":"MultiplierThreshold","var":"HunterItem"}],
			"flags":0,"keywordFlags":0,"name":"LifeRegenPercent","type":"BASE","value":35},
			{"@list":[{"type":"Condition","var":"LifeRegenBurstAvg"},{"threshold":2,"type":"MultiplierThreshold","var":"HunterItem"}],
			"flags":0,"keywordFlags":0,"name":"LifeRegenPercent","type":"BASE","value":8.75}]`,
		// "your hits ignore enemy monster (%a+) resistances? if all equipped rings are (%a+) rings"
		"Your Hits ignore Enemy Monster Cold Resistances if all Equipped Rings are Cryonic Rings": `[{"@list":[{"threshold":2,
			"type":"MultiplierThreshold","var":"CryonicRingEquipped"}],"flags":0,"keywordFlags":0,"name":"IgnoreColdResistance","type":"FLAG","value":true}]`,

		// hooks_eyejewels.go
		// "... per murderous eye jewel affecting you, up to a maximum of ...": globalLimit and hand condition
		"+10% to Off Hand Critical Strike Multiplier per Murderous Eye Jewel affecting you, up to a maximum of +100%": `[{"@list":[{"globalLimit":100,
			"globalLimitKey":"TecrodGazeOffHand","type":"Multiplier","var":"MurderousEyeJewel"},{"type":"Condition","var":"OffHandAttack"}],
			"flags":0,"keywordFlags":0,"name":"CritMultiplier","type":"BASE","value":10}]`,
		// "minions have ... per ghastly eye jewel ...": the multiplier is read from the parent
		"Minions have +6% to Damage over Time Multiplier per Ghastly Eye Jewel affecting you, up to a maximum of +30%": `[{"flags":0,"keywordFlags":0,
			"name":"MinionModifier","type":"LIST","value":{"mod":{"@list":[{"actor":"parent","globalLimit":30,"globalLimitKey":"AmanamuGaze",
			"type":"Multiplier","var":"GhastlyEyeJewel"}],"flags":0,"keywordFlags":0,"name":"DotMultiplier","type":"BASE","value":6}}}]`,
		// "while you have at least (%d+) murderous eye jewels socketed: ..."
		"While you have at least 3 Murderous Eye Jewels socketed: Intimidate you inflict applies an extra +15% increased Attack Damage taken Intimidate you inflict causes targets to deal 10% less Damage": `[
			{"@list":[{"threshold":3,"type":"MultiplierThreshold","var":"MurderousEyeJewel"}],"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST",
			"value":{"mod":{"@list":[{"type":"Condition","var":"Intimidated"}],"flags":1,"keywordFlags":0,"name":"DamageTaken","type":"INC","value":15}}},
			{"@list":[{"threshold":3,"type":"MultiplierThreshold","var":"MurderousEyeJewel"}],"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST",
			"value":{"mod":{"@list":[{"type":"Condition","var":"Intimidated"}],"flags":0,"keywordFlags":0,"name":"Damage","type":"MORE","value":-10}}}]`,

		// hooks_recovery.go
		// "regenerate (%d+) rage per second for every (%d+) life recovery per second from regeneration": percent = num/div*100
		"Regenerate 1 Rage per second for every 200 Life Recovery per second from Regeneration": `[{"@list":[{"percent":0.5,"stat":"LifeRegen","type":"PercentStat"}],
			"flags":0,"keywordFlags":0,"name":"RageRegen","type":"BASE","value":1},{"flags":0,"keywordFlags":0,"name":"Condition:CanGainRage","type":"FLAG","value":true}]`,
		// "inherent loss of rage is (%d+)%% slower"
		"Inherent loss of Rage is 10% slower": `[{"flags":0,"keywordFlags":0,"name":"InherentRageLoss","type":"INC","value":-10}]`,
		// "... prevented from hits in the past (%d+) seconds ...": the duration stays a string, as in PoB
		"1.5% of Physical Damage prevented from Hits in the past 10 seconds is Regenerated as Life per second": `[
			{"flags":0,"keywordFlags":0,"name":"PhysicalDamageMitigatedLifePseudoRecoup","type":"BASE","value":15},
			{"flags":0,"keywordFlags":0,"name":"PhysicalDamageMitigatedLifePseudoRecoupDuration","type":"BASE","value":"10"}]`,
		// "(%w+) recovery from regeneration is not applied"
		"Mana Recovery from Regeneration is not applied": `[{"flags":0,"keywordFlags":0,"name":"UnaffectedByManaRegen","type":"FLAG","value":true}]`,
		// "regenerate (%d+)%% of mana over 2 seconds when you consume a corpse"
		"Regenerate 3% of Mana over 2 seconds when you Consume a corpse": `[{"@list":[{"percent":1.5,"stat":"Mana","type":"PercentStat"},
			{"type":"Condition","var":"ConsumedCorpseInPast2Sec"}],"flags":0,"keywordFlags":0,"name":"ManaRegen","type":"BASE","value":1}]`,
		// "for each nearby corpse, you and nearby allies regenerate ...": the 3.30 "within N metres" wording
		"For each corpse within 6 metres, you and Allies within 6 metres Regenerate 5 Mana per second, up to 50 per second": `[{"@list":[{"limit":50,
			"limitTotal":true,"type":"Multiplier","var":"NearbyCorpse"}],"flags":0,"keywordFlags":0,"name":"ExtraAura","type":"LIST",
			"value":{"mod":{"flags":0,"keywordFlags":0,"name":"ManaRegen","type":"BASE","value":5}}}]`,
		// "every (%d+) seconds, regenerate energy shield equal to (%d+)%% of evasion rating over (%d+) second": percent stays a string
		"Every 4 seconds, Regenerate Energy Shield equal to 1% of Evasion Rating over 1 second": `[
			{"@list":[{"type":"Condition","var":"LifeRegenBurstFull"},{"percent":"1","stat":"Evasion","type":"PercentStat"}],
			"flags":0,"keywordFlags":0,"name":"EnergyShieldRegen","type":"BASE","value":1},
			{"@list":[{"type":"Condition","var":"LifeRegenBurstAvg"},{"percent":"1","stat":"Evasion","type":"PercentStat"}],
			"flags":0,"keywordFlags":0,"name":"EnergyShieldRegen","type":"BASE","value":0.25}]`,
		// "(%d+)%% chance to gain (%d+) life on hit with attacks"
		"15% chance to gain 200 Life on Hit with Attacks": `[{"@list":[{"type":"Condition","var":"AverageResourceGain"}],"flags":5,"keywordFlags":0,
			"name":"LifeOnHit","type":"BASE","value":30},{"@list":[{"type":"Condition","var":"MaxResourceGain"}],"flags":5,"keywordFlags":0,
			"name":"LifeOnHit","type":"BASE","value":200}]`,

		// hooks_minions.go
		// "(%d+)%% increased minion attack speed per (%d+) dexterity" and the movement speed and damage forms
		"2% increased Minion Attack Speed per 50 Dexterity": `[{"@list":[{"div":50,"stat":"Dex","type":"PerStat"}],"flags":0,"keywordFlags":0,
			"name":"MinionModifier","type":"LIST","value":{"mod":{"flags":1,"keywordFlags":0,"name":"Speed","type":"INC","value":2}}}]`,
		// "minions deal no non%-(%a+) damage": dealNoNonDamageType(dmgType, true)
		"Minions deal no Non-Cold Damage": `[
			{"flags":0,"keywordFlags":0,"name":"MinionModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"DealNoPhysical","type":"FLAG","value":true}}},
			{"flags":0,"keywordFlags":0,"name":"MinionModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"DealNoLightning","type":"FLAG","value":true}}},
			{"flags":0,"keywordFlags":0,"name":"MinionModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"DealNoFire","type":"FLAG","value":true}}},
			{"flags":0,"keywordFlags":0,"name":"MinionModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"DealNoChaos","type":"FLAG","value":true}}}]`,
		// "raised spectres fire (%d+) additional projectiles"
		"Raised Spectres fire 2 additional Projectiles": `[{"@list":[{"includeTransfigured":true,"skillName":"Raise Spectre","type":"SkillName"}],
			"flags":0,"keywordFlags":0,"name":"MinionModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"ProjectileCount","type":"BASE","value":2}}}]`,
		// "summoned arbalists convert (%d+)%% of (.+) damage to (.+) damage"
		"Summoned Arbalists Convert 100% of Physical Damage to Cold Damage": `[{"@list":[{"skillName":"Summon Arbalists","type":"SkillName"}],
			"flags":0,"keywordFlags":0,"name":"MinionModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"PhysicalDamageConvertToCold","type":"BASE","value":100}}}]`,
		// "your linked minions take (%d+)%% less damage"
		"Your Linked Minions take 70% less Damage": `[{"flags":0,"keywordFlags":0,"name":"MinionModifier","type":"LIST","value":{"mod":{"@list":[{"type":"Condition",
			"var":"AffectedByLink"}],"flags":0,"keywordFlags":0,"name":"DamageTaken","type":"MORE","value":-70}}}]`,
		// "nearby allies have (%d+)%% increased cast speed per (%d+) intelligence you have" and the other ally auras
		"Allies within 6 metres have 3% increased Cast Speed per 100 Intelligence you have": `[{"@list":[{"div":100,"stat":"Int","type":"PerStat"}],
			"flags":0,"keywordFlags":0,"name":"ExtraAura","type":"LIST","value":{"mod":{"flags":16,"keywordFlags":0,"name":"Speed","type":"INC","value":3},"onlyAllies":true}}]`,
		// "totems gain %+(%d+)%% to (%w+) resistance"
		"Totems gain +25% to Chaos Resistance": `[{"flags":0,"keywordFlags":0,"name":"TotemChaosResist","type":"BASE","value":25}]`,

		// hooks_curses.go
		// "your curses have (%d+)%% increased effect if (%d+)%% of curse duration expired"
		"Your Curses have 20% increased Effect if 50% of Curse Duration expired": `[{"@list":[{"actor":"enemy","threshold":50,"type":"MultiplierThreshold",
			"var":"CurseExpired"},{"skillType":97,"type":"SkillType"}],"flags":0,"keywordFlags":0,"name":"CurseEffect","type":"INC","value":20}]`,
		// "non%-aura hexes expire upon reaching (%d+)%% of base effect ..."
		"Non-Aura Hexes expire upon reaching 200% of base Effect Non-Aura Hexes gain 20% increased Effect per second": `[{"@list":[{"actor":"enemy","limit":200,
			"limitTotal":true,"type":"Multiplier","var":"CurseDurationExpired"},{"neg":true,"skillType":39,"type":"SkillType"},{"skillType":97,"type":"SkillType"}],
			"flags":0,"keywordFlags":0,"name":"CurseEffect","type":"INC","value":20}]`,
		// "enemies cursed by you are hindered if (%d+)%% of curse duration expired" and the malediction form
		"Enemies Cursed by you are Hindered if 25% of Curse Duration expired": `[{"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST",
			"value":{"mod":{"@list":[{"threshold":25,"type":"MultiplierThreshold","var":"CurseExpired"},{"type":"ActorCondition","var":"Cursed"}],
			"flags":0,"keywordFlags":0,"name":"Condition:Hindered","type":"FLAG","value":true}}}]`,
		// "offering skills have (%d+)%% reduced duration"
		"Offering Skills have 50% reduced Duration": `[{"@list":[{"skillNameList":["Bone Offering","Flesh Offering","Spirit Offering","Blood Offering"],
			"type":"SkillName"}],"flags":0,"keywordFlags":0,"name":"Duration","type":"INC","value":-50}]`,

		// hooks_defence.go
		// "(%d+)%% chance to defend with (%d+)%% of armour for each time you've been hit by an enemy recently, up to (%d+)%%"
		"5% chance to Defend with 200% of Armour for each time you've been Hit by an Enemy Recently, up to 30%": `[
			{"@list":[{"type":"Condition","var":"ArmourMax"}],"flags":0,"keywordFlags":0,"name":"ArmourDefense","source":"Armour Mastery: Max Calc","type":"MAX","value":100},
			{"@list":[{"type":"Condition","var":"ArmourAvg"},{"limit":6,"type":"Multiplier","var":"BeenHitRecently"}],"flags":0,"keywordFlags":0,
			"name":"ArmourDefense","source":"Armour Mastery: Average Calc","type":"MAX","value":5},
			{"@list":[{"neg":true,"type":"Condition","var":"ArmourMax"},{"neg":true,"type":"Condition","var":"ArmourAvg"},{"limit":6,"type":"Multiplier","var":"BeenHitRecently"}],
			"flags":0,"keywordFlags":0,"name":"ArmourDefense","source":"Armour Mastery: Min Calc","type":"MAX","value":0}]`,
		// "(%d+)%% increased armour per second you've been stationary, up to a maximum of (%d+)%%": limit = limit / num
		"20% increased Armour per second you've been stationary, up to a maximum of 100%": `[{"@list":[{"limit":5,"type":"Multiplier","var":"StationarySeconds"},
			{"type":"Condition","var":"Stationary"}],"flags":0,"keywordFlags":0,"name":"Armour","type":"INC","value":20}]`,
		// "warcries grant arcane surge to you and allies, with ..."
		"Warcries grant Arcane Surge to you and Allies, with 10% increased effect per 5 power, up to 50%": `[
			{"@list":[{"type":"Condition","var":"UsedWarcryRecently"}],"flags":0,"keywordFlags":0,"name":"ExtraAura","type":"LIST",
			"value":{"mod":{"flags":0,"keywordFlags":0,"name":"Condition:ArcaneSurge","type":"FLAG","value":true}}},
			{"@list":[{"div":5,"globalLimit":50,"globalLimitKey":"Brinerot Flag","stat":"WarcryPower","type":"PerStat"},{"type":"Condition","var":"UsedWarcryRecently"}],
			"flags":0,"keywordFlags":0,"name":"ArcaneSurgeEffect","type":"INC","value":10}]`,
		// "(%d+)%% increased global maximum energy shield and reduced lightning resistance" and the life and mana forms
		"35% increased Global maximum Energy Shield and reduced Lightning Resistance": `[{"@list":[{"type":"Global"}],"flags":0,"keywordFlags":0,
			"name":"EnergyShield","type":"INC","value":35},{"flags":0,"keywordFlags":0,"name":"LightningResist","type":"INC","value":-35}]`,
		// "non%-damaging ailments have (%d+)%% reduced effect on you while you have arcane surge": data.nonDamagingAilmentTypeList
		"Non-Damaging Ailments have 25% reduced Effect on you while you have Arcane Surge": `[
			{"@list":[{"type":"Condition","var":"AffectedByArcaneSurge"}],"flags":0,"keywordFlags":0,"name":"SelfChillEffect","type":"INC","value":-25},
			{"@list":[{"type":"Condition","var":"AffectedByArcaneSurge"}],"flags":0,"keywordFlags":0,"name":"SelfFreezeEffect","type":"INC","value":-25},
			{"@list":[{"type":"Condition","var":"AffectedByArcaneSurge"}],"flags":0,"keywordFlags":0,"name":"SelfShockEffect","type":"INC","value":-25},
			{"@list":[{"type":"Condition","var":"AffectedByArcaneSurge"}],"flags":0,"keywordFlags":0,"name":"SelfScorchEffect","type":"INC","value":-25},
			{"@list":[{"type":"Condition","var":"AffectedByArcaneSurge"}],"flags":0,"keywordFlags":0,"name":"SelfBrittleEffect","type":"INC","value":-25},
			{"@list":[{"type":"Condition","var":"AffectedByArcaneSurge"}],"flags":0,"keywordFlags":0,"name":"SelfSapEffect","type":"INC","value":-25}]`,
		// "gain no (.+) from equipped (.+)": the slot keeps its space, as in PoB
		"Gain no Armour from Equipped Body Armour": `[{"flags":0,"keywordFlags":0,"name":"GainNoArmourFromBody Armour","type":"FLAG","value":true}]`,
		// "nearby enemy monsters' action speed is at most (%d+)%% of base value"
		"Nearby Enemy Monsters' Action Speed is at most 90% of base value": `[{"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST",
			"value":{"mod":{"flags":0,"keywordFlags":0,"name":"MaximumActionSpeedReduction","type":"MAX","value":10}}}]`,
		// "debuffs on you expire (%d+)%% slower" and the other negated single mods
		"Poisons on you expire 50% slower": `[{"flags":0,"keywordFlags":0,"name":"SelfPoisonDebuffExpirationRate","type":"BASE","value":-50}]`,

		// hooks_offence.go
		// "(%d+)%% chance for bleeding inflicted with this weapon to deal (%d+)%% more damage"
		"15% chance for Bleeding inflicted with this Weapon to deal 100% more Damage": `[{"@list":[{"type":"Condition","var":"{Hand}Attack"},
			{"skillType":1,"type":"SkillType"}],"flags":0,"keywordFlags":4194304,"name":"Damage","type":"MORE","value":15}]`,
		// "(%d+)%% chance to throw up to (%d+) additional traps? or mines?"
		"10% chance to throw up to 1 additional Trap or Mine": `[{"flags":0,"keywordFlags":0,"name":"TrapThrowCount","type":"BASE","value":0.1},
			{"flags":0,"keywordFlags":0,"name":"MineThrowCount","type":"BASE","value":0.1}]`,
		// "traps and mines deal (%d+) to (%d+) additional physical damage"
		"Traps and Mines deal 4 to 13 additional Physical Damage": `[{"flags":0,"keywordFlags":12288,"name":"PhysicalMin","type":"BASE","value":4},
			{"flags":0,"keywordFlags":12288,"name":"PhysicalMax","type":"BASE","value":13}]`,
		// "hits with prismatic skills always ?i?n?f?l?i?c?t? (%w+)"
		"Hits with Prismatic Skills always inflict Brittle": `[{"@list":[{"skillType":102,"type":"SkillType"}],"flags":4,"keywordFlags":0,
			"name":"EnemyBrittleChance","type":"BASE","value":100}]`,
		// "inflict fire exposure on hit against enemies with (%d+) cinderflame, ..."
		"Inflict Fire Exposure on Hit against Enemies with 5 Cinderflame, applying -25% to Fire Resistance": `[
			{"@list":[{"type":"Condition","var":"Effective"},{"threshold":5,"type":"MultiplierThreshold","var":"CinderflameStacks"}],
			"flags":0,"keywordFlags":0,"name":"FireExposureChance","type":"BASE","value":100},
			{"@list":[{"type":"Condition","var":"Effective"},{"threshold":5,"type":"MultiplierThreshold","var":"CinderflameStacks"}],
			"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"FireExposure","type":"BASE","value":-25}}}]`,
		// "gain (%d+)%% of (%a+) damage as extra (%a+) damage for each of your hallowing flames ...": limit = limit / num
		"Gain 10% of Physical Damage as Extra Lightning Damage for each of your Hallowing Flames that have been removed by an allied hit recently, up to 80%": `[
			{"@list":[{"actor":"enemy","type":"ActorCondition","var":"HallowingFlame"},{"limit":8,"type":"Multiplier","var":"HallowingFlameStacksRemovedByAlly"}],
			"flags":0,"keywordFlags":0,"name":"PhysicalDamageGainAsLightning","type":"BASE","value":10}]`,
		// "enemies taunted by your warcries explode on death, ...": explodeFunc
		"Enemies Taunted by your Warcries Explode on death, dealing 8% of their maximum Life as Chaos Damage": `[
			{"@list":[{"actor":"enemy","type":"ActorCondition","var":"Taunted"},{"type":"Condition","var":"UsedWarcryRecently"}],"flags":0,"keywordFlags":0,
			"name":"ExplodeMod","type":"LIST","value":{"amount":8,"chance":1,"keyOfScaledMod":"chance","type":"Chaos"}},
			{"flags":0,"keywordFlags":0,"name":"CanExplode","type":"FLAG","value":true}]`,
		// "travel skills other than ([%a%s]+) are disabled"
		"Travel Skills other than Dash are Disabled": `[{"@list":[{"skillType":79,"type":"SkillType"}],"flags":0,"keywordFlags":0,"name":"DisableSkill","type":"FLAG","value":true},
			{"@list":[{"skillId":"Dash","type":"SkillId"}],"flags":0,"keywordFlags":0,"name":"EnableSkill","type":"FLAG","value":true}]`,
		// "(%d+)%% chance to ... socketed spells when you spend at least (%d+) life on an upfront cost ..."
		"50% chance to Trigger Socketed Spells when you Spend at least 0 Life on an Upfront Cost to Use or Trigger a Skill, with a 0.1 second Cooldown": `[
			{"flags":0,"keywordFlags":0,"name":"FoulbornKitavaTriggerChance","source":"Kitava's Thirst","type":"BASE","value":50},
			{"flags":0,"keywordFlags":0,"name":"FoulbornKitavaRequiredLifeCost","source":"Kitava's Thirst","type":"BASE","value":0},
			{"@list":[{"slotName":"{SlotName}","type":"SocketedIn"}],"flags":0,"keywordFlags":0,"name":"ExtraSupport","type":"LIST",
			"value":{"level":1,"skillId":"SupportCastOnLifeSpent"}}]`,
		// "base (%a+) duration is ([%d%.]+) seconds?"
		"Base Ignite Duration is 1 second": `[{"flags":0,"keywordFlags":0,"name":"IgniteDurationBase","type":"OVERRIDE","value":1}]`,

		// 5226: a skill gemIdLookup does not know becomes skillId "Unknown"
		"Grants Level 5 Nonexistent Curse Aura during Effect": `[{"@list":[{"type":"Condition","var":"UsingFlask"}],"flags":0,"keywordFlags":0,
			"name":"ExtraCurse","type":"LIST","value":{"level":5,"skillId":"Unknown"}}]`,
		// 5539: gemIdLookup[name] is nil, so the SkillId tag has no skillId
		"Travel Skills other than Nonexistent are Disabled": `[{"@list":[{"skillType":79,"type":"SkillType"}],"flags":0,"keywordFlags":0,
			"name":"DisableSkill","type":"FLAG","value":true},
			{"@list":[{"type":"SkillId"}],"flags":0,"keywordFlags":0,"name":"EnableSkill","type":"FLAG","value":true}]`,

		// Round 2: the hooks that had no port. Each case is the PoB v2.68.1
		// ModParser result of the line (modLib.parseMod in the oracle); the
		// two ExtraJewelFunc ones carry their Lua function as "$jewelFunc"
		// data instead of a function.
		// modTagList for each enemy you or your minions have killed recently, up to (%d+)%% per second (ModParser.lua:1435)
		"10% increased Damage for each Enemy you or your Minions have Killed Recently, up to 50% per second": `[{"@list":[{"limit":50,"limitTotal":true,"type":"Multiplier","varList":["EnemyKilledRecently","EnemyKilledByMinionsRecently"]}],"flags":0,"keywordFlags":0,"name":"Damage","type":"INC","value":10}]`,
		// modTagList for each enemy you or your minions have killed recently, up to (%d+)%% (ModParser.lua:1434)
		"10% increased Movement Speed for each Enemy you or your Minions have Killed Recently, up to 50%": `[{"@list":[{"limit":50,"limitTotal":true,"type":"Multiplier","varList":["EnemyKilledRecently","EnemyKilledByMinionsRecently"]}],"flags":0,"keywordFlags":0,"name":"MovementSpeed","type":"INC","value":10}]`,
		// modTagList for each remaining chain, up to (%d+)%% (ModParser.lua:1570)
		"Projectiles deal 10% increased Damage for each remaining Chain, up to 40%": `[{"@list":[{"globalLimit":40,"globalLimitKey":"FollowThrough","stat":"ChainRemaining","type":"PerStat"}],"flags":1024,"keywordFlags":0,"name":"Damage","type":"INC","value":10}]`,
		// modTagList if both equipped ([%a%s]+) have a?n? ?([%a%s]+) modifiers? (ModParser.lua:1644)
		"20% increased Attack Speed if both Equipped Rings have an Elder Modifier": `[{"@list":[{"bothSlots":true,"itemSlot":"ring","searchCond":"elder","type":"ItemCondition"}],"flags":1,"keywordFlags":0,"name":"Speed","type":"INC","value":20}]`,
		// modTagList per (%d+) maximum mana, up to a maximum of (%d+)%% (ModParser.lua:1500)
		"1% increased Spell Damage per 100 Maximum Mana, up to a maximum of 50%": `[{"@list":[{"div":100,"limit":50,"limitTotal":true,"stat":"Mana","type":"PerStat"}],"flags":2,"keywordFlags":0,"name":"Damage","type":"INC","value":1}]`,
		// modTagList per (.+) eye jewel affecting you, up to a maximum of %+?(%d+)%% (ModParser.lua:1412)
		"1% increased Damage per Searching Eye Jewel affecting you, up to a maximum of +30%": `[{"@list":[{"limit":30,"limitTotal":true,"type":"Multiplier","var":"SearchingEyeJewel"}],"flags":0,"keywordFlags":0,"name":"Damage","type":"INC","value":1}]`,
		// modTagList w?h?i[lf]e? dexterity is below (%d+) (ModParser.lua:1580)
		"20% increased Attack Speed while Dexterity is below 100": `[{"@list":[{"stat":"Dex","threshold":99,"type":"StatThreshold","upper":true}],"flags":1,"keywordFlags":0,"name":"Speed","type":"INC","value":20}]`,
		// modTagList w?h?i[lf]e? intelligence is below (%d+) (ModParser.lua:1581)
		"20% increased Cast Speed while Intelligence is below 100": `[{"@list":[{"stat":"Int","threshold":99,"type":"StatThreshold","upper":true}],"flags":16,"keywordFlags":0,"name":"Speed","type":"INC","value":20}]`,
		// modTagList w?h?i[lf]e? strength is below (%d+) (ModParser.lua:1579)
		"20% increased Damage while Strength is below 100": `[{"@list":[{"stat":"Str","threshold":99,"type":"StatThreshold","upper":true}],"flags":0,"keywordFlags":0,"name":"Damage","type":"INC","value":20}]`,
		// modTagList with at least one (%w+) grafted to you (ModParser.lua:1762)
		"10% increased maximum Life with at least one Paranoia grafted to you": `[{"@list":[{"type":"Condition","var":"UsingParanoia"}],"flags":0,"keywordFlags":0,"name":"Life","type":"INC","value":10}]`,
		// preFlagList ^hits against enemies (%a+) by you have  (ModParser.lua:1281)
		"Hits against Enemies Ignited by you have 20% increased Critical Strike Chance": `[{"@list":[{"actor":"enemy","type":"ActorCondition","var":"Ignited"}],"flags":0,"keywordFlags":0,"name":"CritChance","type":"INC","value":20}]`,
		// specialModList ^%+([%d%.]+) metres? to discharge radius$ (ModParser.lua:5952)
		"+1.5 metres to Discharge radius": `[{"@list":[{"includeTransfigured":true,"skillName":"Discharge","type":"SkillName"}],"flags":0,"keywordFlags":0,"name":"AreaOfEffect","type":"BASE","value":15}]`,
		// specialModList ^%-(%d+)%% of toxic rain physical damage converted to chaos damage$ (ModParser.lua:3612)
		"-20% of Toxic Rain Physical Damage Converted to Chaos Damage": `[{"@list":[{"includeTransfigured":true,"skillName":"Toxic Rain","type":"SkillName"}],"flags":0,"keywordFlags":0,"name":"SkillPhysicalDamageConvertToChaos","type":"BASE","value":-20}]`,
		// specialModList ^(%a+) resistance cannot be penetrated$ (ModParser.lua:5159)
		"Fire Resistance cannot be Penetrated": `[{"flags":0,"keywordFlags":0,"name":"EnemyCannotPenFireResistance","type":"FLAG","value":true}]`,
		// specialModList ^(%d+)%% chance for bleeding inflicted with cobra lash or venom gyre to deal (%d+)%% more damage$ (ModParser.lua:3965)
		"25% chance for Bleeding inflicted with Cobra Lash or Venom Gyre to deal 100% more Damage": `[{"@list":[{"skillNameList":["Cobra Lash","Venom Gyre"],"type":"SkillName"}],"flags":0,"keywordFlags":4194304,"name":"Damage","type":"MORE","value":25}]`,
		// specialModList ^(%d+)%% chance for ignites inflicted with lightning arrow or ice shot to deal (%d+)%% more damage$ (ModParser.lua:3799)
		"25% chance for Ignites inflicted with Lightning Arrow or Ice Shot to deal 100% more Damage": `[{"@list":[{"includeTransfigured":true,"skillNameList":["Lightning Arrow","Ice Shot"],"type":"SkillName"}],"flags":0,"keywordFlags":8388608,"name":"Damage","type":"MORE","value":25}]`,
		// specialModList ^(%d+)%% chance for ignites inflicted with lightning strike or frost blades to deal (%d+)%% more damage$ (ModParser.lua:3796)
		"25% chance for Ignites inflicted with Lightning Strike or Frost Blades to deal 100% more Damage": `[{"@list":[{"includeTransfigured":true,"skillNameList":["Lightning Strike","Frost Blades"],"type":"SkillName"}],"flags":0,"keywordFlags":8388608,"name":"Damage","type":"MORE","value":25}]`,
		// specialModList ^(%d+)%% chance for ignites inflicted with shock nova or storm call to deal (%d+)%% more damage$ (ModParser.lua:3802)
		"25% chance for Ignites inflicted with Shock Nova or Storm Call to deal 100% more Damage": `[{"@list":[{"includeTransfigured":true,"skillNameList":["Shock Nova","Storm Call"],"type":"SkillName"}],"flags":0,"keywordFlags":8388608,"name":"Damage","type":"MORE","value":25}]`,
		// specialModList ^(%d+)%% chance to [c?t?][a?r?][s?i?][t?g?]g?e?r? socketed spells when you spend at least (%d+) mana to use a skill$ (ModParser.lua:3253)
		"20% chance to Trigger Socketed Spells when you Spend at least 100 Mana to Use a Skill": `[{"flags":0,"keywordFlags":0,"name":"KitavaTriggerChance","source":"Kitava's Thirst","type":"BASE","value":20},{"flags":0,"keywordFlags":0,"name":"KitavaRequiredManaCost","source":"Kitava's Thirst","type":"BASE","value":100},{"@list":[{"slotName":"{SlotName}","type":"SocketedIn"}],"flags":0,"keywordFlags":0,"name":"ExtraSupport","type":"LIST","value":{"level":1,"skillId":"SupportCastOnManaSpent"}}]`,
		// specialModList ^(%d+)%% chance to defend with double armour$ (ModParser.lua:2502)
		"50% chance to Defend with Double Armour": `[{"@list":[{"type":"Condition","var":"ArmourMax"}],"flags":0,"keywordFlags":0,"name":"ArmourDefense","source":"Armour Mastery: Max Calc","type":"MAX","value":100},{"@list":[{"type":"Condition","var":"ArmourAvg"}],"flags":0,"keywordFlags":0,"name":"ArmourDefense","source":"Armour Mastery: Average Calc","type":"MAX","value":50},{"@list":[{"neg":true,"type":"Condition","var":"ArmourMax"},{"neg":true,"type":"Condition","var":"ArmourAvg"}],"flags":0,"keywordFlags":0,"name":"ArmourDefense","source":"Armour Mastery: Min Calc","type":"MAX","value":0}]`,
		// specialModList ^(%d+)%% chance to defend with double your armour for each time you've been hit by an enemy recently, up to (%d+)%%$ (ModParser.lua:2491)
		"10% chance to Defend with Double your Armour for each time you've been Hit by an Enemy Recently, up to 50%": `[{"@list":[{"type":"Condition","var":"ArmourMax"}],"flags":0,"keywordFlags":0,"name":"ArmourDefense","source":"Armour Mastery: Max Calc","type":"MAX","value":100},{"@list":[{"type":"Condition","var":"ArmourAvg"},{"limit":5,"type":"Multiplier","var":"BeenHitRecently"}],"flags":0,"keywordFlags":0,"name":"ArmourDefense","source":"Armour Mastery: Average Calc","type":"MAX","value":10},{"@list":[{"neg":true,"type":"Condition","var":"ArmourMax"},{"neg":true,"type":"Condition","var":"ArmourAvg"},{"limit":5,"type":"Multiplier","var":"BeenHitRecently"}],"flags":0,"keywordFlags":0,"name":"ArmourDefense","source":"Armour Mastery: Min Calc","type":"MAX","value":0}]`,
		// specialModList ^(%d+)%% chance to throw up to (%d+) additional mines?$ (ModParser.lua:4493)
		"50% chance to throw up to 1 additional Mine": `[{"flags":0,"keywordFlags":0,"name":"MineThrowCount","type":"BASE","value":0.5}]`,
		// specialModList ^(%d+)%% less damage taken for every (%d+)%% life recovery per second from leech$ (ModParser.lua:2232)
		"5% less Damage taken for every 2% Life Recovery per second from Leech": `[{"@list":[{"div":2,"stat":"MaxLifeLeechRatePercent","type":"PerStat"}],"flags":0,"keywordFlags":0,"name":"DamageTaken","type":"MORE","value":-5}]`,
		// specialModList ^(%d+)%% more damage for each endurance charge lost recently, up to (%d+)%%$ (ModParser.lua:2650)
		"5% more Damage for each Endurance Charge lost recently, up to 25%": `[{"@list":[{"limit":25,"limitTotal":true,"type":"Multiplier","var":"EnduranceChargesLostRecently"}],"flags":0,"keywordFlags":0,"name":"Damage","type":"MORE","value":5}]`,
		// specialModList ^(%d+)%% reduced damage taken while there are at least two rare or unique enemies nearby$ (ModParser.lua:2557)
		"10% reduced Damage taken while there are at least two Rare or Unique Enemies nearby": `[{"@list":[{"threshold":2,"type":"MultiplierThreshold","var":"NearbyRareOrUniqueEnemies"}],"flags":0,"keywordFlags":0,"name":"DamageTaken","type":"INC","value":-10}]`,
		// specialModList ^(%d+)%% reduced essence drain and soulrend projectile speed$ (ModParser.lua:4686)
		"20% reduced Essence Drain and Soulrend Projectile Speed": `[{"@list":[null,{"includeTransfigured":true,"skillNameList":["Essence Drain","Soulrend"],"type":"SkillName"}],"flags":0,"keywordFlags":0,"name":"ProjectileSpeed","type":"INC","value":-20}]`,
		// specialModList ^+(%d+)%% chance to suppress spell damage if your e?q?u?i?p?p?e?d? ?boots, helmet and gloves have evasion$ (ModParser.lua:4097)
		"+10% chance to Suppress Spell Damage if your Boots, Helmet and Gloves have Evasion": `[{"@list":[{"stat":"EvasionOnBoots","threshold":1,"type":"StatThreshold"},{"stat":"EvasionOnHelmet","threshold":1,"type":"StatThreshold"},{"stat":"EvasionOnGloves","threshold":1,"type":"StatThreshold"}],"flags":0,"keywordFlags":0,"name":"SpellSuppressionChance","type":"BASE","value":10}]`,
		// specialModList ^arcane cloak grants life regeneration equal to (%d+)%% of mana spent per second$ (ModParser.lua:5920)
		"Arcane Cloak grants Life Regeneration equal to 15% of Mana Spent per Second": `[{"@list":[{"skillName":"Arcane Cloak","type":"SkillName"}],"flags":0,"keywordFlags":0,"name":"ExtraSkillMod","type":"LIST","value":{"mod":{"@list":[{"type":"Multiplier","var":"ArcaneCloakConsumedMana"},{"effectType":"Buff","type":"GlobalEffect"}],"flags":0,"keywordFlags":0,"name":"LifeRegen","type":"BASE","value":0.14999999999999999}}}]`,
		// specialModList ^aura buffs from skills have (%d+)%% increased effect on you for each herald affecting you, up to (%d+)%%$ (ModParser.lua:4137)
		"Aura Buffs from Skills have 4% increased Effect on you for each Herald affecting you, up to 40%": `[{"@list":[{"globalLimit":40,"globalLimitKey":"PurposefulHarbinger","type":"Multiplier","var":"Herald"}],"flags":0,"keywordFlags":0,"name":"SkillAuraEffectOnSelf","type":"INC","value":4}]`,
		// specialModList ^auras from your skills have (%d+)%% increased effect on you for each herald affecting you, up to (%d+)%%$ (ModParser.lua:4140)
		"Auras from your Skills have 4% increased Effect on you for each Herald affecting you, up to 40%": `[{"@list":[{"globalLimit":40,"globalLimitKey":"PurposefulHarbinger","type":"Multiplier","var":"Herald"}],"flags":0,"keywordFlags":0,"name":"SkillAuraEffectOnSelf","type":"INC","value":4}]`,
		// specialModList ^chain hook has %+([%d%.]+) metres? to radius per (%d+) rage$ (ModParser.lua:5951)
		"Chain Hook has +0.2 metres to radius per 5 Rage": `[{"@list":[{"div":5,"stat":"Rage","type":"PerStat"},{"includeTransfigured":true,"skillName":"Chain Hook","type":"SkillName"}],"flags":0,"keywordFlags":0,"name":"AreaOfEffect","type":"BASE","value":2}]`,
		// specialModList ^cobra lash and venom gyre have %-(%d+)%% of physical damage converted to chaos damage$ (ModParser.lua:3615)
		"Cobra Lash and Venom Gyre have -20% of Physical Damage Converted to Chaos Damage": `[{"@list":[{"skillNameList":["Cobra Lash","Venom Gyre"],"type":"SkillName"}],"flags":0,"keywordFlags":0,"name":"SkillPhysicalDamageConvertToChaos","type":"BASE","value":-20}]`,
		// specialModList ^corpses you spawn have (%d+)%% reduced maximum life$ (ModParser.lua:2996)
		"Corpses you Spawn have 20% reduced Maximum Life": `[{"flags":0,"keywordFlags":0,"name":"CorpseLife","type":"INC","value":-20}]`,
		// specialModList ^damage taken recouped as (%a+) is also recouped as energy shield$ (ModParser.lua:4884)
		"Damage taken Recouped as Life is also Recouped as Energy Shield": `[{"flags":0,"keywordFlags":0,"name":"AddLifeRecoupToEnergyShieldRecoup","type":"FLAG","value":true}]`,
		// specialModList ^defend with (%d+)%% of armour while not on low energy shield$ (ModParser.lua:2521)
		"Defend with 120% of Armour while not on Low Energy Shield": `[{"@list":[{"neg":true,"type":"Condition","var":"LowEnergyShield"}],"flags":0,"keywordFlags":0,"name":"ArmourDefense","source":"Armour and Energy Shield Mastery","type":"MAX","value":20}]`,
		// specialModList ^enemies have %-(%d+)%% to total physical damage reduction against your hits$ (ModParser.lua:5683)
		"Enemies have -10% to Total Physical Damage Reduction against your Hits": `[{"flags":0,"keywordFlags":0,"name":"EnemyPhysicalDamageReduction","type":"BASE","value":-10}]`,
		// specialModList ^enemies you hit with elemental damage temporarily get (%+%d+)%% resistance to those elements and (%-%d+)%% resistance to other elements$ (ModParser.lua:2338)
		"Enemies you hit with Elemental Damage temporarily get +25% Resistance to those Elements and -50% Resistance to other Elements": `[{"flags":0,"keywordFlags":0,"name":"ElementalEquilibrium","type":"FLAG","value":true},{"flags":0,"keywordFlags":0,"name":"ElementalEquilibriumLegacy","type":"FLAG","value":true},{"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"@list":[{"type":"Condition","var":"HitByFireDamage"}],"flags":0,"keywordFlags":0,"name":"FireResist","type":"BASE","value":25}}},{"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"@list":[{"neg":true,"type":"Condition","var":"HitByFireDamage"},{"type":"Condition","varList":["HitByColdDamage","HitByLightningDamage"]}],"flags":0,"keywordFlags":0,"name":"FireResist","type":"BASE","value":-50}}},{"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"@list":[{"type":"Condition","var":"HitByColdDamage"}],"flags":0,"keywordFlags":0,"name":"ColdResist","type":"BASE","value":25}}},{"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"@list":[{"neg":true,"type":"Condition","var":"HitByColdDamage"},{"type":"Condition","varList":["HitByFireDamage","HitByLightningDamage"]}],"flags":0,"keywordFlags":0,"name":"ColdResist","type":"BASE","value":-50}}},{"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"@list":[{"type":"Condition","var":"HitByLightningDamage"}],"flags":0,"keywordFlags":0,"name":"LightningResist","type":"BASE","value":25}}},{"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"@list":[{"neg":true,"type":"Condition","var":"HitByLightningDamage"},{"type":"Condition","varList":["HitByFireDamage","HitByColdDamage"]}],"flags":0,"keywordFlags":0,"name":"LightningResist","type":"BASE","value":-50}}}]`,
		// specialModList ^enemies you impale have %-(%d+)%% to total physical damage reduction against impale hits$ (ModParser.lua:5686)
		"Enemies you Impale have -10% to Total Physical Damage Reduction against Impale Hits": `[{"flags":0,"keywordFlags":0,"name":"EnemyImpalePhysicalDamageReduction","type":"BASE","value":-10}]`,
		// specialModList ^flamethrower, seismic and lightning spire trap have %-(%d+) cooldown uses?$ (ModParser.lua:5837)
		"Flamethrower, Seismic and Lightning Spire Trap have -1 Cooldown Uses": `[{"@list":[{"includeTransfigured":true,"skillNameList":["Flamethrower Trap","Seismic Trap","Lightning Spire Trap"],"type":"SkillName"}],"flags":0,"keywordFlags":0,"name":"AdditionalCooldownUses","type":"BASE","value":-1}]`,
		// specialModList ^focus has (%d+)%% reduced cooldown recovery rate$ (ModParser.lua:5834)
		"Focus has 25% reduced Cooldown Recovery Rate": `[{"@list":[{"type":"Condition","var":"Focused"}],"flags":0,"keywordFlags":0,"name":"FocusCooldownRecovery","type":"INC","value":-25}]`,
		// specialModList ^for each element you've been hit by damage of recently, (%d+)%% reduced damage taken of that element$ (ModParser.lua:2709)
		"For each Element you've been Hit by Damage of Recently, 8% reduced Damage taken of that Element": `[{"@list":[{"type":"Condition","var":"HitByFireDamageRecently"}],"flags":0,"keywordFlags":0,"name":"FireDamageTaken","type":"INC","value":-8},{"@list":[{"type":"Condition","var":"HitByColdDamageRecently"}],"flags":0,"keywordFlags":0,"name":"ColdDamageTaken","type":"INC","value":-8},{"@list":[{"type":"Condition","var":"HitByLightningDamageRecently"}],"flags":0,"keywordFlags":0,"name":"LightningDamageTaken","type":"INC","value":-8}]`,
		// specialModList ^frozen sweep deals (%d+)%% less damage$ (ModParser.lua:5879)
		"Frozen Sweep deals 40% less Damage": `[{"@list":[{"includeTransfigured":true,"skillName":"Frozen Sweep","type":"SkillName"}],"flags":0,"keywordFlags":0,"name":"Damage","type":"MORE","value":-40}]`,
		// specialModList ^grants armour equal to (%d+)%% of your reserved life to you and nearby allies$ (ModParser.lua:2917)
		"Grants Armour equal to 160% of your Reserved Life to you and nearby Allies": `[{"flags":0,"keywordFlags":0,"name":"GrantReservedLifeAsAura","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"Armour","type":"BASE","value":1.6000000000000001}}}]`,
		// specialModList ^hits overwhelm (%d+)%% of physical damage reduction while you have sacrificial zeal$ (ModParser.lua:5818)
		"Hits Overwhelm 15% of Physical Damage Reduction while you have Sacrificial Zeal": `[{"@list":[null,{"type":"Condition","var":"SacrificialZeal"}],"flags":0,"keywordFlags":0,"name":"EnemyPhysicalDamageReduction","type":"BASE","value":-15}]`,
		// specialModList ^hits with this weapon overwhelm (%d+)%% physical damage reduction$ (ModParser.lua:5689)
		"Hits with this Weapon Overwhelm 20% Physical Damage Reduction": `[{"@list":[{"type":"Condition","var":"{Hand}Attack"},{"skillType":1,"type":"SkillType"}],"flags":4,"keywordFlags":0,"name":"EnemyPhysicalDamageReduction","type":"BASE","value":-20}]`,
		// specialModList ^mana is increased by (%d+)%% of overcapped lightning resistance$ (ModParser.lua:6057)
		"Mana is increased by 50% of Overcapped Lightning Resistance": `[{"flags":0,"keywordFlags":0,"name":"ManaIncreasedByOvercappedLightningRes","type":"FLAG","value":true},{"@list":[{"stat":"LightningResistOverCap","type":"PerStat"}],"flags":0,"keywordFlags":0,"name":"Mana","type":"INC","value":0.5}]`,
		// specialModList ^minions attacks overwhelm (%d+)%% physical damage reduction$ (ModParser.lua:5827)
		"Minions Attacks Overwhelm 10% Physical Damage Reduction": `[{"flags":0,"keywordFlags":0,"name":"MinionModifier","type":"LIST","value":{"mod":{"@list":[{"skillType":1,"type":"SkillType"}],"flags":0,"keywordFlags":0,"name":"EnemyPhysicalDamageReduction","type":"BASE","value":-10}}}]`,
		// specialModList ^minions take (%d+)%% reduced damage$ (ModParser.lua:4838)
		"Minions take 20% reduced Damage": `[{"flags":0,"keywordFlags":0,"name":"MinionModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"DamageTaken","type":"INC","value":-20}}}]`,
		// specialModList ^nearby enemies have (%d+)%% less accuracy rating while you have phasing$ (ModParser.lua:3029)
		"Nearby Enemies have 10% less Accuracy Rating while you have Phasing": `[{"@list":[{"type":"Condition","var":"Phasing"}],"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"Accuracy","type":"MORE","value":-10}}}]`,
		// specialModList ^nearby enemies have fire, cold and lightning exposure while you have phasing, applying %-(%d+)%% to those resistances$ (ModParser.lua:3031)
		"Nearby Enemies have Fire, Cold and Lightning Exposure while you have Phasing, applying -10% to those Resistances": `[{"@list":[{"type":"Condition","var":"Phasing"}],"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"FireExposure","type":"BASE","value":-10}}},{"@list":[{"type":"Condition","var":"Phasing"}],"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"ColdExposure","type":"BASE","value":-10}}},{"@list":[{"type":"Condition","var":"Phasing"}],"flags":0,"keywordFlags":0,"name":"EnemyModifier","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"LightningExposure","type":"BASE","value":-10}}}]`,
		// specialModList ^non%-critical strikes deal (%d+)%% damage$ (ModParser.lua:3642)
		"Non-Critical Strikes deal 25% Damage": `[{"@list":[{"neg":true,"type":"Condition","var":"CriticalStrike"}],"flags":4,"keywordFlags":0,"name":"Damage","type":"MORE","value":-75}]`,
		// specialModList ^non%-instant warcries you use yourself have no cooldown$ (ModParser.lua:5640)
		"Non-Instant Warcries you use yourself have no Cooldown": `[{"@list":[{"neg":true,"skillTypeList":{"@list":[64],"3":37},"type":"SkillType"}],"flags":0,"keywordFlags":4,"name":"CooldownRecovery","type":"OVERRIDE","value":0}]`,
		// specialModList ^non%-unique jewels cause increases and reductions to other damage types in a (%a+) radius to be transformed to apply to (%a+) damage$ (ModParser.lua:2658)
		"Non-Unique Jewels cause Increases and Reductions to other Damage Types in a Large Radius to be Transformed to apply to Fire Damage": `[{"@list":[{"itemSlot":"{SlotName}","neg":true,"rarityCond":"UNIQUE","type":"ItemCondition"},{"itemSlot":"{SlotName}","neg":true,"rarityCond":"RELIC","type":"ItemCondition"}],"flags":0,"keywordFlags":0,"name":"ExtraJewelFunc","type":"LIST","value":{"func":{"$jewelFunc":"convertDamageTypes","dst":"FireDamage"},"radius":"Large","type":"Other"}}]`,
		// specialModList ^non%-unique jewels cause small and notable passive skills in a (%a+) radius to also grant %+(%d+) to (%a+)$ (ModParser.lua:2666)
		"Non-Unique Jewels cause Small and Notable Passive Skills in a Large Radius to also grant +4 to Strength": `[{"@list":[{"itemSlot":"{SlotName}","neg":true,"rarityCond":"UNIQUE","type":"ItemCondition"},{"itemSlot":"{SlotName}","neg":true,"rarityCond":"RELIC","type":"ItemCondition"}],"flags":0,"keywordFlags":0,"name":"ExtraJewelFunc","type":"LIST","value":{"func":{"$jewelFunc":"grantAttribute","stat":"Str","value":4},"radius":"Large","type":"Other"}}]`,
		// specialModList ^overwhelm (%d+)%% physical damage reduction$ (ModParser.lua:5692)
		"Overwhelm 20% Physical Damage Reduction": `[{"flags":0,"keywordFlags":0,"name":"EnemyPhysicalDamageReduction","type":"BASE","value":-20}]`,
		// specialModList ^projectiles deal (%d+)%% increased damage with hits and ailments for each remaining chain, up to (%d+)%%$ (ModParser.lua:2692)
		"Projectiles deal 10% increased Damage with Hits and Ailments for each remaining Chain, up to 40%": `[{"@list":[{"globalLimit":40,"globalLimitKey":"FollowThrough","stat":"ChainRemaining","type":"PerStat"},{"skillType":3,"type":"SkillType"}],"flags":0,"keywordFlags":786432,"name":"Damage","type":"INC","value":10}]`,
		// specialModList ^shield charge and chain hook have (%d+)%% increased attack speed per (%d+) rampage kills$ (ModParser.lua:5877)
		"Shield Charge and Chain Hook have 4% increased Attack Speed per 10 Rampage Kills": `[{"@list":[{"div":"10","limit":100,"limitTotal":true,"type":"Multiplier","var":"Rampage"},{"includeTransfigured":true,"skillNameList":["Shield Charge","Chain Hook"],"type":"SkillName"}],"flags":1,"keywordFlags":0,"name":"Speed","type":"INC","value":4}]`,
		// specialModList ^socketed skill gems get a (%d+)%% mana multiplier$ (ModParser.lua:3291)
		"Socketed Skill Gems get a 80% Mana Multiplier": `[{"@list":[{"slotName":"{SlotName}","type":"SocketedIn"}],"flags":0,"keywordFlags":0,"name":"ExtraSkillMod","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"SupportManaMultiplier","type":"MORE","value":-20}}}]`,
		// specialModList ^socketed triggered bow skills deal (%d+)%% less damage$ (ModParser.lua:3312)
		"Socketed Triggered Bow Skills deal 20% less Damage": `[{"@list":[{"keyword":"bow","slotName":"{SlotName}","type":"SocketedIn"},{"skillType":32,"type":"SkillType"}],"flags":0,"keywordFlags":0,"name":"ExtraSkillMod","type":"LIST","value":{"mod":{"flags":0,"keywordFlags":0,"name":"Damage","type":"MORE","value":-20}}}]`,
		// specialModList ^totems explode on death, dealing (%d+)%% of their life as (.+) damage$ (ModParser.lua:2220)
		"Totems Explode on Death, dealing 15% of their Life as Physical Damage": `[{"flags":0,"keywordFlags":0,"name":"ExplodeMod","type":"LIST","value":{"amount":15,"chance":1,"keyOfScaledMod":"chance","type":"Physical"}},{"flags":0,"keywordFlags":0,"name":"CanExplode","type":"FLAG","value":true}]`,
		// specialModList ^traps and mines deal (%d+)%-(%d+) additional physical damage$ (ModParser.lua:4478)
		"Traps and Mines deal 3-5 additional Physical Damage": `[{"flags":0,"keywordFlags":12288,"name":"PhysicalMin","type":"BASE","value":3},{"flags":0,"keywordFlags":12288,"name":"PhysicalMax","type":"BASE","value":5}]`,
		// specialModList ^vaal volcanic fissure and vaal molten strike have (%d+)%% reduced soul gain prevention duration$ (ModParser.lua:2327)
		"Vaal Volcanic Fissure and Vaal Molten Strike have 40% reduced Soul Gain Prevention Duration": `[{"@list":[{"includeTransfigured":true,"skillNameList":["Volcanic Fissure","Molten Strike"],"type":"SkillName"},{"skillType":38,"type":"SkillType"}],"flags":0,"keywordFlags":0,"name":"SoulGainPreventionDuration","type":"INC","value":-40}]`,
		// specialModList ^while a pinnacle atlas boss is in your presence, flasks gain a charge every (%d+) seconds$ (ModParser.lua:5289)
		"While a Pinnacle Atlas Boss is in your Presence, Flasks gain a Charge every 3 seconds": `[{"@list":[{"actor":"enemy","type":"ActorCondition","var":"PinnacleBoss"}],"flags":0,"keywordFlags":0,"name":"FlaskChargesGenerated","type":"BASE","value":0.33333333333333331}]`,
		// specialModList ^while a unique enemy is in your presence, flasks gain a charge every (%d+) seconds$ (ModParser.lua:5286)
		"While a Unique Enemy is in your Presence, Flasks gain a Charge every 3 seconds": `[{"@list":[{"actor":"enemy","type":"ActorCondition","var":"RareOrUnique"}],"flags":0,"keywordFlags":0,"name":"FlaskChargesGenerated","type":"BASE","value":0.33333333333333331}]`,
		// specialModList ^while stationary, gain (%d+)%% increased area of effect every second, up to a maximum of (%d+)%%$ (ModParser.lua:5764)
		"While stationary, gain 5% increased Area of Effect every second, up to a maximum of 50%": `[{"@list":[{"globalLimit":50,"globalLimitKey":"ExpansiveMight","type":"Multiplier","var":"StationarySeconds"},{"type":"Condition","var":"Stationary"}],"flags":0,"keywordFlags":0,"name":"AreaOfEffect","type":"INC","value":5}]`,
		// specialModList ^while stationary, gain ([%d%.]+)%% of life regenerated per second every second, up to a maximum of (%d+)%%$ (ModParser.lua:5893)
		"While stationary, gain 0.5% of Life Regenerated per second every second, up to a maximum of 2%": `[{"@list":[{"limit":2,"limitTotal":true,"type":"Multiplier","var":"StationarySeconds"},{"type":"Condition","var":"Stationary"}],"flags":0,"keywordFlags":0,"name":"LifeRegenPercent","type":"BASE","value":0.5}]`,
		// m_huge (ModParser.lua:12) resolves to +Inf, which ModCache writes as {"$ref":"math.huge"}.
		"Your Hexes have infinite Duration": `[{"@list":[{"skillType":69,"type":"SkillType"}],"flags":0,"keywordFlags":0,"name":"Duration","type":"BASE",
			"value":{"$ref":"math.huge"}}]`,
		// "^+(%d+) to ...": a leading "+" in a Lua pattern is a plain character.
		"+25 to maximum Life per Allocated Journey Tattoo of the Body": `[{"@list":[{"type":"Multiplier","var":"JourneyTattooBody"}],"flags":0,"keywordFlags":0,
			"name":"Life","type":"BASE","value":25},{"flags":0,"keywordFlags":0,"name":"Multiplier:JourneyTattooBody","type":"BASE","value":1}]`,
	}
	p, err := PoE1()
	if err != nil {
		t.Fatal(err)
	}
	// Lines whose Lua function returns nil: PoB leaves them unparsed.
	for _, line := range []string{
		"Base Chill Duration is 2 seconds", // 3785: only bleed, ignite and poison
	} {
		if got, extra := p.parseRaw(line); got != nil && extra == "" {
			gj, _ := json.Marshal(got)
			t.Errorf("%q: parsed as %s, want unparsed", line, gj)
		}
	}
	for line, wantJSON := range hookCases {
		var want any
		if jerr := json.Unmarshal([]byte(wantJSON), &want); jerr != nil {
			t.Fatalf("%q: bad case: %v", line, jerr)
		}
		got, extra := p.parseRaw(line)
		if extra != "" || !equalMods(got, want) {
			gj, _ := json.Marshal(got)
			t.Errorf("%q (extra %q)\n  got  %s\n  want %s", line, extra, gj, wantJSON)
		}
	}
}
