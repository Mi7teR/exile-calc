package luadata

import (
	"fmt"
	"math"
	"strconv"
)

type scope struct {
	vars   map[string]Value
	parent *scope
}

func (s *scope) lookup(name string) (Value, bool) {
	for ; s != nil; s = s.parent {
		if v, ok := s.vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}

type evaluator struct {
	src  string
	toks []token
	i    int
	sc   *scope
	root *scope // chunk scope: where unknown globals are created
	env  map[string]Value
}

// Eval evaluates a data file and returns what it returns. If the file returns
// a function, the function is run with every parameter left as a name, so
// calls through them become Call values (Skills/*.lua: mod, flag, skill).
// Its result is what the function returns or, if it returns nothing, the
// table its first parameter was filled into (Bases/*.lua: itemBases[k] = v).
// env provides globals; unknown globals become Refs, and assigning into one
// creates a table.
func Eval(src string, env map[string]Value) (Value, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	root := &scope{vars: map[string]Value{}}
	e := &evaluator{src: src, toks: toks, sc: root, root: root, env: env}
	v, _, err := e.block(tEOF)
	if err != nil {
		return nil, err
	}
	fn, ok := v.(*Func)
	if !ok {
		return v, nil
	}
	args := make([]Value, len(fn.params))
	for i, p := range fn.params {
		args[i] = Ref(p)
	}
	ret, sc, err := e.run(fn, args)
	if err != nil {
		return nil, err
	}
	if ret != nil {
		return ret, nil
	}
	if len(fn.params) > 0 {
		if t, ok := sc.vars[fn.params[0]].(*Table); ok {
			return t, nil
		}
	}
	return &Table{}, nil
}

func (e *evaluator) tok() token     { return e.toks[e.i] }
func (e *evaluator) advance() token { t := e.toks[e.i]; e.i++; return t }

func (e *evaluator) is(kind tokKind, text string) bool {
	t := e.tok()
	return t.kind == kind && t.text == text
}

func (e *evaluator) accept(kind tokKind, text string) bool {
	if e.is(kind, text) {
		e.i++
		return true
	}
	return false
}

func (e *evaluator) errf(format string, a ...any) error {
	return fmt.Errorf("line %d: %s", e.tok().line, fmt.Sprintf(format, a...))
}

func describe(t token) string {
	switch t.kind {
	case tEOF:
		return "end of file"
	case tString:
		return "string"
	case tNumber:
		return "number"
	}
	return strconv.Quote(t.text)
}

func (e *evaluator) expect(kind tokKind, text string) error {
	if !e.accept(kind, text) {
		return e.errf("expected %q, found %s", text, describe(e.tok()))
	}
	return nil
}

// block runs statements until the terminator (EOF for a chunk) and returns
// the value of a return statement, if any.
func (e *evaluator) block(term tokKind) (Value, bool, error) {
	for e.tok().kind != term {
		if e.accept(tOp, ";") {
			continue
		}
		if e.accept(tKeyword, "return") {
			var v Value
			if e.tok().kind != term && !e.is(tOp, ";") {
				var err error
				if v, err = e.expr(); err != nil {
					return nil, false, err
				}
			}
			e.accept(tOp, ";")
			if e.tok().kind != term {
				return nil, false, e.errf("statements after return")
			}
			return v, true, nil
		}
		if err := e.statement(); err != nil {
			return nil, false, err
		}
	}
	return nil, false, nil
}

func (e *evaluator) statement() error {
	t := e.tok()
	switch {
	case t.kind == tKeyword && t.text == "local":
		e.i++
		if e.is(tKeyword, "function") {
			return e.errf("local function declarations are not supported")
		}
		var names []string
		for {
			n := e.tok()
			if n.kind != tName {
				return e.errf("expected name after local")
			}
			e.i++
			names = append(names, n.text)
			if !e.accept(tOp, ",") {
				break
			}
		}
		vals := make([]Value, len(names))
		if e.accept(tOp, "=") {
			for i := 0; ; i++ {
				v, err := e.expr()
				if err != nil {
					return err
				}
				if i < len(vals) {
					vals[i] = v
				}
				if !e.accept(tOp, ",") {
					break
				}
			}
		}
		for i, n := range names {
			e.sc.vars[n] = vals[i]
		}
		return nil
	case t.kind == tKeyword && t.text == "function" && e.toks[e.i+1].kind == tName:
		// "function a.b(...) ... end": store the function like an assignment.
		start := e.advance()
		path := []string{e.advance().text}
		for e.accept(tOp, ".") {
			n := e.tok()
			if n.kind != tName {
				return e.errf("expected field name")
			}
			e.i++
			path = append(path, n.text)
		}
		fn, err := e.funcRest(start)
		if err != nil {
			return err
		}
		if len(path) == 1 {
			e.assignName(path[0], fn)
			return nil
		}
		tb := e.tableFor(path[0])
		for _, k := range path[1 : len(path)-1] {
			tb = vivify(tb, String(k))
		}
		tb.Set(path[len(path)-1], fn)
		return nil
	case t.kind == tName || (t.kind == tOp && t.text == "("):
		return e.exprStatement()
	}
	return e.errf("unsupported statement starting with %s", describe(t))
}

// exprStatement is an assignment "a[k].f = v" or a call statement.
func (e *evaluator) exprStatement() error {
	if e.tok().kind == tName && (e.toks[e.i+1].kind == tOp && (e.toks[e.i+1].text == "[" || e.toks[e.i+1].text == "." || e.toks[e.i+1].text == "=")) {
		if ok, err := e.tryAssignment(); ok || err != nil {
			return err
		}
	}
	v, err := e.suffixed(true)
	if err != nil {
		return err
	}
	if _, ok := v.(callResult); !ok {
		return e.errf("expression is not a statement")
	}
	return nil
}

// tryAssignment parses "name {[k] | .f} = expr". It rewinds if no "=" follows.
func (e *evaluator) tryAssignment() (bool, error) {
	save := e.i
	name := e.advance().text
	var keys []Value
	for {
		switch {
		case e.accept(tOp, "."):
			n := e.tok()
			if n.kind != tName {
				return true, e.errf("expected field name")
			}
			e.i++
			keys = append(keys, String(n.text))
			continue
		case e.accept(tOp, "["):
			k, err := e.expr()
			if err != nil {
				return true, err
			}
			if err := e.expect(tOp, "]"); err != nil {
				return true, err
			}
			keys = append(keys, k)
			continue
		case e.accept(tOp, "="):
		default:
			e.i = save // not an assignment; key expressions have no effects
			return false, nil
		}
		break
	}
	v, err := e.expr()
	if err != nil {
		return true, err
	}
	if len(keys) == 0 {
		e.assignName(name, v)
		return true, nil
	}
	tb := e.tableFor(name)
	for _, k := range keys[:len(keys)-1] {
		tb = vivify(tb, k)
	}
	tb.Set(keys[len(keys)-1], v)
	return true, nil
}

// assignName sets a variable where it is declared, or as a chunk global.
func (e *evaluator) assignName(name string, v Value) {
	for s := e.sc; s != nil; s = s.parent {
		if _, ok := s.vars[name]; ok {
			s.vars[name] = v
			return
		}
	}
	e.root.vars[name] = v
}

// tableFor returns the table a name holds, creating one if the name holds
// none (an unknown global or a function parameter that is filled in).
func (e *evaluator) tableFor(name string) *Table {
	if v, ok := e.sc.lookup(name); ok {
		if t, ok := v.(*Table); ok {
			return t
		}
	} else if t, ok := e.env[name].(*Table); ok {
		return t
	}
	t := &Table{}
	e.assignName(name, t)
	return t
}

func vivify(tb *Table, k Value) *Table {
	if t, ok := tb.Get(k).(*Table); ok {
		return t
	}
	t := &Table{}
	tb.Set(k, t)
	return t
}

// callResult marks the value of a call used as a statement.
type callResult struct{}

func (e *evaluator) expr() (Value, error) {
	v, err := e.concat()
	if err != nil {
		return nil, err
	}
	t := e.tok()
	if t.kind == tOp {
		switch t.text {
		case "==", "~=", "<", ">", "<=", ">=":
			return nil, e.errf("unsupported operator %q", t.text)
		}
	}
	if t.kind == tKeyword && (t.text == "and" || t.text == "or") {
		return nil, e.errf("unsupported operator %q", t.text)
	}
	return v, nil
}

func (e *evaluator) concat() (Value, error) {
	l, err := e.additive()
	if err != nil || !e.is(tOp, "..") {
		return l, err
	}
	e.i++
	r, err := e.concat() // right associative
	if err != nil {
		return nil, err
	}
	ls, lok := toStr(l)
	rs, rok := toStr(r)
	if !lok || !rok {
		return nil, e.errf("cannot concatenate %s and %s", typeName(l), typeName(r))
	}
	return String(ls + rs), nil
}

func toStr(v Value) (string, bool) {
	switch x := v.(type) {
	case String:
		return string(x), true
	case Number:
		return strconv.FormatFloat(float64(x), 'g', 14, 64), true
	}
	return "", false
}

func (e *evaluator) arith(op string, l, r Value) (Value, error) {
	a, aok := l.(Number)
	b, bok := r.(Number)
	if !aok || !bok {
		return nil, e.errf("cannot apply %q to %s and %s", op, typeName(l), typeName(r))
	}
	switch op {
	case "+":
		return a + b, nil
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "/":
		return a / b, nil
	case "%":
		return Number(float64(a) - math.Floor(float64(a)/float64(b))*float64(b)), nil
	case "^":
		return Number(math.Pow(float64(a), float64(b))), nil
	}
	return nil, e.errf("unsupported operator %q", op)
}

func (e *evaluator) additive() (Value, error) {
	l, err := e.multiplicative()
	for err == nil && (e.is(tOp, "+") || e.is(tOp, "-")) {
		op := e.advance().text
		var r Value
		if r, err = e.multiplicative(); err == nil {
			l, err = e.arith(op, l, r)
		}
	}
	return l, err
}

func (e *evaluator) multiplicative() (Value, error) {
	l, err := e.unary()
	for err == nil && (e.is(tOp, "*") || e.is(tOp, "/") || e.is(tOp, "%")) {
		op := e.advance().text
		var r Value
		if r, err = e.unary(); err == nil {
			l, err = e.arith(op, l, r)
		}
	}
	return l, err
}

func (e *evaluator) unary() (Value, error) {
	if e.accept(tOp, "-") {
		v, err := e.unary()
		if err != nil {
			return nil, err
		}
		n, ok := v.(Number)
		if !ok {
			return nil, e.errf("cannot negate %s", typeName(v))
		}
		return -n, nil
	}
	if e.is(tKeyword, "not") || e.is(tOp, "#") {
		return nil, e.errf("unsupported operator %q", e.tok().text)
	}
	v, err := e.primary()
	if err != nil {
		return nil, err
	}
	if e.accept(tOp, "^") {
		r, err := e.unary()
		if err != nil {
			return nil, err
		}
		return e.arith("^", v, r)
	}
	return v, nil
}

func (e *evaluator) primary() (Value, error) {
	t := e.tok()
	switch t.kind {
	case tNumber:
		e.i++
		return Number(t.num), nil
	case tString:
		e.i++
		return String(t.text), nil
	case tKeyword:
		switch t.text {
		case "nil":
			e.i++
			return nil, nil
		case "true":
			e.i++
			return Bool(true), nil
		case "false":
			e.i++
			return Bool(false), nil
		case "function":
			return e.function()
		}
	case tOp:
		if t.text == "{" {
			return e.table()
		}
	}
	v, err := e.suffixed(false)
	if _, isCall := v.(callResult); isCall && err == nil {
		return nil, e.errf("a function run for its effects has no value")
	}
	return v, err
}

// suffixed parses a name or "(expr)" followed by fields, indexes and calls.
// asStatement allows a call whose only effect is running a Func.
func (e *evaluator) suffixed(asStatement bool) (Value, error) {
	var cur Value
	path := ""
	t := e.tok()
	switch {
	case t.kind == tName:
		e.i++
		path = t.text
		if v, ok := e.sc.lookup(t.text); ok {
			cur = v
		} else if v, ok := e.env[t.text]; ok {
			cur = v
		} else {
			cur = Ref(t.text)
		}
	case t.kind == tOp && t.text == "(":
		e.i++
		v, err := e.expr()
		if err != nil {
			return nil, err
		}
		if err := e.expect(tOp, ")"); err != nil {
			return nil, err
		}
		cur = v
	default:
		return nil, e.errf("unexpected %s", describe(t))
	}
	for {
		t := e.tok()
		switch {
		case t.kind == tOp && t.text == ".":
			e.i++
			n := e.tok()
			if n.kind != tName {
				return nil, e.errf("expected field name")
			}
			e.i++
			switch c := cur.(type) {
			case *Table:
				if c.Has(n.text) {
					cur = c.Get(n.text)
				} else {
					cur = Ref(path + "." + n.text)
				}
			case Ref:
				cur = Ref(string(c) + "." + n.text)
			default:
				return nil, e.errf("cannot index %s", typeName(cur))
			}
			path += "." + n.text
		case t.kind == tOp && t.text == "[":
			e.i++
			k, err := e.expr()
			if err != nil {
				return nil, err
			}
			if err := e.expect(tOp, "]"); err != nil {
				return nil, err
			}
			tb, ok := cur.(*Table)
			if !ok {
				return nil, e.errf("cannot index %s", typeName(cur))
			}
			cur = tb.Get(k)
			path = ""
		case t.kind == tOp && t.text == ":":
			return nil, e.errf("method calls are not supported")
		case (t.kind == tOp && (t.text == "(" || t.text == "{")) || t.kind == tString:
			args, err := e.callArgs()
			if err != nil {
				return nil, err
			}
			switch fn := cur.(type) {
			case Ref:
				cur = &Call{Func: string(fn), Args: args}
			case *Func:
				if !asStatement {
					return nil, e.errf("calling a function for its value is not supported")
				}
				if _, _, err := e.run(fn, args); err != nil {
					return nil, err
				}
				cur = callResult{}
			default:
				return nil, e.errf("cannot call %s", typeName(cur))
			}
			path = ""
		default:
			return cur, nil
		}
	}
}

func (e *evaluator) callArgs() ([]Value, error) {
	t := e.tok()
	if t.kind == tString {
		e.i++
		return []Value{String(t.text)}, nil
	}
	if t.text == "{" {
		v, err := e.table()
		return []Value{v}, err
	}
	e.i++ // "("
	var args []Value
	if e.accept(tOp, ")") {
		return args, nil
	}
	for {
		v, err := e.expr()
		if err != nil {
			return nil, err
		}
		args = append(args, v)
		if e.accept(tOp, ")") {
			return args, nil
		}
		if err := e.expect(tOp, ","); err != nil {
			return nil, err
		}
	}
}

func (e *evaluator) table() (Value, error) {
	if err := e.expect(tOp, "{"); err != nil {
		return nil, err
	}
	tb := &Table{}
	for !e.accept(tOp, "}") {
		switch {
		case e.is(tOp, "["):
			e.i++
			k, err := e.expr()
			if err != nil {
				return nil, err
			}
			if err := e.expect(tOp, "]"); err != nil {
				return nil, err
			}
			if err := e.expect(tOp, "="); err != nil {
				return nil, err
			}
			v, err := e.expr()
			if err != nil {
				return nil, err
			}
			tb.Set(k, v)
		case e.tok().kind == tName && e.toks[e.i+1].kind == tOp && e.toks[e.i+1].text == "=":
			k := e.advance().text
			e.i++ // "="
			v, err := e.expr()
			if err != nil {
				return nil, err
			}
			tb.Set(k, v)
		default:
			v, err := e.expr()
			if err != nil {
				return nil, err
			}
			tb.List = append(tb.List, v)
		}
		if !e.accept(tOp, ",") && !e.accept(tOp, ";") {
			if err := e.expect(tOp, "}"); err != nil {
				return nil, err
			}
			break
		}
	}
	return tb, nil
}

// function captures a function literal without running it: its parameters,
// its body tokens and its source text. Nested blocks are matched by keyword.
func (e *evaluator) function() (Value, error) {
	return e.funcRest(e.advance())
}

// funcRest parses a function after its "function" keyword (and name, if any).
func (e *evaluator) funcRest(start token) (*Func, error) {
	if err := e.expect(tOp, "("); err != nil {
		return nil, err
	}
	var params []string
	for !e.accept(tOp, ")") {
		t := e.advance()
		if t.kind == tName {
			params = append(params, t.text)
		} else if !(t.kind == tOp && (t.text == "," || t.text == "...")) {
			return nil, fmt.Errorf("line %d: bad parameter list", t.line)
		}
	}
	bodyStart := e.i
	depth := 1
	for {
		t := e.tok()
		if t.kind == tEOF {
			return nil, fmt.Errorf("line %d: function is missing its end", start.line)
		}
		if t.kind == tKeyword {
			switch t.text {
			case "function", "if", "do", "repeat":
				depth++
			case "end", "until":
				depth--
			}
		}
		if depth == 0 {
			break
		}
		e.i++
	}
	end := e.advance() // "end"
	body := append(append([]token{}, e.toks[bodyStart:e.i-1]...), token{kind: tEOF, line: end.line})
	return &Func{
		Source:    e.src[start.pos:end.end],
		Line:      start.line,
		params:    params,
		body:      body,
		bodyScope: e.sc,
	}, nil
}

// run executes a Func body with its parameters bound to args and returns
// what the body returns together with the scope it ran in.
func (e *evaluator) run(fn *Func, args []Value) (Value, *scope, error) {
	sc := &scope{vars: map[string]Value{}, parent: fn.bodyScope}
	for i, p := range fn.params {
		if i < len(args) {
			sc.vars[p] = args[i]
		} else {
			sc.vars[p] = nil
		}
	}
	sub := &evaluator{src: e.src, toks: fn.body, sc: sc, root: e.root, env: e.env}
	v, _, err := sub.block(tEOF)
	return v, sc, err
}
