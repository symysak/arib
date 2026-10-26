package server

import (
	"context"
	"io/fs"
	"net/http"
	"net/url"
	"strconv"

	"github.com/symysak/arib/internal/std-t61/pane"
	"github.com/symysak/arib/internal/webui"
)

type Hub = webui.Hub

func NewHub() *Hub { return webui.NewHub() }

type Server struct {
	p   *Pipeline
	w   *webui.Server
	mux *http.ServeMux
}

func NewServer(p *Pipeline) *Server {
	var panes []fs.FS
	if p.cfg.Ext != nil {
		panes = append(panes, p.cfg.Ext.Static())
	}
	panes = append(panes, pane.FS())
	s := &Server{p: p}
	s.w = webui.New(webui.Config{
		Panes:    panes,
		Snapshot: func() any { sn := p.Snapshot(); return Event{Type: "snapshot", Snapshot: &sn} },
		Status:   func() any { return p.Snapshot() },
		FileDir:  func() string { return p.cfg.Watch.LogDir },
		Control:  pipelineControl{p: p, Control: webui.WideControl(p.Engine)},
		Inputs:   webui.WideInputs(p.Engine, 1+len(p.cfg.Watch.Inputs)),
	})
	s.mux = s.w.Mux()
	s.mux.HandleFunc("/api/select", s.handleSelect)
	s.mux.HandleFunc("/api/l1all", s.handleL1All)
	if p.cfg.Ext != nil {
		p.cfg.Ext.Routes(s.mux, s)
	}
	if _, pat := s.mux.Handler(&http.Request{Method: "GET", URL: &url.URL{Path: "/ws/audio"}}); pat != "/ws/audio" {
		p.audio = webui.NewHub()
		s.mux.HandleFunc("/ws/audio", func(w http.ResponseWriter, r *http.Request) { s.w.Serve(p.audio, w, r, true) })
	}
	return s
}

func (s *Server) Serve(h *Hub, w http.ResponseWriter, r *http.Request, binary bool) {
	s.w.Serve(h, w, r, binary)
}

func (s *Server) Handler() http.Handler { return s.w }

func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	return s.w.ListenAndServe(ctx, addr, func(ctx context.Context) { _ = s.p.Run(ctx, s.onEvent) })
}

func (s *Server) Run(ctx context.Context) error { return s.p.Run(ctx, s.onEvent) }

func (s *Server) Pipeline() *Pipeline { return s.p }

func (s *Server) Addr() string { return s.w.Addr() }

func (s *Server) onEvent(e Event) { s.w.Publish(e) }

func (s *Server) handleSelect(w http.ResponseWriter, r *http.Request) {
	v, err := strconv.Atoi(r.URL.Query().Get("idx"))
	if err != nil {
		http.Error(w, "idx が不正です", http.StatusBadRequest)
		return
	}
	s.p.Select(v)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleL1All(w http.ResponseWriter, r *http.Request) {
	on := r.URL.Query().Get("on") == "1"
	s.p.SetL1All(on)
	w.WriteHeader(http.StatusNoContent)
}

type pipelineControl struct {
	webui.Control
	p *Pipeline
}

func (c pipelineControl) Remove(mhz float64) error {
	c.p.forget(mhz)
	return c.Control.Remove(mhz)
}

func (c pipelineControl) Tune(hz float64) error {
	if err := c.Control.Tune(hz); err != nil {
		return err
	}
	c.p.setMeta(func(m *Meta) { m.CenterMHz = hz / 1e6 })
	return nil
}

func (c pipelineControl) SetGain(db float64, agc bool) error {
	if err := c.Control.SetGain(db, agc); err != nil {
		return err
	}
	c.p.setMeta(func(m *Meta) {
		m.GainDB = db
		if agc {
			m.GainDB = 0
		}
	})
	return nil
}
