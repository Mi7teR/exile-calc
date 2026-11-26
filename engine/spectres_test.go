package engine

import (
	"math"
	"slices"
	"testing"

	"github.com/Mi7teR/poe-lib/gamedata"
	"github.com/Mi7teR/poe-lib/pobcode"
)

func TestSpectreModList(t *testing.T) {
	listMod := func(name, inner string) map[string]any {
		return map[string]any{"name": name, "type": "LIST", "value": map[string]any{
			"mod": map[string]any{"name": inner, "type": "BASE", "value": 5.0},
		}}
	}
	e := spectreEffects{curseLimit: 1}
	e.addModList([]map[string]any{
		listMod("AllyModifier", "Ally"),
		listMod("MinionModifier", "Minion"),
		listMod("PlayerModifier", "Player"),
		{"name": "EnemyCurseLimit", "type": "BASE", "value": 3.0},
		listMod("PlayerModifier", "AfterLimit"), // CalcPerform.lua breaks out at EnemyCurseLimit
	}, "Spectre:X")
	names := func(l []Mod) []string {
		var out []string
		for _, m := range l {
			out = append(out, m.Name+"/"+m.Source)
		}
		return out
	}
	if got, want := names(e.player), []string{"Ally/Spectre:X", "Player/Spectre:X"}; !slices.Equal(got, want) {
		t.Errorf("player mods %v, want %v", got, want)
	}
	if got, want := names(e.minion), []string{"Ally/Spectre:X", "Minion/Spectre:X"}; !slices.Equal(got, want) {
		t.Errorf("minion mods %v, want %v", got, want)
	}
	if e.curseLimit != 4 {
		t.Errorf("curse limit %d, want 4", e.curseLimit)
	}
}

func TestMonsterTagMatches(t *testing.T) {
	md := &gamedata.Minion{MonsterTags: []string{"construct", "undead"}}
	for _, c := range []struct {
		md   *gamedata.Minion
		tag  map[string]any
		want bool
	}{
		{md, map[string]any{"monsterTag": "Construct"}, true},
		{md, map[string]any{"monsterTag": "Beast"}, false},
		{md, map[string]any{"monsterTag": "Construct", "neg": true}, false},
		{md, map[string]any{"monsterTagList": []any{"beast", "Undead"}}, true},
		{nil, map[string]any{"monsterTag": "Beast", "neg": true}, false}, // not a minion: never applies
	} {
		if got := monsterTagMatches(c.md, c.tag); got != c.want {
			t.Errorf("%v on %v: %v, want %v", c.tag, c.md, got, c.want)
		}
	}
}

// TestRaiseSpectreUsesSpectreList checks Raise Spectre's minion and the
// curses its spectre puts on the enemy (Perfect Blasphemer's Vulnerability),
// and that without the build's spectre list it has no minion.
func TestRaiseSpectreUsesSpectreList(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "Wpo4TuYfRXFT")
	ms, err := c.CalcMinions(b)
	if err != nil {
		t.Fatal(err)
	}
	var spectres []string
	for _, m := range ms {
		if m.Skill == "Raise Spectre" {
			spectres = append(spectres, m.Minion)
		}
	}
	if want := []string{"Perfect Blasphemer", "Perfect Forest Warrior"}; !slices.Equal(spectres, want) {
		t.Errorf("spectres %v, want %v", spectres, want)
	}
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	cursed := false
	for _, r := range res.DB.Enemy().Tabulate("PhysicalDamageTaken") {
		cursed = cursed || r.Source == "Skill:TalismanT1Vulnerability"
	}
	if !cursed {
		t.Error("the spectre's Vulnerability is not on the enemy")
	}

	b.SpectreList = nil
	res, err = c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Skills {
		if s.Name == "Raise Spectre" && s.HasMinion {
			t.Error("Raise Spectre has a minion without a spectre list")
		}
	}
}

const (
	geofriHigh = "Metadata/Monsters/LeagueAzmeri/SpecialCorpses/GeofriHigh"
	vikingHigh = "Metadata/Monsters/LeagueAzmeri/SpecialCorpses/VikingHigh"
)

// vulnerability is the enemy's PhysicalDamageTaken from the Perfect
// Blasphemer's Vulnerability (0 when it is not on the enemy).
func vulnerability(res *Result) float64 {
	for _, r := range res.DB.Enemy().Tabulate("PhysicalDamageTaken") {
		if r.Source == "Skill:TalismanT1Vulnerability" {
			v, _ := num(r.Value)
			return v
		}
	}
	return 0
}

// withMinionCurseEffect adds "Minions have 50% increased Curse Effect" as a
// custom mod of the active config set.
func withMinionCurseEffect(b *pobcode.Build) {
	for i := range b.ConfigSets {
		if b.ConfigSets[i].ID == b.ActiveConfigSetID || len(b.ConfigSets) == 1 {
			b.ConfigSets[i].CustomMods = append(b.ConfigSets[i].CustomMods,
				pobcode.CustomModBlock{Title: "Test", Enabled: true, Text: "Minions have 50% increased Curse Effect"})
		}
	}
}

// blasphemerMain is the Wpo4TuYfRXFT build with the Raise Spectre that
// raises the Perfect Blasphemer as the main skill.
func blasphemerMain(t *testing.T, c *Calculator, b *pobcode.Build) *pobcode.Build {
	t.Helper()
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Skills {
		if s.Name != "Raise Spectre" {
			continue
		}
		mb := withMainSkill(res.build, res.buildGroup(s.Group), s.GroupSkill)
		r, merr := c.Calc(mb)
		if merr != nil {
			t.Fatal(merr)
		}
		if r.Minion != nil && r.Minion.Minion == "Perfect Blasphemer" {
			return mb
		}
	}
	t.Fatal("no Raise Spectre raises the Perfect Blasphemer")
	return nil
}

// TestMainSpectreCurseEffect: with Raise Spectre as the main skill, the
// player's MinionModifier curse effect scales the spectre's curse, as PoB
// does for env.minion (CalcPerform.lua:1991).
func TestMainSpectreCurseEffect(t *testing.T) {
	c := newCalc(t)
	main := blasphemerMain(t, c, corpusBuild(t, "Wpo4TuYfRXFT"))
	res, err := c.Calc(main)
	if err != nil {
		t.Fatal(err)
	}
	if v := vulnerability(res); v != 25 {
		t.Fatalf("Vulnerability %g, want 25", v)
	}
	withMinionCurseEffect(main)
	if res, err = c.Calc(main); err != nil {
		t.Fatal(err)
	}
	// 25 * 1.5, truncated as ScaleAddList does integer values.
	if v := vulnerability(res); v != 37 {
		t.Errorf("Vulnerability with 50%% minion curse effect %g, want 37", v)
	}
}

// TestOtherMinionCurseEffect: a minion that is not the main skill's reads
// its own curse effect mods too (the game; PoB builds it on a bare ModDB,
// so there it would stay 25). 25 * 1.5, truncated.
func TestOtherMinionCurseEffect(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "Wpo4TuYfRXFT") // the main skill is the player's Smite
	withMinionCurseEffect(b)
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if res.Minion != nil {
		t.Fatalf("main skill has a minion %q", res.Minion.Minion)
	}
	if v := vulnerability(res); v != 37 {
		t.Errorf("Vulnerability %g, want 37", v)
	}
}

func TestMinionSkillIndex(t *testing.T) {
	for _, c := range []struct{ set, n, want int }{
		{0, 3, 0}, {1, 3, 0}, {2, 3, 1}, {3, 3, 2}, {9, 3, 2}, {-1, 3, 0}, {2, 0, 0},
	} {
		s := &activeSkill{gem: &pobcode.Gem{SkillMinionSkill: c.set}}
		if got := minionSkillIndex(s, c.n); got != c.want {
			t.Errorf("skillMinionSkill %d of %d: %d, want %d", c.set, c.n, got, c.want)
		}
	}
	if got := minionSkillIndex(&activeSkill{}, 3); got != 0 {
		t.Errorf("no gem: %d, want 0", got)
	}
}

func TestDamageFixup(t *testing.T) {
	c := newCalc(t)
	mdb := NewModDB()
	mdb.minionData = &gamedata.Minion{Name: "X", DamageFixup: 0.33}
	eff := &skillEffect{id: "Melee", ge: c.data.Skills.Effects["Melee"], level: 1, actorLevel: 50}
	ms := c.createActiveSkill(eff, nil, nil, [2]string{"None"}, nil)
	ms.cfg = &Cfg{}
	c.buildSkillMods(mdb, ms, &Result{})
	attack := &Cfg{Flags: mfAttack}
	if got := ms.mods.MoreCfg(attack, "Damage"); math.Abs(got-0.67) > 1e-9 {
		t.Errorf("attack damage more %g, want 0.67", got)
	}
	if got := ms.mods.MoreCfg(attack, "Speed"); math.Abs(got-1.33) > 1e-9 {
		t.Errorf("attack speed more %g, want 1.33", got)
	}
	if got := ms.mods.MoreCfg(&Cfg{Flags: mfSpell}, "Damage"); got != 1 {
		t.Errorf("spell damage more %g, want 1", got)
	}
}

func TestHallowingFlame(t *testing.T) {
	db := NewModDB()
	hallowingFlame(db, nil)
	if len(db.List(nil, "ExtraAura")) != 0 {
		t.Fatal("ExtraAura without CanInflictHallowingFlame")
	}
	db.Add(Mod{Name: "Condition:CanInflictHallowingFlame", Type: "FLAG", Value: true})
	db.NewMod("HallowingFlameMagnitude", "INC", 30, "Test")
	hallowingFlame(db, nil)
	l := db.List(nil, "ExtraAura")
	if len(l) != 1 {
		t.Fatalf("%d ExtraAura mods, want 1", len(l))
	}
	m := modFromMap(l[0].Value.(map[string]any)["mod"].(map[string]any), "")
	if v, _ := num(m.Value); m.Name != "PhysicalDamageGainAsFire" || v != 32 { // floor(25 * 1.3)
		t.Errorf("ExtraAura %s %v, want PhysicalDamageGainAsFire 32", m.Name, m.Value)
	}
}

// TestMinionResistEqualToYours: "enemy resistance equal to yours" is the
// player's flag and resistance also for a minion's hits, else a party
// member's (CalcOffence.lua:498-501).
func TestMinionResistEqualToYours(t *testing.T) {
	player, enemy, minion := NewModDB(), NewModDB(), NewModDB()
	enemy.NewMod("LightningResist", "BASE", 50, "Base")
	player.enemy, minion.enemy, minion.owner = enemy, enemy, player
	player.partyMembers = NewModDB()
	if got := enemyResist(minion, nil, "Lightning"); got != 50 {
		t.Errorf("no flag: %g, want 50", got)
	}
	player.partyMembers.Add(Mod{Name: "EnemyLightningResistEqualToYours", Type: "FLAG", Value: true})
	player.partyMembers.output["LightningResist"] = 10
	if got := enemyResist(minion, nil, "Lightning"); got != 10 {
		t.Errorf("party member's flag: %g, want 10", got)
	}
	player.Add(Mod{Name: "EnemyLightningResistEqualToYours", Type: "FLAG", Value: true})
	player.output["LightningResist"] = -60
	minion.output["LightningResist"] = 75
	if got := enemyResist(minion, nil, "Lightning"); got != -60 {
		t.Errorf("player's flag: %g, want the player's -60", got)
	}
}

// TestMinionModifiersBeforeBuffs: env.minion takes the main skill's
// MinionModifier mods before the buffs (CalcPerform.lua:1991), so Summon
// Carrion Golem's buff reaches the skeletons once, through the buff.
func TestMinionModifiersBeforeBuffs(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "JtugGaLoWqgT")
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range res.Skills {
		if s.Name != "Summon Skeletons" {
			continue
		}
		r, merr := c.Calc(withMainSkill(res.build, res.buildGroup(s.Group), s.GroupSkill))
		if merr != nil {
			t.Fatal(merr)
		}
		fromBuff := func(l []ListEntry) int {
			n := 0
			for _, e := range l {
				if e.Mod.Source == "Skill:SummonCarrionGolem" {
					n++
				}
			}
			return n
		}
		if n := fromBuff(r.main.minionModifiers); n != 0 {
			t.Errorf("%d Carrion Golem buff mods read with the skill's MinionModifier mods, want 0", n)
		}
		if n := fromBuff(r.DB.List(nil, "MinionModifier")); n == 0 {
			t.Error("the Carrion Golem buff is not on the player")
		}
		return
	}
	t.Fatal("no Summon Skeletons")
}

// setSpectres points every Raise Spectre gem at one spectre.
func setSpectres(b *pobcode.Build, id string) {
	set := b.ActiveSkills()
	for g := range set.Groups {
		for k := range set.Groups[g].Gems {
			if gem := &set.Groups[g].Gems[k]; gem.NameSpec == "Raise Spectre" {
				gem.SkillMinion = id
			}
		}
	}
}

// TestSpectreNotRaised: a spectre in the library that no gem raises gives
// neither its buffs nor its curses (activeSpectreList, CalcPerform.lua:2243).
func TestSpectreNotRaised(t *testing.T) {
	c := newCalc(t)
	b := corpusBuild(t, "Wpo4TuYfRXFT")
	setSpectres(b, vikingHigh)
	res, err := c.Calc(b)
	if err != nil {
		t.Fatal(err)
	}
	if v := vulnerability(res); v != 0 {
		t.Errorf("unraised Perfect Blasphemer curses the enemy: %g", v)
	}
	onslaught := func(res *Result) bool {
		for _, r := range res.DB.Tabulate("Condition:Onslaught") {
			if r.Source == "Spectre:Perfect Forest Warrior" {
				return true
			}
		}
		return false
	}
	if !onslaught(res) {
		t.Error("raised Perfect Forest Warrior gives no Onslaught")
	}
	b = corpusBuild(t, "Wpo4TuYfRXFT")
	setSpectres(b, geofriHigh)
	if res, err = c.Calc(b); err != nil {
		t.Fatal(err)
	}
	if onslaught(res) {
		t.Error("unraised Perfect Forest Warrior gives Onslaught")
	}
	if vulnerability(res) == 0 {
		t.Error("raised Perfect Blasphemer does not curse the enemy")
	}
}

// TestMinionCurseLimit: minionCurses are slotted after the player's
// curses within their own limit, and a higher priority one displaces a
// player curse ("Assign curses to slots", CalcPerform.lua:3258).
func TestMinionCurseLimit(t *testing.T) {
	c := newCalc(t)
	run := func(minionPriority float64) []string {
		db := NewModDB()
		db.enemy = NewModDB()
		db.NewMod("EnemyCurseLimit", "BASE", 1, "Base")
		curses := []*enemyCurse{
			{name: "Despair", priority: 200,
				mods: []Mod{{Name: "ChaosResist", Type: "BASE", Value: -20.0, Source: "Despair"}}},
			{name: "Vulnerability", priority: minionPriority, minion: true,
				mods: []Mod{{Name: "PhysicalDamageTaken", Type: "INC", Value: 25.0, Source: "Vulnerability"}}},
		}
		c.applyCurses(db, nil, curses, &Result{})
		var on []string
		for _, n := range []string{"ChaosResist", "PhysicalDamageTaken"} {
			for _, r := range db.enemy.Tabulate(n) {
				on = append(on, r.Source)
			}
		}
		return on
	}
	if got := run(300); !slices.Equal(got, []string{"Vulnerability"}) {
		t.Errorf("higher priority minion curse: %v, want [Vulnerability]", got)
	}
	if got := run(100); !slices.Equal(got, []string{"Despair"}) {
		t.Errorf("lower priority minion curse: %v, want [Despair]", got)
	}
}
