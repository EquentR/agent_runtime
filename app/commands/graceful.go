package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/EquentR/agent_runtime/pkg/log"
)

var (
	globalCtx context.Context
	cancel    context.CancelFunc
)

// GracefulExit —— 零侵入初始化，只需要 GracefulExit() 一句即可
func GracefulExit() {
	globalCtx, cancel = context.WithCancel(context.Background())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		s := <-sigCh
		log.Infof("[graceful] received signal: %v", s)

		cancel() // 通知所有 goroutine 退出

		// 给业务逻辑一点时间退出
		time.Sleep(200 * time.Millisecond)

		log.Info("[graceful] exiting process")
		os.Exit(0)
	}()
}
