package scpca1f40


import (
	"github.com/symysak/arib/internal/bitpack"
	"github.com/symysak/arib/internal/data"
	"github.com/symysak/arib/internal/std-t61/fec"
	"github.com/symysak/arib/internal/std-t61/l1"
)

const ControlChannel data.Channel = "T61-A1-F40-CTL"

const ControlName = "f40-ctl"

const (
	ControlBytes = 4
	ControlBits = FrameBits
	ctlCols     = 8
	ctlUnit     = 2
)

var ctlOrder = func() (t [sec0]int) {
	units := sec0 / ctlUnit
	rows := (units + ctlCols - 1) / ctlCols
	var start [ctlCols + 1]int
	for c := 0; c < ctlCols; c++ {
		l := rows
		if r := units % ctlCols; r != 0 && c >= r {
			l--
		}
		start[c+1] = start[c] + l
	}
	j := 0
	for r := 0; r < rows; r++ {
		for c := 0; c < ctlCols; c++ {
			if r < start[c+1]-start[c] {
				for b := 0; b < ctlUnit; b++ {
					t[j] = (start[c]+r)*ctlUnit + b
					j++
				}
			}
		}
	}
	return
}()

type ControlWord struct {
	Info [ControlBits]uint8
	Errors int
}

func (c ControlWord) Usable() bool { return c.Errors <= MaxErrors }

func (c ControlWord) Payload(wc int) ([ControlBytes]byte, bool) {
	var out [ControlBytes]byte
	if !c.Usable() {
		return out, false
	}
	m := Mask(wc)
	var x [ControlBits]uint8
	for i := range x {
		x[i] = c.Info[i] ^ m[i]
	}
	if fec.CRC8(x[:32], 0) != uint8(bitpack.ToBytes(x[32:])[0]) {
		return out, false
	}
	copy(out[:], bitpack.ToBytes(x[:32]))
	return out, true
}

func Control(tch1 []float32) (ControlWord, bool) {
	var w ControlWord
	if len(tch1) != l1.A1TCH1Bits {
		return w, false
	}
	soft := make([]float32, sec0)
	for j, p := range ctlOrder {
		soft[j] = tch1[p]
	}
	info, errs := decode(soft, true)
	copy(w.Info[:], info)
	w.Errors = errs
	return w, true
}

func EncodeControl(d [ControlBytes]byte, wc int) []uint8 {
	info := make([]uint8, ControlBits+tail)
	for i := 0; i < 32; i++ {
		info[i] = d[i/8] >> (7 - i%8) & 1
	}
	crc := fec.CRC8(info[:32], 0)
	for i := 0; i < 8; i++ {
		info[32+i] = crc >> (7 - i) & 1
	}
	m := Mask(wc)
	for i := 0; i < ControlBits; i++ {
		info[i] ^= m[i]
	}
	code := fec.ConvEncodeG(info, codeK, codeG1, codeG2)
	out := make([]uint8, sec0)
	for j, p := range ctlOrder {
		out[p] = code[j]
	}
	return out
}

const (
	ControlHintVotes = 2
	ControlLoseAfter = 16
)

type ControlKeyer struct {
	wc      int
	decided bool
	fixed   bool
	hint    int
	hintN   int
	seen    map[[ControlBits]uint8]bool
	held    []ControlItem
	misses  int
	Judge    func(cands map[int][]ControlDecoded) (wc int, ok bool)
	distinct []ControlItem
}

const ControlJudgeMin = 3

type ControlItem struct {
	Word ControlWord
	Tag  any
}

type ControlDecoded struct {
	ControlItem
	WC   int
	Data [ControlBytes]byte
}

func NewControlKeyer(wc int) *ControlKeyer {
	k := &ControlKeyer{hint: -1}
	k.reset()
	if wc >= 0 && wc < l1.A1Patterns {
		k.wc, k.decided, k.fixed = wc, true, true
	}
	return k
}

func (k *ControlKeyer) reset() {
	k.seen, k.held, k.hintN, k.misses, k.distinct = map[[ControlBits]uint8]bool{}, nil, 0, 0, nil
}

func (k *ControlKeyer) WC() (int, bool) { return k.wc, k.decided }

func (k *ControlKeyer) Hint(wc int) {
	if k.fixed || wc < 0 || wc >= l1.A1Patterns || wc == k.hint {
		return
	}
	k.hint, k.hintN = wc, 0
	if k.decided && wc != k.wc {
		k.decided = false
		k.reset()
	}
}

func (k *ControlKeyer) Feed(it ControlItem) []ControlDecoded {
	if !it.Word.Usable() {
		return nil
	}
	if k.decided {
		d, ok := it.Word.Payload(k.wc)
		if ok {
			k.misses = 0
			return []ControlDecoded{{it, k.wc, d}}
		}
		if k.misses++; !k.fixed && k.misses >= ControlLoseAfter {
			k.decided = false
			k.reset()
		}
		return nil
	}
	k.held = append(k.held, it)
	if len(k.held) > 4*ControlLoseAfter {
		k.held = k.held[1:]
	}
	if k.seen[it.Word.Info] {
		return nil
	}
	k.seen[it.Word.Info] = true
	k.distinct = append(k.distinct, it)
	if len(k.distinct) > 4*ControlLoseAfter {
		k.distinct = k.distinct[1:]
	}
	if k.hint >= 0 {
		if _, ok := it.Word.Payload(k.hint); ok {
			if k.hintN++; k.hintN >= ControlHintVotes {
				return k.decide(k.hint)
			}
		}
	}
	if k.Judge == nil || len(k.distinct) < ControlJudgeMin {
		return nil
	}
	cands := map[int][]ControlDecoded{}
	for wc := 0; wc < l1.A1Patterns; wc++ {
		var ds []ControlDecoded
		for _, h := range k.distinct {
			d, ok := h.Word.Payload(wc)
			if !ok {
				ds = nil
				break
			}
			ds = append(ds, ControlDecoded{h, wc, d})
		}
		if ds != nil {
			cands[wc] = ds
		}
	}
	if len(cands) == 0 {
		k.distinct = k.distinct[1:]
		return nil
	}
	if wc, ok := k.Judge(cands); ok {
		return k.decide(wc)
	}
	return nil
}

func (k *ControlKeyer) decide(wc int) []ControlDecoded {
	held := k.held
	k.reset()
	k.wc, k.decided = wc, true
	var out []ControlDecoded
	for _, h := range held {
		if d, ok := h.Word.Payload(wc); ok {
			out = append(out, ControlDecoded{h, wc, d})
		}
	}
	return out
}
