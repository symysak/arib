package control

import "strings"


type FieldValue struct {
	Oct int `json:"oct"`
	OctTo int `json:"oct_to,omitempty"`
	Bits string `json:"bits"`
	Name string `json:"name"`
	Value int `json:"value"`
	Bin string `json:"bin"`
	Meaning string `json:"meaning,omitempty"`
	Note string `json:"note,omitempty"`
	Spare bool `json:"spare,omitempty"`
	Collapsed bool `json:"collapsed,omitempty"`
	CollapsedBits int `json:"collapsed_bits,omitempty"`
}

type IDValue struct {
	Name string `json:"name"`
	Value int `json:"value"`
	Simultaneous bool `json:"simultaneous"`
}

type Expanded struct {
	Channel string `json:"channel"`
	Octets int `json:"octets"`
	Received int `json:"received"`
	Raw []int `json:"raw"`
	PayloadOct []int `json:"payload_oct,omitempty"`
	Header []FieldValue `json:"header"`
	Body []FieldValue `json:"body"`
	HasBody bool `json:"has_body"`
	ID *IDValue `json:"id,omitempty"`
}

func Expand(raw []byte, msgType int) Expanded {
	ch := ChannelOf(msgType)
	e := Expanded{
		Channel:  ch,
		Octets:   OctetsFor(ch),
		Received: len(raw),
		Raw:      make([]int, len(raw)),
	}
	for i, b := range raw {
		e.Raw[i] = int(b)
	}
	e.Header = expandFields(raw, CommonHeader)
	body := FieldsFor(msgType)
	e.HasBody = body != nil
	if body != nil {
		e.Body = expandFields(raw, collapsePayload(body, e.Octets))
		for _, f := range body {
			if f.Payload {
				e.PayloadOct = append(e.PayloadOct, f.Oct)
			}
		}
		e.ID = pairID(raw, body)
	}
	return e
}

func collapsePayload(body []Field, total int) []Field {
	var out []Field
	from, name := -1, ""
	flush := func(to int) {
		if from < 0 {
			return
		}
		out = append(out, Field{Oct: from, Hi: to, Lo: 0, Name: name, Payload: true})
		from = -1
	}
	for _, f := range body {
		if f.Payload {
			if from < 0 {
				from, name = f.Oct, f.Name
			}
			continue
		}
		flush(f.Oct - 1)
		out = append(out, f)
	}
	flush(total)
	return out
}

func expandFields(raw []byte, fields []Field) []FieldValue {
	out := make([]FieldValue, 0, len(fields))
	for _, f := range fields {
		if f.Payload && f.Lo == 0 {
			if f.Oct > len(raw) {
				continue
			}
			out = append(out, FieldValue{
				Oct: f.Oct, OctTo: f.Hi, Bits: "b8-1", Name: f.Name,
				Collapsed: true, CollapsedBits: (f.Hi - f.Oct + 1) * 8,
			})
			continue
		}
		if f.Oct > len(raw) || f.Oct < 1 {
			continue
		}
		width := f.Hi - f.Lo + 1
		v := int(raw[f.Oct-1]>>(f.Lo-1)) & ((1 << width) - 1)
		fv := FieldValue{
			Oct: f.Oct, Bits: bitRange(f.Hi, f.Lo), Name: f.Name,
			Value: v, Bin: binary(v, width), Note: f.Note,
			Spare: isSpare(f.Name),
		}
		switch {
		case f.Fmt != nil:
			fv.Meaning = f.Fmt(v)
		case f.Enum != nil:
			if m, ok := f.Enum[v]; ok {
				fv.Meaning = m
			} else {
				fv.Meaning = "予備/予約"
			}
		}
		out = append(out, fv)
	}
	return out
}

func pairID(raw []byte, body []Field) *IDValue {
	for _, f := range body {
		if !strings.Contains(f.Name, "識別番号(上位)") {
			continue
		}
		if f.Oct+1 > len(raw) {
			return nil
		}
		v := int(raw[f.Oct-1])<<8 | int(raw[f.Oct])
		name := strings.SplitN(f.Name, "(上位)", 2)[0]
		return &IDValue{Name: name, Value: v, Simultaneous: v == 0}
	}
	return nil
}

func bitRange(hi, lo int) string {
	if hi == lo {
		return "b" + itoa(hi)
	}
	return "b" + itoa(hi) + "-" + itoa(lo)
}

func binary(v, width int) string {
	b := make([]byte, width)
	for i := width - 1; i >= 0; i-- {
		b[i] = byte('0' + (v & 1))
		v >>= 1
	}
	return string(b)
}

func isSpare(name string) bool {
	return strings.Contains(name, "予備") || strings.Contains(name, "予約")
}
