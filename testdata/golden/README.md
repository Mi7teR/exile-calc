# Golden corpus

Real Path of Building builds on the 3.30 tree and the stats PoB computes for
them; the engine's diff tests compare against `expected/`.

85 builds: public pobb.in links listed on poebuilds.cc, level 85+, at most
two per ascendancy for the later additions. Builds published on an older
tree were added with `goldencorpus add -tree 3_30`, which moves the active
spec to the 3.30 tree (PoB drops nodes the tree lacks), so the oracle and
the engine compute the same build. The engine matches PoB on all
15 compared stats (attributes, life, mana, resistances, armour, evasion,
energy shield, ward, block) within 0.5% for every build.

- `builds/*.code` — PoB codes after a `pobcode` round trip (notes, import
  links and UI state removed). `sources.tsv`: id, public link, ascendancy,
  tree, level.
- `expected/*.json` — `mainOutput` of headless PoB v2.68.1 from
  [poe-pob-oracle](https://github.com/Mi7teR/poe-pob-oracle) at commit b266ae7.
- `quirks.json` — expected values that deliberately differ from PoB, where
  the engine fixes a PoB bug (build → stat → value and reason). The test
  applies them over `expected/`, so regenerating keeps them.

Regenerate:

```bash
go run ./cmd/goldencorpus input > /tmp/builds.jsonl
../poe-pob-oracle/run.sh v2.68.1 < /tmp/builds.jsonl > /tmp/results.jsonl
go run ./cmd/goldencorpus expected < /tmp/results.jsonl
```
