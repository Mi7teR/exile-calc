// Command enginediff compares the engine with headless PoB mod by mod.
// It runs both on golden builds and prints, per stat and source, the sum of
// each mod type where they differ.
//
//	go run ./cmd/enginediff -builds ID1,ID2 -stats Dex,Life [-oracle ../poe-pob-oracle/run.sh]
//	go run ./cmd/enginediff -combat -builds ID1,ID2
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
	skills := flag.Bool("skills", false, "compare active skills (level, quality, buffs, aura effect) instead of mods")
	combat := flag.Bool("combat", false, "print combat DPS (boss and pack) of the main skill for -builds; needs no oracle")
	flag.Parse()
	if *combat {
		if err := runCombat(*builds); err != nil {
			fmt.Fprintln(os.Stderr, "enginediff:", err)
			os.Exit(1)
		}
		return
	}
	sourcePrefix = *source
	compareConditions = *conds
	compareSkills = *skills
	if err := run(*builds, strings.Split(*stats, ","), *oracle); err != nil {
		fmt.Fprintln(os.Stderr, "enginediff:", err)
		os.Exit(1)
	}
}

type key struct{ typ, source string }

var (
	sourcePrefix      string
	compareConditions bool
	compareSkills     bool
)

type skillRow struct {
	Name     string   `json:"name"`
	Level    int      `json:"level"`
	Quality  int      `json:"quality"`
	Disabled bool     `json:"disabled"`
	Buffs    []string `json:"buffs"`
	AuraInc  float64  `json:"auraInc"`
	AuraMore float64  `json:"auraMore"`
	Rows     []struct {
		Name   string `json:"name"`
		Value  any    `json:"value"`
		Source string `json:"source"`
	} `json:"rows"`
}

// key is the row without the oracle's buff mod lines, buffs sorted, for
// comparing.
func (r skillRow) key() string {
	var buffs []string
	for _, b := range r.Buffs {
		if !strings.HasPrefix(b, "  ") {
			buffs = append(buffs, b)
		}
	}
	sort.Strings(buffs)
	r.Buffs = buffs
	return r.String()
}

func (r skillRow) String() string {
	return fmt.Sprintf("%-28s lvl %-2d q %-2d dis %-5v inc %-5g more %-5g %v", r.Name, r.Level, r.Quality, r.Disabled, r.AuraInc, r.AuraMore, r.Buffs)
}

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
	oursSkills := map[string][]skillRow{}
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
		for _, sk := range res.Skills {
			oursSkills[name] = append(oursSkills[name], skillRow{Name: sk.Name, Level: sk.Level, Quality: sk.Quality,
				Disabled: sk.Disabled, Buffs: sk.Buffs, AuraInc: sk.AuraEffectInc, AuraMore: sk.AuraEffectMore})
		}
		ours[name] = map[string]map[key]float64{}
		for _, s := range stats {
			ours[name][s] = map[key]float64{}
			db, stat := res.DB, s
			if n, ok := strings.CutPrefix(s, "Enemy:"); ok {
				db, stat = res.DB.Enemy(), n
			}
			for _, r := range db.Tabulate(stat) {
				if f, ok := r.Value.(float64); ok {
					ours[name][s][key{r.Type, r.Source}] += f
				}
			}
		}
		if err := enc.Encode(map[string]any{"name": name, "xml": string(x), "tabulate": stats, "conditions": compareConditions, "skills": compareSkills, "buffMods": compareSkills}); err != nil {
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
			Skills      []skillRow         `json:"skills"`
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
		if compareSkills {
			for i, p := range r.Skills {
				var o skillRow
				if i < len(oursSkills[r.Name]) {
					o = oursSkills[r.Name][i]
				}
				mark := " "
				if o.key() != p.key() {
					mark = "!"
				}
				fmt.Printf("%s pob  %s\n  ours %s\n", mark, p, o)
				if mark == "!" {
					for _, row := range p.Rows {
						fmt.Printf("      pob row %-22s %-6v %s\n", row.Name, row.Value, row.Source)
					}
				}
			}
			continue
		}
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
