package decoder

type repeatCombiner struct {
	ring [][]float32
}

const combineDepth = 3

func (c *repeatCombiner) pick(code []float32, dec func([]float32) (ok, filler bool)) ([]float32, int) {
	good, filler := dec(code)
	if good {
		c.push(code)
		return code, 1
	}
	if filler {
		return code, 0
	}
	ok := func(x []float32) bool { g, _ := dec(x); return g }
	n := len(c.ring)
	buf := make([]float32, len(code))
	for m := 1; m < 1<<uint(n); m++ {
		copy(buf, code)
		cnt, bad := 1, false
		for j := 0; j < n; j++ {
			if m&(1<<uint(j)) == 0 {
				continue
			}
			p := c.ring[n-1-j]
			if len(p) != len(code) {
				bad = true
				break
			}
			for i := range buf {
				buf[i] += p[i]
			}
			cnt++
		}
		if bad {
			continue
		}
		if ok(buf) {
			c.push(code)
			return append([]float32(nil), buf...), cnt
		}
	}
	c.push(code)
	return code, 0
}

func (c *repeatCombiner) push(code []float32) {
	c.ring = append(c.ring, append([]float32(nil), code...))
	if len(c.ring) > combineDepth {
		c.ring = c.ring[len(c.ring)-combineDepth:]
	}
}

func (c *repeatCombiner) reset() { c.ring = nil }
