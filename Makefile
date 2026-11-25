STATSDUMP_DIR := $(HOME)/.cache/poe-lib/statsdump-base

.PHONY: test lint bench dump-base dump-check

test:
	go vet ./... && go test ./...

# why GOTOOLCHAIN: the installed golangci-lint is built with Go 1.26 and panics on the Go 1.27 standard library;
# linting under the cached Go 1.25 toolchain (the go.mod version) works. Drop it once golangci-lint is rebuilt with Go 1.27.
lint:
	GOTOOLCHAIN=go1.25.11 golangci-lint run ./...

# Compare current benchmarks against the committed baseline.
bench:
	go test ./engine -run '^$$' -bench . -count=10 > /tmp/bench.txt && go -C tools tool benchstat ../testdata/bench/baseline.txt /tmp/bench.txt

# Write the stats snapshot that dump-check compares against.
dump-base:
	STATSDUMP_OUT=$(STATSDUMP_DIR) go test ./engine -run '^TestStatsDump$$' -count=1 -timeout 30m

# Fail on any difference against the snapshot taken by dump-base.
dump-check:
	STATSDUMP_BASE=$(STATSDUMP_DIR) go test ./engine -run '^TestStatsDump$$' -count=1 -timeout 30m -v
