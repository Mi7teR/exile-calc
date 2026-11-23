package engine

import (
	"math"

	"github.com/Mi7teR/poe-lib/gamedata"
)

// perStat is a getPerStat jewel function (Modules/ModParser.lua) of the
// second pass: it sums a stat over the nodes in radius (allocated ones for
// jewelSelfFuncs, unallocated ones for jewelSelfUnallocFuncs) and the
// finaliser adds floor(sum * factor) of dst.
type perStat struct {
	dst, typ, stat string
	flags          uint64
	factor         float64
	unalloc        bool
}

const (
	mfClaw    = 0x40000
	mfUnarmed = 0x1000000
)

// selfFuncs are jewelSelfFuncs and jewelSelfUnallocFuncs built on
// getPerStat, by lower-cased line.
var selfFuncs = map[string]perStat{
	"adds 1 to maximum life per 3 intelligence in radius":                                                    {"Life", "BASE", "Int", 0, 1.0 / 3, false},
	"adds 1 to maximum life per 3 intelligence allocated in radius":                                          {"Life", "BASE", "Int", 0, 1.0 / 3, false},
	"1% increased evasion rating per 3 dexterity allocated in radius":                                        {"Evasion", "INC", "Dex", 0, 1.0 / 3, false},
	"1% increased claw physical damage per 3 dexterity allocated in radius":                                  {"PhysicalDamage", "INC", "Dex", mfClaw, 1.0 / 3, false},
	"1% increased melee physical damage while unarmed per 3 dexterity allocated in radius":                   {"PhysicalDamage", "INC", "Dex", mfUnarmed, 1.0 / 3, false},
	"3% increased totem life per 10 strength in radius":                                                      {"TotemLife", "INC", "Str", 0, 3.0 / 10, false},
	"3% increased totem life per 10 strength allocated in radius":                                            {"TotemLife", "INC", "Str", 0, 3.0 / 10, false},
	"adds 1 maximum lightning damage to attacks per 1 dexterity allocated in radius":                         {"LightningMax", "BASE", "Dex", mfAttack, 1, false},
	"5% increased chaos damage per 10 intelligence from allocated passives in radius":                        {"ChaosDamage", "INC", "Int", 0, 5.0 / 10, false},
	"-1 strength per 1 strength on allocated passives in radius":                                             {"Str", "BASE", "Str", 0, -1, false},
	"1% additional physical damage reduction per 10 strength on allocated passives in radius":                {"PhysicalDamageReduction", "BASE", "Str", 0, 1.0 / 10, false},
	"2% increased life recovery rate per 10 strength on allocated passives in radius":                        {"LifeRecoveryRate", "INC", "Str", 0, 2.0 / 10, false},
	"3% increased life recovery rate per 10 strength on allocated passives in radius":                        {"LifeRecoveryRate", "INC", "Str", 0, 3.0 / 10, false},
	"-1 intelligence per 1 intelligence on allocated passives in radius":                                     {"Int", "BASE", "Int", 0, -1, false},
	"0.4% of energy shield regenerated per second for every 10 intelligence on allocated passives in radius": {"EnergyShieldRegenPercent", "BASE", "Int", 0, 0.4 / 10, false},
	"regenerate 0.4% of energy shield per second for every 10 intelligence on allocated passives in radius":  {"EnergyShieldRegenPercent", "BASE", "Int", 0, 0.4 / 10, false},
	"2% increased mana recovery rate per 10 intelligence on allocated passives in radius":                    {"ManaRecoveryRate", "INC", "Int", 0, 2.0 / 10, false},
	"3% increased mana recovery rate per 10 intelligence on allocated passives in radius":                    {"ManaRecoveryRate", "INC", "Int", 0, 3.0 / 10, false},
	"-1 dexterity per 1 dexterity on allocated passives in radius":                                           {"Dex", "BASE", "Dex", 0, -1, false},
	"2% increased movement speed per 10 dexterity on allocated passives in radius":                           {"MovementSpeed", "INC", "Dex", 0, 2.0 / 10, false},
	"3% increased movement speed per 10 dexterity on allocated passives in radius":                           {"MovementSpeed", "INC", "Dex", 0, 3.0 / 10, false},

	"+5% to critical strike multiplier per 10 strength on unallocated passives in radius":      {"CritMultiplier", "BASE", "Str", 0, 5.0 / 10, true},
	"+7% to critical strike multiplier per 10 strength on unallocated passives in radius":      {"CritMultiplier", "BASE", "Str", 0, 7.0 / 10, true},
	"2% reduced life recovery rate per 10 strength on unallocated passives in radius":          {"LifeRecoveryRate", "INC", "Str", 0, -2.0 / 10, true},
	"+15 to maximum mana per 10 dexterity on unallocated passives in radius":                   {"Mana", "BASE", "Dex", 0, 15.0 / 10, true},
	"+100 to accuracy rating per 10 intelligence on unallocated passives in radius":            {"Accuracy", "BASE", "Int", 0, 100.0 / 10, true},
	"+125 to accuracy rating per 10 intelligence on unallocated passives in radius":            {"Accuracy", "BASE", "Int", 0, 125.0 / 10, true},
	"2% reduced mana recovery rate per 10 intelligence on unallocated passives in radius":      {"ManaRecoveryRate", "INC", "Int", 0, -2.0 / 10, true},
	"+3% to damage over time multiplier per 10 intelligence on unallocated passives in radius": {"DotMultiplier", "BASE", "Int", 0, 3.0 / 10, true},
	"2% reduced movement speed per 10 dexterity on unallocated passives in radius":             {"MovementSpeed", "INC", "Dex", 0, -2.0 / 10, true},
	"+125 to accuracy rating per 10 dexterity on unallocated passives in radius":               {"Accuracy", "BASE", "Dex", 0, 125.0 / 10, true},
}

// secondPassFunc runs on a node's final (scaled) mods in the second pass.
type secondPassFunc struct {
	unalloc bool // SelfUnalloc: unallocated nodes; Self: allocated ones
	f       func(node *gamedata.Node, mods []Mod)
}

// newPerStat returns the second-pass function and its finaliser.
func newPerStat(p perStat, source string) (secondPassFunc, func() []Mod) {
	sum := 0.0
	f := func(_ *gamedata.Node, mods []Mod) {
		node := NewModDB() // out:Sum("BASE", nil, stat) over the node's list
		for _, m := range mods {
			node.Add(m)
		}
		sum += node.Sum("BASE", p.stat)
	}
	final := func() []Mod {
		if sum == 0 {
			return nil
		}
		return []Mod{{Name: p.dst, Type: p.typ, Value: math.Floor(sum * p.factor), Flags: p.flags, Source: source}}
	}
	return secondPassFunc{unalloc: p.unalloc, f: f}, final
}
