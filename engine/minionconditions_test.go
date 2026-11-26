package engine

import (
	"maps"
	"slices"
	"testing"

	"github.com/Mi7teR/exile-calc/gamedata"
	"github.com/Mi7teR/exile-calc/itemtext"
)

func TestMinionEquipmentConditions(t *testing.T) {
	item := func(typ, sub string, weapon bool) *itemtext.Item {
		b := &gamedata.Base{Type: typ, SubType: sub}
		if weapon {
			b.Weapon = &gamedata.WeaponStats{}
		}
		return &itemtext.Item{Base: b}
	}
	for _, c := range []struct {
		name   string
		items  map[string]*itemtext.Item
		w1, w2 string
		want   []string
	}{
		{"monster weapon", nil, "One Handed Sword", "",
			[]string{"OffHandIsEmpty", "UsingMeleeWeapon", "UsingOneHandedWeapon", "UsingSword"}},
		{"unarmed", nil, "None", "",
			[]string{"OffHandIsEmpty", "Unarmed", "Unencumbered"}},
		{"guardian staff and armour", map[string]*itemtext.Item{
			"Weapon 1": item("Staff", "Warstaff", true), "Body Armour": item("Body Armour", "", false),
		}, "Staff", "",
			[]string{"OffHandIsEmpty", "UsingBody Armour", "UsingMeleeWeapon", "UsingStaff", "UsingTwoHandedWeapon"}},
		{"sword and shield", map[string]*itemtext.Item{
			"Weapon 1": item("One Handed Sword", "", true), "Weapon 2": item("Shield", "", false),
		}, "One Handed Sword", "",
			[]string{"UsingMeleeWeapon", "UsingOneHandedWeapon", "UsingShield", "UsingSword"}},
		{"thrusting and one handed sword", map[string]*itemtext.Item{
			"Weapon 1": item("One Handed Sword", "Thrusting", true), "Weapon 2": item("One Handed Sword", "", true),
		}, "One Handed Sword", "One Handed Sword",
			[]string{"DualWielding", "UsingMeleeWeapon", "UsingOneHandedWeapon", "UsingSword"}},
		{"claw and dagger", map[string]*itemtext.Item{
			"Weapon 1": item("Claw", "", true), "Weapon 2": item("Dagger", "", true),
		}, "Claw", "Dagger",
			[]string{"DualWielding", "UsingClaw", "UsingDagger", "UsingMeleeWeapon", "UsingOneHandedWeapon", "WieldingDifferentWeaponTypes"}},
	} {
		cond := map[string]bool{}
		minionEquipmentConditions(cond, c.items, c.w1, c.w2, [2]bool{})
		if got := slices.Sorted(maps.Keys(cond)); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

// TestAnimatedGuardianStaffBlock: the Animated Guardian's staff sets
// UsingStaff on the minion, so the staff's block chance applies
// (a2nRZLW9ZVuu: 36, PoB's value).
func TestAnimatedGuardianStaffBlock(t *testing.T) {
	ms, err := newCalc(t).CalcMinions(corpusBuild(t, "a2nRZLW9ZVuu"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range ms {
		if m.Minion == "Animated Guardian" {
			if got := m.Stats["BlockChance"]; got != 36 {
				t.Errorf("Animated Guardian BlockChance %v, want 36", got)
			}
			return
		}
	}
	t.Fatal("no Animated Guardian")
}

// TestMinionCombatConditions: doActorAttribsConditions' mode_combat and
// mode_effective parts for env.minion (CalcPerform.lua:246-296, 400-410):
// the minion's main skill sets its "recently" conditions; the scorch,
// brittle and sap checks read the minion's mods with
// env.player.mainSkill's skillCfg, and exposure the summoning skill's mods
// or the minion's.
func TestMinionCombatConditions(t *testing.T) {
	pm := triggerTestSkill("Raise Spectre")
	pm.cfg = &Cfg{SkillName: "Raise Spectre"}
	pm.mods.NewMod("ColdExposureChance", "BASE", 25, "Gem")
	ms := triggerTestSkill("Minion Melee")
	ms.flags["attack"], ms.flags["hit"], ms.flags["minion"] = true, true, true
	mdb := NewModDB()
	buffMode(mdb, "")
	mdb.NewMod("FireExposureChance", "BASE", 10, "Minion")
	mdb.Add(Mod{Name: "CritAlwaysAltAilments", Type: "FLAG", Value: true, Source: "Minion"})
	combatConditions(mdb, ms, pm)
	want := []string{"AttackedRecently", "Buffed", "CanApplyColdExposure", "CanApplyFireExposure", "CanInflictBrittle",
		"CanInflictSap", "CanInflictScorch", "Combat", "Effective", "HitRecently", "UsedMinionSkillRecently"}
	if got := slices.Sorted(maps.Keys(mdb.conditions)); !slices.Equal(got, want) {
		t.Errorf("conditions %v, want %v", got, want)
	}

	// NeverCrit on env.player.mainSkill's skillCfg stops the alternate
	// ailments; a triggered main skill sets no "recently" use conditions.
	mdb = NewModDB()
	buffMode(mdb, "")
	mdb.Add(Mod{Name: "CritAlwaysAltAilments", Type: "FLAG", Value: true, Source: "Minion"})
	mdb.Add(Mod{Name: "NeverCrit", Type: "FLAG", Value: true, Source: "Minion",
		Tags: []map[string]any{{"type": "SkillName", "skillName": "Raise Spectre"}}})
	ms.data.set("triggered", true)
	combatConditions(mdb, ms, pm)
	want = []string{"Buffed", "CanApplyColdExposure", "Combat", "Effective", "HitRecently"}
	if got := slices.Sorted(maps.Keys(mdb.conditions)); !slices.Equal(got, want) {
		t.Errorf("triggered, NeverCrit: conditions %v, want %v", got, want)
	}
}

// TestMinionMisc: the parts of doActorMisc that differ for env.minion
// (CalcPerform.lua 646-978): Cruelty from env.player.mainSkill, the
// parent's fortification, the player's Silver Flask with the minion's own
// flask effect, and the second enemy modifier pass.
func TestMinionMisc(t *testing.T) {
	pm := triggerTestSkill("Raise Spectre")
	ms := triggerTestSkill("Minion Melee")
	ms.mods.Add(Mod{Name: "Cruelty", Type: "FLAG", Value: true})
	db := NewModDB()
	mdb := NewModDB()
	mdb.owner, mdb.enemy = db, NewModDB()
	minionBaseMods(mdb)
	actorMisc(mdb, ms, pm, false, -1)
	if mdb.multipliers["Cruelty"] != 0 {
		t.Error("the minion's own Cruelty counted; PoB reads env.player.mainSkill")
	}
	pm.mods.Add(Mod{Name: "Cruelty", Type: "FLAG", Value: true})
	db.output["FortificationStacks"] = 12
	mdb.Add(Mod{Name: "YourFortifyEqualToParent", Type: "FLAG", Value: true})
	mdb.NewMod("MaximumFortification", "BASE", 20, "Base")
	mdb.Add(Mod{Name: "Onslaught", Type: "FLAG", Value: true})
	silver, inc := silverOnslaught(mdb, []silverFlask{{localInc: 20}, {localInc: 10, magic: true}})
	mdb.NewMod("FlaskEffect", "INC", 30, "Minion")
	mdb.NewMod("MagicUtilityFlaskEffect", "INC", 50, "Minion")
	if !silver || inc != 0.2 {
		t.Fatalf("silverOnslaught before the flask mods: %v %v, want true 0.2", silver, inc)
	}
	if s, i := silverOnslaught(mdb, nil); s || i != -1 {
		t.Errorf("silverOnslaught without a Silver Flask: %v %v, want false -1", s, i)
	}
	silver, inc = silverOnslaught(mdb, []silverFlask{{localInc: 20}, {localInc: 10, magic: true}})
	if !silver || inc != 0.9 {
		t.Errorf("silverOnslaught %v %v, want true 0.9", silver, inc)
	}
	actorMisc(mdb, ms, pm, silver, inc)
	if mdb.multipliers["Cruelty"] != 40 {
		t.Errorf("Cruelty %v, want 40", mdb.multipliers["Cruelty"])
	}
	if !mdb.conditions["Fortified"] || mdb.output["FortificationStacks"] != 20 {
		t.Errorf(
			"fortification: Fortified %v stacks %v, want true 20",
			mdb.conditions["Fortified"],
			mdb.output["FortificationStacks"],
		)
	}
	// floor(20 * (1 + 0.9 + 0)).
	if got := mdb.SumCfg(&Cfg{Flags: mfAttack}, "INC", "Speed"); got != 38 {
		t.Errorf("Onslaught attack speed %v, want 38", got)
	}

	// The second "Process enemy modifiers" pass adds only the minion's
	// EnemyModifier mods the first pass did not take.
	enemyMod := func(name string) Mod {
		return Mod{Name: "EnemyModifier", Type: "LIST", Source: "Minion",
			Value: map[string]any{"mod": map[string]any{"name": name, "type": "INC", "value": 10.0}}}
	}
	mdb.Add(enemyMod("CurseEffectOnSelf"))
	applied := enemyModifiers{}
	applied.mark(mdb)
	mdb.Add(enemyMod("DamageTaken"))
	applied.apply(mdb)
	if mdb.enemy.Sum("INC", "CurseEffectOnSelf") != 0 || mdb.enemy.Sum("INC", "DamageTaken") != 10 {
		t.Errorf("enemy mods: CurseEffectOnSelf %v DamageTaken %v, want 0 10",
			mdb.enemy.Sum("INC", "CurseEffectOnSelf"), mdb.enemy.Sum("INC", "DamageTaken"))
	}
}

// TestNecromanticAegis: with the keystone the shield's mods go to the
// minions (env.aegisModList, the shield as their "Weapon 3") and the shield
// leaves the player (CalcSetup.lua:1133-1150, CalcPerform.lua:1203-1206,
// 1324-1326). 0BL0JzvGlFo_ with a path to Necromantic Aegis; PoB: the
// Lightning Golem's block 0 to 26 and life 8601 to 8757, the player's
// block 26 to 0.
func TestNecromanticAegis(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "0BL0JzvGlFo_")
	spec := b.ActiveSpec()
	path := pathToNode(c, spec.Nodes, 45175)
	if path == nil {
		t.Fatal("no path to Necromantic Aegis")
	}
	spec.Nodes = append(spec.Nodes, path...)
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if res.DB.aegis == nil {
		t.Fatal("no aegis shield")
	}
	if got := res.Stats["BlockChance"]; got != 0 {
		t.Errorf("player BlockChance %v, want 0", got)
	}
	mins, err := c.CalcMinions(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mins {
		if m.Minion == "Lightning Golem" {
			if m.Stats["BlockChance"] != 26 || m.Stats["Life"] != 8757 {
				t.Errorf(
					"Lightning Golem BlockChance %v Life %v, want 26 8757",
					m.Stats["BlockChance"],
					m.Stats["Life"],
				)
			}
			return
		}
	}
	t.Fatal("no Lightning Golem")
}

// pathToNode is the shortest path (outside the ascendancies) from the
// allocated nodes to target, without the allocated end; nil if none.
func pathToNode(c *Calculator, alloc []int, target int) []int {
	tree := c.data.Tree
	prev := map[int]int{}
	queue := slices.Clone(alloc)
	slices.Sort(queue)
	for _, id := range queue {
		prev[id] = -1
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if id == target {
			var p []int
			for x := id; prev[x] != -1; x = prev[x] {
				p = append(p, x)
			}
			return p
		}
		n := tree.Nodes[id]
		if n == nil {
			continue
		}
		next := slices.Concat(n.Out, n.In)
		slices.Sort(next)
		for _, o := range next {
			if on := tree.Nodes[o]; on == nil || on.AscendancyName != "" {
				continue
			}
			if _, ok := prev[o]; !ok {
				prev[o] = id
				queue = append(queue, o)
			}
		}
	}
	return nil
}
