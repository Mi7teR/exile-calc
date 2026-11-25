package engine

import "math"

const lowPoolThreshold = 0.5 // Data.lua data.misc.LowPoolThreshold

func floorTo(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Floor(v*p) / p
}

func dataNum(d map[string]any, k string) (float64, bool) {
	f, ok := num(d[k])
	return f, ok
}

// reservations ports "Calculate skill life and mana reservations" of
// calcs.perform and doActorLifeManaReservation (CalcPerform.lua).
func reservations(db *ModDB, skills []*activeSkill, res *Result) {
	out := db.output
	reservedBase := map[string]float64{"Life": 0, "Mana": 0}
	reservedPercent := map[string]float64{"Life": db.Sum("BASE", "ExtraLifeReserved"), "Mana": 0}
	uncancellable := map[string]float64{"Life": db.Sum("BASE", "ExtraLifeReserved"), "Mana": db.Sum("BASE", "ExtraManaReserved")}
	for _, s := range skills {
		if s.disabled || s.mods == nil {
			continue
		}
		if truthy(s.data["requiresManaReservation"]) && out["Mana"] == 0 {
			s.disabled = true
			continue
		}
		if !(s.types[stHasReservation] || truthy(s.data["triggeredByAutoexertion"])) || s.types[stReservationBecomesCost] {
			continue
		}
		list, cfg := s.mods, s.cfg
		level := s.active.ge.Levels[s.active.level]
		if level == nil {
			continue
		}
		mult := floorTo(list.MoreCfg(cfg, "SupportManaMultiplier"), 4)
		base := func(key string, fromLevel float64) float64 {
			if v, ok := dataNum(s.data, key); ok {
				return v
			}
			return fromLevel
		}
		type pool struct{ flat, percent float64 }
		pools := map[string]*pool{
			"Mana": {base("manaReservationFlat", level.ManaReservationFlat), base("manaReservationPercent", level.ManaReservationPercent)},
			"Life": {base("lifeReservationFlat", level.LifeReservationFlat), base("lifeReservationPercent", level.LifeReservationPercent)},
		}
		// Skills whose cost becomes a reservation (Minefield-like supports).
		if level.Cost != nil {
			for _, name := range []string{"Mana", "Life"} {
				if list.FlagCfg(cfg, name+"CostGainAsReservation") {
					pools[name].flat = list.SumCfg(cfg, "BASE", name+"CostBase") + level.Cost[name]
				}
			}
		}
		forced := map[string]any{}
		for _, k := range []string{"ManaReservationFlatForced", "ManaReservationPercentForced", "LifeReservationFlatForced", "LifeReservationPercentForced"} {
			if v, ok := s.data[k]; ok {
				forced[k] = v
			}
		}
		if list.FlagCfg(cfg, "BloodMagicReserved") && !truthy(s.data["requiresManaReservation"]) {
			pools["Life"].flat += pools["Mana"].flat
			pools["Mana"].flat = 0
			pools["Life"].percent += pools["Mana"].percent
			pools["Mana"].percent = 0
			forced["LifeReservationFlatForced"], forced["ManaReservationFlatForced"] = forced["ManaReservationFlatForced"], nil
			forced["LifeReservationPercentForced"], forced["ManaReservationPercentForced"] = forced["ManaReservationPercentForced"], nil
		}
		for _, name := range []string{"Mana", "Life"} {
			p := pools[name]
			resourceMore := list.MoreCfg(cfg, name+"Reserved")
			genericMore := list.MoreCfg(cfg, "Reserved")
			inc := list.SumCfg(cfg, "INC", name+"Reserved", "Reserved")
			eff := math.Max(list.SumCfg(cfg, "INC", name+"ReservationEfficiency", "ReservationEfficiency"), -100)
			effMore := list.MoreCfg(cfg, name+"ReservationEfficiency", "ReservationEfficiency")
			stage := func(v float64) float64 {
				v += modfInt(v * inc / 100)
				v += modfInt(v * (resourceMore - 1))
				v += modfInt(v * (genericMore - 1))
				return v
			}
			var flat, pct float64
			if f, ok := num(forced[name+"ReservationFlatForced"]); ok {
				flat = f
			} else if b := modfInt(p.flat * mult); b != 0 {
				flat = math.Max(round(stage(b)/(1+eff/100)/effMore, 0), 0)
			}
			if f, ok := num(forced[name+"ReservationPercentForced"]); ok {
				pct = f
			} else if b := modfInt(p.percent * 100 * mult); b != 0 {
				pct = math.Max(round(stage(b)/(1+eff/100)/effMore, 0)/100, 0)
			}
			if s.mineCount > 0 {
				flat *= s.mineCount
				pct *= s.mineCount
			}
			// Blood Sacrament increases reservation per stage channelled.
			if s.active.ge.Name == "Blood Sacrament" && s.hasStageCount {
				flat *= s.activeStageCount + 1
				pct *= s.activeStageCount + 1
			}
			// CalcPerform.lua: the skill's own reservation, for GetStat
			// ManaReservedPercent and LifeReservedPercent.
			if flat != 0 {
				s.data[name+"ReservedBase"] = flat
			}
			if pct != 0 {
				s.data[name+"ReservedPercent"] = pct
				s.data[name+"ReservedBase"] = flat + math.Ceil(out[name]*pct/100)
			}
			reservedBase[name] += flat
			reservedPercent[name] += pct
			if list.FlagCfg(cfg, "HasUncancellableReservation") {
				uncancellable[name] += pct
			}
		}
	}
	lifeManaReservation(db, reservedBase, reservedPercent, uncancellable, !db.Flag("ManaIncreasedByOvercappedLightningRes"))
}

// lifeManaReservation is doActorLifeManaReservation.
func lifeManaReservation(db *ModDB, base, percent, uncancellable map[string]float64, addAura bool) {
	out := db.output
	for _, pool := range []string{"Life", "Mana"} {
		max := out[pool]
		reserved := 0.0
		if max > 0 {
			low := db.Sum("BASE", "Low"+pool+"Percentage")
			reserved = base[pool] + math.Ceil(max*percent[pool]/100)
			out[pool+"Reserved"] = math.Min(reserved, max)
			out[pool+"ReservedPercent"] = math.Min(reserved/max*100, 100)
			out[pool+"Unreserved"] = max - reserved
			out[pool+"UnreservedPercent"] = (max - reserved) / max * 100
			out[pool+"UncancellableReservation"] = math.Min(uncancellable[pool], 0)
			out[pool+"CancellableReservation"] = 100 - uncancellable[pool]
			threshold := lowPoolThreshold
			if low > 0 {
				threshold = low
			}
			if (max-reserved)/max <= threshold {
				db.conditions["Low"+pool] = true
			}
		}
		if addAura {
			for _, e := range db.List(nil, "GrantReserved"+pool+"AsAura") {
				v, _ := e.Value.(map[string]any)
				mm, ok := v["mod"].(map[string]any)
				if !ok {
					continue
				}
				aura := modFromMap(mm, e.Mod.Source)
				f, _ := num(aura.Value)
				aura.Value = math.Floor(f * math.Min(reserved, max))
				db.Add(Mod{Name: "ExtraAura", Type: "LIST", Value: map[string]any{"mod": modToMap(aura)}, Source: e.Mod.Source})
			}
		}
	}
}

// modToMap encodes a Mod like the generated data, for LIST values.
func modToMap(m Mod) map[string]any {
	out := map[string]any{"name": m.Name, "type": m.Type, "value": m.Value, "flags": float64(m.Flags), "keywordFlags": float64(m.KeywordFlags)}
	if len(m.Tags) > 0 {
		tags := make([]any, len(m.Tags))
		for i, t := range m.Tags {
			tags[i] = t
		}
		out["@list"] = tags
	}
	return out
}
