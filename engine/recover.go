package engine

import (
	"errors"
	"fmt"
	"runtime/debug"
)

// ErrInternal is wrapped by the error an exported Calculator or Comparer
// method returns when the engine panicked: a bug in the engine, not bad
// input. The message carries the panic value and stack.
var ErrInternal = errors.New("engine: internal error")

// recoverErr, deferred by an exported method with a named error result,
// turns a panic into an ErrInternal error, so a build or item that hits an
// engine bug fails alone instead of taking a long-running caller (a price
// checker) down.
func recoverErr(err *error) {
	if v := recover(); v != nil {
		*err = fmt.Errorf("%w: %v\n%s", ErrInternal, v, debug.Stack())
	}
}
