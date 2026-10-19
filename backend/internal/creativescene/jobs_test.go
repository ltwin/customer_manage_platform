package creativescene_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const tab = "tab_0123456789abcdef"

func newJob(t *testing.T, scope store.AccountScope, in creativescene.NewJob) creativescene.Job {
	t.Helper()
	var job creativescene.Job
	err := scope.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		job, err = creativescene.CreateJob(t.Context(), tx, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func bakeJob(doc, step string) creativescene.NewJob {
	return creativescene.NewJob{DocumentID: doc, Kind: creativescene.KindBakeModel, RunID: "ccrn_test", ToolCallID: step,
		Request: creativescene.BakeRequest{Code: pillarCode, Params: map[string]any{"height": 2.0}, LibVersion: 1,
			Target:   creativescene.BakeTarget{Op: "replace", ObjectID: objectID(1), Scope: "this"},
			Expected: map[string]*creativeops.Revision{"object:" + objectID(1) + ":content": rev(2)}}}
}

func rev(n int64) *creativeops.Revision {
	r := creativeops.Revision(n)
	return &r
}

func TestCreateJobOncePerToolCall(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	first := newJob(t, f.a, bakeJob(doc, "step-1"))
	again := newJob(t, f.a, bakeJob(doc, "step-1"))
	if first.ID != again.ID || first.State != creativescene.JobQueued || !strings.HasPrefix(first.ID, "csjob_") {
		t.Fatalf("replayed tool call made %+v then %+v", first, again)
	}
	if other := newJob(t, f.a, bakeJob(doc, "step-2")); other.ID == first.ID {
		t.Fatal("a second tool call reused the first job")
	}
	err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := creativescene.CreateJob(t.Context(), tx, bakeJob("cwdoc_missing", "step-3"))
		return err
	})
	if !errors.Is(err, creativedocument.ErrNotFound) {
		t.Fatalf("job for a missing scene: %v", err)
	}
	err = f.b.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := creativescene.CreateJob(t.Context(), tx, bakeJob(doc, "step-4"))
		return err
	})
	if !errors.Is(err, creativedocument.ErrNotFound) {
		t.Fatalf("job for another account's scene: %v", err)
	}
}

func TestClaimLeaseAndList(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	other := createScene(t, f.a)

	for _, in := range []creativescene.ClaimInput{
		{EditorSessionID: "short"},
		{EditorSessionID: tab, Kinds: []string{"bake"}},
	} {
		if _, err := creativescene.Claim(t.Context(), f.a, in); !errors.Is(err, creativeops.ErrValidation) {
			t.Fatalf("claim %+v: %v", in, err)
		}
	}
	if _, err := creativescene.Claim(t.Context(), f.b, creativescene.ClaimInput{EditorSessionID: tab, DocumentID: doc}); !errors.Is(err, creativedocument.ErrNotFound) {
		t.Fatalf("claim on another account's scene: %v", err)
	}
	if got, err := creativescene.Claim(t.Context(), f.a, creativescene.ClaimInput{EditorSessionID: tab}); err != nil || got != nil {
		t.Fatalf("empty queue: %+v %v", got, err)
	}

	bake := newJob(t, f.a, bakeJob(doc, "step-1"))
	list, err := creativescene.ListActive(t.Context(), f.a, doc)
	if err != nil || len(list) != 1 {
		t.Fatalf("list %+v %v", list, err)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "export default") || strings.Contains(string(raw), "height") || strings.Contains(string(raw), "clm_") {
		t.Fatalf("list leaks code, params or claim: %s", raw)
	}
	if string(list[0].Target) != `{"object_ids":["`+objectID(1)+`"],"op":"replace"}` || list[0].State != creativescene.JobQueued {
		t.Fatalf("target %s", list[0].Target)
	}
	if empty, err := creativescene.ListActive(t.Context(), f.a, other); err != nil || len(empty) != 0 {
		t.Fatalf("other scene %+v %v", empty, err)
	}
	if _, err := creativescene.ListActive(t.Context(), f.b, doc); !errors.Is(err, creativedocument.ErrNotFound) {
		t.Fatalf("list another account's scene: %v", err)
	}

	if got, _ := creativescene.Claim(t.Context(), f.a, creativescene.ClaimInput{EditorSessionID: tab, DocumentID: other}); got != nil {
		t.Fatal("claimed a job of another scene")
	}
	claimed, err := creativescene.Claim(t.Context(), f.a, creativescene.ClaimInput{EditorSessionID: tab, Kinds: []string{"bake_model"}})
	if err != nil || claimed == nil || claimed.ID != bake.ID || claimed.DocumentID != doc || !strings.HasPrefix(claimed.ClaimToken, "clm_") {
		t.Fatalf("claim %+v %v", claimed, err)
	}
	var req creativescene.BakeRequest
	if err := json.Unmarshal(claimed.Request, &req); err != nil || req.Code != pillarCode {
		t.Fatalf("claimed request %s", claimed.Request)
	}

	until, err := creativescene.Lease(t.Context(), f.a, claimed.ID, claimed.ClaimToken)
	if err != nil || until.Before(claimed.LeaseUntil) || until.After(claimed.Deadline) {
		t.Fatalf("lease %v %v (claimed until %v)", until, err, claimed.LeaseUntil)
	}
	if _, err := creativescene.Lease(t.Context(), f.a, claimed.ID, "clm_wrong"); !errors.Is(err, creativescene.ErrJobLeaseLost) {
		t.Fatalf("lease with another token: %v", err)
	}
	if _, err := creativescene.Lease(t.Context(), f.b, claimed.ID, claimed.ClaimToken); !errors.Is(err, creativedocument.ErrNotFound) {
		t.Fatalf("lease from another account: %v", err)
	}
	if list, _ := creativescene.ListActive(t.Context(), f.a, doc); len(list) != 1 || list[0].State != creativescene.JobClaimed {
		t.Fatalf("claimed job not listed as claimed: %+v", list)
	}
}

func TestCancelRunJobs(t *testing.T) {
	f := setup(t)
	doc := createScene(t, f.a)
	queued := newJob(t, f.a, bakeJob(doc, "step-1"))
	claimedJob := newJob(t, f.a, bakeJob(doc, "step-2"))
	claimed, err := creativescene.Claim(t.Context(), f.a, creativescene.ClaimInput{EditorSessionID: tab})
	if err != nil || claimed == nil || claimed.ID != queued.ID {
		t.Fatalf("claim %+v %v", claimed, err)
	}
	_ = claimedJob
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		return creativescene.CancelRunJobs(t.Context(), tx, "ccrn_test")
	}); err != nil {
		t.Fatal(err)
	}
	var jobs []creativescene.Job
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		var err error
		jobs, err = creativescene.LockJobs(t.Context(), tx, []string{queued.ID, claimedJob.ID})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, job := range jobs {
		var res creativescene.JobFailure
		if job.State != creativescene.JobCancelled || json.Unmarshal(job.Result, &res) != nil || res.Code != "cancelled" {
			t.Fatalf("job %s: %s %s", job.ID, job.State, job.Result)
		}
	}
	if _, err := creativescene.Lease(t.Context(), f.a, claimed.ID, claimed.ClaimToken); !errors.Is(err, creativescene.ErrJobLeaseLost) {
		t.Fatalf("late lease on a cancelled job: %v", err)
	}
	if list, _ := creativescene.ListActive(t.Context(), f.a, doc); len(list) != 0 {
		t.Fatalf("cancelled jobs still listed: %+v", list)
	}
}
