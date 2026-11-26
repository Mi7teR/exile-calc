package engine

import (
	"sort"
	"strings"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

// This file ports the player's skills as far as they give buffs:
// CalcSetup.lua (socket groups to active skills, gem levels and supports),
// CalcActiveSkill.lua (createActiveSkill, buildActiveSkillModList),
// CalcTools.lua (buildSkillInstanceStats and helpers) and the Buff and Aura
// branches of CalcPerform.lua. Skill parts, minions, triggers, weapon flags
// of attacks and party buffs are not ported.

// Data/Global.lua ModFlag and KeywordFlag bits used here.
const (
	mfAttack     = 0x1
	mfSpell      = 0x2
	mfHit        = 0x4
	mfDot        = 0x8
	mfCast       = 0x10
	mfMelee      = 0x100
	mfArea       = 0x200
	mfProjectile = 0x400

	kfAura      = 0x1
	kfCurse     = 0x2
	kfWarcry    = 0x4
	kfMovement  = 0x8
	kfPhysical  = 0x10
	kfFire      = 0x20
	kfCold      = 0x40
	kfLightning = 0x80
	kfChaos     = 0x100
	kfVaal      = 0x200
	kfBow       = 0x400
	kfArrow     = 0x800
	kfTrap      = 0x1000
	kfMine      = 0x2000
	kfTotem     = 0x4000
	kfAttack    = 0x10000
	kfSpell     = 0x20000
	kfHit       = 0x40000
	kfBrand     = 0x100000
)

const matchingSocketQualityBonus = 10 // Data.lua data.misc.MatchingSocketQualityBonus

// gemIndex holds Data.lua's gem lookup tables.
type gemIndex struct {
	gems        map[string]*gamedata.Gem
	byGameID    map[string]map[string]*gamedata.Gem // gemsByGameId[gameId][variantId]
	forBaseName map[string]string                   // gemForBaseName: lower-case name -> gem id
	vaalBase    map[string]string                   // gemVaalGemIdForBaseGemId
	byName      map[string][]string                 // lower-case gem name -> gem ids (FindSkillGem exact match)
}

func newGemIndex(d *gamedata.Data) *gemIndex {
	x := &gemIndex{gems: d.Gems, byGameID: map[string]map[string]*gamedata.Gem{},
		forBaseName: map[string]string{}, vaalBase: map[string]string{}, byName: map[string][]string{}}
	ids := make([]string, 0, len(d.Gems))
	for id := range d.Gems {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		g := d.Gems[id]
		if x.byGameID[g.GameID] == nil {
			x.byGameID[g.GameID] = map[string]*gamedata.Gem{}
		}
		x.byGameID[g.GameID][g.VariantID] = g
		x.byName[strings.ToLower(g.Name)] = append(x.byName[strings.ToLower(g.Name)], id)
		base := g.Name
		if ge := d.Skills.Effects[g.GrantedEffectID]; ge != nil && ge.Support && g.GrantedEffectID != "SupportBarrage" {
			base += " Support"
		}
		x.forBaseName[strings.ToLower(base)] = id
		if g.BaseTypeName != "" && g.BaseTypeName != base {
			x.forBaseName[strings.ToLower(g.BaseTypeName)] = id
		}
	}
	for _, id := range ids {
		g := d.Gems[id]
		if !g.VaalGem {
			continue
		}
		for _, oid := range ids {
			if d.Gems[oid].GrantedEffectID == g.SecondaryGrantedEffectID {
				x.vaalBase[id] = oid
				break
			}
		}
	}
	// Data.lua: a Vaal gem whose base skill has transfigured variants
	// (AltX, AltY) gets a gem per variant, "Vaal <variant name>". The
	// skill's baseTypeName is its name for these.
	x.gems = make(map[string]*gamedata.Gem, len(d.Gems))
	for id, g := range d.Gems {
		x.gems[id] = g
	}
	for _, id := range ids {
		g := d.Gems[id]
		if !g.VaalGem {
			continue
		}
		for _, alt := range []string{"AltX", "AltY"} {
			sec := d.Skills.Effects[g.SecondaryGrantedEffectID+alt]
			if sec == nil {
				continue
			}
			n := *g
			n.ID, n.Name = id+alt, "Vaal "+sec.Name
			n.BaseTypeName = ""
			n.VariantID = g.VariantID + alt
			n.SecondaryGrantedEffectID = g.SecondaryGrantedEffectID + alt
			x.gems[id+alt] = &n
			x.byGameID[n.GameID][n.VariantID] = &n
			x.forBaseName[strings.ToLower(n.Name)] = id + alt
			if b, ok := x.vaalBase[id]; ok {
				x.vaalBase[id+alt] = b + alt
			}
		}
	}
	return x
}

// gameID is calcLib.getGameIdFromGemName(name, true).
func (x *gemIndex) gameID(name string) string {
	id, ok := x.forBaseName[strings.ToLower(name)]
	if !ok {
		return ""
	}
	g := x.gems[id]
	if g.VaalGem {
		if b := x.gems[x.vaalBase[id]]; b != nil {
			return b.GameID
		}
		return ""
	}
	return g.GameID
}

func hasTag(g *gamedata.Gem, tag string) bool {
	for _, t := range g.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// gemIsType is calcLib.gemIsType; idx is set for includeTransfigured.
func gemIsType(g *gamedata.Gem, typ string, idx *gemIndex) bool {
	name := strings.ToLower(g.Name)
	active := typ == "active skill" || typ == "grants_active_skill" || typ == "skill"
	switch {
	case typ == "all":
		return true
	case typ == "elemental" && (hasTag(g, "fire") || hasTag(g, "cold") || hasTag(g, "lightning")):
		return true
	case typ == "aoe" && hasTag(g, "area"):
		return true
	case typ == "trap or mine" && (hasTag(g, "trap") || hasTag(g, "mine")):
		return true
	case active && hasTag(g, "grants_active_skill") && !hasTag(g, "support"):
		return true
	case typ == "non-vaal" && !hasTag(g, "vaal"):
		return true
	case typ == "non-exceptional" && !hasTag(g, "exceptional"):
		return true
	case typ == name || typ == strings.TrimPrefix(name, "vaal "):
		return true
	case idx != nil && sameGameID(idx, g.Name, typ):
		return true
	}
	return !active && hasTag(g, typ)
}

// sameGameID is calcLib.isGemIdSame(a, b, true).
func sameGameID(idx *gemIndex, a, b string) bool {
	x, y := idx.gameID(a), idx.gameID(b)
	return x != "" && y != "" && x == y
}

// gemProperty is one entry of effect.gemPropertyInfo.
type gemProperty struct {
	key        string
	value      float64
	sourceType string // "Global" or "Item"
	mod        *Mod
}

// skillEffect is an activeEffect or supportEffect of CalcSetup.lua.
type skillEffect struct {
	id               string
	ge               *gamedata.Skill
	gem              *gamedata.Gem // gemData; nil for skills granted by items
	level, quality   int
	srcLevel         int // srcInstance.level and quality: the gem's own values
	srcQuality       int
	srcPart          int // srcInstance.skillPart (0: not set)
	hasSrc           bool
	cfg              *Cfg // gemCfg
	props            []gemProperty
	fromItem         bool // set on supports granted by items (ExtraSupport)
	appliesToGranted bool
	actorLevel       float64 // activeEffect.actorLevel: the minion's level for minion skills
	// srcTriggered and srcTriggerChance are srcInstance.triggered and
	// triggerChance: the ExtraSkill mod's fields for a skill granted by an
	// item or a node (CalcSetup.lua:1556-1557).
	srcTriggered        bool
	srcTriggerChance    float64
	srcHasTriggerChance bool
}

// skillBuff is an entry of activeSkill.buffList.
type skillBuff struct {
	typ, name       string
	cond, enemyCond string
	activeSkillBuff bool
	applyNotPlayer  bool
	applyMinions    bool
	applyAllies     bool
	stackVar        string // effectStackVar and its limit (Debuff stacks)
	stackLimitVar   string
	stackLimit      float64
	hasStackLimit   bool
	allowTotemBuff  bool
	mods            []Mod
}

type activeSkill struct {
	// extraSkillMods is activeSkill.extraSkillModList: the ExtraSkillMod mods
	// added to the skill, also given to its minions.
	extraSkillMods []Mod
	active         *skillEffect
	effects        []*skillEffect // the active effect, then compatible supports
	types          map[int]bool
	minionTypes    map[int]bool
	flags          map[string]bool
	group          *pobcode.SocketGroup
	gem            *pobcode.Gem // srcInstance: the gem of the active effect
	cfg            *Cfg
	mods           *ModDB // skillModList
	data           *skillData
	buffs          []*skillBuff
	disabled       bool
	main           bool // env.player.mainSkill
	// weapons are the types of the equipped weapons (weaponData1/2.type),
	// which supports with weaponTypes check for skills granted by supports.
	weapons [2]string
	// supportList is the support list the skill was created with; summon
	// is the summoning skill of a minion's skill (activeSkill.summonSkill).
	supportList []*skillEffect
	summon      *activeSkill
	// all1H is each hand's weaponData.countsAsAll1H, beside weapons.
	all1H [2]bool
	// weapon1Cfg and weapon2Cfg are the skill cfgs of main and off hand
	// attacks (weapon flags).
	weapon1Cfg, weapon2Cfg *Cfg
	// weaponRange1/2 are actor.weaponRange1/2 of calcs.offence (melee).
	weaponRange1, weaponRange2 float64
	// part is activeSkill.skillPart (1-based, 0 for single part skills).
	part int
	// mirage is activeSkill.mirage: the Mirage Archer copy's output.
	mirage *mirageResult
	// mineCount is activeSkill.activeMineCount (0 when unset).
	mineCount float64
	// activeStageCount is activeSkill.activeStageCount of a multi-stage
	// skill (the stages after the first), set when hasStageCount.
	activeStageCount float64
	hasStageCount    bool
	// spectres is env.build.spectreList for a skill whose minionList is
	// empty (Raise Spectre): the ids Build.lua Load keeps, those in
	// data.minions.
	spectres []string
	// minionModifiers are the skill's MinionModifier mods as calcs.perform
	// gives them to env.minion: read after the player's attributes, before
	// the buffs (the main skill only).
	minionModifiers []ListEntry
	// infoTrigger is activeSkill.infoTrigger: the trigger label
	// CalcTriggers.lua defaultTriggerHandler gives a triggered main skill.
	infoTrigger string
}

// modFlagByName is the weapon part of Data/Global.lua ModFlag.
var modFlagByName = map[string]uint64{
	"Axe": 0x10000, "Bow": 0x20000, "Claw": 0x40000, "Dagger": 0x80000, "Mace": 0x100000,
	"Staff": 0x200000, "Sword": 0x400000, "Wand": 0x800000, "Unarmed": 0x1000000, "Fishing": 0x2000000,
}

const (
	mfWeapon       = 0x2000
	mfWeaponMelee  = 0x4000000
	mfWeaponRanged = 0x8000000
	mfWeapon1H     = 0x10000000
	mfWeapon2H     = 0x20000000
)

// weaponFlags is CalcActiveSkill.lua getWeaponFlags without weaponTypes
// checks (minion skills ignore them): the ModFlags of a weapon type.
func weaponFlags(typ string) (uint64, bool) {
	info, ok := weaponTypeInfo[typ]
	if !ok {
		return 0, false
	}
	f := modFlagByName[info.flag]
	if typ != "None" {
		f |= mfWeapon
		if info.oneHand {
			f |= mfWeapon1H
		} else {
			f |= mfWeapon2H
		}
		if info.melee {
			f |= mfWeaponMelee
		} else {
			f |= mfWeaponRanged
		}
	}
	return f, true
}

// mainSkill is env.player.mainSkill, nil when there is none.
func mainSkill(skills []*activeSkill) *activeSkill {
	for _, s := range skills {
		if s.main {
			return s
		}
	}
	return nil
}

// combatConditions ports the mode_combat and mode_effective parts of
// doActorAttribsConditions (CalcPerform.lua:246-296, 400-410) for the
// actor db with main skill s (actor.mainSkill): conditions that follow
// from using the main skill. pm is env.player.mainSkill, which the
// ailment and exposure checks read for every actor: the player's main
// skill, or for env.minion the summoning skill. The shrine buffs of the
// mode_combat block are shrineBuffs.
func combatConditions(db *ModDB, s, pm *activeSkill) {
	if modeCombat(db) {
		combatSkillConditions(db, s, pm)
	}
	if !modeEffective(db) {
		return
	}
	// mode_effective (CalcPerform.lua:400-410): exposure the actor can
	// apply, from env.player.mainSkill's mods or the actor's.
	for _, el := range []string{"Fire", "Cold", "Lightning"} { //nolint:goconst // element names
		if (pm != nil && pm.mods != nil && pm.mods.SumCfg(pm.cfg, "BASE", el+"ExposureChance") > 0) ||
			db.Sum("BASE", el+"ExposureChance") > 0 {
			db.conditions["CanApply"+el+"Exposure"] = true
		}
	}
}

// combatSkillConditions is the mode_combat part of combatConditions
// (CalcPerform.lua:246-296).
func combatSkillConditions(db *ModDB, s, pm *activeSkill) {
	if s != nil {
		skillConditions(db.conditions, s)
	}
	var pmCfg *Cfg
	if pm != nil {
		pmCfg = pm.cfg
	}
	// CalcPerform.lua:288-296.
	altAilments := db.Flag("CritAlwaysAltAilments") && !db.FlagCfg(pmCfg, "NeverCrit")
	if db.Sum("BASE", "EnemyScorchChance") > 0 || altAilments || db.Flag("IgniteCanScorch") {
		db.conditions["CanInflictScorch"] = true
	}
	if db.Sum("BASE", "EnemyBrittleChance") > 0 || altAilments {
		db.conditions["CanInflictBrittle"] = true
	}
	if db.Sum("BASE", "EnemySapChance") > 0 || altAilments {
		db.conditions["CanInflictSap"] = true
	}
}

// skillConditions is CalcPerform.lua:247-287: the "recently" conditions
// of using the actor's main skill s.
//
//nolint:gocognit // moved from combatConditions as it was
func skillConditions(cond map[string]bool, s *activeSkill) {
	f := s.flags
	//nolint:nestif // pre-existing shape of the Lua port; only the skillData access changed
	if !truthy(s.data.val("triggered")) && !f["trap"] && !f["mine"] && !f["totem"] {
		if f["attack"] {
			cond["AttackedRecently"] = true
		} else if f["spell"] {
			cond["CastSpellRecently"] = true
		}
		if s.types[stMovement] {
			cond["UsedMovementSkillRecently"] = true
		}
		if f["minion"] && !f["permanentMinion"] {
			cond["UsedMinionSkillRecently"] = true
		}
		if s.types[stVaal] {
			cond["UsedVaalSkillRecently"] = true
		}
		if s.types[stChannel] {
			cond["Channelling"] = true
		}
	}
	if f["hit"] && !f["trap"] && !f["mine"] && !f["totem"] {
		cond["HitRecently"] = true
		if f["spell"] {
			cond["HitSpellRecently"] = true
		}
	}
	if f["totem"] {
		cond["HaveTotem"], cond["SummonedTotemRecently"] = true, true
		if f["hit"] {
			cond["TotemsHitRecently"] = true
			if f["spell"] {
				cond["TotemsSpellHitRecently"] = true
			}
		}
	}
	if f["mine"] {
		cond["DetonatedMinesRecently"] = true
	}
	if f["trap"] {
		cond["TriggeredTrapsRecently"] = true
	}
}

// mainCfg is env.player.mainSkill.skillCfg, nil when there is none (PoB
// then uses a default Melee skill, which is not built).
func mainCfg(skills []*activeSkill) *Cfg {
	for _, s := range skills {
		if s.main {
			return s.cfg
		}
	}
	return nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func skillInfo(skills []*activeSkill, set *pobcode.SkillSet) []SkillInfo {
	out := make([]SkillInfo, 0, len(skills))
	perGroup := map[*pobcode.SocketGroup]int{}
	for _, s := range skills {
		info := SkillInfo{Name: s.active.ge.Name, Level: s.active.level, Quality: s.active.quality, Disabled: s.disabled}
		if set != nil {
			for i := range set.Groups {
				if s.group == &set.Groups[i] {
					perGroup[s.group]++
					info.Group, info.GroupSkill = i+1, perGroup[s.group]
				}
			}
		}
		list, _ := minionList(s)
		info.HasMinion = len(list) > 0
		for _, b := range s.buffs {
			info.Buffs = append(info.Buffs, b.typ+":"+b.name)
		}
		if s.mods != nil {
			info.AuraEffectInc = s.mods.SumCfg(s.cfg, "INC", auraEffectNames...)
			info.AuraEffectMore = s.mods.MoreCfg(s.cfg, auraEffectNames...)
		}
		out = append(out, info)
	}
	return out
}
