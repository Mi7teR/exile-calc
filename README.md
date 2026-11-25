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
| `modparser` | Mod line → ModDB modifiers, using PoB's own pattern tables; 97.8% of PoB's ModCache lines, 0 wrong | in progress |
| `engine` | PoB's `calcs`: attributes, life, mana, resists, EHP, main skill DPS, ailments, minions, plus combat DPS against a boss and a pack | matches PoB on the golden corpus |

The engine is checked against headless PoB on a [golden corpus](testdata/golden)
of 89 real 3.30 builds: every compared defence stat and the main skill's DPS
stats match within 0.5% on all 89, minions on all 119. Where PoB is wrong
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

`cr.Assumptions` lists what the result assumes (hitbox, pack layout, shape),
in Russian for now.

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
