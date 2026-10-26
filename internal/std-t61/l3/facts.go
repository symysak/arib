package l3


type CommForm int

const (
	FormSimplexIndividual CommForm = 0
	FormDefault           CommForm = 1
	FormDuplexIndividual  CommForm = 2
	FormGroup             CommForm = 4
	FormBroadcast         CommForm = 6
)

type CommMode struct {
	Form      CommForm
	Emergency bool
}

type CCFacts struct {
	Ref      int
	ToOrigin bool
	Mode *CommMode
	Cause, CallState int
	Called, Calling  string
	bearers          int
}

func (c *CCFacts) note(id byte, body []byte) {
	switch id {
	case 0x04:
		c.bearers++
		if c.bearers == 1 && len(body) >= 3 && body[1]&0x80 == 0 {
			o := body[2]
			c.Mode = &CommMode{Form: CommForm(o>>1) & 7, Emergency: o&0x10 != 0}
		}
	case 0x08:
		i := 1
		if len(body) > 0 && body[0]&0x80 == 0 {
			i++
		}
		if i < len(body) {
			c.Cause = int(body[i] & 0x7F)
		}
	case 0x14:
		if len(body) > 0 {
			c.CallState = int(body[0] & 0x3F)
		}
	case 0x70:
		if len(body) > 0 {
			c.Called = bcdDigits(body[1:], body[0]&0x80 != 0)
		}
	case 0x6C:
		if len(body) > 1 {
			c.Calling = bcdDigits(body[2:], body[1]&0x80 != 0)
		}
	}
}

type RTFacts struct {
	Band, Fc int
	GroupType int
	Scramble, Cause int
	OwnBand, OwnFc int
}

func rtFacts(b []byte) *RTFacts {
	f := &RTFacts{Fc: -1, GroupType: -1, Scramble: -1, Cause: -1, OwnFc: -1}
	if len(b) == 0 {
		return f
	}
	at := func(i int) int {
		if i < len(b) {
			return int(b[i])
		}
		return -1
	}
	freq := func(i int) {
		if i+1 < len(b) {
			f.Band, f.Fc = int(b[i]>>4), int(b[i]&0xF)<<8|int(b[i+1])
		}
	}
	switch b[0] {
	case 0x04:
		freq(1)
		if v := at(3); v >= 0 {
			f.GroupType = v & 7
		}
		f.Scramble = at(4)
	case 0x43, 0x61:
		freq(1)
		f.Scramble = at(3)
	case 0x21, 0x71, 0x81:
		f.Cause = at(1)
	case 0xA1:
		if n := at(1); n >= 0 {
			i := 2 + n + 4
			if v := at(i); v >= 0 && v>>4 == 1 && i+4 < len(b) {
				f.OwnBand, f.OwnFc = int(b[i+3]>>4), int(b[i+3]&0xF)<<8|int(b[i+4])
			}
		}
	}
	return f
}

func RTCauseNormal(v int) bool { return v >= 0 && v&0x7F >= 0x10 && v&0x7F <= 0x1F }

func RTCauseName(v int) string {
	if v < 0 {
		return ""
	}
	f, _ := rtCause(&reader{b: []byte{byte(v)}})
	return f.Value
}
