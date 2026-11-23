// Command enginediff compares the engine with headless PoB mod by mod.
// It runs both on golden builds and prints, per stat and source, the sum of
// each mod type where they differ.
//
//	go run ./cmd/enginediff -builds ID1,ID2 -stats Dex,Life [-oracle ../poe-pob-oracle/run.sh]
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Mi7teR/poe-lib/engine"
	"github.com/Mi7teR/poe-lib/pobcode"
)

func main() {
	builds := flag.String("builds", "", "comma-separated golden build ids (default: all)")
	stats := flag.String("stats", "Str,Dex,Int,Life,Mana", "comma-separated mod names")
	oracle := flag.String("oracle", "../poe-pob-oracle/run.sh", "oracle run.sh")
	source := flag.String("source", "", "only sources with this prefix (e.g. Item:)")
	conds := flag.Bool("conditions", false, "compare conditions and multipliers instead of mods")
	flag.Parse()
	sourcePrefix = *source
	compareConditions = *conds
	if err := run(*builds, strings.Split(*stats, ","), *oracle); err != nil {
		fmt.Fprintln(os.Stderr, "enginediff:", err)
		os.Exit(1)
	}
}

type key struct{ typ, source string }

var (
	sourcePrefix      string
	compareConditions bool
)

func run(ids string, stats []string, oracle string) error {
	codes, _ := filepath.Glob("testdata/golden/builds/*.code")
	want := map[string]bool{}
	for _, id := range strings.Split(ids, ",") {
		if id != "" {
			want[id] = true
		}
	}
	calc, err := engine.New()
	if err != nil {
		return err
	}
	var input bytes.Buffer
	ours := map[string]map[string]map[key]float64{}
	oursCond := map[string]map[string]float64{}
	enc := json.NewEncoder(&input)
	for _, c := range codes {
		name := strings.TrimSuffix(filepath.Base(c), ".code")
		if len(want) > 0 && !want[name] {
			continue
		}
		raw, _ := os.ReadFile(c)
		x, err := pobcode.Decode(string(raw))
		if err != nil {
			return err
		}
		b, err := pobcode.Parse(x)
		if err != nil {
			return err
		}
		res, err := calc.Calc(b)
		if err != nil {
			return err
		}
		oursCond[name] = res.DB.Conditions()
		ours[name] = map[string]map[key]float64{}
		for _, s := range stats {
			ours[name][s] = map[key]float64{}
			for _, r := range res.DB.Tabulate(s) {
				if f, ok := r.Value.(float64); ok {
					ours[name][s][key{r.Type, r.Source}] += f
				}
			}
		}
		if err := enc.Encode(map[string]any{"name": name, "xml": string(x), "tabulate": stats, "conditions": compareConditions}); err != nil {
			return err
		}
	}
	cmd := exec.Command(oracle)
	cmd.Stdin = &input
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var r struct {
			Name        string             `json:"name"`
			Error       string             `json:"error"`
			Conditions  []string           `json:"conditions"`
			Multipliers map[string]float64 `json:"multipliers"`
			Tabulate    map[string][]struct {
				Type   string `json:"type"`
				Value  any    `json:"value"`
				Source string `json:"source"`
			} `json:"tabulate"`
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return err
		}
		if r.Error != "" {
			fmt.Printf("== %s: oracle error %s\n", r.Name, r.Error)
			continue
		}
		fmt.Printf("== %s\n", r.Name)
		if compareConditions {
			pob := map[string]float64{}
			for _, c := range r.Conditions {
				pob["Condition:"+c] = 1
			}
			for k, v := range r.Multipliers {
				pob["Multiplier:"+k] = v
			}
			var lines []string
			for k, v := range pob {
				if oursCond[r.Name][k] != v {
					lines = append(lines, fmt.Sprintf("  %-50s ours %-8g pob %g", k, oursCond[r.Name][k], v))
				}
			}
			for k, v := range oursCond[r.Name] {
				if _, ok := pob[k]; !ok && v != 0 {
					lines = append(lines, fmt.Sprintf("  %-50s ours %-8g pob 0", k, v))
				}
			}
			sort.Strings(lines)
			for _, l := range lines {
				fmt.Println(l)
			}
			continue
		}
		for _, s := range stats {
			pob := map[key]float64{}
			for _, row := range r.Tabulate[s] {
				if f, ok := row.Value.(float64); ok {
					pob[key{row.Type, row.Source}] += f
				}
			}
			keys := map[key]bool{}
			for k := range pob {
				keys[k] = true
			}
			for k := range ours[r.Name][s] {
				keys[k] = true
			}
			var lines []string
			for k := range keys {
				if !strings.HasPrefix(k.source, sourcePrefix) {
					continue
				}
				a, b := ours[r.Name][s][k], pob[k]
				if math.Abs(a-b) > 1e-9 {
					lines = append(lines, fmt.Sprintf("  %-14s %-8s ours %-8g pob %-8g %s", s, k.typ, a, b, k.source))
				}
			}
			sort.Strings(lines)
			for _, l := range lines {
				fmt.Println(l)
			}
		}
	}
	return sc.Err()
}
