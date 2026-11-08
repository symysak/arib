package l1

const (
	richAt  = 0
	sacchAt = RICHBits
	half1At = sacchAt + SACCHBits
	half2At = half1At + TCHBits
	dataAt  = RICHBits
)

type Parsed struct {
	RICH RICH
	Format Format
	SACCH *SACCHUnit
	Half [2]HalfUnit
	Data *DataUnit
	Soft []float32
}

type SACCHUnit struct {
	S, U  uint8
	Data  [18]uint8
	CRCOK bool
	Fixed int
}

type HalfUnit struct {
	Kind Half
	TCH     []uint8
	TCHSoft []float32
	FACCH1 *Result
}

type DataUnit struct {
	Kind  DataKind
	S, U  uint8
	Body  [22]byte
	CRCOK bool
	Fixed int
}

func Parse(body []float32) Parsed {
	var p Parsed
	if len(body) != BodySymbols {
		return p
	}
	p.Soft = SoftBits(body)
	p.RICH = DecodeRICH(body[:8])
	p.Format = p.RICH.Format()
	switch p.Format {
	case FormatVoice:
		r := SACCH.Decode(p.Soft[sacchAt : sacchAt+SACCHBits])
		u := &SACCHUnit{CRCOK: r.CRCOK, Fixed: r.Corrected}
		u.S = r.Info[0]<<1 | r.Info[1]
		u.U = bitsToByte(r.Info[2:8])
		copy(u.Data[:], r.Info[8:26])
		p.SACCH = u
		a, b := p.RICH.Halves()
		for i, k := range [2]Half{a, b} {
			at := half1At
			if i == 1 {
				at = half2At
			}
			soft := p.Soft[at : at+TCHBits]
			h := HalfUnit{Kind: k}
			if k == HalfTCH {
				h.TCH, h.TCHSoft = Hard(soft), append([]float32(nil), soft...)
			} else {
				r := FACCH1.Decode(soft)
				h.FACCH1 = &r
			}
			p.Half[i] = h
		}
	case FormatData:
		if k := p.RICH.DataKind(); k == DataFACCH2 || k == DataUDCH1 {
			r := UDCH1.Decode(p.Soft[dataAt : dataAt+DataBits])
			d := &DataUnit{Kind: k, CRCOK: r.CRCOK, Fixed: r.Corrected}
			d.S = r.Info[0]<<1 | r.Info[1]
			d.U = bitsToByte(r.Info[2:8])
			for i := range d.Body {
				d.Body[i] = bitsToByte(r.Info[8+8*i : 16+8*i])
			}
			p.Data = d
		}
	}
	return p
}

func FACCH1Bytes(r *Result) [10]byte {
	var out [10]byte
	for i := range out {
		out[i] = bitsToByte(r.Info[8*i : 8*i+8])
	}
	return out
}

func bitsToByte(b []uint8) uint8 {
	var v uint8
	for _, x := range b {
		v = v<<1 | x&1
	}
	return v
}


func Symbols(bits []uint8) []float32 {
	out := make([]float32, len(bits)/2)
	for i := range out {
		switch bits[2*i]<<1 | bits[2*i+1] {
		case 1:
			out[i] = 3
		case 0:
			out[i] = 1
		case 2:
			out[i] = -1
		default:
			out[i] = -3
		}
	}
	return out
}

func RICHBitsOf(f, m, o, d uint8) []uint8 {
	raw := f<<6 | m<<4 | o<<2 | d<<1
	x := raw >> 4
	x ^= x >> 2
	x ^= x >> 1
	raw |= x & 1
	out := make([]uint8, RICHBits)
	for i := 0; i < 8; i++ {
		out[2*i] = raw >> (7 - i) & 1
		out[2*i+1] = 1
	}
	return out
}

func SACCHInfo(s, u uint8, data []uint8) []uint8 {
	out := []uint8{s >> 1 & 1, s & 1}
	for i := 5; i >= 0; i-- {
		out = append(out, u>>i&1)
	}
	return append(out, data[:18]...)
}

func BytesToBits(b []byte) []uint8 {
	out := make([]uint8, 0, 8*len(b))
	for _, x := range b {
		for i := 7; i >= 0; i-- {
			out = append(out, x>>i&1)
		}
	}
	return out
}
