# poe-lib

Go libraries for Path of Exile game data, ported from
[Path of Building Community](https://github.com/PathOfBuildingCommunity/PathOfBuilding)
(pinned to v2.68.1). Pure Go: no Lua code and no LuaJIT in the build.

| Package | What it does | Status |
| --- | --- | --- |
| `pobcode` | PoB export code ↔ `Build`: items by slot, gems, passive trees, config | done |
| `gamedata` | Embedded game data: bases, mods with tiers, gems, uniques, passive tree | done |
| `itemtext` | Item text (game Ctrl+C, Ctrl+Alt+C, PoB format) → base, sockets, enchants, implicits, explicits | done |
| `modparser` | Mod text → ModDB modifiers, PoB's own pattern tables; matches PoB on 95.7% of ModCache lines, 0 wrong | in progress |
| `engine` | Defence calculation against headless PoB on the golden corpus (attributes, life, mana, resists) | started |

```go
b, err := pobcode.FromCode(code)
spec := b.ActiveSpec()        // passive tree
set := b.ActiveItems()        // equipped items
weapon := b.Item(set.SlotItem("Weapon 1"))
```

```bash
go test ./...
go run ./cmd/gamedatagen   # regenerate gamedata/poe1 from the pinned PoB tag
```

`cmd/gamedatagen` reads PoB's Lua data files as data with `internal/luadata`
(no Lua runs). Not exported: `Data/Uniques/Special/Generated.lua` (a program
that builds Megalomaniac, Forbidden jewels and similar uniques).

License: MIT, see [LICENSE](LICENSE) — it includes the Path of Building Community notice.
