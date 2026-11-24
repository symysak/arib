package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
)

func interruptContext() (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	sig := make(chan os.Signal, 2)
	signal.Notify(sig, os.Interrupt)
	go func() {
		if _, ok := <-sig; !ok {
			return
		}
		fmt.Fprintln(os.Stderr, "停止します（録音を閉じてから終わる。もう一度で即終了）")
		cancel()
		if _, ok := <-sig; ok {
			os.Exit(130)
		}
	}()
	return ctx, func() {
		signal.Stop(sig)
		close(sig)
		cancel()
	}
}
