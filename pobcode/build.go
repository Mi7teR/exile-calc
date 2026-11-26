package pobcode

// Build is the part of a PoB build that the calculation engine needs.
// UI state (notes, tree view, calcs tab sections, import links) is dropped.
// Legacy builds without skill/config sets are normalised to one set with ID 1.
type Build struct {
	Level            int
	ClassName        string
	AscendClassName  string
	TargetVersion    string
	MainSocketGroup  int
	Bandit           string
	PantheonMajorGod string
	PantheonMinorGod string

	// SpectreList is the build's spectre library (<Spectre id> under
	// <Build>, Modules/Build.lua): monster ids from Data/Spectres.lua that
	// Raise Spectre can raise, in order.
	SpectreList []string

	// PlayerStats are the stats PoB computed when it saved the build.
	PlayerStats []PlayerStat

	ActiveSpecIndex int // 1-based index into Specs (Tree activeSpec)
	Specs           []Spec

	ActiveItemSetID int
	Items           []Item
	ItemSets        []ItemSet

	ActiveSkillSetID int
	SkillSets        []SkillSet

	ActiveConfigSetID int
	ConfigSets        []ConfigSet

	// Party is the Party tab, nil when the build has no imported party
	// buffs and does not export its own.
	Party *Party

	// CalcsInputs are the Calcs tab's inputs (Classes/CalcsTab.lua
	// Load/Save): skill_number (its socket group), misc_buffMode and the
	// like, which PoB's CALCS mode calculation reads.
	CalcsInputs []Input
}

// Party is the Party tab (Classes/PartyTab.lua Save/Load). PoB keeps no
// list of party members: each member's export is pasted into one text
// block per buff kind, and the tab stores those blocks. The tab's own
// export (<ExportedBuffs>) is output that PoB ignores on load, so it is not
// kept.
type Party struct {
	// Buffs are the <ImportedBuffs> blocks in file order.
	Buffs []PartyBuffs
	// ExportBuffs is the Import tab's "Export Support" checkbox
	// (Classes/ImportTab.lua, <Import exportParty>): the build exports its
	// buffs for the Party tab. It does not change the build's own numbers.
	ExportBuffs bool
}

// PartyBuffs is one <ImportedBuffs> block.
type PartyBuffs struct {
	// Name is the block: PartyMemberStats, Aura, Curse, Warcry Skills,
	// Link Skills, EnemyConditions or EnemyMods.
	Name string
	// Text is the block in PoB's export format, without the surrounding
	// whitespace (runtime/lua/xml.lua trims text nodes).
	Text string
}

type PlayerStat struct {
	Stat  string
	Value float64
}

// Spec is one passive tree (Classes/PassiveSpec.lua).
type Spec struct {
	Title                  string
	TreeVersion            string
	ClassID                int
	AscendClassID          int
	SecondaryAscendClassID int
	Nodes                  []int
	MasteryEffects         []MasteryEffect
	Sockets                []Socket // jewel sockets: tree node -> item
	// ClusterHashFormatVersion is the clusterHashFormatVersion attribute
	// (PassiveSpec.lua Load/Save). A spec saved without it is format 1:
	// its cluster subgraph node ids predate the hash fix and PoB remaps
	// them on load. 0 means not set, a new spec: format 2 (PassiveSpec
	// Init).
	ClusterHashFormatVersion int
}

type MasteryEffect struct {
	Node   int
	Effect int
}

type Socket struct {
	NodeID int
	ItemID int // 0 = empty
}

// Item is one item from the Items tab. Raw is the item text in PoB's
// format (the game's Ctrl+C text plus PoB lines such as "Implicits: N"),
// with indentation and blank lines removed.
type Item struct {
	ID         int
	Variant    int
	VariantAlt [5]int // variantAlt .. variantAlt5
	Raw        string
	ModRanges  []ModRange
}

// ModRange is the roll (0..1) of a ranged mod line, by line number.
type ModRange struct {
	ID    int
	Range float64
}

type ItemSet struct {
	ID                 int
	Title              string
	UseSecondWeaponSet bool
	Slots              []Slot
}

type Slot struct {
	Name   string
	ItemID int // 0 = empty
	Active bool
}

type SkillSet struct {
	ID     int
	Title  string
	Groups []SocketGroup
}

// SocketGroup is one <Skill> element: gems linked in one slot.
type SocketGroup struct {
	Enabled          bool
	IncludeInFullDPS bool
	// GroupCount is groupCount, the group's Full DPS count; nil when not
	// set (0 is a count: the group's hits count zero times).
	GroupCount           *int
	Label                string
	Slot                 string
	Source               string
	MainActiveSkill      int
	MainActiveSkillCalcs int
	Gems                 []Gem
	// NoSupports is group.noSupports as a calculation leaves it on a group
	// it creates (PoB's "On Kill Monster Explosion" group). PoB does not
	// save it, so Decode leaves it false and Code does not write it; it
	// lives on in the calculated build, as on PoB's session group.
	NoSupports bool
}

type Gem struct {
	NameSpec        string
	SkillID         string
	GemID           string
	VariantID       string
	QualityID       string
	Level           int
	Quality         int
	Enabled         bool
	EnableGlobal1   bool
	EnableGlobal2   bool
	Count           int
	SkillPart       int // 0 = not set
	SkillStageCount int // 0 = not set
	SkillMineCount  int // 0 = not set
	SkillMinion     string
	// SkillMinionItemSet is the item set id an Animate Guardian-style
	// minion wears (0 = not set: the first item set).
	SkillMinionItemSet int
	// SkillMinionSkill is the minion's main skill, a 1-based index into
	// its skill list (0 = not set: the first).
	SkillMinionSkill int

	// The Calcs tab's selections (skillPartCalcs ... skillMinionSkillCalcs,
	// Classes/SkillsTab.lua:503-514), read in PoB's CALCS mode; 0 or ""
	// is not set.
	SkillPartCalcs          int
	SkillStageCountCalcs    int
	SkillMineCountCalcs     int
	SkillMinionCalcs        string
	SkillMinionItemSetCalcs int
	SkillMinionSkillCalcs   int
}

type ConfigSet struct {
	ID           int
	Title        string
	Inputs       []Input
	Placeholders []Input
	CustomMods   []CustomModBlock
}

type Kind int

const (
	KindBool Kind = iota + 1
	KindNumber
	KindString
)

// Input is one Config tab value; Kind says which field holds it.
type Input struct {
	Name   string
	Kind   Kind
	Bool   bool
	Number float64
	String string
}

type CustomModBlock struct {
	Title   string
	Enabled bool
	Text    string
}

// Stat returns the first saved player stat with that name.
func (b *Build) Stat(name string) (float64, bool) {
	for _, s := range b.PlayerStats {
		if s.Stat == name {
			return s.Value, true
		}
	}
	return 0, false
}

func (b *Build) Item(id int) *Item {
	for i := range b.Items {
		if b.Items[i].ID == id {
			return &b.Items[i]
		}
	}
	return nil
}

func (b *Build) ActiveSpec() *Spec {
	if i := b.ActiveSpecIndex - 1; i >= 0 && i < len(b.Specs) {
		return &b.Specs[i]
	}
	return nil
}

func (b *Build) ActiveItems() *ItemSet {
	for i := range b.ItemSets {
		if b.ItemSets[i].ID == b.ActiveItemSetID {
			return &b.ItemSets[i]
		}
	}
	return nil
}

func (b *Build) ActiveSkills() *SkillSet {
	for i := range b.SkillSets {
		if b.SkillSets[i].ID == b.ActiveSkillSetID {
			return &b.SkillSets[i]
		}
	}
	return nil
}

func (b *Build) ActiveConfig() *ConfigSet {
	for i := range b.ConfigSets {
		if b.ConfigSets[i].ID == b.ActiveConfigSetID {
			return &b.ConfigSets[i]
		}
	}
	return nil
}

// SlotItem returns the item ID in the named slot, 0 if empty or unknown.
func (s *ItemSet) SlotItem(name string) int {
	for _, sl := range s.Slots {
		if sl.Name == name {
			return sl.ItemID
		}
	}
	return 0
}

func (c *ConfigSet) Input(name string) (Input, bool) {
	for _, in := range c.Inputs {
		if in.Name == name {
			return in, true
		}
	}
	return Input{}, false
}
