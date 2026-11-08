package ambe


const Bits3600 = 72

var (
	rW = [36]int{0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 2, 0, 2, 0, 2, 0, 2, 0, 2, 0, 2, 0, 2}
	rX = [36]int{23, 10, 22, 9, 21, 8, 20, 7, 19, 6, 18, 5, 17, 4, 16, 3, 15, 2, 14, 1, 13, 0, 12, 10, 11, 9, 10, 8, 9, 7, 8, 6, 7, 5, 6, 4}
	rY = [36]int{0, 2, 0, 2, 0, 2, 0, 2, 0, 3, 0, 3, 1, 3, 1, 3, 1, 3, 1, 3, 1, 3, 1, 3, 1, 3, 1, 3, 1, 3, 1, 3, 1, 3, 1, 3}
	rZ = [36]int{5, 3, 4, 2, 3, 1, 2, 0, 1, 13, 0, 12, 22, 11, 21, 10, 20, 9, 19, 8, 18, 7, 17, 6, 16, 5, 15, 4, 14, 3, 13, 2, 12, 1, 11, 0}
)

type FR [4][24]uint8

func ToFR(a [Bits3600]uint8) FR {
	var fr FR
	for k := 0; k < 36; k++ {
		fr[rW[k]][rX[k]] = a[2*k]
		fr[rY[k]][rZ[k]] = a[2*k+1]
	}
	return fr
}

func FromFR(fr FR) [Bits3600]uint8 {
	var a [Bits3600]uint8
	for k := 0; k < 36; k++ {
		a[2*k] = fr[rW[k]][rX[k]]
		a[2*k+1] = fr[rY[k]][rZ[k]]
	}
	return a
}

const golayG = 0xC75

func golay23(b []uint8) bool {
	var x uint32
	for i := 22; i >= 0; i-- {
		x = x<<1 | uint32(b[i]&1)
	}
	for s := 22; s >= 11; s-- {
		if x>>s&1 == 1 {
			x ^= golayG << (s - 11)
		}
	}
	return x == 0
}

func golay23Encode(data uint16) [23]uint8 {
	x := uint32(data&0xFFF) << 11
	r := x
	for s := 22; s >= 11; s-- {
		if r>>s&1 == 1 {
			r ^= golayG << (s - 11)
		}
	}
	x |= r
	var b [23]uint8
	for i := range b {
		b[i] = uint8(x >> i & 1)
	}
	return b
}

func prng(fr *FR) (m [23]uint8) {
	var foo uint32
	for i := 23; i >= 12; i-- {
		foo = foo<<1 | uint32(fr[0][i])
	}
	pr := 16 * foo
	for j := 22; j >= 0; j-- {
		pr = (173*pr + 13849) % 65536
		m[j] = uint8(pr / 32768)
	}
	return m
}

func c0OK(fr FR) bool {
	if !golay23(fr[0][1:24]) {
		return false
	}
	var p uint8
	for _, b := range fr[0] {
		p ^= b
	}
	return p == 0
}

func c1OK(fr FR) bool {
	m := prng(&fr)
	var c [23]uint8
	for j := range c {
		c[j] = fr[1][j] ^ m[j]
	}
	return golay23(c[:])
}

func modulate(fr FR) FR {
	m := prng(&fr)
	for j := 0; j < 23; j++ {
		fr[1][j] ^= m[j]
	}
	return fr
}

func Valid3600(a [Bits3600]uint8) bool {
	fr := ToFR(a)
	return c0OK(fr) && c1OK(fr)
}

func MakeFrame3600(c0, c1 uint16, rest [25]uint8) [Bits3600]uint8 {
	var fr FR
	g0 := golay23Encode(c0)
	var p uint8
	for j := 0; j < 23; j++ {
		fr[0][j+1] = g0[j]
		p ^= g0[j]
	}
	fr[0][0] = p
	g1 := golay23Encode(c1)
	copy(fr[1][:23], g1[:])
	copy(fr[2][:11], rest[:11])
	copy(fr[3][:14], rest[11:])
	return FromFR(modulate(fr))
}

var golaySyn = func() (t [2048]uint32) {
	for i := range t {
		t[i] = 1 << 31
	}
	syn := func(e uint32) uint32 {
		x := e
		for s := 22; s >= 11; s-- {
			if x>>s&1 == 1 {
				x ^= golayG << (s - 11)
			}
		}
		return x
	}
	t[0] = 0
	for a := 0; a < 23; a++ {
		for b := a; b < 23; b++ {
			for c := b; c < 23; c++ {
				e := uint32(1)<<a | uint32(1)<<b | uint32(1)<<c
				s := syn(e)
				if t[s] == 1<<31 || bitsOn(e) < bitsOn(t[s]) {
					t[s] = e
				}
			}
		}
	}
	return
}()

func bitsOn(x uint32) int {
	n := 0
	for ; x != 0; x &= x - 1 {
		n++
	}
	return n
}

func golayFix(b []uint8) int {
	var x uint32
	for i := 22; i >= 0; i-- {
		x = x<<1 | uint32(b[i]&1)
	}
	s := x
	for k := 22; k >= 11; k-- {
		if s>>k&1 == 1 {
			s ^= golayG << (k - 11)
		}
	}
	e := golaySyn[s]
	for i := 0; i < 23; i++ {
		b[i] ^= uint8(e >> i & 1)
	}
	return bitsOn(e)
}

func Errors3600(a [Bits3600]uint8) (c0, c1 int) {
	fr := ToFR(a)
	c0 = golayFix(fr[0][1:24])
	var p uint8
	for _, b := range fr[0] {
		p ^= b
	}
	if p != 0 {
		c0++
	}
	m := prng(&fr)
	var c [23]uint8
	for j := range c {
		c[j] = fr[1][j] ^ m[j]
	}
	c1 = golayFix(c[:])
	return
}

func Params49(a [Bits3600]uint8) (d [49]uint8) {
	fr := ToFR(a)
	golayFix(fr[0][1:24])
	for i := 0; i < 12; i++ {
		d[i] = fr[0][23-i] & 1
	}
	m := prng(&fr)
	var c [23]uint8
	for j := range c {
		c[j] = fr[1][j] ^ m[j]
	}
	golayFix(c[:])
	for i := 0; i < 12; i++ {
		d[12+i] = c[22-i] & 1
	}
	for j := 0; j < 11; j++ {
		d[24+j] = fr[2][10-j] & 1
	}
	for j := 0; j < 14; j++ {
		d[35+j] = fr[3][13-j] & 1
	}
	return
}

func FromParams49(d [49]uint8) [Bits3600]uint8 {
	var c0, c1 uint16
	for i := 0; i < 12; i++ {
		c0 = c0<<1 | uint16(d[i]&1)
		c1 = c1<<1 | uint16(d[12+i]&1)
	}
	var rest [25]uint8
	for j := 0; j < 11; j++ {
		rest[j] = d[34-j] & 1
	}
	for j := 0; j < 14; j++ {
		rest[11+j] = d[48-j] & 1
	}
	return MakeFrame3600(c0, c1, rest)
}
