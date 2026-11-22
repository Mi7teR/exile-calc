# Golden corpus

Real Path of Building builds on the 3.30 tree and the stats PoB computes for
them; the engine's diff tests compare against `expected/`.

- `builds/*.code` — PoB codes after a `pobcode` round trip (notes, import
  links and UI state removed). `sources.tsv`: id, public link, ascendancy,
  tree, level.
- `expected/*.json` — `mainOutput` of headless PoB v2.68.1 from
  [poe-pob-oracle](https://github.com/Mi7teR/poe-pob-oracle) at commit 7f91869.

Regenerate:

```bash
go run ./cmd/goldencorpus input > /tmp/builds.jsonl
../poe-pob-oracle/run.sh v2.68.1 < /tmp/builds.jsonl > /tmp/results.jsonl
go run ./cmd/goldencorpus expected < /tmp/results.jsonl
```
