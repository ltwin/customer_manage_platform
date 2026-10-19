package creativescene_test

import (
	"encoding/json"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

func jobRow(t *testing.T, f fixture, id string) (state string, attempt int, token *string, result json.RawMessage) {
	t.Helper()
	var raw string
	if err := f.db.QueryRow(`SELECT state, attempt, claim_token, coalesce(result::text, '') FROM creative_scene_jobs WHERE id=$1`, id).Scan(&state, &attempt, &token, &raw); err != nil {
		t.Fatal(err)
	}
	return state, attempt, token, json.RawMessage(raw)
}

func sweep(t *testing.T, scope store.AccountScope) creativescene.SweepCounts {
	t.Helper()
	counts, err := creativescene.SweepJobs(t.Context(), scope, 100)
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func TestSweepRequeuesThenFailsALostJob(t *testing.T) {
	f := setup(t)
	waiter := &fakeWaiter{}
	creativescene.SetJobWaiterForTest(t, waiter)
	doc := createScene(t, f.a)
	job := newJob(t, f.a, bakeJob(doc, "step-1"))
	lapse := func() {
		t.Helper()
		if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, job.ID); err != nil {
			t.Fatal(err)
		}
	}
	for attempt := 1; attempt <= 2; attempt++ {
		claimed := claimOne(t, f.a)
		if claimed.Attempt != attempt-1 {
			t.Fatalf("claim %d saw attempt %d", attempt, claimed.Attempt)
		}
		if got := sweep(t, f.a); got.Requeued != 0 {
			t.Fatalf("a held lease was reclaimed: %+v", got)
		}
		lapse()
		if got := sweep(t, f.a); got.Requeued != 1 {
			t.Fatalf("lapse %d: %+v", attempt, got)
		}
		if state, n, token, _ := jobRow(t, f, job.ID); state != creativescene.JobQueued || n != attempt || token != nil {
			t.Fatalf("after lapse %d: %s attempt %d token %v", attempt, state, n, token)
		}
		// The host that lost the lease can no longer report.
		if _, err := creativescene.Lease(t.Context(), f.a, job.ID, claimed.ClaimToken); err == nil {
			t.Fatal("a reclaimed claim renewed")
		}
	}
	claimOne(t, f.a)
	lapse()
	if got := sweep(t, f.a); got.HostErrors != 1 || got.Requeued != 0 {
		t.Fatalf("third lapse: %+v", got)
	}
	state, _, _, result := jobRow(t, f, job.ID)
	var fail creativescene.JobFailure
	if state != creativescene.JobFailed || json.Unmarshal(result, &fail) != nil || fail.Code != "host_error" {
		t.Fatalf("third lapse left %s %s", state, result)
	}
	if len(waiter.results) != 1 || waiter.results[0].Failure.Code != "host_error" {
		t.Fatalf("settled %+v", waiter.results)
	}
}

func TestSweepExpiresAndDeletes(t *testing.T) {
	f := setup(t)
	waiter := &fakeWaiter{}
	creativescene.SetJobWaiterForTest(t, waiter)
	doc := createScene(t, f.a)
	queued := newJob(t, f.a, bakeJob(doc, "step-1"))
	old := newJob(t, f.a, bakeJob(doc, "step-2"))
	if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET deadline=now()-interval '1 second' WHERE id=$1`, queued.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET state='succeeded', result='{}', updated_at=now()-interval '31 days' WHERE id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	got := sweep(t, f.a)
	if got.Expired != 1 || got.Deleted != 1 {
		t.Fatalf("sweep %+v", got)
	}
	state, _, _, result := jobRow(t, f, queued.ID)
	var fail creativescene.JobFailure
	if state != creativescene.JobExpired || json.Unmarshal(result, &fail) != nil || fail.Code != "expired" {
		t.Fatalf("expired job %s %s", state, result)
	}
	var n int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_scene_jobs WHERE id=$1`, old.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("old job kept: %d %v", n, err)
	}
	if len(waiter.results) != 1 || waiter.results[0].Failure.Code != "expired" || waiter.settled[0].State != creativescene.JobExpired {
		t.Fatalf("settled %+v", waiter.settled)
	}
	if again := sweep(t, f.a); again != (creativescene.SweepCounts{}) {
		t.Fatalf("second sweep did %+v", again)
	}
}

func TestSweepAbortsARunThatFailsAdmit(t *testing.T) {
	f := setup(t)
	waiter := &fakeWaiter{refuse: creativescene.ErrRunGate}
	creativescene.SetJobWaiterForTest(t, waiter)
	doc := createScene(t, f.a)
	job := newJob(t, f.a, bakeJob(doc, "step-1"))
	if _, err := f.db.Exec(`UPDATE creative_scene_jobs SET deadline=now()-interval '1 second' WHERE id=$1`, job.ID); err != nil {
		t.Fatal(err)
	}
	if got := sweep(t, f.a); got.Aborted != 1 || got.Expired != 0 {
		t.Fatalf("sweep %+v", got)
	}
	if len(waiter.aborted) != 1 || waiter.aborted[0] != "ccrn_test" {
		t.Fatalf("aborted %v", waiter.aborted)
	}
}
