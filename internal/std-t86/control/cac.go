package control

import (
	"fmt"

	"github.com/symysak/arib/internal/std-t86/citycodes"
	"github.com/symysak/arib/internal/std-t86/fec"
)

const (
	CACOffset   = 320
	CACLen      = 256
	CACPilotAt  = 232
	CACSpan     = CACLen + 4
	PayloadLen  = 104
	BitsPerSlot = 600

	FACCHPayloadLen = 232
	FACCHBits       = 512
)

func ExtractCAC(slot []uint8, offset int) []uint8 {
	out := make([]uint8, 0, CACLen)
	out = append(out, slot[offset:offset+CACPilotAt]...)
	out = append(out, slot[offset+CACPilotAt+4:offset+CACSpan]...)
	return out
}

func DecodeCAC(cac []uint8, seed int) ([]uint8, bool, error) {
	if len(cac) != CACLen {
		return nil, false, fmt.Errorf("CAC は %d bit 必要（%d 受領）", CACLen, len(cac))
	}
	info := fec.ControlViterbiDecode(fec.Descramble(cac, seed))
	payload := info[:PayloadLen]
	rxCRC := uint16(bitsToInt(info[PayloadLen : PayloadLen+16]))
	return payload, fec.CRC16CCITT(payload) == rxCRC, nil
}

func DecodeSlot(slot []uint8, seed, offset int) (Message, error) {
	payload, crcOK, err := DecodeCAC(ExtractCAC(slot, offset), seed)
	if err != nil {
		return Message{}, err
	}
	msg := ParseMessage(payload, seed)
	msg.CRCOK = crcOK
	return msg, nil
}

func DecodeSlots(slots [][]uint8, seed int) ([]Message, error) {
	out := make([]Message, 0, len(slots))
	for _, s := range slots {
		m, err := DecodeSlot(s, seed, CACOffset)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func ParseFACCHMessage(payload []uint8) Message {
	if len(payload) > FACCHPayloadLen {
		payload = payload[:FACCHPayloadLen]
	}
	b := payload
	msg := Message{
		RawHex:  bitsToHex(b),
		Channel: "FACCH",
	}
	msg.Type = bitsToInt(b[9:16])
	msg.TypeName = TypeName(msg.Type)
	msg.Section = SpecSection(msg.Type)
	if msg.Type == MsgNumberNotify {
		code := bitsToInt(b[72:88])
		mfr := bitsToInt(b[88:104])
		f := &NotifyFields{
			Spare0:        int(b[8] & 1),
			SpareOct2To5:  bitsToInt(b[16:48]),
			SpareOct2To5X: bitsToHex(b[16:48]),
			SpareOct6:     bitsToInt(b[48:52]),

			CallNo:           bitsToInt(b[52:56]),
			SubStationID:     bitsToInt(b[56:72]),
			MunicipalCode:    code,
			ManufacturerCode: mfr,
			ManufacturerName: ManufacturerName(mfr),

			LicenseInfoLen: bitsToInt(b[104:120]),
			LicenseInfo: bitsToHex(b[120:FACCHPayloadLen]),
		}
		if name, ok := citycodes.Name(code); ok {
			f.CityName = name
		}
		msg.Notify = f
	}
	return msg
}

func DecodeFACCH(tch []uint8, seed int) (Message, error) {
	if len(tch) != FACCHBits {
		return Message{}, fmt.Errorf("FACCH は %d bit 必要（%d 受領）", FACCHBits, len(tch))
	}
	info := fec.ControlViterbiDecode(fec.Descramble(tch, seed))
	payload := info[:FACCHPayloadLen]
	rxCRC := uint16(bitsToInt(info[FACCHPayloadLen : FACCHPayloadLen+16]))
	msg := ParseFACCHMessage(payload)
	msg.CRCOK = fec.CRC16CCITT(payload) == rxCRC
	return msg, nil
}

func CandidatesForSeed(seed int) []citycodes.Candidate {
	return citycodes.CandidatesForSeed(seed, true)
}

func CityForMunicipalCode(code int) (string, bool) { return citycodes.Name(code) }

func SeedForMunicipalCode(code int) (int, error) { return fec.MunicipalCodeToSeed(code) }
