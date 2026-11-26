# poe-lib

Pure Go port of the [Path of Building Community](https://github.com/PathOfBuildingCommunity/PathOfBuilding)
calculation engine and game data for Path of Exile 1 (pinned to PoB v2.68.1).
Decode a PoB export code, parse items and mods, and get the same defence and
offence numbers PoB shows, with no Lua or LuaJIT in the build and no
dependencies outside the standard library.

```bash
go get github.com/Mi7teR/poe-lib
```

## Packages

| Package | What it does | Status |
| --- | --- | --- |
| `pobcode` | PoB export code ↔ `Build`: items by slot, gems, passive trees, config | done |
| `gamedata` | Embedded game data: bases, mods with tiers, gems, uniques, passive tree, minions | done |
| `itemtext` | Item text (game Ctrl+C, Ctrl+Alt+C, PoB format) → base, sockets, enchants, implicits, explicits | done |
| `modparser` | Mod line → ModDB modifiers, using PoB's own pattern tables; all 19265 lines PoB's ModCache parses fully, 0 wrong | done |
| `engine` | PoB's `calcs`: attributes, life, mana, resists, EHP, main skill DPS, ailments, minions, plus combat DPS against a boss and a pack | matches PoB on the golden corpus |

The engine is checked against headless PoB on a [golden corpus](testdata/golden)
of 145 real 3.30 builds (140 public, five of them Raise Spectre builds;
5 synthetic: two Party-tab, one linked-minion and two Oak-bandit variants
of public builds). Every compared defence stat matches within 0.5% on all
145; the main skill's DPS stats and Full DPS on all 145, and minion
defence stats and minion DPS on all 241 oracle minions. Where PoB is wrong
and the engine follows the game instead, the difference is recorded in
[quirks.json](testdata/golden/quirks.json).

## Usage

### Stats of a PoB build

```go
b, err := pobcode.FromCode(code) // the string from PoB's "Export" or pobb.in
if err != nil {
	return err
}
calc, err := engine.New() // load once; safe for concurrent use
if err != nil {
	return err
}
res, err := calc.Calc(b)
if err != nil {
	return err
}
fmt.Printf("Life %.0f, EHP %.0f, DPS %.0f\n",
	res.Stats["Life"], res.Stats["TotalEHP"], res.Stats["CombinedDPS"])
```

`res.Stats` uses PoB's output names (`Life`, `FireResist`, `TotalDPS`,
`CombinedDPS`, ...). The engine never guesses silently: mod lines it could
not parse are in `res.Unparsed`, parts it could not compute are in
`res.Incomplete`.

`res.Skills` lists the active skills with gem level, quality and buffs;
`calc.CalcMinions(b)` computes each minion skill as the main skill.
Like PoB, `Calc` gives skills granted by the tree or items a socket group
when the build has none and drops granted groups nothing grants any more.
`SkillInfo.Group` (and `SkillDPS.Group`) still index your build's groups:
an added group is `len(groups)+k`, so `b.MainSocketGroup = info.Group`
selects that skill. `res.WithMainSkill(info.Group, info.GroupSkill)` returns
a ready copy with that main skill; `res.Build()` is the build with those
groups added and removed (its own indexes differ from `Group`).
The first `Calc` of a process parses mod lines; later calls reuse a
process-wide parse cache and are much faster.

### Full DPS

`calc.FullDPS(b)` is PoB's Full DPS over the socket groups marked "Include in
Full DPS": `FullDPS`, `FullDotDPS` and one `SkillDPS` per skill, minion,
mirage and DoT total (a build with no such group gives 0 and no entries).

```go
fd, err := calc.FullDPS(b)
if err != nil {
	return err
}
fmt.Printf("Full DPS %.0f (DoT %.0f)\n", fd.FullDPS, fd.FullDotDPS)
for _, s := range fd.Skills {
	fmt.Printf("  %s x%.0f: %.0f\n", s.Name, s.Count, s.DPS)
}
```

### Compare an item against a build

Like PoB's item tooltip: compute the build once, then ask what an item
copied from the game would change in a slot.

```go
comparer, err := calc.NewComparer(b) // one Calc; keep it while the build is loaded
if err != nil {
	return err
}
r, err := comparer.Item("Ring 1", clipboardText) // game Ctrl+C text or PoB item text
if err != nil {
	return err // unknown slot, item that does not fit the slot, or text that is not an item
}
for _, stat := range []string{"Life", "FireResist", "ColdResist", "LightningResist", "TotalEHP", "CombinedDPS", "FullDPS"} {
	if d, ok := r.Delta[stat]; ok {
		fmt.Printf("%s %+.0f (%.0f -> %.0f)\n", stat, d, r.Before[stat], r.After[stat])
	}
}
```

`Delta` holds only the stats that changed, unrounded; `FullDPS` is there
when the build has skills in Full DPS. `r.Unparsed` and `r.Incomplete` list
the item's mod lines the engine did not understand and the parts it left
out (`After` and `Delta` may then be incomplete). A two-hander in `"Weapon 1"`, or a bow
unless `"Weapon 2"` holds a quiver, empties `"Weapon 2"` (`r.Replaced` says so).
The slot is a PoB slot name: `"Weapon 1"`, `"Weapon 2"`, `"Weapon 1 Swap"`,
`"Weapon 2 Swap"`, `"Helmet"`, `"Body Armour"`, `"Gloves"`, `"Boots"`,
`"Amulet"`, `"Ring 1"`..`"Ring 3"`, `"Belt"`, `"Flask 1"`..`"Flask 5"`,
`"Graft 1"`, `"Graft 2"`, the Wickerman slots (`"Head"`, `"Body"`, `"Arm"`,
`"Leg"`), abyssal sockets (`"Helmet Abyssal Socket 1"`) and tree jewel
sockets by node id (`"Jewel 26725"`), the sockets inside cluster jewels
included. The item must fit the slot as in PoB's slot lists, or `Item`
returns an error wrapping `engine.ErrItemNotValidForSlot`: a ring only in a
ring slot, a shield or one-hander in `"Weapon 2"` only beside a one-hander
(wands only beside a wand) or no weapon, a quiver only beside a bow, an
abyss jewel in an abyssal or tree socket, a cluster jewel only in a cluster
socket of its size or larger, flasks and tinctures in flask slots. A slot
PoB hides for the build (`"Ring 3"` without a "+1 Ring Slot" passive, the
Graft slots off a 3.27 tree, the Wickerman slots off a 3.30 tree) gives an
error wrapping `engine.ErrSlotNotAvailable`.
When the build uses Full DPS, each `Item` call runs one Calc per Full DPS
skill, so it is slower for builds with many of them. `Item` is safe to call from several
goroutines; the build must not be changed while the Comparer is in use.

Skills granted by items count as they do in the game: a compared item that
grants a skill (a "Curse Enemies with ... on Hit" ring, say) adds it, and the
skill of the item it replaces goes. PoB's tooltip differs here: it skips the
granted skill block, so a new item's skill is not added and the replaced
item's skill stays.

### Combat DPS against a boss and a pack

`Combat` takes PoB's single-target numbers and lays the main skill's shape
(area, projectiles, chains, mines, totems, ...) over a boss hitbox and a pack
of mobs:

```go
cr, err := calc.Combat(b, engine.CombatOptions{
	Boss: engine.BossLarge,
	Pack: engine.PackParams{Count: 15}, // zero fields take the defaults
})
if err != nil {
	return err
}
fmt.Printf("%s: boss %.0f (PoB %.0f), pack %.0f\n",
	cr.Skill, cr.Boss.Sustained, cr.Boss.PoB, cr.Pack.Sustained)
for name, dps := range cr.Pack.Components { // "Hit", "Ignite", "Poison", ...
	fmt.Println(name, dps)
}
```

`cr.Assumptions` lists what the result assumes (hitbox, pack layout, shape).

### Read a build without computing it

```go
spec := b.ActiveSpec()                      // passive tree: class, nodes, masteries
set := b.ActiveItems()                      // equipped items
weapon := b.Item(set.SlotItem("Weapon 1"))
```

### Parse an item copied from the game

```go
data, err := gamedata.PoE1()
if err != nil {
	return err
}
item, err := itemtext.New(data).Parse(clipboard) // Ctrl+C or Ctrl+Alt+C text
if err != nil {
	return err
}
fmt.Println(item.Rarity, item.Name, item.BaseName, item.ItemLevel)
for _, m := range item.ActiveExplicits() {
	fmt.Println(m.Text)
}
```

### Parse a mod line

```go
p, err := modparser.PoE1()
if err != nil {
	return err
}
r := p.Parse("10% increased Attack Speed while Fortified")
for _, m := range r.Mods {
	fmt.Println(m.Name, m.Type, m.Value, m.Tags)
	// Speed INC 10 [map[type:Condition var:Fortified]]
}
```

`r.Parsed()` is false when part of the line was not understood (`r.Extra`).

## Timeless jewels

Timeless jewel tables are too large to embed. `go run ./cmd/gamedatagen`
downloads them into the user cache, where the engine finds them; or set
`POE_LIB_TIMELESS_DIR` to PoB's `Data/TimelessJewelData`. Without them a
build with a timeless jewel is still computed, and the jewel is reported in
`res.Incomplete`.

## Development

```bash
go test ./...
make test | lint | bench | dump-check | luaport                # test, lint, benchmarks, stats-dump diff, Lua port check
go run ./cmd/gamedatagen                                  # regenerate gamedata/poe1 from the pinned PoB tag
go run ./cmd/enginediff -builds ID1,ID2 -stats Life,Armour # engine vs headless PoB, mod by mod
go run ./cmd/enginediff -combat -builds ID1                # combat DPS report
```

`cmd/gamedatagen` reads PoB's Lua data files as data with `internal/luadata`
(no Lua runs). Not exported: `Data/Uniques/Special/Generated.lua` (a program
that builds Megalomaniac, Forbidden jewels and similar uniques). The
reference stats come from headless PoB in
[poe-pob-oracle](https://github.com/Mi7teR/poe-pob-oracle).

## License

MIT, see [LICENSE](LICENSE). It includes the Path of Building Community notice.
