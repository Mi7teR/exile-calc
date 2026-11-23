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

var plainNumberRe = regexp.MustCompile(`\d+\.?\d*`)

var antonymRe = regexp.MustCompile(`-(\d+\.?\d*%) (increased|reduced|more|less)\b`)

var antonyms = map[string]string{"increased": "reduced", "reduced": "increased", "more": "less", "less": "more"}

// scalar is the catalyst scalar: values are scaled and floored toward zero
// (itemLib.formatValue floorSymmetric), ranged ones after rolling.
func applyRange(line string, r, scalar float64) string {
	precision := 0
	if strings.Contains(line, ".") && regexp.MustCompile(`\d+\.\d`).MatchString(line) {
		precision = 1 // data.defaultHighPrecision for lines with decimals
	}
	scale := func(v float64) float64 {
		if scalar == 1 {
			return v
		}
		p := math.Pow(10, float64(precision))
		return math.Trunc(v*scalar*p+1e-9) / p
	}
	ranged := rangeRe.MatchString(line)
	line = rangeRe.ReplaceAllStringFunc(line, func(s string) string {
		m := rangeRe.FindStringSubmatch(s)
		lo, _ := strconv.ParseFloat(m[2], 64)
		hi, _ := strconv.ParseFloat(m[3], 64)
		p := math.Pow(10, float64(precision))
		v := scale(math.Floor((lo+r*(hi-lo))*p+0.5) / p)
		plus := m[1]
		if v < 0 {
			plus = ""
		}
		return plus + strconv.FormatFloat(v, 'f', -1, 64)
	})
	if !ranged && scalar != 1 {
		line = plainNumberRe.ReplaceAllStringFunc(line, func(s string) string {
			v, _ := strconv.ParseFloat(s, 64)
			return strconv.FormatFloat(scale(v), 'f', -1, 64)
		})
	}
	return antonymRe.ReplaceAllStringFunc(line, func(s string) string {
		m := antonymRe.FindStringSubmatch(s)
		return m[1] + " " + antonyms[m[2]]
	})
}
