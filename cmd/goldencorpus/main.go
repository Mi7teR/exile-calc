// Command goldencorpus maintains the engine's golden corpus in
// testdata/golden: real PoB builds and the stats headless PoB computes for
// them (github.com/Mi7teR/poe-pob-oracle).
//
//	goldencorpus add -name ID -source URL [-tree 3_30] < code    # sanitise and store a build
//	goldencorpus input > builds.jsonl               # oracle input: {"name","xml","minions"}
//	goldencorpus expected < results.jsonl            # store oracle output
//
// Builds are stored after a pobcode round trip, which keeps only what the
// calculation needs and drops notes, import links and UI state. The oracle
// computes on exactly that build, so engine and reference see the same input.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Mi7teR/poe-lib/pobcode"
)

const dir = "testdata/golden"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: goldencorpus add|input|expected")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "add":
		fs := flag.NewFlagSet("add", flag.ExitOnError)
		name := fs.String("name", "", "build id (file name)")
		source := fs.String("source", "", "where the build was published")
		tree := fs.String("tree", "", "move the active spec to this tree version (engine data has one tree)")
		_ = fs.Parse(os.Args[2:])
		err = add(*name, *source, *tree, os.Stdin)
	case "input":
		err = input(os.Stdout)
	case "expected":
		err = expected(os.Stdin)
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "goldencorpus:", err)
		os.Exit(1)
	}
}

func add(name, source, tree string, r io.Reader) error {
	if name == "" || strings.ContainsAny(name, "/\\. ") {
		return errors.New("-name must be a plain id")
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	b, err := pobcode.FromCode(strings.TrimSpace(string(raw)))
	if err != nil {
		return err
	}
	if spec := b.ActiveSpec(); tree != "" && spec != nil {
		// PoB drops allocated nodes the new tree lacks, as on its own
		// tree conversion; the oracle and the engine see the same build.
		spec.TreeVersion = tree
	}
	code, err := b.Code()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "builds"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "builds", name+".code"), []byte(code+"\n"), 0o644); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "sources.tsv"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	spec := b.ActiveSpec()
	tree = ""
	if spec != nil {
		tree = spec.TreeVersion
	}
	_, err = fmt.Fprintf(f, "%s\t%s\t%s\t%s\t%d\n", name, source, b.AscendClassName, tree, b.Level)
	return err
}

func input(w io.Writer) error {
	codes, err := filepath.Glob(filepath.Join(dir, "builds", "*.code"))
	if err != nil {
		return err
	}
	sort.Strings(codes)
	enc := json.NewEncoder(w)
	for _, c := range codes {
		raw, err := os.ReadFile(c)
		if err != nil {
			return err
		}
		x, err := pobcode.Decode(string(raw))
		if err != nil {
			return fmt.Errorf("%s: %w", c, err)
		}
		name := strings.TrimSuffix(filepath.Base(c), ".code")
		if err := enc.Encode(map[string]any{"name": name, "xml": string(x), "minions": true}); err != nil {
			return err
		}
	}
	return nil
}

// expected stores the oracle's output, one JSON file per build, with sorted
// keys so regenerating gives readable diffs.
func expected(r io.Reader) error {
	if err := os.MkdirAll(filepath.Join(dir, "expected"), 0o755); err != nil {
		return err
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	n := 0
	for sc.Scan() {
		var res struct {
			Name    string         `json:"name"`
			Output  map[string]any `json:"output"`
			Minions []any          `json:"minions"`
			Error   string         `json:"error"`
		}
		if err := json.Unmarshal(sc.Bytes(), &res); err != nil {
			return err
		}
		if res.Error != "" {
			return fmt.Errorf("oracle failed on %s: %s", res.Name, res.Error)
		}
		// The oracle's {"minions": true} output: each minion skill's
		// output.Minion, recalculated with that skill as the main skill.
		if len(res.Minions) > 0 {
			res.Output["Minions"] = res.Minions
		}
		data, err := json.MarshalIndent(res.Output, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "expected", res.Name+".json"), append(data, '\n'), 0o644); err != nil {
			return err
		}
		n++
	}
	fmt.Fprintf(os.Stderr, "stored %d expected results\n", n)
	return sc.Err()
}
