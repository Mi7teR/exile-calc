package engine

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// applyRange turns a ranged mod line into the rolled value, porting the
// fallback path of itemLib.applyRange (Modules/ItemTools.lua):
// "(min-max)" becomes floor(min + range*(max-min) [* 10^p] + 0.5), p being
// the decimals the line uses, and "-N% increased" reads as "N% reduced".
// The ModScalability path (catalysts, per-stat display formats) is not
// ported; at range scalar 1 both paths agree for integer ranges.
var rangeRe = regexp.MustCompile(`(\+?)\((-?\d+\.?\d*)-(-?\d+\.?\d*)\)`)

var antonymRe = regexp.MustCompile(`-(\d+\.?\d*%) (increased|reduced|more|less)\b`)

var antonyms = map[string]string{"increased": "reduced", "reduced": "increased", "more": "less", "less": "more"}

func applyRange(line string, r float64) string {
	precision := 0
	if strings.Contains(line, ".") && regexp.MustCompile(`\d+\.\d`).MatchString(line) {
		precision = 1 // data.defaultHighPrecision for lines with decimals
	}
	line = rangeRe.ReplaceAllStringFunc(line, func(s string) string {
		m := rangeRe.FindStringSubmatch(s)
		lo, _ := strconv.ParseFloat(m[2], 64)
		hi, _ := strconv.ParseFloat(m[3], 64)
		p := math.Pow(10, float64(precision))
		v := math.Floor((lo+r*(hi-lo))*p+0.5) / p
		plus := m[1]
		if v < 0 {
			plus = ""
		}
		return plus + strconv.FormatFloat(v, 'f', -1, 64)
	})
	return antonymRe.ReplaceAllStringFunc(line, func(s string) string {
		m := antonymRe.FindStringSubmatch(s)
		return m[1] + " " + antonyms[m[2]]
	})
}
