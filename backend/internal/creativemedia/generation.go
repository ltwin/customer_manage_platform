package creativemedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativegraph"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/jobs"
	"github.com/samson/customer-manage-platform/backend/internal/platform/llmgateway"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

const (
	jobFactsProbe = "media.facts_probe"
	// FactsExtractorVersion 绑定探测语义：方向修正后的图片尺寸、毫秒级
	// 取整的时长、精确有理数帧率。任何语义变化必须升版本；新版本的事实
	// 落在自己的键下，从不改写旧版本。
	FactsExtractorVersion = "facts-v1"
	factsSchemaVersion    = 1
	// 租约须覆盖 Handlers 里登记的任务尝试超时，领取方才不会把仍在探测
	// 的存活 worker 误判为死亡。
	factsProbeLease = 10 * time.Minute
	// probeWakeSlack 把等租约的延期再垫过租约终点一点，补投递不会在
	// 租约仍读作存活的瞬间落到行上。
	probeWakeSlack = 5 * time.Second
	// factsProbeVersionWait 是 worker 遇到非本版本探测任务时的等待窗口：
	// 滚动升级混布期间，让投递原地等待而不消耗队列尝试次数，直到能解释
	// 该版本的 worker 接手。
	factsProbeVersionWait = time.Minute
	// factsProbeFinishBudget 是回写收尾的独立短超时：整体执行超时后原
	// ctx 已失效，失败记账与 pin 释放仍必须在这个预算内落库。
	factsProbeFinishBudget = 30 * time.Second
	factsProbeMaxTries     = 3
	factsProbePinOwner     = "processing"
)

// generationReader 把一个服务端绑定的账户 scope 适配成 Gateway 的可信
// 媒体端口。它绝不接受客户端提供的账户号、对象位置或探测结果；授权、
// 身份与事实只来自该 scope 之下的存储层。
type generationReader struct {
	svc   *Service
	scope store.AccountScope
}

// GenerationReaderFor 为一个账户构建可信的生成参考媒体读取器。组合根在
// 预检/准入入口注入它；它不是公开工具，也不携带派发权限。
func (s *Service) GenerationReaderFor(scope store.AccountScope) llmgateway.GenerationMediaReader {
	return &generationReader{svc: s, scope: scope}
}

// ResolveGenerationMedia 把引用对照该修订的 original rendition 校验后返回
// 版本化的事实。身份字段（摘要、MIME、大小、类型）永远取自已校验的
// blob 行；度量字段只有当事实行存在于「精确对象版本 + 当前提取器」键下
// 才会上报，否则保持 nil（未知），绝不是零。摘要不匹配、MIME/大小/类型
// 不符或跨账户修订都会被拒绝；客户端声称的元数据永远不能替代存储事实。
func (r *generationReader) ResolveGenerationMedia(ctx context.Context, ref llmgateway.GenerationReference) (llmgateway.GenerationMediaMetadata, error) {
	digest, err := hex.DecodeString(ref.Digest)
	if err != nil || len(digest) != 32 || strings.ToLower(ref.Digest) != ref.Digest ||
		ref.RevisionID == "" || len(ref.RevisionID) > 256 || ref.Role == "" || len(ref.Role) > 256 ||
		ref.ByteSize <= 0 || len(ref.MIME) < 3 || len(ref.MIME) > 120 ||
		(ref.Kind != llmgateway.GenerationImage && ref.Kind != llmgateway.GenerationVideo && ref.Kind != llmgateway.GenerationAudio) {
		return llmgateway.GenerationMediaMetadata{}, creativeops.ErrValidation
	}
	var meta llmgateway.GenerationMediaMetadata
	err = r.scope.WithReadSnapshot(ctx, func(tx store.ReadTxAccountScope) error {
		if err := tx.RequireCreativeRead(ctx); err != nil {
			return err
		}
		rev, err := creativecontent.ReadGenerationReferenceSnapshot(ctx, tx, ref.RevisionID)
		if err != nil {
			return err
		}
		if rev.Kind != string(ref.Kind) {
			return fmt.Errorf("%w: generation reference kind", llmgateway.ErrConflict)
		}
		// 生成参考永远读 original rendition。first_frame 之类的模型输入
		// 角色不是存储 rendition 角色。
		blobID := ""
		for _, m := range rev.Media {
			if m.Role == "original" {
				blobID = m.BlobID
				break
			}
		}
		if blobID == "" {
			return creativecontent.ErrNotFound
		}
		var state, objectVersion, sha256, mime string
		var byteSize int64
		err = tx.QueryRow(ctx, "creative_blobs", "state,object_version,sha256,byte_size,mime", "id=$2", blobID).Scan(&state, &objectVersion, &sha256, &byteSize, &mime)
		if errors.Is(err, store.ErrNoRows) {
			return creativecontent.ErrNotFound
		}
		if err != nil {
			return err
		}
		if state != "ready" || objectVersion == "" {
			return creativecontent.ErrNotFound
		}
		stored, ok := strings.CutPrefix(sha256, "sha256-")
		if !ok || stored != ref.Digest || mime != ref.MIME || byteSize != ref.ByteSize {
			return fmt.Errorf("%w: generation reference identity", llmgateway.ErrConflict)
		}
		meta = llmgateway.GenerationMediaMetadata{
			RevisionID:       ref.RevisionID,
			Digest:           ref.Digest,
			ExtractorVersion: FactsExtractorVersion,
			Kind:             ref.Kind,
			MIME:             mime,
			ByteSize:         byteSize,
		}
		var width, height *int
		var durationMs *int64
		var frameNum, frameDen *int64
		var rowDigest, rowKind, rowMime string
		var rowSize int64
		err = tx.QueryRow(ctx, "creative_media_facts", "digest,kind,mime,byte_size,width,height,duration_ms,frame_rate_num,frame_rate_den",
			"blob_id=$2 AND object_version=$3 AND extractor_version=$4", blobID, objectVersion, FactsExtractorVersion).
			Scan(&rowDigest, &rowKind, &rowMime, &rowSize, &width, &height, &durationMs, &frameNum, &frameDen)
		if errors.Is(err, store.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if rowDigest != sha256 || rowKind != rev.Kind || rowMime != mime || rowSize != byteSize {
			return fmt.Errorf("%w: generation facts identity", llmgateway.ErrConflict)
		}
		if width != nil {
			meta.Width = new(int64)
			*meta.Width = int64(*width)
		}
		if height != nil {
			meta.Height = new(int64)
			*meta.Height = int64(*height)
		}
		if durationMs != nil {
			meta.DurationMS = new(int64)
			*meta.DurationMS = *durationMs
		}
		if frameNum != nil && frameDen != nil {
			meta.FrameRate = &llmgateway.GenerationRatio{Numerator: *frameNum, Denominator: *frameDen}
		}
		return nil
	})
	if err != nil {
		return llmgateway.GenerationMediaMetadata{}, err
	}
	return meta, nil
}

// FactsProbeStatus 报告一个精确对象版本下去重后的探测任务；State 是任务
// 状态机的取值。
type FactsProbeStatus struct {
	ProbeID string `json:"probe_id"`
	State   string `json:"state"`
}

// ScheduleFactsProbe 为修订的 original 对象版本在当前提取器版本下登记一个
// 去重的探测任务。登记时重查授权；任务行、短期读 pin 与队列投递在同一
// 事务里创建。已存活的任务不会被重复建立；队列按（账户、类型、载荷）
// 对投递去重，唯一投递被队列侧耗尽丢弃后，本入口的再次调用会补发新
// 投递。终态 failed 只被这个显式调用重置（后台循环永不重置）；succeeded
// 与 unsupported 对该提取器版本是终局——语义版本升级会在自己的键下重新
// 探测。
func (s *Service) ScheduleFactsProbe(ctx context.Context, scope store.AccountScope, contentRevisionID string) (FactsProbeStatus, error) {
	var result FactsProbeStatus
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		rev, err := creativecontent.RequireGenerationReference(ctx, tx, contentRevisionID)
		if err != nil {
			return err
		}
		blobID := ""
		for _, m := range rev.Media {
			if m.Role == "original" {
				blobID = m.BlobID
				break
			}
		}
		if blobID == "" {
			return creativecontent.ErrNotFound
		}
		var state, objectVersion string
		if err := tx.QueryRow(ctx, "creative_blobs", "state,object_version", "id=$2", blobID).Scan(&state, &objectVersion); err != nil {
			return err
		}
		if state != "ready" || objectVersion == "" {
			return creativecontent.ErrNotFound
		}
		var probeID, probeState string
		var retryRound int64
		var pinID *string
		var lease *time.Time
		err = tx.QueryRowForUpdate(ctx, "creative_media_probes", "id,state,lease_until,read_pin_id,retry_round", "blob_id=$2 AND object_version=$3 AND extractor_version=$4", blobID, objectVersion, FactsExtractorVersion).Scan(&probeID, &probeState, &lease, &pinID, &retryRound)
		now, err2 := tx.CreativeNow(ctx)
		if err2 != nil {
			return err2
		}
		// create 在这个键下登记任务。并发登记可能赢走唯一键；输方删掉自己
		// 未用的 pin、报告赢方且不另行入队——赢方自带投递。
		create := func() (won bool, err error) {
			candidate := "ccmp_" + uuid.NewString()
			pin, err := s.registerProbePin(ctx, tx, blobID, "", now)
			if err != nil {
				return false, err
			}
			err = tx.InsertOnConflictDoNothingReturning(ctx, "creative_media_probes",
				[]string{"id", "blob_id", "object_version", "extractor_version", "state", "read_pin_id"},
				[]string{"account_id", "blob_id", "object_version", "extractor_version"}, []string{"id"},
				candidate, blobID, objectVersion, FactsExtractorVersion, "pending", pin).Scan(&probeID)
			if errors.Is(err, store.ErrNoRows) {
				if _, derr := tx.Delete(ctx, "creative_blob_read_pins", "id=$2", pin); derr != nil {
					return false, derr
				}
				if err := tx.QueryRowForUpdate(ctx, "creative_media_probes", "id,state", "blob_id=$2 AND object_version=$3 AND extractor_version=$4", blobID, objectVersion, FactsExtractorVersion).Scan(&probeID, &probeState); err != nil {
					return false, err
				}
				return false, nil
			}
			if err != nil {
				return false, err
			}
			probeState = "pending"
			return true, nil
		}
		switch {
		case errors.Is(err, store.ErrNoRows):
			won, err := create()
			if err != nil {
				return err
			}
			if !won {
				result = FactsProbeStatus{ProbeID: probeID, State: probeState}
				return nil
			}
		case err != nil:
			return err
		case probeState == "failed":
			// 只有显式重试；后台循环永不重置 failed 探测。
			replace := ""
			if pinID != nil {
				replace = *pinID
			}
			pin, err := s.registerProbePin(ctx, tx, blobID, replace, now)
			if err != nil {
				return err
			}
			if _, err := tx.Update(ctx, "creative_media_probes", "state='pending',attempts=0,retry_round=retry_round+1,execution_epoch=execution_epoch+1,lease_until=NULL,read_pin_id=$2,error_code='',completed_at=NULL,updated_at=clock_timestamp()", "id=$3", pin, probeID); err != nil {
				return err
			}
			retryRound++
			probeState = "pending"
		case probeState == "pending" || probeState == "running" && (lease == nil || !now.Before(*lease)):
			// pending：若存活投递仍在，队列按载荷去重会把重复入队跳过；
			// 若唯一投递已被队列侧耗尽丢弃，这里补发新投递。running 且租约
			// 已过意味着 worker 已死，投递可能随它一起消失，同样补发。
		default:
			// 存活 running、终态：照实报告。
			result = FactsProbeStatus{ProbeID: probeID, State: probeState}
			return nil
		}
		if err := s.enqueueFactsProbe(ctx, tx, now, probeID, retryRound); err != nil {
			return err
		}
		result = FactsProbeStatus{ProbeID: probeID, State: probeState}
		return nil
	})
	if err != nil {
		return FactsProbeStatus{}, err
	}
	return result, nil
}

// registerProbePin 用一个覆盖租约窗口的新 pin 替换该探测此前的 pin；
// 只释放探测自己的 pin，绝不碰其他执行的保留。
func (s *Service) registerProbePin(ctx context.Context, tx store.TxAccountScope, blobID, oldPin string, now time.Time) (string, error) {
	if oldPin != "" {
		if _, err := tx.Delete(ctx, "creative_blob_read_pins", "id=$2", oldPin); err != nil {
			return "", err
		}
	}
	pin := "ccpn_" + uuid.NewString()
	if err := tx.Insert(ctx, "creative_blob_read_pins", []string{"id", "blob_id", "owner_kind", "expires_at"}, pin, blobID, factsProbePinOwner, now.Add(factsProbeLease)); err != nil {
		return "", err
	}
	return pin, nil
}

func (s *Service) enqueueFactsProbe(ctx context.Context, tx store.TxAccountScope, at time.Time, probeID string, retryRound int64) error {
	if s.runtime == nil {
		return jobs.ErrNoHandlers
	}
	// 持久效果由探测行状态机去重；每次派发带自己的操作身份。投递本身
	// 请求队列按探测 ID 与重试轮次去重，上一轮尚在收尾的投递不占新轮的键。
	_, err := jobs.EnqueueInTx(ctx, tx.Jobs(s.runtime), jobs.Request{Kind: jobFactsProbe, OperationID: uuid.NewString(), CreatedAt: at, Payload: creativegraph.Canonical(factsProbeJob{ProbeID: probeID, RetryRound: retryRound}), UniqueByArgs: true})
	return err
}

// factsProbeClaim 是被领取的任务加上回写时要复核的 pin 住的对象身份。
type factsProbeClaim struct {
	ProbeID          string
	BlobID           string
	ObjectVersion    string
	ExtractorVersion string
	ObjectKey        string
	SHA256           string
	Mime             string
	ByteSize         int64
	Epoch            int64
	RetryRound       int64
	// Attempts 是本次执行在领取事务里持久化后的尝试序号（第几次领取）。
	Attempts int
}

type factsProbeJob struct {
	ProbeID string `json:"probe_id"`
	// 缺失轮次的旧载荷只代表初始轮，绝不代表当前轮。
	RetryRound int64 `json:"retry_round,omitempty"`
}

// workFactsProbe 执行一次有界探测：事务内领取，事务外读取并探测对象，
// 然后在写入不可变事实行之前复核 epoch、对象版本与摘要。任何数据库事务
// 都不跨对象读取持有。执行阶段（读取与探测）使用执行 ctx；各回写函数
// 内部自行改用独立短超时 ctx——整体执行超时后原 ctx 已失效，失败记账与
// pin 释放仍必须落库。
func (s *Service) workFactsProbe(ctx context.Context, scope store.AccountScope, job jobs.Request) error {
	var p factsProbeJob
	if err := creativeops.Decode(job.Payload, &p); err != nil || p.ProbeID == "" || p.RetryRound < 0 {
		return creativeops.ErrValidation
	}
	claim, ok, err := s.claimFactsProbe(ctx, scope, p.ProbeID, p.RetryRound)
	if err != nil || !ok {
		return err
	}
	body, _, err := s.adapter.OpenVersion(ctx, claim.ObjectKey, claim.ObjectVersion, nil)
	if err != nil {
		return s.retryFactsProbe(ctx, scope, claim, "open_failed", err)
	}
	facts, err := s.verifier.Probe(ctx, body, s.verifier.MaxBytes(mimeKind(claim.Mime)))
	// 暂存副本已拿到它需要的一切；这里的关闭错误没有剩余动作可做。
	_ = body.Close()
	if errors.Is(err, ErrUnsupported) {
		// 没有 ffprobe，或该格式的流布局不可用：上报不可用，绝不在永久性
		// 条件上循环。
		s.logger.Warn("creative media facts probe unsupported",
			slog.String("probe_id", claim.ProbeID), slog.String("account_id", scope.AccountID()), slog.String("kind", mimeKind(claim.Mime)))
		return s.finishFactsProbe(ctx, scope, claim, "unsupported", "media_unsupported")
	}
	if err != nil {
		return s.retryFactsProbe(ctx, scope, claim, "probe_failed", err)
	}
	if facts.SHA256 != claim.SHA256 || facts.Mime != claim.Mime || facts.Size != claim.ByteSize {
		return s.finishFactsProbe(ctx, scope, claim, "failed", "identity_mismatch")
	}
	return s.completeFactsProbe(ctx, scope, claim, facts)
}

// probeFinishCtx 把回写用的 ctx 与执行 ctx 的取消解绑，只保留独立短超时：
// 整体执行超时后原 ctx 已失效，失败记账、终态翻转与 pin 释放仍必须落库。
func probeFinishCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), factsProbeFinishBudget)
}

// claimFactsProbe 取得执行权：任务必须 pending，或 running 且租约已过
// （重启恢复）。存活租约属于别的 worker。领取即把本次尝试计数持久化
// （attempts+1 与状态翻转同一事务），worker 中途死亡也不会丢预算。blob 被
// 锁定并随领取换新 pin；对象不再匹配任务的精确版本则任务终态失败。
// 非本提取器版本的任务（滚动升级混布）不由本 worker 触碰：投递原地等待
// 而不消耗队列尝试次数。
func (s *Service) claimFactsProbe(ctx context.Context, scope store.AccountScope, probeID string, retryRound int64) (factsProbeClaim, bool, error) {
	var claim factsProbeClaim
	var waitOutLease time.Duration
	waitForeignVersion := false
	claimed := false
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		var state string
		var epoch int64
		var lease *time.Time
		var pinID *string
		err := tx.QueryRowForUpdate(ctx, "creative_media_probes", "blob_id,object_version,extractor_version,state,attempts,execution_epoch,lease_until,read_pin_id,retry_round", "id=$2", probeID).
			Scan(&claim.BlobID, &claim.ObjectVersion, &claim.ExtractorVersion, &state, &claim.Attempts, &epoch, &lease, &pinID, &claim.RetryRound)
		if errors.Is(err, store.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		claim.ProbeID = probeID
		// 终态与失效轮次均无需执行，不受当前 worker 提取器版本限制。
		if state == "succeeded" || state == "failed" || state == "unsupported" || claim.RetryRound != retryRound {
			return nil
		}
		if claim.ExtractorVersion != FactsExtractorVersion {
			// 本 worker 无法解释该版本的事实语义：既不能写事实，也不能把
			// 任务标成任何终态。行保持原样，投递等待新版本 worker。
			waitForeignVersion = true
			return nil
		}
		now, err := tx.CreativeNow(ctx)
		if err != nil {
			return err
		}
		switch {
		case state == "running" && lease != nil && now.Before(*lease):
			// 存活租约属于别的 worker。用延期而不是报错：等租约不该消耗
			// 队列尝试次数，否则 worker 死亡会把任务困死。
			waitOutLease = lease.Sub(now) + probeWakeSlack
			return nil
		case state != "pending" && state != "running":
			return ErrState
		}
		if claim.Attempts >= factsProbeMaxTries {
			// 已过租约的执行可能没有机会收尾；封顶时不再读取对象，
			// 同一事务推进 epoch 并释放 pin，阻止迟到 worker 覆盖终态。
			claim.Epoch = epoch + 1
			if _, err := tx.Update(ctx, "creative_media_probes", "execution_epoch=$2,updated_at=clock_timestamp()", "id=$3", claim.Epoch, probeID); err != nil {
				return err
			}
			return s.finishFactsProbeInTx(ctx, tx, claim, "failed", "attempts_exhausted")
		}
		var blobState, blobVersion string
		err = tx.QueryRowForUpdate(ctx, "creative_blobs", "state,object_key,object_version,sha256,byte_size,mime", "id=$2", claim.BlobID).
			Scan(&blobState, &claim.ObjectKey, &blobVersion, &claim.SHA256, &claim.ByteSize, &claim.Mime)
		if errors.Is(err, store.ErrNoRows) {
			blobState = "gone"
			blobVersion = claim.ObjectVersion
			err = nil
		}
		if err != nil {
			return err
		}
		if blobState != "ready" || blobVersion != claim.ObjectVersion {
			// 任务创建时的精确对象版本已不存在；取走 epoch，让收尾记在
			// 本次领取名下。
			claim.Epoch = epoch + 1
			if _, err := tx.Update(ctx, "creative_media_probes", "execution_epoch=$2,updated_at=clock_timestamp()", "id=$3", claim.Epoch, probeID); err != nil {
				return err
			}
			return s.finishFactsProbeInTx(ctx, tx, claim, "failed", "object_changed")
		}
		claim.Epoch = epoch + 1
		var oldPin string
		if pinID != nil {
			oldPin = *pinID
		}
		pin, err := s.registerProbePin(ctx, tx, claim.BlobID, oldPin, now)
		if err != nil {
			return err
		}
		// 尝试计数在领取事务里持久化：即使 worker 在探测中途整体超时或
		// 死亡，这次执行也已经花掉了一份内部预算。
		if _, err := tx.Update(ctx, "creative_media_probes", "state='running',attempts=attempts+1,execution_epoch=$2,lease_until=$3,read_pin_id=$4,updated_at=clock_timestamp()", "id=$5", claim.Epoch, now.Add(factsProbeLease), pin, probeID); err != nil {
			return err
		}
		claim.Attempts++
		claimed = true
		return nil
	})
	if err != nil {
		return claim, false, err
	}
	if waitForeignVersion {
		return claim, false, jobs.Defer(factsProbeVersionWait)
	}
	if waitOutLease > 0 {
		return claim, false, jobs.Defer(waitOutLease)
	}
	return claim, claimed, nil
}

// completeFactsProbe 写入不可变事实行并完结任务。对象版本与摘要在探测
// 自己的 epoch 下复核；对象变了就失败任务，而不是为别的字节记录事实。
// 事实落在任务行携带的提取器版本键下（版本门禁已在领取处拦下不匹配的
// 任务，这里再取行上版本是同一约束的回写侧）。
func (s *Service) completeFactsProbe(ctx context.Context, scope store.AccountScope, claim factsProbeClaim, facts ProbedFacts) error {
	ctx, cancel := probeFinishCtx(ctx)
	defer cancel()
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		if err := s.requireProbeOwnership(ctx, tx, claim); err != nil {
			return err
		}
		var blobState, blobVersion, sha string
		var size int64
		var mime string
		if err := tx.QueryRowForUpdate(ctx, "creative_blobs", "state,object_version,sha256,byte_size,mime", "id=$2", claim.BlobID).Scan(&blobState, &blobVersion, &sha, &size, &mime); err != nil {
			return err
		}
		if blobState != "ready" || blobVersion != claim.ObjectVersion || sha != claim.SHA256 || size != claim.ByteSize || mime != claim.Mime {
			return s.finishFactsProbeInTx(ctx, tx, claim, "failed", "object_changed")
		}
		exists, err := tx.Exists(ctx, "creative_media_facts", "blob_id=$2 AND object_version=$3 AND extractor_version=$4", claim.BlobID, claim.ObjectVersion, claim.ExtractorVersion)
		if err != nil {
			return err
		}
		if !exists {
			var frameNum, frameDen any
			if facts.FrameRateNum > 0 && facts.FrameRateDen > 0 {
				frameNum, frameDen = facts.FrameRateNum, facts.FrameRateDen
			}
			now, err := tx.CreativeNow(ctx)
			if err != nil {
				return err
			}
			if err := tx.Insert(ctx, "creative_media_facts", []string{"id", "blob_id", "object_version", "digest", "extractor_version", "facts_schema_version", "kind", "mime", "byte_size", "width", "height", "dimension_basis", "duration_ms", "frame_rate_num", "frame_rate_den", "probed_at", "facts_sha256"},
				"ccmf_"+uuid.NewString(), claim.BlobID, claim.ObjectVersion, facts.SHA256, claim.ExtractorVersion, factsSchemaVersion, facts.Kind, facts.Mime, facts.Size, facts.Width, facts.Height, facts.DimensionBasis, facts.DurationMs, frameNum, frameDen, now, factsDigest(claim.ExtractorVersion, facts)); err != nil {
				return err
			}
		}
		return s.finishFactsProbeInTx(ctx, tx, claim, "succeeded", "")
	})
}

// retryFactsProbe 把任务放回有界重试的 pending，预算耗尽则终态失败。
// 尝试计数已在领取时持久化，这里不再自增；若这次回写本身也失败（进程
// 死亡），行停在 running 直到租约过期，由补投递再次领取——预算已在
// 领取时扣除。两种结局都释放探测自己的 pin，其他执行的保留不受影响。
func (s *Service) retryFactsProbe(ctx context.Context, scope store.AccountScope, claim factsProbeClaim, code string, cause error) error {
	ctx, cancel := probeFinishCtx(ctx)
	defer cancel()
	terminal := claim.Attempts >= factsProbeMaxTries
	err := scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		if terminal {
			return s.finishFactsProbeInTx(ctx, tx, claim, "failed", code)
		}
		if err := s.releaseProbePin(ctx, tx, claim); err != nil {
			return err
		}
		n, err := tx.Update(ctx, "creative_media_probes", "state='pending',lease_until=NULL,read_pin_id=NULL,updated_at=clock_timestamp()", "id=$2 AND execution_epoch=$3 AND retry_round=$4 AND state='running'", claim.ProbeID, claim.Epoch, claim.RetryRound)
		if err != nil {
			return err
		}
		if n == 0 {
			return ErrEpoch
		}
		return nil
	})
	if err != nil {
		return err
	}
	if terminal {
		s.logger.Warn("creative media facts probe failed terminally",
			slog.String("probe_id", claim.ProbeID), slog.String("account_id", scope.AccountID()), slog.String("code", code), slog.String("error", cause.Error()))
		return nil
	}
	// 返回原因，让队列在下一次有界尝试前退避。
	return fmt.Errorf("creative media facts probe retry (%s): %w", code, cause)
}

// finishFactsProbe 把任务移入终态并释放它的 pin。
func (s *Service) finishFactsProbe(ctx context.Context, scope store.AccountScope, claim factsProbeClaim, state, code string) error {
	ctx, cancel := probeFinishCtx(ctx)
	defer cancel()
	return scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := tx.RequireCreativeCapability(ctx, "creative_read"); err != nil {
			return err
		}
		return s.finishFactsProbeInTx(ctx, tx, claim, state, code)
	})
}

func (s *Service) finishFactsProbeInTx(ctx context.Context, tx store.TxAccountScope, claim factsProbeClaim, state, code string) error {
	if err := s.releaseProbePin(ctx, tx, claim); err != nil {
		return err
	}
	// epoch 守卫是所有权证明：过期 worker 持旧 epoch，无法完结已被更新
	// 领取接手的任务。pin 引用随 pin 一起清空，终态行不悬垂任何引用。
	n, err := tx.Update(ctx, "creative_media_probes", "state=$2,error_code=$3,lease_until=NULL,read_pin_id=NULL,completed_at=clock_timestamp(),updated_at=clock_timestamp()", "id=$4 AND execution_epoch=$5 AND retry_round=$6 AND state IN ('pending','running')", state, code, claim.ProbeID, claim.Epoch, claim.RetryRound)
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrEpoch
	}
	return nil
}

// requireProbeOwnership 在回写前复核该 worker 仍持有任务：running 状态加
// 被领取时的 epoch。
func (s *Service) requireProbeOwnership(ctx context.Context, tx store.TxAccountScope, claim factsProbeClaim) error {
	var state string
	var epoch, retryRound int64
	if err := tx.QueryRowForUpdate(ctx, "creative_media_probes", "state,execution_epoch,retry_round", "id=$2", claim.ProbeID).Scan(&state, &epoch, &retryRound); err != nil {
		return err
	}
	if state != "running" || epoch != claim.Epoch || retryRound != claim.RetryRound {
		return ErrEpoch
	}
	return nil
}

// releaseProbePin 只删除该探测任务上当前记录的那个 pin。
func (s *Service) releaseProbePin(ctx context.Context, tx store.TxAccountScope, claim factsProbeClaim) error {
	var pinID *string
	var state string
	var epoch, retryRound int64
	if err := tx.QueryRowForUpdate(ctx, "creative_media_probes", "read_pin_id,state,execution_epoch,retry_round", "id=$2", claim.ProbeID).Scan(&pinID, &state, &epoch, &retryRound); err != nil {
		return err
	}
	if (state != "pending" && state != "running") || epoch != claim.Epoch || retryRound != claim.RetryRound {
		return ErrEpoch
	}
	if pinID == nil {
		return nil
	}
	if _, err := tx.Delete(ctx, "creative_blob_read_pins", "id=$2", *pinID); err != nil {
		return err
	}
	return nil
}

// mimeKind 从已校验的 blob MIME 推导存储模态。
func mimeKind(mime string) string {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return "image"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "audio/"):
		return "audio"
	}
	return ""
}

// factsDigest 对规范化的事实字段做哈希；同一身份、提取器与语义必须永远
// 得出同一摘要。
func factsDigest(extractorVersion string, f ProbedFacts) string {
	var frameNum, frameDen int64
	if f.FrameRateNum > 0 && f.FrameRateDen > 0 {
		frameNum, frameDen = f.FrameRateNum, f.FrameRateDen
	}
	raw := creativegraph.Canonical(map[string]any{
		"extractor_version": extractorVersion,
		"schema_version":    factsSchemaVersion,
		"kind":              f.Kind,
		"mime":              f.Mime,
		"byte_size":         f.Size,
		"width":             f.Width,
		"height":            f.Height,
		"dimension_basis":   f.DimensionBasis,
		"duration_ms":       f.DurationMs,
		"frame_rate_num":    frameNum,
		"frame_rate_den":    frameDen,
	})
	sum := sha256.Sum256(raw)
	return "sha256-" + hex.EncodeToString(sum[:])
}
