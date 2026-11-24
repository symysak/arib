package main


import (
	"context"
	"net/http"

	"github.com/symysak/arib/internal/rx"
	std39cli "github.com/symysak/arib/internal/std-39/qpsk/cli"
	std39 "github.com/symysak/arib/internal/std-39/qpsk/server"
)

func init() {
	register(&System{Name: "39-qpsk", Dir: std39.System, Label: "RCR STD-39 第 2 編", Family: T79Family,
		Live: std39cli.Live, New: newSTD39})
}

type std39Mon struct{ m *std39.Monitor }

func (t *std39Mon) Handler() http.Handler         { return t.m.Handler() }
func (t *std39Mon) Run(ctx context.Context) error { return t.m.Run(ctx) }
func (t *std39Mon) Engine() rx.Stations           { return engineStations(t.m.Engine()) }
func (t *std39Mon) Evidence() Evidence {
	ok, score, text := std39.Evidence(t.m)
	return Evidence{OK: ok, Score: score, Text: text}
}

func newSTD39(e Env) (Monitor, error) {
	ec := e.Live.Engine(e.Plan)
	ec.Open, ec.LogDir, ec.LogOut, ec.Inputs = e.Open, e.LogDir, e.LogOut, nil
	return &std39Mon{m: std39.NewMonitor(std39.Config{Engine: ec, Codec: "auto", AudioTap: audioTap(e)})}, nil
}
