package engine

import (
	"fmt"
	"os"
	"testing"
)

// TestMain keeps the tests off the user's timeless table cache: unless the
// caller set TimelessCacheEnv (the Makefile sets it to the repository's
// .timeless-cache), the decoded tables go to a temporary directory removed
// at the end.
func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	if os.Getenv(TimelessCacheEnv) == "" {
		dir, err := os.MkdirTemp("", "poe-lib-timeless-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(dir)
		if err = os.Setenv(TimelessCacheEnv, dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return m.Run()
}
