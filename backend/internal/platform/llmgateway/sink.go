package llmgateway

import "context"

// DeltaKind names what a streamed fragment is. A provider only emits the kinds
// it understands, so adding one never changes an existing adapter.
type DeltaKind int

const (
	// DeltaText is a fragment of the visible answer text.
	DeltaText DeltaKind = iota + 1
)

// Sink receives fragments while a streamed attempt is still in flight. They are
// display only: the gateway still assembles and validates the complete Result,
// and a fragment is never a result (see Result in contract.go). Reset means the
// fragments delivered so far belong to an attempt that is being replaced.
//
// Calls happen on the goroutine running the attempt; an implementation must not
// block on the database for long, because the provider connection is open.
type Sink interface {
	Delta(kind DeltaKind, text string)
	Reset()
}

type sinkKey struct{}

// withSink carries the caller's sink from Call down to the one attempt that
// dispatches, without widening every internal stage's signature.
func withSink(ctx context.Context, sink Sink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, sinkKey{}, sink)
}

func sinkOf(ctx context.Context) Sink {
	sink, _ := ctx.Value(sinkKey{}).(Sink)
	return sink
}

// emit forwards a fragment when someone is listening. Empty fragments are not
// news and are dropped here once instead of in every adapter.
func emit(sink Sink, kind DeltaKind, text string) {
	if sink != nil && text != "" {
		sink.Delta(kind, text)
	}
}
