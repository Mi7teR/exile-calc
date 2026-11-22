package luadata

import (
	"fmt"
	"strconv"
	"strings"
)

type tokKind int

const (
	tEOF tokKind = iota
	tName
	tKeyword
	tString
	tNumber
	tOp
)

type token struct {
	kind tokKind
	text string // name, keyword, operator, or decoded string
	num  float64
	line int
	pos  int // byte offset of the token start
	end  int // byte offset just after the token
}

var keywords = map[string]bool{
	"and": true, "break": true, "do": true, "else": true, "elseif": true, "end": true,
	"false": true, "for": true, "function": true, "goto": true, "if": true, "in": true,
	"local": true, "nil": true, "not": true, "or": true, "repeat": true, "return": true,
	"then": true, "true": true, "until": true, "while": true,
}

type lexer struct {
	src  string
	i    int
	line int
}

func lex(src string) ([]token, error) {
	l := &lexer{src: src, line: 1}
	var toks []token
	for {
		t, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.kind == tEOF {
			return toks, nil
		}
	}
}

func (l *lexer) errf(format string, a ...any) error {
	return fmt.Errorf("line %d: %s", l.line, fmt.Sprintf(format, a...))
}

func (l *lexer) peek(off int) byte {
	if l.i+off < len(l.src) {
		return l.src[l.i+off]
	}
	return 0
}

// longBracket returns the level of a long bracket "[==[" at l.i, or -1.
func (l *lexer) longBracket() int {
	if l.peek(0) != '[' {
		return -1
	}
	n := 1
	for l.peek(n) == '=' {
		n++
	}
	if l.peek(n) == '[' {
		return n - 1
	}
	return -1
}

func (l *lexer) readLong(level int) (string, error) {
	startLine := l.line
	l.i += level + 2
	if l.peek(0) == '\r' {
		l.i++
	}
	if l.peek(0) == '\n' { // first newline is skipped
		l.i++
		l.line++
	}
	closer := "]" + strings.Repeat("=", level) + "]"
	j := strings.Index(l.src[l.i:], closer)
	if j < 0 {
		l.line = startLine
		return "", l.errf("unterminated long string or comment")
	}
	s := l.src[l.i : l.i+j]
	l.line += strings.Count(s, "\n")
	l.i += j + len(closer)
	return s, nil
}

func (l *lexer) skipSpaceAndComments() error {
	for l.i < len(l.src) {
		c := l.src[l.i]
		switch {
		case c == '\n':
			l.line++
			l.i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			l.i++
		case c == '-' && l.peek(1) == '-':
			l.i += 2
			if lvl := l.longBracket(); lvl >= 0 {
				if _, err := l.readLong(lvl); err != nil {
					return err
				}
				continue
			}
			for l.i < len(l.src) && l.src[l.i] != '\n' {
				l.i++
			}
		default:
			return nil
		}
	}
	return nil
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c == '_' || (c|0x20 >= 'a' && c|0x20 <= 'z') }

func (l *lexer) next() (token, error) {
	if err := l.skipSpaceAndComments(); err != nil {
		return token{}, err
	}
	start, line := l.i, l.line
	mk := func(k tokKind, text string, num float64) token {
		return token{kind: k, text: text, num: num, line: line, pos: start, end: l.i}
	}
	if l.i >= len(l.src) {
		return mk(tEOF, "", 0), nil
	}
	c := l.src[l.i]
	switch {
	case isLetter(c):
		for l.i < len(l.src) && (isLetter(l.src[l.i]) || isDigit(l.src[l.i])) {
			l.i++
		}
		w := l.src[start:l.i]
		if keywords[w] {
			return mk(tKeyword, w, 0), nil
		}
		return mk(tName, w, 0), nil
	case isDigit(c) || (c == '.' && isDigit(l.peek(1))):
		return l.number(start, line)
	case c == '"' || c == '\'':
		s, err := l.quoted(c)
		if err != nil {
			return token{}, err
		}
		return mk(tString, s, 0), nil
	case c == '[':
		if lvl := l.longBracket(); lvl >= 0 {
			s, err := l.readLong(lvl)
			if err != nil {
				return token{}, err
			}
			return token{kind: tString, text: s, line: line, pos: start, end: l.i}, nil
		}
	}
	for _, op := range []string{"...", "..", "==", "~=", "<=", ">=", "::", "//"} {
		if strings.HasPrefix(l.src[l.i:], op) {
			l.i += len(op)
			return mk(tOp, op, 0), nil
		}
	}
	if strings.IndexByte("+-*/%^#&~|<>=(){}[];:,.", c) >= 0 {
		l.i++
		return mk(tOp, string(c), 0), nil
	}
	return token{}, l.errf("unexpected character %q", c)
}

func (l *lexer) number(start, line int) (token, error) {
	if l.peek(0) == '0' && (l.peek(1)|0x20) == 'x' {
		l.i += 2
		for l.i < len(l.src) && strings.IndexByte("0123456789abcdefABCDEF", l.src[l.i]) >= 0 {
			l.i++
		}
		v, err := strconv.ParseUint(l.src[start+2:l.i], 16, 64)
		if err != nil {
			return token{}, l.errf("bad hex number %q", l.src[start:l.i])
		}
		return token{kind: tNumber, num: float64(v), line: line, pos: start, end: l.i}, nil
	}
	for l.i < len(l.src) {
		c := l.src[l.i]
		if isDigit(c) || c == '.' {
			l.i++
		} else if (c|0x20) == 'e' && l.i > start {
			l.i++
			if l.peek(0) == '+' || l.peek(0) == '-' {
				l.i++
			}
		} else {
			break
		}
	}
	v, err := strconv.ParseFloat(l.src[start:l.i], 64)
	if err != nil {
		return token{}, l.errf("bad number %q", l.src[start:l.i])
	}
	return token{kind: tNumber, num: v, line: line, pos: start, end: l.i}, nil
}

func (l *lexer) quoted(q byte) (string, error) {
	l.i++
	var b strings.Builder
	for {
		if l.i >= len(l.src) || l.src[l.i] == '\n' {
			return "", l.errf("unterminated string")
		}
		c := l.src[l.i]
		if c == q {
			l.i++
			return b.String(), nil
		}
		if c != '\\' {
			b.WriteByte(c)
			l.i++
			continue
		}
		l.i++
		e := l.peek(0)
		l.i++
		switch e {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '\\', '"', '\'':
			b.WriteByte(e)
		case '\n':
			b.WriteByte('\n')
			l.line++
		case 'x':
			if l.i+2 > len(l.src) {
				return "", l.errf("bad \\x escape")
			}
			v, err := strconv.ParseUint(l.src[l.i:l.i+2], 16, 8)
			if err != nil {
				return "", l.errf("bad \\x escape")
			}
			b.WriteByte(byte(v))
			l.i += 2
		case 'z':
			for l.i < len(l.src) && strings.IndexByte(" \t\r\n", l.src[l.i]) >= 0 {
				if l.src[l.i] == '\n' {
					l.line++
				}
				l.i++
			}
		default:
			if !isDigit(e) {
				return "", l.errf("bad escape \\%c", e)
			}
			j := l.i - 1
			for l.i < len(l.src) && l.i-j < 3 && isDigit(l.src[l.i]) {
				l.i++
			}
			v, _ := strconv.Atoi(l.src[j:l.i])
			if v > 255 {
				return "", l.errf("escape \\%d out of range", v)
			}
			b.WriteByte(byte(v))
		}
	}
}
