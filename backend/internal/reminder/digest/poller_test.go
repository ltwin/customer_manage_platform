package digest

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type recordingUpdateProcessor struct {
	seen   []int64
	failID int64
}

func (p *recordingUpdateProcessor) HandleUpdate(_ context.Context, update Update) (bool, error) {
	p.seen = append(p.seen, update.ID)
	if update.ID == p.failID {
		return false, errors.New("transaction outcome unknown")
	}
	return true, nil
}

func TestPollerAdvancesOffsetOnlyAcrossDurableTerminalPrefix(t *testing.T) {
	source := NewFakeTelegram()
	source.QueueUpdates([]Update{{ID: 3}, {ID: 1}, {ID: 2}}, nil)
	processor := &recordingUpdateProcessor{failID: 2}
	poller := NewPoller(source, processor)

	err := poller.PollOnce(context.Background())
	if err == nil {
		t.Fatal("non-durable update must stop the batch")
	}
	if !reflect.DeepEqual(processor.seen, []int64{1, 2}) {
		t.Fatalf("poller did not stop continuous suffix: %v", processor.seen)
	}
	if poller.NextOffset() != 2 {
		t.Fatalf("failed update was acknowledged: next offset=%d", poller.NextOffset())
	}

	processor.failID = 0
	source.QueueUpdates([]Update{{ID: 3}, {ID: 2}}, nil)
	if err := poller.PollOnce(context.Background()); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if poller.NextOffset() != 4 {
		t.Fatalf("durable replay did not advance suffix: %d", poller.NextOffset())
	}
}

func TestPollerContextCancellationInterruptsLongPoll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	poller := NewPoller(NewFakeTelegram(), &recordingUpdateProcessor{})
	if err := poller.PollOnce(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want canceled long poll, got %v", err)
	}
}
