package digest

import (
	"context"
	"sync"
	"time"
)

type FakeSendCall struct {
	ChatID string
	Text   string
}

// FakeTelegram is a deterministic true-external substitute shared by digest tests.
type FakeTelegram struct {
	mu          sync.Mutex
	sendCalls   []FakeSendCall
	sendResults []error
	updates     [][]Update
	updateErrs  []error
}

func NewFakeTelegram() *FakeTelegram { return &FakeTelegram{} }

func (f *FakeTelegram) QueueSendError(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendResults = append(f.sendResults, err)
}

func (f *FakeTelegram) QueueUpdates(updates []Update, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, append([]Update(nil), updates...))
	f.updateErrs = append(f.updateErrs, err)
}

func (f *FakeTelegram) SendMessage(ctx context.Context, chatID, text string) (MessageRef, error) {
	if err := ctx.Err(); err != nil {
		return MessageRef{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendCalls = append(f.sendCalls, FakeSendCall{ChatID: chatID, Text: text})
	if len(f.sendResults) > 0 {
		err := f.sendResults[0]
		f.sendResults = f.sendResults[1:]
		return MessageRef{}, err
	}
	return MessageRef{MessageID: int64(len(f.sendCalls))}, nil
}

func (f *FakeTelegram) GetUpdates(ctx context.Context, _ int64, _ time.Duration) ([]Update, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.updates) == 0 {
		return nil, nil
	}
	updates := f.updates[0]
	err := f.updateErrs[0]
	f.updates = f.updates[1:]
	f.updateErrs = f.updateErrs[1:]
	return append([]Update(nil), updates...), err
}

func (f *FakeTelegram) SendCalls() []FakeSendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]FakeSendCall(nil), f.sendCalls...)
}

var (
	_ TelegramSender       = (*FakeTelegram)(nil)
	_ TelegramUpdateSource = (*FakeTelegram)(nil)
)
