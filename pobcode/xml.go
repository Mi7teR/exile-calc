package pobcode

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Wire format of the build XML. Element and attribute names follow the
// Save/Load functions in PoB: Modules/Build.lua, Classes/PassiveSpec.lua,
// Classes/ItemsTab.lua, Classes/SkillsTab.lua, Classes/ConfigTab.lua.

type xPoB struct {
	XMLName xml.Name `xml:"PathOfBuilding"`
	Build   xBuild   `xml:"Build"`
	Tree    xTree    `xml:"Tree"`
	Items   xItems   `xml:"Items"`
	Skills  xSkills  `xml:"Skills"`
	Config  xConfig  `xml:"Config"`
	Import  *xImport `xml:"Import"`
	Party   *xParty  `xml:"Party"`
	Calcs   *xCalcs  `xml:"Calcs"`
}

// xCalcs is Classes/CalcsTab.lua Save; only the inputs are kept.
type xCalcs struct {
	Inputs []xInput `xml:"Input"`
}

// xImport is Classes/ImportTab.lua Save; only exportParty is kept.
type xImport struct {
	ExportParty string `xml:"exportParty,attr,omitempty"`
}

// xParty is Classes/PartyTab.lua Save; only the imported buffs are kept.
type xParty struct {
	Imported []xPartyBuffs `xml:"ImportedBuffs"`
}

type xPartyBuffs struct {
	Name string `xml:"name,attr"`
	Text string `xml:",chardata"`
}

type xBuild struct {
	Level            string `xml:"level,attr"`
	TargetVersion    string `xml:"targetVersion,attr,omitempty"`
	ClassName        string `xml:"className,attr"`
	AscendClassName  string `xml:"ascendClassName,attr"`
	MainSocketGroup  string `xml:"mainSocketGroup,attr"`
	Bandit           string `xml:"bandit,attr,omitempty"`
	PantheonMajorGod string `xml:"pantheonMajorGod,attr,omitempty"`
	PantheonMinorGod string `xml:"pantheonMinorGod,attr,omitempty"`
	// Spectres come before PlayerStat, as Build.lua Save writes them.
	Spectres    []xSpectre `xml:"Spectre"`
	PlayerStats []xStat    `xml:"PlayerStat"`
}

// xSpectre is a <Spectre id="..."/> of Modules/Build.lua Save/Load.
type xSpectre struct {
	ID string `xml:"id,attr"`
}

type xStat struct {
	Stat  string `xml:"stat,attr"`
	Value string `xml:"value,attr"`
}

type xTree struct {
	ActiveSpec string  `xml:"activeSpec,attr"`
	Specs      []xSpec `xml:"Spec"`
}

type xSpec struct {
	Title                  string    `xml:"title,attr,omitempty"`
	TreeVersion            string    `xml:"treeVersion,attr"`
	ClassID                string    `xml:"classId,attr"`
	AscendClassID          string    `xml:"ascendClassId,attr"`
	SecondaryAscendClassID string    `xml:"secondaryAscendClassId,attr,omitempty"`
	Nodes                  string    `xml:"nodes,attr"`
	MasteryEffects         string    `xml:"masteryEffects,attr"`
	ClusterHashFormat      string    `xml:"clusterHashFormatVersion,attr,omitempty"`
	Sockets                []xSocket `xml:"Sockets>Socket"`
}

type xSocket struct {
	NodeID string `xml:"nodeId,attr"`
	ItemID string `xml:"itemId,attr"`
}

type xItems struct {
	ActiveItemSet string     `xml:"activeItemSet,attr"`
	Items         []xItem    `xml:"Item"`
	ItemSets      []xItemSet `xml:"ItemSet"`
	Slots         []xSlot    `xml:"Slot"` // very old builds: slots without an item set
}

type xItem struct {
	ID          string      `xml:"id,attr"`
	Variant     string      `xml:"variant,attr,omitempty"`
	VariantAlt  string      `xml:"variantAlt,attr,omitempty"`
	VariantAlt2 string      `xml:"variantAlt2,attr,omitempty"`
	VariantAlt3 string      `xml:"variantAlt3,attr,omitempty"`
	VariantAlt4 string      `xml:"variantAlt4,attr,omitempty"`
	VariantAlt5 string      `xml:"variantAlt5,attr,omitempty"`
	Text        string      `xml:",chardata"`
	ModRanges   []xModRange `xml:"ModRange"`
}

type xModRange struct {
	ID    string `xml:"id,attr"`
	Range string `xml:"range,attr"`
}

type xItemSet struct {
	ID                 string  `xml:"id,attr"`
	Title              string  `xml:"title,attr,omitempty"`
	UseSecondWeaponSet string  `xml:"useSecondWeaponSet,attr"`
	Slots              []xSlot `xml:"Slot"`
}

type xSlot struct {
	Name   string `xml:"name,attr"`
	ItemID string `xml:"itemId,attr"`
	Active string `xml:"active,attr,omitempty"`
}

type xSkills struct {
	ActiveSkillSet string      `xml:"activeSkillSet,attr,omitempty"`
	SkillSets      []xSkillSet `xml:"SkillSet"`
	Skills         []xSkill    `xml:"Skill"` // legacy: no skill sets
}

type xSkillSet struct {
	ID     string   `xml:"id,attr"`
	Title  string   `xml:"title,attr,omitempty"`
	Skills []xSkill `xml:"Skill"`
}

type xSkill struct {
	Enabled              string `xml:"enabled,attr"`
	Active               string `xml:"active,attr,omitempty"` // legacy spelling of enabled
	IncludeInFullDPS     string `xml:"includeInFullDPS,attr,omitempty"`
	GroupCount           string `xml:"groupCount,attr,omitempty"`
	Label                string `xml:"label,attr,omitempty"`
	Slot                 string `xml:"slot,attr,omitempty"`
	Source               string `xml:"source,attr,omitempty"`
	MainActiveSkill      string `xml:"mainActiveSkill,attr,omitempty"`
	MainActiveSkillCalcs string `xml:"mainActiveSkillCalcs,attr,omitempty"`
	Gems                 []xGem `xml:"Gem"`
}

type xGem struct {
	NameSpec           string `xml:"nameSpec,attr"`
	SkillID            string `xml:"skillId,attr,omitempty"`
	GemID              string `xml:"gemId,attr,omitempty"`
	VariantID          string `xml:"variantId,attr,omitempty"`
	QualityID          string `xml:"qualityId,attr,omitempty"`
	Level              string `xml:"level,attr"`
	Quality            string `xml:"quality,attr"`
	Enabled            string `xml:"enabled,attr,omitempty"`
	EnableGlobal1      string `xml:"enableGlobal1,attr,omitempty"`
	EnableGlobal2      string `xml:"enableGlobal2,attr,omitempty"`
	Count              string `xml:"count,attr,omitempty"`
	SkillPart          string `xml:"skillPart,attr,omitempty"`
	SkillStageCount    string `xml:"skillStageCount,attr,omitempty"`
	SkillMineCount     string `xml:"skillMineCount,attr,omitempty"`
	SkillMinion        string `xml:"skillMinion,attr,omitempty"`
	SkillMinionItemSet string `xml:"skillMinionItemSet,attr,omitempty"`
	SkillMinionSkill   string `xml:"skillMinionSkill,attr,omitempty"`

	SkillPartCalcs          string `xml:"skillPartCalcs,attr,omitempty"`
	SkillStageCountCalcs    string `xml:"skillStageCountCalcs,attr,omitempty"`
	SkillMineCountCalcs     string `xml:"skillMineCountCalcs,attr,omitempty"`
	SkillMinionCalcs        string `xml:"skillMinionCalcs,attr,omitempty"`
	SkillMinionItemSetCalcs string `xml:"skillMinionItemSetCalcs,attr,omitempty"`
	SkillMinionSkillCalcs   string `xml:"skillMinionSkillCalcs,attr,omitempty"`
}

type xConfig struct {
	ActiveConfigSet string       `xml:"activeConfigSet,attr,omitempty"`
	ConfigSets      []xConfigSet `xml:"ConfigSet"`
	Inputs          []xInput     `xml:"Input"`       // legacy: no config sets
	Placeholders    []xInput     `xml:"Placeholder"` // legacy
}

type xConfigSet struct {
	ID           string      `xml:"id,attr"`
	Title        string      `xml:"title,attr,omitempty"`
	Inputs       []xInput    `xml:"Input"`
	Placeholders []xInput    `xml:"Placeholder"`
	CustomMods   []xCustomMB `xml:"CustomModifierBlock"`
}

type xInput struct {
	Name    string  `xml:"name,attr"`
	Number  *string `xml:"number,attr"`
	Boolean *string `xml:"boolean,attr"`
	String  *string `xml:"string,attr"`
}

type xCustomMB struct {
	Title   string `xml:"title,attr"`
	Enabled string `xml:"enabled,attr"`
	Text    string `xml:",chardata"`
}

// parser keeps the first conversion error so the mapping code stays flat.
type parser struct{ err error }

func (p *parser) int(field, s string, def int) int {
	if s == "" || s == "nil" {
		return def
	}
	v, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		// PoB stores some counts as floats ("20.0"); accept whole numbers.
		f, ferr := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if ferr == nil && f == float64(int(f)) {
			return int(f)
		}
		if p.err == nil {
			p.err = fmt.Errorf("pobcode: %s: %q is not an integer", field, s)
		}
		return def
	}
	return v
}

// intPtr is tonumber(attr) for an optional count: nil when absent or
// "nil", so a saved 0 stays set.
func (p *parser) intPtr(field, s string) *int {
	if s == "" || s == "nil" {
		return nil
	}
	v := p.int(field, s, 0)
	return &v
}

func (p *parser) float(field, s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil && p.err == nil {
		p.err = fmt.Errorf("pobcode: %s: %q is not a number", field, s)
	}
	return v
}

func (p *parser) intList(field, s string) []int {
	var out []int
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, p.int(field, part, 0))
		}
	}
	return out
}

func boolAttr(s string, def bool) bool {
	if s == "" {
		return def
	}
	return s == "true"
}

var masteryRe = regexp.MustCompile(`\{(\d+),(\d+)\}`)

// NormaliseItemText is the form of Item.Raw: item text without
// indentation, surrounding spaces or blank lines. Parse stores items so;
// an item added to a Build by hand should be normalised the same way.
func NormaliseItemText(s string) string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return strings.Join(lines, "\n")
}

// Parse reads build XML as saved by PoB (any version that has <PathOfBuilding>).
func Parse(data []byte) (*Build, error) {
	var x xPoB
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("pobcode: xml: %w", err)
	}
	p := &parser{}
	b := &Build{
		Level:            p.int("Build.level", x.Build.Level, 1),
		ClassName:        x.Build.ClassName,
		AscendClassName:  x.Build.AscendClassName,
		TargetVersion:    x.Build.TargetVersion,
		MainSocketGroup:  p.int("Build.mainSocketGroup", x.Build.MainSocketGroup, 1),
		Bandit:           x.Build.Bandit,
		PantheonMajorGod: x.Build.PantheonMajorGod,
		PantheonMinorGod: x.Build.PantheonMinorGod,
	}
	// Build.lua Load keeps only ids in data.minions; pobcode has no game
	// data, so every id is kept and the engine skips unknown ones.
	for _, sp := range x.Build.Spectres {
		if sp.ID != "" {
			b.SpectreList = append(b.SpectreList, sp.ID)
		}
	}
	for _, s := range x.Build.PlayerStats {
		// PoB sometimes saves a Lua table reference ("table: 0x...") as a value;
		// such stats carry no number and are skipped.
		v, err := strconv.ParseFloat(strings.TrimSpace(s.Value), 64)
		if err != nil {
			continue
		}
		b.PlayerStats = append(b.PlayerStats, PlayerStat{Stat: s.Stat, Value: v})
	}

	b.ActiveSpecIndex = p.int("Tree.activeSpec", x.Tree.ActiveSpec, 1)
	for _, s := range x.Tree.Specs {
		spec := Spec{
			Title:                  s.Title,
			TreeVersion:            s.TreeVersion,
			ClassID:                p.int("Spec.classId", s.ClassID, 0),
			AscendClassID:          p.int("Spec.ascendClassId", s.AscendClassID, 0),
			SecondaryAscendClassID: p.int("Spec.secondaryAscendClassId", s.SecondaryAscendClassID, 0),
			Nodes:                  p.intList("Spec.nodes", s.Nodes),
			// PassiveSpec.lua:106 Load: tonumber(attr) or (xml.attrib.nodes and 1 or 2).
			// Save always writes nodes; a URL-format spec (no nodes attribute, 2)
			// cannot reach here, since pobcode reads only the nodes list.
			// So an absent attribute is 1.
			ClusterHashFormatVersion: p.int("Spec.clusterHashFormatVersion", s.ClusterHashFormat, 1),
		}
		for _, m := range masteryRe.FindAllStringSubmatch(s.MasteryEffects, -1) {
			spec.MasteryEffects = append(spec.MasteryEffects, MasteryEffect{
				Node: p.int("Spec.masteryEffects", m[1], 0), Effect: p.int("Spec.masteryEffects", m[2], 0),
			})
		}
		for _, so := range s.Sockets {
			spec.Sockets = append(spec.Sockets, Socket{
				NodeID: p.int("Socket.nodeId", so.NodeID, 0), ItemID: p.int("Socket.itemId", so.ItemID, 0),
			})
		}
		b.Specs = append(b.Specs, spec)
	}

	b.ActiveItemSetID = p.int("Items.activeItemSet", x.Items.ActiveItemSet, 1)
	for _, it := range x.Items.Items {
		item := Item{
			ID:      p.int("Item.id", it.ID, 0),
			Variant: p.int("Item.variant", it.Variant, 0),
			Raw:     NormaliseItemText(it.Text),
		}
		for i, v := range []string{it.VariantAlt, it.VariantAlt2, it.VariantAlt3, it.VariantAlt4, it.VariantAlt5} {
			item.VariantAlt[i] = p.int("Item.variantAlt", v, 0)
		}
		for _, mr := range it.ModRanges {
			item.ModRanges = append(item.ModRanges, ModRange{
				ID: p.int("ModRange.id", mr.ID, 0), Range: p.float("ModRange.range", mr.Range),
			})
		}
		b.Items = append(b.Items, item)
	}
	sets := x.Items.ItemSets
	if len(sets) == 0 && len(x.Items.Slots) > 0 {
		sets = []xItemSet{{ID: "1", Slots: x.Items.Slots}}
	}
	for _, s := range sets {
		set := ItemSet{
			ID:                 p.int("ItemSet.id", s.ID, 1),
			Title:              s.Title,
			UseSecondWeaponSet: boolAttr(s.UseSecondWeaponSet, false),
		}
		for _, sl := range s.Slots {
			set.Slots = append(set.Slots, Slot{
				Name: sl.Name, ItemID: p.int("Slot.itemId", sl.ItemID, 0), Active: boolAttr(sl.Active, false),
			})
		}
		b.ItemSets = append(b.ItemSets, set)
	}

	b.ActiveSkillSetID = p.int("Skills.activeSkillSet", x.Skills.ActiveSkillSet, 1)
	skillSets := x.Skills.SkillSets
	if len(skillSets) == 0 && len(x.Skills.Skills) > 0 {
		skillSets = []xSkillSet{{ID: "1", Skills: x.Skills.Skills}}
	}
	for _, s := range skillSets {
		set := SkillSet{ID: p.int("SkillSet.id", s.ID, 1), Title: s.Title}
		for _, sk := range s.Skills {
			// SkillsTab.lua: enabled = active == "true" or enabled == "true"
			g := SocketGroup{
				Enabled:              sk.Active == "true" || sk.Enabled == "true",
				IncludeInFullDPS:     sk.IncludeInFullDPS == "true",
				GroupCount:           p.intPtr("Skill.groupCount", sk.GroupCount),
				Label:                sk.Label,
				Slot:                 sk.Slot,
				Source:               sk.Source,
				MainActiveSkill:      p.int("Skill.mainActiveSkill", sk.MainActiveSkill, 1),
				MainActiveSkillCalcs: p.int("Skill.mainActiveSkillCalcs", sk.MainActiveSkillCalcs, 1),
			}
			for _, gm := range sk.Gems {
				// SkillsTab.lua: enabled and enableGlobal1 default to true, count to 1.
				g.Gems = append(g.Gems, Gem{
					NameSpec:           gm.NameSpec,
					SkillID:            gm.SkillID,
					GemID:              gm.GemID,
					VariantID:          gm.VariantID,
					QualityID:          gm.QualityID,
					Level:              p.int("Gem.level", gm.Level, 0),
					Quality:            p.int("Gem.quality", gm.Quality, 0),
					Enabled:            boolAttr(gm.Enabled, true),
					EnableGlobal1:      boolAttr(gm.EnableGlobal1, true),
					EnableGlobal2:      boolAttr(gm.EnableGlobal2, false),
					Count:              p.int("Gem.count", gm.Count, 1),
					SkillPart:          p.int("Gem.skillPart", gm.SkillPart, 0),
					SkillStageCount:    p.int("Gem.skillStageCount", gm.SkillStageCount, 0),
					SkillMineCount:     p.int("Gem.skillMineCount", gm.SkillMineCount, 0),
					SkillMinion:        gm.SkillMinion,
					SkillMinionItemSet: p.int("Gem.skillMinionItemSet", gm.SkillMinionItemSet, 0),
					SkillMinionSkill:   p.int("Gem.skillMinionSkill", gm.SkillMinionSkill, 0),

					SkillPartCalcs:          p.int("Gem.skillPartCalcs", gm.SkillPartCalcs, 0),
					SkillStageCountCalcs:    p.int("Gem.skillStageCountCalcs", gm.SkillStageCountCalcs, 0),
					SkillMineCountCalcs:     p.int("Gem.skillMineCountCalcs", gm.SkillMineCountCalcs, 0),
					SkillMinionCalcs:        gm.SkillMinionCalcs,
					SkillMinionItemSetCalcs: p.int("Gem.skillMinionItemSetCalcs", gm.SkillMinionItemSetCalcs, 0),
					SkillMinionSkillCalcs:   p.int("Gem.skillMinionSkillCalcs", gm.SkillMinionSkillCalcs, 0),
				})
			}
			set.Groups = append(set.Groups, g)
		}
		b.SkillSets = append(b.SkillSets, set)
	}

	b.ActiveConfigSetID = p.int("Config.activeConfigSet", x.Config.ActiveConfigSet, 1)
	configSets := x.Config.ConfigSets
	if len(configSets) == 0 && (len(x.Config.Inputs) > 0 || len(x.Config.Placeholders) > 0) {
		configSets = []xConfigSet{{ID: "1", Inputs: x.Config.Inputs, Placeholders: x.Config.Placeholders}}
	}
	for _, c := range configSets {
		set := ConfigSet{ID: p.int("ConfigSet.id", c.ID, 1), Title: c.Title}
		for _, in := range c.Inputs {
			set.Inputs = append(set.Inputs, p.input(in))
		}
		for _, in := range c.Placeholders {
			set.Placeholders = append(set.Placeholders, p.input(in))
		}
		for _, cm := range c.CustomMods {
			set.CustomMods = append(set.CustomMods, CustomModBlock{
				Title: cm.Title, Enabled: boolAttr(cm.Enabled, true), Text: strings.TrimSpace(cm.Text),
			})
		}
		// ConfigTab.lua "Migration check for legacy builds": the old
		// customMods input becomes the Default block when there is no block
		// text, and the input is dropped either way.
		legacy := ""
		inputs := set.Inputs[:0]
		for _, in := range set.Inputs {
			if in.Name == "customMods" {
				legacy = in.String
				continue
			}
			inputs = append(inputs, in)
		}
		set.Inputs = inputs
		if legacy != "" && (len(set.CustomMods) == 0 || (len(set.CustomMods) == 1 && set.CustomMods[0].Text == "")) {
			set.CustomMods = []CustomModBlock{{Title: "Default", Enabled: true, Text: legacy}}
		}
		b.ConfigSets = append(b.ConfigSets, set)
	}

	b.Party = parseParty(&x)
	if x.Calcs != nil {
		for _, in := range x.Calcs.Inputs {
			b.CalcsInputs = append(b.CalcsInputs, p.input(in))
		}
	}

	if p.err != nil {
		return nil, p.err
	}
	return b, nil
}

// parseParty is PartyTabClass:Load and ImportTabClass:Load: blocks without
// a name are skipped, as PoB does. A build with no blocks and no export is
// a nil Party.
func parseParty(x *xPoB) *Party {
	party := &Party{ExportBuffs: x.Import != nil && x.Import.ExportParty == "true"}
	if x.Party != nil {
		for _, ib := range x.Party.Imported {
			if ib.Name == "" {
				continue
			}
			party.Buffs = append(party.Buffs, PartyBuffs{Name: ib.Name, Text: strings.TrimSpace(ib.Text)})
		}
	}
	if len(party.Buffs) == 0 && !party.ExportBuffs {
		return nil
	}
	return party
}

func (p *parser) input(in xInput) Input {
	out := Input{Name: in.Name}
	switch {
	case in.Number != nil:
		out.Kind, out.Number = KindNumber, p.float("Input."+in.Name, *in.Number)
	case in.Boolean != nil:
		out.Kind, out.Bool = KindBool, *in.Boolean == "true"
	case in.String != nil:
		out.Kind, out.String = KindString, *in.String
	}
	return out
}

func itoa(v int) string { return strconv.Itoa(v) }

func itoaPtr(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func itoaOpt(v int) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(v)
}

func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func btoa(v bool) string { return strconv.FormatBool(v) }

// XML serialises the build in the current PoB format (skill and config sets).
func (b *Build) XML() ([]byte, error) {
	x := xPoB{
		Build: xBuild{
			Level:            itoa(b.Level),
			TargetVersion:    b.TargetVersion,
			ClassName:        b.ClassName,
			AscendClassName:  b.AscendClassName,
			MainSocketGroup:  itoa(b.MainSocketGroup),
			Bandit:           b.Bandit,
			PantheonMajorGod: b.PantheonMajorGod,
			PantheonMinorGod: b.PantheonMinorGod,
		},
		Tree:    xTree{ActiveSpec: itoa(b.ActiveSpecIndex)},
		Items:   xItems{ActiveItemSet: itoa(b.ActiveItemSetID)},
		Skills:  xSkills{ActiveSkillSet: itoa(b.ActiveSkillSetID)},
		Config:  xConfig{ActiveConfigSet: itoa(b.ActiveConfigSetID)},
		XMLName: xml.Name{Local: "PathOfBuilding"},
	}
	for _, id := range b.SpectreList {
		x.Build.Spectres = append(x.Build.Spectres, xSpectre{ID: id})
	}
	for _, s := range b.PlayerStats {
		x.Build.PlayerStats = append(x.Build.PlayerStats, xStat{Stat: s.Stat, Value: ftoa(s.Value)})
	}
	for _, s := range b.Specs {
		nodes := make([]string, len(s.Nodes))
		for i, n := range s.Nodes {
			nodes[i] = itoa(n)
		}
		mastery := make([]string, len(s.MasteryEffects))
		for i, m := range s.MasteryEffects {
			mastery[i] = "{" + itoa(m.Node) + "," + itoa(m.Effect) + "}"
		}
		xs := xSpec{
			Title:                  s.Title,
			TreeVersion:            s.TreeVersion,
			ClassID:                itoa(s.ClassID),
			AscendClassID:          itoa(s.AscendClassID),
			SecondaryAscendClassID: itoaOpt(s.SecondaryAscendClassID),
			Nodes:                  strings.Join(nodes, ","),
			MasteryEffects:         strings.Join(mastery, ","),
			ClusterHashFormat:      clusterHashFormatAttr(s.ClusterHashFormatVersion),
		}
		for _, so := range s.Sockets {
			xs.Sockets = append(xs.Sockets, xSocket{NodeID: itoa(so.NodeID), ItemID: itoa(so.ItemID)})
		}
		x.Tree.Specs = append(x.Tree.Specs, xs)
	}
	for _, it := range b.Items {
		xi := xItem{
			ID:          itoa(it.ID),
			Variant:     itoaOpt(it.Variant),
			VariantAlt:  itoaOpt(it.VariantAlt[0]),
			VariantAlt2: itoaOpt(it.VariantAlt[1]),
			VariantAlt3: itoaOpt(it.VariantAlt[2]),
			VariantAlt4: itoaOpt(it.VariantAlt[3]),
			VariantAlt5: itoaOpt(it.VariantAlt[4]),
			Text:        "\n" + it.Raw + "\n",
		}
		for _, mr := range it.ModRanges {
			xi.ModRanges = append(xi.ModRanges, xModRange{ID: itoa(mr.ID), Range: ftoa(mr.Range)})
		}
		x.Items.Items = append(x.Items.Items, xi)
	}
	for _, s := range b.ItemSets {
		xs := xItemSet{ID: itoa(s.ID), Title: s.Title, UseSecondWeaponSet: btoa(s.UseSecondWeaponSet)}
		for _, sl := range s.Slots {
			active := ""
			if sl.Active {
				active = "true"
			}
			xs.Slots = append(xs.Slots, xSlot{Name: sl.Name, ItemID: itoa(sl.ItemID), Active: active})
		}
		x.Items.ItemSets = append(x.Items.ItemSets, xs)
	}
	for _, s := range b.SkillSets {
		xs := xSkillSet{ID: itoa(s.ID), Title: s.Title}
		for _, g := range s.Groups {
			xk := xSkill{
				Enabled:              btoa(g.Enabled),
				IncludeInFullDPS:     btoa(g.IncludeInFullDPS),
				GroupCount:           itoaPtr(g.GroupCount),
				Label:                g.Label,
				Slot:                 g.Slot,
				Source:               g.Source,
				MainActiveSkill:      itoa(g.MainActiveSkill),
				MainActiveSkillCalcs: itoa(g.MainActiveSkillCalcs),
			}
			for _, gm := range g.Gems {
				xk.Gems = append(xk.Gems, xGem{
					NameSpec:           gm.NameSpec,
					SkillID:            gm.SkillID,
					GemID:              gm.GemID,
					VariantID:          gm.VariantID,
					QualityID:          gm.QualityID,
					Level:              itoa(gm.Level),
					Quality:            itoa(gm.Quality),
					Enabled:            btoa(gm.Enabled),
					EnableGlobal1:      btoa(gm.EnableGlobal1),
					EnableGlobal2:      btoa(gm.EnableGlobal2),
					Count:              itoa(gm.Count),
					SkillPart:          itoaOpt(gm.SkillPart),
					SkillStageCount:    itoaOpt(gm.SkillStageCount),
					SkillMineCount:     itoaOpt(gm.SkillMineCount),
					SkillMinion:        gm.SkillMinion,
					SkillMinionItemSet: itoaOpt(gm.SkillMinionItemSet),
					SkillMinionSkill:   itoaOpt(gm.SkillMinionSkill),

					SkillPartCalcs:          itoaOpt(gm.SkillPartCalcs),
					SkillStageCountCalcs:    itoaOpt(gm.SkillStageCountCalcs),
					SkillMineCountCalcs:     itoaOpt(gm.SkillMineCountCalcs),
					SkillMinionCalcs:        gm.SkillMinionCalcs,
					SkillMinionItemSetCalcs: itoaOpt(gm.SkillMinionItemSetCalcs),
					SkillMinionSkillCalcs:   itoaOpt(gm.SkillMinionSkillCalcs),
				})
			}
			xs.Skills = append(xs.Skills, xk)
		}
		x.Skills.SkillSets = append(x.Skills.SkillSets, xs)
	}
	for _, c := range b.ConfigSets {
		xc := xConfigSet{ID: itoa(c.ID), Title: c.Title}
		for _, in := range c.Inputs {
			xc.Inputs = append(xc.Inputs, toXInput(in))
		}
		for _, in := range c.Placeholders {
			xc.Placeholders = append(xc.Placeholders, toXInput(in))
		}
		for _, cm := range c.CustomMods {
			xc.CustomMods = append(xc.CustomMods, xCustomMB{Title: cm.Title, Enabled: btoa(cm.Enabled), Text: cm.Text})
		}
		x.Config.ConfigSets = append(x.Config.ConfigSets, xc)
	}

	if p := b.Party; p != nil {
		if p.ExportBuffs {
			x.Import = &xImport{ExportParty: "true"}
		}
		if len(p.Buffs) > 0 {
			x.Party = &xParty{}
			for _, pb := range p.Buffs {
				x.Party.Imported = append(x.Party.Imported, xPartyBuffs(pb))
			}
		}
	}

	if len(b.CalcsInputs) > 0 {
		x.Calcs = &xCalcs{}
		for _, in := range b.CalcsInputs {
			x.Calcs.Inputs = append(x.Calcs.Inputs, toXInput(in))
		}
	}

	out, err := xml.MarshalIndent(x, "", "\t")
	if err != nil {
		return nil, fmt.Errorf("pobcode: xml: %w", err)
	}
	return append([]byte(xml.Header), pobEscapes.Replace(string(out))...), nil
}

// pobEscapes rewrites encoding/xml's numeric character references into what
// PoB's runtime/lua/xml.lua understands: it decodes only lt, gt, amp, apos
// and quot, and silently drops any other entity (so "&#xA;" would glue item
// text lines together). Every "&" in the text is already "&amp;", so these
// sequences can only be escapes.
var pobEscapes = strings.NewReplacer("&#xA;", "\n", "&#x9;", "\t", "&#xD;", "\r", "&#39;", "&apos;", "&#34;", "&quot;")

func toXInput(in Input) xInput {
	out := xInput{Name: in.Name}
	switch in.Kind {
	case KindNumber:
		v := ftoa(in.Number)
		out.Number = &v
	case KindBool:
		v := btoa(in.Bool)
		out.Boolean = &v
	case KindString:
		v := in.String
		out.String = &v
	}
	return out
}

// clusterHashFormatAttr writes Spec.ClusterHashFormatVersion: format 1 is
// the absent attribute (how such specs are saved), 0 (a new spec) is 2.
func clusterHashFormatAttr(v int) string {
	switch v {
	case 0:
		return "2"
	case 1:
		return ""
	}
	return itoa(v)
}
