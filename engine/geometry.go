package engine

import (
	"math"

	"github.com/Mi7teR/poe-lib/gamedata"
)

// orbitAngles is PassiveTreeClass:CalcOrbitAngles: orbits of 16 and 40
// skills use 30/45 and 10/45 degree steps, others are uniform.
func orbitAngles(n int) []float64 {
	var deg []float64
	switch n {
	case 16:
		deg = []float64{0, 30, 45, 60, 90, 120, 135, 150, 180, 210, 225, 240, 270, 300, 315, 330}
	case 40:
		deg = []float64{0, 10, 20, 30, 40, 45, 50, 60, 70, 80, 90, 100, 110, 120, 130, 135, 140, 150, 160, 170, 180, 190, 200, 210, 220, 225, 230, 240, 250, 260, 270, 280, 290, 300, 310, 315, 320, 330, 340, 350}
	default:
		for i := 0; i <= n; i++ {
			deg = append(deg, 360*float64(i)/float64(n))
		}
	}
	out := make([]float64, len(deg))
	for i, d := range deg {
		out[i] = d * math.Pi / 180
	}
	return out
}

type point struct{ x, y float64 }

// nodePositions ports the position part of PassiveTreeClass:ProcessNode.
func nodePositions(t *gamedata.Tree) map[int]point {
	angles := make([][]float64, len(t.Constants.SkillsPerOrbit))
	for i, n := range t.Constants.SkillsPerOrbit {
		angles[i] = orbitAngles(n)
	}
	out := make(map[int]point, len(t.Nodes))
	for id, n := range t.Nodes {
		if n.Group < 1 || n.Group > len(t.Groups) || n.Orbit >= len(angles) || n.OrbitIndex >= len(angles[n.Orbit]) || n.Orbit >= len(t.Constants.OrbitRadii) {
			continue
		}
		g := t.Groups[n.Group-1]
		a := angles[n.Orbit][n.OrbitIndex]
		r := t.Constants.OrbitRadii[n.Orbit]
		out[id] = point{g.X + math.Sin(a)*r, g.Y - math.Cos(a)*r}
	}
	return out
}

// jewelRadius is data.jewelRadii["3_16"] (Modules/Data.lua).
type radius struct {
	label        string
	inner, outer float64
}

var jewelRadii = []radius{
	{"Small", 0, 960}, {"Medium", 0, 1440}, {"Large", 0, 1800}, {"Very Large", 0, 2400}, {"Massive", 0, 2880},
	{"Variable", 960, 1320}, {"Variable", 1320, 1680}, {"Variable", 1680, 2040}, {"Variable", 2040, 2400}, {"Variable", 2400, 2880},
}

// nodesInRadius ports the socket radius precalculation (PassiveTree.lua):
// nodes with a group that are not proxies, masteries or blighted, within
// [inner, outer] of the socket.
func nodesInRadius(t *gamedata.Tree, pos map[int]point, socket int, r radius) []int {
	s, ok := pos[socket]
	if !ok {
		return nil
	}
	var out []int
	for id, p := range pos {
		n := t.Nodes[id]
		if id == socket || n.IsBlighted || n.IsProxy || n.IsMastery || t.Groups[n.Group-1].IsProxy {
			continue
		}
		dx, dy := p.x-s.x, p.y-s.y
		d := dx*dx + dy*dy
		if d <= r.outer*r.outer && r.inner*r.inner <= d {
			out = append(out, id)
		}
	}
	return out
}
