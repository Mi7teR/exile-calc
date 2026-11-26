// Package itemtext parses Path of Exile item text into a structured item:
// base, rarity, sockets, requirements and mod lines split into enchants,
// implicits and explicits. It reads both the game's clipboard text
// (Ctrl+C and the advanced Ctrl+Alt+C) and PoB's item format.
//
// Ported from ItemClass:ParseRaw in src/Classes/Item.lua (PoB v2.68.1).
// Differences, on purpose:
//   - Mod lines are not parsed into modifiers here (that is modparser).
//     PoB keeps a line it cannot parse only in some section states; without a
//     parser every candidate line is kept, except in the section after the
//     explicits (unique flavour text) and the base type line.
//   - Advanced copy values such as "+48(40-49)" keep the rolled value
//     ("+48 ...") and record the roll in Range; PoB rewrites them to "(40-49)".
//   - Values are not scaled (catalysts, "modifier magnitudes"); Range is kept.
package itemtext

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
)

type Rarity string

const (
	Normal Rarity = "NORMAL"
	Magic  Rarity = "MAGIC"
	Rare   Rarity = "RARE"
	Unique Rarity = "UNIQUE"
	Relic  Rarity = "RELIC"
)

type Socket struct {
	Color byte // R, G, B, W or A
	Group int  // linked sockets share a group
}

type Requirements struct {
	Str, Dex, Int int
}

// ModLine is one mod line with PoB's markers removed.
type ModLine struct {
	Text     string   // e.g. "+(25-40) to maximum Life" or "+48 to maximum Life"
	Range    float64  // roll 0..1 when HasRange
	HasRange bool     // from {range:x} or an advanced copy "48(40-49)"
	Tags     []string // {tags:...} or advanced copy header tags
	Variants []int    // {variant:1,2}: shown only for those variants
	Groups   []int    // {group:1}: variant groups the line belongs to
	Versions []int    // {version:1}: versions the line exists in
	ModGroup string   // {modGroup:x}: generated lines of one modifier

	Crafted, Implicit, Enchant, Fractured, Scourge, Crucible, Custom,
	Mutated, Synthesis, Eater, Exarch, Unscalable, Disabled bool
	Prefix, Suffix, Unveiled, Vestigial bool
}

type Item struct {
	ItemClass string
	Rarity    Rarity
	// Name is the full first line: the title of a rare or unique, or the
	// whole name of a normal or magic item ("Sturdy Leather Belt of ...").
	Name       string
	BaseName   string
	Base       *gamedata.Base
	NamePrefix string // magic items: text before the base name
	NameSuffix string // magic items: text after the base name

	ItemLevel int
	Quality   int
	LevelReq  int
	Req       Requirements
	Sockets   []Socket

	Corrupted, Mirrored, Split, Fractured, Synthesised, Unidentified, Veiled bool
	Influences                                                               []string // shaper, elder, adjudicator, basilisk, crusader, eyrie, cleansing, tangle

	UniqueID, League, Source, Note string

	// Catalyst is the catalyst name ("Prismatic", ...) and CatalystQuality
	// its quality; 0 when the text does not say (PoB then assumes 20).
	Catalyst        string
	CatalystQuality int

	// Radius is a jewel's radius label ("Large", "Variable", ...).
	Radius string

	// CraftedItem and AdvancedCopy are Item.lua's self.crafted and
	// self.advancedCopy: modifier magnitudes apply only to such items.
	CraftedItem, AdvancedCopy bool

	// ArmourData holds the defence spec lines (Item.lua armourData): the
	// values "Armour", "Evasion", "EnergyShield", "Ward" and their
	// "<name>BasePercentile" rolls.
	ArmourData map[string]float64

	Variants []string // "Variant:" lines of a unique
	Variant  int      // selected variant, 1-based; the last one by default
	// VariantAlt are the "Selected Alt Variant[ Two..Five]" picks, used when
	// HasAltVariant is set for that slot.
	VariantAlt    [5]int
	HasAltVariant [5]bool
	// UsesVariantGroups: the item selects one variant per group ("Selected
	// Variant Group: g=v") and optionally a version, instead of Variant.
	UsesVariantGroups bool
	GroupSelections   map[int]int
	SelectedVersion   int
	versionCount      int

	classRestriction string    // "Requires Class" spec line
	classLines       []ModLine // "{variant:n}Requires Class X" lines

	Enchants  []ModLine
	Scourge   []ModLine
	Implicits []ModLine
	Explicits []ModLine
	Crucible  []ModLine
}

// ClassRestriction is item.classRestriction: the class a "Requires Class"
// line names, a variant line of the selected variant taking precedence
// (Item.lua BuildModList).
func (it *Item) ClassRestriction() string {
	for _, l := range it.active(it.classLines) {
		return l.Text
	}
	return it.classRestriction
}

// ActiveImplicits returns the implicits shown for the selected variant.
func (it *Item) ActiveImplicits() []ModLine { return it.active(it.Implicits) }

// ActiveExplicits returns the explicits shown for the selected variant.
func (it *Item) ActiveExplicits() []ModLine { return it.active(it.Explicits) }

func (it *Item) active(lines []ModLine) []ModLine {
	var out []ModLine
	for _, l := range lines {
		if it.LineActive(l) {
			out = append(out, l)
		}
	}
	return out
}

// LineActive is ItemClass:CheckModLineVariant: whether a line belongs to
// the selected variant, alt variants, variant groups and version.
func (it *Item) LineActive(l ModLine) bool {
	if it.UsesVariantGroups {
		if len(l.Versions) > 0 && (it.SelectedVersion == 0 || !containsInt(l.Versions, it.SelectedVersion)) {
			return false
		}
		if len(l.Groups) > 0 {
			if len(l.Variants) == 0 {
				return false
			}
			for _, g := range l.Groups {
				if v, ok := it.GroupSelections[g]; ok && containsInt(l.Variants, v) {
					return true
				}
			}
			return false
		}
		return len(l.Variants) == 0
	}
	if len(l.Variants) == 0 || containsInt(l.Variants, it.Variant) {
		return true
	}
	for i, has := range it.HasAltVariant {
		if has && containsInt(l.Variants, it.VariantAlt[i]) {
			return true
		}
	}
	return false
}

// normaliseVariantGroups is ItemClass:NormaliseVariantSelections: a group
// keeps a selection that is eligible and not used by an earlier group,
// otherwise it takes its first eligible variant not used yet.
func (it *Item) normaliseVariantGroups(lines []ModLine) {
	if it.versionCount > 0 {
		if it.SelectedVersion == 0 {
			it.SelectedVersion = it.versionCount
		}
		it.SelectedVersion = max(1, min(it.versionCount, it.SelectedVersion))
	} else {
		it.SelectedVersion = 0
	}
	// variantGroups[group][variant] = versions (0: any)
	groups := map[int]map[int]map[int]bool{}
	for _, l := range lines {
		for _, g := range l.Groups {
			if groups[g] == nil {
				groups[g] = map[int]map[int]bool{}
			}
			for _, v := range l.Variants {
				if v < 1 || v > len(it.Variants) {
					continue
				}
				if groups[g][v] == nil {
					groups[g][v] = map[int]bool{}
				}
				if len(l.Versions) == 0 {
					groups[g][v][0] = true
				}
				for _, ver := range l.Versions {
					if ver >= 1 && ver <= it.versionCount {
						groups[g][v][ver] = true
					}
				}
			}
		}
	}
	eligible := func(g, v int) bool {
		vers := groups[g][v]
		return vers != nil && (vers[0] || (it.SelectedVersion != 0 && vers[it.SelectedVersion]))
	}
	var ids []int
	for g := range groups {
		ids = append(ids, g)
	}
	sort.Ints(ids)
	for g := range it.GroupSelections {
		if groups[g] == nil {
			delete(it.GroupSelections, g)
		}
	}
	used := map[int]bool{}
	var needs []int
	for _, g := range ids {
		any := false
		for v := 1; v <= len(it.Variants); v++ {
			any = any || eligible(g, v)
		}
		if !any {
			continue
		}
		if sel, ok := it.GroupSelections[g]; ok && eligible(g, sel) && !used[sel] {
			used[sel] = true
		} else {
			needs = append(needs, g)
		}
	}
	for _, g := range needs {
		delete(it.GroupSelections, g)
		for v := 1; v <= len(it.Variants); v++ {
			if eligible(g, v) && !used[v] {
				it.GroupSelections[g] = v
				used[v] = true
				break
			}
		}
	}
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Parser resolves base names against game data.
type Parser struct {
	bases map[string]*gamedata.Base
}

func New(d *gamedata.Data) *Parser {
	return &Parser{bases: d.Bases}
}

// Item.lua: lineFlags
var lineFlags = map[string]bool{
	"crafted": true, "crucible": true, "custom": true, "disabled": true, "eater": true,
	"enchant": true, "exarch": true, "fractured": true, "implicit": true, "scourge": true,
	"synthesis": true, "mutated": true, "unscalable": true,
	"prefix": true, "suffix": true, "unveiled": true, "vestigial": true,
}

// ItemTools.lua: itemLib.influenceInfo.all, as "<display> Item" lines.
var influenceLines = map[string]string{
	"Shaper Item": "shaper", "Elder Item": "elder", "Warlord Item": "adjudicator",
	"Hunter Item": "basilisk", "Crusader Item": "crusader", "Redeemer Item": "eyrie",
	"Searing Exarch Item": "cleansing", "Eater of Worlds Item": "tangle",
}

// Spec lines ParseRaw reads or ignores; any other "Name: value" line is a mod
// with a colon in it (Item.lua: "Anything else is an explicit with a colon").
var rangeTextRe = regexp.MustCompile(`\((-?\d+\.?\d*)-(-?\d+\.?\d*)\)`)

var variantGroupRe = regexp.MustCompile(`^(\d+)\s*=\s*(\d+)$`)

// altIndex maps "Has Alt Variant[ Two..Five]" and "Selected Alt Variant[ ...]" to 0-4.
func altIndex(spec string) int {
	switch {
	case strings.HasSuffix(spec, "Two"):
		return 1
	case strings.HasSuffix(spec, "Three"):
		return 2
	case strings.HasSuffix(spec, "Four"):
		return 3
	case strings.HasSuffix(spec, "Five"):
		return 4
	}
	return 0
}

var knownSpecs = map[string]bool{
	"Unique ID": true, "Item Level": true, "Memory Strands": true, "Requires Class": true,
	"Quality": true, "Sockets": true, "Radius": true, "Limited to": true, "Version": true,
	"Variant": true, "Talisman Tier": true, "Armour": true, "Evasion Rating": true,
	"Evasion": true, "Energy Shield": true, "Ward": true, "Requires Level": true,
	"Level": true, "LevelReq": true, "Has Alt Variant": true, "Has Alt Variant Two": true,
	"Has Alt Variant Three": true, "Has Alt Variant Four": true, "Has Alt Variant Five": true,
	"Selected Version": true, "Selected Variant Group": true, "Selected Variant": true,
	"Selected Alt Variant": true, "Selected Alt Variant Two": true,
	"Selected Alt Variant Three": true, "Selected Alt Variant Four": true,
	"Selected Alt Variant Five": true, "Allow Duplicate Variants": true, "Has Variants": true,
	"Selected Variants": true, "League": true, "Crafted": true, "Scourge": true,
	"Crucible": true, "Implicit": true, "Prefix": true, "Suffix": true, "Implicits": true,
	"Unreleased": true, "Upgrade": true, "Source": true, "Cluster Jewel Skill": true,
	"Cluster Jewel Node Count": true, "Catalyst": true, "CatalystQuality": true,
	"Intangibility": true, "Note": true, "Str": true, "Strength": true, "Dex": true,
	"Dexterity": true, "Int": true, "Intelligence": true, "Critical Strike Range": true,
	"Attacks per Second": true, "Weapon Range": true, "Critical Strike Chance": true,
	"Physical Damage": true, "Elemental Damage": true, "Chaos Damage": true,
	"Chance to Block": true, "Block chance": true,
}

var (
	rarityRe      = regexp.MustCompile(`^Rarity: ([A-Za-z]+)`)
	specRe        = regexp.MustCompile(`^([A-Za-z ()]+:?): (.+)$`)
	requiresRe    = regexp.MustCompile(`^(Requires [A-Za-z]+) (.+)$`)
	catalystQRe   = regexp.MustCompile(`Quality \(([A-Za-z\s]+) Modifiers\)`)
	reminderRe    = regexp.MustCompile(`^\([A-Za-z]+|^\(\d+%? of `)
	flaskStateRe  = regexp.MustCompile(`^Lasts .+ Seconds$|^Consumes \d+ of \d+ Charges on use$|^Currently has \d+ Charges$`)
	headerRe      = []*regexp.Regexp{regexp.MustCompile(`^\{ (.*?) - (.*?)  - (\d*).*\}$`), regexp.MustCompile(`^\{ (.*?) - (.*?) \}$`), regexp.MustCompile(`^\{ (.*?) \}$`)}
	tokenRe       = regexp.MustCompile(`\{([A-Za-z]*):?([^}]*)\}`)
	flagSuffixRe  = regexp.MustCompile(` \(([a-z]+)\)`)
	wordRe        = regexp.MustCompile(`[A-Za-z]+`)
	tagRe         = regexp.MustCompile(`[A-Za-z_]+`)
	digitsRe      = regexp.MustCompile(`\d+`)
	numberRe      = regexp.MustCompile(`[+-]?\d+(?:\.\d+)?`)
	fixedValueRe  = regexp.MustCompile(`(-?\d+\.?\d*)\((-?\d+\.?\d*)\)`)
	rolledValueRe = regexp.MustCompile(`(-?\d+\.?\d*)\((-?\d+\.?\d*)-(-?\d+\.?\d*)\)`)
	enumRangeRe   = regexp.MustCompile(`\s*\([^()\d]*-[^()\d]*\)`)
	parenSuffixRe = regexp.MustCompile(` \(.+\)`)
	radiusLabelRe = regexp.MustCompile(`^[A-Za-z ]+`)
)

// Item.lua catalystList / catalystDescriptorList: the game text names the
// catalyst by what it improves.
var catalystByDescriptor = map[string]string{
	"Attack": "Abrasive", "Speed": "Accelerating", "Suffix": "Dextral", "Life and Mana": "Fertile",
	"Caster": "Imbued", "Attribute": "Intrinsic", "Physical and Chaos": "Noxious", "Resistance": "Prismatic",
	"Prefix": "Sinistral", "Defence": "Tempering", "Elemental": "Turbulent", "Critical": "Unstable",
}

// specNumber returns the first number in a spec value ("+20% (augmented)" -> 20).
func specNumber(s string) int {
	m := numberRe.FindString(s)
	if m == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(m, 64)
	return int(f)
}

type stage int

const (
	findImplicit stage = iota
	implicitStage
	findExplicit
	explicitStage
	done
)

// Parse parses one item. It fails on empty text and on a base it cannot find.
func (p *Parser) Parse(text string) (*Item, error) {
	text = sanitise(strings.ReplaceAll(text, "\r", ""))
	var lines []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, escapeGGG(l))
		}
	}
	if len(lines) == 0 {
		return nil, errors.New("itemtext: empty item text")
	}

	it := &Item{Rarity: Unique}
	gameMode := false
	l := 0
	if strings.HasPrefix(lines[l], "Item Class:") {
		it.ItemClass = strings.TrimSpace(strings.TrimPrefix(lines[l], "Item Class:"))
		l++
	}
	if l < len(lines) {
		if m := rarityRe.FindStringSubmatch(lines[l]); m != nil {
			gameMode = true
			switch r := Rarity(strings.ToUpper(m[1])); r {
			case Normal, Magic, Rare, Unique, Relic:
				it.Rarity = r
			}
			l++
		}
	}
	for _, line := range lines {
		if line == "Unidentified" {
			it.Unidentified = true
		}
		if strings.Contains(line, "Foil Unique") && (it.Rarity == Unique || it.Rarity == Relic) {
			it.Rarity = Relic
		}
	}
	if l < len(lines) {
		if lines[l] == "--------" {
			l++
		}
		if l < len(lines) {
			it.Name = lines[l]
			if !(it.Rarity == Normal || it.Rarity == Magic || it.Unidentified) {
				l++
			}
		}
	}

	var (
		st            = findImplicit
		checkSection  bool
		foundExplicit bool
		foundImplicit bool
		implicitLines int
		importedLevel int
		levelSet      bool
		linePrefix    string
		linePostfix   string
		flaskBuff     map[string]bool
		baseSet       bool
	)

	for ; l < len(lines); l++ {
		line := lines[l]
		if line == "Veiled Prefix" || line == "Veiled Suffix" {
			it.Veiled = true
		}
		switch {
		case flaskBuff[line]:
			delete(flaskBuff, line)
			continue
		case line == "--------":
			linePrefix, linePostfix = "", ""
			checkSection = true
			continue
		case line == "Split":
			it.Split = true
			continue
		case line == "Mirrored":
			it.Mirrored = true
			continue
		case line == "Corrupted":
			it.Corrupted = true
			continue
		case line == "Fractured Item":
			it.Fractured = true
			continue
		case line == "Synthesised Item":
			it.Synthesised = true
			continue
		case line == "Unidentified", strings.Contains(line, "Foil Unique"), line == "Requirements:":
			continue
		case influenceLines[line] != "":
			it.Influences = append(it.Influences, influenceLines[line])
			continue
		case strings.HasPrefix(line, "(") && reminderRe.MatchString(line):
			for l < len(lines) && !strings.HasSuffix(lines[l], ")") {
				l++
			}
			continue
		case it.Base != nil && it.Base.Flask != nil && flaskStateRe.MatchString(line):
			continue
		case strings.HasPrefix(line, "{ "):
			// Advanced copy header: flags and tags apply to the lines below it.
			it.AdvancedCopy, it.CraftedItem = true, true
			linePrefix, linePostfix = "", ""
			var fullName, tags string
			for _, re := range headerRe {
				if m := re.FindStringSubmatch(line); m != nil {
					fullName = m[1]
					if len(m) > 2 {
						tags = m[2]
					}
					break
				}
			}
			if strings.Contains(fullName, "Enhancement") {
				linePostfix = " (enchant)"
			}
			if i := strings.Index(strings.ReplaceAll(fullName, "Foulborn", "Mutated"), "Modifier"); i >= 0 {
				for _, w := range wordRe.FindAllString(strings.ReplaceAll(fullName, "Foulborn", "Mutated")[:i], -1) {
					if lineFlags[strings.ToLower(w)] {
						linePrefix += "{" + strings.ToLower(w) + "}"
					}
				}
			}
			if strings.HasPrefix(fullName, "Allocated Crucible Passive Skill") {
				linePrefix += "{crucible}"
			}
			if tags != "" {
				linePrefix += "{tags:" + strings.Join(strings.Fields(strings.ToLower(tags)), "") + "}"
			}
			continue
		}

		line = linePrefix + line + linePostfix
		if checkSection {
			switch {
			case st == implicitStage:
				if foundImplicit {
					st, foundExplicit = explicitStage, true
				} else {
					st = findExplicit
				}
			case st == explicitStage:
				st = done
			case st == findImplicit && it.ItemLevel > 0 && !strings.Contains(line, " (implicit)") &&
				!strings.Contains(line, " (enchant)") && !strings.Contains(line, "Talisman Tier"):
				st, foundExplicit = explicitStage, true
			}
			checkSection = false
		}

		specName, specVal := "", ""
		// The strings.Contains checks skip regexps that need that text.
		if m := matchIf(specRe, line, ": "); m != nil {
			specName, specVal = m[1], m[2]
			if specName == "Class:" {
				specName = "Requires Class"
			}
		} else if m := requiresRe.FindStringSubmatch(line); m != nil {
			specName, specVal = m[1], m[2]
		}
		if specName != "" {
			switch specName {
			case "Unique ID":
				it.UniqueID = specVal
			case "Item Level":
				it.ItemLevel = specNumber(specVal)
			case "Quality":
				it.Quality = specNumber(specVal)
			case "Armour", "Evasion Rating", "Evasion", "Energy Shield", "Ward":
				key := map[string]string{"Evasion Rating": "Evasion", "Energy Shield": "EnergyShield"}[specName]
				if key == "" {
					key = specName
				}
				if it.ArmourData == nil {
					it.ArmourData = map[string]float64{}
				}
				it.ArmourData[key] = float64(specNumber(specVal))
			case "Sockets":
				group := 0
				for i := 0; i < len(specVal); i++ {
					switch c := specVal[i]; c {
					case 'R', 'G', 'B', 'W', 'A':
						it.Sockets = append(it.Sockets, Socket{Color: c, Group: group})
					case ' ':
						group++
					}
				}
			case "Requires Level", "LevelReq":
				it.LevelReq, levelSet = specNumber(specVal), true
			case "Level":
				importedLevel = specNumber(specVal) // "Requirements from imported items can't always be trusted"
			case "Variant":
				// "{ver}name" is an old format: keep the name.
				if i := strings.Index(specVal, "}"); strings.HasPrefix(specVal, "{") && i > 0 {
					specVal = specVal[i+1:]
				}
				it.Variants = append(it.Variants, specVal)
			case "Selected Variant":
				it.Variant = specNumber(specVal)
			case "Crafted":
				it.CraftedItem = true
			case "Selected Version":
				it.SelectedVersion, it.UsesVariantGroups = specNumber(specVal), true
			case "Version":
				it.versionCount++
			case "Selected Variant Group":
				if m := variantGroupRe.FindStringSubmatch(specVal); m != nil {
					g, _ := strconv.Atoi(m[1])
					v, _ := strconv.Atoi(m[2])
					if it.GroupSelections == nil {
						it.GroupSelections = map[int]int{}
					}
					it.GroupSelections[g], it.UsesVariantGroups = v, true
				}
			case "Has Alt Variant", "Has Alt Variant Two", "Has Alt Variant Three", "Has Alt Variant Four", "Has Alt Variant Five":
				it.HasAltVariant[altIndex(specName)] = true
			case "Selected Alt Variant", "Selected Alt Variant Two", "Selected Alt Variant Three", "Selected Alt Variant Four", "Selected Alt Variant Five":
				it.VariantAlt[altIndex(specName)] = specNumber(specVal)
			case "Has Variants", "Selected Variants":
				l++ // Item.lua skips the next line (old Watcher's Eye format)
			case "Requires Class":
				it.classRestriction = specVal
			case "League":
				it.League = specVal
			case "Source":
				it.Source = specVal
			case "Note":
				it.Note = specVal
			case "Radius":
				// Item.lua: specVal:match("^[%a ]+")
				it.Radius = strings.TrimSpace(radiusLabelRe.FindString(specVal))
			case "Catalyst":
				it.Catalyst = specVal
			case "CatalystQuality":
				it.CatalystQuality = specNumber(specVal)
			case "Implicits":
				implicitLines = specNumber(specVal)
				st = explicitStage
			case "Str", "Strength":
				it.Req.Str = specNumber(specVal)
			case "Dex", "Dexterity":
				it.Req.Dex = specNumber(specVal)
			case "Int", "Intelligence":
				it.Req.Int = specNumber(specVal)
			default:
				if m := catalystQRe.FindStringSubmatch(specName); m != nil {
					// Game text: "Quality (Resistance Modifiers): +20%".
					it.CatalystQuality = specNumber(specVal)
					it.Catalyst = catalystByDescriptor[m[1]]
				} else if strings.Contains(specName, "BasePercentile") {
					if it.ArmourData == nil {
						it.ArmourData = map[string]float64{}
					}
					f, _ := strconv.ParseFloat(numberRe.FindString(specVal), 64)
					it.ArmourData[specName] = f
				} else if !knownSpecs[specName] &&
					!(strings.Contains(it.Name, specName) && strings.Contains(it.Name, specVal)) {
					foundExplicit, st = true, explicitStage
				}
			}
		}
		if line == "Prefixes:" {
			foundExplicit, st = true, explicitStage
		}
		if specName != "" && !foundExplicit && !foundImplicit {
			continue
		}

		mod := ModLine{}
		line = replaceIf(tokenRe, line, "{", func(tok string) string {
			m := tokenRe.FindStringSubmatch(tok)
			k, val := m[1], m[2]
			switch {
			case k == "variant":
				for _, d := range digitsRe.FindAllString(val, -1) {
					n, _ := strconv.Atoi(d)
					mod.Variants = append(mod.Variants, n)
				}
			case k == "group":
				for _, d := range digitsRe.FindAllString(val, -1) {
					if n, _ := strconv.Atoi(d); n > 0 {
						mod.Groups = append(mod.Groups, n)
					}
				}
				it.UsesVariantGroups = true
			case k == "version":
				for _, d := range digitsRe.FindAllString(val, -1) {
					n, _ := strconv.Atoi(d)
					mod.Versions = append(mod.Versions, n)
				}
				it.UsesVariantGroups = true
			case k == "modGroup":
				mod.ModGroup = val
			case k == "tags":
				mod.Tags = append(mod.Tags, tagRe.FindAllString(val, -1)...)
			case k == "range":
				it.AdvancedCopy = true
				if f, err := strconv.ParseFloat(val, 64); err == nil {
					mod.Range, mod.HasRange = f, true
				}
			case lineFlags[k]:
				setFlag(&mod, k)
			}
			return ""
		})
		line = replaceIf(flagSuffixRe, line, " (", func(tok string) string {
			if k := flagSuffixRe.FindStringSubmatch(tok)[1]; lineFlags[k] {
				setFlag(&mod, k)
			}
			return ""
		})
		if mod.Enchant {
			mod.Crafted, mod.Implicit = true, true
		}

		// Base line.
		baseName := ""
		if it.Base == nil && (it.Rarity == Normal || it.Rarity == Magic) {
			baseName = p.baseInName(it)
		}
		if baseName == "" {
			baseName = strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(line, "Superior "), "Synthesised "), "Vestigial ")
		}
		if baseName == "Two-Toned Boots" {
			baseName = "Two-Toned Boots (Armour/Energy Shield)"
		}
		if base := p.bases[baseName]; base != nil {
			if it.Variant == 0 || len(mod.Variants) == 0 || containsInt(mod.Variants, it.Variant) {
				it.BaseName, it.Base, baseSet = baseName, base, true
				it.Req = Requirements{Str: base.Req.Str, Dex: base.Req.Dex, Int: base.Req.Int}
				if base.Flask != nil && len(base.Flask.Buff) > 0 && flaskBuff == nil {
					flaskBuff = map[string]bool{}
					for _, b := range base.Flask.Buff {
						flaskBuff[b] = true
					}
				}
			}
			continue
		}

		if mod.Implicit {
			foundImplicit, st = true, implicitStage
		}
		if strings.HasSuffix(line, " - Unscalable Value") {
			line, mod.Unscalable = strings.TrimSuffix(line, " - Unscalable Value"), true
		}
		if rangeTextRe.MatchString(line) {
			it.AdvancedCopy = true // "(#-#)" lines keep their unapplied range
		}
		line = advancedValues(line, &mod)

		// Item.lua: which list the line belongs to.
		target := &it.Explicits
		switch {
		case mod.Enchant || (mod.Crafted && len(it.Enchants)+len(it.Implicits) < implicitLines):
			target = &it.Enchants
		case mod.Scourge:
			target = &it.Scourge
		case strings.Contains(line, "Requires Class"):
			target = nil
			if _, c, ok := strings.Cut(line, "Requires Class "); ok && len(mod.Variants) > 0 {
				it.classLines = append(it.classLines, ModLine{Text: c, Variants: mod.Variants})
			}
		case mod.Implicit || (!mod.Crafted && len(it.Enchants)+len(it.Scourge)+len(it.Implicits) < implicitLines):
			target = &it.Implicits
		case mod.Crucible:
			target = &it.Crucible
		}

		if gameMode {
			// Without a mod parser, keep the line unless it is the base type
			// line of the property section or follows the explicits.
			if st == done {
				continue
			}
			if st == findImplicit && p.isBaseTypeLine(it, line) {
				continue
			}
			switch st {
			case findImplicit:
				st = implicitStage
			case findExplicit:
				foundExplicit, st = true, explicitStage
			case explicitStage:
				foundExplicit = true
			}
		} else {
			foundExplicit = true
		}
		if target != nil {
			mod.Text = strings.TrimSpace(line)
			*target = append(*target, mod)
		}
	}

	if !baseSet {
		return nil, fmt.Errorf("itemtext: unknown base for %q", it.Name)
	}
	if it.Rarity == Normal || it.Rarity == Magic {
		it.Name = strings.TrimSpace(it.Name)
	}
	if len(it.Variants) > 0 && (it.Variant == 0 || it.Variant > len(it.Variants)) {
		it.Variant = len(it.Variants)
	}
	for i := range it.VariantAlt {
		if len(it.Variants) > 0 && (it.VariantAlt[i] == 0 || it.VariantAlt[i] > len(it.Variants)) {
			it.VariantAlt[i] = len(it.Variants)
		}
	}
	if it.UsesVariantGroups {
		if it.GroupSelections == nil {
			it.GroupSelections = map[int]int{}
		}
		var all []ModLine
		for _, g := range [][]ModLine{it.Enchants, it.Scourge, it.Implicits, it.Explicits, it.Crucible} {
			all = append(all, g...)
		}
		it.normaliseVariantGroups(all)
	}
	if !levelSet {
		switch {
		case importedLevel > 0:
			it.LevelReq = importedLevel
		default:
			it.LevelReq = it.Base.Req.Level
		}
	}
	return it, nil
}

// Common.lua: sanitiseText (the UTF-8 replacements).
var sanitiser = strings.NewReplacer(
	"‐", "-", "‑", "-", "‒", "-", "–", "-", "—", "-", "―", "-",
	"​", "", "−", "-", "ä", "a", "ö", "o",
)

var angleRe = regexp.MustCompile(`<[^<>]*>`)

func sanitise(s string) string {
	if !strings.ContainsAny(s, "<") && isASCII(s) {
		return s
	}
	// sanitiseText strips <...> markup too, but escapeGGG must see "<tag>{text}" first.
	s = escapeMarkupRe.ReplaceAllString(s, "$1")
	return sanitiser.Replace(angleRe.ReplaceAllString(s, ""))
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// Common.lua: escapeGGGString strips the game's text markup:
// "<tag>{text}" -> text, "[Key]" -> Key, "[Key|Shown]" -> Shown.
var (
	escapeMarkupRe = regexp.MustCompile(`<[^>]+>\{([^}]+)\}`)
	escapeKeyRe    = regexp.MustCompile(`\[([^|\]]+)\]`)
	escapeShownRe  = regexp.MustCompile(`\[[^|]+\|([^|]+)\]`)
)

func escapeGGG(s string) string {
	// Each pattern needs its "<" or "["; markup removal never adds one.
	if strings.Contains(s, "<") {
		s = escapeMarkupRe.ReplaceAllString(s, "$1")
	}
	if !strings.Contains(s, "[") {
		return s
	}
	s = escapeKeyRe.ReplaceAllString(s, "$1")
	return escapeShownRe.ReplaceAllString(s, "$1")
}

// matchIf is re.FindStringSubmatch(s), skipped when s lacks text that
// every match of re contains.
func matchIf(re *regexp.Regexp, s, text string) []string {
	if !strings.Contains(s, text) {
		return nil
	}
	return re.FindStringSubmatch(s)
}

// replaceIf is re.ReplaceAllStringFunc(s, f), skipped when s lacks text
// that every match of re contains.
func replaceIf(re *regexp.Regexp, s, text string, f func(string) string) string {
	if !strings.Contains(s, text) {
		return s
	}
	return re.ReplaceAllStringFunc(s, f)
}

func setFlag(m *ModLine, k string) {
	switch k {
	case "prefix":
		m.Prefix = true
	case "suffix":
		m.Suffix = true
	case "unveiled":
		m.Unveiled = true
	case "vestigial":
		m.Vestigial = true
	case "crafted":
		m.Crafted = true
	case "implicit":
		m.Implicit = true
	case "enchant":
		m.Enchant = true
	case "fractured":
		m.Fractured = true
	case "scourge":
		m.Scourge = true
	case "crucible":
		m.Crucible = true
	case "custom":
		m.Custom = true
	case "mutated":
		m.Mutated = true
	case "synthesis":
		m.Synthesis = true
	case "eater":
		m.Eater = true
	case "exarch":
		m.Exarch = true
	case "unscalable":
		m.Unscalable = true
	case "disabled":
		m.Disabled = true
	}
}

// baseInName finds the base of a normal or magic item in its name: an exact
// match, else the longest base name inside it (Item.lua, "Partial match").
func (p *Parser) baseInName(it *Item) string {
	if p.bases[it.Name] != nil {
		return it.Name
	}
	best, bestStart := "", -1
	for name := range p.bases {
		if i := strings.Index(it.Name, name); i >= 0 && (len(name) > len(best) || (len(name) == len(best) && name < best)) {
			best, bestStart = name, i
		}
	}
	if best == "" {
		if i := strings.Index(it.Name, "Two-Toned Boots"); i >= 0 {
			best, bestStart = "Two-Toned Boots", i
		}
	}
	if best != "" {
		it.NamePrefix = it.Name[:bestStart]
		it.NameSuffix = it.Name[bestStart+len(best):]
	}
	it.Name = parenSuffixRe.ReplaceAllString(it.Name, "")
	return best
}

// isBaseTypeLine reports the non-mod lines of a game text property section
// (Item.lua: the FINDIMPLICIT exclusions).
func (p *Parser) isBaseTypeLine(it *Item, line string) bool {
	if p.bases[line] != nil || line == it.Name || strings.Contains(line, "Two-Toned") {
		return true
	}
	if b := it.Base; b != nil && (line == b.Type || (b.SubType != "" && line == b.SubType+" "+b.Type)) {
		return true
	}
	return false
}

// advancedValues turns advanced copy values into plain ones and records the
// roll: "5(5)" -> "5", "+48(40-49)" -> "+48" with Range (48-40)/9.
func advancedValues(line string, mod *ModLine) string {
	if !strings.Contains(line, "(") {
		return line // all three patterns need a "("
	}
	line = fixedValueRe.ReplaceAllString(line, "$1")
	line = enumRangeRe.ReplaceAllString(line, "")
	bestDelta := -1.0
	line = rolledValueRe.ReplaceAllStringFunc(line, func(s string) string {
		m := rolledValueRe.FindStringSubmatch(s)
		v, _ := strconv.ParseFloat(m[1], 64)
		lo, _ := strconv.ParseFloat(m[2], 64)
		hi, _ := strconv.ParseFloat(m[3], 64)
		if lo > hi {
			lo, hi = hi, lo
		}
		if d := hi - lo; d > bestDelta {
			bestDelta = d
			r := 0.5
			if d > 0 {
				r = roundTo((v-lo)/d, 6)
			}
			if r >= 0 && r <= 1 {
				mod.Range, mod.HasRange = r, true
			}
		}
		return m[1]
	})
	return line
}

func roundTo(v float64, digits int) float64 {
	p := 1.0
	for range digits {
		p *= 10
	}
	if v < 0 {
		return -float64(int64(-v*p+0.5)) / p
	}
	return float64(int64(v*p+0.5)) / p
}
