STATSDUMP_DIR := $(HOME)/.cache/poe-lib/statsdump-base

.PHONY: test lint bench dump-base dump-check luaport

test:
	go vet ./... && go test ./...

# Compare skill hooks with PoB's own Lua functions (needs PoB src, POB_SRC).
luaport:
	go test -tags luaport ./engine -run TestLuaPort -count=1

lint:
	@test -z "$$(gofmt -l .)" || { echo 'gofmt needed:'; gofmt -l .; exit 1; }
	golangci-lint run ./...

# Compare current benchmarks against the committed baseline.
bench:
	go test ./engine -run '^$$' -bench . -count=10 > /tmp/bench.txt && go -C tools tool benchstat ../testdata/bench/baseline.txt /tmp/bench.txt

# Write the stats snapshot that dump-check compares against.
dump-base:
	STATSDUMP_OUT=$(STATSDUMP_DIR) go test ./engine -run '^TestStatsDump$$' -count=1 -timeout 30m

# Fail on any difference against the snapshot taken by dump-base.
dump-check:
	STATSDUMP_BASE=$(STATSDUMP_DIR) go test ./engine -run '^TestStatsDump$$' -count=1 -timeout 30m -v
