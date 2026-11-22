package luadata

import "fmt"

// EvalGlobals runs a chunk that only defines globals (Global.lua) and returns them.
func EvalGlobals(src string, env map[string]Value) (map[string]Value, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	root := &scope{vars: map[string]Value{}}
	e := &evaluator{src: src, toks: toks, sc: root, root: root, env: env}
	if _, _, err := e.block(tEOF); err != nil {
		return nil, err
	}
	return root.vars, nil
}

// ExtractLocal evaluates the expression of the first "local <name> = <expr>"
// in src without running the rest of the file. It reads single tables out of
// a program such as Modules/ModParser.lua; functions inside stay Func values.
func ExtractLocal(src, name string, env map[string]Value) (Value, error) {
	toks, err := lex(src)
	if err != nil {
		return nil, err
	}
	for i := 0; i+3 < len(toks); i++ {
		if toks[i].kind == tKeyword && toks[i].text == "local" &&
			toks[i+1].kind == tName && toks[i+1].text == name &&
			toks[i+2].kind == tOp && toks[i+2].text == "=" {
			root := &scope{vars: map[string]Value{}}
			e := &evaluator{src: src, toks: toks, i: i + 3, sc: root, root: root, env: env}
			return e.expr()
		}
	}
	return nil, fmt.Errorf("luadata: no \"local %s =\" found", name)
}
