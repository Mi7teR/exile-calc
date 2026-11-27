package engine

import (
	"math"
	"strconv"
	"strings"

	"github.com/Mi7teR/exile-calc/gamedata"
	"github.com/Mi7teR/exile-calc/itemtext"
	"github.com/Mi7teR/exile-calc/modparser"
	"github.com/Mi7teR/exile-calc/pobcode"
)

// gemRequirement is an env.requirementsTableGems entry: a gem and
// gemInstance.reqOverride (nil when no GemProperty "req" applies).
type gemRequirement struct {
	gem      *gamedata.Gem
	inst     *pobcode.Gem
	override *float64
}

// effectReq is effect.req after applyGemMods (CalcSetup.lua:424): the sum
// of the GemProperty "req" values, nil when there is none.
func effectReq(e *skillEffect) *float64 {
	var v *float64
	for _, p := range e.props {
		if p.key == "req" {
			if v == nil {
				v = new(float64)
			}
			*v += p.value
		}
	}
	return v
}

// gemStatRequirement is calcLib.getGemStatRequirement (CalcTools.lua:179-189).
func gemStatRequirement(level int, support bool, multi int) float64 {
	if multi == 0 {
		return 0
	}
	statType := 0.7
	if support {
		statType = 0.5
	}
	req := round((20+float64(float64(level-3)*3))*math.Pow(float64(multi)/100, 0.9)*statType, 0)
	if req < 14 {
		return 0
	}
	return req
}

// requirements is a requirementsTable entry: Str, Dex and Int.
type requirements [3]float64

// requirements is the entry of a gem: gemInstance.reqStr/reqDex/reqInt from
// ProcessSocketGroup (SkillsTab.lua:1236-1241: the requirement level of the
// gem's validated level), or reqOverride for all three.
func (c *Calculator) gemRequirements(r gemRequirement) requirements {
	if r.override != nil {
		return requirements{*r.override, *r.override, *r.override}
	}
	ge := c.data.Skills.Effects[r.gem.GrantedEffectID]
	if ge == nil {
		return requirements{}
	}
	level := 0
	if lv := ge.Levels[validateLevel(r.inst.Level, ge, r.gem)]; lv != nil {
		level = lv.LevelRequirement
	}
	return requirements{
		gemStatRequirement(level, ge.Support, r.gem.ReqStr),
		gemStatRequirement(level, ge.Support, r.gem.ReqDex),
		gemStatRequirement(level, ge.Support, r.gem.ReqInt),
	}
}

// itemRequirements is item.requirements.strMod/dexMod/intMod (Item.lua:
// 2652-2665): the base's requirements (the item's own Str/Dex/Int lines
// unless it has sockets, 1391-1396; Tabula Rasa and the like need no
// Int), with the local StrRequirement, DexRequirement and IntRequirement
// mods, or none with NoAttributeRequirements.
func itemRequirements(it *itemtext.Item, mods []modparser.Mod) requirements {
	req := it.Req
	if it.Base != nil && len(it.Sockets) > 0 {
		req = itemtext.Requirements{Str: it.Base.Req.Str, Dex: it.Base.Req.Dex, Int: it.Base.Req.Int}
	}
	switch it.Name {
	case "Tabula Rasa, Simple Robe", "Skin of the Loyal, Simple Robe", "Skin of the Lords, Simple Robe",
		"The Apostate, Cabalist Regalia":
		req.Int = 0
	}
	if calcLocalFlag(mods, "NoAttributeRequirements") {
		return requirements{}
	}
	mod := func(base int, name string) float64 {
		return math.Floor((float64(base) + calcLocal(mods, name, "BASE")) * (1 + calcLocal(mods, name, "INC")/100))
	}
	return requirements{mod(req.Str, "StrRequirement"), mod(req.Dex, "DexRequirement"), mod(req.Int, "IntRequirement")}
}

// calcLocalFlag is calcLocal for a FLAG.
func calcLocalFlag(mods []modparser.Mod, name string) bool {
	for _, m := range mods {
		if m.Name == name && m.Type == "FLAG" && m.Flags == 0 && m.KeywordFlags == 0 &&
			(len(m.Tags) == 0 || tagStr(m.Tags[0], "type") == "InSlot") && truthy(m.Value) {
			return true
		}
	}
	return false
}

// itemListItem is the build's item in slot, an itemList slot name: a gear or
// abyssal slot of the active item set (of its swap slot with the second
// weapon set), or "Jewel <node>" for a tree socket.
func itemListItem(b *pobcode.Build, slot string) *pobcode.Item {
	if id, ok := strings.CutPrefix(slot, "Jewel "); ok {
		if spec := b.ActiveSpec(); spec != nil {
			for _, s := range spec.Sockets {
				if strconv.Itoa(s.NodeID) == id && s.ItemID != 0 {
					return b.Item(s.ItemID)
				}
			}
		}
		return nil
	}
	set := b.ActiveItems()
	if set == nil {
		return nil
	}
	if set.UseSecondWeaponSet && strings.HasPrefix(slot, "Weapon ") {
		if id := set.SlotItem(slot[:len("Weapon 1")] + " Swap" + slot[len("Weapon 1"):]); id != 0 {
			return b.Item(id)
		}
	}
	if id := set.SlotItem(slot); id != 0 {
		return b.Item(id)
	}
	return nil
}

// attributeRequirements is calcs.perform's "Process attribute
// requirements" (CalcPerform.lua:2131-2196): the highest requirement of
// each attribute over the equipped items and the gems, scaled by
// GlobalAttributeRequirements (and turned into Omniscience with
// OmniscienceRequirements), as ReqStr, ReqDex, ReqInt (or ReqOmni). A
// requirement of 0 sets nothing. The ReqXString and ReqXItem outputs are
// display data and are not ported.
func (c *Calculator) attributeRequirements(db *ModDB, b *pobcode.Build, compared int) {
	var table []requirements
	// env.requirementsTableItems: every item of env.player.itemList
	// (CalcSetup.lua:1101-1112); the order does not change the maximum.
	for slot, it := range db.items {
		if mods, ok := db.gearMods[slot]; ok {
			table = append(table, itemRequirements(it, mods))
			continue
		}
		// Jewels and abyss jewels: the item step keeps no parse of them, so
		// their requirements are cached per item like item.grantedSkills.
		item := itemListItem(b, slot)
		if item == nil {
			table = append(table, itemRequirements(it, nil))
			continue
		}
		key := grantKey(item)
		if v, ok := c.reqs.Load(key); ok {
			r, _ := v.(requirements)
			table = append(table, r)
			continue
		}
		_, mods := c.itemMods(item, &Result{}) // its unparsed lines are reported by the item step
		r := itemRequirements(it, mods)
		if compared == 0 || item.ID != compared {
			c.reqs.Store(key, r)
		}
		table = append(table, r)
	}
	for _, g := range db.requirementGems {
		table = append(table, c.gemRequirements(g))
	}
	reqMult := calcMod(db, nil, "GlobalAttributeRequirements")
	omni := db.Flag("OmniscienceRequirements")
	omniMult := calcMod(db, nil, "OmniAttributeRequirements")
	ignore := db.Flag("IgnoreAttributeRequirements")
	attrs := []string{"Str", "Dex", "Int"}
	for ai, attr := range attrs {
		target := attr
		if omni {
			target = "Omni"
		}
		best := 0.0
		for _, r := range table {
			if r[ai] <= 0 {
				continue
			}
			req := math.Floor(r[ai] * reqMult)
			if omni {
				req = math.Floor(req * (1 / (omniMult - 1)))
			}
			best = math.Max(best, req)
		}
		if ignore {
			best = 0
		}
		// CalcPerform.lua:2186-2193: Req<attr>String is 0, or the new
		// requirement; with a breakdown an unmet one is a coloured string
		// (left out here: Stats holds numbers).
		db.output["Req"+attr+"String"] = 0
		if best > db.output["Req"+target] {
			db.output["Req"+target+"String"] = best
			if db.buildBreakdown && best > db.output[target] {
				delete(db.output, "Req"+target+"String")
			}
			db.output["Req"+target] = best
		}
	}
}
