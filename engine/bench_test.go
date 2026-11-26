package engine

import (
	"strconv"
	"testing"
)

// benchBuilds returns corpus builds of different size and shape.
func benchBuilds() []string {
	return []string{"2HB8-0xwYEA6", "vPuXJxFgodF9", "VFJTgt8AL50o", "-fxcKCnOf7D2", "3uy_tEm_vqpB"}
}

func BenchmarkCalc(b *testing.B) {
	c := newCalc(b)
	for _, id := range benchBuilds() {
		build := corpusBuild(b, id)
		b.Run(id, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := c.Calc(build); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCombat(b *testing.B) {
	c := newCalc(b)
	for _, id := range benchBuilds() {
		build := corpusBuild(b, id)
		b.Run(id, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := c.Combat(build, CombatOptions{}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkFullDPS(b *testing.B) {
	c := newCalc(b)
	for _, id := range benchBuilds() {
		build := corpusBuild(b, id)
		b.Run(id, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, err := c.FullDPS(build); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCompareItem is one Comparer.Item call, a rare ring in Ring 1,
// with the base computed once outside the loop.
func BenchmarkCompareItem(b *testing.B) {
	c := newCalc(b)
	for _, id := range benchBuilds() {
		cp, err := c.NewComparer(corpusBuild(b, id))
		if err != nil {
			b.Fatal(err)
		}
		b.Run(id, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if _, ierr := cp.Item("Ring 1", gameRing); ierr != nil {
					b.Fatal(ierr)
				}
			}
		})
	}
}

// BenchmarkTimelessLoad is the first use of each timeless jewel table on
// a fresh table set, as a new Calculator does: "decode" decompresses the
// table, "cached" reads it from a warm decoded table cache.
func BenchmarkTimelessLoad(b *testing.B) {
	c := newCalc(b)
	dir := defaultTimelessDir()
	if dir == "" {
		b.Skip("timeless jewel tables not found")
	}
	t := c.data.Timeless
	cache := b.TempDir()
	for _, mode := range []string{"decode", "cached"} {
		for _, typ := range []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11} {
			load := func() {
				l := &timelessLUTs{dir: dir}
				if mode == "cached" {
					l.cacheDir = cache
				}
				var err error
				switch {
				case typ == 1:
					_, err = l.gloriousVanity(t)
				case typ >= 7:
					_, err = l.abyss(typ, t.LocalToGlobal[typ])
				default:
					_, err = l.table(typ)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
			if mode == "cached" {
				load() // warm the cache
			}
			b.Run(mode+"/"+strconv.Itoa(typ), func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					load()
				}
			})
		}
	}
}
