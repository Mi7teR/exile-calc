package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps the tests off the user's timeless table cache: unless the
// caller set TimelessCacheEnv (the Makefile sets it to the repository's
// .timeless-cache), the decoded tables go to a temporary directory removed
// at the end. It also points TimelessDirEnv at PoB's own tables (POB_SRC,
// else the oracle's checkout) when neither the variable nor the
// gamedatagen cache provides them.
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
	if os.Getenv(TimelessDirEnv) == "" && defaultTimelessDir() == "" {
		src := os.Getenv("POB_SRC")
		if src == "" {
			home, _ := os.UserHomeDir()
			src = filepath.Join(home, "GoProjects", "exile-oracle", ".pob", "v2.68.1", "src")
		}
		if err := os.Setenv(TimelessDirEnv, filepath.Join(src, "Data", "TimelessJewelData")); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	return m.Run()
}
