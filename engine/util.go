package engine

// Small helpers shared across the engine.

// b2f converts a bool to 1 or 0.
func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// numOr is num(v) with a default.
func numOr(v any, def float64) float64 {
	if f, ok := num(v); ok {
		return f
	}
	return def
}

// truthy is Lua truthiness for the loosely typed skill data: nil and false are
// false, everything else is true.
func truthy(v any) bool { return v != nil && v != false }
