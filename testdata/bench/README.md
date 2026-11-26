`baseline.txt` is `make bench` output (`./engine`, count=10). BenchmarkCalc/BenchmarkCombat run with a warm parse cache (the process-wide modparser cache fills on the first iteration); cold parse speed is `go test ./modparser -run "^$" -bench ParseCorpusCold`.

First-call latency (a fresh process: `engine.New`, the first and second `Calc`, `NewComparer` and the first `Comparer.Item`) is `go run ./cmd/coldstart -runs 11` from the repository root; `-build ID` picks a corpus build, and `-phase`, `-cpuprofile` and `-memprofile` profile one step (see the command's doc comment).
