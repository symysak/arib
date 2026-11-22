package scpca1u264

import (
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/std-t61/fec"
	"github.com/symysak/arib/internal/std-t61/l1"
)

const (
	Bits = l1.A1TCH1Bits
	BodyBits = 264
	CheckBits = Bits - BodyBits
	Patterns = l1.A1Patterns

	zeroFrom, zeroTo = 64, 176
	MaxZeroErr = 4
)

const Channel data.Channel = "T61-A1-U264"

const Name = "u264"

var delta = [5]int{0, 128, 256, 384, 159}

var order = func() (t [Bits]int) {
	const unit, cols = 2, 8
	units := Bits / unit
	rows := (units + cols - 1) / cols
	var start [cols + 1]int
	for c := 0; c < cols; c++ {
		l := rows
		if r := units % cols; r != 0 && c >= r {
			l--
		}
		start[c+1] = start[c] + l
	}
	j := 0
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			if r < start[c+1]-start[c] {
				for b := 0; b < unit; b++ {
					t[j] = (start[c]+r)*unit + b
					j++
				}
			}
		}
	}
	return
}()

func Mask(wc int) []uint8 {
	if wc <= 0 || wc >= Patterns {
		return nil
	}
	m := fec.PN95(wc, 511)
	out := make([]uint8, Bits)
	for i := range out {
		out[i] = m[(delta[i%5]+i/5)%511]
	}
	return out
}

type Result struct {
	Body []uint8
	Check []uint8
	ZeroErr int
	Long bool
	OK bool
	WC int
}

func (r Result) Words() []uint16 {
	if len(r.Body) != BodyBits {
		return nil
	}
	out := make([]uint16, 0, BodyBits/16+1)
	for i := 0; i < BodyBits; i += 16 {
		end := min(i+16, BodyBits)
		out = append(out, uint16(fec.BitsToUint(r.Body[i:end])))
	}
	return out
}

func Decode(tch1 []uint8, wc int) Result {
	res := Result{WC: wc}
	m := Mask(wc)
	if len(tch1) != Bits || m == nil {
		return res
	}
	x := make([]uint8, Bits)
	for i, p := range order {
		x[i] = (tch1[p] & 1) ^ m[i]
	}
	res.Body, res.Check = x[:BodyBits], x[BodyBits:]
	for _, b := range x[zeroFrom:zeroTo] {
		res.ZeroErr += int(b)
	}
	res.Long = IsLong(x)
	res.OK = res.ZeroErr <= MaxZeroErr || res.Long
	return res
}

var longTypes = [...]uint64{0x0F, 0x3B}

const longMark = 0b00000011001

func IsLong(x []uint8) bool {
	if len(x) < 75 {
		return false
	}
	t := fec.BitsToUint(x[8:16])
	if t != longTypes[0] && t != longTypes[1] {
		return false
	}
	return fec.BitsToUint(x[64:75]) == longMark
}

func Encode(body, check []uint8, wc int) []uint8 {
	m := Mask(wc)
	if len(body) != BodyBits || len(check) != CheckBits || m == nil {
		return nil
	}
	out := make([]uint8, Bits)
	for i, p := range order {
		var b uint8
		if i < BodyBits {
			b = body[i]
		} else {
			b = check[i-BodyBits]
		}
		out[p] = (b & 1) ^ m[i]
	}
	return out
}

func KeyFromZeros(tch1 []uint8, maxErr int) (int, bool) {
	if len(tch1) != Bits {
		return 0, false
	}
	var rx [zeroTo - zeroFrom]uint8
	for i := range rx {
		rx[i] = tch1[order[zeroFrom+i]] & 1
	}
	best, bestErr := 0, Bits
	for wc := 1; wc < Patterns; wc++ {
		m := fec.PN95(wc, 511)
		e := 0
		for i := range rx {
			j := zeroFrom + i
			if rx[i] != m[(delta[j%5]+j/5)%511] {
				if e++; e > maxErr || e >= bestErr {
					break
				}
			}
		}
		if e < bestErr {
			best, bestErr = wc, e
		}
	}
	return best, bestErr <= maxErr
}
