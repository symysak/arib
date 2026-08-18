package server

import (
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

var staticFS embed.FS

type hub struct {
	mu    sync.Mutex
	conns map[*conn]struct{}
}

type conn struct {
	ws   *websocket.Conn
	send chan []byte
	bin  bool
}

func newHub() *hub { return &hub{conns: map[*conn]struct{}{}} }

func (h *hub) add(c *conn) {
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) remove(c *conn) {
	h.mu.Lock()
	if _, ok := h.conns[c]; ok {
		delete(h.conns, c)
		close(c.send)
	}
	h.mu.Unlock()
}

func (h *hub) broadcast(b []byte) {
	h.mu.Lock()
	for c := range h.conns {
		select {
		case c.send <- b:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *hub) broadcastJSON(e Event) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	h.broadcast(b)
}

func (h *hub) broadcastBinary(b []byte) { h.broadcast(b) }

func pcmFrame(seq int64, pcm []int16) []byte {
	b := make([]byte, 8+2*len(pcm))
	binary.LittleEndian.PutUint64(b[0:8], uint64(seq))
	for i, v := range pcm {
		binary.LittleEndian.PutUint16(b[8+2*i:], uint16(v))
	}
	return b
}

func WavBytes(pcm []int16, rate int) []byte {
	n := len(pcm) * 2
	b := make([]byte, 0, 44+n)
	le32 := func(v uint32) { b = append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }
	le16 := func(v uint16) { b = append(b, byte(v), byte(v>>8)) }
	b = append(b, "RIFF"...)
	le32(uint32(36 + n))
	b = append(b, "WAVEfmt "...)
	le32(16)
	le16(1)
	le16(1)
	le32(uint32(rate))
	le32(uint32(rate * 2))
	le16(2)
	le16(16)
	b = append(b, "data"...)
	le32(uint32(n))
	for _, v := range pcm {
		b = append(b, byte(uint16(v)), byte(uint16(v)>>8))
	}
	return b
}

type Server struct {
	p    *Pipeline
	mux  *http.ServeMux
	http *http.Server
}

func NewServer(p *Pipeline) *Server {
	s := &Server{p: p, mux: http.NewServeMux()}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	s.mux.Handle("/", http.FileServer(http.FS(sub)))
	s.mux.HandleFunc("/ws", s.handleWS)
	s.mux.HandleFunc("/ws/audio", s.handleWSAudio)
	s.mux.HandleFunc("/api/status", s.handleStatus)
	s.mux.HandleFunc("/api/audio/", s.handleAudio)
	s.mux.HandleFunc("/api/iq/", s.handleIQ)
	s.mux.HandleFunc("/api/scramble", s.handleScramble)
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) ListenAndServe(addr string) (string, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		host, portStr = "127.0.0.1", "8000"
	}
	port, _ := strconv.Atoi(portStr)
	if port == 0 {
		port = 8000
	}
	var ln net.Listener
	for i := 0; i < 20; i++ {
		ln, err = net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port+i)))
		if err == nil {
			break
		}
	}
	if ln == nil {
		return "", fmt.Errorf("待ち受けできません: %w", err)
	}
	s.http = &http.Server{Handler: s.mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.http.Serve(ln) }()
	return ln.Addr().String(), nil
}

func (s *Server) Close() error {
	if s.http == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.http.Shutdown(ctx)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(s.p.Snapshot())
}

func (s *Server) handleAudio(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/api/audio/")
	name = strings.TrimSuffix(name, ".wav")
	id, err := strconv.Atoi(name)
	if err != nil {
		http.Error(w, "通報 ID が不正です", http.StatusBadRequest)
		return
	}
	b, ok := s.p.AudioWav(id)
	if !ok {
		http.Error(w, "その通報の音声はありません", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=\"t115_broadcast%d.wav\"", id))
	_, _ = w.Write(b)
}

func (s *Server) handleScramble(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("value")
	if q == "" {
		http.Error(w, "value= を指定してください（0 = 自動判定へ戻す）",
			http.StatusBadRequest)
		return
	}
	v, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimPrefix(q, "0x"), "0X"), 16, 32)
	if err != nil || v < 0 || v > 0xFFFF {
		v, err = strconv.ParseInt(q, 10, 32)
		if err != nil || v < 0 || v > 0xFFFF {
			http.Error(w, "スクランブル値は 0〜65535（16 進なら 0x 付き）",
				http.StatusBadRequest)
			return
		}
	}
	s.p.PinScramble(int(v))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"requested": v})
}

func (s *Server) handleIQ(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/iq/"), ".wav")
	id, err := strconv.Atoi(name)
	if err != nil {
		http.Error(w, "通報 ID が不正です", http.StatusBadRequest)
		return
	}
	path := s.p.IQPath(id)
	if path == "" {
		http.Error(w, "その通報の I/Q 録音はありません", http.StatusNotFound)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "I/Q を書き出し中です（通報終了の 2 秒後に確定します）",
			http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "I/Q を読めません", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	s.serveWS(w, r, s.p.hub, false)
}

func (s *Server) handleWSAudio(w http.ResponseWriter, r *http.Request) {
	s.serveWS(w, r, s.p.audioHub, true)
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request, h *hub, bin bool) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}
	c := &conn{ws: ws, send: make(chan []byte, 256), bin: bin}
	h.add(c)
	defer func() {
		h.remove(c)
		_ = ws.Close(websocket.StatusNormalClosure, "bye")
	}()

	if !bin {
		b, err := json.Marshal(ev(EvSnapshot, s.p.Snapshot()))
		if err == nil {
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			_ = ws.Write(ctx, websocket.MessageText, b)
			cancel()
		}
	}

	go func() {
		for {
			if _, _, err := ws.Read(r.Context()); err != nil {
				h.remove(c)
				return
			}
		}
	}()

	typ := websocket.MessageText
	if bin {
		typ = websocket.MessageBinary
	}
	for b := range c.send {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		err := ws.Write(ctx, typ, b)
		cancel()
		if err != nil {
			return
		}
	}
}
