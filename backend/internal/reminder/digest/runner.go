package digest

import (
	"context"
	"log/slog"
	"sync"
)

// DigestLoop 是 TelegramRunner 内部子循环的最小生命周期面。
type DigestLoop interface {
	Run(context.Context)
}

// TelegramRunner 是 composition root 唯一注册的 Telegram 后台运行器。
// poll/daily/sender 共用调用方 context，并在返回前等待三个子循环全部退出。
type TelegramRunner struct {
	poller DigestLoop
	daily  DigestLoop
	sender DigestLoop
	logger *slog.Logger
}

func NewTelegramRunner(poller, daily, sender DigestLoop, logger *slog.Logger) *TelegramRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &TelegramRunner{poller: poller, daily: daily, sender: sender, logger: logger}
}

func (r *TelegramRunner) Run(ctx context.Context) {
	r.logger.Info("telegram integration runner started")
	var runners sync.WaitGroup
	runners.Add(3)
	for _, loop := range []DigestLoop{r.poller, r.daily, r.sender} {
		go func() {
			defer runners.Done()
			loop.Run(ctx)
		}()
	}
	runners.Wait()
	r.logger.Info("telegram integration runner stopped")
}
