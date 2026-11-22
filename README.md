# poe-lib

Go libraries for Path of Exile game data, ported from
[Path of Building Community](https://github.com/PathOfBuildingCommunity/PathOfBuilding)
(pinned to v2.68.1). Pure Go: no Lua code and no LuaJIT in the build.

| Package | What it does | Status |
| --- | --- | --- |
| `pobcode` | PoB export code ↔ `Build`: items by slot, gems, passive trees, config | done |
| `gamedata` | Generated game data (bases, mods, gems, uniques, tree) | planned |
| `itemtext` | Item text → structured item | planned |
| `modparser` | Mod text → ModDB modifiers | planned |
| `engine` | Defence/offence calculation, swap and diff | planned |

```go
b, err := pobcode.FromCode(code)
spec := b.ActiveSpec()        // passive tree
set := b.ActiveItems()        // equipped items
weapon := b.Item(set.SlotItem("Weapon 1"))
```

```bash
go test ./...
```

License: MIT, see [LICENSE](LICENSE) — it includes the Path of Building Community notice.
