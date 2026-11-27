package engine

import (
	"math"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/Mi7teR/exile-calc/gamedata"
	"github.com/Mi7teR/exile-calc/internal/boundcache"
	"github.com/Mi7teR/exile-calc/itemtext"
	"github.com/Mi7teR/exile-calc/modparser"
	"github.com/Mi7teR/exile-calc/pobcode"
)

// Calculator holds the game data; it is safe for concurrent use.
type Calculator struct {
	data       *gamedata.Data
	parser     *modparser.Parser
	items      *itemtext.Parser
	luts       *timelessLUTs
	config     configApplies
	mapAffixes mapAffixFuncs
	gemIdx     *gemIndex

	keystoneOnce sync.Once
	keystones    map[string]keystone // PassiveTree.lua keystoneMap

	nodePosOnce sync.Once
	nodePos     map[int]point // nodePositions of the tree

	notableMapsOnce sync.Once
	notable         map[string]*gamedata.Node // PassiveTree.lua notableMap (notableMaps)
	ascendancy      map[string]*gamedata.Node // PassiveTree.lua ascendancyMap

	clusterNotablesOnce sync.Once
	clusterNotables     map[string]*gamedata.Node // clusterNotableMap (clusterNotablesByName)

	// The caches below are bounded (itemCacheCap) and drop what is not used
	// lately, for a long-running consumer that sees arbitrary items.
	grants *boundcache.Cache[itemGrantInfo] // item.grantedSkills by grantKey (grantedskills.go)
	// reqs caches the attribute requirements of jewels by grantKey
	// (requirements.go) and Life flask data (lifeFlaskData).
	reqs *boundcache.Cache[any]
	// merges caches mergeTwoLineMods decisions by line pair: whether a line
	// and the next parse only together.
	merges *boundcache.Cache[bool]
}

// itemCacheCap bounds each of the Calculator's item caches, as
// modparser's parseCacheCap bounds the parse cache.
const itemCacheCap = 10_000

func New() (*Calculator, error) {
	// The two loads are independent: the mod tables load while the game
	// data decodes.
	var (
		p    *modparser.Parser
		pErr error
		wg   sync.WaitGroup
	)
	wg.Go(func() { p, pErr = modparser.PoE1() })
	d, err := gamedata.PoE1()
	wg.Wait()
	if err != nil {
		return nil, err
	}
	if pErr != nil {
		return nil, pErr
	}
	return &Calculator{data: d, parser: p, items: itemtext.New(d), luts: newTimelessLUTs(), gemIdx: newGemIndex(d),
		grants: boundcache.New[itemGrantInfo](itemCacheCap), reqs: boundcache.New[any](itemCacheCap),
		merges: boundcache.New[bool](itemCacheCap)}, nil
}

// Result is the computed output. Unparsed lists mod lines the parser did
// not understand; UnsupportedTags counts mods left out because a tag type
// is not ported. Both make the result incomplete, never silently wrong.
type Result struct {
	Stats           map[string]float64
	DB              *ModDB
	Unparsed        []string
	Incomplete      []string // parts of the build not computed (timeless data missing, ...)
	UnsupportedTags map[string]int
	Skills          []SkillInfo   // the player's active skills, in PoB's order
	Minion          *MinionResult // the main skill's minion (env.minion)
	// main is env.player.mainSkill, nil when there is none.
	main *activeSkill
	// build is the calculated build (Build).
	build *pobcode.Build
	// party is the Party tab export, when the build exports one.
	party *partyExportCache
	// callerGroups maps build's groups to the input build's (the second
	// result of applyGrantedSkills); nil is the identity.
	callerGroups []int
	// compared is calcState.compared, for the Full DPS passes.
	compared int
	// grants is calcState.grants: what the granted skill block set on
	// build's groups, for the Full DPS passes.
	grants []groupGrant
}

// callerGroup is the input build's index of build's group g (both
// 1-based); g itself when there is no mapping or g is out of range.
func (r *Result) callerGroup(g int) int {
	if r.callerGroups == nil || g < 1 || g > len(r.callerGroups) {
		return g
	}
	return r.callerGroups[g-1]
}

// buildGroup is build's index of the input build's group g (as
// SkillInfo.Group), 0 when build has no such group.
func (r *Result) buildGroup(g int) int {
	if r.callerGroups == nil {
		if set := r.build.ActiveSkills(); set == nil || g < 1 || g > len(set.Groups) {
			return 0
		}
		return g
	}
	return slices.Index(r.callerGroups, g) + 1
}

// Build is the calculated build: the input build after CalcSetup.lua's
// granted skill block, which gives every skill granted by the passive
// tree, an item or a Config tab modifier a socket group (appended to the
// active skill set), builds the "Explode" group, and removes saved granted
// groups that nothing grants any more. It is the input build itself when
// nothing changes; otherwise a copy that shares all but the active skill
// set's group list. It must not be modified.
//
// Its group indexes are its own: SkillInfo.Group and SkillDPS.Group index
// the input build instead (see SkillInfo). WithMainSkill takes those.
func (r *Result) Build() *pobcode.Build { return r.build }

// WithMainSkill returns a copy of Build whose main skill is the n-th active
// skill of the input build's socket group g (both 1-based, as
// SkillInfo.Group and GroupSkill), for Calc, FullDPS or NewComparer; nil
// when Build has no such group. Build is not changed. Setting the input
// build's MainSocketGroup to g selects the same group.
func (r *Result) WithMainSkill(g, n int) *pobcode.Build {
	p := r.buildGroup(g)
	if p == 0 {
		return nil
	}
	return withMainSkill(r.build, p, n)
}

// SkillInfo describes one active skill: its effective gem level and
// quality, its buffs ("Aura:Purity of Elements") and the aura effect its
// buffs get on the player (sums over AuraEffect, BuffEffect and the other
// names of CalcPerform.lua's Aura branch).
type SkillInfo struct {
	Name           string
	Level, Quality int
	Disabled       bool
	Buffs          []string
	AuraEffectInc  float64
	AuraEffectMore float64
	// Group and GroupSkill locate the skill: its socket group in the
	// input build's active skill set and its index among the group's
	// active skills (both 1-based). A group the calculation appended (a
	// granted skill the build had no group for) is len(groups)+k for the
	// k-th one, as it would be once added. Setting the input build's
	// MainSocketGroup to Group makes the group the main one.
	Group, GroupSkill int
	HasMinion         bool
}

// Data/Misc.lua characterConstants and Modules/Data.lua data.misc.
const (
	lifePerLevel        = 12
	manaPerLevel        = 6
	baseMaxResist       = 75
	baseEvasion         = 15
	maxResistCap        = 90
	resistFloor         = -200
	defaultAffixQuality = 0.5 // Main.lua defaultItemAffixQuality
)

var resistTypes = []string{"Fire", "Cold", "Lightning", "Chaos"}

func configInput(c *pobcode.ConfigSet, name string) (pobcode.Input, bool) {
	if c == nil {
		return pobcode.Input{}, false
	}
	return c.Input(name)
}

func configString(c *pobcode.ConfigSet, name, def string) string {
	if in, ok := configInput(c, name); ok && in.Kind == pobcode.KindString {
		return in.String
	}
	return def
}

// val is calcLib.val (Modules/CalcTools.lua).
func val(db *ModDB, name string) float64 {
	base := db.Sum("BASE", name)
	if base == 0 {
		return 0
	}
	return base * (1 + db.Sum("INC", name)/100) * db.More(name)
}

// attributes ports calculateAttributes and the attribute bonuses of
// doActorAttribsConditions (Modules/CalcPerform.lua).
func (c *Calculator) attributes(db *ModDB) {
	out := db.output
	for pass := 0; pass < 2; pass++ { // twice: conditions depend on the attributes
		for _, s := range []string{"Str", "Dex", "Int"} {
			out[s] = math.Max(round(val(db, s), 0), 0)
		}
		str, dex, int_ := out["Str"], out["Dex"], out["Int"]
		cond := db.conditions
		stats := []float64{str, dex, int_}
		sort.Float64s(stats)
		out["LowestAttribute"] = stats[0]
		cond["TwoHighestAttributesEqual"] = stats[1] == stats[2]
		cond["IntSingleHighestAttribute"] = int_ > str && int_ > dex
		cond["DexSingleHighestAttribute"] = dex > str && dex > int_
		cond["StrHigherThanInt"] = str > int_
		cond["StrHigherThanDex"] = str > dex
		cond["IntHigherThanStr"] = int_ > str
		cond["DexHigherThanStr"] = dex > str
		cond["IntHigherThanDex"] = int_ > dex
		cond["DexHigherThanInt"] = dex > int_
		cond["StrHighestAttribute"] = str >= dex && str >= int_
		cond["IntHighestAttribute"] = int_ >= str && int_ >= dex
		cond["DexHighestAttribute"] = dex >= str && dex >= int_
	}
	out["TotalAttr"] = out["Str"] + out["Dex"] + out["Int"]
	out["Devotion"] = db.Sum("BASE", "Devotion") // CalcPerform.lua: special case for Devotion
	if db.Flag("NoAttributeBonuses") {
		return
	}
	if !db.Flag("NoStrengthAttributeBonuses") {
		if !db.Flag("NoStrBonusToLife") {
			db.NewMod("Life", "BASE", math.Floor(out["Str"]/2), "Strength")
		}
		bonus := out["Str"] + db.Sum("BASE", "DexIntToMeleeBonus")
		if r := db.Sum("BASE", "StrDmgBonusRatioOverride"); r > 0 {
			db.strDmgBonus = math.Floor(bonus * r)
		} else {
			db.strDmgBonus = math.Floor(bonus / 5)
		}
		db.Add(Mod{Name: "PhysicalDamage", Type: "INC", Value: db.strDmgBonus, Flags: mfMelee, Source: "Strength"})
	}
	if !db.Flag("NoDexterityAttributeBonuses") {
		perDex, ok := db.Override("DexAccBonusOverride")
		if !ok {
			perDex = 2 // data.misc.AccuracyPerDexBase
		}
		db.NewMod("Accuracy", "BASE", out["Dex"]*perDex, "Dexterity")
		if !db.Flag("NoDexBonusToEvasion") {
			db.NewMod("Evasion", "INC", math.Floor(out["Dex"]/5), "Dexterity")
		}
	}
	if !db.Flag("NoIntelligenceAttributeBonuses") {
		if !db.Flag("NoIntBonusToMana") {
			db.NewMod("Mana", "BASE", math.Floor(out["Int"]/2), "Intelligence")
		}
		if !db.Flag("NoIntBonusToES") {
			db.NewMod("EnergyShield", "INC", math.Floor(out["Int"]/10), "Intelligence")
		}
	}
}

// lifeMana ports doActorLifeMana (Modules/CalcPerform.lua).
func (c *Calculator) lifeMana(db *ModDB) {
	out := db.output
	out["LowLifePercentage"] = 100 * lowPoolThreshold
	if v := db.Sum("BASE", "LowLifePercentage"); v > 0 {
		out["LowLifePercentage"] = 100 * v
	}
	out["FullLifePercentage"] = 100
	if v := db.Sum("BASE", "FullLifePercentage"); v > 0 {
		out["FullLifePercentage"] = 100 * v
	}
	if db.Flag("ChaosInoculation") {
		out["ChaosInoculation"] = 1 // CalcPerform.lua:83: a Flag, nil when unset
		out["Life"] = 1
		db.conditions["FullLife"] = true
	} else if o, ok := db.Override("Life"); ok {
		out["Life"] = o
	} else {
		base := db.Sum("BASE", "Life")
		inc := db.Sum("INC", "Life")
		more := db.More("Life")
		conv := db.Sum("BASE", "LifeConvertToEnergyShield")
		out["Life"] = math.Max(round(base*(1+inc/100)*more*(1-conv/100), 0), 1)
	}
	manaConv := db.Sum("BASE", "ManaConvertToArmour")
	out["Mana"] = round(val(db, "Mana")*(1-manaConv/100), 0)
	// CalcPerform.lua:133-134.
	out["LowestOfMaximumLifeAndMaximumMana"] = math.Min(out["Life"], out["Mana"])
	out["MaximumLifeManaDifference"] = math.Abs(out["Life"] - out["Mana"])
}

// resistances ports calcs.resistances (Modules/CalcDefence.lua) without
// resistance conversion and Melding of the Flesh.
func (c *Calculator) resistances(db *ModDB) {
	out := db.output
	out["PhysicalResist"] = 0 // CalcDefence.lua:528
	resistMax := func(elem string) float64 {
		maxNames := []string{elem + "ResistMax"}
		if elem != "Chaos" {
			maxNames = append(maxNames, "ElementalResistMax")
		}
		if max, ok := db.Override(elem + "ResistMax"); ok {
			return max
		}
		return math.Min(maxResistCap, db.Sum("BASE", maxNames...))
	}
	// Resistance conversion: non-base max resistances, then resistances.
	sumNonBase := func(name, typ string) float64 {
		total := 0.0
		for i := range db.mods[name] {
			m := &db.mods[name][i]
			if m.Source == "Base" || !eligible(m, typ, nil) {
				continue
			}
			if v, ok := db.eval(m, nil); ok {
				if f, isNum := num(v); isNum {
					total += f
				}
			}
		}
		return total
	}
	for _, from := range resistTypes {
		for _, to := range resistTypes {
			if rate := db.Sum("BASE", from+"MaxResConvertTo"+to) / 100; rate != 0 {
				if max := sumNonBase(from+"ResistMax", "BASE"); max != 0 {
					db.NewMod(to+"ResistMax", "BASE", max*rate, from+" To "+to+" Max Resistance Conversion")
				}
			}
		}
	}
	for _, from := range resistTypes {
		for _, to := range resistTypes {
			rate := db.Sum("BASE", from+"ResConvertTo"+to) / 100
			if rate == 0 {
				continue
			}
			if res := sumNonBase(from+"Resist", "BASE"); res != 0 {
				db.NewMod(to+"Resist", "BASE", res*rate, from+" To "+to+" Resistance Conversion")
			}
			for _, typ := range []string{"INC", "MORE"} {
				for _, r := range db.tabulateType(from+"Resist", typ) {
					if f, ok := num(r.Value); ok {
						db.NewMod(to+"Resist", typ, f*rate, r.Source)
					}
				}
			}
		}
	}
	// Melding of the Flesh: every elemental maximum becomes the highest one.
	if db.Flag("ElementalResistMaxIsHighestResistMax") {
		highest, highestType := 0.0, ""
		for _, elem := range resistTypes[:3] {
			if m := resistMax(elem); m > highest {
				highest, highestType = m, elem
			}
		}
		for _, elem := range resistTypes[:3] {
			db.NewMod(elem+"ResistMax", "OVERRIDE", highest, highestType+" Melding of the Flesh")
		}
	}
	for _, elem := range resistTypes {
		names := []string{elem + "Resist"}
		if elem != "Chaos" {
			names = append(names, "ElementalResist")
		}
		max := resistMax(elem)
		total, ok := db.Override(elem + "Resist")
		dotTotal := total
		if !ok {
			base := db.Sum("BASE", names...)
			inc := math.Max((1+db.Sum("INC", names...)/100)*db.More(names...), 0)
			total = base * inc
			dotTotal = db.SumCfg(&Cfg{Flags: mfDot}, "BASE", names...) * inc
		}
		total, max, dotTotal = modfInt(total), modfInt(max), modfInt(dotTotal)
		final := math.Max(math.Min(total, max), resistFloor)
		out[elem+"ResistOverTime"] = math.Max(math.Min(dotTotal, max), resistFloor)
		out[elem+"Resist"] = final
		out[elem+"ResistTotal"] = total
		out[elem+"ResistOverCap"] = math.Max(0, total-max)
		out[elem+"ResistOver75"] = math.Max(0, final-75)
		out["Missing"+elem+"Resist"] = math.Max(0, max-final)
		totemResist(db, elem)
	}
}

// customMods ports the Custom Modifiers of ConfigTabClass:BuildModList:
// each line of each enabled block is parsed; fully parsed lines apply with
// the source "Custom:<block title>".
func (c *Calculator) customMods(db *ModDB, res *Result, config *pobcode.ConfigSet) {
	if config == nil {
		return
	}
	for _, block := range config.CustomMods {
		if !block.Enabled {
			continue
		}
		title := block.Title
		if title == "" {
			title = "Default"
		}
		for _, line := range strings.Split(block.Text, "\n") {
			if line = strings.TrimSpace(line); line == "" {
				continue
			}
			r := c.parser.Parse(line)
			if !r.Parsed() {
				res.Unparsed = append(res.Unparsed, line)
				continue
			}
			for _, m := range r.Mods {
				db.Add(fromParsed(m, "Custom:"+title))
			}
		}
	}
}

// PartyExport is the build's Party tab export of one buff type, the text
// PoB saves as an ExportedBuffs element for party members to import
// (Classes/PartyTab.lua:575-655, exportBuffs 996-1043): "PlayerMods"
// (saved as PartyMemberStats), "Aura", "Curse", "Warcry", "Link",
// "EnemyConditions" or "EnemyMods". It is "" unless the build's Party tab
// exports ("Export Support" on the Import tab) and the calculation is in
// PoB's MAIN mode.
func (r *Result) PartyExport(buffType string) string {
	if r == nil || r.party == nil {
		return ""
	}
	s, _ := r.PartyExportErr(buffType)
	return s
}

// PartyExportErr is PartyExport with the error of a failed export pass
// ("" and nil when the build does not export buffs).
func (r *Result) PartyExportErr(buffType string) (string, error) {
	if r == nil || r.party == nil {
		return "", nil
	}
	e, err := r.party.exports()
	if err != nil || e == nil {
		return "", err
	}
	return e.text(buffType), nil
}
