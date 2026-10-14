package creativeagent

import (
	"context"
	"testing"
	"time"
)

// A renewal that fails while the claim still holds is a network blip, not a
// lost claim: the turn keeps going. A stop request still ends it.
func TestLeaseRenewalSurvivesTransientFailure(t *testing.T) {
	renewal, retry := leaseRenewal, leaseRetry
	leaseRenewal, leaseRetry = 200*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { leaseRenewal, leaseRetry = renewal, retry })

	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID, InstructionSegment{Type: "text", Text: "整理参考"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	var claimed time.Time
	if err = f.db.QueryRow(`SELECT lease_until FROM creative_agent_runs WHERE id=$1`, run.ID).Scan(&claimed); err != nil {
		t.Fatal(err)
	}

	// Holding the run row makes every renewal time out, as it does while the
	// database cannot be reached.
	lock, err := f.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lock.Exec(`SELECT 1 FROM creative_agent_runs WHERE id=$1 FOR UPDATE`, run.ID); err != nil {
		t.Fatal(err)
	}
	attempt, cancel := context.WithCancel(t.Context())
	defer cancel()
	stop := f.service.renewLease(attempt, f.alice, held, cancel)
	defer stop()
	time.Sleep(3 * leaseRenewal)
	if err = lock.Rollback(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * leaseRenewal)
	if attempt.Err() != nil {
		t.Fatal("a failed renewal cancelled a turn whose lease still held")
	}
	var renewed time.Time
	if err = f.db.QueryRow(`SELECT lease_until FROM creative_agent_runs WHERE id=$1`, run.ID).Scan(&renewed); err != nil {
		t.Fatal(err)
	}
	if !renewed.After(claimed) {
		t.Fatalf("lease not renewed after the outage: %v -> %v", claimed, renewed)
	}

	if _, err = f.db.Exec(`UPDATE creative_agent_runs SET cancel_requested_at=now() WHERE id=$1`, run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-attempt.Done():
	case <-time.After(5 * leaseRenewal):
		t.Fatal("a lost claim did not stop the turn")
	}
}

// While renewals cannot get through, the turn stops when the lease lapses, not
// a full renewal timeout later: past that point another worker may own it.
func TestLeaseOutageStopsTurnWhenLeaseLapses(t *testing.T) {
	duration, renewal, retry := leaseDuration, leaseRenewal, leaseRetry
	leaseDuration, leaseRenewal, leaseRetry = 600*time.Millisecond, 400*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { leaseDuration, leaseRenewal, leaseRetry = duration, renewal, retry })

	f := setup(t)
	f.queue()
	c := f.conversation(f.alice, f.canvasID)
	run, err := f.startRun(f.alice, c.ID, f.consent(f.alice, c.ID).ID, InstructionSegment{Type: "text", Text: "整理参考"})
	if err != nil {
		t.Fatal(err)
	}
	held, err := f.service.claimRun(t.Context(), f.alice, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := f.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Rollback() }()
	if _, err = lock.Exec(`SELECT 1 FROM creative_agent_runs WHERE id=$1 FOR UPDATE`, run.ID); err != nil {
		t.Fatal(err)
	}
	attempt, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := time.Now()
	stop := f.service.renewLease(attempt, f.alice, held, cancel)
	defer stop()
	select {
	case <-attempt.Done():
	case <-time.After(2 * leaseDuration):
		t.Fatal("the turn outlived its lease")
	}
	if lapsed := time.Since(started); lapsed > leaseDuration+100*time.Millisecond {
		t.Fatalf("the turn stopped %v after starting, past its %v lease", lapsed, leaseDuration)
	}
}
