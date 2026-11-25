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
