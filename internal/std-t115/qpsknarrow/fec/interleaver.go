package fec


var primitiveRoots = [][2]int{
	{7, 3}, {11, 2}, {13, 2}, {17, 3}, {19, 2}, {23, 5}, {29, 2}, {31, 3},
	{37, 2}, {41, 6}, {43, 3}, {47, 5}, {53, 2}, {59, 2}, {61, 2}, {67, 2},
	{71, 7}, {73, 5}, {79, 3}, {83, 2}, {89, 3}, {97, 5}, {101, 2}, {103, 5},
	{107, 2}, {109, 6}, {113, 3}, {127, 3}, {131, 2}, {137, 3}, {139, 2},
	{149, 2}, {151, 6}, {157, 5}, {163, 2}, {167, 5}, {173, 2}, {179, 2},
	{181, 2}, {191, 19}, {193, 5}, {197, 2}, {199, 3}, {211, 2}, {223, 3},
	{227, 2}, {229, 6}, {233, 3}, {239, 7}, {241, 7}, {251, 6}, {257, 3},
}

var (
	interRow5  = []int{4, 3, 2, 1, 0}
	interRow10 = []int{9, 8, 7, 6, 5, 4, 3, 2, 1, 0}
	interRow20 = []int{19, 9, 14, 4, 0, 2, 5, 7, 12, 18, 10, 8, 13, 17, 3, 1, 16, 6, 15, 11}
	interRow20alt = []int{19, 9, 14, 4, 0, 2, 5, 7, 12, 18, 16, 13, 17, 15, 3, 1, 6, 11, 8, 10}
)

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func isPrime(n int) bool {
	if n < 2 {
		return false
	}
	for d := 2; d*d <= n; d++ {
		if n%d == 0 {
			return false
		}
	}
	return true
}

func TurboInterleaver(k int) []int {
	var R int
	switch {
	case k >= 40 && k <= 159:
		R = 5
	case (k >= 160 && k <= 200) || (k >= 481 && k <= 530):
		R = 10
	default:
		R = 20
	}
	var p, v, C int
	if k >= 481 && k <= 530 {
		p, v, C = 53, 2, 53
	} else {
		for _, pv := range primitiveRoots {
			if k <= R*(pv[0]+1) {
				p, v = pv[0], pv[1]
				break
			}
		}
		switch {
		case k <= R*(p-1):
			C = p - 1
		case k <= R*p:
			C = p
		default:
			C = p + 1
		}
	}
	s := make([]int, p-1)
	s[0] = 1
	for j := 1; j < p-1; j++ {
		s[j] = (v * s[j-1]) % p
	}
	q := make([]int, R)
	q[0] = 1
	c := 7
	for i := 1; i < R; i++ {
		for {
			if isPrime(c) && c > 6 && c > q[i-1] && gcd(c, p-1) == 1 {
				break
			}
			c++
		}
		q[i] = c
		c++
	}
	var T []int
	switch {
	case R == 5:
		T = interRow5
	case R == 10:
		T = interRow10
	case (k >= 2281 && k <= 2480) || (k >= 3161 && k <= 3210):
		T = interRow20alt
	default:
		T = interRow20
	}
	r := make([]int, R)
	for i := 0; i < R; i++ {
		r[T[i]] = q[i]
	}
	U := make([][]int, R)
	for i := 0; i < R; i++ {
		U[i] = make([]int, C)
		switch C {
		case p:
			for j := 0; j < p-1; j++ {
				U[i][j] = s[(j*r[i])%(p-1)]
			}
			U[i][p-1] = 0
		case p + 1:
			for j := 0; j < p-1; j++ {
				U[i][j] = s[(j*r[i])%(p-1)]
			}
			U[i][p-1] = 0
			U[i][p] = p
			if k == R*C && i == R-1 {
				U[i][p], U[i][0] = U[i][0], U[i][p]
			}
		default:
			for j := 0; j < p-1; j++ {
				U[i][j] = s[(j*r[i])%(p-1)] - 1
			}
		}
	}
	perm := make([]int, 0, k)
	for j := 0; j < C; j++ {
		for i := 0; i < R; i++ {
			src := T[i]
			idx := src*C + U[src][j]
			if idx < k {
				perm = append(perm, idx)
			}
		}
	}
	return perm
}
