package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/Mi7teR/poe-lib/itemtext"
	"github.com/Mi7teR/poe-lib/modparser"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// itemScaling is how addItem applies an item's scale (CalcSetup.lua
// 1255-1305).
type itemScaling int

const (
	// scaleEach scales each mod line (env.itemModDB:ScaleAddList(srcList, scale)).
	scaleEach itemScaling = iota
	// scaleMerged merges the item's same mods first, then scales them
	// (quivers, corrupted jewel effect: combinedList:MergeMod, ScaleAddList).
	scaleMerged
	// scaleAddPart merges the item's same mods and adds each additive mod's
	// scaled (scale - 1) part back (gloves and boots, CalcSetup.lua
	// 1278-1302): v + trunc(v * (scale - 1)); LIST and FLAG mods stay
	// unscaled (MergeMod(mod, true) skips non-additive mods).
	scaleAddPart
)

// equipmentBonusScale returns the scale of an equipped item's mods and how
// to apply it (CalcSetup.lua): quivers with Widowhail or
// EffectOfBonusesFromQuiver on passives, gloves and boots with
// EffectOfBonusesFromGloves/Boots (passives and custom mods); 1 otherwise.
func (c *Calculator) equipmentBonusScale(
	db *ModDB, b *pobcode.Build, set *pobcode.ItemSet, res *Result,
) func(*pobcode.Item) (float64, itemScaling) {
	gloves := (1 + db.Sum("INC", "EffectOfBonusesFromGloves")/100) * db.More("EffectOfBonusesFromGloves")
	boots := (1 + db.Sum("INC", "EffectOfBonusesFromBoots")/100) * db.More("EffectOfBonusesFromBoots")
	nodeQuiver := db.Sum("INC", "EffectOfBonusesFromQuiver")
	quiver := 1.0
	weapon1 := "Weapon 1"
	if set.UseSecondWeaponSet {
		weapon1 = "Weapon 1 Swap"
	}
	for _, slot := range set.Slots {
		if slot.Name != weapon1 || slot.ItemID == 0 {
			continue
		}
		if it, mods := c.itemMods(b.Item(slot.ItemID), res); it != nil {
			sum := nodeQuiver
			for _, m := range mods {
				if f, ok := num(m.Value); ok && m.Name == "EffectOfBonusesFromQuiver" && m.Type == "INC" {
					sum += f
				}
			}
			if strings.Contains(it.Name, "Widowhail") || nodeQuiver > 0 {
				quiver = 1 + sum/100
			}
		}
	}
	if quiver != 1 {
		db.NewMod("WidowHailMultiplier", "BASE", quiver, "Widowhail")
	}
	return func(item *pobcode.Item) (float64, itemScaling) {
		it := c.peekItem(item)
		if it == nil || it.Base == nil {
			return 1, scaleEach
		}
		switch {
		case it.Base.Type == "Quiver" && quiver != 1:
			return quiver, scaleMerged
		case it.Base.Type == "Gloves" && gloves != 1:
			return gloves, scaleAddPart
		case it.Base.Type == "Boots" && boots != 1:
			return boots, scaleAddPart
		}
		return 1, scaleEach
	}
}

// skipSlot leaves out swap weapons (unless the swap set is active) and
// flasks, whose effects depend on being active.
func skipSlot(name string, useSwap bool) bool {
	isSwap := strings.Contains(name, "Swap")
	switch {
	case strings.HasPrefix(name, "Flask"):
		return true
	case useSwap:
		return strings.HasPrefix(name, "Weapon") && !isSwap
	default:
		return isSwap
	}
}

// localArmour names the mods ItemClass:BuildModListForSlotNum takes out of
// an armour item's list with calcLocal: they make the item's own defences.
var localArmour = map[string]string{
	"Armour": "BASE INC", "Evasion": "BASE INC", "EnergyShield": "BASE INC", "Ward": "BASE INC",
	"ArmourAndEvasion": "BASE INC", "ArmourAndEnergyShield": "BASE INC", "EvasionAndEnergyShield": "BASE INC",
	"Defences": "INC", "AlternateQualityArmour": "BASE",
}

// isLocalArmourMod is calcLocal's selection: no flags or keyword flags and
// no tag but a leading InSlot. Shields also take their block chance.
func isLocalArmourMod(m modparser.Mod, shield bool) bool {
	types, ok := localArmour[m.Name]
	if m.Name == "BlockChance" && shield {
		types, ok = "BASE INC", true
	}
	if !ok || !strings.Contains(types, m.Type) || m.Flags != 0 || m.KeywordFlags != 0 {
		return false
	}
	return len(m.Tags) == 0 || tagStr(m.Tags[0], "type") == "InSlot"
}

// localWeapon are the mods the weapon branch of BuildModListForSlotNum
// consumes with calcLocal (name -> "TYPE:flags"), so they leave the
// item's mod list.
var localWeapon = func() map[string]string {
	m := map[string]string{
		"Speed": "INC:1", "AlternateQualityLocalAttackSpeedPer8Quality": "INC:0",
		"WeaponRange": "BASE:0", "WeaponRangeMetre": "BASE:0", "AlternateQualityLocalWeaponRangePer10Quality": "BASE:0",
		"LocalElementalDamage": "INC:0", "PhysicalDamage": "INC:0", "AlternateQualityWeapon": "BASE:0",
		"AlternateQualityLocalCritChancePer4Quality": "INC:0",
	}
	for _, t := range []string{"Physical", "Lightning", "Cold", "Fire", "Chaos"} {
		m[t+"Min"], m[t+"Max"] = "BASE:0", "BASE:0"
	}
	for _, t := range []string{"Lightning", "Cold", "Fire"} {
		m["Local"+t+"Damage"] = "INC:0"
	}
	return m
}()

// isLocalWeaponMod reports whether calcLocal consumes m on a weapon.
func isLocalWeaponMod(m modparser.Mod) bool {
	if m.KeywordFlags != 0 || (len(m.Tags) > 0 && tagStr(m.Tags[0], "type") != "InSlot") {
		return false
	}
	if m.Name == "CritChance" && (m.Type == "BASE" || m.Type == "INC") && m.Flags == 0 {
		return true
	}
	return localWeapon[m.Name] == fmt.Sprintf("%s:%d", m.Type, m.Flags)
}

// Item.lua catalystList and catalystTags: a catalyst scales the lines that
// carry one of its tags by (100 + quality) / 100.
var catalystTags = map[string][]string{
	"Abrasive": {"attack"}, "Accelerating": {"speed"}, "Dextral": {"suffix"},
	"Fertile": {"life", "mana", "resource"}, "Imbued": {"caster"},
	"Intrinsic": {"jewellery_attribute", "attribute"}, "Noxious": {"physical_damage", "chaos_damage"},
	"Prismatic": {"jewellery_resistance", "resistance"}, "Sinistral": {"prefix"},
	"Tempering": {"jewellery_defense", "defences", "armour", "evasion", "energyshield"},
	"Turbulent": {"jewellery_elemental", "elemental_damage"}, "Unstable": {"critical"},
}

// catalystScalar ports getCatalystScalar (Classes/Item.lua).
func catalystScalar(it *itemtext.Item, l itemtext.ModLine) float64 {
	tags := catalystTags[it.Catalyst]
	if tags == nil || l.Unscalable || len(l.Tags) == 0 {
		return 1
	}
	q := it.CatalystQuality
	if q == 0 {
		q = 20
	}
	for _, want := range tags {
		for _, have := range l.Tags {
			if have == want {
				return float64(100+q) / 100
			}
		}
	}
	return 1
}

// itemLines returns an item's active mod lines with rolls and catalyst
// scaling applied. ModRange ids count lines in ItemsTab.lua's save order.
func (c *Calculator) itemLines(item *pobcode.Item, res *Result) (*itemtext.Item, []string) {
	it, err := c.items.Parse(item.Raw)
	if err != nil {
		res.Unparsed = append(res.Unparsed, "item: "+err.Error())
		return nil, nil
	}
	if item.Variant > 0 {
		it.Variant = item.Variant
	}
	for i, v := range item.VariantAlt { // ItemsTab: variantAlt.. attributes
		if v > 0 {
			it.VariantAlt[i] = v
		}
	}
	// ItemsTab:Load ignores the ModRange ids when there are more of them than
	// saved mod lines (lines that stopped being mod lines).
	ranges := map[int]float64{}
	if len(item.ModRanges) <= len(it.Enchants)+len(it.Scourge)+len(it.Implicits)+len(it.Explicits)+len(it.Crucible) {
		for _, mr := range item.ModRanges {
			ranges[mr.ID] = mr.Range
		}
	}
	type entry struct {
		kind   string // enchant, scourge, implicit, explicit, crucible
		line   itemtext.ModLine
		r      float64
		scalar float64
	}
	var entries []entry
	// The ids count the base flask buff lines (buffModLines) first.
	id := 0
	if it.Base != nil && it.Base.Flask != nil {
		id = len(it.Base.Flask.Buff)
	}
	for gi, group := range [][]itemtext.ModLine{it.Enchants, it.Scourge, it.Implicits, it.Explicits, it.Crucible} {
		kind := []string{"enchant", "scourge", "implicit", "explicit", "crucible"}[gi]
		for _, l := range group {
			id++
			if !it.LineActive(l) {
				continue
			}
			r := defaultAffixQuality
			if l.HasRange {
				r = l.Range
			}
			if v, ok := ranges[id]; ok {
				r = v
			}
			entries = append(entries, entry{kind, l, r, catalystScalar(it, l)})
		}
	}
	// Item.lua modMagnitudeMods: "N% increased <tags> modifier magnitudes"
	// lines scale the matching lines of crafted or advanced-copy items.
	if it.CraftedItem || it.AdvancedCopy {
		ml := make([]magnitudeLine, len(entries))
		for i, e := range entries {
			ml[i] = magnitudeLine{applyRange(e.line.Text, e.r, e.scalar), e.line.Disabled}
		}
		for _, mm := range magnitudeMods(ml) {
			for i := range entries {
				e := &entries[i]
				if mm.modType != "" && mm.modType != e.kind || mm.modType == "" && e.kind != "implicit" && e.kind != "explicit" && e.kind != "enchant" {
					continue
				}
				if e.line.Unscalable || c.grantsSkill(e.line.Text) || !magnitudeMatch(mm, e.line) {
					continue
				}
				if mm.multiplier != 0 {
					e.scalar *= mm.multiplier
				} else {
					e.scalar += mm.quality / 100
				}
			}
		}
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		lines[i] = applyRange(e.line.Text, e.r, e.scalar)
	}
	return it, lines
}

type magnitudeLine struct {
	text     string
	disabled bool
}

type magnitudeMod struct {
	tags, anyTags []string
	quality       float64
	multiplier    float64
	modType       string
}

var (
	magnitudeRes = []*regexp.Regexp{
		regexp.MustCompile(`(\d+)% ([ir][ne][cd][ru][ec][ae][sd]e?d?) ?([a-z\s]*) modifier magnitudes`),
		regexp.MustCompile(`(\d+)% ([ir][ne][cd][ru][ec][ae][sd]e?d?) effect of ([sp][ur][fe]fix)es`),
		regexp.MustCompile(`([a-z\s]*) modifier magnitudes are doubled`),
	}
	magnitudeTagMap = map[string]string{"defence": "defences", "enchantment": "enchant"}
)

// magnitudeMods reads the modifier magnitude lines of an item.
func magnitudeMods(lines []magnitudeLine) []magnitudeMod {
	var out []magnitudeMod
	for _, l := range lines {
		if l.disabled {
			continue
		}
		low := strings.ToLower(l.text)
		for pi, re := range magnitudeRes {
			m := re.FindStringSubmatch(low)
			if m == nil {
				continue
			}
			var amount, dir, tagsText string
			mult := 0.0
			if pi == 2 {
				tagsText, amount, dir, mult = m[1], "100", "increased", 2
			} else {
				amount, dir, tagsText = m[1], m[2], m[3]
			}
			if dir != "increased" && dir != "reduced" {
				continue
			}
			q, _ := strconv.ParseFloat(amount, 64)
			if dir == "reduced" {
				q = -q
			}
			if tagsText == "explicit physical and chaos damage" {
				out = append(out, magnitudeMod{tags: []string{"damage"}, anyTags: []string{"physical", "chaos"}, quality: q, modType: "explicit"})
				break
			}
			mm := magnitudeMod{quality: q, multiplier: mult}
			for _, w := range strings.Fields(tagsText) {
				if t, ok := magnitudeTagMap[w]; ok {
					w = t
				}
				switch w {
				case "implicit", "explicit", "enchant":
					mm.modType = w
				default:
					mm.tags = append(mm.tags, w)
				}
			}
			out = append(out, mm)
			break
		}
	}
	return out
}

func magnitudeMatch(mm magnitudeMod, l itemtext.ModLine) bool {
	has := map[string]bool{"prefix": l.Prefix, "suffix": l.Suffix, "unveiled": l.Unveiled}
	for _, t := range l.Tags {
		has[t] = true
	}
	for _, t := range mm.tags {
		if !has[t] {
			return false
		}
	}
	return mm.anyTags == nil || has[mm.anyTags[0]] || has[mm.anyTags[1]]
}

// grantsSkill: modifiers that grant skills are not affected by magnitudes.
func (c *Calculator) grantsSkill(text string) bool {
	r := c.parser.Parse(text)
	for _, m := range r.Mods {
		if m.Name == "ExtraSkill" {
			return true
		}
	}
	if r.Extra != "" {
		l := strings.ToLower(text)
		return grantsLevelRe.MatchString(l) || grantsNameRe.MatchString(l)
	}
	return false
}

var (
	grantsLevelRe = regexp.MustCompile(`^grants level \d+ `)
	grantsNameRe  = regexp.MustCompile(`^grants \D+$`)
)

// itemMods parses an item's active mod lines.
func (c *Calculator) itemMods(item *pobcode.Item, res *Result) (*itemtext.Item, []modparser.Mod) {
	it, lines := c.itemLines(item, res)
	if it == nil {
		return nil, nil
	}
	var mods []modparser.Mod
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		parsed := c.parser.Parse(line)
		// ItemClass:ParseRaw: a line that does not parse is tried together
		// with the next one (two-line mods such as cluster jewel enchants
		// "Added Small Passive Skills grant: ...").
		if !parsed.Parsed() && i+1 < len(lines) {
			if comb := c.parser.Parse(line + " " + lines[i+1]); comb.Parsed() {
				parsed = comb
				i++
			} else if !isRadiusFuncLine(line) && isRadiusFuncLine(line+" "+lines[i+1]) {
				i++ // a two-line radius jewel function (radius.go)
				continue
			}
		}
		if !parsed.Parsed() {
			if !isRadiusFuncLine(line) {
				res.Unparsed = append(res.Unparsed, line)
			}
			continue
		}
		mods = append(mods, parsed.Mods...)
	}
	return it, mods
}

// itemSource is PoB's modSource: "Item:<id>:<name>", where the name of a
// rare or unique is "<title>, <base>" and a normal or magic name includes
// the base already. Item.lua drops a base's variant suffix from the name
// (gsub(" %(.+%)", ""): "Two-Stone Ring (Cold/Lightning)" names
// "Two-Stone Ring"), at 1005 for normal and magic items and at 1622 for
// the others.
func itemSource(item *pobcode.Item, it *itemtext.Item) string {
	if it.Rarity == itemtext.Normal || it.Rarity == itemtext.Magic {
		return fmt.Sprintf("Item:%d:%s", item.ID, baseVariantRe.ReplaceAllString(it.Name, ""))
	}
	return fmt.Sprintf("Item:%d:%s, %s", item.ID, it.Name, baseVariantRe.ReplaceAllString(it.BaseName, ""))
}

// baseVariantRe is Item.lua's " %(.+%)": a base's variant suffix.
var baseVariantRe = regexp.MustCompile(` \(.+\)`)

// addItem adds an item's global mods, scaled (abyss jewels), and returns
// the parsed item and all its mods.
func (c *Calculator) addItem(
	db *ModDB, res *Result, item *pobcode.Item, scale float64, slot string, mode itemScaling,
) (*itemtext.Item, []modparser.Mod) {
	return c.addItemTo(db, db.Add, res, item, scale, slot, mode)
}

// addItemTo is addItem with the item's mod list (ItemClass modList, in
// order) given to add; db takes the item's class restriction.
func (c *Calculator) addItemTo(
	db *ModDB, add func(Mod), res *Result, item *pobcode.Item, scale float64, slot string, mode itemScaling,
) (*itemtext.Item, []modparser.Mod) {
	if item == nil {
		return nil, nil
	}
	it, mods := c.itemMods(item, res)
	if it == nil {
		return nil, nil
	}
	source := itemSource(item, it)
	// CalcSetup.lua: set conditions on restricted items.
	if cr := it.ClassRestriction(); cr != "" {
		db.classConds[strings.ReplaceAll(it.Name, " ", "")] = cr
	}
	isArmour := it.Base != nil && it.Base.Armour != nil
	isWeapon := it.Base != nil && it.Base.Weapon != nil
	var list []Mod
	for _, m := range mods {
		if isArmour && isLocalArmourMod(m, it.Base.Armour.BlockChance != 0) {
			continue // local to the item: part of its own defence values
		}
		if isWeapon && isLocalWeaponMod(m) {
			continue // local weapon stats: part of the weapon's own data
		}
		if sm, ok := slotTags(fromParsed(m, source), slot, slotNumber(it, slot)); ok {
			if isWeapon {
				sm = weaponHandLocal(sm, m, slotNumber(it, slot))
			}
			if mode == scaleEach {
				list = append(list, sm)
			} else {
				list = mergeMod(list, sm)
			}
		}
	}
	for _, m := range list {
		if mode == scaleAddPart {
			add(addScaledPart(m, scale-1))
		} else {
			add(scaleMod(m, scale))
		}
	}
	// Item.lua: each Grand Spectrum counts for itself and for minions.
	if it.Base != nil && it.Base.Type == "Jewel" && strings.Contains(it.Name, "Grand Spectrum") {
		spectrum := Mod{Name: "Multiplier:GrandSpectrum", Type: "BASE", Value: 1.0, Source: it.Name}
		add(scaleMod(spectrum, scale))
		add(Mod{Name: "MinionModifier", Type: "LIST", Value: map[string]any{"mod": modToMap(spectrum)}, Source: it.Name})
	}
	if isArmour && it.Base.Armour.MovementPenalty != 0 {
		add(Mod{Name: "MovementSpeed", Type: "INC", Value: -it.Base.Armour.MovementPenalty, Source: source,
			Tags: []map[string]any{{"type": "Condition", "var": "IgnoreMovementPenalties", "neg": true}}})
	}
	if slot != "" {
		name := strings.Replace(slot, " Swap", "", 1)
		socketMultipliers(add, it, name)
		add(Mod{Name: "Multiplier:QualityOn" + name, Type: "BASE", Value: float64(it.Quality), Source: "Quality"})
	}
	return it, mods
}

// weaponHandLocal is the end of Item.lua's weapon branch: accuracy, impale
// chance, life/mana on hit and physical leech on a weapon only apply to
// attacks with that hand; poison and bleed chance get the hand condition too.
func weaponHandLocal(sm Mod, m modparser.Mod, slotNum int) Mod {
	hand := map[string]any{"type": "Condition", "var": "OffHandAttack"}
	if slotNum == 1 {
		hand["var"] = "MainHandAttack"
	}
	kwOK := m.KeywordFlags == 0 || m.KeywordFlags == kfAttack
	switch {
	case ((m.Name == "Accuracy" && m.Flags == 0) || (m.Name == "ImpaleChance" && m.Flags != mfSpell) ||
		((m.Name == "LifeOnHit" || m.Name == "ManaOnHit") && m.Flags == mfAttack) ||
		((m.Name == "PhysicalDamageLifeLeech" || m.Name == "PhysicalDamageManaLeech") && m.Flags == mfAttack)) &&
		kwOK && len(m.Tags) == 0:
		sm.Tags = append([]map[string]any{hand}, sm.Tags...)
	case (m.Name == "PoisonChance" || m.Name == "BleedChance") && m.Flags != mfSpell &&
		(len(m.Tags) == 0 || (m.Tags[0]["type"] == "Condition" && m.Tags[0]["var"] == "CriticalStrike" && len(m.Tags) == 1)):
		sm.Tags = append(append([]map[string]any(nil), sm.Tags...), hand)
	}
	return sm
}

// socketMultipliers ports the socket part of ItemClass:BuildModListForSlotNum:
// a multiplier per socket colour and the number of unlinked sockets.
func socketMultipliers(add func(Mod), it *itemtext.Item, slot string) {
	if len(it.Sockets) == 0 {
		return
	}
	names := map[byte]string{'R': "Red", 'G': "Green", 'B': "Blue", 'W': "White"}
	groups := map[int]int{}
	for _, s := range it.Sockets {
		groups[s.Group]++
		if n, ok := names[s.Color]; ok {
			add(Mod{Name: "Multiplier:" + n + "SocketIn" + slot, Type: "BASE", Value: 1.0, Source: "Item Sockets"})
		}
	}
	unlinked := 0
	for _, n := range groups {
		if n == 1 {
			unlinked++
		}
	}
	add(Mod{Name: "Multiplier:UnlinkedSocketIn" + slot, Type: "BASE", Value: float64(unlinked), Source: "Unlinked Item Sockets"})
}

// slotTags is the tag substitution of ItemClass:BuildModListForSlotNum:
// {SlotName}, {Hand} and {OtherSlotNum} for the slot the item is in.
// slotNumber is the slotNum ItemClass:BuildModList builds a list for:
// weapons and rings have one per slot, other items none (0).
func slotNumber(it *itemtext.Item, slot string) int {
	if it == nil || it.Base == nil || (it.Base.Weapon == nil && it.Base.Type != "Ring") {
		return 0
	}
	slot = strings.Replace(slot, " Swap", "", 1)
	if i := strings.LastIndex(slot, " "); i >= 0 {
		n, _ := strconv.Atoi(slot[i+1:])
		return n
	}
	return 0
}

// slotTags is the tag handling of ItemClass:BuildModListForSlotNum: mods
// for another slot number (SlotNumber, InSlot) are dropped, and
// {SlotName}, {Hand} and {OtherSlotNum} are filled in.
func slotTags(m Mod, slot string, slotNum int) (Mod, bool) {
	if slot == "" {
		return m, true
	}
	for _, t := range m.Tags {
		if typ := tagStr(t, "type"); typ == "SlotNumber" || typ == "InSlot" {
			if n, _ := tagNum(t, "num"); int(n) != slotNum {
				return m, false
			}
		}
	}
	slot = strings.Replace(slot, " Swap", "", 1)
	hand, other := "OffHand", "1"
	if slotNum == 1 {
		hand, other = "MainHand", "2"
	}
	r := strings.NewReplacer("{SlotName}", slot, "{Hand}", hand, "{OtherSlotNum}", other)
	var tags []map[string]any
	for i, t := range m.Tags {
		var nt map[string]any
		for k, v := range t {
			if sv, ok := v.(string); ok && strings.Contains(sv, "{") {
				if nt == nil {
					nt = copyMap(t)
				}
				nt[k] = r.Replace(sv)
			}
		}
		if nt != nil {
			if tags == nil {
				tags = append([]map[string]any(nil), m.Tags...)
			}
			tags[i] = nt
		}
	}
	if tags != nil {
		m.Tags = tags
	}
	return m, true
}

func abyssalSockets(it *itemtext.Item) int {
	n := 0
	for _, s := range it.Sockets {
		if s.Color == 'A' {
			n++
		}
	}
	return n
}

// peekItem parses an item's text without recording parse problems.
func (c *Calculator) peekItem(item *pobcode.Item) *itemtext.Item {
	if item == nil {
		return nil
	}
	it, err := c.items.Parse(item.Raw)
	if err != nil {
		return nil
	}
	return it
}

// mergeMod is ModListClass:MergeMod: a BASE, INC or MORE mod with the same
// parameters as one in list adds its value to it; any other mod is
// appended.
func mergeMod(list []Mod, m Mod) []Mod {
	if v, ok := m.Value.(float64); ok && (m.Type == "BASE" || m.Type == "INC" || m.Type == "MORE") {
		for i := range list {
			if w, isNum := list[i].Value.(float64); isNum && sameParams(list[i], m) {
				list[i].Value = w + v
				return list
			}
		}
	}
	return append(list, m)
}

// addScaledPart is the gloves/boots branch of CalcSetup.lua (1286-1302):
// scaledList:ScaleAddList(combinedList, part) then combinedList:MergeMod(mod,
// true): an additive mod gains its scaled part (ScaleAddMod: unscaled for an
// unscalable mod); a non-additive mod is left as it is.
func addScaledPart(m Mod, part float64) Mod {
	v, ok := m.Value.(float64)
	if !ok || (m.Type != "BASE" && m.Type != "INC" && m.Type != "MORE") {
		return m
	}
	if add, isNum := scaleMod(m, part).Value.(float64); isNum {
		m.Value = v + add
	}
	return m
}

// aegisShield is env.aegisModList and env.aegisItem: the shield worn with
// Necromantic Aegis, whose mods go to the minions instead of the player.
type aegisShield struct {
	item *itemtext.Item
	mods []Mod           // env.aegisModList, scaled
	gear []modparser.Mod // all the shield's parsed mods, for its armour data
}

// necromanticAegis is env.allocNodes[45175] being the Necromantic Aegis
// keystone (CalcSetup.lua:1133).
func (c *Calculator) necromanticAegis(allocated map[int]bool) bool {
	if !allocated[45175] || c.data.Tree == nil {
		return false
	}
	n := c.data.Tree.Nodes[45175]
	return n != nil && n.Name == "Necromantic Aegis"
}

// addAegisItem is CalcSetup.lua:1133-1150: a shield worn with Necromantic
// Aegis. Its mods that apply to socketed gems or add supports (SocketedIn
// tags) stay with the player; the rest form env.aegisModList, which every
// minion gets (CalcPerform.lua:1203-1206).
func (c *Calculator) addAegisItem(
	db *ModDB, res *Result, item *pobcode.Item, scale float64, slot string, mode itemScaling,
) (*itemtext.Item, []modparser.Mod) {
	a := &aegisShield{}
	it, mods := c.addItemTo(db, func(m Mod) {
		for _, t := range m.Tags {
			if tagStr(t, "type") == "SocketedIn" {
				db.Add(m)
				return
			}
		}
		a.mods = append(a.mods, m)
	}, res, item, scale, slot, mode)
	if it == nil {
		return nil, nil
	}
	a.item, a.gear = it, mods
	db.aegis = a
	return it, mods
}
