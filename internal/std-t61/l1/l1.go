package l1

import (
	"fmt"
	"strings"
	"sync"

	"github.com/symysak/arib/internal/std-t61/fec"
)

const FrameBits = 384

const FrameSymbols = FrameBits / 2

const SuperframeFrames = 18

const (
	RIStart = 128
	RIEnd   = 184
	SWStart = 184
	FieldAStart = 32
	FieldAEnd   = 128
	MidStart = 204
	MidEnd   = 224
	FieldBStart = 224
	PCCIdleStart = 204
	PCCDataStart = 208
	SBSWEnd  = 216
	SBPIStr  = 272
	SBPIEnd  = 376
	FrameEnd = 384
)

type Edition int

const (
	EditionAuto Edition = iota
	EditionSCPC
	EditionFDMA
	EditionA1
)

func (e Edition) String() string {
	switch e {
	case EditionSCPC:
		return "SCPC"
	case EditionFDMA:
		return "FDMA"
	case EditionA1:
		return "SCPC付録1"
	}
	return "auto"
}

type ChannelKind int

const (
	KindUnknown   ChannelKind = iota
	KindControl
	KindService
	KindSyncBurst
)

func (k ChannelKind) String() string {
	switch k {
	case KindControl:
		return "制御CH"
	case KindService:
		return "通信CH"
	case KindSyncBurst:
		return "同期バースト"
	}
	return "不明"
}

type SyncWord struct {
	Name string
	Bits []uint8
	Kind ChannelKind
	Uplink bool
	SuperframeHead bool
	LastUnit bool
	FDMAOnly bool
}

var SyncWords = buildSyncWords()

func buildSyncWords() []SyncWord {
	down := []SyncWord{
		{Name: "S1", Bits: fec.UintToBits(0x87A4B, 20), Kind: KindControl, SuperframeHead: true, FDMAOnly: true},
		{Name: "S2", Bits: fec.UintToBits(0x31BAF, 20), Kind: KindControl, FDMAOnly: true},
		{Name: "S3", Bits: fec.UintToBits(0x9D236, 20), Kind: KindService, SuperframeHead: true, FDMAOnly: true},
		{Name: "S4", Bits: fec.UintToBits(0x1E56F, 20), Kind: KindService},
		{Name: "SS1", Bits: fec.UintToBits(0x2F94D06B, 32), Kind: KindSyncBurst},
	}
	up := []SyncWord{
		{Name: "S1'", Bits: invert(fec.UintToBits(0x87A4B, 20)), Kind: KindControl, Uplink: true, SuperframeHead: true, FDMAOnly: true},
		{Name: "S2'", Bits: invert(fec.UintToBits(0x31BAF, 20)), Kind: KindControl, Uplink: true, FDMAOnly: true},
		{Name: "S5'", Bits: invert(fec.UintToBits(0x5164C, 20)), Kind: KindControl, Uplink: true, SuperframeHead: true, LastUnit: true, FDMAOnly: true},
		{Name: "S6'", Bits: invert(fec.UintToBits(0x4D9DE, 20)), Kind: KindControl, Uplink: true, LastUnit: true, FDMAOnly: true},
		{Name: "S3'", Bits: invert(fec.UintToBits(0x9D236, 20)), Kind: KindService, Uplink: true, SuperframeHead: true, FDMAOnly: true},
		{Name: "S4'", Bits: invert(fec.UintToBits(0x1E56F, 20)), Kind: KindService, Uplink: true},
		{Name: "SS1'", Bits: invert(fec.UintToBits(0x2F94D06B, 32)), Kind: KindSyncBurst, Uplink: true},
	}
	return append(down, up...)
}

func invert(b []uint8) []uint8 {
	out := make([]uint8, len(b))
	for i, v := range b {
		out[i] = v ^ 1
	}
	return out
}

func MatchSyncWord(bits []uint8, maxErr int) (*SyncWord, int) {
	var best *SyncWord
	bestErr := maxErr + 1
	for i := range SyncWords {
		sw := &SyncWords[i]
		if len(bits) < len(sw.Bits) {
			continue
		}
		e := 0
		for k, v := range sw.Bits {
			if bits[k] != v {
				e++
				if e >= bestErr {
					break
				}
			}
		}
		lim := bestErr
		if e < lim {
			best, bestErr = sw, e
		}
	}
	return best, bestErr
}


type RICH struct {
	OK     bool
	Raw    uint32
	Metric float64
	S, F, M, D, CC uint8
	User           uint16
	SF, SM, SD uint8
}

func DecodeRICH(soft []float32) RICH {
	r := fec.BlockRICH.Decode(soft)
	out := RICHFromRaw(uint32(fec.BitsToUint(r.Info)))
	out.OK, out.Metric = r.CRCOK, r.Metric
	return out
}

func RICHFromRaw(v uint32) RICH {
	out := RICH{OK: true, Raw: v}
	out.S = uint8(v >> 15)
	out.F = uint8(v>>12) & 7
	out.M = uint8(v>>9) & 7
	out.D = uint8(v>>6) & 7
	out.CC = uint8(v) & 0x3F
	out.User = uint16(v) & 0x7FFF
	out.SF = uint8(v>>13) & 7
	out.SM = uint8(v>>10) & 7
	out.SD = uint8(v>>7) & 7
	return out
}

var (
	fdmaS = []string{"予約(SCPC規格で使用)", "FDMAシステムRI情報", "全域FDMAユーザー情報", "予約(SCPC規格で使用)"}
	fdmaF = []string{"物理制御CH(上り第1フレーム)", "物理制御CH(上り第2フレーム以降/下り)", "物理通信CH",
		"無線アプローチ用制御CH(上り第1フレーム)", "無線アプローチ用制御CH(上り第2フレーム以降/下り)",
		"無線アプローチ用通信CH", "物理制御CH用同期バースト", "物理通信CH用同期バースト"}
	fdmaM = []string{"アイドル", "音声信号", "非音声1(訂正無し)", "非音声2(訂正有り)", "FACCH", "空線信号", "予約", "予約"}
	fdmaD = []string{"アイドル", "中継ゾーン", "折返し通信", "直接通信", "予約", "予約", "予約", "予約"}
	scpcF = []string{"未定義", "未定義", "未定義", "通信用チャネル", "未定義", "未定義", "未定義", "同期バースト"}
	scpcM = []string{"未定義", "TCH:音声信号", "未定義", "未定義", "未定義", "空線信号", "BUSY", "未定義"}
	scpcD = []string{"基地局経由通信", "未定義", "未定義", "直接通信", "未定義", "未定義", "未定義", "未定義"}
)

func (r RICH) Describe(ed Edition) string {
	if !r.OK {
		return "RICH CRC NG"
	}
	if ed == EditionSCPC {
		return fmt.Sprintf("F=%s M=%s D=%s", scpcF[r.SF], scpcM[r.SM], scpcD[r.SD])
	}
	if r.S == 2 {
		return fmt.Sprintf("S=全域FDMAユーザー情報 user=0x%04X", r.User)
	}
	return fmt.Sprintf("S=%s F=%s M=%s D=%s CC=%d", fdmaS[r.S], fdmaF[r.F], fdmaM[r.M], fdmaD[r.D], r.CC)
}

func (r RICH) Bits17() string {
	s := fmt.Sprintf("%017b", r.Raw)
	return strings.Join([]string{s[0:2], s[2:5], s[5:8], s[8:11], s[11:17]}, " ")
}

func (r RICH) HasMode() bool { return r.OK && r.S != 2 }

func (r RICH) IsVoice() bool { return r.HasMode() && r.M == 1 }

func (r RICH) IsFACCH() bool { return r.HasMode() && r.M == 4 }

func (r RICH) IsNonVoice2() bool { return r.HasMode() && r.M == 3 }

func (r RICH) IsIdle() bool { return r.HasMode() && r.M == 0 }

func (r RICH) IsControlChannel() bool { return r.HasMode() && (r.F == 0 || r.F == 1) }

func (r RICH) IsIdleLine() bool { return r.HasMode() && r.M == 5 }



func DescrambleService(frame []float32, init int) []float32 {
	out := append([]float32(nil), frame...)
	pn := fec.PN95(init, 276)
	k := 0
	for i := FieldAStart; i < FieldAEnd; i++ {
		if pn[k] == 1 {
			out[i] = -out[i]
		}
		k++
	}
	for i := MidStart; i < FrameEnd; i++ {
		if pn[k] == 1 {
			out[i] = -out[i]
		}
		k++
	}
	return out
}

func DescrambleControl(frame []float32, init int) []float32 {
	out := append([]float32(nil), frame...)
	pn := fec.PN95(init, 272)
	k := 0
	for i := FieldAStart; i < FieldAEnd; i++ {
		if pn[k] == 1 {
			out[i] = -out[i]
		}
		k++
	}
	for i := PCCDataStart; i < FrameEnd; i++ {
		if pn[k] == 1 {
			out[i] = -out[i]
		}
		k++
	}
	return out
}

func DescrambleSyncBurst(frame []float32, init int) []float32 {
	out := append([]float32(nil), frame...)
	pn := fec.PN95(init, 104)
	for i := SBPIStr; i < SBPIEnd; i++ {
		if pn[i-SBPIStr] == 1 {
			out[i] = -out[i]
		}
	}
	return out
}


func ColorCodeScramble(cc uint8) int {
	c := int(cc & 0x3F)
	return (c&1 | c<<1) & 0x1FF
}

const (
	upFirstAStart  = 96
	upSecondAStart = 32
	upBStart       = 208
	upBEnd         = 376
)

func upControlCode(frame []float32, second bool) []float32 {
	a := upFirstAStart
	if second {
		a = upSecondAStart
	}
	code := make([]float32, 0, (FieldAEnd-a)+(upBEnd-upBStart))
	code = append(code, frame[a:FieldAEnd]...)
	return append(code, frame[upBStart:upBEnd]...)
}

var upFillerMasks = sync.OnceValue(func() [2][512][]uint8 {
	var m [2][512][]uint8
	for k, n := range []int{(FieldAEnd - upFirstAStart) + (upBEnd - upBStart), (FieldAEnd - upSecondAStart) + (upBEnd - upBStart)} {
		for s := 1; s < 512; s++ {
			m[k][s] = fec.PN95(s, n)
		}
	}
	return m
})

var serviceTCHMasks = sync.OnceValue(func() [512][]uint8 {
	var m [512][]uint8
	for s := range m {
		pn := fec.PN95(s, 276)
		m[s] = append(append(make([]uint8, 0, 256), pn[:96]...), pn[116:276]...)
	}
	return m
})

func ServiceTCHMask(init int) []uint8 { return serviceTCHMasks()[init&0x1FF] }

func MatchUpControlFiller(frame []float32, second bool) (init, best, next int) {
	code := upControlCode(frame, second)
	k := 0
	if second {
		k = 1
	}
	masks := upFillerMasks()
	best, next, init = len(code)+1, len(code)+1, -1
	for s := 1; s < 512; s++ {
		d := 0
		for i, x := range code {
			if (x < 0) != (masks[k][s][i] == 1) {
				d++
			}
		}
		switch {
		case d < best:
			init, best, next = s, d, best
		case d < next:
			next = d
		}
	}
	return
}

func UpControlSecond(r RICH) (second, ok bool) {
	if !r.OK {
		return false, false
	}
	switch r.F {
	case 0, 3:
		return false, true
	case 1, 4:
		return true, true
	}
	return false, false
}

func DecodeControlUp(frame []float32, init int, second bool) ControlUnit {
	code := fec.DescrambleSoft(upControlCode(frame, second), init)
	blk, n := fec.BlockPCCUp1, 64
	if second {
		blk, n = fec.BlockPCCUp2, 96
	}
	r := blk.Decode(code)
	return ControlUnit{OK: r.CRCOK, Metric: r.Metric, Corrected: r.Corrected, Uplink: true, Second: second,
		W: decodeW(r.Info[0:8]), Data: r.Info[8 : 8+n], N: uint8(fec.BitsToUint(r.Info[8+n : 8+n+7]))}
}


type W struct {
	First, Last bool
	WO          uint8
}

func decodeW(b []uint8) W {
	return W{First: b[0] == 1, Last: b[1] == 1, WO: uint8(fec.BitsToUint(b[2:8]))}
}

func (w W) String() string {
	switch {
	case w.First && w.Last:
		return fmt.Sprintf("単独 L=%d", w.WO)
	case w.First:
		return fmt.Sprintf("先頭 残り%d", w.WO)
	case w.Last:
		return fmt.Sprintf("最終 L=%d", w.WO)
	case w.WO == 0:
		return "メッセージ無し"
	}
	return fmt.Sprintf("途中 残り%d", w.WO)
}

type ControlUnit struct {
	OK        bool
	Metric    float64
	Corrected int
	W         W
	Data      []uint8
	IB, RN bool
	PE     uint16
	N uint8
	Uplink, Second bool
}

func (u ControlUnit) EText() string {
	if u.Uplink {
		return ""
	}
	s := fmt.Sprintf("I/B=%d R/N=%d PE=%04X", b2i(u.IB), b2i(u.RN), u.PE)
	if u.RN {
		s += "（上りの受信応答）"
	}
	return s
}

func DecodeControlDown(frame []float32) ControlUnit {
	code := make([]float32, 0, 272)
	code = append(code, frame[FieldAStart:FieldAEnd]...)
	code = append(code, frame[PCCDataStart:FrameEnd]...)
	r := fec.BlockPCCDown.Decode(code)
	u := ControlUnit{OK: r.CRCOK, Metric: r.Metric, Corrected: r.Corrected}
	u.W = decodeW(r.Info[0:8])
	u.Data = r.Info[8:96]
	u.IB = r.Info[96] == 1
	u.RN = r.Info[97] == 1
	u.PE = uint16(fec.BitsToUint(r.Info[98:114]))
	u.N = r.Info[114]
	return u
}


const fillerCorr = 6

func IsFiller(ok bool, corrected int, allZero bool) bool {
	if ok {
		return false
	}
	return corrected > fillerCorr || allZero
}

func AllZero(data []uint8) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}

type SACCHUnit struct {
	OK        bool
	Metric    float64
	Corrected int
	W         W
	Data      []uint8
	N uint8
}

func DecodeSACCH(code []float32) SACCHUnit {
	r := fec.BlockSACCH.Decode(code)
	return SACCHUnit{OK: r.CRCOK, Metric: r.Metric, Corrected: r.Corrected, W: decodeW(r.Info[0:8]), Data: r.Info[8:56],
		N: uint8(fec.BitsToUint(r.Info[56:59]))}
}

type RCHUnit struct {
	OK        bool
	Metric    float64
	Corrected int
	Raw       uint8
	N         uint8
}

func DecodeRCH(code []float32) RCHUnit {
	r := fec.BlockRCH.Decode(code)
	return RCHUnit{OK: r.CRCOK, Metric: r.Metric, Corrected: r.Corrected, Raw: uint8(fec.BitsToUint(r.Info[0:8])),
		N: r.Info[8]}
}

func (u RCHUnit) Describe(uplink bool) string {
	if !u.OK {
		return "RCH CRC NG"
	}
	if uplink {
		return describeRCHUp(u.Raw)
	}
	if u.Raw&0x80 != 0 {
		return fmt.Sprintf("予備 (%02X)", u.Raw)
	}
	id := (u.Raw >> 4) & 7
	p := u.Raw & 0xF
	switch id {
	case 0:
		return "アイドル"
	case 1:
		return fmt.Sprintf("電力制御 POW=%d", p)
	case 3:
		return fmt.Sprintf("下り設定電力通知 POW-D=%d", p)
	}
	return fmt.Sprintf("免許人固有 id=%d param=%d", id, p)
}

var rchBER = []string{"0.3%以下", "0.3〜1%", "1〜3%", "3〜7%", "7〜10%", "10%以上"}

func describeRCHUp(raw uint8) string {
	id := raw >> 5
	p := raw & 0x1F
	switch id {
	case 0:
		return "アイドル"
	case 1:
		var lv string
		switch {
		case p == 0:
			lv = "0dBμ未満"
		case p == 31:
			lv = "60dBμ以上"
		default:
			lv = fmt.Sprintf("%d〜%ddBμ", 2*(int(p)-1), 2*int(p))
		}
		return fmt.Sprintf("受信レベル報告 RSSI=%d（%s）", p, lv)
	case 2:
		e := p & 7
		s := fmt.Sprintf("誤り検出情報 ERR=%d", e)
		if int(e) < len(rchBER) {
			s += "（BER " + rchBER[e] + "）"
		} else {
			s += "（未定義）"
		}
		if p>>3 != 0 {
			s += fmt.Sprintf(" 予約ビット=%d", p>>3)
		}
		return s
	case 3:
		s := fmt.Sprintf("上り設定電力通知 POW-U=%d", p&7)
		if p>>3 != 0 {
			s += fmt.Sprintf(" 予約ビット=%d", p>>3)
		}
		return s
	case 4:
		return fmt.Sprintf("免許人固有 param=%d", p)
	}
	return fmt.Sprintf("予備 id=%d param=%d", id, p)
}

type FACCHUnit struct {
	OK        bool
	Metric    float64
	Corrected int
	W         W
	Data      []uint8
	N         uint8
}

func FACCHBits(frame []float32, parity int) []float32 {
	out := make([]float32, 0, 128)
	for i := FieldAStart + parity; i < FieldAEnd; i += 2 {
		out = append(out, frame[i])
	}
	for i := FieldBStart + parity; i < FrameEnd; i += 2 {
		out = append(out, frame[i])
	}
	return out
}

func DecodeFACCH(code []float32) FACCHUnit {
	r := fec.BlockFACCH.Decode(code)
	return FACCHUnit{OK: r.CRCOK, Metric: r.Metric, Corrected: r.Corrected, W: decodeW(r.Info[0:8]), Data: r.Info[8:104],
		N: uint8(fec.BitsToUint(r.Info[104:107]))}
}

func TCHBits(frame []float32, facchParity int) []uint8 {
	var out []uint8
	take := func(i int) {
		if frame[i] >= 0 {
			out = append(out, 0)
		} else {
			out = append(out, 1)
		}
	}
	for i := FieldAStart; i < FieldAEnd; i++ {
		if facchParity < 0 || (i-FieldAStart)%2 != facchParity {
			take(i)
		}
	}
	for i := FieldBStart; i < FrameEnd; i++ {
		if facchParity < 0 || (i-FieldBStart)%2 != facchParity {
			take(i)
		}
	}
	return out
}

func TCHSoft(frame []float32, facchParity int) []float32 {
	var out []float32
	for i := FieldAStart; i < FieldAEnd; i++ {
		if facchParity < 0 || (i-FieldAStart)%2 != facchParity {
			out = append(out, frame[i])
		}
	}
	for i := FieldBStart; i < FrameEnd; i++ {
		if facchParity < 0 || (i-FieldBStart)%2 != facchParity {
			out = append(out, frame[i])
		}
	}
	return out
}


type SyncBurst struct {
	OK     bool
	Metric float64
	Raw    []uint8
	B   bool
	I   uint8
	TA  uint8
	SSC uint8
	ID uint32
}

func DecodeSyncBurst(frame []float32) SyncBurst {
	r := fec.BlockSyncBurst.Decode(frame[SBPIStr:SBPIEnd])
	sb := SyncBurst{OK: r.CRCOK, Metric: r.Metric, Raw: r.Info[0:40]}
	sb.B = r.Info[0] == 1
	sb.I = uint8(fec.BitsToUint(r.Info[1:4]))
	sb.TA = uint8(fec.BitsToUint(r.Info[4:8]))
	sb.SSC = uint8(fec.BitsToUint(r.Info[8:16]))
	sb.ID = uint32(fec.BitsToUint(r.Info[16:40]))
	return sb
}

func (s SyncBurst) Describe(ed Edition) string {
	if !s.OK {
		return "SB CRC NG"
	}
	if ed == EditionSCPC {
		return fmt.Sprintf("B=%d param=%010X", b2i(s.B), fec.BitsToUint(s.Raw))
	}
	kind := "初期(SB1/SB2)"
	if s.B {
		kind = "完了(SB3/SB4)"
	}
	return fmt.Sprintf("%s TA=%d SSC=%d ID=#%06X", kind, s.TA, s.SSC, s.ID&0xFFFFFF)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func HexOctets(b []uint8) string {
	var sb strings.Builder
	for i := 0; i < len(b); i += 8 {
		j := i + 8
		if j > len(b) {
			j = len(b)
		}
		v := fec.BitsToUint(b[i:j]) << uint(8-(j-i))
		fmt.Fprintf(&sb, "%02X", v)
	}
	return sb.String()
}
