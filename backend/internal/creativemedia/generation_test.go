package creativemedia_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativelibrary"
	"github.com/samson/customer-manage-platform/backend/internal/creativemedia"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// originalReference 从可信存储取该修订 original rendition 的生成引用，
// 也就是将来画布桥接的构造方式。
func originalReference(t *testing.T, db *sql.DB, account, revisionID string) llmgateway.GenerationReference {
	t.Helper()
	var kind, mime, digest string
	var size int64
	err := db.QueryRow(`SELECT c.kind, b.mime, b.sha256, b.byte_size
		FROM creative_content_objects o
		JOIN creative_blobs b ON b.account_id=o.account_id AND b.id=o.blob_id
		JOIN creative_content_revisions r ON r.account_id=o.account_id AND r.id=o.content_revision_id
		JOIN creative_contents c ON c.account_id=r.account_id AND c.id=r.content_id
		WHERE o.account_id=$2 AND o.content_revision_id=$1 AND o.role='original'`, revisionID, account).Scan(&kind, &mime, &digest, &size)
	if err != nil {
		t.Fatal(err)
	}
	return llmgateway.GenerationReference{Role: "reference_image", RevisionID: revisionID, Digest: strings.TrimPrefix(digest, "sha256-"), Kind: llmgateway.GenerationKind(kind), MIME: mime, ByteSize: size}
}

func uploadAsset(t *testing.T, f *fixture, name, mime, kind string, body []byte) (string, string) {
	t.Helper()
	view := f.uploadFile(t, f.a, name, mime, kind, body, creativemedia.Target{Kind: "asset", Asset: &creativemedia.AssetTarget{Title: name}})
	if view.State != "ready" || view.Publication == nil || view.Publication.Kind != "asset" {
		t.Fatalf("publication %+v", view)
	}
	asset, err := creativelibrary.GetAsset(t.Context(), f.a, view.Publication.ID)
	if err != nil || asset.ContentRevisionID == "" {
		t.Fatalf("asset %+v %v", asset, err)
	}
	return view.Publication.ID, asset.ContentRevisionID
}

func grantGeneration(t *testing.T, scope store.AccountScope, revisionID string) {
	t.Helper()
	if err := creativecontent.GrantGenerationReference(t.Context(), scope, revisionID); err != nil {
		t.Fatal(err)
	}
}

func probeHandler(t *testing.T, f *fixture) func(context.Context, store.AccountScope, jobs.Request) error {
	t.Helper()
	for _, h := range f.svc.Handlers() {
		if h.Kind == "media.facts_probe" {
			return h.Work
		}
	}
	t.Fatal("no facts probe handler")
	return nil
}

func probeJob(t *testing.T, probeID string) jobs.Request {
	t.Helper()
	return jobs.Request{Kind: "media.facts_probe", OperationID: uuid.NewString(), CreatedAt: time.Now(), Payload: creativegraph.Canonical(map[string]string{"probe_id": probeID}), UniqueByArgs: true}
}

func probeRow(t *testing.T, db *sql.DB, probeID string) (state string, attempts int, pinCount int) {
	t.Helper()
	var pin *string
	if err := db.QueryRow(`SELECT state,attempts,read_pin_id FROM creative_media_probes WHERE id=$1`, probeID).Scan(&state, &attempts, &pin); err != nil {
		t.Fatal(err)
	}
	if pin != nil {
		if err := db.QueryRow(`SELECT count(*) FROM creative_blob_read_pins WHERE id=$1`, *pin).Scan(&pinCount); err != nil {
			t.Fatal(err)
		}
	}
	return state, attempts, pinCount
}

func availableProbeJobs(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM creative_jobs.river_job WHERE state='available' AND args::text LIKE '%media.facts_probe%'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func factsCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM creative_media_facts`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestGenerationReaderAuthorizationAndAccountIsolation(t *testing.T) {
	f := setup(t)
	_, revision := uploadAsset(t, f, "窗边.png", "image/png", "image", sample(t, "sample.png"))
	ref := originalReference(t, f.db, "media-a", revision)
	reader := f.svc.GenerationReaderFor(f.a)

	// 没有显式生成授权时引用被拒，尽管同一修订用于展示完全可读。
	if _, err := reader.ResolveGenerationMedia(t.Context(), ref); !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatalf("ungranted reference resolved: %v", err)
	}
	if err := f.a.WithTxScope(t.Context(), func(tx store.TxAccountScope) error {
		_, err := creativecontent.RequireUsable(t.Context(), tx, revision, "ai_analysis")
		return err
	}); !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatal("ai_analysis must stay denied", err)
	}

	grantGeneration(t, f.a, revision)
	meta, err := reader.ResolveGenerationMedia(t.Context(), ref)
	if err != nil || meta.RevisionID != revision || meta.MIME != "image/png" || meta.ByteSize != ref.ByteSize || meta.ExtractorVersion != creativemedia.FactsExtractorVersion {
		t.Fatalf("granted reference %+v %v", meta, err)
	}
	if meta.Digest != ref.Digest || meta.Width != nil || meta.Height != nil {
		t.Fatalf("identity settled but facts unknown expected: %+v", meta)
	}

	// 另一个账户根本看不到这个修订。
	if _, err := f.svc.GenerationReaderFor(f.b).ResolveGenerationMedia(t.Context(), ref); !errors.Is(err, creativecontent.ErrNotFound) {
		t.Fatalf("cross-account reference resolved: %v", err)
	}
	// 授权本身按账户隔离：另一个账户无法授权。
	if err := creativecontent.GrantGenerationReference(t.Context(), f.b, revision); !errors.Is(err, creativecontent.ErrNotFound) {
		t.Fatalf("cross-account grant accepted: %v", err)
	}

	// 撤回在下一次守卫生效前收回同意。
	if err := creativecontent.RevokeGenerationReference(t.Context(), f.a, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ResolveGenerationMedia(t.Context(), ref); !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatalf("revoked reference resolved: %v", err)
	}
	if err := creativecontent.RevokeGenerationReference(t.Context(), f.a, revision); err != nil {
		t.Fatal("revoke not idempotent", err)
	}

	// 授权素材在本声明命名空间下表达不了出站标志：授权被拒，而不是
	// 悄悄放宽矩阵。
	_, licensedRevision := uploadAsset(t, f, "授权图.png", "image/png", "image", sample(t, "sample.png"))
	if _, err := f.db.Exec(`UPDATE creative_rights_declarations SET source_class='licensed', rights_basis='license_recorded' WHERE id=(SELECT rights_declaration_id FROM creative_content_revisions WHERE account_id='media-a' AND id=$1)`, licensedRevision); err != nil {
		t.Fatal(err)
	}
	if err := creativecontent.GrantGenerationReference(t.Context(), f.a, licensedRevision); !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatalf("licensed generation grant accepted: %v", err)
	}
}

func TestGenerationReaderRejectsForgedIdentity(t *testing.T) {
	f := setup(t)
	_, revision := uploadAsset(t, f, "窗边.png", "image/png", "image", sample(t, "sample.png"))
	grantGeneration(t, f.a, revision)
	reader := f.svc.GenerationReaderFor(f.a)

	cases := map[string]func(*llmgateway.GenerationReference){
		"wrong digest":     func(r *llmgateway.GenerationReference) { r.Digest = strings.Repeat("b", 64) },
		"wrong mime":       func(r *llmgateway.GenerationReference) { r.MIME = "image/jpeg" },
		"wrong size":       func(r *llmgateway.GenerationReference) { r.ByteSize++ },
		"wrong kind":       func(r *llmgateway.GenerationReference) { r.Kind = llmgateway.GenerationVideo },
		"uppercase digest": func(r *llmgateway.GenerationReference) { r.Digest = strings.ToUpper(r.Digest) },
		"empty revision":   func(r *llmgateway.GenerationReference) { r.RevisionID = "" },
	}
	ref := originalReference(t, f.db, "media-a", revision)
	for name, mutate := range cases {
		forged := ref
		mutate(&forged)
		_, err := reader.ResolveGenerationMedia(t.Context(), forged)
		// 形状非法（大写十六进制、空 id）在身份比对前就被校验拒绝；
		// 形状合法的伪造则倒在身份比对上。
		shapeRejected := name == "uppercase digest" || name == "empty revision"
		if shapeRejected && !errors.Is(err, creativeops.ErrValidation) || !shapeRejected && !errors.Is(err, llmgateway.ErrConflict) {
			t.Fatalf("%s accepted: %+v %v", name, forged, err)
		}
	}
	if _, err := reader.ResolveGenerationMedia(t.Context(), ref); err != nil {
		t.Fatal("honest reference rejected", err)
	}
}

func TestGenerationFactsUnknownUntilProbed(t *testing.T) {
	f := setup(t)
	_, imageRevision := uploadAsset(t, f, "窗边.png", "image/png", "image", sample(t, "sample.png"))
	_, audioRevision := uploadAsset(t, f, "旁白.mp3", "audio/mpeg", "audio", sample(t, "sample.mp3"))
	_, videoRevision := uploadAsset(t, f, "片段.mp4", "video/mp4", "video", sample(t, "sample.mp4"))
	for _, r := range []string{imageRevision, audioRevision, videoRevision} {
		grantGeneration(t, f.a, r)
	}
	reader := f.svc.GenerationReaderFor(f.a)

	imageRef := originalReference(t, f.db, "media-a", imageRevision)
	meta, err := reader.ResolveGenerationMedia(t.Context(), imageRef)
	if err != nil || meta.Width != nil || meta.Height != nil || meta.DurationMS != nil || meta.FrameRate != nil {
		t.Fatalf("facts must be unknown before probing: %+v %v", meta, err)
	}
	// 探测前先调度，报告 pending，随后 worker 填入事实。
	if status, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, imageRevision); err != nil || status.State != "pending" {
		t.Fatalf("schedule %+v %v", status, err)
	}
	f.drain(t, f.a)
	meta, err = reader.ResolveGenerationMedia(t.Context(), imageRef)
	if err != nil || meta.Width == nil || *meta.Width != 64 || *meta.Height != 48 || meta.DurationMS != nil || meta.FrameRate != nil {
		t.Fatalf("image facts after probe: %+v %v", meta, err)
	}

	audioRef := originalReference(t, f.db, "media-a", audioRevision)
	if _, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, audioRevision); err != nil {
		t.Fatal(err)
	}
	f.drain(t, f.a)
	meta, err = reader.ResolveGenerationMedia(t.Context(), audioRef)
	if err != nil || meta.DurationMS == nil || *meta.DurationMS <= 0 {
		t.Fatalf("audio duration unknown after probe: %+v %v", meta, err)
	}
	if meta.Width != nil || meta.Height != nil || meta.FrameRate != nil {
		t.Fatalf("audio must not carry visual facts: %+v", meta)
	}

	videoRef := originalReference(t, f.db, "media-a", videoRevision)
	if _, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, videoRevision); err != nil {
		t.Fatal(err)
	}
	f.drain(t, f.a)
	meta, err = reader.ResolveGenerationMedia(t.Context(), videoRef)
	if err != nil || meta.Width == nil || meta.Height == nil || meta.FrameRate == nil || meta.FrameRate.Numerator <= 0 || meta.FrameRate.Denominator <= 0 {
		t.Fatalf("video facts after probe: %+v %v", meta, err)
	}

	// 存下来的零是已知零；读出时绝不会变回未知。
	var videoBlob string
	if err := f.db.QueryRow(`SELECT blob_id FROM creative_content_objects WHERE account_id='media-a' AND content_revision_id=$1 AND role='original'`, videoRevision).Scan(&videoBlob); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_media_facts SET duration_ms=0 WHERE account_id='media-a' AND blob_id=$1`, videoBlob); err != nil {
		t.Fatal(err)
	}
	meta, err = reader.ResolveGenerationMedia(t.Context(), videoRef)
	if err != nil || meta.DurationMS == nil || *meta.DurationMS != 0 {
		t.Fatalf("known zero must round-trip: %+v %v", meta, err)
	}
}

func TestFactsProbeDeduplicatesAndRecoversAfterRestart(t *testing.T) {
	f := setup(t)
	_, revision := uploadAsset(t, f, "窗边.png", "image/png", "image", sample(t, "sample.png"))
	grantGeneration(t, f.a, revision)

	first, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || first.State != "pending" {
		t.Fatalf("first schedule %+v %v", first, err)
	}
	second, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || second.ProbeID != first.ProbeID || second.State != "pending" {
		t.Fatalf("dedup failed: %+v vs %+v %v", first, second, err)
	}
	if n := availableProbeJobs(t, f.db); n != 1 {
		t.Fatalf("expected one queued probe job, got %d", n)
	}
	// 调度与读取走同一套授权守卫。
	if err := creativecontent.RevokeGenerationReference(t.Context(), f.a, revision); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision); !errors.Is(err, creativecontent.ErrUsageDenied) {
		t.Fatalf("schedule after revoke accepted: %v", err)
	}
	if err := creativecontent.GrantGenerationReference(t.Context(), f.a, revision); err != nil {
		t.Fatal(err)
	}

	handler := probeHandler(t, f)
	// 存活租约属于别的 worker：投递延期到租约过期而不是报错，等待不
	// 消耗队列尝试次数，worker 死亡也不会困死任务。
	if _, err := f.db.Exec(`UPDATE creative_media_probes SET state='running',lease_until=now()+interval '5 minutes' WHERE id=$1`, first.ProbeID); err != nil {
		t.Fatal(err)
	}
	var deferred *jobs.DeferredError
	if err := handler(t.Context(), f.a, probeJob(t, first.ProbeID)); !errors.As(err, &deferred) || deferred.After <= 0 {
		t.Fatalf("live lease must defer, got %v", err)
	}
	if state, _, _ := probeRow(t, f.db, first.ProbeID); state != "running" {
		t.Fatal("live lease was disturbed", state)
	}
	// 租约已过（worker 死亡）：补投递领取恢复并完成。领取即在事务里
	// 持久化尝试计数，一次恢复领取对应 attempts=1。
	if _, err := f.db.Exec(`UPDATE creative_media_probes SET lease_until=now()-interval '1 second' WHERE id=$1`, first.ProbeID); err != nil {
		t.Fatal(err)
	}
	if err := handler(t.Context(), f.a, probeJob(t, first.ProbeID)); err != nil {
		t.Fatal(err)
	}
	state, attempts, pins := probeRow(t, f.db, first.ProbeID)
	if state != "succeeded" || attempts != 1 || pins != 0 {
		t.Fatalf("recovery state=%s attempts=%d pins=%d", state, attempts, pins)
	}
	if n := factsCount(t, f.db); n != 1 {
		t.Fatalf("facts rows=%d", n)
	}
	// 成功后的补投递是无操作，不是重新探测。
	if err := handler(t.Context(), f.a, probeJob(t, first.ProbeID)); err != nil {
		t.Fatal(err)
	}
	if state, _, _ := probeRow(t, f.db, first.ProbeID); state != "succeeded" {
		t.Fatal("redelivery disturbed terminal state")
	}
	if n := factsCount(t, f.db); n != 1 {
		t.Fatalf("facts rows after redelivery=%d", n)
	}
	// succeeded 对该对象版本是终局：不建新任务也不投递。
	beforeJobs := availableProbeJobs(t, f.db)
	third, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || third.ProbeID != first.ProbeID || third.State != "succeeded" {
		t.Fatalf("reschedule of succeeded: %+v %v", third, err)
	}
	if n := availableProbeJobs(t, f.db); n != beforeJobs {
		t.Fatalf("reschedule enqueued new probe jobs: %d -> %d", beforeJobs, n)
	}

	// worker 死亡（running 且租约已过）时旧投递不会仍停在队列里：它要么
	// 已被消费，要么被队列丢弃。先消费掉旧投递，再调度——补投递建立
	// 新的可用任务。
	if _, err := f.db.Exec(`UPDATE creative_jobs.river_job SET state='completed',finalized_at=now() WHERE state='available' AND args::text LIKE '%media.facts_probe%'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE creative_media_probes SET state='running',lease_until=now()-interval '1 second' WHERE id=$1`, first.ProbeID); err != nil {
		t.Fatal(err)
	}
	refreshed, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || refreshed.ProbeID != first.ProbeID || refreshed.State != "running" {
		t.Fatalf("dead-worker refresh: %+v %v", refreshed, err)
	}
	if n := availableProbeJobs(t, f.db); n != beforeJobs {
		t.Fatalf("dead worker did not get a refreshed delivery: %d -> %d", beforeJobs, n)
	}
	// 存活投递还在时，pending 的重复调度被队列按载荷去重，不再堆投递。
	if _, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision); err != nil {
		t.Fatal(err)
	}
	if n := availableProbeJobs(t, f.db); n != beforeJobs {
		t.Fatalf("duplicate schedule must not stack deliveries: %d -> %d", beforeJobs, n)
	}
}

// TestFactsProbeWorkerDefersForeignExtractorVersion 锁住滚动升级混布语义：
// 旧版本 worker 领到新提取器版本的任务时，既不写事实也不完结任务，
// 投递原地等待（不消耗队列尝试次数）；版本回到可解释范围后同一投递
// 正常完成，事实落在任务行自己的版本键下。
func TestFactsProbeWorkerDefersForeignExtractorVersion(t *testing.T) {
	f := setup(t)
	_, revision := uploadAsset(t, f, "窗边.png", "image/png", "image", sample(t, "sample.png"))
	grantGeneration(t, f.a, revision)
	status, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || status.State != "pending" {
		t.Fatalf("schedule %+v %v", status, err)
	}
	// 模拟混布：任务行已被新版本 worker 升到 facts-v2，本进程仍是 facts-v1。
	if _, err := f.db.Exec(`UPDATE creative_media_probes SET extractor_version='facts-v2' WHERE id=$1`, status.ProbeID); err != nil {
		t.Fatal(err)
	}
	handler := probeHandler(t, f)
	var deferred *jobs.DeferredError
	if err := handler(t.Context(), f.a, probeJob(t, status.ProbeID)); !errors.As(err, &deferred) || deferred.After <= 0 {
		t.Fatalf("foreign extractor version must defer, got %v", err)
	}
	state, attempts, pins := probeRow(t, f.db, status.ProbeID)
	// 行原样保留：pending、不计数；创建任务时的 pin 也留在原地——
	// 外版本 worker 连别的执行持有的 pin 都不该碰。
	if state != "pending" || attempts != 0 || pins != 1 {
		t.Fatalf("foreign version disturbed the task: state=%s attempts=%d pins=%d", state, attempts, pins)
	}
	if n := factsCount(t, f.db); n != 0 {
		t.Fatalf("foreign version must not write facts, got %d", n)
	}
	// 版本回到本进程可解释的范围：同一投递正常完成。
	if _, err := f.db.Exec(`UPDATE creative_media_probes SET extractor_version='facts-v1' WHERE id=$1`, status.ProbeID); err != nil {
		t.Fatal(err)
	}
	if err := handler(t.Context(), f.a, probeJob(t, status.ProbeID)); err != nil {
		t.Fatal(err)
	}
	state, attempts, _ = probeRow(t, f.db, status.ProbeID)
	if state != "succeeded" || attempts != 1 {
		t.Fatalf("same-version completion: state=%s attempts=%d", state, attempts)
	}
	var version string
	if err := f.db.QueryRow(`SELECT extractor_version FROM creative_media_facts`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != "facts-v1" {
		t.Fatalf("facts recorded under %q", version)
	}
}

// TestFactsProbeRedeliversAfterQueueDiscard 锁住队列侧恢复缺口：投递被
// 队列耗尽重试并丢弃后，探测行停在 pending；调度入口必须能补发新投递
// 并最终完成探测，而不是永远报告 pending。
func TestFactsProbeRedeliversAfterQueueDiscard(t *testing.T) {
	f := setup(t)
	_, revision := uploadAsset(t, f, "窗边.png", "image/png", "image", sample(t, "sample.png"))
	grantGeneration(t, f.a, revision)
	status, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || status.State != "pending" {
		t.Fatalf("schedule %+v %v", status, err)
	}
	if n := availableProbeJobs(t, f.db); n != 1 {
		t.Fatalf("expected one live delivery, got %d", n)
	}
	// 队列连续投递失败耗尽 MaxAttempts 后丢弃唯一投递。
	if _, err := f.db.Exec(`UPDATE creative_jobs.river_job SET state='discarded',finalized_at=now() WHERE state='available' AND args::text LIKE '%media.facts_probe%'`); err != nil {
		t.Fatal(err)
	}
	if n := availableProbeJobs(t, f.db); n != 0 {
		t.Fatalf("delivery not discarded: %d", n)
	}
	// 调度入口为 pending 任务补发新投递（同载荷在存活投递存在时会被
	// 队列去重跳过；投递已丢弃则建立新投递）。
	again, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || again.ProbeID != status.ProbeID || again.State != "pending" {
		t.Fatalf("redelivery schedule: %+v %v", again, err)
	}
	if n := availableProbeJobs(t, f.db); n != 1 {
		t.Fatalf("expected a fresh delivery after discard, got %d", n)
	}
	// 补发投递可以正常完成探测。
	f.drain(t, f.a)
	state, _, pins := probeRow(t, f.db, status.ProbeID)
	if state != "succeeded" || pins != 0 {
		t.Fatalf("probe after redelivery: state=%s pins=%d", state, pins)
	}
	if n := factsCount(t, f.db); n != 1 {
		t.Fatalf("facts after redelivery=%d", n)
	}
}

// TestFactsProbeAttemptBudgetExhaustsToTerminalFailed 用持续的对象读取
// 故障锁住内部预算：前两次失败回到 pending 且计数递增，第三次领取耗尽
// 预算进入终态失败——领取即记账，预算不会因为执行失败被绕过。
func TestFactsProbeAttemptBudgetExhaustsToTerminalFailed(t *testing.T) {
	f := setup(t)
	_, revision := uploadAsset(t, f, "窗边.png", "image/png", "image", sample(t, "sample.png"))
	grantGeneration(t, f.a, revision)
	status, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || status.State != "pending" {
		t.Fatalf("schedule %+v %v", status, err)
	}
	// blob 行保持 ready 且版本不变（领取的身份校验通过），物理对象被
	// 删除让执行阶段的读取持续失败——属瞬时故障。
	var key, version string
	if err := f.db.QueryRow(`SELECT b.object_key,b.object_version FROM creative_content_objects o
		JOIN creative_blobs b ON b.account_id=o.account_id AND b.id=o.blob_id
		WHERE o.account_id='media-a' AND o.content_revision_id=$1 AND o.role='original'`, revision).Scan(&key, &version); err != nil {
		t.Fatal(err)
	}
	if err := f.local.DeleteExact(t.Context(), key, version); err != nil {
		t.Fatal(err)
	}
	handler := probeHandler(t, f)
	for attempt := 1; attempt <= 2; attempt++ {
		if err := handler(t.Context(), f.a, probeJob(t, status.ProbeID)); err == nil {
			t.Fatalf("attempt #%d must surface the failure for queue backoff", attempt)
		}
		state, attempts, pins := probeRow(t, f.db, status.ProbeID)
		if state != "pending" || attempts != attempt || pins != 0 {
			t.Fatalf("retry #%d: state=%s attempts=%d pins=%d", attempt, state, attempts, pins)
		}
	}
	if err := handler(t.Context(), f.a, probeJob(t, status.ProbeID)); err != nil {
		t.Fatalf("terminal failure must be handled: %v", err)
	}
	state, attempts, pins := probeRow(t, f.db, status.ProbeID)
	if state != "failed" || attempts != 3 || pins != 0 {
		t.Fatalf("budget exhausted: state=%s attempts=%d pins=%d", state, attempts, pins)
	}
	if n := factsCount(t, f.db); n != 0 {
		t.Fatalf("failed probe must not write facts, got %d", n)
	}
	// 显式重置后重新计数；对象已不存在，恢复不了，这里只锁 attempts 归零。
	if _, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision); err != nil {
		t.Fatal(err)
	}
	if _, attempts, _ = probeRow(t, f.db, status.ProbeID); attempts != 0 {
		t.Fatalf("explicit reset must zero attempts, got %d", attempts)
	}
}

func TestFactsProbeReprobesUnderNewExtractorVersion(t *testing.T) {
	f := setup(t)
	_, revision := uploadAsset(t, f, "窗边.png", "image/png", "image", sample(t, "sample.png"))
	grantGeneration(t, f.a, revision)
	if _, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision); err != nil {
		t.Fatal(err)
	}
	f.drain(t, f.a)
	// 模拟历史：对象已在更老的提取器版本下探测过，且该任务已终态。
	if _, err := f.db.Exec(`UPDATE creative_media_probes SET extractor_version='facts-v0'; UPDATE creative_media_facts SET extractor_version='facts-v0'`); err != nil {
		t.Fatal(err)
	}
	ref := originalReference(t, f.db, "media-a", revision)
	if meta, err := f.svc.GenerationReaderFor(f.a).ResolveGenerationMedia(t.Context(), ref); err != nil || meta.Width != nil {
		t.Fatalf("only the current extractor's facts count: %+v %v", meta, err)
	}
	status, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || status.State != "pending" {
		t.Fatalf("new extractor version must re-probe: %+v %v", status, err)
	}
	f.drain(t, f.a)
	var versions []string
	rows, err := f.db.Query(`SELECT extractor_version FROM creative_media_facts ORDER BY extractor_version`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var v string
		_ = rows.Scan(&v)
		versions = append(versions, v)
	}
	_ = rows.Close()
	if len(versions) != 2 || versions[0] != "facts-v0" || versions[1] != "facts-v1" {
		t.Fatalf("expected facts under both extractor versions, got %v", versions)
	}
	meta, err := f.svc.GenerationReaderFor(f.a).ResolveGenerationMedia(t.Context(), ref)
	if err != nil || meta.Width == nil || *meta.Width != 64 {
		t.Fatalf("current version facts after re-probe: %+v %v", meta, err)
	}
}

func TestGenerationReferenceReadsOriginalRendition(t *testing.T) {
	f := setup(t)
	// 比显示边的阈值更大，上传会派生出 display blob。
	var big bytes.Buffer
	if err := png.Encode(&big, image.NewRGBA(image.Rect(0, 0, 1800, 1200))); err != nil {
		t.Fatal(err)
	}
	_, revision := uploadAsset(t, f, "大图.png", "image/png", "image", big.Bytes())
	grantGeneration(t, f.a, revision)

	var renditions int
	if err := f.db.QueryRow(`SELECT count(*) FROM creative_content_objects WHERE account_id='media-a' AND content_revision_id=$1`, revision).Scan(&renditions); err != nil || renditions != 2 {
		t.Fatalf("expected original+display renditions, got %d %v", renditions, err)
	}
	ref := originalReference(t, f.db, "media-a", revision)
	if ref.MIME != "image/png" {
		t.Fatalf("original must be the png, got %s", ref.MIME)
	}
	if _, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision); err != nil {
		t.Fatal(err)
	}
	f.drain(t, f.a)
	meta, err := f.svc.GenerationReaderFor(f.a).ResolveGenerationMedia(t.Context(), ref)
	if err != nil || meta.Width == nil || *meta.Width != 1800 || *meta.Height != 1200 {
		t.Fatalf("original rendition facts: %+v %v", meta, err)
	}
	// display rendition（jpeg、更小）绝不能是事实描述的对象。
	var displayBytes int64
	if err := f.db.QueryRow(`SELECT b.byte_size FROM creative_content_objects o JOIN creative_blobs b ON b.account_id=o.account_id AND b.id=o.blob_id WHERE o.account_id='media-a' AND o.content_revision_id=$1 AND o.role='display'`, revision).Scan(&displayBytes); err != nil {
		t.Fatal(err)
	}
	if displayBytes == ref.ByteSize {
		t.Fatal("display rendition unexpectedly identical to original")
	}
}

func TestFactsProbeUnsupportedWithoutFFProbeIsTerminal(t *testing.T) {
	f := setup(t)
	_, revision := uploadAsset(t, f, "旁白.mp3", "audio/mpeg", "audio", sample(t, "sample.mp3"))
	grantGeneration(t, f.a, revision)

	noAV := f.cfg
	noAV.FFProbe = ""
	probeless, err := creativemedia.NewService(noAV, f.local, creativemedia.NewVerifier(noAV), f.key, "/api/v1/creative/media")
	if err != nil {
		t.Fatal(err)
	}
	status, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || status.State != "pending" {
		t.Fatalf("schedule %+v %v", status, err)
	}
	var handler func(context.Context, store.AccountScope, jobs.Request) error
	for _, h := range probeless.Handlers() {
		if h.Kind == "media.facts_probe" {
			handler = h.Work
		}
	}
	if err := handler(t.Context(), f.a, probeJob(t, status.ProbeID)); err != nil {
		t.Fatal(err)
	}
	state, _, pins := probeRow(t, f.db, status.ProbeID)
	if state != "unsupported" || pins != 0 {
		t.Fatalf("unsupported terminal state=%s pins=%d", state, pins)
	}
	if n := factsCount(t, f.db); n != 0 {
		t.Fatalf("unsupported probe must not write facts, got %d", n)
	}
	// 读取侧继续报告未知，而不是捏造事实。
	meta, err := f.svc.GenerationReaderFor(f.a).ResolveGenerationMedia(t.Context(), originalReference(t, f.db, "media-a", revision))
	if err != nil || meta.DurationMS != nil {
		t.Fatalf("unsupported object must stay unknown: %+v %v", meta, err)
	}
	// 重新调度 unsupported 探测绝不重试。
	beforeJobs := availableProbeJobs(t, f.db)
	again, err := f.svc.ScheduleFactsProbe(t.Context(), f.a, revision)
	if err != nil || again.ProbeID != status.ProbeID || again.State != "unsupported" {
		t.Fatalf("unsupported rescheduled: %+v %v", again, err)
	}
	if n := availableProbeJobs(t, f.db); n != beforeJobs {
		t.Fatalf("unsupported probe re-enqueued: %d -> %d", beforeJobs, n)
	}
}
