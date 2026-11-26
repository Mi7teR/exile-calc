package modparser

import (
	"fmt"
	"regexp"
	"strings"
)

// Lua pattern classes (Lua 5.1 manual, 5.4.1) as RE2 character class bodies.
var luaClasses = map[byte]string{
	'a': `A-Za-z`, 'd': `0-9`, 'l': `a-z`, 's': `\t\n\v\f\r `, 'u': `A-Z`,
	'w': `0-9A-Za-z`, 'x': `0-9A-Fa-f`, 'p': `!-/:-@\[-` + "`" + `{-~`, 'c': `\x00-\x1f\x7f`,
}

// translateLuaPattern converts a Lua pattern to an equivalent Go regexp.
// ModParser.lua uses classes (%d, %a, ...), sets, the lazy "-" quantifier,
// captures and anchors; %b, %f and back-references are rejected.
func translateLuaPattern(p string) (*regexp.Regexp, error) {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '%':
			if i+1 >= len(p) {
				return nil, fmt.Errorf("lua pattern %q: ends with %%", p)
			}
			i++
			n := p[i]
			if cls, ok := luaClass(n); ok {
				b.WriteString(cls)
			} else if n == 'b' || n == 'f' || (n >= '0' && n <= '9') {
				return nil, fmt.Errorf("lua pattern %q: %%%c is not supported", p, n)
			} else {
				b.WriteString(regexp.QuoteMeta(string(n)))
			}
		case c == '[':
			j, set, err := luaSet(p, i)
			if err != nil {
				return nil, err
			}
			b.WriteString(set)
			i = j
		case c == '-':
			// A quantifier after a single-char item, a literal otherwise.
			if b.Len() > 0 && i > 0 && quantifiable(p, i) {
				b.WriteString("*?")
			} else {
				b.WriteString(`\-`)
			}
		case c == '^':
			if i == 0 {
				b.WriteByte('^')
			} else {
				b.WriteString(`\^`)
			}
		case c == '$':
			if i == len(p)-1 {
				b.WriteByte('$')
			} else {
				b.WriteString(`\$`)
			}
		case c == '*' || c == '+' || c == '?':
			// Like "-": Lua reads a quantifier with no single-char item
			// before it ("^+(%d+) to ...") as a plain character.
			if i > 0 && quantifiable(p, i) {
				b.WriteByte(c)
			} else {
				b.WriteString(`\` + string(c))
			}
		case c == '(' || c == ')' || c == '.':
			b.WriteByte(c)
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	return regexp.Compile(b.String())
}

func luaClass(n byte) (string, bool) {
	lower := n | 0x20
	body, ok := luaClasses[lower]
	if !ok {
		return "", false
	}
	if n >= 'A' && n <= 'Z' {
		return "[^" + body + "]", true
	}
	return "[" + body + "]", true
}

// quantifiable reports whether the quantifier ("-", "*", "+", "?") at p[i]
// follows a single-character item (a char, an escape, a class, a set or
// ".") rather than a capture, an anchor, another quantifier or nothing.
func quantifiable(p string, i int) bool {
	if i == 0 {
		return false
	}
	prev := p[i-1]
	if escapedAt(p, i-1) {
		return true // %d, %(, %%, ...: one single-char item
	}
	switch prev {
	case '(', ')', '|', '^':
		return false
	case '*', '+', '?', '-':
		// A quantifier cannot be quantified ("%d++" is %d+ and a plain
		// "+"); a plain-character one can.
		return !quantifiable(p, i-1)
	}
	return true
}

// escapedAt reports whether p[j] follows an unpaired "%" (an odd run of
// them), i.e. is the second byte of an escape or class.
func escapedAt(p string, j int) bool {
	n := 0
	for k := j - 1; k >= 0 && p[k] == '%'; k-- {
		n++
	}
	return n%2 == 1
}

// luaSet translates a set starting at p[i] == '['; it returns the index of
// the closing ']'.
func luaSet(p string, i int) (int, string, error) {
	var b strings.Builder
	b.WriteByte('[')
	j := i + 1
	if j < len(p) && p[j] == '^' {
		b.WriteByte('^')
		j++
	}
	first := true
	for ; j < len(p); j++ {
		c := p[j]
		if c == ']' && !first {
			b.WriteByte(']')
			return j, b.String(), nil
		}
		first = false
		switch {
		case c == '%' && j+1 < len(p):
			j++
			if cls, ok := luaClass(p[j]); ok {
				if strings.HasPrefix(cls, "[^") {
					return 0, "", fmt.Errorf("lua pattern %q: negated class in a set", p)
				}
				b.WriteString(cls[1 : len(cls)-1])
			} else {
				b.WriteString(regexp.QuoteMeta(string(p[j])))
			}
		case c == '-' && j+1 < len(p) && p[j+1] != ']' && b.Len() > 1:
			b.WriteByte('-')
		case c == '\\' || c == '[' || c == ']' || c == '^' || c == '-':
			b.WriteString(`\` + string(c))
		default:
			b.WriteByte(c)
		}
	}
	return 0, "", fmt.Errorf("lua pattern %q: unterminated set", p)
}
