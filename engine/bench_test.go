package engine

import "testing"

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
