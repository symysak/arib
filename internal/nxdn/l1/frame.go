package l1

const (
	sacchAt = RICHBits
	half1At = sacchAt + SACCHBits
	half2At = half1At + TCHBits
	dataAt  = RICHBits
)

type Format int

const (
	FormatUnknown Format = iota
	FormatVoice
	FormatData
	FormatBurst
)

func (f Format) String() string {
	switch f {
	case FormatVoice:
		return "音声"
	case FormatData:
		return "データ"
	case FormatBurst:
		return "データ（バースト）"
	}
	return "不明"
}

type Half int

const (
	HalfTCH Half = iota
	HalfCoded
	HalfRaw
)

type Layout struct {
	Valid bool
	Format Format
	Half [2]Half
	Coded348 bool
}

type Dialect func(rich uint8) Layout

func RICHRaw(sym []float32) uint8 {
	var raw uint8
	for i := 0; i < 8; i++ {
		raw <<= 1
		if sym[i] < 0 {
			raw |= 1
		}
	}
	return raw
}

type Parsed struct {
	RICH   uint8
	Layout Layout
	Format Format
	SACCH *SACCHUnit
	Half [2]HalfUnit
	Data *DataUnit
	Soft []float32
}

type SACCHUnit struct {
	Head  uint8
	Data  [18]uint8
	CRCOK bool
	Fixed int
}

type HalfUnit struct {
	Kind Half
	TCH     []uint8
	TCHSoft []float32
	Coded *Result
}

type DataUnit struct {
	Head  uint8
	Body  [22]byte
	CRCOK bool
	Fixed int
}

func Parse(body []float32, d Dialect) Parsed {
	var p Parsed
	if len(body) != BodySymbols {
		return p
	}
	p.Soft = SoftBits(body)
	p.RICH = RICHRaw(body[:8])
	p.Layout = d(p.RICH)
	if !p.Layout.Valid {
		return p
	}
	p.Format = p.Layout.Format
	switch p.Format {
	case FormatVoice:
		r := SACCH.Decode(p.Soft[sacchAt : sacchAt+SACCHBits])
		u := &SACCHUnit{CRCOK: r.CRCOK, Fixed: r.Corrected, Head: bitsToByte(r.Info[:8])}
		copy(u.Data[:], r.Info[8:26])
		p.SACCH = u
		for i, k := range p.Layout.Half {
			at := half1At
			if i == 1 {
				at = half2At
			}
			soft := p.Soft[at : at+TCHBits]
			h := HalfUnit{Kind: k}
			if k == HalfCoded {
				r := FACCH1.Decode(soft)
				h.Coded = &r
			} else {
				h.TCH, h.TCHSoft = Hard(soft), append([]float32(nil), soft...)
			}
			p.Half[i] = h
		}
	case FormatData:
		if p.Layout.Coded348 {
			r := UDCH1.Decode(p.Soft[dataAt : dataAt+DataBits])
			d := &DataUnit{CRCOK: r.CRCOK, Fixed: r.Corrected, Head: bitsToByte(r.Info[:8])}
			for i := range d.Body {
				d.Body[i] = bitsToByte(r.Info[8+8*i : 16+8*i])
			}
			p.Data = d
		}
	}
	return p
}

func CodedBytes(r *Result) [10]byte {
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

func RICHBitsRaw(raw uint8) []uint8 {
	out := make([]uint8, RICHBits)
	for i := 0; i < 8; i++ {
		out[2*i] = raw >> (7 - i) & 1
		out[2*i+1] = 1
	}
	return out
}

func RICHBitsOf(f, m, o, d uint8) []uint8 {
	raw := f<<6 | m<<4 | o<<2 | d<<1
	x := raw >> 4
	x ^= x >> 2
	x ^= x >> 1
	return RICHBitsRaw(raw | x&1)
}

func HeadInfo(head uint8, data []uint8) []uint8 {
	out := make([]uint8, 0, 26)
	for i := 7; i >= 0; i-- {
		out = append(out, head>>i&1)
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
