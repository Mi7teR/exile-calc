// Command poedbcheck spot-checks the generated game data against poedb.tw,
// which shows data mined from the game client independently of PoB.
//
//	go run ./cmd/poedbcheck base "Cabalist Regalia"
//	go run ./cmd/poedbcheck gem "Purity of Elements"
//
// It fetches one page per run (cached under the user cache directory) and
// prints the values that differ. poedb pages are for people: their layout
// can change, so a failure to find a value is reported, not guessed.
package main

import (
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Mi7teR/poe-lib/gamedata"
)

func main() {
	if len(os.Args) != 3 || (os.Args[1] != "base" && os.Args[1] != "gem") {
		fmt.Fprintln(os.Stderr, "usage: poedbcheck base|gem NAME")
		os.Exit(2)
	}
	d, err := gamedata.PoE1()
	if err != nil {
		fail(err)
	}
	page, err := fetch(os.Args[2])
	if err != nil {
		fail(err)
	}
	var diffs []string
	switch os.Args[1] {
	case "base":
		diffs, err = checkBase(d, os.Args[2], page)
	case "gem":
		diffs, err = checkGem(d, os.Args[2], page)
	}
	if err != nil {
		fail(err)
	}
	if len(diffs) == 0 {
		fmt.Println("ok: matches poedb")
		return
	}
	for _, l := range diffs {
		fmt.Println(l)
	}
	os.Exit(1)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "poedbcheck:", err)
	os.Exit(1)
}

// fetch returns the poedb page of a name ("Purity of Elements" ->
// /us/Purity_of_Elements), cached for a day.
func fetch(name string) (string, error) {
	slug := strings.ReplaceAll(strings.ReplaceAll(name, " ", "_"), "'", "")
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "poe-lib", "poedb", slug+".html")
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < 24*time.Hour {
		b, err := os.ReadFile(path)
		return string(b), err
	}
	req, err := http.NewRequest("GET", "https://poedb.tw/us/"+slug, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; poe-lib poedbcheck)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("poedb %s: %s", slug, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
	return string(b), nil
}

var (
	scriptRe = regexp.MustCompile(`(?s)<script.*?</script>`)
	tagRe    = regexp.MustCompile(`<[^>]+>`)
	spaceRe  = regexp.MustCompile(`\s+`)
)

// text is the page without markup, whitespace collapsed.
func text(s string) string {
	s = scriptRe.ReplaceAllString(s, "")
	s = tagRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(s), " "))
}

// rangeAfter reads "Label: a — b" or "Label: a" after the item's header.
func rangeAfter(t, label string) (lo, hi float64, ok bool) {
	re := regexp.MustCompile(regexp.QuoteMeta(label) + `: ([\d.]+)(?:%)?(?: — ([\d.]+))?`)
	m := re.FindStringSubmatch(t)
	if m == nil {
		return 0, 0, false
	}
	lo, _ = strconv.ParseFloat(m[1], 64)
	hi = lo
	if m[2] != "" {
		hi, _ = strconv.ParseFloat(m[2], 64)
	}
	return lo, hi, true
}

func checkBase(d *gamedata.Data, name, page string) ([]string, error) {
	b := d.Bases[name]
	if b == nil {
		return nil, fmt.Errorf("no base %q in gamedata", name)
	}
	t := text(page)
	// The item box starts at the second "<name> <class>" occurrence; the
	// first is the page title.
	if i := strings.Index(t, "BaseType "+name); i > 0 {
		if j := strings.LastIndex(t[:i], name); j >= 0 {
			t = t[j:i]
		}
	}
	var diffs []string
	found := 0
	defer func() { fmt.Printf("checked %d defence values and requirements of %s\n", found, name) }()
	cmp := func(label string, wantLo, wantHi float64) {
		lo, hi, ok := rangeAfter(t, label)
		if ok {
			found++
		}
		if !ok {
			if wantLo != 0 || wantHi != 0 {
				diffs = append(diffs, fmt.Sprintf("%s: ours %g-%g, poedb none", label, wantLo, wantHi))
			}
			return
		}
		if lo != wantLo || hi != wantHi {
			diffs = append(diffs, fmt.Sprintf("%s: ours %g-%g, poedb %g-%g", label, wantLo, wantHi, lo, hi))
		}
	}
	if a := b.Armour; a != nil {
		cmp("Armour", a.ArmourBaseMin, a.ArmourBaseMax)
		cmp("Evasion Rating", a.EvasionBaseMin, a.EvasionBaseMax)
		cmp("Energy Shield", a.EnergyShieldBaseMin, a.EnergyShieldBaseMax)
		cmp("Ward", a.WardBaseMin, a.WardBaseMax)
		cmp("Chance to Block", a.BlockChance, a.BlockChance)
	}
	if m := regexp.MustCompile(`Requires Level (\d+)`).FindStringSubmatch(t); m != nil {
		if lvl, _ := strconv.Atoi(m[1]); lvl != b.Req.Level {
			diffs = append(diffs, fmt.Sprintf("Requires Level: ours %d, poedb %d", b.Req.Level, lvl))
		}
	}
	for _, a := range []struct {
		name string
		v    int
	}{{"Str", b.Req.Str}, {"Dex", b.Req.Dex}, {"Int", b.Req.Int}} {
		m := regexp.MustCompile(`(\d+) ` + a.name + `\b`).FindStringSubmatch(t)
		got := 0
		if m != nil {
			got, _ = strconv.Atoi(m[1])
		}
		if got != a.v {
			diffs = append(diffs, fmt.Sprintf("Requires %s: ours %d, poedb %d", a.name, a.v, got))
		}
	}
	return diffs, nil
}

var (
	tableRe = regexp.MustCompile(`(?s)<table[^>]*>(.*?)</table>`)
	thRe    = regexp.MustCompile(`(?s)<th>(.*?)</th>`)
	trRe    = regexp.MustCompile(`(?s)<tr>(.*?)</tr>`)
	tdRe    = regexp.MustCompile(`(?s)<td>(.*?)</td>`)
)

// levelTable finds the gem's per-level table (headers start with Level,
// Requires Level) and returns its headers and rows as text.
func levelTable(page string) ([]string, []bool, [][]string, error) {
	for _, m := range tableRe.FindAllStringSubmatch(page, -1) {
		var heads []string
		var stat []bool // stat columns are marked explicitMod
		for _, h := range thRe.FindAllStringSubmatch(m[1], -1) {
			heads = append(heads, text(h[1]))
			stat = append(stat, strings.Contains(h[1], "explicitMod"))
		}
		if len(heads) < 2 || heads[0] != "Level" || heads[1] != "Requires Level" {
			continue
		}
		var rows [][]string
		for _, r := range trRe.FindAllStringSubmatch(m[1], -1) {
			var cells []string
			for _, c := range tdRe.FindAllStringSubmatch(r[1], -1) {
				cells = append(cells, text(c[1]))
			}
			if len(cells) == len(heads) {
				rows = append(rows, cells)
			}
		}
		return heads, stat, rows, nil
	}
	return nil, nil, nil, errors.New("no level table on the page")
}

func checkGem(d *gamedata.Data, name, page string) ([]string, error) {
	var gem *gamedata.Gem
	for _, n := range []string{name, strings.TrimSuffix(name, " Support")} {
		for _, g := range d.Gems {
			if g.Name == n && (gem == nil || g.ID < gem.ID) {
				gem = g
			}
		}
		if gem != nil {
			break
		}
	}
	if gem == nil {
		return nil, fmt.Errorf("no gem %q in gamedata", name)
	}
	ge := d.Skills.Effects[gem.GrantedEffectID]
	if ge == nil {
		return nil, fmt.Errorf("no granted effect %q", gem.GrantedEffectID)
	}
	heads, stat, rows, err := levelTable(page)
	if err != nil {
		return nil, err
	}
	var diffs []string
	levels := map[int][]string{}
	for _, r := range rows {
		lvl, err := strconv.Atoi(r[0])
		if err != nil {
			continue
		}
		levels[lvl] = r
		ours := ge.Levels[lvl]
		if ours == nil {
			diffs = append(diffs, fmt.Sprintf("level %d: not in gamedata", lvl))
			continue
		}
		if req, _ := strconv.Atoi(r[1]); req != ours.LevelRequirement {
			diffs = append(diffs, fmt.Sprintf("level %d: requires level ours %d, poedb %d", lvl, ours.LevelRequirement, req))
		}
	}
	// Each numeric poedb column (a stat) must equal one of our stat columns
	// at every level poedb lists.
	checked := 0
	for c := 2; c < len(heads); c++ {
		if !stat[c] {
			continue
		}
		col := map[int]float64{}
		numeric := true
		for lvl, r := range levels {
			v, err := strconv.ParseFloat(strings.TrimSuffix(strings.ReplaceAll(r[c], ",", ""), "%"), 64)
			if err != nil {
				numeric = false
				break
			}
			col[lvl] = v
		}
		if !numeric || len(col) == 0 {
			continue
		}
		checked++
		if !matchesStat(ge, col) {
			diffs = append(diffs, fmt.Sprintf("column %q: no stat of ours has these values (%s)", heads[c], sample(col)))
		}
	}
	fmt.Printf("checked %d levels, %d stat columns of %s\n", len(levels), checked, ge.Name)
	return diffs, nil
}

// matchesStat reports whether some stat index has the column's values
// (directly, or /10, /100, /1000 and per minute /60 for poedb's display
// units, which round to one decimal).
func matchesStat(ge *gamedata.Skill, col map[int]float64) bool {
	for i := range ge.Stats {
		for _, div := range []float64{1, 10, 60, 100, 1000} {
			ok := true
			for lvl, v := range col {
				l := ge.Levels[lvl]
				if l == nil || i >= len(l.Values) || math.Abs(l.Values[i]/div-v) > 0.051 {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
	}
	return false
}

func sample(col map[int]float64) string {
	var lv []int
	for l := range col {
		lv = append(lv, l)
	}
	sort.Ints(lv)
	var parts []string
	for _, l := range lv[:min(4, len(lv))] {
		parts = append(parts, fmt.Sprintf("L%d=%g", l, col[l]))
	}
	return strings.Join(parts, ", ")
}
