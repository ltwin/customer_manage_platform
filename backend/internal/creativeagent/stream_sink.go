package creativeagent

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	// streamFlushInterval and streamFlushBytes bound how often streamed text is
	// written: at most ten short transactions a second per run, so the run row
	// lock is never held for long.
	streamFlushInterval = 100 * time.Millisecond
	streamFlushBytes    = 512
)

// textSink writes the visible answer into the step's text block while the
// model is still streaming. It is a preview: the block is overwritten with the
// validated result when the step is consumed, and text still buffered when a
// call fails is simply dropped.
type textSink struct {
	service *Service
	scope   store.AccountScope
	held    claim
	current *currentStep
	ctx     context.Context

	mu        sync.Mutex
	step      string
	buffer    strings.Builder
	written   int
	lastFlush time.Time
	// replace makes the next write a set rather than an append: the first write
	// of a step in this process (a redo after a crash finds the old preview)
	// and the first write after the gateway replaced an attempt.
	replace bool
}

var _ llmgateway.Sink = (*textSink)(nil)

func (s *textSink) Delta(kind llmgateway.DeltaKind, text string) {
	if kind != llmgateway.DeltaText {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	step, _ := s.current.get()
	if step == "" {
		return
	}
	if step != s.step {
		s.step, s.written, s.replace = step, 0, true
		s.buffer.Reset()
	}
	s.buffer.WriteString(text)
	if s.buffer.Len() >= streamFlushBytes || time.Since(s.lastFlush) >= streamFlushInterval {
		s.flushLocked()
	}
}

// Reset withdraws what the replaced attempt showed.
func (s *textSink) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buffer.Reset()
	s.replace = true
	if s.written > 0 {
		s.flushLocked() // an empty replacing write clears the preview now
	}
	s.written = 0
}

func (s *textSink) flushLocked() {
	text := s.buffer.String()
	if text == "" && !s.replace {
		return
	}
	if s.written+len(text) > blockTextBytes/2 {
		// The final set splits long answers across blocks; the preview stops.
		s.buffer.Reset()
		return
	}
	change := blockChange{Type: BlockText, State: "streaming"}
	if s.replace {
		change.Set = textPayload{Markdown: text}
	} else {
		change.Append = &textAppend{Path: "markdown", Text: text}
	}
	err := s.scope.WithTxScope(s.ctx, func(tx store.TxAccountScope) error {
		// Only the current holder may show text for this run.
		var state string
		var epoch int64
		if err := tx.QueryRowForUpdate(s.ctx, "creative_agent_runs", "state,execution_epoch",
			"id=$2 AND claim_token=$3", s.held.RunID, s.held.Token).Scan(&state, &epoch); err != nil {
			return err
		}
		if state != RunRunning || epoch != s.held.Epoch {
			return ErrRunState
		}
		_, err := writeBlockInTx(s.ctx, tx, s.held.RunID, textSourceKey(s.step, 0), change)
		return err
	})
	s.lastFlush = time.Now()
	s.buffer.Reset()
	if err != nil {
		// Display only: the consumed result still becomes the block.
		s.service.logger.DebugContext(s.ctx, "creative agent could not stream text",
			slog.String("run_id", s.held.RunID), slog.String("error", err.Error()))
		return
	}
	s.written += len(text)
	s.replace = false
}
