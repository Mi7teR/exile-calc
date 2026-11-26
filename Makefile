STATSDUMP_DIR := $(HOME)/.cache/poe-lib/statsdump-base
# Tests and dumps cache decoded timeless tables here, not in the user cache
# (engine.TimelessCacheEnv; "off" turns the cache off).
export POE_LIB_TIMELESS_CACHE ?= $(CURDIR)/.timeless-cache

.PHONY: test test-short lint bench dump-base dump-check dump-cross luaport

test:
	go vet ./... && go test ./...

# Fast loop: skips the corpus-heavy engine tests (make test runs them).
test-short:
	go vet ./... && go test -short ./...

# Compare skill hooks with PoB's own Lua functions (needs PoB src, POB_SRC).
luaport:
	go test -tags luaport ./engine -run TestLuaPort -count=1

lint:
	@test -z "$$(gofmt -l .)" || { echo 'gofmt needed:'; gofmt -l .; exit 1; }
	golangci-lint run ./...

# Compare current benchmarks against the committed baseline.
bench:
	@f=$$(mktemp) && trap 'rm -f "$$f"' EXIT && \
	go test ./engine -run '^$$' -bench . -count=10 > "$$f" && go -C tools tool benchstat ../testdata/bench/baseline.txt "$$f"

# Write the stats snapshot that dump-check compares against.
dump-base:
	STATSDUMP_OUT=$(STATSDUMP_DIR) go test ./engine -run '^TestStatsDump$$' -count=1 -timeout 30m

# Fail on any difference against the snapshot taken by dump-base.
dump-check:
	STATSDUMP_BASE=$(STATSDUMP_DIR) go test ./engine -run '^TestStatsDump$$' -count=1 -timeout 30m -v

# Compare the native dump with an amd64 (Rosetta) dump; FMA differences.
dump-cross:
	sh tools/dumpcross.sh
