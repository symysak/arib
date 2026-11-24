package nonvoice


import (
	"github.com/symysak/arib/internal/rcr39qpsk/l1"
	"github.com/symysak/arib/internal/std-39/qpsk/l3"
)

type Slots struct {
	Mark func(n int, on bool)
	Log func(n int, text string)
	CtxOf func() l3.Ctx

	s        [4]slotNV
	restrict bool
	allowed  [4]bool
}

type slotNV struct {
	dec    *Decoder
	pkt    bool
	ok     bool
	t      float64
	scr    int
	hasScr bool
}

func (st *slotNV) changed(method string, scramble int) bool {
	if m := st.dec.Method(); m != "" && m != method {
		return true
	}
	if scramble < 0 {
		return false
	}
	if st.hasScr && scramble != st.scr {
		return true
	}
	init := l1.InitFromPattern(scramble)
	for dir := 1; dir <= 2; dir++ {
		if cur := st.dec.Init(dir); cur >= 0 && cur != init {
			return true
		}
	}
	return false
}

func (c *Slots) Restrict(slots []int) {
	c.restrict = true
	for _, n := range slots {
		if n >= 0 && n < 4 {
			c.allowed[n] = true
		}
	}
}

func (c *Slots) Allowed(n int) bool { return n >= 0 && n < 4 && (!c.restrict || c.allowed[n]) }

func (c *Slots) Start(n int, method string, scramble int, sure bool, why string) bool {
	if method == "" || !c.Allowed(n) {
		return false
	}
	st := &c.s[n]
	if st.dec != nil && st.changed(method, scramble) {
		c.log(n, "ST#"+itoa(n)+" の指定が変わった（"+why+"）: 組立途中を捨てる")
		st.dec = nil
	}
	if st.dec == nil {
		st.dec = New()
		if c.CtxOf != nil {
			st.dec.Ctx = c.CtxOf()
		}
		st.ok = false
		c.log(n, "ST#"+itoa(n)+" を非音声の"+method+"方式（"+AppendixOf(method)+"）として読む（"+why+"）")
	}
	st.dec.SetMethod(method)
	if scramble >= 0 {
		st.dec.SetScramble(scramble)
		st.scr, st.hasScr = scramble, true
	}
	st.t = 0
	if sure {
		c.mark(n, true)
	}
	return true
}

func (c *Slots) Stop(n int) {
	if n < 0 || n > 3 || (c.restrict && c.allowed[n]) {
		return
	}
	c.s[n] = slotNV{pkt: c.s[n].pkt}
	c.mark(n, false)
}

func (c *Slots) Active(n int) bool { return n >= 0 && n < 4 && c.s[n].dec != nil }
func (c *Slots) Method(n int) string {
	if !c.Active(n) {
		return ""
	}
	return c.s[n].dec.Method()
}

func (c *Slots) Data(n int) bool {
	return c.Active(n) && (c.s[n].pkt || c.s[n].ok)
}

func (c *Slots) Observe(n int, sig l3.Signal) {
	if c.Active(n) {
		c.s[n].dec.Observe(sig)
	}
}

func (c *Slots) Feed(n int, up, idle bool, hard []uint8, t float64) []Frame {
	if n < 0 || n > 3 {
		return nil
	}
	if idle {
		c.Stop(n)
		return nil
	}
	st := &c.s[n]
	if st.dec == nil {
		return nil
	}
	if st.t == 0 {
		st.t = t
	}
	if t-st.t > IdleSec && !(c.restrict && c.allowed[n]) {
		c.log(n, "ST#"+itoa(n)+" は正しいユニットが 10 秒無いので、非音声として読むのをやめる")
		c.Stop(n)
		return nil
	}
	fs := st.dec.FeedHard(n, up, hard, t)
	for _, f := range fs {
		if f.Kind != KindCRCError {
			st.t = t
		}
		if f.Confirms() {
			st.ok = true
			c.mark(n, true)
		}
	}
	return fs
}

func (c *Slots) mark(n int, on bool) {
	if c.s[n].pkt == on {
		return
	}
	c.s[n].pkt = on
	if c.Mark != nil {
		c.Mark(n, on)
	}
}

func (c *Slots) log(n int, s string) {
	if c.Log != nil {
		c.Log(n, s)
	}
}

func itoa(n int) string { return string(rune('0' + n)) }
