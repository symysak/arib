package data

import "strings"

const (
	PickMin = 2
	PickKeep = 32
)

type Station struct {
	ch    Channel
	spec  string
	fmt   Format
	r     Reader
	err   error
	held  []Frame
	tried bool
}

func NewStation(ch Channel, spec string) *Station {
	s := &Station{ch: ch, spec: strings.TrimSpace(spec)}
	if s.spec == "" {
		s.spec = Auto
	}
	switch {
	case strings.EqualFold(s.spec, Off), strings.EqualFold(s.spec, Auto):
	default:
		f, ok := Lookup(ch, s.spec)
		if !ok {
			s.err = Valid(ch, s.spec)
			return s
		}
		s.use(f)
	}
	return s
}

func (s *Station) use(f Format) {
	r, err := f.New(nil)
	if err != nil {
		s.err = err
		return
	}
	s.fmt, s.r = f, r
}

func (s *Station) Err() error { return s.err }

func (s *Station) Format() string {
	if s.r == nil {
		return ""
	}
	return s.fmt.Name
}

func (s *Station) Read(f Frame) []Event {
	if s.err != nil || strings.EqualFold(s.spec, Off) {
		return nil
	}
	if s.r != nil {
		return s.r.Read(f)
	}
	s.held = append(s.held, f)
	if len(s.held) > PickKeep {
		s.held = s.held[len(s.held)-PickKeep:]
	}
	best, bestOK := Format{}, 0
	for _, c := range Formats(s.ch) {
		if c.ManualOnly || c.Probe == nil {
			continue
		}
		ok, n := c.Probe(s.held)
		if ok >= PickMin && 2*ok >= n && ok > bestOK {
			best, bestOK = c, ok
		}
	}
	if bestOK == 0 {
		return nil
	}
	s.use(best)
	if s.r == nil {
		return nil
	}
	var out []Event
	for _, h := range s.held {
		out = append(out, s.r.Read(h)...)
	}
	s.held = nil
	return out
}
