# Golden corpus

Real Path of Building builds on the 3.30 tree and the stats PoB computes for
them; the engine's diff tests compare against `expected/`.

94 builds: public pobb.in links listed on poebuilds.cc, level 85+, at most
two per ascendancy for the later additions. Builds published on an older
tree were added with `goldencorpus add -tree 3_30`, which moves the active
spec to the 3.30 tree (PoB drops nodes the tree lacks), so the oracle and
the engine compute the same build. The engine matches PoB on all
15 compared stats (attributes, life, mana, resistances, armour, evasion,
energy shield, ward, block) within 0.5% for every build but one:
-s4JtJhUcgeE misses 9 EHP stats (FireTakenHit, PhysicalTakenHit,
totalTakenHit, Physical/FireMaximumHitTaken, NumberOf(Mitigated)DamagingHits,
TotalEHP, EHPSurvivalTime) because its Arctic Armour is inactive in the
engine (a Weapon 2 socket/gem count gap).

`party-<base id>` builds are synthetic: a corpus build with a Party tab
(imported auras, curses, warcries, links, party member stats and enemy
mods) taken from public builds or written by hand; `sources.tsv` names
both. They check the Party tab, which few public builds fill.
`link-<base id>` is the base build with the "Linked To Minion?" config
option set, which its author left off: it checks Link skills on a minion
(Link Skills can target Damageable Minions).

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

## Combat DPS report

`go run ./cmd/enginediff -combat -builds ID1,ID2` prints, per build, the main
skill's combat DPS model against a boss and a pack: PoB's CombinedDPS, the
sustained and burst DPS, hits per cast, the components and the assumptions.
It does not run the PoB oracle. The numbers are the engine's model (hit
geometry, sources, DoT stacking), not checked against the oracle.
