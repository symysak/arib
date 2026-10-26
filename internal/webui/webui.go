package webui

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

//go:embed all:static
var staticFS embed.FS

func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

type Config struct {
	Panes []fs.FS
	Snapshot func() any
	Status func() any
	FileDir func() string
	Control Control
	Inputs []Input
}

type Server struct {
	cfg  Config
	mux  *http.ServeMux
	ev   *Hub
	addr string
}

func New(cfg Config) *Server {
	s := &Server{cfg: cfg, mux: http.NewServeMux(), ev: NewHub()}
	layers := make(overlayFS, 0, len(cfg.Panes)+1)
	for _, p := range cfg.Panes {
		if p != nil {
			layers = append(layers, p)
		}
	}
	layers = append(layers, Static())
	s.mux.Handle("/", http.FileServer(http.FS(layers)))
	s.mux.HandleFunc("/ws", s.handleWS)
	if cfg.Status != nil {
		s.mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cfg.Status())
		})
	}
	if cfg.FileDir != nil {
		s.mux.HandleFunc("/api/iq/", s.handleFile)
		s.mux.HandleFunc("/api/audio/", s.handleFile)
	}
	s.routeControl()
	return s
}

func (s *Server) Mux() *http.ServeMux { return s.mux }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func (s *Server) Publish(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	s.ev.Broadcast(b)
}

func (s *Server) PublishJSON(b []byte) { s.ev.Broadcast(b) }

func Mount(mux *http.ServeMux, prefix string, h http.Handler) {
	p := "/" + strings.Trim(prefix, "/")
	mux.Handle(p+"/", http.StripPrefix(p, h))
	mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
		u := *r.URL
		u.Path = p + "/"
		http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
	})
}

func (s *Server) ListenAndServe(ctx context.Context, addr string, run func(ctx context.Context)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.addr = ln.Addr().String()
	srv := &http.Server{Handler: s.mux}
	piped := make(chan struct{})
	go func() {
		defer close(piped)
		if run != nil {
			run(ctx)
		}
	}()
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	<-piped
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		srv.Close()
	}
	return nil
}

func (s *Server) Addr() string { return s.addr }

func (s *Server) Serve(h *Hub, w http.ResponseWriter, r *http.Request, binary bool) {
	ws, err := accept(w, r, !binary)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ctx := r.Context()
	c := &conn{ws: ws, send: make(chan []byte, 256)}
	h.add(c)
	go drain(ctx, ws, h, c)
	typ := websocket.MessageText
	if binary {
		typ = websocket.MessageBinary
	}
	pump(ctx, h, c, typ)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	ws, err := accept(w, r, true)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ctx := r.Context()
	c := &conn{ws: ws, send: make(chan []byte, 512)}
	if s.cfg.Snapshot != nil {
		if b, err := json.Marshal(s.cfg.Snapshot()); err == nil {
			c.send <- b
		}
	}
	s.ev.add(c)
	go drain(ctx, ws, s.ev, c)
	pump(ctx, s.ev, c, websocket.MessageText)
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	rel := r.URL.Path
	for _, p := range []string{"/api/iq/", "/api/audio/"} {
		rel = strings.TrimPrefix(rel, p)
	}
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
		http.NotFound(w, r)
		return
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			http.NotFound(w, r)
			return
		}
	}
	dir := s.cfg.FileDir()
	if dir == "" {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	back, err := filepath.Rel(dir, full)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, "まだ書き出し中か、存在しません", http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, filepath.Base(full), st.ModTime(), f)
}


type Hub struct {
	mu    sync.Mutex
	conns map[*conn]struct{}
}

type conn struct {
	ws   *websocket.Conn
	send chan []byte
}

func NewHub() *Hub { return &Hub{conns: map[*conn]struct{}{}} }

func (h *Hub) add(c *conn) {
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *conn) {
	h.mu.Lock()
	if _, ok := h.conns[c]; ok {
		delete(h.conns, c)
		close(c.send)
	}
	h.mu.Unlock()
}

func (h *Hub) Broadcast(b []byte) {
	h.mu.Lock()
	for c := range h.conns {
		select {
		case c.send <- b:
		default:
		}
	}
	h.mu.Unlock()
}

func accept(w http.ResponseWriter, r *http.Request, compress bool) (*websocket.Conn, error) {
	opt := &websocket.AcceptOptions{InsecureSkipVerify: true}
	if compress {
		opt.CompressionMode = websocket.CompressionContextTakeover
	}
	return websocket.Accept(w, r, opt)
}

func drain(ctx context.Context, ws *websocket.Conn, h *Hub, c *conn) {
	for {
		if _, _, err := ws.Read(ctx); err != nil {
			h.remove(c)
			return
		}
	}
}

func pump(ctx context.Context, h *Hub, c *conn, typ websocket.MessageType) {
	defer h.remove(c)
	for {
		select {
		case <-ctx.Done():
			return
		case b, ok := <-c.send:
			if !ok {
				return
			}
			ctxw, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.ws.Write(ctxw, typ, b)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

type overlayFS []fs.FS

func (o overlayFS) Open(name string) (fs.File, error) {
	var last error = fs.ErrNotExist
	for _, f := range o {
		file, err := f.Open(name)
		if err == nil {
			return file, nil
		}
		last = err
	}
	return nil, last
}
