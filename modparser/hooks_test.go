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
