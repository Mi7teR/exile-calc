package modparser

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"regexp/syntax"
	"slices"
	"sort"
	"sync"
)

// data/poe1.json.gz holds the pattern tables of PoB's Modules/ModParser.lua,
// exported by cmd/gamedatagen. Values follow the generator's encoding: a Lua
// table is an array, or an object with its positional entries under "@list";
// a Lua function is {"$hook": name}.
//
//go:embed data/poe1.json.gz
var poe1Data []byte

type entry struct {
	pattern string
	re      *lazyRegexp // nil for plain tables
	value   any
}

// lazyRegexp is a pattern compiled on first use: most patterns of a table
// are never run (scan skips those whose literal is not in the line), and
// compiling all of them up front was most of PoE1's cost.
type lazyRegexp struct {
	expr string // validated by syntax.Parse
	// lit is text every match contains (requiredLiteral), "" for none.
	lit  string
	once sync.Once
	re   *regexp.Regexp
}

func newLazyRegexp(luaPattern string) (*lazyRegexp, error) {
	expr, err := luaPatternExpr(luaPattern)
	if err != nil {
		return nil, err
	}
	// regexp.Compile fails only where syntax.Parse does.
	parsed, err := syntax.Parse(expr, syntax.Perl)
	if err != nil {
		return nil, err
	}
	return &lazyRegexp{expr: expr, lit: requiredLiteral(parsed)}, nil
}

func (l *lazyRegexp) get() *regexp.Regexp {
	l.once.Do(func() { l.re = regexp.MustCompile(l.expr) })
	return l.re
}

type tables struct {
	pob string
	t   map[string][]entry
	idx map[string]*gramIndex // by table name, over t's entries
	// lookup tables keyed by exact text (resourceTypes, dmgTypes, ...)
	exact map[string]map[string]any
	// patterns that could not be translated (counted, not used)
	skipped []string
}

// Tables looked up by the exact lower-cased line, not scanned.
var exactTables = map[string]bool{"clusterJewelSkills": true, "unsupportedModList": true, "SkillType": true,
	"resourceTypes": true, "regenTypes": true, "degenTypes": true, "dmgTypes": true,
	"gemIdLookup": true, "supportGems": true, "conquerorList": true,
	"itemTagSpecial": true, "itemTagSpecialExclusionPattern": true}

// Which ModParser.lua tables scan() reads with plain=true (substring search).
var plainTables = map[string]bool{
	"modNameList": true, "modFlagList": true, "penTypes": true, "baseCostTypes": true,
	"costTypes": true, "suffixTypes": true,
}

func loadTables(gz []byte) (*tables, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	var raw struct {
		PoB    string                    `json:"pob"`
		Tables map[string]map[string]any `json:"tables"`
	}
	if err := json.NewDecoder(zr).Decode(&raw); err != nil {
		return nil, fmt.Errorf("modparser: data: %w", err)
	}
	t := &tables{pob: raw.PoB, t: map[string][]entry{}, idx: map[string]*gramIndex{}, exact: raw.Tables}
	for name, tbl := range raw.Tables {
		if exactTables[name] {
			continue
		}
		keys := make([]string, 0, len(tbl))
		for k := range tbl {
			keys = append(keys, k)
		}
		sort.Strings(keys) // deterministic tie-break where Lua's pairs() is not
		for _, k := range keys {
			e := entry{pattern: k, value: tbl[k]}
			if !plainTables[name] {
				re, rerr := newLazyRegexp(k)
				if rerr != nil {
					t.skipped = append(t.skipped, name+": "+k)
					continue
				}
				e.re = re
			}
			t.t[name] = append(t.t[name], e)
		}
		t.idx[name] = newGramIndex(t.t[name])
	}
	return t, nil
}

// gramLen is the length of the substrings gramIndex files entries under.
const gramLen = 3

// gramIndex finds the entries of a table that can match a line: each entry
// with a required text (the plain pattern, or the regexp's literal) of at
// least gramLen bytes is filed under one gramLen-byte piece of that text,
// which any line it matches contains.
type gramIndex struct {
	byGram map[string][]int32 // entry indexes, ascending
	always []int32            // entries with a shorter or no required text
}

func newGramIndex(entries []entry) *gramIndex {
	lits := make([]string, len(entries))
	freq := map[string]int{}
	for i := range entries {
		lits[i] = entries[i].pattern
		if entries[i].re != nil {
			lits[i] = entries[i].re.lit
		}
		for j := 0; j+gramLen <= len(lits[i]); j++ {
			freq[lits[i][j:j+gramLen]]++
		}
	}
	x := &gramIndex{byGram: map[string][]int32{}}
	for i, lit := range lits {
		if len(lit) < gramLen {
			x.always = append(x.always, int32(i))
			continue
		}
		// The rarest piece across the table keeps the candidate lists short.
		best := lit[:gramLen]
		for j := 1; j+gramLen <= len(lit); j++ {
			if g := lit[j : j+gramLen]; freq[g] < freq[best] {
				best = g
			}
		}
		x.byGram[best] = append(x.byGram[best], int32(i))
	}
	return x
}

// candidates returns, ascending, the indexes of the entries that may match
// line; scan checks each as before, so the result is the same as checking
// every entry in order.
func (x *gramIndex) candidates(line string) []int32 {
	if x == nil {
		return nil // not a scanned table: no entries
	}
	out := slices.Clone(x.always)
	for j := 0; j+gramLen <= len(line); j++ {
		out = append(out, x.byGram[line[j:j+gramLen]]...)
	}
	slices.Sort(out)
	return slices.Compact(out)
}
