package shootplanning

import (
	"testing"
	"time"
)

func TestReplayCurrentOutcomeTruthTable(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	result := func(id string, seq int64, kind ShotResult) ExecutionFact {
		return ExecutionFact{Kind: ExecutionFactResult, ID: id, Sequence: seq, Result: kind, CheckedAt: now}
	}
	void := func(id string, seq int64, target string) ExecutionFact {
		return ExecutionFact{Kind: ExecutionFactVoid, ID: id, Sequence: seq, TargetEventID: target, CheckedAt: now}
	}

	tests := []struct {
		name    string
		facts   []ExecutionFact
		wantID  string
		wantSet bool
	}{
		{name: "captured E1 then cleared E2", facts: []ExecutionFact{result("E1", 1, ShotResultCaptured), result("E2", 2, ShotResultCleared)}},
		{name: "void latest cleared falls back", facts: []ExecutionFact{result("E1", 1, ShotResultCaptured), result("E2", 2, ShotResultCleared), void("V1", 3, "E2")}, wantID: "E1", wantSet: true},
		{name: "new capture after cleared is current", facts: []ExecutionFact{result("E1", 1, ShotResultCaptured), result("E2", 2, ShotResultCleared), result("E3", 3, ShotResultCaptured)}, wantID: "E3", wantSet: true},
		{name: "void E3 exposes cleared and remains unset", facts: []ExecutionFact{result("E1", 1, ShotResultCaptured), result("E2", 2, ShotResultCleared), result("E3", 3, ShotResultCaptured), void("V1", 4, "E3")}},
		{name: "then void cleared falls back E1", facts: []ExecutionFact{result("E1", 1, ShotResultCaptured), result("E2", 2, ShotResultCleared), result("E3", 3, ShotResultCaptured), void("V1", 4, "E3"), void("V2", 5, "E2")}, wantID: "E1", wantSet: true},
		{name: "void historical skipped does not affect later capture", facts: []ExecutionFact{result("E1", 1, ShotResultSkipped), result("E2", 2, ShotResultCaptured), void("V1", 3, "E1")}, wantID: "E2", wantSet: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ReplayCurrentOutcome(tt.facts)
			if err != nil {
				t.Fatalf("ReplayCurrentOutcome() error = %v", err)
			}
			if got.Set != tt.wantSet || got.EventID != tt.wantID {
				t.Fatalf("ReplayCurrentOutcome() = %+v, want set=%v id=%q", got, tt.wantSet, tt.wantID)
			}
		})
	}
}

func TestReplayRejectsInvalidTimeline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		facts []ExecutionFact
	}{
		{name: "duplicate sequence", facts: []ExecutionFact{{Kind: ExecutionFactResult, ID: "E1", Sequence: 1, Result: ShotResultCaptured}, {Kind: ExecutionFactResult, ID: "E2", Sequence: 1, Result: ShotResultCaptured}}},
		{name: "void unknown event", facts: []ExecutionFact{{Kind: ExecutionFactVoid, ID: "V1", Sequence: 1, TargetEventID: "missing"}}},
		{name: "void a void", facts: []ExecutionFact{{Kind: ExecutionFactResult, ID: "E1", Sequence: 1, Result: ShotResultCaptured}, {Kind: ExecutionFactVoid, ID: "V1", Sequence: 2, TargetEventID: "E1"}, {Kind: ExecutionFactVoid, ID: "V2", Sequence: 3, TargetEventID: "V1"}}},
		{name: "duplicate void", facts: []ExecutionFact{{Kind: ExecutionFactResult, ID: "E1", Sequence: 1, Result: ShotResultCaptured}, {Kind: ExecutionFactVoid, ID: "V1", Sequence: 2, TargetEventID: "E1"}, {Kind: ExecutionFactVoid, ID: "V2", Sequence: 3, TargetEventID: "E1"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ReplayCurrentOutcome(tt.facts); err == nil {
				t.Fatal("ReplayCurrentOutcome() error = nil, want invalid timeline")
			}
		})
	}
}
