package server

import (
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/symysak/arib/internal/std-t86/decoder"
	"github.com/symysak/arib/internal/std-t86/fec"
	"github.com/symysak/arib/internal/std-t86/wavio"
)


var staticFS embed.FS

type hub struct {
	maxsize int
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func newHub(maxsize int) *hub {
	return &hub{maxsize: maxsize, clients: map[chan []byte]struct{}{}}
}

func (h *hub) register() chan []byte {
	ch := make(chan []byte, h.maxsize)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *hub) unregister(ch chan []byte) {
	h.mu.Lock()
	delete(h.clients, ch)
	h.mu.Unlock()
}

func (h *hub) publish(b []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- b:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- b:
			default:
			}
		}
	}
}

type Server struct {
	pipeline *Pipeline
	events   *hub
	audio    *hub
	mux      *http.ServeMux

	audioSeq uint32
	seqMu    sync.Mutex
	stop     chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
}

func NewServer(p *Pipeline) *Server {
	s := &Server{
		pipeline: p,
		events:   newHub(512),
		audio:    newHub(64),
		mux:      http.NewServeMux(),
		stop:     make(chan struct{}),
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) Start() {
	s.pipeline.Start()
	s.wg.Add(2)
	go s.drainEvents()
	go s.drainPCM()
}

func (s *Server) Stop() {
	s.stopOnce.Do(func() {
		close(s.stop)
		s.pipeline.Stop()
		s.wg.Wait()
	})
}

func (s *Server) drainEvents() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case ev := <-s.pipeline.Events():
			b, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			s.events.publish(b)
		}
	}
}

func (s *Server) drainPCM() {
	defer s.wg.Done()
	for {
		select {
		case <-s.stop:
			return
		case f := <-s.pipeline.PCM():
			s.seqMu.Lock()
			s.audioSeq++
			seq := s.audioSeq
			s.seqMu.Unlock()
			buf := make([]byte, 8+len(f.PCM)*2)
			binary.LittleEndian.PutUint32(buf[0:], uint32(f.WindowID))
			binary.LittleEndian.PutUint32(buf[4:], seq)
			for i, v := range f.PCM {
				binary.LittleEndian.PutUint16(buf[8+2*i:], uint16(v))
			}
			s.audio.publish(buf)
		}
	}
}

func (s *Server) routes() {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic("static の埋め込みが壊れています: " + err.Error())
	}
	files := http.FileServer(http.FS(sub))
	s.mux.Handle("GET /", noCache(files))

	s.mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.pipeline.state.snapshot())
	})

	s.mux.HandleFunc("POST /api/squelch", func(w http.ResponseWriter, r *http.Request) {
		v, err := boolParam(r, "enabled")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK,
			map[string]bool{"squelch_enabled": s.pipeline.SetSquelch(v)})
	})

	s.mux.HandleFunc("POST /api/broadcast_strict", func(w http.ResponseWriter, r *http.Request) {
		v, err := boolParam(r, "enabled")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK,
			map[string]bool{"broadcast_strict": s.pipeline.SetBroadcastStrict(v)})
	})

	s.mux.HandleFunc("POST /api/cfo", func(w http.ResponseWriter, r *http.Request) {
		v, err := boolParam(r, "enabled")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK,
			map[string]bool{"cfo_enabled": s.pipeline.SetCFOEnabled(v)})
	})

	s.mux.HandleFunc("POST /api/cfo/reset", func(w http.ResponseWriter, r *http.Request) {
		s.pipeline.RequestCFOReset()
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	s.mux.HandleFunc("POST /api/seed", func(w http.ResponseWriter, r *http.Request) {
		raw := r.URL.Query().Get("value")
		v := decoder.SeedAuto
		if raw != "auto" {
			n, err := intParam(r, "value")
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			v = n
		}
		if v < decoder.SeedAuto || v >= fec.NSeeds {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "value は -1/auto（自動判定）または 0..511 です"})
			return
		}
		s.pipeline.RequestSeedPin(v)
		writeJSON(w, http.StatusOK, map[string]any{
			"seed": v, "seed_pinned": v != decoder.SeedAuto})
	})

	s.mux.HandleFunc("GET /api/audio/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := windowIDParam(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		pcm := s.pipeline.audio.windowPCM(id)
		if pcm == nil {
			writeJSON(w, http.StatusNotFound,
				map[string]string{"error": "この通報ウィンドウの音声はありません"})
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(wavio.Bytes(pcm, 1, audioSampleRate))
	})

	s.mux.HandleFunc("GET /api/iq/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, ok := windowIDParam(r)
		if !ok {
			http.NotFound(w, r)
			return
		}
		win := s.pipeline.state.windowInfoCopy(id)
		if win == nil || win.IQ == nil || win.IQ.Path == "" {
			writeJSON(w, http.StatusNotFound,
				map[string]string{"error": "この通報ウィンドウの IQ 録音はありません"})
			return
		}
		if _, err := os.Stat(win.IQ.Path); err != nil {
			writeJSON(w, http.StatusNotFound,
				map[string]string{"error": "IQ 録音ファイルが見つかりません"})
			return
		}
		http.ServeFile(w, r, win.IQ.Path)
	})

	s.mux.HandleFunc("GET /ws", func(w http.ResponseWriter, r *http.Request) {
		s.serveWS(w, r, s.events, true)
	})
	s.mux.HandleFunc("GET /ws/audio", func(w http.ResponseWriter, r *http.Request) {
		s.serveWS(w, r, s.audio, false)
	})
}

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request, h *hub, sendSnapshot bool) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := r.Context()

	typ := websocket.MessageText
	if !sendSnapshot {
		typ = websocket.MessageBinary
	}
	if sendSnapshot {
		b, err := json.Marshal(s.pipeline.state.snapshot())
		if err == nil {
			if err := conn.Write(ctx, typ, b); err != nil {
				return
			}
		}
	}

	ch := h.register()
	defer h.unregister(ch)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			conn.Close(websocket.StatusGoingAway, "shutdown")
			return
		case b := <-ch:
			wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(wctx, typ, b)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func boolParam(r *http.Request, name string) (bool, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return false, errors.New(name + " が必要です（?enabled=true|false）")
	}
	return strconv.ParseBool(v)
}

func intParam(r *http.Request, name string) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return 0, errors.New(name + " が必要です")
	}
	return strconv.Atoi(v)
}

func windowIDParam(r *http.Request) (int, bool) {
	id := strings.TrimSuffix(r.PathValue("id"), ".wav")
	n, err := strconv.Atoi(id)
	if err != nil {
		return 0, false
	}
	return n, true
}
