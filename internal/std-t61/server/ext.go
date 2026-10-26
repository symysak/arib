package server

import (
	"io/fs"
	"net/http"

	dec "github.com/symysak/arib/internal/std-t61/decoder"
	"github.com/symysak/arib/internal/std-t61/l1"
)

type Extension interface {
	Name() string
	Attach(p *Pipeline)
	Static() fs.FS
	Routes(mux *http.ServeMux, s *Server)
	Snapshot() map[string]any
	LabelID(id uint32) string
	OrgID(id uint32) string
	ExpandUnit(channel string, w l1.W, info []byte) []l1.Row
	ExpandRICH(r l1.RICH) []l1.Row
	ExpandSyncBurst(sb l1.SyncBurst) []l1.Row
	NoteRICH(r l1.RICH) string
	NoteSyncBurst(sb l1.SyncBurst) string
	NewChannel(idx int, mhz float64) ChannelExt
}

type RecStop struct {
	KeepIQ bool
	Audio bool
	Sec float64
}

type ChannelExt interface {
	Frame(f dec.Frame)
	Reset()
	RecordStart(base, reason string)
	RecordStop(base string) RecStop
	Station(ext map[string]any)
}
