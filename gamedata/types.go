// Package gamedata holds Path of Exile game data exported from Path of
// Building Community by cmd/gamedatagen: item bases, mods with tiers, gems,
// uniques and the passive tree. Data (c) Grinding Gear Games.
//
// The `lua` tags name the fields in PoB's src/Data files; they are used only
// by the generator.
package gamedata

// Version says which PoB release and tree the data was generated from.
type Version struct {
	PoB  string `json:"pob"`  // git tag, e.g. "v2.68.1"
	Tree string `json:"tree"` // tree directory, e.g. "3_30"
}

// Base is an item base (Data/Bases/*.lua).
type Base struct {
	Name             string            `json:"name"`
	Type             string            `json:"type" lua:"type"`
	SubType          string            `json:"subType,omitempty" lua:"subType"`
	BaseID           string            `json:"baseId" lua:"baseId"`
	Tags             []string          `json:"tags" lua:"tags,keys"`
	InfluenceTags    map[string]string `json:"influenceTags,omitempty" lua:"influenceTags"`
	Implicit         string            `json:"implicit,omitempty" lua:"implicit"`
	ImplicitModTypes [][]string        `json:"implicitModTypes,omitempty" lua:"implicitModTypes"`
	SocketLimit      int               `json:"socketLimit,omitempty" lua:"socketLimit"`
	Hidden           bool              `json:"hidden,omitempty" lua:"hidden"`
	Req              Requirements      `json:"req" lua:"req"`
	Weapon           *WeaponStats      `json:"weapon,omitempty" lua:"weapon"`
	Armour           *ArmourStats      `json:"armour,omitempty" lua:"armour"`
	Flask            *FlaskStats       `json:"flask,omitempty" lua:"flask"`
	Tincture         *TinctureStats    `json:"tincture,omitempty" lua:"tincture"`
}

type Requirements struct {
	Level int `json:"level,omitempty" lua:"level"`
	Str   int `json:"str,omitempty" lua:"str"`
	Dex   int `json:"dex,omitempty" lua:"dex"`
	Int   int `json:"int,omitempty" lua:"int"`
}

type WeaponStats struct {
	PhysicalMin    float64 `json:"physicalMin" lua:"PhysicalMin"`
	PhysicalMax    float64 `json:"physicalMax" lua:"PhysicalMax"`
	CritChanceBase float64 `json:"critChanceBase" lua:"CritChanceBase"`
	AttackRateBase float64 `json:"attackRateBase" lua:"AttackRateBase"`
	Range          int     `json:"range" lua:"Range"`
}

type ArmourStats struct {
	ArmourBaseMin       float64 `json:"armourBaseMin,omitempty" lua:"ArmourBaseMin"`
	ArmourBaseMax       float64 `json:"armourBaseMax,omitempty" lua:"ArmourBaseMax"`
	EvasionBaseMin      float64 `json:"evasionBaseMin,omitempty" lua:"EvasionBaseMin"`
	EvasionBaseMax      float64 `json:"evasionBaseMax,omitempty" lua:"EvasionBaseMax"`
	EnergyShieldBaseMin float64 `json:"energyShieldBaseMin,omitempty" lua:"EnergyShieldBaseMin"`
	EnergyShieldBaseMax float64 `json:"energyShieldBaseMax,omitempty" lua:"EnergyShieldBaseMax"`
	WardBaseMin         float64 `json:"wardBaseMin,omitempty" lua:"WardBaseMin"`
	WardBaseMax         float64 `json:"wardBaseMax,omitempty" lua:"WardBaseMax"`
	BlockChance         float64 `json:"blockChance,omitempty" lua:"BlockChance"`
	MovementPenalty     float64 `json:"movementPenalty,omitempty" lua:"MovementPenalty"`
}

type FlaskStats struct {
	Life        float64  `json:"life,omitempty" lua:"life"`
	Mana        float64  `json:"mana,omitempty" lua:"mana"`
	Duration    float64  `json:"duration" lua:"duration"`
	ChargesUsed int      `json:"chargesUsed" lua:"chargesUsed"`
	ChargesMax  int      `json:"chargesMax" lua:"chargesMax"`
	Buff        []string `json:"buff,omitempty" lua:"buff"`
}

type TinctureStats struct {
	ManaBurn float64 `json:"manaBurn" lua:"manaBurn"`
	Cooldown float64 `json:"cooldown" lua:"cooldown"`
}

// Mod is one modifier with its tier data (Data/Mod*.lua).
type Mod struct {
	ID     string `json:"id"`
	Domain string `json:"domain"` // source file: "Explicit", "ItemExclusive", "Corrupted", ...
	// Type is "Prefix" or "Suffix" for affixes, empty otherwise.
	Type      string    `json:"type,omitempty" lua:"type"`
	Affix     string    `json:"affix,omitempty" lua:"affix"`
	Lines     []string  `json:"lines" lua:",list"`         // mod text, one entry per stat line
	StatOrder []float64 `json:"statOrder" lua:"statOrder"` // fractional for hybrid lines (418.1)
	Level     int       `json:"level" lua:"level"`
	Group     string    `json:"group" lua:"group"`
	WeightKey []string  `json:"weightKey,omitempty" lua:"weightKey"`
	WeightVal []int     `json:"weightVal,omitempty" lua:"weightVal"`
	ModTags   []string  `json:"modTags,omitempty" lua:"modTags"`
	// TradeHashes maps a trade site stat hash to the mod lines it covers.
	TradeHashes map[uint32][]string `json:"tradeHashes,omitempty" lua:"tradeHashes"`
}

// Gem is a skill or support gem (Data/Gems.lua), keyed by its PoB id.
type Gem struct {
	ID                       string   `json:"id"`
	Name                     string   `json:"name" lua:"name"`
	BaseTypeName             string   `json:"baseTypeName,omitempty" lua:"baseTypeName"`
	GameID                   string   `json:"gameId" lua:"gameId"`
	VariantID                string   `json:"variantId" lua:"variantId"`
	GrantedEffectID          string   `json:"grantedEffectId" lua:"grantedEffectId"`
	SecondaryGrantedEffectID string   `json:"secondaryGrantedEffectId,omitempty" lua:"secondaryGrantedEffectId"`
	SecondaryEffectName      string   `json:"secondaryEffectName,omitempty" lua:"secondaryEffectName"`
	VaalGem                  bool     `json:"vaalGem,omitempty" lua:"vaalGem"`
	Tags                     []string `json:"tags" lua:"tags,keys"`
	TagString                string   `json:"tagString" lua:"tagString"`
	ReqStr                   int      `json:"reqStr" lua:"reqStr"`
	ReqDex                   int      `json:"reqDex" lua:"reqDex"`
	ReqInt                   int      `json:"reqInt" lua:"reqInt"`
	NaturalMaxLevel          int      `json:"naturalMaxLevel" lua:"naturalMaxLevel"`
}

// Unique is a unique item in PoB's item text format (Data/Uniques/*.lua):
// name, base, then PoB lines such as "Variant:", "Implicits: N" and mod
// lines that may carry "{tags:...}" or "{variant:...}" prefixes.
type Unique struct {
	Name string `json:"name"`
	Base string `json:"base"`
	File string `json:"file"` // e.g. "ring"
	Raw  string `json:"raw"`
}

// Tree is the passive skill tree (TreeData/<version>/tree.lua).
type Tree struct {
	Classes []Class `json:"classes" lua:"classes"`
	// Groups[i-1] is the group a node refers to as Group i.
	Groups []Group `json:"groups" lua:"groups"`
	// Nodes by id; Root is PoB's virtual "root" node linking to class starts.
	// The generator fills both (tree.lua mixes the "root" key into nodes).
	Nodes      map[int]*Node `json:"nodes"`
	Root       *Node         `json:"root,omitempty"`
	JewelSlots []int         `json:"jewelSlots" lua:"jewelSlots"`
	Constants  TreeConstants `json:"constants" lua:"constants"`
	Points     TreePoints    `json:"points" lua:"points"`
}

// Class is a character class; its index in Tree.Classes is PoB's classId.
type Class struct {
	Name         string       `json:"name" lua:"name"`
	BaseStr      int          `json:"baseStr" lua:"base_str"`
	BaseDex      int          `json:"baseDex" lua:"base_dex"`
	BaseInt      int          `json:"baseInt" lua:"base_int"`
	Ascendancies []Ascendancy `json:"ascendancies" lua:"ascendancies"`
}

type Ascendancy struct {
	ID   string `json:"id" lua:"id"`
	Name string `json:"name" lua:"name"`
}

type Group struct {
	X       float64 `json:"x" lua:"x"`
	Y       float64 `json:"y" lua:"y"`
	Orbits  []int   `json:"orbits" lua:"orbits"`
	Nodes   []int   `json:"nodes" lua:"nodes"`
	IsProxy bool    `json:"isProxy,omitempty" lua:"isProxy"`
}

// TreeConstants positions nodes on their orbits (PassiveTree.lua ProcessNode).
type TreeConstants struct {
	OrbitRadii     []float64 `json:"orbitRadii" lua:"orbitRadii"`
	SkillsPerOrbit []int     `json:"skillsPerOrbit" lua:"skillsPerOrbit"`
}

type TreePoints struct {
	TotalPoints      int `json:"totalPoints" lua:"totalPoints"`
	AscendancyPoints int `json:"ascendancyPoints" lua:"ascendancyPoints"`
}

type Node struct {
	ID                     int             `json:"id" lua:"skill"`
	Name                   string          `json:"name" lua:"name"`
	Stats                  []string        `json:"stats,omitempty" lua:"stats"`
	ReminderText           []string        `json:"reminderText,omitempty" lua:"reminderText"`
	Group                  int             `json:"group,omitempty" lua:"group"`
	Orbit                  int             `json:"orbit,omitempty" lua:"orbit"`
	OrbitIndex             int             `json:"orbitIndex,omitempty" lua:"orbitIndex"`
	Out                    []int           `json:"out,omitempty" lua:"out"`
	In                     []int           `json:"in,omitempty" lua:"in"`
	AscendancyName         string          `json:"ascendancyName,omitempty" lua:"ascendancyName"`
	IsNotable              bool            `json:"isNotable,omitempty" lua:"isNotable"`
	IsKeystone             bool            `json:"isKeystone,omitempty" lua:"isKeystone"`
	IsMastery              bool            `json:"isMastery,omitempty" lua:"isMastery"`
	IsJewelSocket          bool            `json:"isJewelSocket,omitempty" lua:"isJewelSocket"`
	IsAscendancyStart      bool            `json:"isAscendancyStart,omitempty" lua:"isAscendancyStart"`
	IsBloodline            bool            `json:"isBloodline,omitempty" lua:"isBloodline"`
	IsBlighted             bool            `json:"isBlighted,omitempty" lua:"isBlighted"`
	IsProxy                bool            `json:"isProxy,omitempty" lua:"isProxy"`
	IsMultipleChoice       bool            `json:"isMultipleChoice,omitempty" lua:"isMultipleChoice"`
	IsMultipleChoiceOption bool            `json:"isMultipleChoiceOption,omitempty" lua:"isMultipleChoiceOption"`
	MasteryEffects         []MasteryEffect `json:"masteryEffects,omitempty" lua:"masteryEffects"`
	GrantedStrength        int             `json:"grantedStrength,omitempty" lua:"grantedStrength"`
	GrantedDexterity       int             `json:"grantedDexterity,omitempty" lua:"grantedDexterity"`
	GrantedIntelligence    int             `json:"grantedIntelligence,omitempty" lua:"grantedIntelligence"`
	GrantedPassivePoints   int             `json:"grantedPassivePoints,omitempty" lua:"grantedPassivePoints"`
	ClassStartIndex        *int            `json:"classStartIndex,omitempty" lua:"classStartIndex"`
	ExpansionJewel         *ExpansionJewel `json:"expansionJewel,omitempty" lua:"expansionJewel"`
}

// ExpansionJewel marks a cluster jewel socket: its size (0 small, 1 medium,
// 2 large), its index among sibling sockets, the proxy node whose group
// positions the generated subgraph, and the parent socket.
type ExpansionJewel struct {
	Size   int `json:"size" lua:"size"`
	Index  int `json:"index" lua:"index"`
	Proxy  int `json:"proxy" lua:"proxy"`
	Parent int `json:"parent,omitempty" lua:"parent"`
}

// ClusterJewels is Data/ClusterJewels.lua: how each cluster jewel base lays
// out its subgraph, and the order notables take on it.
// MonsterTables are Data/Misc.lua's per-level monster stats, index 0 =
// level 1.
type MonsterTables struct {
	Evasion             []float64 `json:"evasion"`
	Accuracy            []float64 `json:"accuracy"`
	Damage              []float64 `json:"damage"`
	AllyDamage          []float64 `json:"allyDamage"` // monsterAllyDamageTable
	Armour              []float64 `json:"armour"`
	PhysConversionMulti []float64 `json:"physConversionMulti"`
	AllyLife            []float64 `json:"allyLife"`
	Life                []float64 `json:"life"`  // monsterLifeTable
	Life2               []float64 `json:"life2"` // monsterLifeTable2 (lifeScaling AltLife1)
	Life3               []float64 `json:"life3"` // monsterLifeTable3 (lifeScaling AltLife2)
	// TotemLifeMult is data.totemLifeMult by skillTotemId.
	TotemLifeMult map[int]float64 `json:"totemLifeMult"`
}

// Pantheon is a god of Data/Pantheons.lua; Souls[0] is the god's own soul
// and names the source of every soul's mods.
type Pantheon struct {
	IsMajorGod bool           `json:"isMajorGod,omitempty" lua:"isMajorGod"`
	Souls      []PantheonSoul `json:"souls" lua:"souls"`
}

type PantheonSoul struct {
	Name string        `json:"name" lua:"name"`
	Mods []PantheonMod `json:"mods" lua:"mods"`
}

type PantheonMod struct {
	Line string `json:"line" lua:"line"`
}

type ClusterJewels struct {
	Jewels           map[string]*ClusterJewel `json:"jewels" lua:"jewels"`
	NotableSortOrder map[string]float64       `json:"notableSortOrder" lua:"notableSortOrder"`
}

type ClusterJewel struct {
	Size            string                  `json:"size" lua:"size"`
	SizeIndex       int                     `json:"sizeIndex" lua:"sizeIndex"`
	MinNodes        int                     `json:"minNodes" lua:"minNodes"`
	MaxNodes        int                     `json:"maxNodes" lua:"maxNodes"`
	SmallIndicies   []int                   `json:"smallIndicies" lua:"smallIndicies"`
	NotableIndicies []int                   `json:"notableIndicies" lua:"notableIndicies"`
	SocketIndicies  []int                   `json:"socketIndicies" lua:"socketIndicies"`
	TotalIndicies   int                     `json:"totalIndicies" lua:"totalIndicies"`
	Skills          map[string]ClusterSkill `json:"skills" lua:"skills"`
}

type ClusterSkill struct {
	Name  string   `json:"name" lua:"name"`
	Stats []string `json:"stats" lua:"stats"`
}

type MasteryEffect struct {
	Effect int      `json:"effect" lua:"effect"`
	Stats  []string `json:"stats" lua:"stats"`
}

// Timeless is the timeless jewel data of Data/TimelessJewelData: the
// passives conquered nodes turn into, and how the lookup tables index
// nodes. The lookup tables themselves (*.zip) are large and loaded by the
// engine from a directory at run time.
type Timeless struct {
	Nodes     []LegionNode `json:"nodes"`     // legion.nodes, Lua index i at [i-1]
	Additions []LegionNode `json:"additions"` // legion.additions
	// NodeIndex maps a tree node id to its index in the lookup tables;
	// indices up to SizeNotable are notables (nodeIDList).
	NodeIndex   map[int]int `json:"nodeIndex"`
	SizeNotable int         `json:"sizeNotable"`
	// Size is nodeIDList.size, the number of nodes in the Glorious Vanity
	// table; NodeSize is each node's data length in it.
	Size     int         `json:"size"`
	NodeSize map[int]int `json:"nodeSize"`
	// LocalToGlobal maps, per jewel type, the ids stored in the tables to
	// legion ids (nodeIDList.localIdToGlobalId).
	LocalToGlobal map[int]map[int]int `json:"localToGlobal"`
}

type LegionNode struct {
	ID    string   `json:"id" lua:"id"`
	Name  string   `json:"name" lua:"dn"`
	Stats []string `json:"stats" lua:"sd"`
	// Keystone is set for keystone replacements ("ks").
	Keystone bool `json:"keystone,omitempty" lua:"ks"`
	// StatMods describes the rolled values in Stats, by stat id ("stats"):
	// abyssal timeless jewels store the roll for each index.
	StatMods map[string]LegionStat `json:"statMods,omitempty" lua:"stats"`
	// SortedStats are the stat ids of Stats, in order ("sortedStats").
	SortedStats []string `json:"sortedStats,omitempty" lua:"sortedStats"`
}

type LegionStat struct {
	Fmt   string  `json:"fmt" lua:"fmt"`
	Index int     `json:"index" lua:"index"`
	Min   float64 `json:"min" lua:"min"`
	Max   float64 `json:"max" lua:"max"`
}

// ConfigOption is one entry of Modules/ConfigOptions.lua's varList.
type ConfigOption struct {
	Var  string `json:"var"`
	Type string `json:"type"` // check, count, integer, countAllowZero, float, list, text
	// Apply is the Lua source of apply(val, modList, enemyModList, build);
	// the engine runs it with internal/luadata. Empty when there is none.
	Apply                   string `json:"apply,omitempty"`
	DefaultState            any    `json:"defaultState,omitempty"`
	DefaultPlaceholderState any    `json:"defaultPlaceholderState,omitempty"`
	// DefaultValue is list[defaultIndex].val for a list with a defaultIndex.
	DefaultValue any `json:"defaultValue,omitempty"`
}

// Skills is the skill data of Modules/Data.lua: every granted effect
// (Data/Skills/*.lua) and the shared stat map (Data/SkillStatMap.lua).
type Skills struct {
	Effects map[string]*Skill   `json:"effects"` // by granted effect id
	StatMap map[string]*StatMap `json:"statMap"`
}

// Skill is a granted effect. Mods are encoded like modparser's data:
// {"name", "type", "value", "flags", "keywordFlags", "@list": [tags]}.
type Skill struct {
	Name                     string              `json:"name"`
	Color                    int                 `json:"color,omitempty"`
	Support                  bool                `json:"support,omitempty"`
	SkillTypes               []int               `json:"skillTypes,omitempty"`
	MinionSkillTypes         []int               `json:"minionSkillTypes,omitempty"`
	AddSkillTypes            []int               `json:"addSkillTypes,omitempty"`
	RequireSkillTypes        []int               `json:"requireSkillTypes,omitempty"` // postfix expression
	ExcludeSkillTypes        []int               `json:"excludeSkillTypes,omitempty"`
	IgnoreMinionTypes        bool                `json:"ignoreMinionTypes,omitempty"`
	IsTrigger                bool                `json:"isTrigger,omitempty"`
	AddFlags                 []string            `json:"addFlags,omitempty"`
	PlusVersionOf            string              `json:"plusVersionOf,omitempty"`
	CannotBeSupported        bool                `json:"cannotBeSupported,omitempty"`
	SupportGemsOnly          bool                `json:"supportGemsOnly,omitempty"`
	Unsupported              bool                `json:"unsupported,omitempty"`
	FromItem                 bool                `json:"fromItem,omitempty"`
	WeaponTypes              []string            `json:"weaponTypes,omitempty"`
	BaseFlags                []string            `json:"baseFlags,omitempty"`
	StatMap                  map[string]*StatMap `json:"statMap,omitempty"`
	BaseMods                 []map[string]any    `json:"baseMods,omitempty"`
	QualityStats             []StatValue         `json:"qualityStats,omitempty"`
	Stats                    []string            `json:"stats,omitempty"`
	ConstantStats            []StatValue         `json:"constantStats,omitempty"`
	NotMinionStat            []string            `json:"notMinionStat,omitempty"`
	Levels                   map[int]*SkillLevel `json:"levels,omitempty"`
	BaseEffectiveness        float64             `json:"baseEffectiveness,omitempty"`
	IncrementalEffectiveness float64             `json:"incrementalEffectiveness,omitempty"`
	HasGlobalEffect          bool                `json:"hasGlobalEffect,omitempty"`
	SkillTotemID             int                 `json:"skillTotemId,omitempty"`
	MinionList               []string            `json:"minionList,omitempty"`
	AddMinionList            []string            `json:"addMinionList,omitempty"`
	MinionHasItemSet         bool                `json:"minionHasItemSet,omitempty"`
	CastTime                 float64             `json:"castTime,omitempty"`
	MinionUses               []string            `json:"minionUses,omitempty"` // item slots the minion uses
	Parts                    []SkillPart         `json:"parts,omitempty"`
}

// SkillPart is one of a granted effect's parts: its name and the skill
// flags it sets (true) or clears (false).
type SkillPart struct {
	Name  string          `json:"name"`
	Flags map[string]bool `json:"flags,omitempty"`
}

// StatValue is a {stat, value} pair (qualityStats, constantStats).
type StatValue struct {
	Stat  string  `json:"stat"`
	Value float64 `json:"value"`
}

// StatMap maps one skill stat to mods. Items are mods or groups of mods
// with their own scalars, in PoB's order.
type StatMap struct {
	Items     []StatMapItem `json:"items,omitempty"`
	Value     *float64      `json:"value,omitempty"`
	Mult      *float64      `json:"mult,omitempty"`
	Div       *float64      `json:"div,omitempty"`
	Base      *float64      `json:"base,omitempty"`
	SkillFlag string        `json:"skillFlag,omitempty"`
}

type StatMapItem struct {
	Mod   map[string]any `json:"mod,omitempty"`
	Group *StatMap       `json:"group,omitempty"`
}

// SkillLevel is one entry of a granted effect's levels table: Values are
// its positional stat values, in the order of Skill.Stats.
type SkillLevel struct {
	Values                 []float64 `json:"values,omitempty"`
	StatInterpolation      []int     `json:"statInterpolation,omitempty"`
	LevelRequirement       int       `json:"levelRequirement,omitempty"`
	ManaMultiplier         float64   `json:"manaMultiplier,omitempty"`
	ManaReservationPercent float64   `json:"manaReservationPercent,omitempty"`
	ManaReservationFlat    float64   `json:"manaReservationFlat,omitempty"`
	LifeReservationPercent float64   `json:"lifeReservationPercent,omitempty"`
	LifeReservationFlat    float64   `json:"lifeReservationFlat,omitempty"`
	Cooldown               float64   `json:"cooldown,omitempty"`
	// Offence: base crit chance, base and added damage multipliers, the
	// attack damage multiplier (Damage MORE), attack time and speed.
	CritChance            float64 `json:"critChance,omitempty"`
	BaseMultiplier        float64 `json:"baseMultiplier,omitempty"`
	DamageEffectiveness   float64 `json:"damageEffectiveness,omitempty"`
	DamageMultiplier      float64 `json:"damageMultiplier,omitempty"`
	AttackTime            float64 `json:"attackTime,omitempty"`
	AttackSpeedMultiplier float64 `json:"attackSpeedMultiplier,omitempty"`
	StoredUses            float64 `json:"storedUses,omitempty"`
	// Cost is the level's cost by resource (Data/Costs.lua names).
	Cost map[string]float64 `json:"cost,omitempty"`
}

// Minion is an entry of Data/Minions.lua or Data/Spectres.lua (data.minions);
// ModList is encoded like Skill.BaseMods.
type Minion struct {
	Name            string           `json:"name"`
	Life            float64          `json:"life"`
	LifeScaling     string           `json:"lifeScaling,omitempty"`
	EnergyShield    float64          `json:"energyShield,omitempty"`
	Armour          float64          `json:"armour,omitempty"`
	Evasion         float64          `json:"evasion,omitempty"`
	FireResist      float64          `json:"fireResist,omitempty"`
	ColdResist      float64          `json:"coldResist,omitempty"`
	LightningResist float64          `json:"lightningResist,omitempty"`
	ChaosResist     float64          `json:"chaosResist,omitempty"`
	Limit           string           `json:"limit,omitempty"`
	Hostile         bool             `json:"hostile,omitempty"`
	ModList         []map[string]any `json:"modList,omitempty"`
	// Offence: base damage multiplier and spread, attack time, range and
	// accuracy multiplier, the minion's skills and weapon type.
	Damage                       float64  `json:"damage,omitempty"`
	DamageSpread                 float64  `json:"damageSpread,omitempty"`
	AttackTime                   float64  `json:"attackTime,omitempty"`
	AttackRange                  float64  `json:"attackRange,omitempty"`
	Accuracy                     float64  `json:"accuracy,omitempty"`
	WeaponType1                  string   `json:"weaponType1,omitempty"`
	BaseDamageIgnoresAttackSpeed bool     `json:"baseDamageIgnoresAttackSpeed,omitempty"`
	SkillList                    []string `json:"skillList,omitempty"`
}
