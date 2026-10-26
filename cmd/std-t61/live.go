package main

import (
	"os"

	"github.com/symysak/arib/internal/std-t61/cli"
)

func cmdLive(argv []string) int {
	ctx, stop := interruptContext()
	defer stop()
	return cli.Live(ctx, "std-t61 live", argv, os.Stderr)
}
