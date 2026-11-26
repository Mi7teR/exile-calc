#!/bin/sh
# dump-cross: write the stats dump with an amd64 build of the engine tests
# under Rosetta and compare the native (arm64) dump against it. arm64 fuses
# x*y+z into one rounding (FMA) where amd64 and PoB round each product, so
# any difference is an FMA spot. Skips cleanly without an amd64 runner.
set -eu
dir=${CROSS_DIR:-$(pwd)/.statsdump-cross}
if [ "$(uname -m)" = "x86_64" ]; then
	echo "dump-cross: native amd64, nothing to compare"
	exit 0
fi
if ! arch -x86_64 /usr/bin/true 2>/dev/null; then
	echo "dump-cross: no amd64 runner (Rosetta), skipped"
	exit 0
fi
mkdir -p "$dir"
GOARCH=amd64 go test -c -o "$dir/engine.amd64" ./engine
(cd engine && STATSDUMP_OUT="$dir/amd64" arch -x86_64 "$dir/engine.amd64" \
	-test.run '^TestStatsDump$' -test.count 1 -test.timeout 30m >/dev/null)
STATSDUMP_BASE="$dir/amd64" go test ./engine -run '^TestStatsDump$' -count=1 -timeout 30m -v
