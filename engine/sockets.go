package engine

import (
	"math"

	"github.com/Mi7teR/poe-lib/pobcode"
)

// socketCounts ports the socket part of CalcSetup.lua's item loop: per
// gear slot, gems of enabled non-item groups in the slot fill its sockets
// in order; the rest are empty. It sets the SocketedGemsIn<slot>,
// Socketed<Colour>GemsIn<slot>, SocketedSkillGemsIn<slot>,
// EmptySocketIn<slot> and Empty<Colour>SocketsInAnySlot multipliers.
func (c *Calculator) socketCounts(db *ModDB, b *pobcode.Build, gear []equipped, config *pobcode.ConfigSet) {
	set := b.ActiveSkills()
	mult := db.multipliers
	colours := map[byte]string{'R': "Red", 'G': "Green", 'B': "Blue", 'W': "White"}
	for _, g := range gear {
		it := g.it
		if it.Base == nil {
			continue
		}
		switch it.Base.Type {
		case "Jewel", "Flask", "Tincture", "Graft", "Viscera":
			continue
		}
		type gemRef struct {
			tags map[string]bool
		}
		var socketed []gemRef
		if set != nil {
			for i := range set.Groups {
				grp := &set.Groups[i]
				if grp.Source != "" || !grp.Enabled || grp.Slot != g.slot {
					continue
				}
				for j := range grp.Gems {
					gm := &grp.Gems[j]
					gem, _, ok := c.resolveGem(gm)
					if !ok || gem == nil || !gm.Enabled {
						continue
					}
					tags := map[string]bool{}
					for _, t := range gem.Tags {
						tags[t] = true
					}
					socketed = append(socketed, gemRef{tags})
				}
			}
		}
		empty := map[byte]float64{}
		gemSockets := 0
		for i, s := range it.Sockets {
			if _, ok := colours[s.Color]; ok {
				gemSockets++
				if i+1 > len(socketed) {
					empty[s.Color]++
				}
			}
		}
		n := min(gemSockets, len(socketed))
		var red, green, blue, skills float64
		for _, s := range socketed[:n] {
			if !s.tags["support"] {
				skills++
			}
			if s.tags["strength"] {
				red++
			}
			if s.tags["dexterity"] {
				green++
			}
			if s.tags["intelligence"] {
				blue++
			}
		}
		mult["SocketedGemsIn"+g.slot] = float64(n)
		mult["SocketedRedGemsIn"+g.slot] = red
		mult["SocketedGreenGemsIn"+g.slot] = green
		mult["SocketedBlueGemsIn"+g.slot] = blue
		mult["SocketedSkillGemsIn"+g.slot] = skills
		mult["EmptySocketIn"+g.slot] = math.Min(float64(gemSockets), empty['R']+empty['G']+empty['B']+empty['W'])
		for col, name := range colours {
			mult["Empty"+name+"SocketsInAnySlot"] += empty[col]
		}
	}
	for _, name := range []string{"Red", "Green", "Blue", "White"} {
		if in, ok := configInput(config, "overrideEmpty"+name+"Sockets"); ok && in.Kind == pobcode.KindNumber {
			mult["Empty"+name+"SocketsInAnySlot"] = in.Number
		}
	}
}
