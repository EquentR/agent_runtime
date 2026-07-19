package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/EquentR/agent_runtime/pkg/log"
)

var (
	globalCtx context.Context
	cancel    context.CancelFunc
)

func GracefulExit() {
	globalCtx, cancel = context.WithCancel(context.Background())
	signalChannel := make(chan os.Signal, 1)
	signal.Notify(signalChannel, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		received := <-signalChannel
		log.Infof("[graceful] received signal: %v", received)
		cancel()
	}()
}

func RequestShutdown() {
	if cancel != nil {
		cancel()
	}
}
