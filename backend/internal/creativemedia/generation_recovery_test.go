package creativemedia_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/versionedfs"
)

type failingProbeRead struct{ versionedfs.Adapter }

func (failingProbeRead) OpenVersion(context.Context, string, string, *versionedfs.ByteRange) (io.ReadCloser, versionedfs.ObjectStat, error) {
	return nil, versionedfs.ObjectStat{}, errors.New("object read temporarily unavailable")
}

func queuedProbeRequest(t *testing.T, db *sql.DB, probeID string, latest bool) (int64, jobs.Request) {
	t.Helper()
	order := "ASC"
	if latest {
		order = "DESC"
	}
	var id int64
	var raw []byte
	if err := db.QueryRow(`SELECT id,args->'request' FROM creative_jobs.river_job WHERE args->'request'->'payload'->>'probe_id'=$1 ORDER BY id `+order+` LIMIT 1`, probeID).Scan(&id, &raw); err != nil {
		t.Fatal(err)
	}
	var req jobs.Request
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	return id, req
}

func TestFactsProbeExplicitRetrySurvivesOldRunningDelivery(t *testing.T) {
	f := setup(t)
	_, revision := uploadAsset(t, f, "重试.png", "image/png", "image", sample(t, "sample.png"))
	grantGeneration(t, f.a, revision)
	status, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil {
		t.Fatal(err)
	}
	oldID, oldRequest := queuedProbeRequest(t, f.db, status.ProbeID, false)
	if _, err := f.db.Exec(`UPDATE creative_jobs.river_job SET state='running' WHERE id=$1`, oldID); err != nil {
		t.Fatal(err)
	}
	failing, err := creativemedia.NewService(f.cfg, failingProbeRead{f.local}, creativemedia.NewVerifier(f.cfg), f.key, "/api/v1/creative/media")
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range failing.Handlers() {
		if h.Kind != "media.facts_probe" {
			continue
		}
		for attempt := 1; attempt <= 3; attempt++ {
			err := h.Work(t.Context(), f.a, oldRequest)
			if (attempt < 3 && err == nil) || (attempt == 3 && err != nil) {
				t.Fatalf("attempt %d: %v", attempt, err)
			}
		}
	}
	if state, _, _ := probeRow(t, f.db, status.ProbeID); state != "failed" {
		t.Fatalf("expected worker failure, got %s", state)
	}
	// 领域失败已提交，旧 River 投递尚未完成：在这个间隙显式重试。
	again, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || again.ProbeID != status.ProbeID || again.State != "pending" {
		t.Fatalf("explicit retry: %+v %v", again, err)
	}
	if _, err := f.db.Exec(`UPDATE creative_jobs.river_job SET state='completed',finalized_at=now() WHERE id=$1`, oldID); err != nil {
		t.Fatal(err)
	}
	if n := availableProbeJobs(t, f.db); n != 1 {
		t.Fatalf("explicit retry lost its delivery: available=%d", n)
	}
	newID, newRequest := queuedProbeRequest(t, f.db, status.ProbeID, true)
	if newID == oldID || string(newRequest.Payload) == string(oldRequest.Payload) {
		t.Fatal("explicit retry reused the old delivery identity")
	}
	// 旧轮次重复投递不得消耗新轮次预算，也不得把新轮次提前做完。
	if err := probeHandler(t, f)(t.Context(), f.a, oldRequest); err != nil {
		t.Fatalf("stale delivery must finish harmlessly: %v", err)
	}
	if state, attempts, pins := probeRow(t, f.db, status.ProbeID); state != "pending" || attempts != 0 || pins != 1 {
		t.Fatalf("stale delivery touched new round: state=%s attempts=%d pins=%d", state, attempts, pins)
	}
	// 同轮次重复调度仍只保留一个可执行投递。
	if _, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision); err != nil {
		t.Fatal(err)
	}
	if n := availableProbeJobs(t, f.db); n != 1 {
		t.Fatalf("same round duplicated deliveries: %d", n)
	}
	f.drain(t, f.a)
	if state, attempts, pins := probeRow(t, f.db, status.ProbeID); state != "succeeded" || attempts != 1 || pins != 0 {
		t.Fatalf("new round did not finish: state=%s attempts=%d pins=%d", state, attempts, pins)
	}
}

func TestFactsProbeHistoricalTerminalDeliveryCompletes(t *testing.T) {
	for _, terminal := range []string{"succeeded", "failed", "unsupported"} {
		t.Run(terminal, func(t *testing.T) {
			f := setup(t)
			_, revision := uploadAsset(t, f, "历史.png", "image/png", "image", sample(t, "sample.png"))
			grantGeneration(t, f.a, revision)
			status, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
			if err != nil {
				t.Fatal(err)
			}
			_, req := queuedProbeRequest(t, f.db, status.ProbeID, false)
			f.drain(t, f.a)
			if _, err := f.db.Exec(`UPDATE creative_media_probes SET state=$2,extractor_version='facts-v0' WHERE id=$1`, status.ProbeID, terminal); err != nil {
				t.Fatal(err)
			}
			if err := probeHandler(t, f)(t.Context(), f.a, req); err != nil {
				t.Fatalf("historical terminal delivery must finish, got %v", err)
			}
			if state, attempts, pins := probeRow(t, f.db, status.ProbeID); state != terminal || attempts != 1 || pins != 0 {
				t.Fatalf("terminal changed: state=%s attempts=%d pins=%d", state, attempts, pins)
			}
			if n := factsCount(t, f.db); n != 1 {
				t.Fatalf("terminal redelivery changed facts: %d", n)
			}
		})
	}
}
