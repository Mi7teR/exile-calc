package engine

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/modparser"
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

// flaskBreakdown is CalcPerform.lua's "flask breakdown" (1617-1637): the
// flask outputs PoB sets only with a breakdown, which MAIN mode always
// has. used is the number of active flasks (env.flasks).
func flaskBreakdown(db *ModDB, effectInc float64, used int) {
	out := db.output
	generated := db.Sum("BASE", "FlaskChargesGenerated")
	const flaskSlots = 5
	perFlask := db.Sum("BASE", "FlaskChargesGeneratedPerEmptyFlask") * float64(flaskSlots-used)
	total := float64(generated) + float64(perFlask)
	out["FlaskEffect"] = effectInc
	out["FlaskChargeGen"] = total
	out["LifeFlaskChargeGen"] = total + db.Sum("BASE", "LifeFlaskChargesGenerated")
	out["ManaFlaskChargeGen"] = total + db.Sum("BASE", "ManaFlaskChargesGenerated")
	out["UtilityFlaskChargeGen"] = total + db.Sum("BASE", "UtilityFlaskChargesGenerated")
	const chanceCap = 100
	out["FlaskChargeOnCritChance"] = math.Min(chanceCap, db.Sum("BASE", "FlaskChargeOnCritChance"))
}

// It returns what later steps need: the Silver Flask's effect for
// Onslaught (actorMisc) and the recovery flasks (flaskRecovery).
func (c *Calculator) applyFlasks(db *ModDB, res *Result, slots []flaskSlot) *flaskState {
	st := &flaskState{silverInc: -100, // as in doActorMisc
		durInc: db.Sum("INC", "FlaskDuration"), totalRateInc: db.Sum("INC", "FlaskRecoveryRate")}
	if !modeCombat(db) { // CalcPerform.lua:1968
		return st
	}
	flasks := c.activeFlasks(db, slots)
	player := &Cfg{Actor: "player"}
	effectInc := db.SumCfg(player, "INC", "FlaskEffect")
	st.effectInc = effectInc
	effectIncMagic := db.SumCfg(player, "INC", "MagicUtilityFlaskEffect")
	if db.buildBreakdown { // breakdown-only outputs: MAIN mode
		flaskBreakdown(db, effectInc, len(flasks))
	}
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
		// mergeFlasks: only flasks that are not fully instant count as used.
		if flaskInstant(db, calcLocal(mods, "FlaskInstantRecovery", "BASE"), calcLocal(mods, "FlaskLowLifeInstantRecovery", "BASE"), base.Life > 0, base.Mana > 0) < 100 {
			db.conditions["UsingFlask"] = true
			db.conditions["Using"+strings.ReplaceAll(it.BaseName, " ", "")] = true
			if base.Life > 0 && !db.Flag("CannotRecoverLifeOutsideLeech") {
				db.conditions["UsingLifeFlask"] = true
			}
			if base.Mana > 0 {
				db.conditions["UsingManaFlask"] = true
			}
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
		localInc := inc - effectInc // flaskData.effectInc
		if stat := flaskTypeEffect[it.BaseName]; stat != "" {
			inc += db.SumCfg(player, "INC", stat)
		}
		if it.Rarity == "MAGIC" && !recovery {
			inc += effectIncMagic
		}
		scale := 1 + inc/100
		if strings.Contains(it.BaseName, "Silver Flask") {
			st.silver = true
			st.silverInc = math.Max(st.silverInc, inc/100)
			db.silverFlasks = append(db.silverFlasks, silverFlask{localInc: localInc, magic: it.Rarity == "MAGIC"})
		}
		if recovery {
			st.recovery = append(st.recovery, recoveryFlask(it, item, mods, scale))
		}
		source := itemSource(item, it)
		// mergeFlasks' flaskBuffsNonPlayer inputs, for the minion: a flask
		// with buff or mod lines, a non-unique one, an active Quicksilver
		// (flaskBuffsPerBaseNonPlayer gets a key per active base).
		hasMods := len(base.Buff) > 0 || len(mods) > 0
		db.minionFlasks.any = db.minionFlasks.any || hasMods
		unique := it.Rarity == "UNIQUE" || it.Rarity == "RELIC" //nolint:goconst // item rarity, as below
		db.minionFlasks.nonUnique = db.minionFlasks.nonUnique || (hasMods && !unique)
		db.minionFlasks.quicksilver = db.minionFlasks.quicksilver || it.BaseName == "Quicksilver Flask"

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
		// Item.lua: the mods calcLocal reads are removed from the flask's
		// mod list.
		local := [][2]string{{"Duration", "INC"}, {"Duration", "MORE"}, {"FlaskCharges", "BASE"},
			{"FlaskChargesUsed", "INC"}, {"FlaskChargeRecovery", "INC"}, {"FlaskEffect", "INC"}, {"LocalEffect", "INC"}}
		if recovery {
			local = append(local, [2]string{"FlaskInstantRecovery", "BASE"}, [2]string{"FlaskLowLifeInstantRecovery", "BASE"},
				[2]string{"FlaskRecovery", "INC"}, [2]string{"FlaskRecoveryRate", "INC"})
			if base.Life > 0 {
				local = append(local, [2]string{"FlaskAdditionalLifeRecovery", "BASE"})
			}
		}
		kept := withoutLocal(mods, local)
		key := it.Name
		if it.Rarity != "UNIQUE" && it.Rarity != "RELIC" {
			var b strings.Builder
			for _, m := range kept {
				b.WriteString(formatModParams(m) + "&")
			}
			key = b.String()
		}
		for _, m := range kept {
			add("mods:"+key, scaleMod(fromParsed(m, source), scale))
		}
	}
	for _, k := range keys {
		for _, m := range merged[k] {
			db.Add(m)
		}
	}
	return st
}

// flaskState carries mergeFlasks' locals to the recovery pass.
type flaskState struct {
	silver               bool
	silverInc            float64
	effectInc            float64 // FlaskEffect on the player
	durInc, totalRateInc float64 // FlaskDuration, FlaskRecoveryRate
	recovery             []flaskData
}

// flaskData is the part of Item.lua's flaskData that calcFlaskRecovery reads.
type flaskData struct {
	name                           string
	life, mana                     bool
	lifeBase, manaBase, duration   float64
	instantPerc, instantLowLife    float64
	lifeNotRemoved, manaNotRemoved bool
	effectMod                      float64 // calcFlaskMods' effectMod
}

func recoveryFlask(it *itemtext.Item, item *pobcode.Item, mods []modparser.Mod, effectMod float64) flaskData {
	f := it.Base.Flask
	local := func(name, typ string) float64 { return calcLocal(mods, name, typ) }
	more := 1.0
	flag := func(name string) bool {
		for _, m := range mods {
			if m.Name == name && m.Type == "FLAG" && truthy(m.Value) {
				return true
			}
		}
		return false
	}
	for _, m := range mods {
		if m.Name == "Duration" && m.Type == "MORE" && m.Flags == 0 && m.KeywordFlags == 0 && len(m.Tags) == 0 {
			if v, ok := num(m.Value); ok {
				more *= 1 + v/100
			}
		}
	}
	recoveryMod := 1 + local("FlaskRecovery", "INC")/100
	rateMod := 1 + local("FlaskRecoveryRate", "INC")/100
	q := 1 + float64(it.Quality)/100
	d := flaskData{name: itemSource(item, it), life: f.Life > 0, mana: f.Mana > 0,
		duration:       round(f.Duration*(1+local("Duration", "INC")/100)/rateMod*more, 1),
		instantPerc:    local("FlaskInstantRecovery", "BASE"),
		instantLowLife: local("FlaskLowLifeInstantRecovery", "BASE"),
		lifeBase:       f.Life * q * recoveryMod, manaBase: f.Mana * q * recoveryMod,
		lifeNotRemoved: flag("LifeFlaskEffectNotRemoved"), manaNotRemoved: flag("ManaFlaskEffectNotRemoved"),
		effectMod: effectMod}
	return d
}

// flaskInstant is getFlaskInstantRecovery: the flask's instant recovery
// percentage, capped at 100.
func flaskInstant(db *ModDB, perc, lowLife float64, life, mana bool) float64 {
	if db.Flag("Condition:LowLife") {
		perc += lowLife
	}
	if life {
		perc += db.Sum("BASE", "LifeFlaskInstantRecovery")
	}
	if mana {
		perc += db.Sum("BASE", "ManaFlaskInstantRecovery")
	}
	return math.Min(100, perc)
}

// flaskRecovery is the second mergeFlasks pass (onlyRecovery): flasks whose
// effect is not removed at full recover over their duration
// (calcFlaskRecovery); the highest flask of each kind applies.
func flaskRecovery(db *ModDB, st *flaskState) {
	if st == nil || !modeCombat(db) || db.Flag("FlasksDoNotApplyToPlayer") { // mode_combat: CalcPerform.lua:3152
		return
	}
	merged := map[string][]Mod{}
	add := func(key string, m Mod) {
		for i, d := range merged[key] {
			if sameParams(m, d) {
				a, _ := num(m.Value)
				b, _ := num(d.Value)
				if a > b {
					merged[key][i] = m
				}
				return
			}
		}
		merged[key] = append(merged[key], m)
	}
	for _, fd := range st.recovery {
		instant := flaskInstant(db, fd.instantPerc, fd.instantLowLife, fd.life, fd.mana)
		for _, x := range []struct {
			typ         string
			has, notRem bool
			base        float64
		}{{"Life", fd.life && !db.Flag("CannotRecoverLifeOutsideLeech"), fd.lifeNotRemoved, fd.lifeBase},
			{"Mana", fd.mana, fd.manaNotRemoved, fd.manaBase}} {
			if !x.has || !(x.notRem || db.Flag(x.typ+"FlaskEffectNotRemoved")) {
				continue
			}
			total := x.base * (1 - instant/100) * (1 + db.Sum("INC", "Flask"+x.typ+"Recovery")/100) *
				db.More("Flask"+x.typ+"Recovery") * (1 + st.durInc/100)
			dur := fd.duration * (1 + st.durInc/100) / (1 + st.totalRateInc/100) /
				(1 + db.Sum("INC", "Flask"+x.typ+"RecoveryRate")/100) / db.More("Flask"+x.typ+"RecoveryRate")
			if low := db.More("FlaskLifeRecoveryLowLife"); low > 1 && x.typ == "Life" {
				total *= (low-1)/(1+st.effectInc/100) + 1
			}
			rate := total / dur
			out := []Mod{{Name: x.typ + "Recovery", Type: "BASE", Value: rate, Source: fd.name}}
			if db.Flag(x.typ + "FlaskAppliesToEnergyShield") {
				out = append(out, Mod{Name: "EnergyShieldRecovery", Type: "BASE", Value: rate, Source: fd.name})
			}
			if db.Flag(x.typ + "FlaskAppliesToLife") {
				out = append(out, Mod{Name: "LifeRecovery", Type: "BASE", Value: rate, Source: fd.name})
			}
			for _, m := range out {
				add(x.typ+"Flask", scaleMod(m, fd.effectMod))
			}
		}
	}
	for _, k := range []string{"LifeFlask", "ManaFlask"} {
		for _, m := range merged[k] {
			db.Add(m)
		}
	}
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

// tinctureEffect holds the tincture effect increases and limit of
// calcs.perform, read before the flasks are merged.
type tinctureEffect struct{ inc, incMagic, limit float64 }

func readTinctureEffect(db *ModDB) tinctureEffect {
	player := &Cfg{Actor: "player"}
	return tinctureEffect{db.SumCfg(player, "INC", "TinctureEffect"), db.SumCfg(player, "INC", "MagicTinctureEffect"), db.Sum("BASE", "TinctureLimit")}
}

// applyTinctures is mergeTinctures of calcs.perform (mode_combat): the
// active tinctures, up to TinctureLimit in slot order, scaled by tincture
// effect and quality.
//
// The effect increases and the limit are tinctureEffect's, read before the
// flasks are merged (CalcPerform.lua reads them above mergeFlasks), so
// flask conditions do not reach them.
func (c *Calculator) applyTinctures(db *ModDB, res *Result, slots []flaskSlot, te tinctureEffect) {
	if !modeCombat(db) { // CalcPerform.lua:2128
		return
	}
	notBurn := math.Min(db.Sum("BASE", "TincturesNotInflictManaBurn"), 100)
	if !db.Flag("Condition:WeepingWoundsInsteadOfManaBurn") && !(notBurn < 100 && db.output["ManaUnreserved"] > 0) {
		return
	}
	effectInc, effectIncMagic, limit := te.inc, te.incMagic, te.limit
	sort.SliceStable(slots, func(i, j int) bool { return slots[i].num < slots[j].num })
	merged := map[string][]Mod{}
	var keys []string
	for _, sl := range slots {
		if !sl.active {
			continue
		}
		// applyFlasks already read every active slot and reported its
		// unparsed lines; a scratch Result keeps them from being listed twice.
		it, mods := c.itemMods(sl.item, &Result{})
		if it == nil || it.Base == nil || it.Base.Tincture == nil {
			continue
		}
		if limit <= 0 {
			break
		}
		limit--
		db.conditions["UsingTincture"] = true
		db.conditions["Using"+strings.ReplaceAll(it.BaseName, " ", "")] = true
		inc := effectInc + calcLocal(mods, "TinctureEffect", "INC") + calcLocal(mods, "LocalEffect", "INC")
		if it.Rarity == "MAGIC" {
			inc += effectIncMagic
		}
		scale := math.Floor((1+inc/100)*(1+float64(it.Quality)/100)*100) / 100
		mods = withoutLocal(mods, [][2]string{{"TinctureManaBurnRate", "INC"}, {"TinctureManaBurnRate", "MORE"},
			{"TinctureCooldownRecovery", "INC"}, {"CooldownRecovery", "INC"}, {"TinctureEffect", "INC"}, {"LocalEffect", "INC"}})
		key := it.Name
		if it.Rarity != "UNIQUE" && it.Rarity != "RELIC" {
			var b strings.Builder
			for _, m := range mods {
				b.WriteString(formatModParams(m) + "&")
			}
			key = b.String()
		}
		if _, ok := merged[key]; !ok {
			keys = append(keys, key)
		}
		source := itemSource(sl.item, it)
	next:
		for _, pm := range mods {
			m := scaleMod(fromParsed(pm, source), scale)
			for i, d := range merged[key] {
				if m.Type != "LIST" && sameParams(m, d) {
					if a, ok := num(m.Value); ok {
						if b, ok := num(d.Value); ok && a > b {
							merged[key][i] = m
						}
					}
					continue next
				}
			}
			merged[key] = append(merged[key], m)
		}
	}
	ranged := db.Flag("TinctureRangedWeapons")
	for _, k := range keys {
		for _, m := range merged[k] {
			db.Add(m)
			if ranged && m.Flags&mfWeaponMelee == mfWeaponMelee {
				m.Flags = m.Flags&^mfWeaponMelee | mfWeaponRanged
				db.Add(m)
			}
		}
	}
}

// withoutLocal drops the mods Item.lua calcLocal consumes (and removes):
// the given names and types, without flags, keyword flags or tags other
// than InSlot.
func withoutLocal(mods []modparser.Mod, local [][2]string) []modparser.Mod {
	var out []modparser.Mod
next:
	for _, m := range mods {
		if m.Flags == 0 && m.KeywordFlags == 0 && (len(m.Tags) == 0 || tagStr(m.Tags[0], "type") == "InSlot") {
			for _, l := range local {
				if m.Name == l[0] && m.Type == l[1] {
					continue next
				}
			}
		}
		out = append(out, m)
	}
	return out
}

// formatModParams is ModTools.lua modLib.formatModParams: a mod's name, type,
// flags and tags without its value, the key that groups magic flasks and
// tinctures whose mods differ only in value.
func formatModParams(m modparser.Mod) string {
	return fmt.Sprintf("%s|%s|%d|%d|%v", m.Name, m.Type, m.Flags, m.KeywordFlags, m.Tags)
}
