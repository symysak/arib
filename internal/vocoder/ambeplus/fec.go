package ambeplus

const FrameBits = 128

const ParamBits = 80

const MaxDist = 3

const protBits = 36

const tailBits = 6

const codedBits = (protBits + tailBits) * 2

const g0, g1 = 0x4f, 0x6d

var chanPos = func() (p [FrameBits]int) {
	for i := range p {
		col, row := i/12, i%12
		if row < 8 {
			p[i] = row*11 + col
		} else {
			p[i] = 88 + (row-8)*10 + col
		}
	}
	return p
}()

var trans = func() (t [64][2]struct{ out, next uint8 }) {
	for s := 0; s < 64; s++ {
		for b := 0; b < 2; b++ {
			reg := (s<<1 | b) & 0x7f
			t[s][b].out = uint8(parity(reg&g0)<<1 | parity(reg&g1))
			t[s][b].next = uint8(reg & 0x3f)
		}
	}
	return t
}()

func parity(x int) int {
	x ^= x >> 4
	x ^= x >> 2
	x ^= x >> 1
	return x & 1
}

func Encode(params []uint8) []uint8 {
	coded := make([]uint8, 0, FrameBits)
	s := uint8(0)
	for i := 0; i < protBits+tailBits; i++ {
		b := uint8(0)
		if i < protBits {
			b = params[i] & 1
		}
		tr := trans[s][b]
		coded = append(coded, tr.out>>1, tr.out&1)
		s = tr.next
	}
	for _, b := range params[protBits:ParamBits] {
		coded = append(coded, b&1)
	}
	ch := make([]uint8, FrameBits)
	for i, b := range coded {
		ch[chanPos[i]] = b
	}
	return ch
}

func Decode(ch []uint8) (params []uint8, dist int) {
	var coded [FrameBits]uint8
	for i := range coded {
		coded[i] = ch[chanPos[i]] & 1
	}
	const inf = 1 << 30
	var metric [64]int
	for i := 1; i < 64; i++ {
		metric[i] = inf
	}
	steps := protBits + tailBits
	var from [protBits + tailBits][64]uint8
	for k := 0; k < steps; k++ {
		rx := coded[2*k]<<1 | coded[2*k+1]
		var next [64]int
		for i := range next {
			next[i] = inf
		}
		maxIn := 1
		if k >= protBits {
			maxIn = 0
		}
		for s := 0; s < 64; s++ {
			if metric[s] >= inf {
				continue
			}
			for b := 0; b <= maxIn; b++ {
				tr := trans[s][b]
				d := metric[s] + int((tr.out^rx)>>1) + int((tr.out^rx)&1)
				if d < next[tr.next] {
					next[tr.next] = d
					from[k][tr.next] = uint8(s)
				}
			}
		}
		metric = next
	}
	params = make([]uint8, ParamBits)
	s := uint8(0)
	for k := steps - 1; k >= 0; k-- {
		ps := from[k][s]
		if k < protBits {
			params[k] = s & 1
		}
		s = ps
	}
	copy(params[protBits:], coded[codedBits:])
	return params, metric[0]
}
