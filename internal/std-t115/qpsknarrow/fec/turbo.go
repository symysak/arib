package fec

import "math"


func TurboOutLen(k int) int { return 3*k + 12 }

func rscStep(s, u int) (int, int) {
	r1, r2, r3 := s&1, (s>>1)&1, (s>>2)&1
	a := u ^ r2 ^ r3
	z := a ^ r1 ^ r3
	return a | (r1 << 1) | (r2 << 2), z
}

func rscTailStep(s int) (next, u, z int) {
	r2, r3 := (s>>1)&1, (s>>2)&1
	u = r2 ^ r3
	next, z = rscStep(s, u)
	return next, u, z
}

func TurboEncode(x []uint8) []uint8 {
	k := len(x)
	perm := TurboInterleaver(k)
	out := make([]uint8, TurboOutLen(k))

	s := 0
	for i := 0; i < k; i++ {
		n, z := rscStep(s, int(x[i]&1))
		out[3*i] = x[i] & 1
		out[3*i+1] = uint8(z)
		s = n
	}
	s1 := s
	s = 0
	for i := 0; i < k; i++ {
		n, z := rscStep(s, int(x[perm[i]]&1))
		out[3*i+2] = uint8(z)
		s = n
	}
	s2 := s
	p := 3 * k
	for i := 0; i < 3; i++ {
		n, u, z := rscTailStep(s1)
		out[p] = uint8(u)
		out[p+1] = uint8(z)
		p += 2
		s1 = n
	}
	for i := 0; i < 3; i++ {
		n, u, z := rscTailStep(s2)
		out[p] = uint8(u)
		out[p+1] = uint8(z)
		p += 2
		s2 = n
	}
	return out
}

const negInf = -1e30

func max2(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func bcjr(lx, lz, la, lxTail, lzTail []float64) []float64 {
	k := len(lx)
	n := k + 3
	const S = 8

	alpha := make([][S]float64, n+1)
	for i := range alpha {
		for s := 0; s < S; s++ {
			alpha[i][s] = negInf
		}
	}
	alpha[0][0] = 0

	gam := func(i, s, u int) float64 {
		_, z := rscStep(s, u)
		g := 0.0
		su := 1.0
		if u == 1 {
			su = -1.0
		}
		sz := 1.0
		if z == 1 {
			sz = -1.0
		}
		g += su * (lx[i] + la[i]) / 2
		g += sz * lz[i] / 2
		return g
	}
	gamTail := func(t, s int) float64 {
		_, u, z := rscTailStep(s)
		su := 1.0
		if u == 1 {
			su = -1.0
		}
		sz := 1.0
		if z == 1 {
			sz = -1.0
		}
		return su*lxTail[t]/2 + sz*lzTail[t]/2
	}

	for i := 0; i < k; i++ {
		for s := 0; s < S; s++ {
			if alpha[i][s] == negInf {
				continue
			}
			for u := 0; u < 2; u++ {
				ns, _ := rscStep(s, u)
				v := alpha[i][s] + gam(i, s, u)
				if v > alpha[i+1][ns] {
					alpha[i+1][ns] = v
				}
			}
		}
	}
	for t := 0; t < 3; t++ {
		i := k + t
		for s := 0; s < S; s++ {
			if alpha[i][s] == negInf {
				continue
			}
			ns, _, _ := rscTailStep(s)
			v := alpha[i][s] + gamTail(t, s)
			if v > alpha[i+1][ns] {
				alpha[i+1][ns] = v
			}
		}
	}

	beta := make([][S]float64, n+1)
	for i := range beta {
		for s := 0; s < S; s++ {
			beta[i][s] = negInf
		}
	}
	beta[n][0] = 0
	for t := 2; t >= 0; t-- {
		i := k + t
		for s := 0; s < S; s++ {
			ns, _, _ := rscTailStep(s)
			if beta[i+1][ns] == negInf {
				continue
			}
			v := beta[i+1][ns] + gamTail(t, s)
			if v > beta[i][s] {
				beta[i][s] = v
			}
		}
	}
	for i := k - 1; i >= 0; i-- {
		for s := 0; s < S; s++ {
			best := negInf
			for u := 0; u < 2; u++ {
				ns, _ := rscStep(s, u)
				if beta[i+1][ns] == negInf {
					continue
				}
				best = max2(best, beta[i+1][ns]+gam(i, s, u))
			}
			beta[i][s] = best
		}
	}

	le := make([]float64, k)
	for i := 0; i < k; i++ {
		m0, m1 := negInf, negInf
		for s := 0; s < S; s++ {
			if alpha[i][s] == negInf {
				continue
			}
			for u := 0; u < 2; u++ {
				ns, _ := rscStep(s, u)
				if beta[i+1][ns] == negInf {
					continue
				}
				v := alpha[i][s] + gam(i, s, u) + beta[i+1][ns]
				if u == 0 {
					m0 = max2(m0, v)
				} else {
					m1 = max2(m1, v)
				}
			}
		}
		if m0 == negInf || m1 == negInf {
			le[i] = 0
			continue
		}
		le[i] = (m0 - m1) - lx[i] - la[i]
		if math.IsNaN(le[i]) || math.IsInf(le[i], 0) {
			le[i] = 0
		}
	}
	return le
}

func TurboDecode(llr []float64, k, iters int) []uint8 {
	if len(llr) != TurboOutLen(k) {
		panic("t115/fec: LLR 長がターボ符号語長と一致しない")
	}
	perm := TurboInterleaver(k)
	lx := make([]float64, k)
	lz1 := make([]float64, k)
	lz2 := make([]float64, k)
	for i := 0; i < k; i++ {
		lx[i] = llr[3*i]
		lz1[i] = llr[3*i+1]
		lz2[i] = llr[3*i+2]
	}
	p := 3 * k
	lxT1 := []float64{llr[p], llr[p+2], llr[p+4]}
	lzT1 := []float64{llr[p+1], llr[p+3], llr[p+5]}
	lxT2 := []float64{llr[p+6], llr[p+8], llr[p+10]}
	lzT2 := []float64{llr[p+7], llr[p+9], llr[p+11]}

	lxI := make([]float64, k)
	la1 := make([]float64, k)
	la2 := make([]float64, k)
	var le1, le2 []float64
	for it := 0; it < iters; it++ {
		le1 = bcjr(lx, lz1, la1, lxT1, lzT1)
		for i := 0; i < k; i++ {
			la2[i] = le1[perm[i]]
			lxI[i] = lx[perm[i]]
		}
		le2 = bcjr(lxI, lz2, la2, lxT2, lzT2)
		for i := 0; i < k; i++ {
			la1[perm[i]] = le2[i]
		}
	}
	out := make([]uint8, k)
	for i := 0; i < k; i++ {
		if lx[i]+le1[i]+la1[i] < 0 {
			out[i] = 1
		}
	}
	return out
}
