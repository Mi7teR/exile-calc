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
	// Ports that predate this check: the generator exports these rules as a
	// $template (their Lua function only substitutes num), so the $hook key
	// is never looked up.
	stale := map[string]bool{
		"specialModList[^your offerings have (%d+)%% increased effect on you$]":                                                                                           true,
		"specialModList[^during f?l?a?s?k? ?effect, damage penetrates (%d+)%% o?f? ?resistance of each element for which your uncapped elemental resistance is highest$]": true,
	}
	for name := range hooks {
		if strings.HasPrefix(name, "specialModList[") && !inData[name] && !stale[name] {
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
	}
	p, err := PoE1()
	if err != nil {
		t.Fatal(err)
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
