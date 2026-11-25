package engine

import "testing"

func TestSkillDataPresence(t *testing.T) {
	var d skillData
	if v, ok := d.get("showAverage"); v != nil || ok {
		t.Fatalf("empty get = (%v, %v), want (nil, false)", v, ok)
	}
	if v, ok := d.get("someKey"); v != nil || ok {
		t.Fatalf("empty get extra = (%v, %v), want (nil, false)", v, ok)
	}
	d.set("showAverage", false)
	if v, ok := d.get("showAverage"); v != false || !ok {
		t.Fatalf("showAverage = (%v, %v), want (false, true)", v, ok)
	}
	d.set("dpsMultiplier", 0.0)
	if v, ok := d.get("dpsMultiplier"); v != 0.0 || !ok {
		t.Fatalf("dpsMultiplier = (%v, %v), want (0, true)", v, ok)
	}
	d.set("someKey", 3.0)
	if v, ok := d.extra["someKey"]; v != 3.0 || !ok {
		t.Fatalf("unknown key not in extra: (%v, %v)", v, ok)
	}
	// A present nil (Lua: a SkillData value copied from an absent key) keeps
	// its presence; a value of another type than the field's comes back as is.
	d.set("hitTimeOverride", nil)
	if v, ok := d.get("hitTimeOverride"); v != nil || !ok {
		t.Fatalf("hitTimeOverride nil = (%v, %v), want (nil, true)", v, ok)
	}
	d.set("hitTimeOverride", 2.0)
	if v, ok := d.get("hitTimeOverride"); v != 2.0 || !ok || d.extra["hitTimeOverride"] != nil {
		t.Fatalf("hitTimeOverride = (%v, %v), extra %v", v, ok, d.extra)
	}
	// A direct field write wins over a stale extra entry.
	d.set("hitTimeOverride", nil)
	d.hitTimeOverride = optF{4, true}
	if v, ok := d.get("hitTimeOverride"); v != 4.0 || !ok {
		t.Fatalf("hitTimeOverride direct = (%v, %v), want (4, true)", v, ok)
	}
	d.del("hitTimeOverride")
	if _, ok := d.get("hitTimeOverride"); ok {
		t.Fatal("hitTimeOverride present after del")
	}
	d.set("triggered", "yes")
	if v, ok := d.get("triggered"); v != "yes" || !ok || d.triggered.set {
		t.Fatalf("triggered string = (%v, %v), field %v", v, ok, d.triggered)
	}
	d.set("triggered", true)
	if v, ok := d.get("triggered"); v != true || !ok {
		t.Fatalf("triggered = (%v, %v)", v, ok)
	}
	d.del("triggered")
	d.del("someKey")
	if _, ok := d.get("triggered"); ok {
		t.Fatal("triggered present after del")
	}
	if _, ok := d.get("someKey"); ok {
		t.Fatal("someKey present after del")
	}
}

func TestSkillDataCopyIsIndependent(t *testing.T) {
	var d skillData
	d.set("cooldown", 1.0)
	d.set("someKey", 1.0)
	c := d.clone()
	c.set("cooldown", 2.0)
	c.set("someKey", 2.0)
	c.set("otherKey", 2.0)
	if v, _ := d.get("cooldown"); v != 1.0 {
		t.Fatalf("original cooldown = %v", v)
	}
	if v, _ := d.get("someKey"); v != 1.0 {
		t.Fatalf("original someKey = %v", v)
	}
	if _, ok := d.get("otherKey"); ok {
		t.Fatal("clone's new key visible in original")
	}
	d.set("duration", 5.0)
	d.set("someKey", 5.0)
	if _, ok := c.get("duration"); ok {
		t.Fatal("original's field visible in clone")
	}
	if v, _ := c.get("someKey"); v != 2.0 {
		t.Fatalf("clone someKey = %v", v)
	}
}
