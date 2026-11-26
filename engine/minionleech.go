package engine

// The leech caps of env.minion. CalcSetup.lua:62-70 (calcs.initModDB)
// gives every actor the base leech mods, and calcs.defence
// (CalcDefence.lua:1243-1272) turns them into the caps; this branch had
// neither for the minion, so its caps were 0. The player's side is the
// misc-rest branch's leechCaps: on a merge, the minion should call that
// and drop minionLeechCaps, keeping minionLeechBaseMods if leechCaps does
// not add the base mods.

// Data/Misc.lua characterConstants.
const (
	maxLifeLeechRatePerMinute  = 1200 // maximum_life_leech_rate_%_per_minute
	maxManaLeechRatePerMinute  = 1200 // maximum_mana_leech_rate_%_per_minute
	maxLeechInstancePercent    = 10   // maximum_*_leech_amount_per_leech_%_max_*
	maxEnergyShieldLeechRate   = 10   // CalcSetup.lua:67
	secondsPerMinute           = 60
	minionLeechMaxRatePercent  = "MaxLifeLeechRatePercent"
	minionLeechParentFlag      = "MaximumLifeLeechIsEqualToParent"
	minionLeechPartyMemberFlag = "MaximumLifeLeechIsEqualToPartyMember"
)

// minionLeechBaseMods is the leech part of calcs.initModDB
// (CalcSetup.lua:62-63, 67-70).
func minionLeechBaseMods(mdb *ModDB) {
	mdb.NewMod("MaxLifeLeechRate", "BASE", maxLifeLeechRatePerMinute/secondsPerMinute, "Base")
	mdb.NewMod("MaxManaLeechRate", "BASE", maxManaLeechRatePerMinute/secondsPerMinute, "Base")
	mdb.NewMod("MaxEnergyShieldLeechRate", "BASE", maxEnergyShieldLeechRate, "Base")
	mdb.NewMod("MaxLifeLeechInstance", "BASE", maxLeechInstancePercent, "Base")
	mdb.NewMod("MaxManaLeechInstance", "BASE", maxLeechInstancePercent, "Base")
	mdb.NewMod("MaxEnergyShieldLeechInstance", "BASE", maxLeechInstancePercent, "Base")
}

// minionLeechCaps is the "Leech caps" block of calcs.defence
// (CalcDefence.lua:1243-1272) for env.minion. A minion whose maximum life
// leech equals its parent's takes the player's MaxLifeLeechRatePercent
// (actor.parent.output).
func minionLeechCaps(mdb *ModDB) {
	const pct = 100.0
	out := mdb.output
	out["MaxLifeLeechInstance"] = out["Life"] * val(mdb, "MaxLifeLeechInstance") / pct
	out[minionLeechMaxRatePercent] = val(mdb, "MaxLifeLeechRate")
	switch {
	case mdb.Flag(minionLeechParentFlag) && mdb.owner != nil:
		out[minionLeechMaxRatePercent] = mdb.owner.output[minionLeechMaxRatePercent]
	case mdb.Flag(minionLeechPartyMemberFlag) && mdb.owner != nil && mdb.owner.partyMembers != nil:
		out[minionLeechMaxRatePercent] = mdb.owner.partyMembers.output[minionLeechMaxRatePercent]
	}
	out["MaxLifeLeechRate"] = out["Life"] * out[minionLeechMaxRatePercent] / pct
	out["MaxEnergyShieldLeechInstance"] = out["EnergyShield"] * val(mdb, "MaxEnergyShieldLeechInstance") / pct
	out["MaxEnergyShieldLeechRate"] = out["EnergyShield"] * val(mdb, "MaxEnergyShieldLeechRate") / pct
	out["MaxManaLeechInstance"] = out["Mana"] * val(mdb, "MaxManaLeechInstance") / pct
	out["MaxManaLeechRate"] = out["Mana"] * val(mdb, "MaxManaLeechRate") / pct
}
