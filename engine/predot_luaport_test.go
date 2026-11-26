//go:build luaport

package engine

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestLuaPortPreDotFuncs: every skill of PoB's Data/Skills with a preDotFunc
// (CalcOffence.lua:5851 runs it before the skill's DoT) has a preDot hook.
// PoB v2.68.1 has none, so the hook point stays empty.
func TestLuaPortPreDotFuncs(t *testing.T) {
	dir := pobSrcDir(t)
	files, err := filepath.Glob(filepath.Join(dir, "Data", "Skills", "*.lua"))
	if err != nil || len(files) == 0 {
		t.Fatal("no skill data", err)
	}
	name := regexp.MustCompile(`name = "([^"]+)"`)
	for _, f := range files {
		raw, rerr := os.ReadFile(f)
		if rerr != nil {
			t.Fatal(rerr)
		}
		for _, loc := range regexp.MustCompile(`preDotFunc\s*=`).FindAllIndex(raw, -1) {
			// The skill's name is the last one before the function.
			names := name.FindAllSubmatch(raw[:loc[0]], -1)
			if len(names) == 0 {
				t.Errorf("%s: preDotFunc without a skill name", f)
				continue
			}
			skill := string(names[len(names)-1][1])
			if h := skillHookRegistry[skill]; h == nil || h.preDot == nil {
				t.Errorf("%s: %s has a preDotFunc and no preDot hook", filepath.Base(f), skill)
			}
		}
	}
	for skill, h := range skillHookRegistry {
		if h.preDot != nil {
			t.Logf("preDot hook: %s", skill)
		}
	}
}
