package engine

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// Base types with their own "increased effect" stat (CalcPerform.lua calcFlaskMods).
var flaskTypeEffect = map[string]string{
	"Iron Flask": "IronFlaskEffect", "Granite Flask": "GraniteFlaskEffect", "Ruby Flask": "RubyFlaskEffect",
	"Sapphire Flask": "SapphireFlaskEffect", "Topaz Flask": "TopazFlaskEffect",
}

// applyFlasks ports the player part of mergeFlasks (Modules/CalcPerform.lua):
// active flasks add their base buff and their own mods, scaled by flask
// effect; mods with the same parameters from flasks of one base (or the
// same unique / the same mod set) do not stack, the highest wins.
// flaskSlot is a flask in the active item set and whether its slot is active.
type flaskSlot struct {
	item   *pobcode.Item
	active bool
	num    int // the N of "Flask N"
}

// activeFlasks ports env.flasks: flasks in active slots (CalcSetup.lua),
// plus Mageblood's magic utility flasks counted from the left and right
// (CalcPerform.lua "Special handling of Mageblood"). It also sets
// Have<BaseName> for every flask.
func (c *Calculator) activeFlasks(db *ModDB, slots []flaskSlot) []*pobcode.Item {
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].num < slots[j].num })
	active := map[*pobcode.Item]bool{}
	var magicUtility []*pobcode.Item
	for _, s := range slots {
		it := c.peekItem(s.item)
		if it == nil {
			continue
		}
		db.conditions["Have"+strings.ReplaceAll(it.BaseName, " ", "")] = true
		if s.active {
			active[s.item] = true
		}
		if it.Rarity == itemtext.Magic && !strings.Contains(it.BaseName, "Life Flask") &&
			!strings.Contains(it.BaseName, "Mana Flask") && !strings.Contains(it.BaseName, "Hybrid Flask") {
			magicUtility = append(magicUtility, s.item)
		}
	}
	left := int(db.Sum("BASE", "LeftActiveMagicUtilityFlasks"))
	right := int(db.Sum("BASE", "RightActiveMagicUtilityFlasks"))
	for i := 0; i < min(left, len(magicUtility)); i++ {
		active[magicUtility[i]] = true
	}
	for i := len(magicUtility) - 1; right > 0 && i >= max(len(magicUtility)-right, 0); i-- {
		active[magicUtility[i]] = true
	}
	var out []*pobcode.Item
	for _, s := range slots {
		if active[s.item] {
			out = append(out, s.item)
		}
	}
	return out
}

// It returns whether a Silver Flask is active and the highest such flask's
// increased effect as a fraction, for Onslaught in actorMisc.
func (c *Calculator) applyFlasks(db *ModDB, res *Result, slots []flaskSlot) (silver bool, silverInc float64) {
	silverInc = -100 // as in doActorMisc
	flasks := c.activeFlasks(db, slots)
	player := &Cfg{Actor: "player"}
	effectInc := db.SumCfg(player, "INC", "FlaskEffect")
	effectIncMagic := db.SumCfg(player, "INC", "MagicUtilityFlaskEffect")
	merged := map[string][]Mod{}
	var keys []string
	add := func(key string, m Mod) {
		if _, ok := merged[key]; !ok {
			keys = append(keys, key)
		}
		for i, d := range merged[key] {
			if m.Type != "LIST" && sameParams(m, d) {
				if a, ok := num(m.Value); ok {
					if b, ok := num(d.Value); ok && a > b {
						merged[key][i] = m
					}
				}
				return
			}
		}
		merged[key] = append(merged[key], m)
	}
	for _, item := range flasks {
		if item == nil {
			continue
		}
		it, mods := c.itemMods(item, res)
		if it == nil || it.Base == nil || it.Base.Flask == nil {
			continue
		}
		base := it.Base.Flask
		recovery := base.Life > 0 || base.Mana > 0
		db.conditions["UsingFlask"] = true
		db.conditions["Using"+strings.ReplaceAll(it.BaseName, " ", "")] = true
		if base.Life > 0 {
			db.conditions["UsingLifeFlask"] = true
		}
		if base.Mana > 0 {
			db.conditions["UsingManaFlask"] = true
		}

		// Item.lua: flaskData.effectInc from the flask's own mods.
		inc := effectInc
		for _, m := range mods {
			if (m.Name == "FlaskEffect" || m.Name == "LocalEffect") && m.Type == "INC" && len(m.Tags) == 0 {
				if f, ok := num(m.Value); ok {
					inc += f
				}
			}
		}
		if stat := flaskTypeEffect[it.BaseName]; stat != "" {
			inc += db.SumCfg(player, "INC", stat)
		}
		if it.Rarity == "MAGIC" && !recovery {
			inc += effectIncMagic
		}
		scale := 1 + inc/100
		if strings.Contains(it.BaseName, "Silver Flask") {
			silver = true
			silverInc = math.Max(silverInc, inc/100)
		}
		source := itemSource(item, it)

		for _, line := range base.Buff {
			r := c.parser.Parse(line)
			if !r.Parsed() {
				res.Unparsed = append(res.Unparsed, line)
				continue
			}
			for _, m := range r.Mods {
				add("base:"+it.BaseName, scaleMod(fromParsed(m, source), scale))
			}
		}
		key := it.Name
		if it.Rarity != "UNIQUE" && it.Rarity != "RELIC" {
			var b strings.Builder
			for _, m := range mods {
				fmt.Fprintf(&b, "%s|%s|%v&", m.Name, m.Type, m.Value)
			}
			key = b.String()
		}
		for _, m := range mods {
			add("mods:"+key, scaleMod(fromParsed(m, source), scale))
		}
	}
	for _, k := range keys {
		for _, m := range merged[k] {
			db.Add(m)
		}
	}
	return
}

// highPrecisionMods is Data.lua data.highPrecisionMods: the decimal places
// a scaled value keeps (ModStoreClass:ScaleAddMod).
var highPrecisionMods = map[[2]string]int{
	{"CritChance", "BASE"}: 2, {"SelfCritChance", "BASE"}: 2, {"LifeRegenPercent", "BASE"}: 2,
	{"ManaRegenPercent", "BASE"}: 2, {"EnergyShieldRegenPercent", "BASE"}: 2, {"LifeDegenPercent", "BASE"}: 2,
	{"LifeDegenPercentTincture", "BASE"}: 2, {"ManaDegenPercent", "BASE"}: 2,
	{"ManaDegenPercentTincture", "BASE"}: 2, {"EnergyShieldDegenPercent", "BASE"}: 2,
	{"DamageLifeLeech", "BASE"}: 2, {"DamageManaLeech", "BASE"}: 2, {"DamageEnergyShieldLeech", "BASE"}: 2,
	{"PhysicalDamageLifeLeech", "BASE"}: 2, {"PhysicalDamageManaLeech", "BASE"}: 2,
	{"PhysicalDamageEnergyShieldLeech", "BASE"}: 2, {"ElementalDamageLifeLeech", "BASE"}: 2,
	{"ElementalDamageManaLeech", "BASE"}: 2, {"ElementalDamageEnergyShieldLeech", "BASE"}: 2,
	{"FireDamageLifeLeech", "BASE"}: 2, {"FireDamageManaLeech", "BASE"}: 2,
	{"FireDamageEnergyShieldLeech", "BASE"}: 2, {"ColdDamageLifeLeech", "BASE"}: 2,
	{"ColdDamageManaLeech", "BASE"}: 2, {"ColdDamageEnergyShieldLeech", "BASE"}: 2,
	{"LightningDamageLifeLeech", "BASE"}: 2, {"LightningDamageManaLeech", "BASE"}: 2,
	{"LightningDamageEnergyShieldLeech", "BASE"}: 2, {"ChaosDamageLifeLeech", "BASE"}: 2,
	{"ChaosDamageManaLeech", "BASE"}: 2, {"ChaosDamageEnergyShieldLeech", "BASE"}: 2,
	{"LifeRegen", "BASE"}: 1, {"ManaRegen", "BASE"}: 1, {"EnergyShieldRegen", "BASE"}: 1,
	{"RageRegen", "BASE"}: 1, {"LifeDegen", "BASE"}: 1, {"ManaDegen", "BASE"}: 1,
	{"EnergyShieldDegen", "BASE"}:     1,
	{"SupportManaMultiplier", "MORE"}: 4,
}

const defaultHighPrecision = 1 // Data.lua data.defaultHighPrecision

// scaleValue is the number part of ModStoreClass:ScaleAddMod.
func scaleValue(name, typ string, v, scale float64) float64 {
	precision, ok := highPrecisionMods[[2]string{name, typ}]
	if !ok && math.Floor(v) != v {
		precision, ok = defaultHighPrecision, true
	}
	if ok {
		p := math.Pow(10, float64(precision))
		return math.Floor(v*scale*p) / p
	}
	return modfInt(round(v*scale, 2))
}

// scaleMod is ModStoreClass:ScaleAddMod: a copy of m with its value (or
// its sub-mod's, or the keyOfScaledMod entry) scaled.
func scaleMod(m Mod, scale float64) Mod {
	if scale == 1 {
		return m
	}
	for _, t := range m.Tags {
		if tagBool(t, "unscalable") {
			return m
		}
	}
	switch v := m.Value.(type) {
	case float64:
		m.Value = scaleValue(m.Name, m.Type, v, scale)
	case map[string]any:
		if sub, ok := v["mod"].(map[string]any); ok {
			if f, ok := sub["value"].(float64); ok {
				c := copyMap(v)
				s := copyMap(sub)
				name, _ := sub["name"].(string)
				typ, _ := sub["type"].(string)
				s["value"] = scaleValue(name, typ, f, scale)
				c["mod"] = s
				m.Value = c
			}
		} else if key, ok := v["keyOfScaledMod"].(string); ok {
			if f, ok := num(v[key]); ok {
				c := copyMap(v)
				c[key] = round(f*scale, 2)
				m.Value = c
			}
		}
	}
	return m
}

func copyMap(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// sameParams is modLib.compareModParams: name, type, flags, keywordFlags
// and tags equal.
func sameParams(a, b Mod) bool {
	if a.Name != b.Name || a.Type != b.Type || a.Flags != b.Flags || a.KeywordFlags != b.KeywordFlags || len(a.Tags) != len(b.Tags) {
		return false
	}
	return fmt.Sprint(a.Tags) == fmt.Sprint(b.Tags)
}
