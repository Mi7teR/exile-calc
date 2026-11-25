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

// optB is a bool with presence.
type optB struct{ v, set bool }

// skillData is activeSkill.skillData (a Lua table in PoB). The frequent keys
// are typed fields with presence; the rest, and any value of another type
// than its field's, live in extra: a present nil (hitTimeOverride copied from
// an absent damageInterval) or a statMap SkillData value, which the stat's
// number replaces (showAverage and triggered arrive as float64 that way).
// All string-keyed access goes through get, set and del.
//
// A set field wins over extra, so code may write a float field directly
// (d.cooldown = optF{v, true}) without clearing a stale extra entry. Code
// reads a field directly only where extra cannot hold another number or a
// truthy value for its key; the bool fields are read through val because
// statMap values land in extra.
type skillData struct {
	dpsMultiplier, hitTimeOverride, cooldown, duration, storedUses, timeOverride optF
	triggered, showAverage                                                       optB
	// SkillData mods, statMap and dynamic keys (t+"BonusMin").
	extra map[string]any
}

// floatField and boolField route a key to its typed field, or nil.
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

func (d *skillData) boolField(key string) *optB {
	switch key {
	case "triggered":
		return &d.triggered
	case "showAverage":
		return &d.showAverage
	}
	return nil
}

// get is data[key] with presence: absent → (nil, false).
func (d *skillData) get(key string) (any, bool) {
	if f := d.floatField(key); f != nil && f.set {
		return f.v, true
	}
	if b := d.boolField(key); b != nil && b.set {
		return b.v, true
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
	if b := d.boolField(key); b != nil {
		if x, ok := v.(bool); ok {
			*b = optB{x, true}
			delete(d.extra, key)
			return
		}
		*b = optB{}
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
	if b := d.boolField(key); b != nil {
		*b = optB{}
	}
	delete(d.extra, key)
}

// clone is a copy that shares nothing with d.
func (d *skillData) clone() skillData {
	c := *d
	c.extra = maps.Clone(d.extra)
	return c
}
