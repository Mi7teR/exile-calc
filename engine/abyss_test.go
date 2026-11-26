package engine

import (
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/Mi7teR/exile-calc/gamedata"
)

func TestFixLegionStat(t *testing.T) {
	cases := []struct {
		line, key string
		sm        gamedata.LegionStat
		v         float64
		want      string
	}{
		{"+(21-30) to maximum Life", "base_maximum_life", gamedata.LegionStat{Fmt: "d", Index: 1, Min: 21, Max: 30}, 25, "+25 to maximum Life"},
		{"+12 to Strength", "additional_strength", gamedata.LegionStat{Fmt: "d", Index: 1, Min: 12, Max: 12}, 16, "+16 to Strength"},
		{"Regenerate (1-2) Life per second", "life_regeneration_rate_per_minute", gamedata.LegionStat{Fmt: "g", Index: 1, Min: 1, Max: 2}, 90, "Regenerate 1.5 Life per second"},
	}
	for _, c := range cases {
		if got := fixLegionStat(c.line, c.key, c.sm, c.v); got != c.want {
			t.Errorf("%q: got %q, want %q", c.line, got, c.want)
		}
	}
}

// A synthetic ABYS table: one socket (1234), seeds 100..102, the second
// seed changing node 555 by addition 7 with rolls 5 and -3.
func TestParseAbyss(t *testing.T) {
	le := func(v int) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, uint16(v)); return b }
	b := []byte("ABYS")
	b = append(b, 1, 7)
	b = append(b, le(100)...)
	b = append(b, le(102)...)
	b = append(b, le(1)...)
	b = append(b, 1, 0) // socket count, abyssSize
	b = append(b, le(1234)...)
	b = append(b, 0)          // seed 100: no nodes
	b = append(b, 1)          // seed 101: one node
	b = append(b, le(555)...) // node id
	b = append(b, 1, 2, 7, 2) // one component: type 2, local id 7, 2 stats
	b = append(b, le(5)...)
	b = append(b, le(-3&0xffff)...)
	b = append(b, 0) // seed 102
	l, err := parseAbyss(7, b, map[int]int{7: 9})
	if err != nil {
		t.Fatal(err)
	}
	got := l.read(101, 1234, nil)
	want := map[int][]abyssComponent{555: {{typ: 2, id: 9, rolls: []int{5, -3}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if got := l.read(100, 1234, nil); len(got) != 0 {
		t.Errorf("seed 100: %+v", got)
	}
	if _, err := parseAbyss(8, b, nil); err == nil {
		t.Error("wrong jewel type must fail")
	}
}
