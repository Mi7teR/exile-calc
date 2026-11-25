package engine

import "maps"

// optF is a float with presence: a Lua table field that may be absent.
type optF struct {
	v   float64
	set bool
}

// or is the value, or def when it is not set: numOr on the field.
func (o optF) or(def float64) float64 {
	if o.set {
		return o.v
	}
	return def
}

// get is the value and whether it is set: num on the field.
func (o optF) get() (float64, bool) { return o.v, o.set }

// skillData is activeSkill.skillData (a Lua table in PoB). The frequent float
// keys are typed fields with presence; the rest, and any value of another type
// than its field's, live in extra: a present nil (hitTimeOverride copied from
// an absent damageInterval), bools, strings and dynamic keys. All string-keyed
// access goes through get, set and del. activeSkill holds it by pointer, so a
// copy of the skill shares it; cloneSkill takes its own copy with clone.
//
// A set field wins over extra, so code may write a float field directly
// (d.cooldown = optF{v, true}) without clearing a stale extra entry. Direct
// field reads rely on every number that reaches set being a float64: SkillData
// JSON numbers and statMap values are float64, so a numeric value of a typed
// key always lands in its field, and extra can hold only a non-number for it.
type skillData struct {
	dpsMultiplier, hitTimeOverride, cooldown, duration, storedUses, timeOverride optF
	// SkillData mods, statMap and dynamic keys (t+"BonusMin").
	extra map[string]any
}

// floatField routes a key to its typed field, or nil.
func (d *skillData) floatField(key string) *optF {
	switch key {
	case "dpsMultiplier":
		return &d.dpsMultiplier
	case "hitTimeOverride":
		return &d.hitTimeOverride
	case "cooldown":
		return &d.cooldown
	case "duration":
		return &d.duration
	case "storedUses":
		return &d.storedUses
	case "timeOverride":
		return &d.timeOverride
	}
	return nil
}

// get is data[key] with presence: absent → (nil, false).
func (d *skillData) get(key string) (any, bool) {
	if f := d.floatField(key); f != nil && f.set {
		return f.v, true
	}
	v, ok := d.extra[key]
	return v, ok
}

// val is data[key]: nil when absent.
func (d *skillData) val(key string) any {
	v, _ := d.get(key)
	return v
}

// set is data[key] = v. A value of its field's type goes to the field,
// anything else (nil included, which Go keeps as present) to extra.
func (d *skillData) set(key string, v any) {
	if f := d.floatField(key); f != nil {
		if x, ok := v.(float64); ok {
			*f = optF{x, true}
			delete(d.extra, key)
			return
		}
		*f = optF{}
	}
	if d.extra == nil {
		d.extra = map[string]any{}
	}
	d.extra[key] = v
}

// del is data[key] = nil in Lua: the key becomes absent.
func (d *skillData) del(key string) {
	if f := d.floatField(key); f != nil {
		*f = optF{}
	}
	delete(d.extra, key)
}

// clone is a copy that shares nothing with d.
func (d *skillData) clone() *skillData {
	c := *d
	c.extra = maps.Clone(d.extra)
	return &c
}
