package main

import (
	"os"

	"github.com/symysak/arib/internal/std-t115/qpsknarrow/cli"
)

func cmdLive(argv []string) int {
	ctx, stop := interruptContext()
	defer stop()
	return cli.Live(ctx, "std-t115-qpsknarrow live", argv, os.Stderr)
}
