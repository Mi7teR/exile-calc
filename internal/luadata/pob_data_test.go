package luadata

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestPoBDataFiles parses every file of a PoB checkout:
//
//	POB_SRC=/path/to/PathOfBuilding/src go test ./internal/luadata -run PoBData -v
//
// Files listed in Unsupported are expected to fail (real programs, not data).
func TestPoBDataFiles(t *testing.T) {
	root := os.Getenv("POB_SRC")
	if root == "" {
		t.Skip("POB_SRC not set")
	}
	var files []string
	_ = filepath.Walk(filepath.Join(root, "Data"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && strings.HasSuffix(p, ".lua") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	failed := 0
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Eval(string(src), nil)
		_, expected := Unsupported[filepath.ToSlash(rel)]
		switch {
		case err != nil && !expected:
			failed++
			t.Errorf("%s: %v", rel, err)
		case err == nil && expected:
			t.Errorf("%s: parses now, remove it from Unsupported", rel)
		}
	}
	t.Logf("%d files, %d unexpected failures, %d unsupported", len(files), failed, len(Unsupported))
}
