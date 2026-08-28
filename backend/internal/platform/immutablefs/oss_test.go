package immutablefs

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
)

// fakeOSSAPI 模拟 *oss.Client 的对象语义：x-oss-meta-* 元数据随对象存取、
// ListObjectsV2 以 continuation token 接续（token 编码了 after-key）。
type fakeOSSAPI struct {
	mu               sync.Mutex
	objects          map[string]fakeObject
	putConflictCalls int              // 前 N 次 PutObject 返回 409 冲突
	forceErr         map[string]error // 方法名 → 强制错误
	maxResponseKeys  int              // >0 时每响应最多返回这么多条（模拟服务端小页）
	headNotFound     map[string]bool  // List 可见但 HEAD 时已消失的键
	retainOnDelete   bool             // 模拟 Delete 成功但对象仍可见
	headDelay        time.Duration    // 用于验证 List 的 HEAD 并发上限
	headInFlight     int
	maxHeadInFlight  int
}

type fakeObject struct {
	body        []byte
	contentType string
	userMeta    map[string]string
}

func newFakeOSSAPI() *fakeOSSAPI {
	return &fakeOSSAPI{
		objects:      make(map[string]fakeObject),
		forceErr:     make(map[string]error),
		headNotFound: make(map[string]bool),
	}
}

func (f *fakeOSSAPI) fail(name string) error {
	if err, ok := f.forceErr[name]; ok {
		return err
	}
	return nil
}

func serviceError(status int, code, message string) error {
	return &oss.ServiceError{StatusCode: status, Code: code, Message: message, RequestID: "req-fake"}
}

func (f *fakeOSSAPI) PutObject(ctx context.Context, request *oss.PutObjectRequest, _ ...func(*oss.Options)) (*oss.PutObjectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("PutObject"); err != nil {
		return nil, err
	}
	key := stringFromPtr(request.Key)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	if f.putConflictCalls > 0 {
		f.putConflictCalls--
		return nil, serviceError(409, "TaskAlreadyExist", "concurrent create")
	}
	f.objects[key] = fakeObject{
		body:        body,
		contentType: stringFromPtr(request.ContentType),
		userMeta:    cloneStringMap(request.Metadata),
	}
	return &oss.PutObjectResult{}, nil
}

func (f *fakeOSSAPI) HeadObject(ctx context.Context, request *oss.HeadObjectRequest, _ ...func(*oss.Options)) (*oss.HeadObjectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.headInFlight++
	if f.headInFlight > f.maxHeadInFlight {
		f.maxHeadInFlight = f.headInFlight
	}
	delay := f.headDelay
	f.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			f.mu.Lock()
			f.headInFlight--
			f.mu.Unlock()
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	f.mu.Lock()
	defer func() {
		f.headInFlight--
		f.mu.Unlock()
	}()
	if err := f.fail("HeadObject"); err != nil {
		return nil, err
	}
	key := stringFromPtr(request.Key)
	if f.headNotFound[key] {
		return nil, serviceError(404, "NoSuchKey", "object disappeared")
	}
	object, ok := f.objects[key]
	if !ok {
		return nil, serviceError(404, "NoSuchKey", "object not found")
	}
	return &oss.HeadObjectResult{
		ContentLength: int64(len(object.body)),
		ContentType:   oss.Ptr(object.contentType),
		LastModified:  oss.Ptr(fakeLastModified()),
		Metadata:      cloneStringMap(object.userMeta),
	}, nil
}

func (f *fakeOSSAPI) GetObject(ctx context.Context, request *oss.GetObjectRequest, _ ...func(*oss.Options)) (*oss.GetObjectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("GetObject"); err != nil {
		return nil, err
	}
	object, ok := f.objects[stringFromPtr(request.Key)]
	if !ok {
		return nil, serviceError(404, "NoSuchKey", "object not found")
	}
	return &oss.GetObjectResult{
		ContentLength: int64(len(object.body)),
		ContentType:   oss.Ptr(object.contentType),
		Body:          io.NopCloser(strings.NewReader(string(object.body))),
		Metadata:      cloneStringMap(object.userMeta),
	}, nil
}

func (f *fakeOSSAPI) DeleteObject(ctx context.Context, request *oss.DeleteObjectRequest, _ ...func(*oss.Options)) (*oss.DeleteObjectResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("DeleteObject"); err != nil {
		return nil, err
	}
	if !f.retainOnDelete {
		delete(f.objects, stringFromPtr(request.Key))
	}
	return &oss.DeleteObjectResult{}, nil
}

func (f *fakeOSSAPI) ListObjectsV2(ctx context.Context, request *oss.ListObjectsV2Request, _ ...func(*oss.Options)) (*oss.ListObjectsV2Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("ListObjectsV2"); err != nil {
		return nil, err
	}
	prefix := stringFromPtr(request.Prefix)
	after := stringFromPtr(request.StartAfter)
	if token := stringFromPtr(request.ContinuationToken); token != "" {
		decoded, err := decodeFakeToken(token)
		if err != nil {
			return nil, serviceError(400, "InvalidArgument", "continuation token 非法")
		}
		after = decoded
	}
	keys := make([]string, 0, len(f.objects))
	for key := range f.objects {
		if prefix != "" && !strings.HasPrefix(key, prefix) {
			continue
		}
		if key <= after {
			continue
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	maxKeys := int(request.MaxKeys)
	if maxKeys <= 0 {
		maxKeys = 100
	}
	if f.maxResponseKeys > 0 && maxKeys > f.maxResponseKeys {
		maxKeys = f.maxResponseKeys
	}
	end := len(keys)
	if end > maxKeys {
		end = maxKeys
	}
	contents := make([]oss.ObjectProperties, 0, end)
	for _, key := range keys[:end] {
		object := f.objects[key]
		contents = append(contents, oss.ObjectProperties{
			Key:  oss.Ptr(key),
			Size: int64(len(object.body)),
		})
	}
	result := &oss.ListObjectsV2Result{Contents: contents}
	if end < len(keys) {
		result.IsTruncated = true
		result.NextContinuationToken = oss.Ptr(encodeFakeToken(keys[end-1]))
	}
	return result, nil
}

func (f *fakeOSSAPI) GetBucketInfo(ctx context.Context, request *oss.GetBucketInfoRequest, _ ...func(*oss.Options)) (*oss.GetBucketInfoResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail("GetBucketInfo"); err != nil {
		return nil, err
	}
	if stringFromPtr(request.Bucket) == "" {
		return nil, serviceError(404, "NoSuchBucket", "bucket not found")
	}
	return &oss.GetBucketInfoResult{}, nil
}

func fakeLastModified() time.Time {
	return time.Unix(1770000000, 0).UTC()
}

func encodeFakeToken(afterKey string) string {
	return "tok-" + hex.EncodeToString([]byte(afterKey))
}

func decodeFakeToken(token string) (string, error) {
	if !strings.HasPrefix(token, "tok-") {
		return "", errors.New("bad token")
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(token, "tok-"))
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	clone := make(map[string]string, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func newTestOSS(api ossAPI) *OSS {
	return &OSS{api: api, bucket: "conf-bucket"}
}

func TestOSSConformanceAgainstFake(t *testing.T) {
	RunConformance(t, func(t *testing.T) (ObjectStore, string) {
		return newTestOSS(newFakeOSSAPI()), ""
	})
}

func TestOSSPutImmutableConflictConvergesToExisting(t *testing.T) {
	api := newFakeOSSAPI()
	store := newTestOSS(api)
	body := []byte("conflict-body")
	meta := Metadata{MediaType: "image/jpeg", Size: int64(len(body)), Checksum: checksum(body)}
	if _, created, err := store.PutImmutable(context.Background(), "neutral/a/k1", body, meta); err != nil || !created {
		t.Fatalf("首次 Put: err=%v created=%v", err, created)
	}
	// 写入新键时 fake 对 Put 返回 409 且对象确实不存在（模拟非版本化并发抢先删除等异常）。
	api.putConflictCalls = 1
	if _, created, err := store.PutImmutable(context.Background(), "neutral/a/k2", body, meta); !errors.Is(err, ErrIntegrity) || created {
		t.Fatalf("409 冲突应报 ErrIntegrity（对象实际未落成）: err=%v created=%v", err, created)
	}
	// 409 但对象已由并发方写入同内容 → 收敛为已存在。
	api.putConflictCalls = 1
	api.objects["neutral/a/k3"] = fakeObject{
		body:        body,
		contentType: "image/jpeg",
		userMeta:    metadataToUserMeta(meta),
	}
	result, created, err := store.PutImmutable(context.Background(), "neutral/a/k3", body, meta)
	if err != nil || created {
		t.Fatalf("409 收敛: err=%v created=%v", err, created)
	}
	if !sameMeta(result, meta) {
		t.Fatalf("409 收敛 meta 不符: %+v", result)
	}
}

func TestOSSPutImmutableRequiresChecksumInDomainKey(t *testing.T) {
	store := newTestOSS(newFakeOSSAPI())
	body := []byte("domain-key-checksum")
	meta := Metadata{MediaType: "image/png", Size: int64(len(body)), Checksum: checksum(body)}
	if _, _, err := store.PutImmutable(context.Background(), "planning/a/assets/x/g1/display", body, meta); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("planning key without checksum must be rejected, got %v", err)
	}
	key := "planning/a/assets/x/g1/" + meta.Checksum + "/display"
	if _, created, err := store.PutImmutable(context.Background(), key, body, meta); err != nil || !created {
		t.Fatalf("checksum-bound planning key rejected: created=%v err=%v", created, err)
	}
}

func TestOSSErrorClassification(t *testing.T) {
	store := newTestOSS(newFakeOSSAPI())
	if _, err := store.Stat(context.Background(), "planning/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("404 应 ErrNotFound, got %v", err)
	}
	api := newFakeOSSAPI()
	api.forceErr["HeadObject"] = serviceError(500, "InternalError", "boom")
	store = newTestOSS(api)
	if _, err := store.Stat(context.Background(), "planning/x"); !errors.Is(err, ErrTemporary) {
		t.Fatalf("5xx 应 ErrTemporary, got %v", err)
	}
	api.forceErr["HeadObject"] = serviceError(403, "AccessDenied", "denied")
	if _, err := store.Stat(context.Background(), "planning/x"); errors.Is(err, ErrTemporary) || errors.Is(err, ErrNotFound) {
		t.Fatalf("403 应保持永久错误, got %v", err)
	}
	api.forceErr["HeadObject"] = serviceError(404, "NoSuchBucket", "gone")
	if _, err := store.Stat(context.Background(), "planning/x"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("NoSuchBucket 不得伪装成对象不存在, got %v", err)
	}
	for _, status := range []int{http.StatusRequestTimeout, http.StatusTooManyRequests} {
		api.forceErr["HeadObject"] = serviceError(status, "Retryable", "retry later")
		if _, err := store.Stat(context.Background(), "planning/x"); !errors.Is(err, ErrTemporary) {
			t.Fatalf("HTTP %d 应 ErrTemporary, got %v", status, err)
		}
	}
	api.forceErr["HeadObject"] = &oss.ClientError{Code: "INVALID_ARGUMENT", Message: "bad param"}
	if _, err := store.Stat(context.Background(), "planning/x"); errors.Is(err, ErrTemporary) || errors.Is(err, ErrNotFound) {
		t.Fatalf("客户端侧错误应原样上抛（永久错误）, got %v", err)
	}
}

func TestOSSStatRejectsForeignObjectWithoutUserMeta(t *testing.T) {
	api := newFakeOSSAPI()
	api.objects["planning/foreign"] = fakeObject{
		body:        []byte("foreign"),
		contentType: "application/octet-stream",
	}
	store := newTestOSS(api)
	if _, err := store.Stat(context.Background(), "planning/foreign"); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("外来对象应 ErrIntegrity, got %v", err)
	}
}

func TestOSSStatRejectsMissingRequiredSizeMetadata(t *testing.T) {
	api := newFakeOSSAPI()
	body := []byte("missing-size-meta")
	api.objects["planning/foreign"] = fakeObject{
		body:        body,
		contentType: "image/png",
		userMeta: map[string]string{
			ossMetaMediaType: "image/png",
			ossMetaChecksum:  checksum(body),
		},
	}
	if _, err := newTestOSS(api).Stat(context.Background(), "planning/foreign"); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("缺少 size 用户元数据必须 ErrIntegrity, got %v", err)
	}
}

func TestOSSListSkipsObjectDeletedAfterListing(t *testing.T) {
	api := newFakeOSSAPI()
	store := newTestOSS(api)
	body := []byte("listed-then-deleted")
	meta := Metadata{MediaType: "image/png", Size: int64(len(body)), Checksum: checksum(body)}
	for _, key := range []string{"planning/a/gone", "planning/a/live"} {
		api.objects[key] = fakeObject{body: body, contentType: meta.MediaType, userMeta: metadataToUserMeta(meta)}
	}
	api.headNotFound["planning/a/gone"] = true

	page, err := store.List(context.Background(), "planning/a/", "", 10)
	if err != nil {
		t.Fatalf("List 应跳过扫描期间消失的对象: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Key != "planning/a/live" || !page.Done {
		t.Fatalf("List 结果不符: %+v", page)
	}
}

func TestOSSDeleteRequiresNotFoundBarrier(t *testing.T) {
	api := newFakeOSSAPI()
	body := []byte("delete-barrier")
	meta := Metadata{MediaType: "image/png", Size: int64(len(body)), Checksum: checksum(body)}
	api.objects["planning/a/still-visible"] = fakeObject{body: body, contentType: meta.MediaType, userMeta: metadataToUserMeta(meta)}
	api.retainOnDelete = true

	if err := newTestOSS(api).Delete(context.Background(), "planning/a/still-visible"); !errors.Is(err, ErrTemporary) {
		t.Fatalf("Delete 后对象仍可见必须 ErrTemporary, got %v", err)
	}
}

func TestOSSPutReturnsPersistedLastModified(t *testing.T) {
	store := newTestOSS(newFakeOSSAPI())
	body := []byte("persisted-last-modified")
	meta := Metadata{MediaType: "image/png", Size: int64(len(body)), Checksum: checksum(body)}
	stored, created, err := store.PutImmutable(context.Background(), "neutral/a/put-meta", body, meta)
	if err != nil || !created {
		t.Fatalf("PutImmutable: created=%v err=%v", created, err)
	}
	if !stored.ModifiedAt.Equal(fakeLastModified()) {
		t.Fatalf("PutImmutable ModifiedAt=%v, want persisted %v", stored.ModifiedAt, fakeLastModified())
	}
}

func TestOSSListAggregatesBeyondSinglePageWithKeysetCursor(t *testing.T) {
	api := newFakeOSSAPI()
	store := newTestOSS(api)
	body := []byte("agg")
	meta := Metadata{MediaType: "image/jpeg", Size: int64(len(body)), Checksum: checksum(body)}
	for _, name := range []string{"agg/a1", "agg/a2", "agg/a3", "agg/a4", "agg/a5"} {
		if _, _, err := store.PutImmutable(context.Background(), name, body, meta); err != nil {
			t.Fatalf("PutImmutable(%q): %v", name, err)
		}
	}
	// 每页只回 2 条，验证内部翻页聚合与 keyset 游标。
	seen := make([]string, 0, 5)
	cursor := ""
	pages := 0
	for {
		page, err := store.List(context.Background(), "agg", cursor, 2)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		pages++
		if !page.Done && len(page.Items) != 2 {
			t.Fatalf("非末页大小应恰为 limit=2, got %d", len(page.Items))
		}
		if page.Done && len(page.Items) > 2 {
			t.Fatalf("末页大小不应超过 limit=2, got %d", len(page.Items))
		}
		for _, item := range page.Items {
			seen = append(seen, item.Key)
		}
		if page.Done {
			break
		}
		cursor = page.NextCursor
		if pages > 10 {
			t.Fatalf("翻页未收敛")
		}
	}
	want := []string{"agg/a1", "agg/a2", "agg/a3", "agg/a4", "agg/a5"}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("聚合结果不符: %v", seen)
	}
}

func TestOSSListAggregatesServerSideShortPages(t *testing.T) {
	api := newFakeOSSAPI()
	api.maxResponseKeys = 1 // 服务端每次只回 1 条，单次 List 必须多轮聚合。
	store := newTestOSS(api)
	body := []byte("short-page")
	meta := Metadata{MediaType: "image/jpeg", Size: int64(len(body)), Checksum: checksum(body)}
	for _, name := range []string{"short/b1", "short/b2", "short/b3"} {
		if _, _, err := store.PutImmutable(context.Background(), name, body, meta); err != nil {
			t.Fatalf("PutImmutable(%q): %v", name, err)
		}
	}
	page, err := store.List(context.Background(), "short", "", 3)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 3 || !page.Done {
		t.Fatalf("多轮聚合应凑满 3 条且 done, got n=%d done=%v", len(page.Items), page.Done)
	}
	got := []string{page.Items[0].Key, page.Items[1].Key, page.Items[2].Key}
	want := []string{"short/b1", "short/b2", "short/b3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("聚合顺序不符: %v", got)
	}
}

func TestOSSListBoundsConcurrentMetadataHeads(t *testing.T) {
	api := newFakeOSSAPI()
	store := newTestOSS(api)
	body := []byte("concurrency")
	meta := Metadata{MediaType: "image/png", Size: int64(len(body)), Checksum: checksum(body)}
	for index := range 24 {
		key := fmt.Sprintf("neutral/bounded/%02d", index)
		api.objects[key] = fakeObject{body: body, contentType: meta.MediaType, userMeta: metadataToUserMeta(meta)}
	}
	api.headDelay = 2 * time.Millisecond
	page, err := store.List(context.Background(), "neutral/bounded/", "", 24)
	if err != nil || len(page.Items) != 24 {
		t.Fatalf("List: n=%d err=%v", len(page.Items), err)
	}
	if api.maxHeadInFlight < 2 || api.maxHeadInFlight > listHeadConcurrency {
		t.Fatalf("HEAD concurrency=%d, want 2..%d", api.maxHeadInFlight, listHeadConcurrency)
	}
}

func TestOSSProbeRejectsMissingBucket(t *testing.T) {
	store := &OSS{api: newFakeOSSAPI(), bucket: ""}
	if err := store.Probe(context.Background()); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("空 bucket 探活应保留 bucket 故障, got %v", err)
	}
}

func TestOSSDeleteDoesNotTreatMissingBucketAsIdempotentSuccess(t *testing.T) {
	api := newFakeOSSAPI()
	api.forceErr["DeleteObject"] = serviceError(http.StatusNotFound, "NoSuchBucket", "gone")
	store := newTestOSS(api)
	if err := store.Delete(context.Background(), "planning/a/object"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("bucket 不存在时 Delete 必须失败且不可映射为对象缺失, got %v", err)
	}
}

func TestNewOSSClientConfigEnablesCNameExplicitly(t *testing.T) {
	cfg := newOSSClientConfig(OSSConfig{
		Region: "cn-hangzhou", Endpoint: "https://media.example.com", Bucket: "crm", UseCName: true,
	})
	if cfg.UseCName == nil || !*cfg.UseCName {
		t.Fatal("custom domain endpoint must enable SDK CNAME mode")
	}
	regular := newOSSClientConfig(OSSConfig{
		Region: "cn-hangzhou", Endpoint: "oss-cn-hangzhou-internal.aliyuncs.com", Bucket: "crm",
	})
	if regular.UseCName != nil && *regular.UseCName {
		t.Fatal("regular OSS endpoint must not enable CNAME mode")
	}
}

func TestEnvironmentCredentialsSelectionTreatsEmptyComposeValuesAsUnset(t *testing.T) {
	t.Setenv("OSS_ACCESS_KEY_ID", "")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "")
	if hasEnvironmentOSSCredentials() {
		t.Fatal("empty compose credentials must allow ECS role fallback")
	}
	t.Setenv("OSS_ACCESS_KEY_ID", "partial")
	if !hasEnvironmentOSSCredentials() {
		t.Fatal("partial environment credentials must be selected and fail closed")
	}
}
