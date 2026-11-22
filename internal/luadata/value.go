// Package luadata reads the data files of Path of Building (src/Data/*.lua)
// without running Lua. It understands the subset those files are written in:
// table constructors, literals, constant arithmetic and concatenation,
// locals, assignments into tables, "return function(t) ... end" wrappers and
// immediately invoked "(function() ... end)()" blocks.
//
// What it cannot evaluate it keeps as data: an unknown name becomes a Ref,
// a call through a Ref becomes a Call, and a function literal becomes a Func
// holding its source text. Anything else is an error with a line number.
package luadata

import "fmt"

// Value is nil, Bool, Number, String, *Table, Ref, *Call or *Func.
type Value any

type Bool bool
type Number float64
type String string

// Ref is a name the evaluator could not resolve, such as "SkillType.Spell".
type Ref string

// Call is a call through a Ref, such as mod("Life", "BASE", 10).
type Call struct {
	Func string
	Args []Value
}

// Func is a function literal that was not executed.
type Func struct {
	Source string // "function(...) ... end"
	Line   int    // line of the "function" keyword

	params    []string
	body      []token
	bodyScope *scope
}

// Table is a Lua table: List holds positional entries (Lua indexes 1..n),
// Keys holds explicit keys in the order they were first set.
type Table struct {
	List []Value
	Keys []Value
	vals map[Value]Value
}

func key(k any) Value {
	switch v := k.(type) {
	case string:
		return String(v)
	case int:
		return Number(v)
	case float64:
		return Number(v)
	}
	return k
}

// Get looks up an explicit key; a whole number falls back to the list part.
// Plain Go strings and numbers are accepted for convenience.
func (t *Table) Get(k any) Value {
	kk := key(k)
	if v, ok := t.vals[kk]; ok {
		return v
	}
	if n, ok := kk.(Number); ok && n >= 1 && float64(n) == float64(int(n)) && int(n) <= len(t.List) {
		return t.List[int(n)-1]
	}
	return nil
}

// Has reports whether an explicit key is set.
func (t *Table) Has(k any) bool {
	_, ok := t.vals[key(k)]
	return ok
}

// Set assigns an explicit key, keeping first-set order.
func (t *Table) Set(k any, v Value) {
	kk := key(k)
	if t.vals == nil {
		t.vals = map[Value]Value{}
	}
	if _, ok := t.vals[kk]; !ok {
		t.Keys = append(t.Keys, kk)
	}
	t.vals[kk] = v
}

func typeName(v Value) string {
	switch v.(type) {
	case nil:
		return "nil"
	case Bool:
		return "boolean"
	case Number:
		return "number"
	case String:
		return "string"
	case *Table:
		return "table"
	case Ref:
		return fmt.Sprintf("unresolved name %s", v)
	case *Call:
		return "call"
	case *Func:
		return "function"
	}
	return fmt.Sprintf("%T", v)
}
